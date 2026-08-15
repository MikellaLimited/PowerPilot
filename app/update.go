//go:build windows

package main

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

const (
	powerPilotUpdateRepo     = "MikellaLimited/PowerPilot"
	powerPilotUpdateInterval = 30 * time.Minute
)

// These variables are overridden with -ldflags for rolling develop builds.
// Stable builds keep the source defaults and continue to use /releases/latest.
var (
	powerPilotBuildVersion  = appVersion
	powerPilotUpdateChannel = "stable"
)

type powerPilotReleaseAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
	Digest             string `json:"digest"`
}

type powerPilotRelease struct {
	TagName string                   `json:"tag_name"`
	Name    string                   `json:"name"`
	Body    string                   `json:"body"`
	HTMLURL string                   `json:"html_url"`
	Assets  []powerPilotReleaseAsset `json:"assets"`
}

type powerPilotUpdateStateData struct {
	sync.RWMutex
	Checking      bool
	Downloading   bool
	Available     bool
	LatestVersion string
	ReleaseName   string
	ReleaseNotes  string
	ReleaseURL    string
	AssetURL      string
	AssetName     string
	AssetDigest   string
	AssetSize     int64
	Downloaded    int64
	LastCheck     time.Time
	LastError     string
	PackagePath   string
}

var powerPilotUpdateState powerPilotUpdateStateData

func normalizeReleaseVersion(v string) string {
	return strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(v), "v"), "V")
}

func currentPowerPilotVersion() string {
	v := strings.TrimSpace(powerPilotBuildVersion)
	if v == "" {
		return appVersion
	}
	return v
}

func isDevelopUpdateChannel() bool {
	return strings.EqualFold(strings.TrimSpace(powerPilotUpdateChannel), "develop")
}

func powerPilotReleaseAPIURL() string {
	base := "https://api.github.com/repos/" + powerPilotUpdateRepo + "/releases/"
	if isDevelopUpdateChannel() {
		return base + "tags/develop"
	}
	return base + "latest"
}

func powerPilotReleaseVersion(rel powerPilotRelease) (string, error) {
	if !isDevelopUpdateChannel() {
		v := normalizeReleaseVersion(rel.TagName)
		if v == "" {
			return "", fmt.Errorf("GitHub Release не содержит версии")
		}
		return v, nil
	}
	const marker = "powerpilot-develop-version:"
	for _, line := range strings.Split(rel.Body, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(strings.ToLower(line), marker) {
			v := normalizeReleaseVersion(strings.TrimSpace(line[len(marker):]))
			if v != "" {
				return v, nil
			}
		}
	}
	return "", fmt.Errorf("develop-релиз не содержит маркер версии")
}

func versionParts(v string) []int {
	v = normalizeReleaseVersion(v)
	fields := strings.Split(v, ".")
	out := make([]int, 0, len(fields))
	for _, f := range fields {
		n := 0
		for i, r := range f {
			if r < '0' || r > '9' {
				if i == 0 {
					n = 0
				}
				break
			}
			n = n*10 + int(r-'0')
		}
		out = append(out, n)
	}
	return out
}

func compareVersions(a, b string) int {
	aa, bb := versionParts(a), versionParts(b)
	n := len(aa)
	if len(bb) > n {
		n = len(bb)
	}
	for i := 0; i < n; i++ {
		av, bv := 0, 0
		if i < len(aa) {
			av = aa[i]
		}
		if i < len(bb) {
			bv = bb[i]
		}
		if av < bv {
			return -1
		}
		if av > bv {
			return 1
		}
	}
	return 0
}

func fetchLatestPowerPilotRelease(ctx context.Context) (powerPilotRelease, error) {
	var rel powerPilotRelease
	url := powerPilotReleaseAPIURL()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return rel, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "PowerPilot/"+currentPowerPilotVersion())
	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		return rel, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return rel, fmt.Errorf("репозиторий обновлений %s не найден или недоступен", powerPilotUpdateRepo)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return rel, fmt.Errorf("GitHub HTTP %d", resp.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&rel); err != nil {
		return rel, err
	}
	if strings.TrimSpace(rel.TagName) == "" {
		return rel, fmt.Errorf("GitHub Release не содержит версии")
	}
	return rel, nil
}

