//go:build windows

package main

import (
	"runtime"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

var (
	processPsapi            = syscall.NewLazyDLL("psapi.dll")
	pGetProcessMemoryInfo   = processPsapi.NewProc("GetProcessMemoryInfo")
	pGetProcessTimesRM      = resourceKernel32.NewProc("GetProcessTimes")
	pGetProcessIoCountersRM = resourceKernel32.NewProc("GetProcessIoCounters")
)

const (
	processQueryInformationRM = 0x0400
	processVMReadRM           = 0x0010
)

type processMemoryCounters struct {
	Cb                         uint32
	PageFaultCount             uint32
	PeakWorkingSetSize         uintptr
	WorkingSetSize             uintptr
	QuotaPeakPagedPoolUsage    uintptr
	QuotaPagedPoolUsage        uintptr
	QuotaPeakNonPagedPoolUsage uintptr
	QuotaNonPagedPoolUsage     uintptr
	PagefileUsage              uintptr
	PeakPagefileUsage          uintptr
}

type ioCountersRM struct {
	ReadOperationCount  uint64
	WriteOperationCount uint64
	OtherOperationCount uint64
	ReadTransferCount   uint64
	WriteTransferCount  uint64
	OtherTransferCount  uint64
}

type ProcessMetric struct {
	Name       string
	PID        uint32
	System     bool
	CPUPercent float64
	GPUPercent float64
	RAMMB      float64
	ReadKBps   float64
	WriteKBps  float64
	OtherKBps  float64
	Threads    uint32
	ImagePath  string
	Updated    time.Time
}

type processPrevSample struct {
	Proc100ns  uint64
	At         time.Time
	ReadBytes  uint64
	WriteBytes uint64
	OtherBytes uint64
}

var procMetricRuntime = struct {
	sync.Mutex
	Prev map[uint32]processPrevSample
}{Prev: map[uint32]processPrevSample{}}

func processInstancesForMetrics() []ProcessInfo {
	snap, _, _ := pCreateToolhelp32Snapshot.Call(TH32CS_SNAPPROCESS, 0)
	if snap == INVALID_HANDLE_VALUE || snap == 0 {
		return nil
	}
	defer pCloseHandle.Call(snap)
	pe := PROCESSENTRY32{Size: uint32(unsafe.Sizeof(PROCESSENTRY32{}))}
	r, _, _ := pProcess32FirstW.Call(snap, uintptr(unsafe.Pointer(&pe)))
	if r == 0 {
		return nil
	}
	out := make([]ProcessInfo, 0, 128)
	for {
		n := syscall.UTF16ToString(pe.ExeFile[:])
		if n != "" && !strings.EqualFold(n, "PowerPilot.exe") {
			path, session0 := processImagePathAndSession(pe.ProcessID)
			out = append(out, ProcessInfo{Name: n, PID: pe.ProcessID, System: isLikelySystemProcess(pe.ProcessID, n, path, session0), ImagePath: path})
		}
		r, _, _ = pProcess32NextW.Call(snap, uintptr(unsafe.Pointer(&pe)))
		if r == 0 {
			break
		}
	}
	return out
}

func sampleProcessMetrics(now time.Time, gpuByPID map[uint32]float64) []ProcessMetric {
	infos := processInstancesForMetrics()
	out := make([]ProcessMetric, 0, len(infos))
	seen := map[uint32]bool{}
	cpuCount := runtime.NumCPU()
	if cpuCount < 1 {
		cpuCount = 1
	}

	procMetricRuntime.Lock()
	defer procMetricRuntime.Unlock()
	for _, info := range infos {
		seen[info.PID] = true
		h, _, _ := pOpenProcess.Call(processQueryInformationRM|processVMReadRM, 0, uintptr(info.PID))
		if h == 0 {
			h, _, _ = pOpenProcess.Call(PROCESS_QUERY_LIMITED_INFORMATION, 0, uintptr(info.PID))
		}
		if h == 0 {
			gpu := -1.0
			if v, ok := gpuByPID[info.PID]; ok {
				gpu = v
			}
			out = append(out, ProcessMetric{Name: info.Name, PID: info.PID, System: info.System, CPUPercent: -1, GPUPercent: gpu, RAMMB: -1, ReadKBps: -1, WriteKBps: -1, OtherKBps: -1, ImagePath: info.ImagePath, Updated: now})
			continue
		}
		var create, exit, kernel, user FILETIME
		pGetProcessTimesRM.Call(h, uintptr(unsafe.Pointer(&create)), uintptr(unsafe.Pointer(&exit)), uintptr(unsafe.Pointer(&kernel)), uintptr(unsafe.Pointer(&user)))
		proc100 := filetime64(kernel) + filetime64(user)
		var pm processMemoryCounters
		pm.Cb = uint32(unsafe.Sizeof(pm))
		pGetProcessMemoryInfo.Call(h, uintptr(unsafe.Pointer(&pm)), uintptr(pm.Cb))
		var io ioCountersRM
		pGetProcessIoCountersRM.Call(h, uintptr(unsafe.Pointer(&io)))
		pCloseHandle.Call(h)

		cpu, readKB, writeKB, otherKB := 0.0, 0.0, 0.0, 0.0
		if prev, ok := procMetricRuntime.Prev[info.PID]; ok {
			sec := now.Sub(prev.At).Seconds()
			if sec > 0 {
				d := proc100 - prev.Proc100ns
				cpu = float64(d) / (sec * 1e7 * float64(cpuCount)) * 100
				if cpu < 0 {
					cpu = 0
				}
				if cpu > 100 {
					cpu = 100
				}
				if io.ReadTransferCount >= prev.ReadBytes {
					readKB = float64(io.ReadTransferCount-prev.ReadBytes) / 1024 / sec
				}
				if io.WriteTransferCount >= prev.WriteBytes {
					writeKB = float64(io.WriteTransferCount-prev.WriteBytes) / 1024 / sec
				}
				if io.OtherTransferCount >= prev.OtherBytes {
					otherKB = float64(io.OtherTransferCount-prev.OtherBytes) / 1024 / sec
				}
			}
		}
		procMetricRuntime.Prev[info.PID] = processPrevSample{Proc100ns: proc100, At: now, ReadBytes: io.ReadTransferCount, WriteBytes: io.WriteTransferCount, OtherBytes: io.OtherTransferCount}
		ramMB := float64(pm.WorkingSetSize) / (1024 * 1024)
		gpu := gpuByPID[info.PID]
		out = append(out, ProcessMetric{Name: info.Name, PID: info.PID, System: info.System, CPUPercent: cpu, GPUPercent: gpu, RAMMB: ramMB, ReadKBps: readKB, WriteKBps: writeKB, OtherKBps: otherKB, ImagePath: info.ImagePath, Updated: now})
	}
	for pid := range procMetricRuntime.Prev {
		if !seen[pid] {
			delete(procMetricRuntime.Prev, pid)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		ai := out[i].CPUPercent + out[i].GPUPercent + out[i].RAMMB/512
		aj := out[j].CPUPercent + out[j].GPUPercent + out[j].RAMMB/512
		if ai == aj {
			return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
		}
		return ai > aj
	})
	return out
}

func processMetricByName(name string) (ProcessMetric, bool) {
	name = strings.TrimSpace(name)
	if name == "" {
		return ProcessMetric{}, false
	}
	list := processMetricSnapshot()
	sum := ProcessMetric{Name: name, CPUPercent: -1, GPUPercent: -1, RAMMB: -1, ReadKBps: -1, WriteKBps: -1, OtherKBps: -1}
	found := false
	for _, p := range list {
		if !strings.EqualFold(p.Name, name) {
			continue
		}
		found = true
		if p.CPUPercent >= 0 {
			if sum.CPUPercent < 0 {
				sum.CPUPercent = 0
			}
			sum.CPUPercent += p.CPUPercent
		}
		if p.GPUPercent >= 0 {
			if sum.GPUPercent < 0 {
				sum.GPUPercent = 0
			}
			sum.GPUPercent += p.GPUPercent
		}
		if p.RAMMB >= 0 {
			if sum.RAMMB < 0 {
				sum.RAMMB = 0
			}
			sum.RAMMB += p.RAMMB
		}
		if p.ReadKBps >= 0 {
			if sum.ReadKBps < 0 {
				sum.ReadKBps = 0
			}
			sum.ReadKBps += p.ReadKBps
		}
		if p.WriteKBps >= 0 {
			if sum.WriteKBps < 0 {
				sum.WriteKBps = 0
			}
			sum.WriteKBps += p.WriteKBps
		}
		if p.OtherKBps >= 0 {
			if sum.OtherKBps < 0 {
				sum.OtherKBps = 0
			}
			sum.OtherKBps += p.OtherKBps
		}
	}
	if sum.CPUPercent > 100 {
		sum.CPUPercent = 100
	}
	if sum.GPUPercent > 100 {
		sum.GPUPercent = 100
	}
	return sum, found
}
