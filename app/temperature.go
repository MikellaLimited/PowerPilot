//go:build windows

package main

import (
	"archive/zip"
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

type TemperatureSensor struct {
	Hardware     string  `json:"Hardware"`
	Name         string  `json:"Name"`
	ValueC       float64 `json:"Value"`
	Source       string  `json:"Source"`
	HardwareType string  `json:"HardwareType,omitempty"`
	Identifier   string  `json:"Identifier,omitempty"`
}

// HardwareSensor is the unfiltered sensor record published by the elevated
// LibreHardwareMonitor collector. Keeping the complete sensor tree lets the
// advanced monitor expose the same families of values users expect from
// hardware tools: temperatures, voltages, fans, power, clocks, load and more.
type HardwareSensor struct {
	Hardware     string  `json:"Hardware"`
	Name         string  `json:"Name"`
	Value        float64 `json:"Value"`
	Min          float64 `json:"Min"`
	Max          float64 `json:"Max"`
	HasMin       bool    `json:"HasMin"`
	HasMax       bool    `json:"HasMax"`
	SensorType   string  `json:"SensorType"`
	Source       string  `json:"Source"`
	HardwareType string  `json:"HardwareType,omitempty"`
	Identifier   string  `json:"Identifier,omitempty"`
}

var hardwareSensorState struct {
	sync.RWMutex
	Sensors []HardwareSensor
	Updated time.Time
}

var temperatureState struct {
	sync.RWMutex
	Sensors []TemperatureSensor
	Updated time.Time
	Stop    chan struct{}
}

var temperatureProviderState struct {
	sync.RWMutex
	Installing      bool
	Checking        bool
	LastError       string
	LastOK          time.Time
	LatestVersion   string
	LatestAssetURL  string
	UpdateAvailable bool
}

func temperatureProviderDir() string {
	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		if d, err := os.UserConfigDir(); err == nil {
			base = d
		}
	}
	if base == "" {
		return ""
	}
	return filepath.Join(base, "PowerPilot", "HardwareProvider")
}

func temperatureProviderActivePointerFile() string {
	root := temperatureProviderDir()
	if root == "" {
		return ""
	}
	return filepath.Join(root, "active_provider_dir.txt")
}

// Provider runtime files are versioned side-by-side. Updating DLLs in-place is
// unsafe on Windows because the elevated collector keeps CLR assemblies mapped.
// The active pointer lets PowerPilot switch to a freshly prepared provider
// directory without ever overwriting a loaded System.Memory.dll (or friends).
func temperatureProviderRuntimeDir() string {
	root := temperatureProviderDir()
	if root == "" {
		return ""
	}
	pointer := temperatureProviderActivePointerFile()
	if pointer != "" {
		if b, err := os.ReadFile(pointer); err == nil {
			v := strings.TrimSpace(string(b))
			if v != "" {
				candidate := v
				if !filepath.IsAbs(candidate) {
					candidate = filepath.Join(root, candidate)
				}
				rootAbs, _ := filepath.Abs(root)
				candAbs, _ := filepath.Abs(candidate)
				rootPrefix := strings.ToLower(filepath.Clean(rootAbs)) + string(os.PathSeparator)
				if strings.HasPrefix(strings.ToLower(filepath.Clean(candAbs))+string(os.PathSeparator), rootPrefix) {
					if _, err := os.Stat(filepath.Join(candAbs, "LibreHardwareMonitorLib.dll")); err == nil {
						return candAbs
					}
				}
			}
		}
	}
	return root
}

const temperatureCollectorRevision = "9"
const temperatureBundledProviderVersion = "0.9.7-pre724+825dc3d"
const temperatureBundledProviderRevision = "1"
const pawnIOVersion = "2.2.0"
const pawnIOSHA256 = "1f519a22e47187f70a1379a48ca604981c4fcf694f4e65b734aaa74a9fba3032"
const pawnIOURL = "https://github.com/namazso/PawnIO.Setup/releases/download/2.2.0/PawnIO_setup.exe"

// PowerPilot ships a known-good x64/net472 LibreHardwareMonitor snapshot from
// the official upstream master CI. This avoids pinning temperature support to
// the older v0.9.6 stable build on newer AM5/GPU/mainboard hardware.
//
//go:embed assets/LibreHardwareMonitor_master_net472.zip
var bundledTemperatureProviderZip []byte

func temperatureProviderCollectorExe() string {
	d := temperatureProviderRuntimeDir()
	if d == "" {
		return ""
	}
	return filepath.Join(d, "PowerPilotSensors.exe")
}

func temperatureProviderCollectorSourceFile() string {
	d := temperatureProviderRuntimeDir()
	if d == "" {
		return ""
	}
	return filepath.Join(d, "PowerPilotSensors.cs")
}

func temperatureProviderRevisionFile() string {
	d := temperatureProviderRuntimeDir()
	if d == "" {
		return ""
	}
	return filepath.Join(d, "collector_revision.txt")
}

func temperatureProviderPawnMarker() string {
	d := temperatureProviderDir()
	if d == "" {
		return ""
	}
	return filepath.Join(d, "pawnio-installed.txt")
}

func temperatureProviderRebootMarker() string {
	d := temperatureProviderDir()
	if d == "" {
		return ""
	}
	return filepath.Join(d, "reboot_required.txt")
}

func temperatureProviderBundleMarkerFile() string {
	d := temperatureProviderDir()
	if d == "" {
		return ""
	}
	return filepath.Join(d, "provider_bundle_revision.txt")
}

func temperatureBundledProviderNeedsInstall() bool {
	if !temperatureProviderBasePresent() {
		return false
	}
	p := temperatureProviderBundleMarkerFile()
	if p == "" {
		return true
	}
	b, err := os.ReadFile(p)
	return err != nil || strings.TrimSpace(string(b)) != temperatureBundledProviderRevision
}

