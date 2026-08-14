//go:build windows

package main

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"
)

var (
	resourceKernel32              = syscall.NewLazyDLL("kernel32.dll")
	resourceUser32                = syscall.NewLazyDLL("user32.dll")
	pGlobalMemoryStatusEx         = resourceKernel32.NewProc("GlobalMemoryStatusEx")
	pGetSystemPowerStatus         = resourceKernel32.NewProc("GetSystemPowerStatus")
	pGetDiskFreeSpaceExW          = resourceKernel32.NewProc("GetDiskFreeSpaceExW")
	pGetLogicalDrives             = resourceKernel32.NewProc("GetLogicalDrives")
	pGetDriveTypeW                = resourceKernel32.NewProc("GetDriveTypeW")
	pLockWorkStation              = resourceUser32.NewProc("LockWorkStation")
	pPdhGetFormattedCounterArrayW = metricPdh.NewProc("PdhGetFormattedCounterArrayW")
)

type memoryStatusEx struct {
	Length               uint32
	MemoryLoad           uint32
	TotalPhys            uint64
	AvailPhys            uint64
	TotalPageFile        uint64
	AvailPageFile        uint64
	TotalVirtual         uint64
	AvailVirtual         uint64
	AvailExtendedVirtual uint64
}

type systemPowerStatus struct {
	ACLineStatus        byte
	BatteryFlag         byte
	BatteryLifePercent  byte
	SystemStatusFlag    byte
	BatteryLifeTime     uint32
	BatteryFullLifeTime uint32
}

type pdhFmtCounterValueItem struct {
	Name  *uint16
	Value pdhFmtValue
}

const pdhMoreData = 0x800007D2

func sampleRAM() (percent, usedGB, totalGB float64) {
	st := memoryStatusEx{Length: uint32(unsafe.Sizeof(memoryStatusEx{}))}
	ok, _, _ := pGlobalMemoryStatusEx.Call(uintptr(unsafe.Pointer(&st)))
	if ok == 0 || st.TotalPhys == 0 {
		return 0, 0, 0
	}
	used := st.TotalPhys - st.AvailPhys
	const gib = 1024.0 * 1024.0 * 1024.0
	return float64(st.MemoryLoad), float64(used) / gib, float64(st.TotalPhys) / gib
}

func samplePowerStatus() (percent float64, onAC bool) {
	var st systemPowerStatus
	ok, _, _ := pGetSystemPowerStatus.Call(uintptr(unsafe.Pointer(&st)))
	if ok == 0 {
		return -1, false
	}
	pct := float64(st.BatteryLifePercent)
	if st.BatteryLifePercent == 255 {
		pct = -1
	}
	return pct, st.ACLineStatus == 1
}

func currentPowerStatus() systemPowerStatus {
	var st systemPowerStatus
	pGetSystemPowerStatus.Call(uintptr(unsafe.Pointer(&st)))
	return st
}

func initGPUCounters() {
	var query uintptr
	r, _, _ := pPdhOpenQueryW.Call(0, 0, uintptr(unsafe.Pointer(&query)))
	if uint32(r) != 0 || query == 0 {
		return
	}
	gpuPath, _ := syscall.UTF16PtrFromString(`\GPU Engine(*)\Utilization Percentage`)
	var gpu uintptr
	r, _, _ = pPdhAddEnglishCounterW.Call(query, uintptr(unsafe.Pointer(gpuPath)), 0, uintptr(unsafe.Pointer(&gpu)))
	if uint32(r) != 0 || gpu == 0 {
		pPdhCloseQuery.Call(query)
		return
	}
	vramPath, _ := syscall.UTF16PtrFromString(`\GPU Adapter Memory(*)\Dedicated Usage`)
	var vram uintptr
	rv, _, _ := pPdhAddEnglishCounterW.Call(query, uintptr(unsafe.Pointer(vramPath)), 0, uintptr(unsafe.Pointer(&vram)))
	if uint32(rv) != 0 {
		vram = 0
	}
	pPdhCollectQueryData.Call(query)
	metricState.Lock()
	metricState.gpuQuery, metricState.gpuCounter, metricState.vramCounter, metricState.gpuPrimed = query, gpu, vram, true
	metricState.Unlock()
}

func closeGPUCounters() {
	metricState.Lock()
	q := metricState.gpuQuery
	metricState.gpuQuery, metricState.gpuCounter, metricState.vramCounter, metricState.gpuPrimed = 0, 0, 0, false
	metricState.Unlock()
	if q != 0 {
		pPdhCloseQuery.Call(q)
	}
}

func samplePDHArray(counter uintptr, mode string) float64 {
	if counter == 0 {
		return -1
	}
	var size, count uint32
	r, _, _ := pPdhGetFormattedCounterArrayW.Call(counter, pdhFmtDouble, uintptr(unsafe.Pointer(&size)), uintptr(unsafe.Pointer(&count)), 0)
	if uint32(r) != pdhMoreData || size == 0 {
		return -1
	}
	buf := make([]byte, int(size))
	r, _, _ = pPdhGetFormattedCounterArrayW.Call(counter, pdhFmtDouble, uintptr(unsafe.Pointer(&size)), uintptr(unsafe.Pointer(&count)), uintptr(unsafe.Pointer(&buf[0])))
	if uint32(r) != 0 || count == 0 {
		return -1
	}
	items := unsafe.Slice((*pdhFmtCounterValueItem)(unsafe.Pointer(&buf[0])), int(count))
	result := 0.0
	found := false
	for _, item := range items {
		v := item.Value.Double
		if v < 0 {
			continue
		}
		found = true
		if mode == "sum" {
			result += v
		} else if v > result {
			result = v
		}
	}
	if !found {
		return -1
	}
	return result
}

