//go:build windows

package main

import (
	_ "embed"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"unsafe"
)

//go:embed PowerPilot.ico
var iconData []byte

var (
	user32              = syscall.NewLazyDLL("user32.dll")
	pMessageBoxW        = user32.NewProc("MessageBoxW")
	pSetProcessDPIAware = user32.NewProc("SetProcessDPIAware")
)

const (
	MB_YESNO           = 0x4
	MB_ICONQUESTION    = 0x20
	MB_ICONINFORMATION = 0x40
	IDYES              = 6
)

func w(s string) *uint16 { p, _ := syscall.UTF16PtrFromString(s); return p }
func msg(title, text string, flags uintptr) int {
	r, _, _ := pMessageBoxW.Call(0, uintptr(unsafe.Pointer(w(text))), uintptr(unsafe.Pointer(w(title))), flags)
	return int(r)
}
func main() {
	runtime.LockOSThread()
	pSetProcessDPIAware.Call()
	if msg("PowerPilot — удаление", "Удалить PowerPilot с этого компьютера?", MB_YESNO|MB_ICONQUESTION) != IDYES {
		return
	}
	exe, _ := os.Executable()
	dir := filepath.Dir(exe)
	_ = exec.Command("taskkill.exe", "/F", "/IM", "PowerPilot.exe").Run()
	_ = exec.Command("schtasks.exe", "/End", "/TN", "PowerPilot Hardware Sensors").Run()
	_ = exec.Command("schtasks.exe", "/Delete", "/TN", "PowerPilot Hardware Sensors", "/F").Run()
	removeShortcut(filepath.Join(os.Getenv("USERPROFILE"), "Desktop", "PowerPilot.lnk"))
	if appdata := os.Getenv("APPDATA"); appdata != "" {
		removeShortcut(filepath.Join(appdata, "Microsoft", "Windows", "Start Menu", "Programs", "PowerPilot.lnk"))
	}
	_ = exec.Command("reg.exe", "delete", `HKCU\Software\Microsoft\Windows\CurrentVersion\Uninstall\PowerPilot`, "/f").Run()
	_ = exec.Command("reg.exe", "delete", `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`, "/v", "PowerPilot", "/f").Run()
	if msg("PowerPilot — удаление", "Удалить также настройки, историю и установленные датчики PowerPilot?", MB_YESNO|MB_ICONQUESTION) == IDYES {
		if cfg, err := os.UserConfigDir(); err == nil {
			_ = os.RemoveAll(filepath.Join(cfg, "PowerPilot"))
		}
		if local := os.Getenv("LOCALAPPDATA"); local != "" {
			_ = os.RemoveAll(filepath.Join(local, "PowerPilot"))
		}
	}
	cmd := exec.Command("cmd.exe", "/C", `ping 127.0.0.1 -n 3 >nul & rmdir /s /q "`+strings.ReplaceAll(dir, `"`, `""`)+`"`)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	_ = cmd.Start()
	msg("PowerPilot", "PowerPilot удалён.", MB_ICONINFORMATION)
}
func removeShortcut(p string) { _ = os.Remove(p) }
