//go:build windows

package main

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

type sensorAdminResult struct {
	OK        bool   `json:"ok"`
	Action    string `json:"action"`
	Step      string `json:"step"`
	Error     string `json:"error,omitempty"`
	Provider  string `json:"provider,omitempty"`
	Collector string `json:"collector,omitempty"`
	Runtime   string `json:"runtime,omitempty"`
	UpdatedAt string `json:"updated_at"`
}

func temperatureAdminResultFile(action string) string {
	root := temperatureProviderDir()
	if root == "" {
		return ""
	}
	if action == "task-repair" || action == "task-disable" {
		return filepath.Join(root, "sensor_task_result.json")
	}
	return filepath.Join(root, "sensor_update_result.json")
}

func writeTemperatureAdminResult(action, step string, err error, runtimeDir string) {
	p := temperatureAdminResultFile(action)
	if p == "" {
		return
	}
	_ = os.MkdirAll(filepath.Dir(p), 0755)
	r := sensorAdminResult{OK: err == nil, Action: action, Step: step, Provider: temperatureBundledProviderVersion, Collector: temperatureCollectorRevision, Runtime: runtimeDir, UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	if err != nil {
		r.Error = err.Error()
	}
	b, _ := json.MarshalIndent(r, "", "  ")
	tmp := p + ".tmp"
	_ = os.WriteFile(tmp, b, 0644)
	_ = os.Rename(tmp, p)
}

func handleTemperatureAdminCommand() bool {
	if len(os.Args) < 2 {
		return false
	}
	var action string
	switch os.Args[1] {
	case "--sensor-admin-update":
		action = "update"
	case "--sensor-admin-task-repair":
		action = "task-repair"
	case "--sensor-admin-task-disable":
		action = "task-disable"
	default:
		return false
	}
	runtimeDir := ""
	var err error
	if action == "update" {
		runtimeDir, err = nativeTemperatureProviderUpdate()
	} else if action == "task-repair" {
		err = nativeTemperatureTaskRepair()
		runtimeDir = temperatureProviderRuntimeDir()
	} else {
		err = nativeTemperatureTaskDisable()
		runtimeDir = temperatureProviderRuntimeDir()
	}
	step := "done"
	if err != nil {
		step = "failed"
	}
	writeTemperatureAdminResult(action, step, err, runtimeDir)
	return true
}

func runElevatedTemperatureHelper(action string, timeout time.Duration) error {
	resultPath := temperatureAdminResultFile(action)
	if resultPath == "" {
		return fmt.Errorf("не удалось определить файл результата")
	}
	_ = os.MkdirAll(filepath.Dir(resultPath), 0755)
	_ = os.Remove(resultPath)
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	arg := "--sensor-admin-update"
	if action == "task-repair" {
		arg = "--sensor-admin-task-repair"
	} else if action == "task-disable" {
		arg = "--sensor-admin-task-disable"
	}

	shellExecuteW := syscall.NewLazyDLL("shell32.dll").NewProc("ShellExecuteW")
	r, _, callErr := shellExecuteW.Call(0, uintptr(unsafe.Pointer(wstr("runas"))), uintptr(unsafe.Pointer(wstr(exe))), uintptr(unsafe.Pointer(wstr(arg))), 0, SW_HIDE)
	if r <= 32 {
		if callErr != syscall.Errno(0) {
			return fmt.Errorf("не удалось запросить права администратора: %v", callErr)
		}
		return fmt.Errorf("не удалось запросить права администратора (ShellExecute=%d)", r)
	}

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if b, e := os.ReadFile(resultPath); e == nil && len(b) > 0 {
			var res sensorAdminResult
			if json.Unmarshal(b, &res) == nil {
				if res.OK {
					return nil
				}
				if res.Error != "" {
					return fmt.Errorf("%s", res.Error)
				}
				return fmt.Errorf("повышенная операция завершилась с ошибкой")
			}
		}
		time.Sleep(180 * time.Millisecond)
	}
	return fmt.Errorf("повышенная операция не завершилась за %s", timeout.Round(time.Second))
}

func extractBundledProviderTo(dir string) error {
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
			return fmt.Errorf("некорректный путь в пакете: %s", f.Name)
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
		src, err := f.Open()
		if err != nil {
			return err
		}
		dst, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
		if err != nil {
			src.Close()
			return err
		}
		_, cErr := io.Copy(dst, src)
		dErr := dst.Close()
		src.Close()
		if cErr != nil {
			return cErr
		}
		if dErr != nil {
			return dErr
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "LibreHardwareMonitorLib.dll")); err != nil {
		return fmt.Errorf("LibreHardwareMonitorLib.dll отсутствует во встроенном пакете")
	}
	return nil
}