func installBundledTemperatureProviderFiles(dir string) error {
	if dir == "" {
		return fmt.Errorf("не удалось определить папку датчиков")
	}
	zr, err := zip.NewReader(bytes.NewReader(bundledTemperatureProviderZip), int64(len(bundledTemperatureProviderZip)))
	if err != nil {
		return fmt.Errorf("встроенный пакет датчиков повреждён: %w", err)
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	for _, f := range zr.File {
		name := filepath.Clean(filepath.FromSlash(f.Name))
		if name == "." || name == "" {
			continue
		}
		if filepath.IsAbs(name) || name == ".." || strings.HasPrefix(name, ".."+string(os.PathSeparator)) {
			return fmt.Errorf("некорректный путь во встроенном пакете: %s", f.Name)
		}
		target := filepath.Join(dir, name)
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return err
		}
		r, err := f.Open()
		if err != nil {
			return err
		}
		w, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
		if err != nil {
			r.Close()
			return err
		}
		_, copyErr := io.Copy(w, r)
		closeErr := w.Close()
		r.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	if err := os.WriteFile(temperatureProviderVersionFile(), []byte(temperatureBundledProviderVersion), 0644); err != nil {
		return err
	}
	if err := os.WriteFile(temperatureProviderBundleMarkerFile(), []byte(temperatureBundledProviderRevision), 0644); err != nil {
		return err
	}
	_ = os.WriteFile(filepath.Join(dir, "provider_source.txt"), []byte("LibreHardwareMonitor official master CI\ncommit=825dc3de36c5816bb2a8b10b309244a8c362a7f9\nversion=0.9.7-pre724\nworkflow_run=31707859588\n"), 0644)
	return nil
}

func temperatureProviderBasePresent() bool {
	p := temperatureProviderDLL()
	if p == "" {
		return false
	}
	_, err := os.Stat(p)
	return err == nil
}

func temperatureProviderNeedsRepair() bool {
	if !temperatureProviderBasePresent() {
		return false
	}
	if temperatureBundledProviderNeedsInstall() {
		return true
	}
	for _, p := range []string{temperatureProviderCollectorExe(), temperatureProviderPawnMarker(), temperatureProviderRevisionFile()} {
		if p == "" {
			return true
		}
		if _, err := os.Stat(p); err != nil {
			return true
		}
	}
	b, err := os.ReadFile(temperatureProviderRevisionFile())
	return err != nil || strings.TrimSpace(string(b)) != temperatureCollectorRevision
}

func temperatureProviderRebootRequired() bool {
	p := temperatureProviderRebootMarker()
	if p == "" {
		return false
	}
	st, err := os.Stat(p)
	if err != nil {
		return false
	}
	// Clear a stale reboot marker automatically once Windows has actually restarted.
	if tick, _, _ := pGetTickCount64.Call(); tick > 0 {
		boot := time.Now().Add(-time.Duration(uint64(tick)) * time.Millisecond)
		if st.ModTime().Before(boot.Add(5 * time.Second)) {
			_ = os.Remove(p)
			return false
		}
	}
	return true
}

func temperatureProviderDLL() string {
	d := temperatureProviderRuntimeDir()
	if d == "" {
		return ""
	}
	return filepath.Join(d, "LibreHardwareMonitorLib.dll")
}

func temperatureProviderVersionFile() string {
	d := temperatureProviderDir()
	if d == "" {
		return ""
	}
	return filepath.Join(d, "version.txt")
}

func temperatureProviderSnapshotFile() string {
	d := temperatureProviderDir()
	if d == "" {
		return ""
	}
	return filepath.Join(d, "sensors.json")
}

func temperatureProviderBridgeFile() string {
	d := temperatureProviderDir()
	if d == "" {
		return ""
	}
	return filepath.Join(d, "PowerPilotSensors.ps1")
}

func temperatureProviderInstalledVersion() string {
	p := temperatureProviderVersionFile()
	if p != "" {
		if b, err := os.ReadFile(p); err == nil {
			if v := strings.TrimSpace(string(b)); v != "" {
				return v
			}
		}
	}
	if temperatureProviderInstalled() {
		// Provider installs made by 0.6.6 did not write version.txt.
		return "v0.9.6"
	}
	return ""
}

func temperatureProviderUpdateAvailable() bool {
	temperatureProviderState.RLock()
	v := temperatureProviderState.UpdateAvailable
	temperatureProviderState.RUnlock()
	return v || temperatureProviderNeedsRepair()
}

func fetchLatestTemperatureProvider(ctx context.Context) (string, string, error) {
	return fetchLatestTemperatureProviderNative(ctx)
}

func checkTemperatureProviderUpdatesAsync() {
	temperatureProviderState.Lock()
	if temperatureProviderState.Checking || temperatureProviderState.Installing {
		temperatureProviderState.Unlock()
		return
	}
	temperatureProviderState.Checking = true
	temperatureProviderState.Unlock()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
		tag, url, err := fetchLatestTemperatureProvider(ctx)
		cancel()
		notifyUpdate := false
		notifyKey := ""
		temperatureProviderState.Lock()
		temperatureProviderState.Checking = false
		if err == nil {
			temperatureProviderState.LatestVersion = tag
			temperatureProviderState.LatestAssetURL = url
			installed := temperatureProviderInstalledVersion()
			if strings.EqualFold(installed, temperatureBundledProviderVersion) {
				temperatureProviderState.UpdateAvailable = false
			} else {
				temperatureProviderState.UpdateAvailable = installed != "" && tag != "" && !strings.EqualFold(installed, tag)
			}
			notifyUpdate = temperatureProviderState.UpdateAvailable
			if notifyUpdate {
				notifyKey = "sensor-update-" + strings.ToLower(tag)
			}
		}
		temperatureProviderState.Unlock()
		if notifyUpdate {
			pushAppNotificationUnique(notifyKey, notifUpdate, "Обновление аппаратных датчиков", "Доступна новая версия провайдера датчиков: "+tag, notifTargetSensors)
		}
		if app.hwnd != 0 {
			pPostMessageW.Call(app.hwnd, WM_RESOURCE_UPDATED, 0, 0)
		}
	}()
}

func temperatureProviderInstalled() bool {
	// “Installed” means the user explicitly installed the temperature provider.
	// A stale collector revision must not hide every temperature while PowerPilot repairs it.
	if !temperatureProviderBasePresent() {
		return false
	}
	_, err := os.Stat(temperatureProviderCollectorExe())
	return err == nil
}

func temperatureMonitoringEnabled() bool {
	// The user opted in once the provider payload exists. Even if the elevated collector
	// is temporarily stale/broken, keep native/Windows/vendor fallbacks visible.
	return temperatureProviderBasePresent()
}

func temperatureCollectorNeedsRepair() bool {
	if !temperatureProviderBasePresent() {
		return false
	}
	p := temperatureProviderCollectorExe()
	if p == "" {
		return true
	}
	if _, err := os.Stat(p); err != nil {
		return true
	}
	b, err := os.ReadFile(temperatureProviderRevisionFile())
	return err != nil || strings.TrimSpace(string(b)) != temperatureCollectorRevision
}

func temperatureProviderStatus() (bool, string) {
	temperatureProviderState.RLock()
	installing := temperatureProviderState.Installing
	checking := temperatureProviderState.Checking
	lastErr := temperatureProviderState.LastError
	latest := temperatureProviderState.LatestVersion
	update := temperatureProviderState.UpdateAvailable
	temperatureProviderState.RUnlock()
	if installing {
		return true, "Устанавливается…"
	}
	if lastErr != "" {
		return false, lastErr
	}
	if temperatureProviderNeedsRepair() {
		return false, "Требуется обновить компоненты датчиков (низкоуровневый доступ CPU/платы)"
	}
	if temperatureProviderInstalled() {
		v := temperatureProviderInstalledVersion()
		if temperatureProviderRebootRequired() {
			return false, fmt.Sprintf("Установлен %s · для полного доступа требуется перезагрузка Windows", v)
		}
		if update && latest != "" {
			return false, fmt.Sprintf("Установлен %s · доступен %s", v, latest)
		}
		if checking {
			return false, fmt.Sprintf("Установлен %s · проверка обновлений…", v)
		}
		return false, fmt.Sprintf("Установлен %s · PawnIO %s · расширенный доступ активен", v, pawnIOVersion)
	}
	return false, "Не установлены — аппаратные датчики скрыты"
}

