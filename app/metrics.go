//go:build windows

package main

import (
	"math"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

var (
	metricKernel32 = syscall.NewLazyDLL("kernel32.dll")
	metricPdh      = syscall.NewLazyDLL("pdh.dll")
	metricIP       = syscall.NewLazyDLL("iphlpapi.dll")

	pGetSystemTimes         = metricKernel32.NewProc("GetSystemTimes")
	pPdhOpenQueryW          = metricPdh.NewProc("PdhOpenQueryW")
	pPdhAddEnglishCounterW  = metricPdh.NewProc("PdhAddEnglishCounterW")
	pPdhCollectQueryData    = metricPdh.NewProc("PdhCollectQueryData")
	pPdhGetFormattedCounter = metricPdh.NewProc("PdhGetFormattedCounterValue")
	pPdhCloseQuery          = metricPdh.NewProc("PdhCloseQuery")
	pGetIfTable             = metricIP.NewProc("GetIfTable")
)

const pdhFmtDouble = 0x00000200

type FILETIME struct{ LowDateTime, HighDateTime uint32 }
type pdhFmtValue struct {
	CStatus uint32
	_pad    uint32
	Double  float64
}

type mibIfRow struct {
	Name            [256]uint16
	Index           uint32
	Type            uint32
	MTU             uint32
	Speed           uint32
	PhysAddrLen     uint32
	PhysAddr        [8]byte
	AdminStatus     uint32
	OperStatus      uint32
	LastChange      uint32
	InOctets        uint32
	InUcastPkts     uint32
	InNUcastPkts    uint32
	InDiscards      uint32
	InErrors        uint32
	InUnknownProtos uint32
	OutOctets       uint32
	OutUcastPkts    uint32
	OutNUcastPkts   uint32
	OutDiscards     uint32
	OutErrors       uint32
	OutQLen         uint32
	DescrLen        uint32
	Descr           [256]byte
}

type DiskMetric struct {
	Name     string
	Activity float64
	FreeGB   float64
	TotalGB  float64
}

type MetricSnapshot struct {
	CPU             float64
	GPU             float64
	RAMPercent      float64
	RAMUsedGB       float64
	RAMTotalGB      float64
	NetworkKBps     float64
	NetworkDownKBps float64
	NetworkUpKBps   float64
	DiskPercent     float64
	DiskFreeGB      float64
	DiskTotalGB     float64
	Disks           []DiskMetric
	VRAMUsedMB      float64
	BatteryPercent  float64
	OnAC            bool
	Updated         time.Time
}

var metricState struct {
	sync.RWMutex
	snap                        MetricSnapshot
	stop                        chan struct{}
	cpuIdle, cpuKernel, cpuUser uint64
	cpuInit                     bool
	netPrev                     map[uint32]uint64
	netAt                       time.Time
	diskQuery                   uintptr
	diskCounter                 uintptr
	diskCounters                map[string]uintptr
	diskPrimed                  bool
	gpuQuery                    uintptr
	gpuCounter                  uintptr
	vramCounter                 uintptr
	gpuPrimed                   bool
	interval                    time.Duration
	history                     []MetricSnapshot
	processes                   []ProcessMetric
	processAt                   time.Time
}

func startMetricSampler() {
	metricState.Lock()
	if metricState.stop != nil {
		metricState.Unlock()
		return
	}
	metricState.stop = make(chan struct{})
	stopCh := metricState.stop
	metricState.netPrev = map[uint32]uint64{}
	metricState.snap.GPU = -1
	metricState.snap.DiskPercent = -1
	metricState.snap.VRAMUsedMB = -1
	metricState.snap.BatteryPercent = -1
	metricState.interval = time.Duration(clampInt(app.settings.ResourceRefreshMS, 250, 5000)) * time.Millisecond
	metricState.Unlock()
	initResourceStats()
	initDiskCounter()
	initGPUCounters()
	startTemperatureSampler()
	go func() {
		for {
			sampleMetrics()
			metricState.RLock()
			d := metricState.interval
			metricState.RUnlock()
			if d < 250*time.Millisecond {
				d = time.Second
			}
			t := time.NewTimer(d)
			select {
			case <-t.C:
			case <-stopCh:
				if !t.Stop() {
					select {
					case <-t.C:
					default:
					}
				}
				closeDiskCounter()
				closeGPUCounters()
				flushResourceStats(true)
				return
			}
		}
	}()
}

func stopMetricSampler() {
	stopTemperatureSampler()
	metricState.Lock()
	if metricState.stop != nil {
		close(metricState.stop)
		metricState.stop = nil
	}
	metricState.Unlock()
}

func metricsSnapshot() MetricSnapshot {
	metricState.RLock()
	s := metricState.snap
	metricState.RUnlock()
	return s
}

func metricHistory() []MetricSnapshot {
	metricState.RLock()
	out := append([]MetricSnapshot(nil), metricState.history...)
	metricState.RUnlock()
	return out
}

func setMetricRefreshInterval(ms int) {
	ms = clampInt(ms, 250, 5000)
	metricState.Lock()
	metricState.interval = time.Duration(ms) * time.Millisecond
	metricState.Unlock()
}

func sampleMetrics() {
	cpu := sampleCPU()
	down, up := sampleNetwork()
	disk, disks := sampleDisks()
	ramPct, ramUsed, ramTotal := sampleRAM()
	gpu, vram, gpuByPID := sampleGPUMetricsDetailed()
	batt, onAC := samplePowerStatus()
	if gpu < 0 {
		metricState.RLock()
		gpu = metricState.snap.GPU
		metricState.RUnlock()
	}
	if vram < 0 {
		metricState.RLock()
		vram = metricState.snap.VRAMUsedMB
		metricState.RUnlock()
	}
	diskFree, diskTotal := -1.0, -1.0
	if len(disks) > 0 {
		diskFree, diskTotal = disks[0].FreeGB, disks[0].TotalGB
	}
	now := time.Now()
	snap := MetricSnapshot{CPU: cpu, GPU: gpu, RAMPercent: ramPct, RAMUsedGB: ramUsed, RAMTotalGB: ramTotal,
		NetworkKBps: (down + up) / 1024.0, NetworkDownKBps: down / 1024.0, NetworkUpKBps: up / 1024.0,
		DiskPercent: disk, DiskFreeGB: diskFree, DiskTotalGB: diskTotal, Disks: disks, VRAMUsedMB: vram, BatteryPercent: batt, OnAC: onAC, Updated: now}
	normalizeMetricSnapshot(&snap)
	metricState.Lock()
	metricState.snap = snap
	metricState.history = append(metricState.history, cloneMetricSnapshot(snap))
	if len(metricState.history) > 360 {
		metricState.history = append([]MetricSnapshot(nil), metricState.history[len(metricState.history)-360:]...)
	}
	shouldProc := metricState.processAt.IsZero() || now.Sub(metricState.processAt) >= time.Second
	metricState.Unlock()
	if shouldProc {
		procs := sampleProcessMetrics(now, gpuByPID)
		metricState.Lock()
		metricState.processes = procs
		metricState.processAt = now
		metricState.Unlock()
		recordResourceStats(now, snap, procs)
	}
	if app.hwnd != 0 && (app.section == 18 || app.section == 19 || app.section == 20) {
		pPostMessageW.Call(app.hwnd, WM_RESOURCE_UPDATED, 0, 0)
	}
}

func cloneMetricSnapshot(s MetricSnapshot) MetricSnapshot {
	s.Disks = append([]DiskMetric(nil), s.Disks...)
	return s
}

func processMetricSnapshot() []ProcessMetric {
	metricState.RLock()
	out := append([]ProcessMetric(nil), metricState.processes...)
	metricState.RUnlock()
	return out
}

func filetime64(f FILETIME) uint64 { return uint64(f.HighDateTime)<<32 | uint64(f.LowDateTime) }

func normalizePercentageMetric(v float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return -1
	}
	if v < 0 {
		return -1
	}
	if v > 100 {
		return 100
	}
	return v
}

