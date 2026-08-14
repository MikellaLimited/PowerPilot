//go:build windows

package main

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

const (
	processQueryLimitedInformation = 0x1000
	synchronize                    = 0x00100000
	waitTimeout                    = 0x00000102
	swHide                         = 0
)

var (
	kernel32             = syscall.NewLazyDLL("kernel32.dll")
	shell32              = syscall.NewLazyDLL("shell32.dll")
	pOpenProcess         = kernel32.NewProc("OpenProcess")
	pWaitForSingleObject = kernel32.NewProc("WaitForSingleObject")
	pCloseHandle         = kernel32.NewProc("CloseHandle")
	pShellExecuteW       = shell32.NewProc("ShellExecuteW")
)

type updateFile struct {
	Source string `json:"source"`
	Role   string `json:"role"`
	SHA256 string `json:"sha256"`
}

type updateManifest struct {
	Version string       `json:"version"`
	Files   []updateFile `json:"files"`
}

type options struct {
	Package    string
	AppPath    string
	PID        uint32
	Version    string
	OldVersion string
	Elevated   bool
}

func main() {
	opt, err := parseArgs(os.Args[1:])
	if err != nil {
		writeResult(false, "args", err.Error())
		return
	}
	if err := run(opt); err != nil {
		writeResult(false, "update", err.Error())
		return
	}
	writeResult(true, "complete", "ok")
}

func parseArgs(args []string) (options, error) {
	var o options
	for i := 0; i < len(args); i++ {
		next := func() (string, error) {
			if i+1 >= len(args) {
				return "", errors.New("missing argument value")
			}
			i++
			return args[i], nil
		}
		switch args[i] {
		case "--package":
			v, e := next()
			if e != nil {
				return o, e
			}
			o.Package = v
		case "--app":
			v, e := next()
			if e != nil {
				return o, e
			}
			o.AppPath = v
		case "--pid":
			v, e := next()
			if e != nil {
				return o, e
			}
			n, e := strconv.ParseUint(v, 10, 32)
			if e != nil {
				return o, e
			}
			o.PID = uint32(n)
		case "--version":
			v, e := next()
			if e != nil {
				return o, e
			}
			o.Version = v
		case "--old-version":
			v, e := next()
			if e != nil {
				return o, e
			}
			o.OldVersion = v
		case "--elevated":
			o.Elevated = true
		}
	}
	if strings.TrimSpace(o.Package) == "" || strings.TrimSpace(o.AppPath) == "" || o.PID == 0 {
		return o, errors.New("package, app and pid are required")
	}
	ap, err := filepath.Abs(o.AppPath)
	if err != nil {
		return o, err
	}
	o.AppPath = ap
	pp, err := filepath.Abs(o.Package)
	if err != nil {
		return o, err
	}
	o.Package = pp
	return o, nil
}

func run(o options) error {
	targetDir := filepath.Dir(o.AppPath)
	if !o.Elevated && !canWriteDir(targetDir) {
		return relaunchElevated(o)
	}
	if err := waitForPID(o.PID, 45*time.Second); err != nil {
		return err
	}

	manifest, stagedApp, err := preparePackage(o.Package)
	if err != nil {
		return err
	}
	if o.Version != "" && manifest.Version != "" && normalizeVersion(manifest.Version) != normalizeVersion(o.Version) {
		return fmt.Errorf("package version %s does not match expected %s", manifest.Version, o.Version)
	}

	backupDir := filepath.Join(targetDir, ".powerpilot-update-backup")
	_ = os.RemoveAll(backupDir)
	if err := os.MkdirAll(backupDir, 0755); err != nil {
		return err
	}
	backupApp := filepath.Join(backupDir, filepath.Base(o.AppPath))
	if err := copyFile(o.AppPath, backupApp); err != nil {
		return fmt.Errorf("backup: %w", err)
	}

	incoming := o.AppPath + ".new"
	oldPath := o.AppPath + ".old"
	_ = os.Remove(incoming)
	_ = os.Remove(oldPath)
	if err := copyFile(stagedApp, incoming); err != nil {
		return fmt.Errorf("stage target: %w", err)
	}
	if err := os.Rename(o.AppPath, oldPath); err != nil {
		_ = os.Remove(incoming)
		return fmt.Errorf("move current PowerPilot.exe aside: %w", err)
	}
	if err := os.Rename(incoming, o.AppPath); err != nil {
		_ = os.Rename(oldPath, o.AppPath)
		_ = os.Remove(incoming)
		return fmt.Errorf("activate new PowerPilot.exe: %w", err)
	}
	updateDisplayVersion(manifest.Version)

	cmd := exec.Command(o.AppPath)
	cmd.Dir = targetDir
	if err := cmd.Start(); err != nil {
		_ = copyFile(backupApp, o.AppPath)
		return fmt.Errorf("restart: %w", err)
	}

	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	select {
	case err := <-exited:
		// A fresh build that exits almost immediately is treated as a failed update.
		_ = copyFile(backupApp, o.AppPath)
		updateDisplayVersion(o.OldVersion)
		old := exec.Command(o.AppPath)
		old.Dir = targetDir
		_ = old.Start()
		if err == nil {
			err = errors.New("new PowerPilot exited during startup")
		}
		return fmt.Errorf("new version failed startup check: %w", err)
	case <-time.After(6 * time.Second):
		_ = os.Remove(oldPath)
		_ = os.RemoveAll(backupDir)
		_ = os.Remove(o.Package)
		return nil
	}
}

