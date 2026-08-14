//go:build windows

package main

import (
	"strings"
	"syscall"
	"unsafe"
)

var (
	v040Ole32               = syscall.NewLazyDLL("ole32.dll")
	pCoInitializeEx040      = v040Ole32.NewProc("CoInitializeEx")
	pCoCreateInstance040    = v040Ole32.NewProc("CoCreateInstance")
	pGetForegroundWindow040 = user32.NewProc("GetForegroundWindow")
)

const (
	coinitMultithreaded040 = 0x0
	clsctxAll040           = 23
	wmSyscommand040        = 0x0112
	scMonitorPower040      = 0xF170
	hwndBroadcast040       = 0xffff
)

func windowText040(hwnd uintptr) string {
	n, _, _ := pGetWindowTextLen.Call(hwnd)
	if n == 0 {
		return ""
	}
	buf := make([]uint16, int(n)+1)
	pGetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return syscall.UTF16ToString(buf)
}

func processNameByPID040(pid uint32) string {
	for _, p := range listProcessInfos() {
		if p.PID == pid {
			return p.Name
		}
	}
	return ""
}

func windowMatch040(query string, activeOnly, titleOnly bool) bool {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return false
	}
	active := uintptr(0)
	if activeOnly {
		active, _, _ = pGetForegroundWindow040.Call()
	}
	found := false
	cb := syscall.NewCallback(func(hwnd uintptr, _ uintptr) uintptr {
		if found {
			return 0
		}
		if activeOnly && hwnd != active {
			return 1
		}
		vis, _, _ := pIsWindowVisible.Call(hwnd)
		if vis == 0 {
			return 1
		}
		title := strings.ToLower(windowText040(hwnd))
		if strings.Contains(title, q) {
			found = true
			return 0
		}
		if titleOnly {
			return 1
		}
		var pid uint32
		pGetWindowThreadProcessId.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
		if strings.Contains(strings.ToLower(processNameByPID040(pid)), q) {
			found = true
			return 0
		}
		return 1
	})
	pEnumWindows.Call(cb, 0)
	return found
}

func monitorPower040(on bool) {
	// SC_MONITORPOWER: 2 powers off. -1 wakes on the Windows desktop; also nudge
	// the input queue for hardware/drivers that only wake on user input.
	value := uintptr(2)
	if on {
		value = ^uintptr(0)
		pPostMessageW.Call(hwndBroadcast040, wmSyscommand040, scMonitorPower040, value)
	} else {
		pPostMessageW.Call(hwndBroadcast040, wmSyscommand040, scMonitorPower040, value)
	}
}

// Core Audio helpers use the default render endpoint. No external DLL/runtime is required.
var (
	clsidMMDeviceEnumerator040   = guid{0xBCDE0395, 0xE52F, 0x467C, [8]byte{0x8E, 0x3D, 0xC4, 0x57, 0x92, 0x91, 0x69, 0x2E}}
	iidIMMDeviceEnumerator040    = guid{0xA95664D2, 0x9614, 0x4F35, [8]byte{0xA7, 0x46, 0xDE, 0x8D, 0xB6, 0x36, 0x17, 0xE6}}
	iidIAudioMeterInformation040 = guid{0xC02216F6, 0x8C67, 0x4B5B, [8]byte{0x9D, 0x00, 0xD0, 0x08, 0xE7, 0x3E, 0x00, 0x64}}
	iidIAudioEndpointVolume040   = guid{0x5CDF2C82, 0x841E, 0x4546, [8]byte{0x97, 0x22, 0x0C, 0xF7, 0x40, 0x78, 0x22, 0x9A}}
)

func defaultAudioDevice040() uintptr {
	pCoInitializeEx040.Call(0, coinitMultithreaded040)
	var en uintptr
	hr, _, _ := pCoCreateInstance040.Call(uintptr(unsafe.Pointer(&clsidMMDeviceEnumerator040)), 0, clsctxAll040, uintptr(unsafe.Pointer(&iidIMMDeviceEnumerator040)), uintptr(unsafe.Pointer(&en)))
	if int32(hr) < 0 || en == 0 {
		return 0
	}
	defer comRelease(en)
	var dev uintptr
	// IMMDeviceEnumerator::GetDefaultAudioEndpoint(eRender=0,eMultimedia=1)
	hr = d2dCall(en, 4, 0, 1, uintptr(unsafe.Pointer(&dev)))
	if int32(hr) < 0 {
		return 0
	}
	return dev
}

func audioPeak040() float32 {
	dev := defaultAudioDevice040()
	if dev == 0 {
		return -1
	}
	defer comRelease(dev)
	var meter uintptr
	hr := d2dCall(dev, 3, uintptr(unsafe.Pointer(&iidIAudioMeterInformation040)), clsctxAll040, 0, uintptr(unsafe.Pointer(&meter)))
	if int32(hr) < 0 || meter == 0 {
		return -1
	}
	defer comRelease(meter)
	var peak float32
	hr = d2dCall(meter, 3, uintptr(unsafe.Pointer(&peak)))
	if int32(hr) < 0 {
		return -1
	}
	return peak
}