func normalizeRateMetric(v float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
		return 0
	}
	return v
}

func normalizeMetricSnapshot(s *MetricSnapshot) {
	s.CPU = normalizePercentageMetric(s.CPU)
	s.GPU = normalizePercentageMetric(s.GPU)
	s.RAMPercent = normalizePercentageMetric(s.RAMPercent)
	s.DiskPercent = normalizePercentageMetric(s.DiskPercent)
	s.BatteryPercent = normalizePercentageMetric(s.BatteryPercent)
	s.NetworkDownKBps = normalizeRateMetric(s.NetworkDownKBps)
	s.NetworkUpKBps = normalizeRateMetric(s.NetworkUpKBps)
	s.NetworkKBps = s.NetworkDownKBps + s.NetworkUpKBps
	for i := range s.Disks {
		s.Disks[i].Activity = normalizePercentageMetric(s.Disks[i].Activity)
	}
}

func sampleCPU() float64 {
	var idle, kernel, user FILETIME
	ok, _, _ := pGetSystemTimes.Call(uintptr(unsafe.Pointer(&idle)), uintptr(unsafe.Pointer(&kernel)), uintptr(unsafe.Pointer(&user)))
	if ok == 0 {
		return 0
	}
	i, k, u := filetime64(idle), filetime64(kernel), filetime64(user)
	metricState.Lock()
	defer metricState.Unlock()
	if !metricState.cpuInit {
		metricState.cpuInit = true
		metricState.cpuIdle, metricState.cpuKernel, metricState.cpuUser = i, k, u
		return 0
	}
	di, dk, du := i-metricState.cpuIdle, k-metricState.cpuKernel, u-metricState.cpuUser
	metricState.cpuIdle, metricState.cpuKernel, metricState.cpuUser = i, k, u
	total := dk + du
	if total == 0 {
		return 0
	}
	busy := float64(total-di) * 100 / float64(total)
	if busy < 0 {
		busy = 0
	}
	if busy > 100 {
		busy = 100
	}
	return busy
}