// installTemperatureProviderAsync installs/updates the private hardware sensor provider.
// It is user-triggered from Settings; PowerPilot never installs a low-level driver silently.
func temperatureCollectorCSharp() string {
	return `using System;
using System.Collections.Generic;
using System.Globalization;
using System.IO;
using System.Text;
using System.Threading;
using LibreHardwareMonitor.Hardware;

internal sealed class UpdateVisitor : IVisitor {
    public void VisitComputer(IComputer computer) { computer.Traverse(this); }
    public void VisitHardware(IHardware hardware) {
        hardware.Update();
        foreach (IHardware sh in hardware.SubHardware) sh.Accept(this);
    }
    public void VisitSensor(ISensor sensor) { }
    public void VisitParameter(IParameter parameter) { }
}

internal sealed class Profile {
    public string Name;
    public Computer Computer;
    public string Error;
}

internal static class Program {
    private static readonly UpdateVisitor Visitor = new UpdateVisitor();

    private static string Esc(string value) {
        if (value == null) return "";
        return value.Replace("\\", "\\\\").Replace("\"", "\\\"").Replace("\r", "\\r").Replace("\n", "\\n").Replace("\t", "\\t");
    }

    private static Profile OpenProfile(string name, Action<Computer> configure) {
        Profile p = new Profile { Name = name };
        try {
            Computer c = new Computer();
            configure(c);
            c.Open();
            p.Computer = c;
        } catch (Exception ex) {
            p.Error = ex.GetType().Name + ": " + ex.Message;
        }
        return p;
    }

    private static List<Profile> OpenProfiles() {
        List<Profile> profiles = new List<Profile>();
        // First try the same broad profile as the official monitor. Some systems have a
        // single optional hardware group that throws during Open(); if that happens, the
        // isolated profiles below keep CPU/GPU/motherboard/storage temperatures alive.
        Profile full = OpenProfile("Full", delegate(Computer c) {
            c.IsCpuEnabled = true; c.IsGpuEnabled = true; c.IsMemoryEnabled = true;
            c.IsMotherboardEnabled = true; c.IsControllerEnabled = true;
            c.IsStorageEnabled = true; c.IsPsuEnabled = true; c.IsPowerMonitorEnabled = true;
            c.IsBatteryEnabled = true;
        });
        profiles.Add(full); // keep both success and failure visible in collector_status.json
        // Even when the broad profile opens successfully, retry the low-level groups that
        // most often expose partial/zero values on AM5 and Super-I/O hardware in isolation.
        // This mirrors a diagnostic restart of those providers and can recover Tctl/Tdie,
        // CCD/SoC and motherboard/VRM sensors that a broad first pass missed.
        profiles.Add(OpenProfile("CPU", delegate(Computer c) { c.IsCpuEnabled = true; }));
        profiles.Add(OpenProfile("Motherboard", delegate(Computer c) { c.IsMotherboardEnabled = true; }));
        profiles.Add(OpenProfile("Controllers", delegate(Computer c) { c.IsControllerEnabled = true; }));
        if (full.Computer == null) {
            profiles.Add(OpenProfile("GPU", delegate(Computer c) { c.IsCpuEnabled = true; c.IsGpuEnabled = true; }));
            profiles.Add(OpenProfile("Storage", delegate(Computer c) { c.IsStorageEnabled = true; }));
            profiles.Add(OpenProfile("Memory", delegate(Computer c) { c.IsMemoryEnabled = true; }));
            profiles.Add(OpenProfile("PSU", delegate(Computer c) { c.IsPsuEnabled = true; }));
            profiles.Add(OpenProfile("Power", delegate(Computer c) { c.IsPowerMonitorEnabled = true; }));
            profiles.Add(OpenProfile("Battery", delegate(Computer c) { c.IsBatteryEnabled = true; }));
        }
        return profiles;
    }

    private static void Collect(IHardware h, string profileName, List<string> rows) {
        try {
            foreach (ISensor s in h.Sensors) {
                if (!s.Value.HasValue) continue;
                double v = s.Value.Value;
                if (Double.IsNaN(v) || Double.IsInfinity(v)) continue;
                // LibreHardwareMonitor can expose a failed AMD temperature read as 0.
                // Keep zero for non-temperature metrics (0% load is meaningful), but
                // suppress physically implausible temperature samples.
                if (s.SensorType == SensorType.Temperature && (v <= 1 || v > 170)) continue;
                string src = "LibreHardwareMonitor · " + profileName;
                string hwType = ""; try { hwType = h.HardwareType.ToString(); } catch { }
                string ident = ""; try { ident = s.Identifier.ToString(); } catch { try { ident = h.Identifier.ToString(); } catch { } }
                string sensorType = ""; try { sensorType = s.SensorType.ToString(); } catch { }
                bool hasMin = s.Min.HasValue && !Double.IsNaN(s.Min.Value) && !Double.IsInfinity(s.Min.Value);
                bool hasMax = s.Max.HasValue && !Double.IsNaN(s.Max.Value) && !Double.IsInfinity(s.Max.Value);
                double min = hasMin ? s.Min.Value : 0.0;
                double max = hasMax ? s.Max.Value : 0.0;
                rows.Add("{\"Hardware\":\"" + Esc(h.Name) + "\",\"Name\":\"" + Esc(s.Name) + "\",\"Value\":" +
                    v.ToString("0.######", CultureInfo.InvariantCulture) + ",\"Min\":" + min.ToString("0.######", CultureInfo.InvariantCulture) +
                    ",\"Max\":" + max.ToString("0.######", CultureInfo.InvariantCulture) + ",\"HasMin\":" + (hasMin ? "true" : "false") +
                    ",\"HasMax\":" + (hasMax ? "true" : "false") + ",\"SensorType\":\"" + Esc(sensorType) + "\",\"Source\":\"" + Esc(src) +
                    "\",\"HardwareType\":\"" + Esc(hwType) + "\",\"Identifier\":\"" + Esc(ident) + "\"}");
            }
        } catch { }
        try { foreach (IHardware sh in h.SubHardware) Collect(sh, profileName, rows); } catch { }
    }

    private static void DescribeHardware(IHardware h, string profileName, List<string> states) {
        try {
            int seen = 0, valid = 0;
            foreach (ISensor s in h.Sensors) {
                if (s.SensorType != SensorType.Temperature) continue;
                seen++;
                if (s.Value.HasValue) {
                    double v = s.Value.Value;
                    if (!Double.IsNaN(v) && !Double.IsInfinity(v) && v > 1 && v <= 170) valid++;
                }
            }
            string hwType = ""; try { hwType = h.HardwareType.ToString(); } catch { }
            states.Add("{\"Profile\":\"" + Esc(profileName) + "\",\"Name\":\"" + Esc(h.Name) +
                "\",\"Type\":\"" + Esc(hwType) + "\",\"TemperatureSensors\":" + seen.ToString(CultureInfo.InvariantCulture) +
                ",\"ValidTemperatures\":" + valid.ToString(CultureInfo.InvariantCulture) + "}");
        } catch { }
        try { foreach (IHardware sh in h.SubHardware) DescribeHardware(sh, profileName, states); } catch { }
    }

    private static void AtomicWrite(string path, string value) {
        string tmp = path + ".tmp";
        File.WriteAllText(tmp, value, new UTF8Encoding(false));
        if (File.Exists(path)) File.Delete(path);
        File.Move(tmp, path);
    }

    private static void Publish(List<Profile> profiles, string output, string statusPath) {
        List<string> rows = new List<string>();
        List<string> states = new List<string>();
        List<string> hardwareStates = new List<string>();
        foreach (Profile p in profiles) {
            if (p.Computer == null) {
                states.Add("{\"Name\":\"" + Esc(p.Name) + "\",\"OK\":false,\"Error\":\"" + Esc(p.Error ?? "Open failed") + "\"}");
                continue;
            }
            try {
                p.Computer.Accept(Visitor);
                foreach (IHardware h in p.Computer.Hardware) { Collect(h, p.Name, rows); DescribeHardware(h, p.Name, hardwareStates); }
                states.Add("{\"Name\":\"" + Esc(p.Name) + "\",\"OK\":true,\"Error\":\"\"}");
            } catch (Exception ex) {
                states.Add("{\"Name\":\"" + Esc(p.Name) + "\",\"OK\":false,\"Error\":\"" + Esc(ex.GetType().Name + ": " + ex.Message) + "\"}");
            }
        }
        AtomicWrite(output, "[" + String.Join(",", rows.ToArray()) + "]");
        string status = "{\"UpdatedUtc\":\"" + DateTime.UtcNow.ToString("o", CultureInfo.InvariantCulture) +
            "\",\"SensorCount\":" + rows.Count.ToString(CultureInfo.InvariantCulture) +
            ",\"Profiles\":[" + String.Join(",", states.ToArray()) + "]" +
            ",\"Hardware\":[" + String.Join(",", hardwareStates.ToArray()) + "]}";
        AtomicWrite(statusPath, status);
    }

    public static int Main(string[] args) {
        Mutex mutex = null;
        bool owns = false;
        try {
            mutex = new Mutex(false, "Local\\PowerPilotHardwareSensors");
            try { owns = mutex.WaitOne(0, false); } catch { owns = true; }
            if (!owns) return 0;

            string dir = AppDomain.CurrentDomain.BaseDirectory;
            string outputDir = dir;
            bool once = false;
            if (args != null) {
                for (int i = 0; i < args.Length; i++) {
                    string a = args[i] ?? "";
                    if (String.Equals(a, "--once", StringComparison.OrdinalIgnoreCase)) once = true;
                    else if (a.StartsWith("--output-dir=", StringComparison.OrdinalIgnoreCase)) outputDir = a.Substring("--output-dir=".Length).Trim('"');
                    else if (String.Equals(a, "--output-dir", StringComparison.OrdinalIgnoreCase) && i + 1 < args.Length) outputDir = (args[++i] ?? "").Trim('"');
                }
            }
            if (String.IsNullOrWhiteSpace(outputDir)) outputDir = dir;
            Directory.CreateDirectory(outputDir);
            string output = Path.Combine(outputDir, "sensors.json");
            string status = Path.Combine(outputDir, "collector_status.json");
            List<Profile> profiles = OpenProfiles();
            // Give low-level providers a short warm-up before the first snapshot.
            Thread.Sleep(250);
            Publish(profiles, output, status);
            if (once) return 0;
            while (true) {
                Thread.Sleep(1400);
                Publish(profiles, output, status);
            }
        } catch (Exception ex) {
            try {
                string fatalDir = AppDomain.CurrentDomain.BaseDirectory;
                if (args != null) {
                    for (int i = 0; i < args.Length; i++) {
                        string a = args[i] ?? "";
                        if (a.StartsWith("--output-dir=", StringComparison.OrdinalIgnoreCase)) fatalDir = a.Substring("--output-dir=".Length).Trim('"');
                        else if (String.Equals(a, "--output-dir", StringComparison.OrdinalIgnoreCase) && i + 1 < args.Length) fatalDir = (args[++i] ?? "").Trim('"');
                    }
                }
                string status = Path.Combine(fatalDir, "collector_status.json");
                AtomicWrite(status, "{\"UpdatedUtc\":\"" + DateTime.UtcNow.ToString("o", CultureInfo.InvariantCulture) +
                    "\",\"SensorCount\":0,\"Fatal\":\"" + Esc(ex.GetType().Name + ": " + ex.Message) + "\",\"Profiles\":[]}");
            } catch { }
            return 4;
        } finally {
            if (owns && mutex != null) try { mutex.ReleaseMutex(); } catch { }
            if (mutex != null) mutex.Close();
        }
    }
}`
}