func endpointVolume040() uintptr {
	dev := defaultAudioDevice040()
	if dev == 0 {
		return 0
	}
	defer comRelease(dev)
	var ep uintptr
	hr := d2dCall(dev, 3, uintptr(unsafe.Pointer(&iidIAudioEndpointVolume040)), clsctxAll040, 0, uintptr(unsafe.Pointer(&ep)))
	if int32(hr) < 0 {
		return 0
	}
	return ep
}
func setMasterVolume040(v float32) bool {
	if v < 0 {
		v = 0
	}
	if v > 1 {
		v = 1
	}
	ep := endpointVolume040()
	if ep == 0 {
		return false
	}
	defer comRelease(ep)
	hr := d2dCall(ep, 7, uintptr(*(*uint32)(unsafe.Pointer(&v))), 0)
	return int32(hr) >= 0
}
func setMasterMute040(m bool) bool {
	ep := endpointVolume040()
	if ep == 0 {
		return false
	}
	defer comRelease(ep)
	v := uintptr(0)
	if m {
		v = 1
	}
	hr := d2dCall(ep, 14, v, 0)
	return int32(hr) >= 0
}

// Per-application audio silence uses the Windows Audio Session API. When a process
// name is provided, every active audio session owned by a matching executable is
// sampled and the loudest peak is returned. If no matching session exists, zero
// is returned with found=false so callers can distinguish "silent" from "not running".
var (
	iidIAudioSessionManager2040 = guid{0x77AA99A0, 0x1BD6, 0x484F, [8]byte{0x8B, 0xC7, 0x2C, 0x65, 0x4C, 0x9A, 0x9B, 0x6F}}
	iidIAudioSessionControl2040 = guid{0xBFB7FF88, 0x7239, 0x4FC9, [8]byte{0x8F, 0xA2, 0x07, 0xC9, 0x50, 0xBE, 0x9C, 0x6D}}
)

func queryInterface040(obj uintptr, iid *guid) uintptr {
	if obj == 0 {
		return 0
	}
	var out uintptr
	hr := d2dCall(obj, 0, uintptr(unsafe.Pointer(iid)), uintptr(unsafe.Pointer(&out)))
	if int32(hr) < 0 {
		return 0
	}
	return out
}

func processPIDsByName040(name string) map[uint32]bool {
	q := strings.ToLower(strings.TrimSpace(name))
	q = strings.TrimSuffix(q, ".exe")
	out := map[uint32]bool{}
	if q == "" {
		return out
	}
	snap, _, _ := pCreateToolhelp32Snapshot.Call(TH32CS_SNAPPROCESS, 0)
	if snap == 0 || snap == uintptr(^uint32(0)) {
		return out
	}
	defer pCloseHandle.Call(snap)
	pe := PROCESSENTRY32{Size: uint32(unsafe.Sizeof(PROCESSENTRY32{}))}
	r, _, _ := pProcess32FirstW.Call(snap, uintptr(unsafe.Pointer(&pe)))
	for r != 0 {
		n := strings.ToLower(syscall.UTF16ToString(pe.ExeFile[:]))
		base := strings.TrimSuffix(n, ".exe")
		if n == strings.ToLower(name) || base == q {
			out[pe.ProcessID] = true
		}
		r, _, _ = pProcess32NextW.Call(snap, uintptr(unsafe.Pointer(&pe)))
	}
	return out
}

func audioPeakForProcess040(name string) (peak float32, found bool) {
	pids := processPIDsByName040(name)
	if len(pids) == 0 {
		return 0, false
	}
	dev := defaultAudioDevice040()
	if dev == 0 {
		return -1, false
	}
	defer comRelease(dev)
	var mgr uintptr
	hr := d2dCall(dev, 3, uintptr(unsafe.Pointer(&iidIAudioSessionManager2040)), clsctxAll040, 0, uintptr(unsafe.Pointer(&mgr)))
	if int32(hr) < 0 || mgr == 0 {
		return -1, false
	}
	defer comRelease(mgr)
	var enum uintptr
	hr = d2dCall(mgr, 5, uintptr(unsafe.Pointer(&enum))) // IAudioSessionManager2::GetSessionEnumerator
	if int32(hr) < 0 || enum == 0 {
		return -1, false
	}
	defer comRelease(enum)
	var count int32
	hr = d2dCall(enum, 3, uintptr(unsafe.Pointer(&count)))
	if int32(hr) < 0 {
		return -1, false
	}
	maxPeak := float32(0)
	for i := int32(0); i < count; i++ {
		var ctl uintptr
		hr = d2dCall(enum, 4, uintptr(i), uintptr(unsafe.Pointer(&ctl)))
		if int32(hr) < 0 || ctl == 0 {
			continue
		}
		ctl2 := queryInterface040(ctl, &iidIAudioSessionControl2040)
		if ctl2 == 0 {
			comRelease(ctl)
			continue
		}
		var pid uint32
		hr2 := d2dCall(ctl2, 14, uintptr(unsafe.Pointer(&pid)))
		if int32(hr2) >= 0 && pids[pid] {
			meter := queryInterface040(ctl, &iidIAudioMeterInformation040)
			if meter == 0 {
				meter = queryInterface040(ctl2, &iidIAudioMeterInformation040)
			}
			if meter != 0 {
				var v float32
				if h := d2dCall(meter, 3, uintptr(unsafe.Pointer(&v))); int32(h) >= 0 {
					found = true
					if v > maxPeak {
						maxPeak = v
					}
				}
				comRelease(meter)
			}
		}
		comRelease(ctl2)
		comRelease(ctl)
	}
	return maxPeak, found
}
