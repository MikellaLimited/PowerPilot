//go:build windows

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

const (
	gpuSensorProbeInterval = 10 * time.Second
	gpuSensorProbeTimeout  = 3500 * time.Millisecond
	gpuSensorFailureLimit  = 3
)

type gpuSensorChildResult struct {
	Sensors []TemperatureSensor `json:"sensors"`
	AMD     bool                `json:"amd"`
	NVIDIA  bool                `json:"nvidia"`
}

var gpuSensorProbeState struct {
	sync.Mutex
	Sensors  []TemperatureSensor
	Updated  time.Time
	Failures int
	LastErr  string
}

func gpuSensorQuarantineFile() string {
	return filepath.Join(settingsDir(), "gpu_sensor_quarantine.flag")
}

func gpuSensorProbeQuarantined() bool {
	_, err := os.Stat(gpuSensorQuarantineFile())
	return err == nil
}

func gpuSensorsMayRun(enabled, quarantined bool) bool {
	return enabled && !quarantined
}

func gpuSensorSettingsDetail() string {
	if gpuSensorProbeQuarantined() {
		return "Отключены защитой после ошибок; включите вручную для новой проверки."
	}
	if !app.settings.GPUSensorsEnabled {
		return "Выключены · загрузка и VRAM работают через Windows."
	}
	gpuSensorProbeState.Lock()
	lastErr := gpuSensorProbeState.LastErr
	gpuSensorProbeState.Unlock()
	if lastErr != "" {
		return "Ожидание следующей проверки · " + lastErr
	}
	return "AMD ADL / NVIDIA · изолированный опрос раз в 10 секунд."
}

func setGPUSensorsEnabled(enabled bool) {
	if enabled {
		_ = os.Remove(gpuSensorQuarantineFile())
	}
	gpuSensorProbeState.Lock()
	gpuSensorProbeState.Sensors = nil
	gpuSensorProbeState.Updated = time.Time{}
	gpuSensorProbeState.Failures = 0
	gpuSensorProbeState.LastErr = ""
	gpuSensorProbeState.Unlock()
	app.settings.GPUSensorsEnabled = enabled
	saveSettings()
	techLog040(fmt.Sprintf("isolated GPU sensors enabled=%v", enabled))
}

func shouldQuarantineGPUSensorProbe(failures int) bool {
	return failures >= gpuSensorFailureLimit
}

func recordGPUSensorProbeFailure(err error) {
	if err == nil {
		return
	}
	gpuSensorProbeState.Lock()
	gpuSensorProbeState.Failures++
	failures := gpuSensorProbeState.Failures
	gpuSensorProbeState.LastErr = err.Error()
	gpuSensorProbeState.Updated = time.Now()
	gpuSensorProbeState.Unlock()
	techLog040(fmt.Sprintf("isolated GPU sensor probe failed (%d/%d): %v", failures, gpuSensorFailureLimit, err))
	if !shouldQuarantineGPUSensorProbe(failures) {
		return
	}
	_ = os.MkdirAll(settingsDir(), 0755)
	_ = os.WriteFile(gpuSensorQuarantineFile(), []byte(time.Now().Format(time.RFC3339)+"\n"+err.Error()+"\n"), 0644)
	app.settings.GPUSensorsEnabled = false
	saveSettings()
	techLog040("isolated GPU sensors quarantined")
	if app.hwnd != 0 {
		pPostMessageW.Call(app.hwnd, WM_RESOURCE_UPDATED, 0, 0)
	}
}