func installTemperatureProviderAsync() {
	temperatureProviderState.Lock()
	if temperatureProviderState.Installing {
		temperatureProviderState.Unlock()
		return
	}
	temperatureProviderState.Installing = true
	temperatureProviderState.LastError = ""
	temperatureProviderState.Unlock()
	if app.hwnd != 0 {
		pPostMessageW.Call(app.hwnd, WM_RESOURCE_UPDATED, 0, 0)
	}
	go func() {
		stopTemperatureSampler()
		defer startTemperatureSampler()
		time.Sleep(300 * time.Millisecond)
		err := repairTemperatureProviderBundleElevated()
		temperatureProviderState.Lock()
		temperatureProviderState.Installing = false
		if err != nil {
			temperatureProviderState.LastError = err.Error()
		} else {
			temperatureProviderState.LastError = ""
			temperatureProviderState.LastOK = time.Now()
			temperatureProviderState.LatestVersion = temperatureBundledProviderVersion
			temperatureProviderState.UpdateAvailable = false
			temperatureCollectorControl.Lock()
			temperatureCollectorControl.ProviderUpdatePendingLogged = false
			temperatureCollectorControl.Unlock()
		}
		temperatureProviderState.Unlock()
		if err != nil {
			pushAppNotification(notifError, "Не удалось обновить датчики", err.Error(), notifTargetSensors)
		} else {
			pushAppNotification(notifSuccess, "Датчики обновлены", "Аппаратный провайдер успешно обновлён и перезапущен.", notifTargetSensors)
		}
		if err == nil {
			time.Sleep(1200 * time.Millisecond)
			sampleTemperatures()
		}
		if app.hwnd != 0 {
			pPostMessageW.Call(app.hwnd, WM_RESOURCE_UPDATED, 0, 0)
		}
	}()
}

var temperatureCollectorControl struct {
	sync.Mutex
	Repairing                   bool
	TaskRepairing               bool
	LastEnsure                  time.Time
	LastTaskRepair              time.Time
	ProviderUpdatePendingLogged bool
}

var temperatureDiagnosticsState struct {
	sync.Mutex
	LastZeroLog time.Time
}

var temperatureWindowsFallbackCache struct {
	sync.Mutex
	Sensors []TemperatureSensor
	Updated time.Time
}

func temperatureCollectorStatusFile() string {
	d := temperatureProviderDir()
	if d == "" {
		return ""
	}
	return filepath.Join(d, "collector_status.json")
}

