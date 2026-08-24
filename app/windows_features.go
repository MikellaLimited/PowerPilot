//go:build windows

package main

import (
	"fmt"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

var (
	wfAdvapi32           = syscall.NewLazyDLL("advapi32.dll")
	pRegOpenKeyExW       = wfAdvapi32.NewProc("RegOpenKeyExW")
	pRegQueryValueExW    = wfAdvapi32.NewProc("RegQueryValueExW")
	pRegCloseKey         = wfAdvapi32.NewProc("RegCloseKey")
	pGetForegroundWindow = user32.NewProc("GetForegroundWindow")
)

const (
	hkeyCurrentUser = 0xffffffff80000001
	keyRead         = 0x20019
	regDWORD        = 4
	nifInfo         = 0x00000010
	niifInfo        = 0x00000001
	niifWarning     = 0x00000002
	niifNoSound     = 0x00000010
)

func systemUsesLightTheme() bool {
	sub, _ := syscall.UTF16PtrFromString(`Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`)
	var key uintptr
	r, _, _ := pRegOpenKeyExW.Call(hkeyCurrentUser, uintptr(unsafe.Pointer(sub)), 0, keyRead, uintptr(unsafe.Pointer(&key)))
	if r != 0 || key == 0 {
		return false
	}
	defer pRegCloseKey.Call(key)
	name, _ := syscall.UTF16PtrFromString("AppsUseLightTheme")
	var typ uint32
	var value uint32
	size := uint32(4)
	r, _, _ = pRegQueryValueExW.Call(key, uintptr(unsafe.Pointer(name)), 0, uintptr(unsafe.Pointer(&typ)), uintptr(unsafe.Pointer(&value)), uintptr(unsafe.Pointer(&size)))
	return r == 0 && typ == regDWORD && value != 0
}

func showNotification(title, text string) {
	// Legacy UI feedback must stay inside PowerPilot. Windows balloons are
	// intentionally reserved for explicit notification actions and the optional
	// one-minute task warning.
}

func showWindowsNotification(title, text string) {
	if !app.settings.Notifications || app.hwnd == 0 {
		return
	}
	icon := app.appIcon
	if icon == 0 {
		icon, _, _ = pLoadIconW.Call(0, IDI_APPLICATION)
	}
	flags := uint32(niifInfo)
	if !app.settings.NotificationSounds {
		flags |= niifNoSound
	}
	ni := NOTIFYICONDATA{CbSize: uint32(unsafe.Sizeof(NOTIFYICONDATA{})), HWnd: app.hwnd, UID: 1,
		UFlags: NIF_MESSAGE | NIF_ICON | NIF_TIP | nifInfo, UCallbackMessage: WM_TRAY, HIcon: icon, DwInfoFlags: flags}
	copy(ni.SzTip[:], syscall.StringToUTF16("PowerPilot"))
	copy(ni.SzInfoTitle[:], syscall.StringToUTF16(truncateUTF16(title, 63)))
	copy(ni.SzInfo[:], syscall.StringToUTF16(truncateUTF16(text, 255)))
	pShellNotifyIconW.Call(NIM_MODIFY, uintptr(unsafe.Pointer(&ni)))
}

func truncateUTF16(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

func checkSafetyRules() (bool, string) {
	if app.settings.SafetyRecentInput {
		idle := getIdleDuration()
		need := timeDurationMinutes(max(app.settings.SafetyIdleMinutes, 1))
		if idle < need {
			return false, fmt.Sprintf("пользователь активен (простой %s)", formatDuration(idle))
		}
	}
	if app.settings.SafetyFullscreen && foregroundFullscreen() {
		return false, "обнаружено полноэкранное приложение"
	}
	for _, p := range app.settings.SafetyProcesses {
		if processRunning(p) {
			return false, "запущен защищённый процесс: " + p
		}
	}
	return true, ""
}

func timeDurationMinutes(m int) time.Duration { return time.Duration(m) * time.Minute }

func foregroundFullscreen() bool {
	fg, _, _ := pGetForegroundWindow.Call()
	if fg == 0 || fg == app.hwnd {
		return false
	}
	var wr RECT
	if ok, _, _ := pGetWindowRect.Call(fg, uintptr(unsafe.Pointer(&wr))); ok == 0 {
		return false
	}
	mon, _, _ := pMonitorFromWindow.Call(fg, MONITOR_DEFAULTTONEAREST)
	if mon == 0 {
		return false
	}
	mi := MONITORINFO{CbSize: uint32(unsafe.Sizeof(MONITORINFO{}))}
	if ok, _, _ := pGetMonitorInfoW.Call(mon, uintptr(unsafe.Pointer(&mi))); ok == 0 {
		return false
	}
	margin := int32(3)
	return wr.Left <= mi.RcMonitor.Left+margin && wr.Top <= mi.RcMonitor.Top+margin && wr.Right >= mi.RcMonitor.Right-margin && wr.Bottom >= mi.RcMonitor.Bottom-margin
}

func normalizeSettings() {
	if app.settings.ResourceRefreshMS != 250 && app.settings.ResourceRefreshMS != 500 && app.settings.ResourceRefreshMS != 1000 && app.settings.ResourceRefreshMS != 2000 && app.settings.ResourceRefreshMS != 5000 {
		app.settings.ResourceRefreshMS = 1000
	}
	if app.settings.ResourceTimelineMode < 0 || app.settings.ResourceTimelineMode > 1 {
		app.settings.ResourceTimelineMode = 0
	}
	if app.settings.GraphWindowSize < -1 || app.settings.GraphWindowSize > 2 {
		app.settings.GraphWindowSize = 0
	}
	if app.settings.GraphWindowWidth <= 0 {
		app.settings.GraphWindowWidth, _ = scenarioGraphPresetSize(app.settings.GraphWindowSize)
	}
	if app.settings.GraphWindowHeight <= 0 {
		_, app.settings.GraphWindowHeight = scenarioGraphPresetSize(app.settings.GraphWindowSize)
	}
	app.settings.GraphWindowWidth = clampInt(app.settings.GraphWindowWidth, 900, 3840)
	app.settings.GraphWindowHeight = clampInt(app.settings.GraphWindowHeight, 760, 2160)
	app.settings.AdvancedConditions = migrateLegacyConditionGroups(app.settings.AdvancedConditions)
	for i := range app.settings.SavedTasks {
		app.settings.SavedTasks[i].Conditions = migrateLegacyConditionGroups(app.settings.SavedTasks[i].Conditions)
	}
	if app.settings.TaskKind == 1 || len(app.settings.AdvancedConditions) > 0 || len(app.settings.ActionSteps) > 0 {
		app.settings.ScenarioGraph = ensureScenarioGraph(app.settings.ScenarioGraph, legacyTaskStateFromSettings(app.settings))
	}
	for i := range app.settings.SavedTasks {
		if app.settings.SavedTasks[i].TaskKind == 1 {
			app.settings.SavedTasks[i].Graph = ensureScenarioGraph(app.settings.SavedTasks[i].Graph, taskStateFromSaved040(app.settings.SavedTasks[i]))
		}
	}
	if app.settings.SoundVolume < 0 || app.settings.SoundVolume > 100 {
		app.settings.SoundVolume = 65
	}
	if app.settings.ThemeMode < 0 || app.settings.ThemeMode > 2 {
		app.settings.ThemeMode = 0
	}
	if app.settings.AnimationMode < 0 || app.settings.AnimationMode > 2 {
		app.settings.AnimationMode = 0
	}
	if app.settings.Background < 0 || app.settings.Background > 5 {
		app.settings.Background = 0
	}
	if app.settings.SurfaceStyle < 0 || app.settings.SurfaceStyle > 4 {
		app.settings.SurfaceStyle = 0
	}
	if app.settings.LockMinimumSize {
		app.settings.LockCurrentSize = false
		app.settings.LockedWindowW = normalMinClientW
		app.settings.LockedWindowH = normalMinClientH
	}
	if app.settings.LockCurrentSize {
		if app.settings.LockedWindowW < normalMinClientW {
			app.settings.LockedWindowW = normalMinClientW
		}
		if app.settings.LockedWindowH < normalMinClientH {
			app.settings.LockedWindowH = normalMinClientH
		}
	}
	if strings.TrimSpace(app.settings.Recurrence.TimeHHMM) == "" {
		app.settings.Recurrence.TimeHHMM = "23:00"
	}
	if app.settings.SafetyIdleMinutes <= 0 {
		app.settings.SafetyIdleMinutes = 5
	}
	if app.settings.WakeLeadMinutes < 0 || app.settings.WakeLeadMinutes > 60 {
		app.settings.WakeLeadMinutes = 1
	}
	// Migrate pre-0.3.1 advanced tasks to the new block-task model.
	if app.settings.TaskKind == 0 && (len(app.settings.AdvancedConditions) > 0 || len(app.settings.ActionSteps) > 0) {
		app.settings.TaskKind = 1
	}
	for i := range app.settings.AdvancedConditions {
		c := &app.settings.AdvancedConditions[i]
		if c.ID == "" {
			c.ID = newAutomationID("cond")
		}
		if c.Compare == 0 {
			c.Compare = -1
		}
		c.OpenGroups = clampInt(c.OpenGroups, 0, 3)
		c.CloseGroups = clampInt(c.CloseGroups, 0, 3)
		c.DelayAfter = clampInt(c.DelayAfter, 0, 3600)
		if !c.Enabled && c.Type >= 0 { /* explicit disabled is preserved */
		}
	}
	// 0.3.3: advanced tasks own process-closing steps. Migrate the old task-level
	// process list once so existing advanced scenarios keep their behaviour.
	if app.settings.TaskKind == 1 && app.settings.CloseBefore && len(app.settings.Processes) > 0 {
		hasCloseStep := false
		for i := range app.settings.ActionSteps {
			if app.settings.ActionSteps[i].Type == stepCloseProcesses {
				hasCloseStep = true
				break
			}
		}
		if !hasCloseStep {
			st := ActionStep{ID: newAutomationID("step"), Type: stepCloseProcesses, Processes: append([]string(nil), app.settings.Processes...)}
			app.settings.ActionSteps = append([]ActionStep{st}, app.settings.ActionSteps...)
		}
		app.settings.CloseBefore = false
		app.settings.Processes = nil
	}
	for i := range app.settings.ActionSteps {
		st := &app.settings.ActionSteps[i]
		if st.ID == "" {
			st.ID = newAutomationID("step")
		}
		st.OnError = clampInt(st.OnError, 0, 2)
		st.Retries = clampInt(st.Retries, 0, 10)
		st.DelayAfter = clampInt(st.DelayAfter, 0, 3600)
		if st.Type == stepCloseProcesses && st.Processes == nil {
			st.Processes = []string{}
		}
	}
	for i := range app.settings.SavedTasks {
		normalizeSavedTask(&app.settings.SavedTasks[i])
	}
}

func normalizeSavedTask(t *SavedTask) {
	if t.ID == "" {
		t.ID = newAutomationID("task")
	}
	if t.TaskKind == 0 && (len(t.Conditions) > 0 || len(t.Steps) > 0) {
		t.TaskKind = 1
	}
	if strings.TrimSpace(t.Recurrence.TimeHHMM) == "" {
		t.Recurrence.TimeHHMM = "23:00"
	}
	for i := range t.Conditions {
		if t.Conditions[i].ID == "" {
			t.Conditions[i].ID = newAutomationID("cond")
		}
		if t.Conditions[i].Compare == 0 {
			t.Conditions[i].Compare = -1
		}
		t.Conditions[i].OpenGroups = clampInt(t.Conditions[i].OpenGroups, 0, 3)
		t.Conditions[i].CloseGroups = clampInt(t.Conditions[i].CloseGroups, 0, 3)
		t.Conditions[i].DelayAfter = clampInt(t.Conditions[i].DelayAfter, 0, 3600)
	}
	if t.TaskKind == 1 && t.CloseBefore && len(t.Processes) > 0 {
		hasCloseStep := false
		for i := range t.Steps {
			if t.Steps[i].Type == stepCloseProcesses {
				hasCloseStep = true
				break
			}
		}
		if !hasCloseStep {
			st := ActionStep{ID: newAutomationID("step"), Type: stepCloseProcesses, Processes: append([]string(nil), t.Processes...)}
			t.Steps = append([]ActionStep{st}, t.Steps...)
		}
		t.CloseBefore = false
		t.Processes = nil
	}
	for i := range t.Steps {
		st := &t.Steps[i]
		if st.ID == "" {
			st.ID = newAutomationID("step")
		}
		st.OnError = clampInt(st.OnError, 0, 2)
		st.Retries = clampInt(st.Retries, 0, 10)
		st.DelayAfter = clampInt(st.DelayAfter, 0, 3600)
		if st.Type == stepCloseProcesses && st.Processes == nil {
			st.Processes = []string{}
		}
	}
}

var (
	wfWininet                     = syscall.NewLazyDLL("wininet.dll")
	pInternetGetConnectedState040 = wfWininet.NewProc("InternetGetConnectedState")
	pSetPriorityClass040          = kernel32.NewProc("SetPriorityClass")
)

func internetConnected040() bool {
	var flags uint32
	r, _, _ := pInternetGetConnectedState040.Call(uintptr(unsafe.Pointer(&flags)), 0)
	return r != 0
}

func setProcessPriority040(name string, level int) bool {
	classes := []uintptr{0x00000040, 0x00000020, 0x00000080} // IDLE, NORMAL, HIGH
	level = clampInt(level, 0, 2)
	okAny := false
	for _, info := range processInstancesForMetrics() {
		if !strings.EqualFold(info.Name, strings.TrimSpace(name)) {
			continue
		}
		h, _, _ := pOpenProcess.Call(0x0200|PROCESS_QUERY_LIMITED_INFORMATION, 0, uintptr(info.PID))
		if h == 0 {
			continue
		}
		r, _, _ := pSetPriorityClass040.Call(h, classes[level])
		pCloseHandle.Call(h)
		if r != 0 {
			okAny = true
		}
	}
	return okAny
}
