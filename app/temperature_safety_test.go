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
		"Thread.Sleep(1400)",
		"File.Delete(path)",
	} {
		if strings.Contains(src, forbidden) {
			t.Fatalf("collector contains unsafe pattern %q", forbidden)
		}
	}
	for _, required := range []string{
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
