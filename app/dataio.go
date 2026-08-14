//go:build windows

package main

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"
)

var (
	comdlg32              = syscall.NewLazyDLL("comdlg32.dll")
	pGetOpenFileNameW     = comdlg32.NewProc("GetOpenFileNameW")
	pGetSaveFileNameW     = comdlg32.NewProc("GetSaveFileNameW")
	shell32DataIO         = syscall.NewLazyDLL("shell32.dll")
	pSHBrowseForFolderW   = shell32DataIO.NewProc("SHBrowseForFolderW")
	pSHGetPathFromIDListW = shell32DataIO.NewProc("SHGetPathFromIDListW")
	ole32DataIO           = syscall.NewLazyDLL("ole32.dll")
	pCoTaskMemFreeDataIO  = ole32DataIO.NewProc("CoTaskMemFree")
)

const (
	ofnExplorer         = 0x00080000
	ofnPathMustExist    = 0x00000800
	ofnFileMustExist    = 0x00001000
	ofnOverwritePrompt  = 0x00000002
	bifReturnOnlyFSDirs = 0x00000001
	bifNewDialogStyle   = 0x00000040
)

type OPENFILENAME struct {
	LStructSize       uint32
	HwndOwner         uintptr
	HInstance         uintptr
	LpstrFilter       *uint16
	LpstrCustomFilter *uint16
	NMaxCustFilter    uint32
	NFilterIndex      uint32
	LpstrFile         *uint16
	NMaxFile          uint32
	LpstrFileTitle    *uint16
	NMaxFileTitle     uint32
	LpstrInitialDir   *uint16
	LpstrTitle        *uint16
	Flags             uint32
	NFileOffset       uint16
	NFileExtension    uint16
	LpstrDefExt       *uint16
	LCustData         uintptr
	LpfnHook          uintptr
	LpTemplateName    *uint16
	PvReserved        uintptr
	DwReserved        uint32
	FlagsEx           uint32
}

type BROWSEINFO struct {
	HwndOwner      uintptr
	PidlRoot       uintptr
	PszDisplayName *uint16
	LpszTitle      *uint16
	UlFlags        uint32
	Lpfn           uintptr
	LParam         uintptr
	IImage         int32
}

type taskExportFile struct {
	FormatVersion int         `json:"format_version"`
	AppVersion    string      `json:"app_version"`
	ExportedAt    string      `json:"exported_at"`
	Tasks         []SavedTask `json:"tasks"`
}

func utf16Multi(parts ...string) []uint16 {
	var out []uint16
	for _, p := range parts {
		out = append(out, utf16.Encode([]rune(p))...)
		out = append(out, 0)
	}
	out = append(out, 0)
	return out
}

func chooseSaveFile(title, defName, defExt string, filters []string) string {
	buf := make([]uint16, 4096)
	copy(buf, utf16.Encode([]rune(defName)))
	filter := utf16Multi(filters...)
	t, _ := syscall.UTF16PtrFromString(title)
	de, _ := syscall.UTF16PtrFromString(defExt)
	ofn := OPENFILENAME{
		LStructSize: uint32(unsafe.Sizeof(OPENFILENAME{})), HwndOwner: app.hwnd,
		LpstrFilter: &filter[0], NFilterIndex: 1, LpstrFile: &buf[0], NMaxFile: uint32(len(buf)),
		LpstrTitle: t, Flags: ofnExplorer | ofnOverwritePrompt | ofnPathMustExist, LpstrDefExt: de,
	}
	r, _, _ := pGetSaveFileNameW.Call(uintptr(unsafe.Pointer(&ofn)))
	if r == 0 {
		return ""
	}
	return syscall.UTF16ToString(buf)
}

func chooseOpenFile(title string, filters []string) string {
	buf := make([]uint16, 4096)
	filter := utf16Multi(filters...)
	t, _ := syscall.UTF16PtrFromString(title)
	ofn := OPENFILENAME{
		LStructSize: uint32(unsafe.Sizeof(OPENFILENAME{})), HwndOwner: app.hwnd,
		LpstrFilter: &filter[0], NFilterIndex: 1, LpstrFile: &buf[0], NMaxFile: uint32(len(buf)),
		LpstrTitle: t, Flags: ofnExplorer | ofnFileMustExist | ofnPathMustExist,
	}
	r, _, _ := pGetOpenFileNameW.Call(uintptr(unsafe.Pointer(&ofn)))
	if r == 0 {
		return ""
	}
	return syscall.UTF16ToString(buf)
}