func findFrameworkCSC() string {
	windir := os.Getenv("WINDIR")
	if windir == "" {
		windir = `C:\Windows`
	}
	candidates := []string{
		filepath.Join(windir, `Microsoft.NET\Framework64\v4.0.30319\csc.exe`),
		filepath.Join(windir, `Microsoft.NET\Framework\v4.0.30319\csc.exe`),
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

func compileTemperatureCollector(outPath string) error {
	if !temperatureProviderBasePresent() {
		return fmt.Errorf("LibreHardwareMonitorLib.dll не установлен")
	}
	csc := findFrameworkCSC()
	if csc == "" {
		return fmt.Errorf("не найден .NET Framework csc.exe")
	}
	src := temperatureProviderCollectorSourceFile()
	if src == "" {
		return fmt.Errorf("не удалось определить путь коллектора")
	}
	if err := os.WriteFile(src, []byte(temperatureCollectorCSharp()), 0644); err != nil {
		return err
	}
	_ = os.Remove(outPath)
	cmd := exec.Command(csc,
		"/nologo", "/optimize+", "/target:winexe", "/platform:x64",
		"/out:"+outPath,
		"/reference:"+temperatureProviderDLL(),
		src,
	)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("не удалось собрать коллектор: %s", msg)
	}
	if _, err := os.Stat(outPath); err != nil {
		return fmt.Errorf("коллектор не создан")
	}
	return nil
}

const temperatureTaskName = "PowerPilot Hardware Sensors"

// Scheduled-task operations must pass the task name as a real argv item. 0.6.11
// wrapped schtasks.exe in `cmd /s /c` to change the console code page; on Windows,
// cmd.exe then stripped the nested quotes around a task name containing spaces and
// schtasks received "Hardware" as a separate option. That made both /Query and /Run
// fail even when the task had been created correctly.
func runSchtasks(args ...string) ([]byte, error) {
	cmd := exec.Command("schtasks.exe", args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	return cmd.CombinedOutput()
}

func temperatureScheduledTaskExists() bool {
	_, err := runSchtasks("/Query", "/TN", temperatureTaskName, "/FO", "LIST")
	return err == nil
}

func runTemperatureScheduledTask() error {
	out, err := runSchtasks("/Run", "/TN", temperatureTaskName)
	if err != nil {
		// Do not route this command through cmd.exe merely to change the code page:
		// preserving the argv boundary is more important than localized diagnostic
		// text. The Win32 exit error is always readable; append native output only
		// when it is present.
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		} else {
			msg = msg + " (" + err.Error() + ")"
		}
		return fmt.Errorf("не удалось запустить повышенный коллектор: %s", msg)
	}
	return nil
}

func repairTemperatureScheduledTaskElevated() error {
	return runElevatedTemperatureHelper("task-repair", 90*time.Second)
}

func ensureTemperatureScheduledTaskAsync(force bool) {
	if !temperatureProviderInstalled() || !temperatureProviderBasePresent() {
		return
	}
	if !force && temperatureScheduledTaskExists() {
		return
	}
	temperatureCollectorControl.Lock()
	if temperatureCollectorControl.TaskRepairing || (!force && time.Since(temperatureCollectorControl.LastTaskRepair) < 2*time.Minute) {
		temperatureCollectorControl.Unlock()
		return
	}
	temperatureCollectorControl.TaskRepairing = true
	temperatureCollectorControl.LastTaskRepair = time.Now()
	temperatureCollectorControl.Unlock()

	go func() {
		defer func() {
			temperatureCollectorControl.Lock()
			temperatureCollectorControl.TaskRepairing = false
			temperatureCollectorControl.Unlock()
			if app.hwnd != 0 {
				pPostMessageW.Call(app.hwnd, WM_RESOURCE_UPDATED, 0, 0)
			}
		}()
		techLog040("temperature scheduled task missing; starting elevated self-repair")
		if err := repairTemperatureScheduledTaskElevated(); err != nil {
			techLog040("temperature scheduled task self-repair failed: " + err.Error())
			temperatureProviderState.Lock()
			temperatureProviderState.LastError = "Не удалось восстановить фоновую задачу датчиков: " + err.Error()
			temperatureProviderState.Unlock()
			return
		}
		temperatureProviderState.Lock()
		temperatureProviderState.LastError = ""
		temperatureProviderState.Unlock()
		techLog040("temperature scheduled task restored successfully")
		time.Sleep(1200 * time.Millisecond)
		sampleTemperatures()
	}()
}

func launchTemperatureCollectorFallback() {
	exe := temperatureProviderCollectorExe()
	if exe == "" {
		return
	}
	root := temperatureProviderDir()
	if root == "" {
		root = filepath.Dir(exe)
	}
	cmd := exec.Command(exe, "--output-dir", root)
	cmd.Dir = filepath.Dir(exe)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	_ = cmd.Start()
	if cmd.Process != nil {
		_ = cmd.Process.Release()
	}
}

func repairTemperatureProviderBundleElevated() error {
	techLog040("temperature provider manual native elevated update starting")
	err := runElevatedTemperatureHelper("update", 180*time.Second)
	if err != nil {
		if p := temperatureAdminResultFile("update"); p != "" {
			if b, e := os.ReadFile(p); e == nil {
				techLog040("temperature provider native update result: " + strings.TrimSpace(string(b)))
			}
		}
		return fmt.Errorf("не удалось выполнить обновление датчиков: %w", err)
	}
	techLog040("temperature provider native elevated update completed")
	return nil
}

func repairTemperatureCollectorAsync() {
	if !temperatureProviderBasePresent() || !temperatureCollectorNeedsRepair() {
		return
	}
	temperatureCollectorControl.Lock()
	if temperatureCollectorControl.Repairing {
		temperatureCollectorControl.Unlock()
		return
	}
	temperatureCollectorControl.Repairing = true
	temperatureCollectorControl.Unlock()

	go func() {
		defer func() {
			temperatureCollectorControl.Lock()
			temperatureCollectorControl.Repairing = false
			temperatureCollectorControl.Unlock()
			if app.hwnd != 0 {
				pPostMessageW.Call(app.hwnd, WM_RESOURCE_UPDATED, 0, 0)
			}
		}()

		runtimeDir := temperatureProviderRuntimeDir()
		if runtimeDir == "" {
			return
		}
		_ = os.MkdirAll(runtimeDir, 0755)
		next := filepath.Join(runtimeDir, "PowerPilotSensors.next.exe")
		if err := compileTemperatureCollector(next); err != nil {
			techLog040("temperature collector repair compile failed: " + err.Error())
			temperatureProviderState.Lock()
			temperatureProviderState.LastError = err.Error()
			temperatureProviderState.Unlock()
			return
		}

		// Make the currently installed collector console-less even when a provider
		// update is waiting for manual confirmation. This removes the persistent
		// terminal window independently of the LibreHardwareMonitor bundle update.
		_, _ = runSchtasks("/End", "/TN", temperatureTaskName)
		cmdKill := exec.Command("taskkill.exe", "/IM", "PowerPilotSensors.exe", "/F")
		cmdKill.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
		_ = cmdKill.Run()
		time.Sleep(350 * time.Millisecond)
		current := temperatureProviderCollectorExe()
		backup := current + ".bak"
		_ = os.Remove(backup)
		if _, err := os.Stat(current); err == nil {
			_ = os.Rename(current, backup)
		}
		if err := os.Rename(next, current); err != nil {
			if _, statErr := os.Stat(current); statErr != nil {
				_ = os.Rename(backup, current)
			}
			techLog040("temperature collector image locked; native elevated task repair: " + err.Error())
			temperatureProviderState.Lock()
			temperatureProviderState.LastError = "Не удалось обновить скрытый коллектор датчиков: " + err.Error()
			temperatureProviderState.Unlock()
			return
		}
		_ = os.Remove(backup)
		_ = os.WriteFile(temperatureProviderRevisionFile(), []byte(temperatureCollectorRevision), 0644)
		if err := runTemperatureScheduledTask(); err != nil {
			techLog040("temperature scheduled task restart failed: " + err.Error())
			ensureTemperatureScheduledTaskAsync(true)
		} else {
			techLog040("temperature collector revision " + temperatureCollectorRevision + " repaired as windows GUI and restarted")
		}

		if temperatureBundledProviderNeedsInstall() {
			temperatureCollectorControl.Lock()
			shouldLog := !temperatureCollectorControl.ProviderUpdatePendingLogged
			temperatureCollectorControl.ProviderUpdatePendingLogged = true
			temperatureCollectorControl.Unlock()
			if shouldLog {
				techLog040("temperature provider update pending; current hidden collector kept running until manual update")
			}
			temperatureProviderState.Lock()
			if temperatureProviderState.LastError == "" {
				temperatureProviderState.LastError = "Доступно обновление датчиков. Установите его вручную в Настройки → Данные."
			}
			temperatureProviderState.Unlock()
		} else {
			temperatureProviderState.Lock()
			temperatureProviderState.LastError = ""
			temperatureProviderState.Unlock()
		}
	}()
}

func ensureTemperatureCollectorRunningAsync() {
	if !temperatureProviderInstalled() {
		return
	}
	p := temperatureProviderSnapshotFile()
	fresh := false
	if p != "" {
		if st, err := os.Stat(p); err == nil && time.Since(st.ModTime()) <= 10*time.Second {
			fresh = true
		}
	}
	if fresh {
		return
	}
	// A pending provider update must never stop the currently installed collector.
	// 0.6.15 treated “new bundle available” as a runtime repair and returned here,
	// leaving sensors.json stale and reducing the UI to the single Storage fallback.
	// Only a genuinely missing collector blocks restart; revision/provider upgrades
	// happen independently and explicitly.
	if exe := temperatureProviderCollectorExe(); exe == "" {
		return
	} else if _, err := os.Stat(exe); err != nil {
		repairTemperatureCollectorAsync()
		return
	}
	temperatureCollectorControl.Lock()
	if time.Since(temperatureCollectorControl.LastEnsure) < 12*time.Second {
		temperatureCollectorControl.Unlock()
		return
	}
	temperatureCollectorControl.LastEnsure = time.Now()
	temperatureCollectorControl.Unlock()
	go func() {
		if !temperatureScheduledTaskExists() {
			ensureTemperatureScheduledTaskAsync(false)
			return
		}
		if err := runTemperatureScheduledTask(); err != nil {
			techLog040("temperature collector stale; scheduled restart failed: " + err.Error())
			ensureTemperatureScheduledTaskAsync(true)
			launchTemperatureCollectorFallback()
		}
	}()
}

func startTemperatureSampler() {
	if temperatureProviderBasePresent() {
		repairTemperatureCollectorAsync()
		ensureTemperatureScheduledTaskAsync(false)
		ensureTemperatureCollectorRunningAsync()
	}
	temperatureState.Lock()
	if temperatureState.Stop != nil {
		temperatureState.Unlock()
		return
	}
	temperatureState.Stop = make(chan struct{})
	stop := temperatureState.Stop
	temperatureState.Unlock()
	go func() {
		sampleTemperatures()
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				sampleTemperatures()
			case <-stop:
				return
			}
		}
	}()
}

func stopTemperatureSampler() {
	temperatureState.Lock()
	if temperatureState.Stop != nil {
		close(temperatureState.Stop)
		temperatureState.Stop = nil
	}
	temperatureState.Unlock()
}

func temperatureSensorsSnapshot() []TemperatureSensor {
	temperatureState.RLock()
	out := append([]TemperatureSensor(nil), temperatureState.Sensors...)
	temperatureState.RUnlock()
	return out
}

func hardwareSensorsSnapshot() []HardwareSensor {
	hardwareSensorState.RLock()
	out := append([]HardwareSensor(nil), hardwareSensorState.Sensors...)
	hardwareSensorState.RUnlock()
	return out
}

func hardwareSensorsLastUpdated() time.Time {
	hardwareSensorState.RLock()
	at := hardwareSensorState.Updated
	hardwareSensorState.RUnlock()
	return at
}

func temperatureLastUpdated() time.Time {
	temperatureState.RLock()
	at := temperatureState.Updated
	temperatureState.RUnlock()
	return at
}

func sampleElevatedHardwareSensors() []HardwareSensor {
	p := temperatureProviderSnapshotFile()
	if p == "" {
		return nil
	}
	st, err := os.Stat(p)
	if err != nil || time.Since(st.ModTime()) > 12*time.Second {
		return nil
	}
	raw, err := os.ReadFile(p)
	if err != nil || len(raw) == 0 {
		return nil
	}
	var arr []HardwareSensor
	if json.Unmarshal(raw, &arr) != nil {
		return nil
	}
	for i := range arr {
		// Legacy collector revisions wrote only temperatures and had no SensorType.
		// Treat those records as Temperature until revision 9 replaces the collector.
		if strings.TrimSpace(arr[i].SensorType) == "" {
			arr[i].SensorType = "Temperature"
		}
	}
	return normalizeHardwareSensors(arr)
}