func selectPowerPilotUpdateAsset(rel powerPilotRelease) (powerPilotReleaseAsset, error) {
	if isDevelopUpdateChannel() {
		for _, a := range rel.Assets {
			if strings.EqualFold(a.Name, "PowerPilot_Develop_Update.zip") {
				return a, nil
			}
		}
		return powerPilotReleaseAsset{}, fmt.Errorf("в develop-релизе не найден PowerPilot_Develop_Update.zip")
	}
	ver := strings.ToLower(normalizeReleaseVersion(rel.TagName))
	var fallback *powerPilotReleaseAsset
	for i := range rel.Assets {
		a := &rel.Assets[i]
		low := strings.ToLower(a.Name)
		if low == "powerpilot_update.zip" {
			fallback = a
		}
		if strings.HasPrefix(low, "powerpilot_update_") && strings.HasSuffix(low, ".zip") {
			if ver == "" || strings.Contains(low, ver) {
				return *a, nil
			}
			if fallback == nil {
				fallback = a
			}
		}
	}
	if fallback != nil {
		return *fallback, nil
	}
	return powerPilotReleaseAsset{}, fmt.Errorf("в GitHub Release не найден PowerPilot_Update_<version>.zip")
}

func checkPowerPilotUpdatesAsync(manual bool) {
	powerPilotUpdateState.Lock()
	if powerPilotUpdateState.Checking || powerPilotUpdateState.Downloading {
		powerPilotUpdateState.Unlock()
		return
	}
	powerPilotUpdateState.Checking = true
	if manual {
		powerPilotUpdateState.LastError = ""
	}
	powerPilotUpdateState.Unlock()
	invalidate(app.hwnd)

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		defer cancel()
		rel, err := fetchLatestPowerPilotRelease(ctx)
		now := time.Now()
		powerPilotUpdateState.Lock()
		powerPilotUpdateState.Checking = false
		powerPilotUpdateState.LastCheck = now
		if err != nil {
			powerPilotUpdateState.LastError = err.Error()
			powerPilotUpdateState.Available = false
			powerPilotUpdateState.Unlock()
			invalidate(app.hwnd)
			return
		}
		latest, verr := powerPilotReleaseVersion(rel)
		if verr != nil {
			powerPilotUpdateState.LastError = verr.Error()
			powerPilotUpdateState.Available = false
			powerPilotUpdateState.Unlock()
			invalidate(app.hwnd)
			return
		}
		powerPilotUpdateState.LatestVersion = latest
		powerPilotUpdateState.ReleaseName = rel.Name
		powerPilotUpdateState.ReleaseNotes = rel.Body
		powerPilotUpdateState.ReleaseURL = rel.HTMLURL
		powerPilotUpdateState.LastError = ""
		if compareVersions(currentPowerPilotVersion(), latest) >= 0 {
			powerPilotUpdateState.Available = false
			powerPilotUpdateState.AssetURL = ""
			powerPilotUpdateState.AssetName = ""
			powerPilotUpdateState.AssetDigest = ""
			powerPilotUpdateState.AssetSize = 0
			powerPilotUpdateState.Unlock()
			invalidate(app.hwnd)
			return
		}
		asset, aerr := selectPowerPilotUpdateAsset(rel)
		if aerr != nil {
			powerPilotUpdateState.Available = false
			powerPilotUpdateState.LastError = aerr.Error()
			powerPilotUpdateState.Unlock()
			invalidate(app.hwnd)
			return
		}
		powerPilotUpdateState.Available = true
		powerPilotUpdateState.AssetURL = asset.BrowserDownloadURL
		powerPilotUpdateState.AssetName = asset.Name
		powerPilotUpdateState.AssetDigest = asset.Digest
		powerPilotUpdateState.AssetSize = asset.Size
		powerPilotUpdateState.Unlock()
		if !manual {
			pushAppNotificationUnique("powerpilot-update:"+latest, notifUpdate, "Доступно обновление PowerPilot", "Версия "+latest+" готова к установке.", notifTargetData)
		}
		invalidate(app.hwnd)
	}()
}

func maybeCheckPowerPilotUpdates() {
	powerPilotUpdateState.RLock()
	busy := powerPilotUpdateState.Checking || powerPilotUpdateState.Downloading
	last := powerPilotUpdateState.LastCheck
	powerPilotUpdateState.RUnlock()
	if busy {
		return
	}
	if last.IsZero() || time.Since(last) >= powerPilotUpdateInterval {
		checkPowerPilotUpdatesAsync(false)
	}
}