func compileTemperatureCollectorAt(runtimeDir string) error {
	csc := findFrameworkCSC()
	if csc == "" {
		return fmt.Errorf("не найден .NET Framework csc.exe")
	}
	src := filepath.Join(runtimeDir, "PowerPilotSensors.cs")
	exe := filepath.Join(runtimeDir, "PowerPilotSensors.exe")
	if err := os.WriteFile(src, []byte(temperatureCollectorCSharp()), 0644); err != nil {
		return err
	}
	_ = os.Remove(exe)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, csc, "/nologo", "/optimize+", "/target:winexe", "/platform:x64", "/out:"+exe, "/reference:"+filepath.Join(runtimeDir, "LibreHardwareMonitorLib.dll"), src)
	cmd.Dir = runtimeDir
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		return fmt.Errorf("сборка коллектора превысила 45 секунд")
	}
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("не удалось собрать скрытый коллектор: %s", msg)
	}
	if _, err := os.Stat(exe); err != nil {
		return fmt.Errorf("PowerPilotSensors.exe не создан")
	}
	return os.WriteFile(filepath.Join(runtimeDir, "collector_revision.txt"), []byte(temperatureCollectorRevision), 0644)
}

func stopTemperatureTaskAndCollectors() {
	signalTemperatureCollectorStop()
	time.Sleep(1200 * time.Millisecond)
	_, _ = runSchtasks("/End", "/TN", temperatureTaskName)
	cmd := exec.Command("taskkill.exe", "/IM", "PowerPilotSensors.exe", "/F")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	_ = cmd.Run()
	time.Sleep(450 * time.Millisecond)
}

func scheduledTaskCommand(exe, root string) string {
	return `"` + exe + `" --output-dir "` + root + `"`
}

func registerTemperatureTaskNative(exe, root string) error {
	if exe == "" || root == "" {
		return fmt.Errorf("пустой путь задачи датчиков")
	}
	_, _ = runSchtasks("/End", "/TN", temperatureTaskName)
	_, _ = runSchtasks("/Delete", "/TN", temperatureTaskName, "/F")
	_ = os.Remove(filepath.Join(root, "collector_quarantine.flag"))
	tr := scheduledTaskCommand(exe, root)
	out, err := runSchtasks("/Create", "/TN", temperatureTaskName, "/TR", tr, "/SC", "ONLOGON", "/RL", "HIGHEST", "/F")
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("не удалось зарегистрировать задачу датчиков: %s", msg)
	}
	if out, err = runSchtasks("/Run", "/TN", temperatureTaskName); err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("не удалось запустить задачу датчиков: %s", msg)
	}
	return nil
}

func validateCollectorOnce(exe, runtimeDir string) error {
	testDir := filepath.Join(runtimeDir, "_probe")
	_ = os.RemoveAll(testDir)
	if err := os.MkdirAll(testDir, 0755); err != nil {
		return err
	}
	defer os.RemoveAll(testDir)
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, "--once", "--probe-cycles=5", "--output-dir", testDir)
	cmd.Dir = runtimeDir
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	if err := cmd.Run(); err != nil {
		status, _ := os.ReadFile(filepath.Join(testDir, "collector_status.json"))
		msg := strings.TrimSpace(string(status))
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("пробный запуск нового коллектора завершился ошибкой: %s", msg)
	}
	var status struct {
		SensorCount       int    `json:"SensorCount"`
		SnapshotPublished bool   `json:"SnapshotPublished"`
		Fatal             string `json:"Fatal"`
		Profiles          []struct {
			Name string `json:"Name"`
			OK   bool   `json:"OK"`
		} `json:"Profiles"`
	}
	raw, err := os.ReadFile(filepath.Join(testDir, "collector_status.json"))
	if err != nil {
		return fmt.Errorf("новый коллектор не создал collector_status.json")
	}
	if json.Unmarshal(raw, &status) != nil {
		return fmt.Errorf("новый коллектор создал повреждённый collector_status.json")
	}
	if status.Fatal != "" {
		return fmt.Errorf("новый коллектор: %s", status.Fatal)
	}
	if status.SensorCount <= 0 {
		return fmt.Errorf("новый коллектор запустился, но не обнаружил ни одного температурного датчика")
	}
	if !status.SnapshotPublished {
		return fmt.Errorf("новый коллектор не смог безопасно опубликовать снимок")
	}
	if len(status.Profiles) != 1 || status.Profiles[0].Name != "Minimal" || !status.Profiles[0].OK {
		return fmt.Errorf("новый коллектор не прошёл проверку безопасного профиля")
	}
	return nil
}