func sampleElevatedTemperatureSensors() []TemperatureSensor {
	hw := sampleElevatedHardwareSensors()
	out := make([]TemperatureSensor, 0, len(hw))
	for _, s := range hw {
		if !strings.EqualFold(s.SensorType, "Temperature") {
			continue
		}
		if s.Value <= 1 || s.Value > 170 || math.IsNaN(s.Value) || math.IsInf(s.Value, 0) {
			continue
		}
		out = append(out, TemperatureSensor{Hardware: s.Hardware, Name: s.Name, ValueC: s.Value, Source: s.Source, HardwareType: s.HardwareType, Identifier: s.Identifier})
	}
	return out
}

func normalizeHardwareSensors(in []HardwareSensor) []HardwareSensor {
	best := make(map[string]HardwareSensor, len(in))
	for _, s := range in {
		if strings.TrimSpace(s.Hardware) == "" || strings.TrimSpace(s.Name) == "" || strings.TrimSpace(s.SensorType) == "" {
			continue
		}
		if math.IsNaN(s.Value) || math.IsInf(s.Value, 0) {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(s.Hardware) + "|" + strings.TrimSpace(s.Name) + "|" + strings.TrimSpace(s.SensorType))
		prev, ok := best[key]
		// Prefer the broad Full profile when duplicate sensors exist; otherwise keep
		// the first stable reading. Isolated profiles are diagnostic fallbacks.
		if !ok || (strings.Contains(strings.ToLower(s.Source), "· full") && !strings.Contains(strings.ToLower(prev.Source), "· full")) {
			best[key] = s
		}
	}
	out := make([]HardwareSensor, 0, len(best))
	for _, s := range best {
		out = append(out, s)
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if !strings.EqualFold(a.Hardware, b.Hardware) {
			return strings.ToLower(a.Hardware) < strings.ToLower(b.Hardware)
		}
		if !strings.EqualFold(a.SensorType, b.SensorType) {
			return strings.ToLower(a.SensorType) < strings.ToLower(b.SensorType)
		}
		return strings.ToLower(a.Name) < strings.ToLower(b.Name)
	})
	return out
}

func sampleTemperatures() {
	// Temperatures are opt-in: before the user installs the provider, show none.
	// Once the payload exists, however, a broken/stale elevated collector must not
	// blank vendor/Windows fallback temperatures as happened in 0.6.8/0.6.9.
	if !temperatureMonitoringEnabled() {
		temperatureState.Lock()
		temperatureState.Sensors = nil
		temperatureState.Updated = time.Time{}
		temperatureState.Unlock()
		hardwareSensorState.Lock()
		hardwareSensorState.Sensors = nil
		hardwareSensorState.Updated = time.Time{}
		hardwareSensorState.Unlock()
		return
	}

	ensureTemperatureCollectorRunningAsync()
	hardwareSensors := sampleElevatedHardwareSensors()
	sensors := make([]TemperatureSensor, 0, len(hardwareSensors))
	for _, hs := range hardwareSensors {
		if strings.EqualFold(hs.SensorType, "Temperature") && hs.Value > 1 && hs.Value <= 170 && !math.IsNaN(hs.Value) && !math.IsInf(hs.Value, 0) {
			sensors = append(sensors, TemperatureSensor{Hardware: hs.Hardware, Name: hs.Name, ValueC: hs.Value, Source: hs.Source, HardwareType: hs.HardwareType, Identifier: hs.Identifier})
		}
	}
	// Merge complementary Windows/LHM/ACPI/storage sources even when the elevated
	// collector returned some sensors. 0.6.12 skipped this entire branch as soon as
	// one LHM temperature existed, which hid additional storage/thermal-zone values.
	sensors = append(sensors, sampleWindowsTemperatureSensorsCached()...)
	nv := sampleNVMLTemperatureSensors()
	sensors = append(sensors, nv...)
	if len(nv) == 0 {
		nv = sampleNVAPITemperatureSensors()
		sensors = append(sensors, nv...)
	}
	if len(nv) == 0 {
		sensors = append(sensors, sampleNvidiaSMITemperatureSensors()...)
	}
	sensors = normalizeTemperatureSensors(sensors)
	// Surface complementary fallback temperatures in the generic sensor browser as
	// well. They intentionally have no Min/Max because Windows/vendor fallbacks do
	// not maintain the LibreHardwareMonitor lifetime extrema.
	advancedHardware := append([]HardwareSensor(nil), hardwareSensors...)
	for _, ts := range sensors {
		advancedHardware = append(advancedHardware, HardwareSensor{Hardware: ts.Hardware, Name: ts.Name, Value: ts.ValueC, SensorType: "Temperature", Source: ts.Source, HardwareType: ts.HardwareType, Identifier: ts.Identifier})
	}
	advancedHardware = normalizeHardwareSensors(advancedHardware)
	hardwareSensorState.Lock()
	hardwareSensorState.Sensors = advancedHardware
	if len(advancedHardware) > 0 {
		hardwareSensorState.Updated = time.Now()
	}
	hardwareSensorState.Unlock()
	// Do not stay silent when only part of the hardware tree is visible. This makes
	// future diagnostics useful even when GPU/storage sensors keep the total non-zero.
	missingCPU := true
	for _, ts := range sensors {
		if temperatureSensorClass(ts) == "cpu" {
			missingCPU = false
			break
		}
	}
	if missingCPU {
		temperatureDiagnosticsState.Lock()
		shouldLogPartial := time.Since(temperatureDiagnosticsState.LastZeroLog) >= time.Minute
		if shouldLogPartial {
			temperatureDiagnosticsState.LastZeroLog = time.Now()
		}
		temperatureDiagnosticsState.Unlock()
		if shouldLogPartial {
			status := ""
			if p := temperatureCollectorStatusFile(); p != "" {
				if raw, err := os.ReadFile(p); err == nil {
					status = strings.TrimSpace(string(raw))
					if len(status) > 1200 {
						status = status[:1200] + "…"
					}
				}
			}
			techLog040(fmt.Sprintf("temperature partial: sensors=%d cpu=missing provider=%s status=%s", len(sensors), temperatureProviderInstalledVersion(), status))
		}
	}
	if len(sensors) == 0 {
		temperatureDiagnosticsState.Lock()
		shouldLog := time.Since(temperatureDiagnosticsState.LastZeroLog) >= time.Minute
		if shouldLog {
			temperatureDiagnosticsState.LastZeroLog = time.Now()
		}
		temperatureDiagnosticsState.Unlock()
		if shouldLog {
			status := ""
			if p := temperatureCollectorStatusFile(); p != "" {
				if raw, err := os.ReadFile(p); err == nil {
					status = strings.TrimSpace(string(raw))
					if len(status) > 900 {
						status = status[:900] + "…"
					}
				}
			}
			techLog040(fmt.Sprintf("temperature sample empty: base=%v installed=%v collectorRepair=%v status=%s", temperatureProviderBasePresent(), temperatureProviderInstalled(), temperatureCollectorNeedsRepair(), status))
		}
	}
	temperatureState.Lock()
	temperatureState.Sensors = sensors
	temperatureState.Updated = time.Now()
	temperatureState.Unlock()
	if app.hwnd != 0 && (app.section == 18 || app.section == 19) {
		pPostMessageW.Call(app.hwnd, WM_RESOURCE_UPDATED, 0, 0)
	}
}

func sampleWindowsTemperatureSensorsCached() []TemperatureSensor {
	temperatureWindowsFallbackCache.Lock()
	if time.Since(temperatureWindowsFallbackCache.Updated) < 30*time.Second {
		out := append([]TemperatureSensor(nil), temperatureWindowsFallbackCache.Sensors...)
		temperatureWindowsFallbackCache.Unlock()
		return out
	}
	temperatureWindowsFallbackCache.Unlock()
	out := sampleWindowsTemperatureSensors()
	temperatureWindowsFallbackCache.Lock()
	temperatureWindowsFallbackCache.Sensors = append([]TemperatureSensor(nil), out...)
	temperatureWindowsFallbackCache.Updated = time.Now()
	temperatureWindowsFallbackCache.Unlock()
	return out
}