func powerPilotUpdateCard() (string, string) {
	powerPilotUpdateState.RLock()
	defer powerPilotUpdateState.RUnlock()
	if powerPilotUpdateState.Downloading {
		pct := 0
		if powerPilotUpdateState.AssetSize > 0 {
			pct = int(powerPilotUpdateState.Downloaded * 100 / powerPilotUpdateState.AssetSize)
			if pct > 100 {
				pct = 100
			}
		}
		return "Обновления PowerPilot", "Версия " + powerPilotUpdateState.LatestVersion + " · загружено " + strconv.Itoa(pct) + "% · после проверки приложение перезапустится"
	}
	if powerPilotUpdateState.Checking {
		return "Обновления PowerPilot", "Проверяется последняя версия через GitHub Releases · " + powerPilotUpdateRepo
	}
	if powerPilotUpdateState.Available {
		return "Обновления PowerPilot", "Доступна версия " + powerPilotUpdateState.LatestVersion + " · установлена " + currentPowerPilotVersion()
	}
	if powerPilotUpdateState.LastError != "" {
		return "Обновления PowerPilot", "Не удалось проверить: " + powerPilotUpdateState.LastError
	}
	if !powerPilotUpdateState.LastCheck.IsZero() {
		return "Обновления PowerPilot", "Установлена последняя версия " + currentPowerPilotVersion() + " · проверено " + powerPilotUpdateState.LastCheck.Format("15:04")
	}
	return "Обновления PowerPilot", "Автопроверка при запуске и каждые 30 минут через GitHub Releases"
}

func powerPilotUpdateActionLabel() (string, bool) {
	powerPilotUpdateState.RLock()
	defer powerPilotUpdateState.RUnlock()
	if powerPilotUpdateState.Downloading {
		return "Установка…", true
	}
	if powerPilotUpdateState.Checking {
		return "Проверка…", true
	}
	if powerPilotUpdateState.Available {
		return "Установить", false
	}
	return "Проверить обновление", false
}

func handlePowerPilotUpdateAction() {
	powerPilotUpdateState.RLock()
	available := powerPilotUpdateState.Available
	busy := powerPilotUpdateState.Checking || powerPilotUpdateState.Downloading
	powerPilotUpdateState.RUnlock()
	if busy {
		return
	}
	if available {
		downloadAndApplyPowerPilotUpdateAsync()
	} else {
		checkPowerPilotUpdatesAsync(true)
	}
}

func downloadAndApplyPowerPilotUpdateAsync() {
	powerPilotUpdateState.Lock()
	if powerPilotUpdateState.Downloading || !powerPilotUpdateState.Available || powerPilotUpdateState.AssetURL == "" {
		powerPilotUpdateState.Unlock()
		return
	}
	url := powerPilotUpdateState.AssetURL
	name := powerPilotUpdateState.AssetName
	digest := strings.TrimSpace(powerPilotUpdateState.AssetDigest)
	expectedSize := powerPilotUpdateState.AssetSize
	latest := powerPilotUpdateState.LatestVersion
	powerPilotUpdateState.Downloading = true
	powerPilotUpdateState.Downloaded = 0
	powerPilotUpdateState.LastError = ""
	powerPilotUpdateState.Unlock()
	invalidate(app.hwnd)

	go func() {
		fail := func(err error) {
			powerPilotUpdateState.Lock()
			powerPilotUpdateState.Downloading = false
			powerPilotUpdateState.LastError = err.Error()
			powerPilotUpdateState.Unlock()
			techLog040("PowerPilot update failed: " + err.Error())
			invalidate(app.hwnd)
		}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			fail(err)
			return
		}
		req.Header.Set("User-Agent", "PowerPilot/"+currentPowerPilotVersion())
		resp, err := (&http.Client{Timeout: 10 * time.Minute}).Do(req)
		if err != nil {
			fail(err)
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			fail(fmt.Errorf("скачивание обновления: GitHub HTTP %d", resp.StatusCode))
			return
		}

		dir := filepath.Join(os.TempDir(), "PowerPilot-update", latest)
		if err := os.MkdirAll(dir, 0755); err != nil {
			fail(err)
			return
		}
		if strings.TrimSpace(name) == "" {
			name = "PowerPilot_Update_" + latest + ".zip"
		}
		finalPath := filepath.Join(dir, filepath.Base(name))
		partial := finalPath + ".partial"
		_ = os.Remove(partial)
		f, err := os.Create(partial)
		if err != nil {
			fail(err)
			return
		}
		h := sha256.New()
		buf := make([]byte, 256*1024)
		var nTotal int64
		for {
			n, rerr := resp.Body.Read(buf)
			if n > 0 {
				if _, err := f.Write(buf[:n]); err != nil {
					_ = f.Close()
					_ = os.Remove(partial)
					fail(err)
					return
				}
				_, _ = h.Write(buf[:n])
				nTotal += int64(n)
				powerPilotUpdateState.Lock()
				powerPilotUpdateState.Downloaded = nTotal
				powerPilotUpdateState.Unlock()
				invalidate(app.hwnd)
			}
			if rerr == io.EOF {
				break
			}
			if rerr != nil {
				_ = f.Close()
				_ = os.Remove(partial)
				fail(rerr)
				return
			}
		}
		if err := f.Close(); err != nil {
			_ = os.Remove(partial)
			fail(err)
			return
		}
		if expectedSize > 0 && nTotal != expectedSize {
			_ = os.Remove(partial)
			fail(fmt.Errorf("размер обновления не совпал: получено %d, ожидалось %d", nTotal, expectedSize))
			return
		}
		if digest == "" || !strings.HasPrefix(strings.ToLower(digest), "sha256:") {
			_ = os.Remove(partial)
			fail(fmt.Errorf("GitHub не предоставил SHA-256 digest для пакета; автообновление отменено"))
			return
		}
		expected := strings.TrimSpace(strings.TrimPrefix(strings.ToLower(digest), "sha256:"))
		got := hex.EncodeToString(h.Sum(nil))
		if !strings.EqualFold(expected, got) {
			_ = os.Remove(partial)
			fail(fmt.Errorf("SHA-256 пакета обновления не совпал"))
			return
		}
		_ = os.Remove(finalPath)
		if err := os.Rename(partial, finalPath); err != nil {
			fail(err)
			return
		}

		updaterPath, err := extractPowerPilotUpdater(finalPath, dir)
		if err != nil {
			fail(err)
			return
		}
		appPath, err := os.Executable()
		if err != nil {
			fail(err)
			return
		}
		appPath, _ = filepath.Abs(appPath)
		args := []string{
			"--package", finalPath,
			"--app", appPath,
			"--pid", strconv.Itoa(os.Getpid()),
			"--version", latest,
			"--old-version", currentPowerPilotVersion(),
		}
		cmd := exec.Command(updaterPath, args...)
		cmd.Dir = dir
		if err := cmd.Start(); err != nil {
			if !errors.Is(err, syscall.Errno(740)) {
				fail(fmt.Errorf("не удалось запустить модуль обновления: %v", err))
				return
			}
			if err := launchUpdaterElevated(updaterPath, dir, args); err != nil {
				fail(fmt.Errorf("не удалось запустить модуль обновления через UAC: %v", err))
				return
			}
			techLog040("PowerPilot updater requested elevation and was launched through UAC")
		} else {
			techLog040("PowerPilot updater launched without elevation")
		}
		powerPilotUpdateState.Lock()
		powerPilotUpdateState.Downloading = false
		powerPilotUpdateState.PackagePath = finalPath
		powerPilotUpdateState.Unlock()
		invalidate(app.hwnd)
		time.Sleep(220 * time.Millisecond)
		app.exiting = true
		pSendMessageW.Call(app.hwnd, WM_CLOSE, 0, 0)
	}()
}