func waitForFreshCollectorOutput(root string, after time.Time) error {
	deadline := time.Now().Add(18 * time.Second)
	for time.Now().Before(deadline) {
		sp := filepath.Join(root, "collector_status.json")
		jp := filepath.Join(root, "sensors.json")
		ss, e1 := os.Stat(sp)
		js, e2 := os.Stat(jp)
		if e1 == nil && e2 == nil && ss.ModTime().After(after.Add(-time.Second)) && js.ModTime().After(after.Add(-time.Second)) {
			return nil
		}
		time.Sleep(220 * time.Millisecond)
	}
	return fmt.Errorf("фоновый коллектор не создал свежий снимок датчиков за 18 секунд")
}

func installPawnIONativeIfNeeded(root string) error {
	marker := filepath.Join(root, "pawnio-installed.txt")
	if b, err := os.ReadFile(marker); err == nil && strings.Contains(string(b), pawnIOVersion) {
		return nil
	}
	tmp := filepath.Join(os.TempDir(), "PowerPilot_PawnIO_setup.exe")
	_ = os.Remove(tmp)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, pawnIOURL, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("не удалось скачать PawnIO: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("PawnIO HTTP %d", resp.StatusCode)
	}
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	h := sha256.New()
	_, cErr := io.Copy(io.MultiWriter(f, h), resp.Body)
	closeErr := f.Close()
	if cErr != nil {
		return cErr
	}
	if closeErr != nil {
		return closeErr
	}
	got := hex.EncodeToString(h.Sum(nil))
	if !strings.EqualFold(got, pawnIOSHA256) {
		return fmt.Errorf("PawnIO SHA256 mismatch: %s", got)
	}
	cmd := exec.Command(tmp, "-install", "-silent")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	err = cmd.Run()
	exitCode := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			exitCode = ee.ExitCode()
		} else {
			return err
		}
		if exitCode != 3010 {
			return fmt.Errorf("PawnIO installer exit code %d", exitCode)
		}
	}
	_ = os.Remove(tmp)
	_ = os.WriteFile(marker, []byte(pawnIOVersion+" "+pawnIOSHA256), 0644)
	reboot := filepath.Join(root, "reboot_required.txt")
	if exitCode == 3010 {
		_ = os.WriteFile(reboot, []byte("1"), 0644)
	} else {
		_ = os.Remove(reboot)
	}
	return nil
}