func preparePackage(path string) (updateManifest, string, error) {
	var m updateManifest
	zr, err := zip.OpenReader(path)
	if err != nil {
		return m, "", err
	}
	defer zr.Close()
	var manifestBytes []byte
	var appEntry *zip.File
	for _, f := range zr.File {
		clean := filepath.ToSlash(filepath.Clean(f.Name))
		if strings.HasPrefix(clean, "../") || filepath.IsAbs(f.Name) {
			return m, "", errors.New("unsafe update package path")
		}
		if strings.EqualFold(clean, "update_manifest.json") {
			rc, e := f.Open()
			if e != nil {
				return m, "", e
			}
			manifestBytes, e = io.ReadAll(io.LimitReader(rc, 1<<20))
			rc.Close()
			if e != nil {
				return m, "", e
			}
		}
	}
	if len(manifestBytes) == 0 {
		return m, "", errors.New("update_manifest.json missing")
	}
	if err := json.Unmarshal(manifestBytes, &m); err != nil {
		return m, "", err
	}
	var appFile updateFile
	found := false
	for _, f := range m.Files {
		if strings.EqualFold(f.Role, "app") {
			appFile = f
			found = true
			break
		}
	}
	if !found || strings.TrimSpace(appFile.Source) == "" {
		return m, "", errors.New("app entry missing from manifest")
	}
	for _, f := range zr.File {
		if strings.EqualFold(filepath.ToSlash(filepath.Clean(f.Name)), filepath.ToSlash(filepath.Clean(appFile.Source))) {
			appEntry = f
			break
		}
	}
	if appEntry == nil {
		return m, "", errors.New("PowerPilot payload missing")
	}
	dir, err := os.MkdirTemp("", "PowerPilot-update-stage-")
	if err != nil {
		return m, "", err
	}
	dst := filepath.Join(dir, "PowerPilot.exe")
	rc, err := appEntry.Open()
	if err != nil {
		return m, "", err
	}
	out, err := os.Create(dst)
	if err != nil {
		rc.Close()
		return m, "", err
	}
	h := sha256.New()
	_, copyErr := io.Copy(io.MultiWriter(out, h), rc)
	closeErr := out.Close()
	rc.Close()
	if copyErr != nil {
		return m, "", copyErr
	}
	if closeErr != nil {
		return m, "", closeErr
	}
	expected := strings.TrimSpace(strings.TrimPrefix(strings.ToLower(appFile.SHA256), "sha256:"))
	got := hex.EncodeToString(h.Sum(nil))
	if expected == "" || !strings.EqualFold(expected, got) {
		return m, "", errors.New("inner PowerPilot.exe SHA-256 mismatch")
	}
	return m, dst, nil
}

func canWriteDir(dir string) bool {
	f, err := os.CreateTemp(dir, ".powerpilot-write-test-")
	if err != nil {
		return false
	}
	name := f.Name()
	_ = f.Close()
	_ = os.Remove(name)
	return true
}

func relaunchElevated(o options) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	args := []string{"--package", o.Package, "--app", o.AppPath, "--pid", strconv.FormatUint(uint64(o.PID), 10), "--version", o.Version, "--old-version", o.OldVersion, "--elevated"}
	params := quoteArgs(args)
	verb, _ := syscall.UTF16PtrFromString("runas")
	file, _ := syscall.UTF16PtrFromString(exe)
	par, _ := syscall.UTF16PtrFromString(params)
	dir, _ := syscall.UTF16PtrFromString(filepath.Dir(exe))
	r, _, e := pShellExecuteW.Call(0, uintptr(unsafe.Pointer(verb)), uintptr(unsafe.Pointer(file)), uintptr(unsafe.Pointer(par)), uintptr(unsafe.Pointer(dir)), swHide)
	if r <= 32 {
		return fmt.Errorf("UAC launch failed: %v", e)
	}
	return nil
}

func waitForPID(pid uint32, timeout time.Duration) error {
	h, _, e := pOpenProcess.Call(processQueryLimitedInformation|synchronize, 0, uintptr(pid))
	if h == 0 {
		return nil
	}
	defer pCloseHandle.Call(h)
	ms := uint32(timeout / time.Millisecond)
	r, _, _ := pWaitForSingleObject.Call(h, uintptr(ms))
	if r == waitTimeout {
		return errors.New("PowerPilot did not exit before update timeout")
	}
	if r != 0 {
		_ = e
	}
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := dst + ".tmp"
	_ = os.Remove(tmp)
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0755)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, in)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(tmp)
		return err
	}
	_ = os.Remove(dst)
	return os.Rename(tmp, dst)
}

func updateDisplayVersion(v string) {
	v = normalizeVersion(v)
	if v == "" {
		return
	}
	key := `HKCU\Software\Microsoft\Windows\CurrentVersion\Uninstall\PowerPilot`
	c := exec.Command("reg.exe", "add", key, "/v", "DisplayVersion", "/t", "REG_SZ", "/d", v, "/f")
	c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	_ = c.Run()
}

func normalizeVersion(v string) string {
	return strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(v), "v"), "V")
}

func quoteArgs(args []string) string {
	q := make([]string, 0, len(args))
	for _, a := range args {
		q = append(q, strconv.Quote(a))
	}
	return strings.Join(q, " ")
}

func writeResult(ok bool, stage, message string) {
	dir := filepath.Join(os.Getenv("LOCALAPPDATA"), "PowerPilot")
	_ = os.MkdirAll(dir, 0755)
	b, _ := json.MarshalIndent(map[string]any{"ok": ok, "stage": stage, "message": message, "time": time.Now().Format(time.RFC3339)}, "", "  ")
	_ = os.WriteFile(filepath.Join(dir, "update_result.json"), b, 0644)
}