func sampleNetwork() (float64, float64) {
	var size uint32
	pGetIfTable.Call(0, uintptr(unsafe.Pointer(&size)), 0)
	if size < 4 {
		return 0, 0
	}
	buf := make([]byte, size)
	r, _, _ := pGetIfTable.Call(uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size)), 0)
	if uint32(r) != 0 {
		return 0, 0
	}
	count := *(*uint32)(unsafe.Pointer(&buf[0]))
	rowSize := int(unsafe.Sizeof(mibIfRow{}))
	now := time.Now()
	metricState.Lock()
	defer metricState.Unlock()
	if metricState.netPrev == nil {
		metricState.netPrev = map[uint32]uint64{}
	}
	var inDiff, outDiff uint64
	for i := 0; i < int(count); i++ {
		off := 4 + i*rowSize
		if off+rowSize > len(buf) {
			break
		}
		row := (*mibIfRow)(unsafe.Pointer(&buf[off]))
		if !usableNetworkInterface(row) {
			continue
		}
		curIn, curOut := row.InOctets, row.OutOctets
		if prev, ok := metricState.netPrev[row.Index]; ok {
			prevIn := uint32(prev >> 32)
			prevOut := uint32(prev)
			inDiff += uint64(uint32(curIn - prevIn))
			outDiff += uint64(uint32(curOut - prevOut))
		}
		// Store both 32-bit counters in one uint64-sized map value.
		metricState.netPrev[row.Index] = uint64(curIn)<<32 | uint64(curOut)
	}
	if metricState.netAt.IsZero() {
		metricState.netAt = now
		return 0, 0
	}
	seconds := now.Sub(metricState.netAt).Seconds()
	metricState.netAt = now
	if seconds <= 0 {
		return 0, 0
	}
	return float64(inDiff) / seconds, float64(outDiff) / seconds
}