func nativeTemperatureProviderUpdate() (string, error) {
	root := temperatureProviderDir()
	if root == "" {
		return "", fmt.Errorf("не удалось определить LocalAppData")
	}
	if err := os.MkdirAll(root, 0755); err != nil {
		return "", err
	}
	enabled := loadSettings().HardwareSensorsEnabled

	stageName := "runtime_0.9.7-pre724_825dc3d_" + time.Now().UTC().Format("20060102_150405")
	stage := filepath.Join(root, stageName)
	_ = os.RemoveAll(stage)
	if err := extractBundledProviderTo(stage); err != nil {
		return stage, fmt.Errorf("этап распаковки: %w", err)
	}
	if err := compileTemperatureCollectorAt(stage); err != nil {
		return stage, fmt.Errorf("этап сборки: %w", err)
	}
	if err := installPawnIONativeIfNeeded(root); err != nil {
		return stage, fmt.Errorf("этап PawnIO: %w", err)
	}

	stopTemperatureTaskAndCollectors()
	newExe := filepath.Join(stage, "PowerPilotSensors.exe")
	if enabled {
		if err := validateCollectorOnce(newExe, stage); err != nil {
			_ = os.WriteFile(filepath.Join(root, "collector_quarantine.flag"), []byte("collector validation failed\n"+err.Error()+"\n"), 0644)
			_ = nativeTemperatureTaskDisable()
			return stage, fmt.Errorf("этап проверки: %w", err)
		}
	}

	if err := os.WriteFile(filepath.Join(root, "active_provider_dir.txt"), []byte(stageName), 0644); err != nil {
		return stage, err
	}
	_ = os.WriteFile(filepath.Join(root, "version.txt"), []byte(temperatureBundledProviderVersion), 0644)
	_ = os.WriteFile(filepath.Join(root, "provider_bundle_revision.txt"), []byte(temperatureBundledProviderRevision), 0644)
	_ = os.WriteFile(filepath.Join(root, "provider_source.txt"), []byte("LibreHardwareMonitor official master CI\ncommit=825dc3de36c5816bb2a8b10b309244a8c362a7f9\nversion=0.9.7-pre724\nworkflow_run=31707859588\n"), 0644)

	if !enabled {
		_ = nativeTemperatureTaskDisable()
		return stage, nil
	}

	started := time.Now()
	_ = os.Remove(filepath.Join(root, "sensors.json"))
	_ = os.Remove(filepath.Join(root, "collector_status.json"))
	if err := registerTemperatureTaskNative(newExe, root); err != nil {
		_ = os.WriteFile(filepath.Join(root, "collector_quarantine.flag"), []byte("collector task start failed\n"+err.Error()+"\n"), 0644)
		_ = nativeTemperatureTaskDisable()
		return stage, fmt.Errorf("этап Планировщика заданий: %w", err)
	}
	if err := waitForFreshCollectorOutput(root, started); err != nil {
		_ = os.WriteFile(filepath.Join(root, "collector_quarantine.flag"), []byte("collector output timeout\n"+err.Error()+"\n"), 0644)
		_ = nativeTemperatureTaskDisable()
		return stage, fmt.Errorf("этап запуска: %w", err)
	}
	return stage, nil
}

func nativeTemperatureTaskRepair() error {
	if !loadSettings().HardwareSensorsEnabled {
		return fmt.Errorf("аппаратный мониторинг выключен пользователем")
	}
	root := temperatureProviderDir()
	runtimeDir := temperatureProviderRuntimeDir()
	if root == "" || runtimeDir == "" {
		return fmt.Errorf("не удалось определить папку датчиков")
	}
	exe := filepath.Join(runtimeDir, "PowerPilotSensors.exe")
	if _, err := os.Stat(exe); err != nil {
		return fmt.Errorf("PowerPilotSensors.exe отсутствует: %v", err)
	}
	stopTemperatureTaskAndCollectors()
	clearTemperatureCollectorQuarantine()
	if err := registerTemperatureTaskNative(exe, root); err != nil {
		_ = os.WriteFile(filepath.Join(root, "collector_quarantine.flag"), []byte("collector task repair failed\n"+err.Error()+"\n"), 0644)
		_ = nativeTemperatureTaskDisable()
		return err
	}
	if err := waitForFreshCollectorOutput(root, time.Now().Add(-time.Second)); err != nil {
		_ = os.WriteFile(filepath.Join(root, "collector_quarantine.flag"), []byte("collector task output timeout\n"+err.Error()+"\n"), 0644)
		_ = nativeTemperatureTaskDisable()
		return err
	}
	return nil
}

func nativeTemperatureTaskDisable() error {
	stopTemperatureTaskAndCollectors()
	if !temperatureScheduledTaskExists() {
		return nil
	}
	out, err := runSchtasks("/Change", "/TN", temperatureTaskName, "/DISABLE")
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("не удалось отключить задачу датчиков: %s", msg)
	}
	return nil
}

// Native GitHub release check: no PowerShell process is needed just to learn the
// latest LibreHardwareMonitor release tag.
func fetchLatestTemperatureProviderNative(ctx context.Context) (string, string, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/repos/LibreHardwareMonitor/LibreHardwareMonitor/releases/latest", nil)
	req.Header.Set("User-Agent", "PowerPilot/"+appVersion)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", "", fmt.Errorf("GitHub HTTP %d", resp.StatusCode)
	}
	var v struct {
		TagName string `json:"tag_name"`
		Assets  []struct {
			Name               string `json:"name"`
			BrowserDownloadURL string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&v); err != nil {
		return "", "", err
	}
	for _, a := range v.Assets {
		if a.Name == "LibreHardwareMonitor.zip" {
			return v.TagName, a.BrowserDownloadURL, nil
		}
	}
	return v.TagName, "", fmt.Errorf("LibreHardwareMonitor.zip не найден")
}