func sampleWindowsTemperatureSensors() []TemperatureSensor {
	// Low-level LibreHardwareMonitor access belongs exclusively to the persistent
	// elevated collector. Loading LibreHardwareMonitorLib.dll from a short-lived
	// PowerShell probe every five seconds used to lock System.Memory.dll and other
	// runtime dependencies while an update was trying to replace them. Keep this
	// fallback intentionally lightweight: WMI providers, ACPI thermal zones and
	// Windows storage-reliability temperatures only.
	script := `$ErrorActionPreference='SilentlyContinue'; [Console]::OutputEncoding = New-Object System.Text.UTF8Encoding($false); $out=@();
function Add-T([string]$hw,[string]$name,[double]$value,[string]$source){ if($value -gt -20 -and $value -lt 160){ $script:out += [pscustomobject]@{Hardware=$hw;Name=$name;Value=$value;Source=$source} } }
foreach($prov in @(@('root\LibreHardwareMonitor','LibreHardwareMonitor WMI'),@('root\OpenHardwareMonitor','OpenHardwareMonitor WMI'))){
  try { $map=@{}; Get-CimInstance -Namespace $prov[0] -ClassName Hardware | ForEach-Object { $map[$_.Identifier]=$_.Name }; Get-CimInstance -Namespace $prov[0] -ClassName Sensor | Where-Object { $_.SensorType -eq 'Temperature' -and $null -ne $_.Value } | ForEach-Object { $hw=$map[$_.Parent]; if([string]::IsNullOrWhiteSpace($hw)){$hw=$_.Parent}; Add-T $hw $_.Name ([double]$_.Value) $prov[1] } } catch {}
}
try { Get-CimInstance -Namespace 'root/wmi' -ClassName MSAcpi_ThermalZoneTemperature | ForEach-Object { $v=([double]$_.CurrentTemperature/10.0)-273.15; Add-T $_.InstanceName 'Thermal Zone' $v 'ACPI' } } catch {}
try { Get-CimInstance -Namespace 'root/cimv2' -ClassName Win32_PerfFormattedData_Counters_ThermalZoneInformation | ForEach-Object { $v=[double]$_.Temperature; if($v -gt 200){$v=$v-273.15}; Add-T $_.Name 'Thermal Zone' $v 'Windows Thermal Counter' } } catch {}
try { Get-PhysicalDisk | ForEach-Object { $d=$_; try { $r=$d | Get-StorageReliabilityCounter; if($null -ne $r.Temperature -and [double]$r.Temperature -gt 0){ Add-T $d.FriendlyName 'Температура накопителя' ([double]$r.Temperature) 'Storage' } } catch {} } } catch {}
$out | ConvertTo-Json -Compress -Depth 3`
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-ExecutionPolicy", "Bypass", "-Command", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	raw, err := cmd.Output()
	if err != nil || len(raw) == 0 {
		return nil
	}
	raw = []byte(strings.TrimSpace(string(raw)))
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var arr []TemperatureSensor
	if raw[0] == '[' {
		if json.Unmarshal(raw, &arr) == nil {
			return arr
		}
		return nil
	}
	var one TemperatureSensor
	if json.Unmarshal(raw, &one) == nil {
		return []TemperatureSensor{one}
	}
	return nil
}

func sampleNVMLTemperatureSensors() []TemperatureSensor {
	candidates := []string{"nvml.dll"}
	if root := os.Getenv("SystemRoot"); root != "" {
		candidates = append(candidates, filepath.Join(root, "System32", "nvml.dll"))
	}
	if pf := os.Getenv("ProgramFiles"); pf != "" {
		candidates = append(candidates, filepath.Join(pf, "NVIDIA Corporation", "NVSMI", "nvml.dll"))
	}
	seenPath := map[string]bool{}
	for _, path := range candidates {
		key := strings.ToLower(path)
		if seenPath[key] {
			continue
		}
		seenPath[key] = true
		dll, err := syscall.LoadDLL(path)
		if err != nil {
			continue
		}
		out := sampleNVMLFromDLL(dll)
		_ = dll.Release()
		if len(out) > 0 {
			return out
		}
	}
	return nil
}

func sampleNVMLFromDLL(dll *syscall.DLL) []TemperatureSensor {
	initProc, err := dll.FindProc("nvmlInit_v2")
	if err != nil {
		return nil
	}
	shutdownProc, err := dll.FindProc("nvmlShutdown")
	if err != nil {
		return nil
	}
	countProc, err := dll.FindProc("nvmlDeviceGetCount_v2")
	if err != nil {
		return nil
	}
	handleProc, err := dll.FindProc("nvmlDeviceGetHandleByIndex_v2")
	if err != nil {
		return nil
	}
	tempProc, err := dll.FindProc("nvmlDeviceGetTemperature")
	if err != nil {
		return nil
	}
	nameProc, _ := dll.FindProc("nvmlDeviceGetName")
	if r, _, _ := initProc.Call(); uint32(r) != 0 {
		return nil
	}
	defer shutdownProc.Call()
	var count uint32
	if r, _, _ := countProc.Call(uintptr(unsafe.Pointer(&count))); uint32(r) != 0 || count == 0 {
		return nil
	}
	out := make([]TemperatureSensor, 0, count)
	for i := uint32(0); i < count; i++ {
		var dev uintptr
		if r, _, _ := handleProc.Call(uintptr(i), uintptr(unsafe.Pointer(&dev))); uint32(r) != 0 || dev == 0 {
			continue
		}
		var temp uint32
		if r, _, _ := tempProc.Call(dev, 0, uintptr(unsafe.Pointer(&temp))); uint32(r) != 0 || temp == 0 || temp > 150 {
			continue
		}
		name := fmt.Sprintf("NVIDIA GPU %d", i+1)
		if nameProc != nil {
			buf := make([]byte, 96)
			if r, _, _ := nameProc.Call(dev, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf))); uint32(r) == 0 {
				if z := bytesUntilZero(buf); z != "" {
					name = z
				}
			}
		}
		out = append(out, TemperatureSensor{Hardware: name, Name: "GPU Core", ValueC: float64(temp), Source: "NVIDIA NVML"})
	}
	return out
}

type nvThermalSensor struct {
	Controller     uint32
	DefaultMinTemp uint32
	DefaultMaxTemp uint32
	CurrentTemp    uint32
	Target         uint32
}

type nvThermalSettings struct {
	Version uint32
	Count   uint32
	Sensor  [3]nvThermalSensor
}

func sampleNVAPITemperatureSensors() []TemperatureSensor {
	// NVAPI is installed with the NVIDIA display driver and is independent of NVML.
	// Some consumer-driver installations expose nvapi64.dll but not nvml.dll or
	// nvidia-smi, so this gives PowerPilot a third direct NVIDIA temperature path.
	candidates := []string{"nvapi64.dll"}
	if root := os.Getenv("SystemRoot"); root != "" {
		candidates = append(candidates, filepath.Join(root, "System32", "nvapi64.dll"))
	}
	for _, path := range candidates {
		dll, err := syscall.LoadDLL(path)
		if err != nil {
			continue
		}
		out := sampleNVAPIFromDLL(dll)
		_ = dll.Release()
		if len(out) > 0 {
			return out
		}
	}
	return nil
}