func samplePDHNamedArray(counter uintptr) []pdhFmtCounterValueItem {
	if counter == 0 {
		return nil
	}
	var size, count uint32
	r, _, _ := pPdhGetFormattedCounterArrayW.Call(counter, pdhFmtDouble, uintptr(unsafe.Pointer(&size)), uintptr(unsafe.Pointer(&count)), 0)
	if uint32(r) != pdhMoreData || size == 0 {
		return nil
	}
	buf := make([]byte, int(size))
	r, _, _ = pPdhGetFormattedCounterArrayW.Call(counter, pdhFmtDouble, uintptr(unsafe.Pointer(&size)), uintptr(unsafe.Pointer(&count)), uintptr(unsafe.Pointer(&buf[0])))
	if uint32(r) != 0 || count == 0 {
		return nil
	}
	items := unsafe.Slice((*pdhFmtCounterValueItem)(unsafe.Pointer(&buf[0])), int(count))
	out := make([]pdhFmtCounterValueItem, len(items))
	copy(out, items)
	return out
}

func gpuPIDFromInstance(name string) uint32 {
	name = strings.ToLower(name)
	i := strings.Index(name, "pid_")
	if i < 0 {
		return 0
	}
	i += 4
	j := i
	for j < len(name) && name[j] >= '0' && name[j] <= '9' {
		j++
	}
	if j == i {
		return 0
	}
	var pid uint32
	for _, ch := range name[i:j] {
		pid = pid*10 + uint32(ch-'0')
	}
	return pid
}

func sampleGPUMetricsDetailed() (utilization, dedicatedMB float64, byPID map[uint32]float64) {
	metricState.RLock()
	q, gpu, vram, ok := metricState.gpuQuery, metricState.gpuCounter, metricState.vramCounter, metricState.gpuPrimed
	metricState.RUnlock()
	byPID = map[uint32]float64{}
	if !ok || q == 0 || gpu == 0 {
		return -1, -1, byPID
	}
	if r, _, _ := pPdhCollectQueryData.Call(q); uint32(r) != 0 {
		return -1, -1, byPID
	}
	utilization = -1
	for _, item := range samplePDHNamedArray(gpu) {
		v := item.Value.Double
		if v < 0 || item.Name == nil {
			continue
		}
		if utilization < 0 || v > utilization {
			utilization = v
		}
		name := utf16PtrStringRM(item.Name)
		pid := gpuPIDFromInstance(name)
		if pid != 0 && v > byPID[pid] {
			byPID[pid] = v
		}
	}
	if utilization > 100 {
		utilization = 100
	}
	bytes := samplePDHArray(vram, "sum")
	if bytes < 0 {
		dedicatedMB = -1
	} else {
		dedicatedMB = bytes / (1024.0 * 1024.0)
	}
	return utilization, dedicatedMB, byPID
}

func sampleGPUMetrics() (utilization, dedicatedMB float64) {
	utilization, dedicatedMB, _ = sampleGPUMetricsDetailed()
	return
}

func logicalDriveNames() []string {
	mask, _, _ := pGetLogicalDrives.Call()
	if mask == 0 {
		return []string{"C:"}
	}
	out := make([]string, 0, 8)
	for i := 0; i < 26; i++ {
		if mask&(1<<i) == 0 {
			continue
		}
		name := string(rune('A'+i)) + ":"
		root := name + `\`
		p, _ := syscall.UTF16PtrFromString(root)
		t, _, _ := pGetDriveTypeW.Call(uintptr(unsafe.Pointer(p)))
		// Skip unknown/no-root and optical drives. Fixed/removable/network volumes are useful.
		if t == 0 || t == 1 || t == 5 {
			continue
		}
		out = append(out, name)
	}
	if len(out) == 0 {
		out = append(out, "C:")
	}
	return out
}

func diskFreeInfo(path string) (freeGB, totalGB, percentFree float64, ok bool) {
	path = strings.TrimSpace(path)
	if path == "" {
		path = `C:\`
	}
	if fi, err := os.Stat(path); err == nil && !fi.IsDir() {
		path = filepath.Dir(path)
	}
	p, _ := syscall.UTF16PtrFromString(path)
	var avail, total, free uint64
	r, _, _ := pGetDiskFreeSpaceExW.Call(uintptr(unsafe.Pointer(p)), uintptr(unsafe.Pointer(&avail)), uintptr(unsafe.Pointer(&total)), uintptr(unsafe.Pointer(&free)))
	if r == 0 || total == 0 {
		return 0, 0, 0, false
	}
	const gib = 1024.0 * 1024.0 * 1024.0
	return float64(free) / gib, float64(total) / gib, float64(free) * 100 / float64(total), true
}

func lockWorkstation040() bool {
	r, _, _ := pLockWorkStation.Call()
	return r != 0
}

func utf16PtrStringRM(p *uint16) string {
	if p == nil {
		return ""
	}
	buf := make([]uint16, 0, 128)
	for i := 0; i < 4096; i++ {
		v := *(*uint16)(unsafe.Pointer(uintptr(unsafe.Pointer(p)) + uintptr(i*2)))
		if v == 0 {
			break
		}
		buf = append(buf, v)
	}
	return syscall.UTF16ToString(buf)
}