func chooseFolder(title string) string {
	display := make([]uint16, 260)
	t, _ := syscall.UTF16PtrFromString(title)
	bi := BROWSEINFO{
		HwndOwner: app.hwnd, PszDisplayName: &display[0], LpszTitle: t,
		UlFlags: bifReturnOnlyFSDirs | bifNewDialogStyle,
	}
	pidl, _, _ := pSHBrowseForFolderW.Call(uintptr(unsafe.Pointer(&bi)))
	if pidl == 0 {
		return ""
	}
	defer pCoTaskMemFreeDataIO.Call(pidl)
	path := make([]uint16, 32768)
	ok, _, _ := pSHGetPathFromIDListW.Call(pidl, uintptr(unsafe.Pointer(&path[0])))
	if ok == 0 {
		return ""
	}
	return syscall.UTF16ToString(path)
}

func exportTasks() {
	if len(app.settings.SavedTasks) == 0 {
		showNotification("PowerPilot", "Нет сохранённых задач для экспорта.")
		return
	}
	name := "PowerPilot_Tasks_" + time.Now().Format("20060102_150405") + ".pptasks"
	p := chooseSaveFile("Экспорт задач PowerPilot", name, "pptasks", []string{"PowerPilot Tasks (*.pptasks)", "*.pptasks", "JSON (*.json)", "*.json", "Все файлы (*.*)", "*.*"})
	if p == "" {
		return
	}
	tasks := append([]SavedTask(nil), app.settings.SavedTasks...)
	for i := range tasks {
		tasks[i].LastRunKey = "" // Runtime scheduling state is local to this PC.
	}
	doc := taskExportFile{FormatVersion: 1, AppVersion: appVersion, ExportedAt: time.Now().Format(time.RFC3339), Tasks: tasks}
	b, err := json.MarshalIndent(doc, "", "  ")
	if err != nil || os.WriteFile(p, b, 0644) != nil {
		appendHistory("ERROR", "Ошибка экспорта задач")
		showNotification("PowerPilot", "Не удалось экспортировать задачи.")
		return
	}
	appendHistory("EXPORT", fmt.Sprintf("Экспортировано задач: %d", len(doc.Tasks)))
	showNotification("PowerPilot", "Задачи экспортированы: "+filepath.Base(p))
	pushAppNotification(notifSuccess, "Задачи экспортированы", filepath.Base(p), notifTargetData)
}

func importTasks() {
	p := chooseOpenFile("Импорт задач PowerPilot", []string{"PowerPilot Tasks (*.pptasks;*.json)", "*.pptasks;*.json", "Все файлы (*.*)", "*.*"})
	if p == "" {
		return
	}
	b, err := os.ReadFile(p)
	if err != nil {
		showNotification("PowerPilot", "Не удалось прочитать файл импорта.")
		return
	}
	var doc taskExportFile
	if err = json.Unmarshal(b, &doc); err != nil {
		// Also accept a raw task array for convenience.
		var raw []SavedTask
		if e2 := json.Unmarshal(b, &raw); e2 != nil {
			showNotification("PowerPilot", "Файл не похож на экспорт PowerPilot.")
			return
		}
		doc.Tasks = raw
	}
	existing := map[string]bool{}
	for _, t := range app.settings.SavedTasks {
		existing[t.ID] = true
	}
	imported := 0
	for _, t := range doc.Tasks {
		if strings.TrimSpace(t.Name) == "" {
			continue
		}
		if t.ID == "" || existing[t.ID] {
			t.ID = newAutomationID("task")
		}
		normalizeSavedTask(&t)
		app.settings.SavedTasks = append(app.settings.SavedTasks, t)
		existing[t.ID] = true
		imported++
	}
	saveSettings()
	maintainWakeTimer(time.Now())
	appendHistory("IMPORT", fmt.Sprintf("Импортировано задач: %d", imported))
	showNotification("PowerPilot", fmt.Sprintf("Импортировано задач: %d", imported))
	pushAppNotification(notifSuccess, "Импорт задач завершён", fmt.Sprintf("Импортировано задач: %d", imported), notifTargetData)
	invalidate(app.hwnd)
}