func usableNetworkInterface(row *mibIfRow) bool {
	if row == nil || row.AdminStatus != 1 || row.OperStatus != 5 {
		return false
	}
	// Software loopback and tunnel adapters either duplicate physical traffic
	// or report traffic that never reaches the network card.
	if row.Type == 24 || row.Type == 131 {
		return false
	}
	n := int(row.DescrLen)
	if n > len(row.Descr) {
		n = len(row.Descr)
	}
	description := strings.ToLower(string(row.Descr[:n]))
	for _, marker := range []string{"virtual", "hyper-v", "vmware", "loopback", "tunnel", "teredo", "isatap", "wintun", "wireguard", "tap-windows"} {
		if strings.Contains(description, marker) {
			return false
		}
	}
	return true
}

func initDiskCounter() {
	var query uintptr
	r, _, _ := pPdhOpenQueryW.Call(0, 0, uintptr(unsafe.Pointer(&query)))
	if uint32(r) != 0 || query == 0 {
		return
	}
	path, _ := syscall.UTF16PtrFromString(`\PhysicalDisk(_Total)\% Disk Time`)
	var counter uintptr
	r, _, _ = pPdhAddEnglishCounterW.Call(query, uintptr(unsafe.Pointer(path)), 0, uintptr(unsafe.Pointer(&counter)))
	if uint32(r) != 0 {
		counter = 0
	}
	counters := map[string]uintptr{}
	for _, drive := range logicalDriveNames() {
		cp, _ := syscall.UTF16PtrFromString(`\LogicalDisk(` + drive + `)\% Disk Time`)
		var c uintptr
		rc, _, _ := pPdhAddEnglishCounterW.Call(query, uintptr(unsafe.Pointer(cp)), 0, uintptr(unsafe.Pointer(&c)))
		if uint32(rc) == 0 && c != 0 {
			counters[drive] = c
		}
	}
	pPdhCollectQueryData.Call(query)
	metricState.Lock()
	metricState.diskQuery, metricState.diskCounter, metricState.diskCounters, metricState.diskPrimed = query, counter, counters, true
	metricState.Unlock()
}

func formattedPDHCounter(c uintptr) float64 {
	if c == 0 {
		return -1
	}
	var typ uint32
	var val pdhFmtValue
	r, _, _ := pPdhGetFormattedCounter.Call(c, pdhFmtDouble, uintptr(unsafe.Pointer(&typ)), uintptr(unsafe.Pointer(&val)))
	if uint32(r) != 0 {
		return -1
	}
	v := val.Double
	if v < 0 {
		v = 0
	}
	if v > 100 {
		v = 100
	}
	return v
}

func sampleDisks() (float64, []DiskMetric) {
	metricState.RLock()
	q, total, counters, ok := metricState.diskQuery, metricState.diskCounter, metricState.diskCounters, metricState.diskPrimed
	metricState.RUnlock()
	if !ok || q == 0 {
		return -1, nil
	}
	if r, _, _ := pPdhCollectQueryData.Call(q); uint32(r) != 0 {
		return -1, nil
	}
	totalV := formattedPDHCounter(total)
	drives := logicalDriveNames()
	out := make([]DiskMetric, 0, len(drives))
	for _, drive := range drives {
		activity := -1.0
		if c := counters[drive]; c != 0 {
			activity = formattedPDHCounter(c)
		}
		free, totalGB, _, ok := diskFreeInfo(drive + `\`)
		if !ok {
			free, totalGB = -1, -1
		}
		out = append(out, DiskMetric{Name: drive, Activity: activity, FreeGB: free, TotalGB: totalGB})
	}
	return totalV, out
}

func sampleDisk() float64 {
	v, _ := sampleDisks()
	return v
}

func closeDiskCounter() {
	metricState.Lock()
	q := metricState.diskQuery
	metricState.diskQuery, metricState.diskCounter, metricState.diskCounters, metricState.diskPrimed = 0, 0, nil, false
	metricState.Unlock()
	if q != 0 {
		pPdhCloseQuery.Call(q)
	}
}