func sampleNVAPIFromDLL(dll *syscall.DLL) []TemperatureSensor {
	query, err := dll.FindProc("nvapi_QueryInterface")
	if err != nil {
		query, err = dll.FindProc("NvAPI_QueryInterface")
		if err != nil {
			return nil
		}
	}
	get := func(id uint32) uintptr {
		p, _, _ := query.Call(uintptr(id))
		return p
	}
	const (
		nvAPIInitializeID       = 0x0150E828
		nvAPIEnumPhysicalGPUsID = 0xE5AC921F
		nvAPIGPUFullNameID      = 0xCEEE8E9F
		nvAPIGPUThermalID       = 0xE3640A56
	)
	initPtr := get(nvAPIInitializeID)
	enumPtr := get(nvAPIEnumPhysicalGPUsID)
	thermalPtr := get(nvAPIGPUThermalID)
	if initPtr == 0 || enumPtr == 0 || thermalPtr == 0 {
		return nil
	}
	if r, _, _ := syscall.SyscallN(initPtr); int32(r) != 0 {
		return nil
	}
	var handles [64]uintptr
	var count uint32
	if r, _, _ := syscall.SyscallN(enumPtr, uintptr(unsafe.Pointer(&handles[0])), uintptr(unsafe.Pointer(&count))); int32(r) != 0 || count == 0 {
		return nil
	}
	namePtr := get(nvAPIGPUFullNameID)
	out := make([]TemperatureSensor, 0, count)
	for i := uint32(0); i < count && i < uint32(len(handles)); i++ {
		h := handles[i]
		if h == 0 {
			continue
		}
		name := fmt.Sprintf("NVIDIA GPU %d", i+1)
		if namePtr != 0 {
			buf := make([]byte, 64)
			if r, _, _ := syscall.SyscallN(namePtr, h, uintptr(unsafe.Pointer(&buf[0]))); int32(r) == 0 {
				if z := bytesUntilZero(buf); z != "" {
					name = z
				}
			}
		}
		var settings nvThermalSettings
		settings.Version = uint32(unsafe.Sizeof(settings)) | (2 << 16)
		// Sensor index 0 is the GPU core sensor on drivers that expose this legacy
		// thermal interface. If the driver reports multiple sensors, keep them all.
		if r, _, _ := syscall.SyscallN(thermalPtr, h, 0, uintptr(unsafe.Pointer(&settings))); int32(r) != 0 {
			continue
		}
		limit := settings.Count
		if limit == 0 || limit > uint32(len(settings.Sensor)) {
			limit = 1
		}
		for j := uint32(0); j < limit; j++ {
			t := settings.Sensor[j].CurrentTemp
			if t == 0 || t > 150 {
				continue
			}
			sensorName := "GPU Core"
			if j > 0 {
				sensorName = fmt.Sprintf("GPU Thermal %d", j+1)
			}
			out = append(out, TemperatureSensor{Hardware: name, Name: sensorName, ValueC: float64(t), Source: "NVIDIA NVAPI"})
		}
	}
	return out
}

func sampleNvidiaSMITemperatureSensors() []TemperatureSensor {
	paths := []string{}
	if p, err := exec.LookPath("nvidia-smi.exe"); err == nil {
		paths = append(paths, p)
	}
	if root := os.Getenv("SystemRoot"); root != "" {
		paths = append(paths, filepath.Join(root, "System32", "nvidia-smi.exe"))
	}
	if pf := os.Getenv("ProgramFiles"); pf != "" {
		paths = append(paths, filepath.Join(pf, "NVIDIA Corporation", "NVSMI", "nvidia-smi.exe"))
	}
	seen := map[string]bool{}
	for _, p := range paths {
		if p == "" || seen[strings.ToLower(p)] {
			continue
		}
		seen[strings.ToLower(p)] = true
		if _, err := os.Stat(p); err != nil {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
		cmd := exec.CommandContext(ctx, p, "--query-gpu=name,temperature.gpu", "--format=csv,noheader,nounits")
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
		raw, err := cmd.Output()
		cancel()
		if err != nil {
			continue
		}
		out := []TemperatureSensor{}
		for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
			parts := strings.Split(line, ",")
			if len(parts) < 2 {
				continue
			}
			name := strings.TrimSpace(strings.Join(parts[:len(parts)-1], ","))
			v, err := strconv.ParseFloat(strings.TrimSpace(parts[len(parts)-1]), 64)
			if err != nil || v <= 0 || v > 150 {
				continue
			}
			out = append(out, TemperatureSensor{Hardware: name, Name: "GPU Core", ValueC: v, Source: "NVIDIA SMI"})
		}
		if len(out) > 0 {
			return out
		}
	}
	return nil
}

func bytesUntilZero(b []byte) string {
	n := 0
	for n < len(b) && b[n] != 0 {
		n++
	}
	return strings.TrimSpace(string(b[:n]))
}

func normalizeTemperatureSensors(in []TemperatureSensor) []TemperatureSensor {
	// Prefer direct hardware/vendor readings over generic ACPI duplicates. Do not key on
	// the numeric value: the same physical sensor can be exposed by two providers with
	// slightly different rounding and would otherwise appear twice.
	priority := func(src string) int {
		s := strings.ToLower(src)
		switch {
		case strings.Contains(s, "librehardwaremonitor direct"):
			return 60
		case strings.Contains(s, "nvml"):
			return 58
		case strings.Contains(s, "nvapi"):
			return 56
		case strings.Contains(s, "nvidia smi"):
			return 55
		case strings.Contains(s, "librehardwaremonitor"), strings.Contains(s, "openhardwaremonitor"):
			return 50
		case strings.Contains(s, "storage"):
			return 40
		case strings.Contains(s, "acpi"):
			return 20
		default:
			return 10
		}
	}
	best := map[string]TemperatureSensor{}
	for _, s := range in {
		if math.IsNaN(s.ValueC) || s.ValueC <= 1 || s.ValueC > 160 {
			continue
		}
		s.Hardware = strings.TrimSpace(s.Hardware)
		s.Name = strings.TrimSpace(s.Name)
		s.Source = strings.TrimSpace(s.Source)
		if s.Hardware == "" {
			s.Hardware = "Датчик"
		}
		if s.Name == "" {
			s.Name = "Температура"
		}
		key := strings.ToLower(s.Hardware + "\x00" + s.Name)
		if old, ok := best[key]; !ok || priority(s.Source) > priority(old.Source) {
			best[key] = s
		}
	}
	out := make([]TemperatureSensor, 0, len(best))
	for _, s := range best {
		out = append(out, s)
	}
	sort.SliceStable(out, func(i, j int) bool {
		ai, aj := strings.ToLower(out[i].Hardware), strings.ToLower(out[j].Hardware)
		if ai == aj {
			return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
		}
		return ai < aj
	})
	return out
}

func temperatureSensorClass(s TemperatureSensor) string {
	ht := strings.ToLower(strings.TrimSpace(s.HardwareType))
	switch {
	case ht == "cpu":
		return "cpu"
	case strings.HasPrefix(ht, "gpu"):
		return "gpu"
	case ht == "storage":
		return "disk"
	}
	t := strings.ToLower(s.Hardware + " " + s.Name)
	if strings.Contains(t, "gpu") || strings.Contains(t, "nvidia") || strings.Contains(t, "radeon") || strings.Contains(t, "graphics") || strings.Contains(t, "igpu") || strings.Contains(t, "arc") {
		return "gpu"
	}
	if strings.Contains(t, "nvme") || strings.Contains(t, "ssd") || strings.Contains(t, "hdd") || strings.Contains(t, "storage") || strings.Contains(t, "disk") || strings.Contains(t, "накопител") ||
		strings.Contains(strings.ToLower(s.Source), "storage") {
		return "disk"
	}
	if strings.Contains(t, "cpu") || strings.Contains(t, "processor") || strings.Contains(t, "package") || strings.Contains(t, "core #") || strings.Contains(t, "ccd") || strings.Contains(t, "tdie") || strings.Contains(t, "tctl") {
		return "cpu"
	}
	return "other"
}

func temperatureSensorIsThreshold(s TemperatureSensor) bool {
	n := strings.ToLower(strings.TrimSpace(s.Name))
	return strings.Contains(n, "critical temperature") ||
		strings.Contains(n, "warning temperature") ||
		strings.Contains(n, "temperature limit") ||
		strings.Contains(n, "thermal limit") ||
		strings.Contains(n, "trip point") ||
		strings.Contains(n, "порог")
}

func averageTemperature(kind string) float64 {
	sensors := temperatureSensorsSnapshot()
	total := 0.0
	n := 0
	for _, s := range sensors {
		if temperatureSensorIsThreshold(s) {
			continue
		}
		if kind != "all" && temperatureSensorClass(s) != kind {
			continue
		}
		total += s.ValueC
		n++
	}
	if n > 0 {
		return total / float64(n)
	}
	// Windows ACPI thermal zones are not guaranteed to be the CPU package, but on
	// systems that expose no vendor/low-level CPU sensor they are still the only
	// OS-reported thermal reading. Use them strictly as a CPU-card fallback while
	// keeping their real names/sources visible in the advanced temperature table.
	if kind == "cpu" {
		total = 0
		n = 0
		for _, s := range sensors {
			src := strings.ToLower(s.Source)
			if (strings.Contains(src, "acpi") || strings.Contains(src, "thermal counter")) && s.ValueC >= 15 && s.ValueC <= 120 {
				total += s.ValueC
				n++
			}
		}
		if n > 0 {
			return total / float64(n)
		}
	}
	return -1
}

func averageTemperatureText(kind string) string {
	v := averageTemperature(kind)
	if v < 0 {
		return "Средняя t°: —"
	}
	return fmt.Sprintf("Средняя t°: %.0f°C", v)
}