func createBackup() {
	name := "PowerPilot_Backup_" + time.Now().Format("20060102_150405") + ".ppbackup"
	p := chooseSaveFile("Резервная копия PowerPilot", name, "ppbackup", []string{"PowerPilot Backup (*.ppbackup)", "*.ppbackup", "ZIP (*.zip)", "*.zip"})
	if p == "" {
		return
	}
	f, err := os.Create(p)
	if err != nil {
		showNotification("PowerPilot", "Не удалось создать резервную копию.")
		return
	}
	zw := zip.NewWriter(f)
	add := func(name, path string) {
		b, e := os.ReadFile(path)
		if e != nil {
			return
		}
		w, e := zw.Create(name)
		if e == nil {
			_, _ = w.Write(b)
		}
	}
	saveSettings()
	add("settings.json", settingsPath())
	add("history.log", historyPath())
	add("notifications.json", notificationsPath())
	flushResourceStats(true)
	add("resource_stats.json", resourceStatsPath())
	add("resource_app_history.json", resourceAppHistoryPath())
	_ = zw.Close()
	_ = f.Close()
	appendHistory("BACKUP", filepath.Base(p))
	showNotification("PowerPilot", "Резервная копия создана.")
	pushAppNotification(notifSuccess, "Резервная копия создана", filepath.Base(p), notifTargetData)
}

func restoreBackup() {
	p := chooseOpenFile("Восстановить PowerPilot", []string{"PowerPilot Backup (*.ppbackup;*.zip)", "*.ppbackup;*.zip", "Все файлы (*.*)", "*.*"})
	if p == "" {
		return
	}
	zr, err := zip.OpenReader(p)
	if err != nil {
		showNotification("PowerPilot", "Не удалось открыть резервную копию.")
		return
	}
	defer zr.Close()
	_ = os.MkdirAll(settingsDir(), 0755)
	restoredSettings := false
	for _, zf := range zr.File {
		if zf.Name != "settings.json" && zf.Name != "history.log" && zf.Name != "notifications.json" && zf.Name != "resource_stats.json" && zf.Name != "resource_app_history.json" {
			continue
		}
		r, e := zf.Open()
		if e != nil {
			continue
		}
		b, e := io.ReadAll(r)
		_ = r.Close()
		if e != nil {
			continue
		}
		target := settingsPath()
		if zf.Name == "history.log" {
			target = historyPath()
		} else if zf.Name == "notifications.json" {
			target = notificationsPath()
		} else if zf.Name == "resource_stats.json" {
			target = resourceStatsPath()
		} else if zf.Name == "resource_app_history.json" {
			target = resourceAppHistoryPath()
		}
		if os.WriteFile(target, b, 0644) == nil && zf.Name == "settings.json" {
			restoredSettings = true
		}
	}
	if restoredSettings {
		app.schedule = Schedule{}
		app.status = "Настройки восстановлены"
		app.countdown = "00:00:00"
		app.progress = 0
		app.settings = loadSettings()
		normalizeSettings()
		app.selectedAction, app.selectedMode = app.settings.Action, app.settings.Mode
		applyTheme()
		syncControlsFromSettings()
		maintainWakeTimer(time.Now())
	}
	reloadResourceStats()
	loadHistoryItems()
	loadNotificationCenter()
	appendHistory("RESTORE", filepath.Base(p))
	showNotification("PowerPilot", "Резервная копия восстановлена.")
	pushAppNotification(notifSuccess, "Резервная копия восстановлена", filepath.Base(p), notifTargetData)
	layoutControls(app.hwnd)
	invalidate(app.hwnd)
}

func syncControlsFromSettings() {
	setEdit := func(id int, s string) {
		if h := app.edits[id]; h != 0 {
			pSetWindowTextW.Call(h, uintptr(unsafe.Pointer(wstr(s))))
		}
	}
	setEdit(idDelayHours, fmt.Sprint(app.settings.DelayHours))
	setEdit(idDelayMinutes, fmt.Sprint(app.settings.DelayMinutes))
	setEdit(idDelaySeconds, fmt.Sprint(app.settings.DelaySeconds))
	setEdit(idExact, app.settings.Exact)
	setExactFields(app.settings.Exact)
	setEdit(idIdleMinutes, fmt.Sprint(max(app.settings.IdleMinutes, 1)))
	setEdit(idWatchProcess, app.settings.WatchProcess)
	setEdit(idWarning, fmt.Sprint(max(app.settings.WarningSeconds, 0)))
	setEdit(idScheduleTime, app.settings.Recurrence.TimeHHMM)
	setEdit(idSafetyIdle, fmt.Sprint(max(app.settings.SafetyIdleMinutes, 1)))
	setEdit(idSoundVolume, fmt.Sprint(clampInt(app.settings.SoundVolume, 0, 100)))
	setEdit(idWakeLead, fmt.Sprint(clampInt(app.settings.WakeLeadMinutes, 0, 60)))
}