func launchUpdaterElevated(updaterPath, dir string, args []string) error {
	verb, _ := syscall.UTF16PtrFromString("runas")
	file, _ := syscall.UTF16PtrFromString(updaterPath)
	params, _ := syscall.UTF16PtrFromString(strings.Join(escapeWindowsArgs(args), " "))
	workDir, _ := syscall.UTF16PtrFromString(dir)
	shellExecute := shell32.NewProc("ShellExecuteW")
	r, _, callErr := shellExecute.Call(0, uintptr(unsafe.Pointer(verb)), uintptr(unsafe.Pointer(file)), uintptr(unsafe.Pointer(params)), uintptr(unsafe.Pointer(workDir)), SW_HIDE)
	if r <= 32 {
		return fmt.Errorf("ShellExecuteW code %d: %v", r, callErr)
	}
	return nil
}

func escapeWindowsArgs(args []string) []string {
	out := make([]string, len(args))
	for i, arg := range args {
		out[i] = syscall.EscapeArg(arg)
	}
	return out
}

func extractPowerPilotUpdater(packagePath, dir string) (string, error) {
	zr, err := zip.OpenReader(packagePath)
	if err != nil {
		return "", err
	}
	defer zr.Close()
	for _, f := range zr.File {
		if !strings.EqualFold(filepath.Base(f.Name), "PowerPilot.Update.exe") {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return "", err
		}
		defer rc.Close()
		path := filepath.Join(dir, "PowerPilot.Update.exe")
		partial := path + ".partial"
		out, err := os.OpenFile(partial, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0755)
		if err != nil {
			return "", err
		}
		_, copyErr := io.Copy(out, rc)
		closeErr := out.Close()
		if copyErr != nil {
			_ = os.Remove(partial)
			return "", copyErr
		}
		if closeErr != nil {
			_ = os.Remove(partial)
			return "", closeErr
		}
		_ = os.Remove(path)
		if err := os.Rename(partial, path); err != nil {
			return "", err
		}
		return path, nil
	}
	return "", fmt.Errorf("в пакете обновления отсутствует PowerPilot.Update.exe")
}
