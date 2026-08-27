//go:build windows

package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"unsafe"
)

func TestHardwareSensorsAreExplicitOptIn(t *testing.T) {
	var migrated Settings
	if err := json.Unmarshal([]byte(`{"temperature_auto_update":true}`), &migrated); err != nil {
		t.Fatal(err)
	}
	if migrated.HardwareSensorsEnabled {
		t.Fatal("settings without hardware_sensors_enabled must remain disabled")
	}
	var optedIn Settings
	if err := json.Unmarshal([]byte(`{"hardware_sensors_enabled":true}`), &optedIn); err != nil {
		t.Fatal(err)
	}
	if !optedIn.HardwareSensorsEnabled {
		t.Fatal("explicit opt-in was not preserved")
	}
	if migrated.GPUSensorsEnabled || optedIn.GPUSensorsEnabled {
		t.Fatal("GPU vendor sensors must remain a separate explicit opt-in")
	}
	var gpuOptedIn Settings
	if err := json.Unmarshal([]byte(`{"gpu_sensors_enabled":true}`), &gpuOptedIn); err != nil {
		t.Fatal(err)
	}
	if !gpuOptedIn.GPUSensorsEnabled {
		t.Fatal("explicit GPU sensor opt-in was not preserved")
	}
}

func TestHardwareSensorsMayRunRequiresEverySafetyGate(t *testing.T) {
	if !hardwareSensorsMayRun(true, true, false) {
		t.Fatal("enabled healthy provider should be allowed")
	}
	for _, tc := range []struct {
		enabled, provider, quarantined bool
	}{
		{false, true, false},
		{true, false, false},
		{true, true, true},
	} {
		if hardwareSensorsMayRun(tc.enabled, tc.provider, tc.quarantined) {
			t.Fatalf("unsafe state was allowed: %+v", tc)
		}
	}
}

func TestCollectorSourceUsesSafeLifecycle(t *testing.T) {
	src := temperatureCollectorCSharp()
	if got := strings.Count(src, "new Computer()"); got != 1 {
		t.Fatalf("collector must create exactly one LHM Computer, got %d", got)
	}
	for _, forbidden := range []string{
		"IsGpuEnabled = true",
		"IsMotherboardEnabled = true",
		"IsControllerEnabled = true",
		"IsPsuEnabled = true",
		"IsPowerMonitorEnabled = true",
		"Thread.Sleep(1400)",
		"File.Delete(path)",
	} {
		if strings.Contains(src, forbidden) {
			t.Fatalf("collector contains unsafe pattern %q", forbidden)
		}
	}
	for _, required := range []string{
		`OpenProfile("Minimal"`,
		"c.IsCpuEnabled = true",
		"c.IsStorageEnabled = true",
		"p.Computer.Close()",
		"File.Replace(tmp, path, null, true)",
		"SnapshotPublished",
		"WaitOne(5000)",
		"collector_quarantine.flag",
	} {
		if !strings.Contains(src, required) {
			t.Fatalf("collector is missing safety mechanism %q", required)
		}
	}
}

func TestGPUSensorSafetyGatesAndTemperatureNormalization(t *testing.T) {
	if !gpuSensorsMayRun(true, false) || gpuSensorsMayRun(false, false) || gpuSensorsMayRun(true, true) {
		t.Fatal("GPU sensor opt-in/quarantine gates are incorrect")
	}
	if !shouldQuarantineGPUSensorProbe(gpuSensorFailureLimit) || shouldQuarantineGPUSensorProbe(gpuSensorFailureLimit-1) {
		t.Fatal("GPU probe circuit breaker threshold is incorrect")
	}
	if got, ok := normalizeADLTemperature(65000); !ok || got != 65 {
		t.Fatalf("ADL millidegrees were not normalized: %.2f, %v", got, ok)
	}
	if _, ok := normalizeADLTemperature(0); ok {
		t.Fatal("invalid ADL temperature was accepted")
	}
	if !isVendorGPUSource("AMD ADL (isolated)", "AMD") || isVendorGPUSource("AMD ADL (isolated)", "NVIDIA") {
		t.Fatal("GPU vendor separation is incorrect")
	}
	if got := unsafe.Sizeof(adlPMLogDataOutput{}); got != 2052 {
		t.Fatalf("AMD ADL PMLog ABI changed: got %d bytes", got)
	}
}

func TestCollectorSourceCompiles(t *testing.T) {
	csc := findFrameworkCSC()
	if csc == "" {
		t.Skip(".NET Framework compiler is unavailable")
	}
	dir := t.TempDir()
	zr, err := zip.NewReader(bytes.NewReader(bundledTemperatureProviderZip), int64(len(bundledTemperatureProviderZip)))
	if err != nil {
		t.Fatal(err)
	}
	dll := filepath.Join(dir, "LibreHardwareMonitorLib.dll")
	found := false
	for _, item := range zr.File {
		if !strings.EqualFold(filepath.Base(filepath.FromSlash(item.Name)), "LibreHardwareMonitorLib.dll") {
			continue
		}
		r, openErr := item.Open()
		if openErr != nil {
			t.Fatal(openErr)
		}
		w, createErr := os.Create(dll)
		if createErr != nil {
			r.Close()
			t.Fatal(createErr)
		}
		_, copyErr := io.Copy(w, r)
		closeErr := w.Close()
		r.Close()
		if copyErr != nil {
			t.Fatal(copyErr)
		}
		if closeErr != nil {
			t.Fatal(closeErr)
		}
		found = true
		break
	}
	if !found {
		t.Fatal("LibreHardwareMonitorLib.dll is missing from the embedded provider")
	}
	src := filepath.Join(dir, "PowerPilotSensors.cs")
	if err := os.WriteFile(src, []byte(temperatureCollectorCSharp()), 0644); err != nil {
		t.Fatal(err)
	}
	outPath := filepath.Join(dir, "PowerPilotSensors.exe")
	cmd := exec.Command(csc, "/nologo", "/target:winexe", "/platform:x64", "/out:"+outPath, "/reference:"+dll, src)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("collector source does not compile: %v\n%s", err, out)
	}
}