func sampleIsolatedVendorGPUTemperatures() []TemperatureSensor {
	gpuSensorProbeState.Lock()
	if !gpuSensorProbeState.Updated.IsZero() && time.Since(gpuSensorProbeState.Updated) < gpuSensorProbeInterval {
		out := append([]TemperatureSensor(nil), gpuSensorProbeState.Sensors...)
		gpuSensorProbeState.Unlock()
		return out
	}
	gpuSensorProbeState.Unlock()

	exe, err := os.Executable()
	if err != nil {
		recordGPUSensorProbeFailure(err)
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), gpuSensorProbeTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, "--gpu-sensor-probe")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	raw, err := cmd.Output()
	if ctx.Err() == context.DeadlineExceeded {
		err = errors.New("тайм-аут изолированного GPU-провайдера")
	}
	if err != nil {
		recordGPUSensorProbeFailure(err)
		return nil
	}
	var result gpuSensorChildResult
	if err := json.Unmarshal(raw, &result); err != nil {
		recordGPUSensorProbeFailure(fmt.Errorf("некорректный ответ GPU-провайдера: %w", err))
		return nil
	}
	result.Sensors = normalizeTemperatureSensors(result.Sensors)
	gpuSensorProbeState.Lock()
	gpuSensorProbeState.Sensors = append([]TemperatureSensor(nil), result.Sensors...)
	gpuSensorProbeState.Updated = time.Now()
	gpuSensorProbeState.Failures = 0
	gpuSensorProbeState.LastErr = ""
	gpuSensorProbeState.Unlock()
	return result.Sensors
}

// handleGPUSensorProbeCommand runs before the single-instance/UI initialization.
// Any vendor DLL crash therefore terminates only this disposable child process.
func handleGPUSensorProbeCommand() bool {
	if len(os.Args) < 2 || os.Args[1] != "--gpu-sensor-probe" {
		return false
	}
	result := gpuSensorChildResult{}
	amd, amdPresent := sampleAMDADLTemperatureSensors()
	result.AMD = amdPresent
	result.Sensors = append(result.Sensors, amd...)

	nv, nvPresent := sampleNVMLTemperatureSensors(), false
	if len(nv) > 0 {
		nvPresent = true
	} else {
		nv = sampleNVAPITemperatureSensors()
		if len(nv) > 0 {
			nvPresent = true
		} else {
			nv = sampleNvidiaSMITemperatureSensors()
			nvPresent = len(nv) > 0
		}
	}
	result.NVIDIA = nvPresent
	result.Sensors = append(result.Sensors, nv...)
	result.Sensors = normalizeTemperatureSensors(result.Sensors)
	_ = json.NewEncoder(os.Stdout).Encode(result)
	return true
}

type adlTemperature struct {
	Size        int32
	Temperature int32
}

type adlSingleSensorData struct {
	Supported int32
	Value     int32
}

type adlPMLogDataOutput struct {
	Size    int32
	Sensors [256]adlSingleSensorData
}

func normalizeADLTemperature(raw int32) (float64, bool) {
	v := float64(raw)
	if v > 1000 || v < -1000 {
		v /= 1000
	}
	return v, v > 1 && v <= 150
}

// sampleAMDADLTemperatureSensors uses AMD's driver-supplied ADL interface only.
// It never loads LibreHardwareMonitor and never calls NVIDIA APIs.
func sampleAMDADLTemperatureSensors() ([]TemperatureSensor, bool) {
	root := os.Getenv("SystemRoot")
	if root == "" {
		return nil, false
	}
	dll, err := syscall.LoadDLL(filepath.Join(root, "System32", "atiadlxx.dll"))
	if err != nil {
		return nil, false
	}
	defer dll.Release()
	create, err := dll.FindProc("ADL2_Main_Control_Create")
	if err != nil {
		return nil, true
	}
	destroy, err := dll.FindProc("ADL2_Main_Control_Destroy")
	if err != nil {
		return nil, true
	}
	adapterCount, err := dll.FindProc("ADL2_Adapter_NumberOfAdapters_Get")
	if err != nil {
		return nil, true
	}
	odNTemp, _ := dll.FindProc("ADL2_OverdriveN_Temperature_Get")
	od5Temp, _ := dll.FindProc("ADL2_Overdrive5_Temperature_Get")
	pmLog, _ := dll.FindProc("ADL2_New_QueryPMLogData_Get")
	active, _ := dll.FindProc("ADL2_Adapter_Active_Get")
	adapterID, _ := dll.FindProc("ADL2_Adapter_ID_Get")
	if pmLog == nil && odNTemp == nil && od5Temp == nil {
		return nil, true
	}

	globalAlloc := syscall.NewLazyDLL("kernel32.dll").NewProc("GlobalAlloc")
	allocCallback := syscall.NewCallback(func(size uintptr) uintptr {
		p, _, _ := globalAlloc.Call(0, size)
		return p
	})
	var adlContext uintptr
	if r, _, _ := create.Call(allocCallback, 1, uintptr(unsafe.Pointer(&adlContext))); int32(r) != 0 || adlContext == 0 {
		return nil, true
	}
	defer destroy.Call(adlContext)

	var count int32
	if r, _, _ := adapterCount.Call(adlContext, uintptr(unsafe.Pointer(&count))); int32(r) != 0 || count <= 0 {
		return nil, true
	}
	if count > 40 {
		count = 40
	}
	types := []struct {
		ID   int32
		Name string
	}{{1, "GPU Core"}, {7, "GPU Hotspot"}, {2, "GPU Memory"}}
	out := []TemperatureSensor{}
	seenAdapterIDs := map[int32]bool{}
	gpuOrdinal := 0
	for i := int32(0); i < count; i++ {
		if active != nil {
			var isActive int32
			if r, _, _ := active.Call(adlContext, uintptr(i), uintptr(unsafe.Pointer(&isActive))); int32(r) == 0 && isActive == 0 {
				continue // Do not wake a sleeping/disconnected adapter.
			}
		}
		if adapterID != nil {
			var id int32
			if r, _, _ := adapterID.Call(adlContext, uintptr(i), uintptr(unsafe.Pointer(&id))); int32(r) == 0 {
				if seenAdapterIDs[id] {
					continue
				}
				seenAdapterIDs[id] = true
			}
		}
		adapterSensors := []TemperatureSensor{}
		adapterFound := false
		if pmLog != nil {
			data := adlPMLogDataOutput{Size: int32(unsafe.Sizeof(adlPMLogDataOutput{}))}
			if r, _, _ := pmLog.Call(adlContext, uintptr(i), uintptr(unsafe.Pointer(&data))); int32(r) == 0 {
				// Modern RDNA drivers expose temperatures through the read-only PMLog
				// table. Values are already Celsius (unlike legacy ODN millidegrees).
				pmSensors := []struct {
					Primary  int
					Fallback int
					Name     string
				}{{8, 28, "GPU Core"}, {27, -1, "GPU Hotspot"}, {9, -1, "GPU Memory"}}
				for _, kind := range pmSensors {
					idx := kind.Primary
					if data.Sensors[idx].Supported == 0 && kind.Fallback >= 0 {
						idx = kind.Fallback
					}
					entry := data.Sensors[idx]
					value := float64(entry.Value)
					if entry.Supported != 0 && value > 1 && value <= 150 {
						adapterSensors = append(adapterSensors, TemperatureSensor{Name: kind.Name, ValueC: value, Source: "AMD ADL PMLog (isolated)", HardwareType: "GpuAmd"})
						adapterFound = true
					}
				}
			}
		}
		if !adapterFound && odNTemp != nil {
			for _, kind := range types {
				var raw int32
				if r, _, _ := odNTemp.Call(adlContext, uintptr(i), uintptr(kind.ID), uintptr(unsafe.Pointer(&raw))); int32(r) != 0 {
					continue
				}
				if value, ok := normalizeADLTemperature(raw); ok {
					adapterSensors = append(adapterSensors, TemperatureSensor{Name: kind.Name, ValueC: value, Source: "AMD ADL (isolated)", HardwareType: "GpuAmd"})
					adapterFound = true
				}
			}
		}
		if !adapterFound && od5Temp != nil {
			legacy := adlTemperature{Size: int32(unsafe.Sizeof(adlTemperature{}))}
			if r, _, _ := od5Temp.Call(adlContext, uintptr(i), 0, uintptr(unsafe.Pointer(&legacy))); int32(r) == 0 {
				if value, ok := normalizeADLTemperature(legacy.Temperature); ok {
					adapterSensors = append(adapterSensors, TemperatureSensor{Name: "GPU Core", ValueC: value, Source: "AMD ADL (isolated)", HardwareType: "GpuAmd"})
				}
			}
		}
		if len(adapterSensors) > 0 {
			gpuOrdinal++
			hardware := fmt.Sprintf("AMD GPU %d", gpuOrdinal)
			for j := range adapterSensors {
				adapterSensors[j].Hardware = hardware
			}
			out = append(out, adapterSensors...)
		}
	}
	return out, true
}

func isVendorGPUSource(source, vendor string) bool {
	return strings.Contains(strings.ToLower(source), strings.ToLower(vendor))
}
