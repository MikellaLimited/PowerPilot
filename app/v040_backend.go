//go:build windows

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

// 0.4.0 infrastructure: single-instance, draft autosave, crash recovery,
// undo/redo, shared block clipboard, templates, validation and execution state.

var (
	v040User32              = syscall.NewLazyDLL("user32.dll")
	v040Kernel32            = syscall.NewLazyDLL("kernel32.dll")
	pFindWindowW040         = v040User32.NewProc("FindWindowW")
	pSetForegroundWindow040 = v040User32.NewProc("SetForegroundWindow")
	pCreateMutexW040        = v040Kernel32.NewProc("CreateMutexW")
	pGetLastError040        = v040Kernel32.NewProc("GetLastError")
	pSetWindowPos040        = v040User32.NewProc("SetWindowPos")
)

const errorAlreadyExists = 183
const hwndTopmost = ^uintptr(0)
const hwndNotopmost = ^uintptr(1)

var singleInstanceMutex uintptr

func acquireSingleInstance() bool {
	name := wstr(`Local\PowerPilot-0F1C2A0E-0D3E-47D7-8A86-737F7D7B48A9`)
	h, _, _ := pCreateMutexW040.Call(0, 0, uintptr(unsafe.Pointer(name)))
	if h == 0 {
		return true // fail open rather than preventing launch
	}
	singleInstanceMutex = h
	errCode, _, _ := pGetLastError040.Call()
	if errCode != errorAlreadyExists {
		return true
	}
	cls := wstr("PowerPilotNativeWindow")
	hwnd, _, _ := pFindWindowW040.Call(uintptr(unsafe.Pointer(cls)), 0)
	if hwnd != 0 {
		pShowWindow.Call(hwnd, SW_RESTORE)
		pSetForegroundWindow040.Call(hwnd)
	}
	pCloseHandle.Call(h)
	singleInstanceMutex = 0
	return false
}

func releaseSingleInstance() {
	if singleInstanceMutex != 0 {
		pCloseHandle.Call(singleInstanceMutex)
		singleInstanceMutex = 0
	}
}

// ---- Draft autosave / crash recovery --------------------------------------

type TaskState struct {
	Action         int                   `json:"action"`
	Mode           int                   `json:"mode"`
	DelayHours     int                   `json:"delay_hours"`
	DelayMinutes   int                   `json:"delay_minutes"`
	DelaySeconds   int                   `json:"delay_seconds"`
	Exact          string                `json:"exact"`
	IdleMinutes    int                   `json:"idle_minutes"`
	WatchProcess   string                `json:"watch_process"`
	CloseBefore    bool                  `json:"close_before"`
	Processes      []string              `json:"processes"`
	WarningSeconds int                   `json:"warning_seconds"`
	Conditions     []AutomationCondition `json:"conditions,omitempty"`
	TriggerLogic   int                   `json:"trigger_logic"`
	Steps          []ActionStep          `json:"steps,omitempty"`
	Recurrence     RecurrenceSpec        `json:"recurrence"`
	TaskKind       int                   `json:"task_kind"`
	Graph          ScenarioGraph         `json:"graph,omitempty"`
}

type DraftFile struct {
	Version            int       `json:"version"`
	UpdatedAt          string    `json:"updated_at"`
	Task               TaskState `json:"task"`
	CurrentTaskKind    int       `json:"current_task_kind"`
	CurrentTaskSection int       `json:"current_task_section"`
	LastTaskSection    int       `json:"last_task_section"`
}

type PersistedSchedule struct {
	Active         bool                  `json:"active"`
	Action         int                   `json:"action"`
	Mode           int                   `json:"mode"`
	Target         time.Time             `json:"target"`
	IdleThreshold  time.Duration         `json:"idle_threshold"`
	WatchProcess   string                `json:"watch_process"`
	Started        time.Time             `json:"started"`
	Total          time.Duration         `json:"total"`
	Conditions     []AutomationCondition `json:"conditions"`
	TriggerLogic   int                   `json:"trigger_logic"`
	Steps          []ActionStep          `json:"steps"`
	CloseBefore    bool                  `json:"close_before"`
	Processes      []string              `json:"processes"`
	WarningSeconds int                   `json:"warning_seconds"`
	SourceTaskID   string                `json:"source_task_id"`
	SourceTaskName string                `json:"source_task_name"`
	RunID          string                `json:"run_id"`
	Graph          ScenarioGraph         `json:"graph,omitempty"`
}

type SessionFile struct {
	Version  int               `json:"version"`
	SavedAt  string            `json:"saved_at"`
	Draft    DraftFile         `json:"draft"`
	Schedule PersistedSchedule `json:"schedule"`
}

func draftPath() string        { return filepath.Join(settingsDir(), "draft.json") }
func sessionPath() string      { return filepath.Join(settingsDir(), "session.json") }
func crashMarkerPath() string  { return filepath.Join(settingsDir(), "running.lock") }
func technicalLogPath() string { return filepath.Join(settingsDir(), "PowerPilot.log") }

func captureTaskState() TaskState {
	s := app.settings
	// Read visible input values as well, so text typed immediately before a crash is retained.
	if app.hwnd != 0 {
		s.DelayHours = parseInt(getText(app.edits[idDelayHours]), s.DelayHours)
		s.DelayMinutes = parseInt(getText(app.edits[idDelayMinutes]), s.DelayMinutes)
		s.DelaySeconds = parseInt(getText(app.edits[idDelaySeconds]), s.DelaySeconds)
		if app.edits[idExactDay] != 0 {
			s.Exact = exactFromFields()
		}
		s.IdleMinutes = parseInt(getText(app.edits[idIdleMinutes]), s.IdleMinutes)
		if v := strings.TrimSpace(getText(app.edits[idWatchProcess])); v != "" || s.WatchProcess != "" {
			s.WatchProcess = v
		}
		s.WarningSeconds = parseInt(getText(app.edits[idWarning]), s.WarningSeconds)
		if v := strings.TrimSpace(getText(app.edits[idScheduleTime])); v != "" {
			s.Recurrence.TimeHHMM = v
		}
	}
	return TaskState{
		Action: app.selectedAction, Mode: app.selectedMode,
		DelayHours: s.DelayHours, DelayMinutes: s.DelayMinutes, DelaySeconds: s.DelaySeconds,
		Exact: s.Exact, IdleMinutes: s.IdleMinutes, WatchProcess: s.WatchProcess,
		CloseBefore: s.CloseBefore, Processes: append([]string(nil), s.Processes...),
		WarningSeconds: s.WarningSeconds, Conditions: append([]AutomationCondition(nil), s.AdvancedConditions...),
		TriggerLogic: s.TriggerLogic, Steps: cloneActionSteps(s.ActionSteps), Recurrence: s.Recurrence, TaskKind: app.currentTaskKind, Graph: cloneScenarioGraph(s.ScenarioGraph),
	}
}

func applyTaskState(t TaskState) {
	app.selectedAction, app.selectedMode = t.Action, t.Mode
	app.settings.Action, app.settings.Mode = t.Action, t.Mode
	app.settings.DelayHours, app.settings.DelayMinutes, app.settings.DelaySeconds = t.DelayHours, t.DelayMinutes, t.DelaySeconds
	app.settings.Exact, app.settings.IdleMinutes, app.settings.WatchProcess = t.Exact, t.IdleMinutes, t.WatchProcess
	app.settings.CloseBefore = t.CloseBefore
	app.settings.Processes = append([]string(nil), t.Processes...)
	app.settings.WarningSeconds = t.WarningSeconds
	app.settings.AdvancedConditions = append([]AutomationCondition(nil), t.Conditions...)
	app.settings.TriggerLogic = t.TriggerLogic
	app.settings.ActionSteps = cloneActionSteps(t.Steps)
	app.settings.Recurrence = t.Recurrence
	app.settings.ScenarioGraph = cloneScenarioGraph(t.Graph)
	app.settings.TaskKind = t.TaskKind
	app.currentTaskKind = t.TaskKind
	if app.hwnd != 0 {
		restoreCurrentInputTexts()
		syncAllTaskInputTexts040()
	}
	resetConditionRuntimes()
}

func syncAllTaskInputTexts040() {
	setEditTextIfDifferent(idDelayHours, fmt.Sprint(app.settings.DelayHours))
	setEditTextIfDifferent(idDelayMinutes, fmt.Sprint(app.settings.DelayMinutes))
	setEditTextIfDifferent(idDelaySeconds, fmt.Sprint(app.settings.DelaySeconds))
	setExactFields(app.settings.Exact)
	setEditTextIfDifferent(idIdleMinutes, fmt.Sprint(max(app.settings.IdleMinutes, 1)))
	setEditTextIfDifferent(idWatchProcess, app.settings.WatchProcess)
	setEditTextIfDifferent(idWarning, fmt.Sprint(max(app.settings.WarningSeconds, 0)))
	setEditTextIfDifferent(idScheduleTime, app.settings.Recurrence.TimeHHMM)
}

func currentDraftFile() DraftFile {
	return DraftFile{Version: 1, UpdatedAt: time.Now().Format(time.RFC3339Nano), Task: captureTaskState(),
		CurrentTaskKind: app.currentTaskKind, CurrentTaskSection: app.currentTaskSection, LastTaskSection: app.lastTaskSection}
}

func saveDraftAutosave() {
	_ = os.MkdirAll(settingsDir(), 0755)
	d := currentDraftFile()
	b, err := json.MarshalIndent(d, "", "  ")
	if err == nil {
		_ = atomicWriteFile040(draftPath(), b)
	}
	saveSession040(d)
}

func atomicWriteFile040(path string, b []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0644); err != nil {
		return err
	}
	_ = os.Remove(path)
	return os.Rename(tmp, path)
}

func loadDraftAutosave() (DraftFile, bool) {
	var d DraftFile
	b, err := os.ReadFile(draftPath())
	if err != nil || json.Unmarshal(b, &d) != nil || d.Version == 0 {
		return d, false
	}
	return d, true
}

func persistedSchedule040() PersistedSchedule {
	s := app.schedule
	return PersistedSchedule{Active: s.active, Action: s.action, Mode: s.mode, Target: s.target, IdleThreshold: s.idleThreshold,
		WatchProcess: s.watchProcess, Started: s.started, Total: s.total, Conditions: append([]AutomationCondition(nil), s.conditions...),
		TriggerLogic: s.triggerLogic, Steps: cloneActionSteps(s.steps), CloseBefore: s.closeBefore, Processes: append([]string(nil), s.processes...),
		WarningSeconds: s.warningSeconds, SourceTaskID: s.sourceTaskID, SourceTaskName: s.sourceTaskName, RunID: s.runID, Graph: cloneScenarioGraph(s.graph)}
}

func scheduleFromPersisted040(p PersistedSchedule) Schedule {
	return Schedule{active: p.Active, action: p.Action, mode: p.Mode, target: p.Target, idleThreshold: p.IdleThreshold, watchProcess: p.WatchProcess,
		started: p.Started, total: p.Total, conditions: append([]AutomationCondition(nil), p.Conditions...), triggerLogic: p.TriggerLogic,
		steps: cloneActionSteps(p.Steps), closeBefore: p.CloseBefore, processes: append([]string(nil), p.Processes...), warningSeconds: p.WarningSeconds,
		sourceTaskID: p.SourceTaskID, sourceTaskName: p.SourceTaskName, runID: p.RunID, graph: cloneScenarioGraph(p.Graph)}
}

func saveSession040(d DraftFile) {
	sf := SessionFile{Version: 1, SavedAt: time.Now().Format(time.RFC3339Nano), Draft: d, Schedule: persistedSchedule040()}
	b, err := json.MarshalIndent(sf, "", "  ")
	if err == nil {
		_ = atomicWriteFile040(sessionPath(), b)
	}
}

func startupRecovery040() {
	// Always restore the unfinished task draft. Saved tasks are intentionally not part of it.
	if d, ok := loadDraftAutosave(); ok {
		applyTaskState(d.Task)
		app.currentTaskKind, app.currentTaskSection, app.lastTaskSection = d.CurrentTaskKind, d.CurrentTaskSection, d.LastTaskSection
		// TaskState.TaskKind in older drafts could lag behind CurrentTaskKind while
		// the user was inside the advanced editor. Keep Settings in sync so later
		// startup initialization cannot accidentally fall back to Simple.
		app.settings.TaskKind = app.currentTaskKind
	}
	_, crashed := os.Stat(crashMarkerPath())
	if crashed != nil {
		return
	}
	var sf SessionFile
	if b, err := os.ReadFile(sessionPath()); err == nil && json.Unmarshal(b, &sf) == nil && sf.Schedule.Active {
		r, _, _ := pMessageBoxW.Call(0, uintptr(unsafe.Pointer(wstr("PowerPilot был завершён при активной задаче.\n\nВозобновить предыдущую задачу?"))), uintptr(unsafe.Pointer(wstr("PowerPilot — восстановление"))), MB_YESNO|MB_ICONQUESTION)
		if r == IDYES {
			app.schedule = scheduleFromPersisted040(sf.Schedule)
			if app.schedule.runID == "" {
				app.schedule.runID = newRunID()
			}
			app.status = "Восстановлена активная задача"
			techLog040("RECOVERY resumed active schedule")
		} else {
			techLog040("RECOVERY active schedule discarded")
		}
	}
}

func markRunning040() {
	_ = os.MkdirAll(settingsDir(), 0755)
	_ = os.WriteFile(crashMarkerPath(), []byte(fmt.Sprintf("pid=%d\nstarted=%s\n", os.Getpid(), time.Now().Format(time.RFC3339Nano))), 0644)
}
func markGracefulExit040() { _ = os.Remove(crashMarkerPath()); _ = os.Remove(sessionPath()) }

// ---- Undo / redo -----------------------------------------------------------

var undoManager040 = struct {
	sync.Mutex
	Undo               []TaskState
	Redo               []TaskState
	Current            TaskState
	Fingerprint        string
	Pending            TaskState
	PendingFingerprint string
	PendingSince       time.Time
	Applying           bool
}{}

func taskFingerprint040(t TaskState) string { b, _ := json.Marshal(t); return string(b) }

func initUndo040() {
	st := captureTaskState()
	undoManager040.Current = st
	undoManager040.Fingerprint = taskFingerprint040(st)
}

func observeUndo040(now time.Time) {
	undoManager040.Lock()
	defer undoManager040.Unlock()
	if undoManager040.Applying {
		return
	}
	st := captureTaskState()
	fp := taskFingerprint040(st)
	if fp == undoManager040.Fingerprint {
		undoManager040.PendingFingerprint = ""
		return
	}
	if fp != undoManager040.PendingFingerprint {
		undoManager040.Pending, undoManager040.PendingFingerprint, undoManager040.PendingSince = st, fp, now
		return
	}
	if now.Sub(undoManager040.PendingSince) < 450*time.Millisecond {
		return
	}
	undoManager040.Undo = append(undoManager040.Undo, undoManager040.Current)
	if len(undoManager040.Undo) > 80 {
		undoManager040.Undo = undoManager040.Undo[len(undoManager040.Undo)-80:]
	}
	undoManager040.Redo = nil
	undoManager040.Current, undoManager040.Fingerprint = undoManager040.Pending, fp
	undoManager040.PendingFingerprint = ""
}

func undoAvailable040() bool {
	undoManager040.Lock()
	defer undoManager040.Unlock()
	return len(undoManager040.Undo) > 0
}

func redoAvailable040() bool {
	undoManager040.Lock()
	defer undoManager040.Unlock()
	return len(undoManager040.Redo) > 0
}

func undoTask040() bool {
	undoManager040.Lock()
	if len(undoManager040.Undo) == 0 {
		undoManager040.Unlock()
		return false
	}
	prev := undoManager040.Undo[len(undoManager040.Undo)-1]
	undoManager040.Undo = undoManager040.Undo[:len(undoManager040.Undo)-1]
	undoManager040.Redo = append(undoManager040.Redo, undoManager040.Current)
	undoManager040.Applying = true
	undoManager040.Unlock()
	applyTaskState(prev)
	saveSettings()
	saveDraftAutosave()
	undoManager040.Lock()
	undoManager040.Current = prev
	undoManager040.Fingerprint = taskFingerprint040(prev)
	undoManager040.PendingFingerprint = ""
	undoManager040.Applying = false
	undoManager040.Unlock()
	startSubReveal()
	updateInputVisibility()
	invalidate(app.hwnd)
	playUI(openSound)
	return true
}

func redoTask040() bool {
	undoManager040.Lock()
	if len(undoManager040.Redo) == 0 {
		undoManager040.Unlock()
		return false
	}
	next := undoManager040.Redo[len(undoManager040.Redo)-1]
	undoManager040.Redo = undoManager040.Redo[:len(undoManager040.Redo)-1]
	undoManager040.Undo = append(undoManager040.Undo, undoManager040.Current)
	undoManager040.Applying = true
	undoManager040.Unlock()
	applyTaskState(next)
	saveSettings()
	saveDraftAutosave()
	undoManager040.Lock()
	undoManager040.Current = next
	undoManager040.Fingerprint = taskFingerprint040(next)
	undoManager040.PendingFingerprint = ""
	undoManager040.Applying = false
	undoManager040.Unlock()
	startSubReveal()
	updateInputVisibility()
	invalidate(app.hwnd)
	playUI(openSound)
	return true
}

// ---- Shared block clipboard / duplication ---------------------------------

type ScenarioClipboard040 struct {
	Kind       int // 1 condition, 2 step, 3 condition group, 4 step group
	Condition  AutomationCondition
	Step       ActionStep
	Conditions []AutomationCondition
	Steps      []ActionStep
}

var scenarioClipboard040 ScenarioClipboard040

func remapConditionTreeIDs(src []AutomationCondition) []AutomationCondition {
	out := append([]AutomationCondition(nil), src...)
	ids := make(map[string]string, len(out))
	for _, c := range out {
		ids[c.ID] = newAutomationID("cond")
	}
	for i := range out {
		out[i].ID = ids[out[i].ID]
		if parent, ok := ids[out[i].GroupID]; ok {
			out[i].GroupID = parent
		} else {
			out[i].GroupID = ""
		}
	}
	return out
}

func copyScenarioBlock040(kind, idx int) bool {
	switch kind {
	case 1:
		v := currentScenarioConditions()
		if idx < 0 || idx >= len(v) {
			return false
		}
		c := v[idx]
		c.ID = newAutomationID("cond")
		c.GroupID = ""
		scenarioClipboard040 = ScenarioClipboard040{Kind: 1, Condition: c}
	case 2:
		v := currentScenarioSteps()
		if idx < 0 || idx >= len(v) {
			return false
		}
		st := cloneActionSteps([]ActionStep{v[idx]})[0]
		st.ID = newAutomationID("step")
		scenarioClipboard040 = ScenarioClipboard040{Kind: 2, Step: st}
	default:
		return false
	}
	showNotification("PowerPilot", "Блок скопирован. Его можно вставить в другую продвинутую задачу.")
	return true
}

func copyScenarioGroup040(kind int) bool {
	switch kind {
	case 1:
		v := currentScenarioConditions()
		if len(v) == 0 {
			return false
		}
		cpy := remapConditionTreeIDs(v)
		scenarioClipboard040 = ScenarioClipboard040{Kind: 3, Conditions: cpy}
	case 2:
		v := currentScenarioSteps()
		if len(v) == 0 {
			return false
		}
		cpy := cloneActionSteps(v)
		for i := range cpy {
			cpy[i].ID = newAutomationID("step")
		}
		scenarioClipboard040 = ScenarioClipboard040{Kind: 4, Steps: cpy}
	default:
		return false
	}
	showNotification("PowerPilot", "Группа блоков скопирована. Её можно вставить в другую продвинутую задачу.")
	return true
}

func duplicateScenarioBlock040(kind, idx int) bool {
	if !copyScenarioBlock040(kind, idx) {
		return false
	}
	return pasteScenarioBlock040(kind, idx+1)
}

func pasteScenarioBlock040(kind, at int) bool {
	if kind == 1 {
		if scenarioClipboard040.Kind != 1 && scenarioClipboard040.Kind != 3 {
			return false
		}
		v := append([]AutomationCondition(nil), currentScenarioConditions()...)
		var incoming []AutomationCondition
		if scenarioClipboard040.Kind == 1 {
			incoming = []AutomationCondition{scenarioClipboard040.Condition}
		} else {
			incoming = append([]AutomationCondition(nil), scenarioClipboard040.Conditions...)
		}
		if len(v)+len(incoming) > 32 {
			return false
		}
		incoming = remapConditionTreeIDs(incoming)
		at = clampInt(at, 0, len(v))
		out := make([]AutomationCondition, 0, len(v)+len(incoming))
		out = append(out, v[:at]...)
		out = append(out, incoming...)
		out = append(out, v[at:]...)
		setCurrentScenarioConditions(out)
	} else if kind == 2 {
		if scenarioClipboard040.Kind != 2 && scenarioClipboard040.Kind != 4 {
			return false
		}
		v := cloneActionSteps(currentScenarioSteps())
		var incoming []ActionStep
		if scenarioClipboard040.Kind == 2 {
			incoming = cloneActionSteps([]ActionStep{scenarioClipboard040.Step})
		} else {
			incoming = cloneActionSteps(scenarioClipboard040.Steps)
		}
		if len(v)+len(incoming) > 32 {
			return false
		}
		for i := range incoming {
			incoming[i].ID = newAutomationID("step")
		}
		at = clampInt(at, 0, len(v))
		out := make([]ActionStep, 0, len(v)+len(incoming))
		out = append(out, v[:at]...)
		out = append(out, incoming...)
		out = append(out, v[at:]...)
		setCurrentScenarioSteps(out)
	} else {
		return false
	}
	saveSettings()
	startSubReveal()
	// Rebuild row geometry immediately. Previously the inserted block existed in
	// data but could remain invisible until leaving/re-entering the page.
	if app.hwnd != 0 {
		layoutControls(app.hwnd)
	}
	invalidate(app.hwnd)
	return true
}

// ---- Templates -------------------------------------------------------------

type TaskTemplate040 struct {
	Name, Description string
	Task              TaskState
}

func templates040() []TaskTemplate040 {
	base := TaskState{Action: 0, Mode: 0, DelayMinutes: 30, WarningSeconds: 60, TaskKind: 1, TriggerLogic: logicAND, Recurrence: RecurrenceSpec{Enabled: true, TimeHHMM: "23:00"}}
	return []TaskTemplate040{
		{"Сон после игры", "Ждёт завершения процесса и переводит ПК в сон", func() TaskState { t := base; t.Action = 2; t.Mode = 3; t.WatchProcess = "game.exe"; return t }()},
		{"После загрузки", "Сеть должна быть тихой 5 минут, затем выключение", func() TaskState {
			t := base
			t.Mode = 5
			t.Conditions = []AutomationCondition{{ID: newAutomationID("cond"), Type: condNetwork, Compare: -1, Threshold: 100, HoldSeconds: 300, Enabled: true}}
			return t
		}()},
		{"После рендера", "CPU ниже 10% две минуты, потом пауза и выключение", func() TaskState {
			t := base
			t.Mode = 5
			t.Conditions = []AutomationCondition{{ID: newAutomationID("cond"), Type: condCPU, Compare: -1, Threshold: 10, HoldSeconds: 120, Enabled: true}}
			t.Steps = []ActionStep{{ID: newAutomationID("step"), Type: stepWait, Value: 30}}
			return t
		}()},
		{"Ночная гибернация", "Каждый день в 02:00 — гибернация", func() TaskState {
			t := base
			t.Action = 3
			t.Mode = 4
			t.Recurrence = RecurrenceSpec{Enabled: true, Kind: 0, TimeHHMM: "02:00"}
			return t
		}()},
		{"Тихий ПК", "Нет звука 10 минут — сон", func() TaskState {
			t := base
			t.Action = 2
			t.Mode = 5
			t.Conditions = []AutomationCondition{{ID: newAutomationID("cond"), Type: condAudioSilent, HoldSeconds: 600, Enabled: true}}
			return t
		}()},
		{"Окно закрыто", "Ждёт исчезновения окна/приложения и выключает ПК", func() TaskState {
			t := base
			t.Mode = 5
			t.Conditions = []AutomationCondition{{ID: newAutomationID("cond"), Type: condWindowMissing, Text: "Название окна", Enabled: true}}
			return t
		}()},
	}
}

func applyTemplate040(i int) bool {
	tt := templates040()
	if i < 0 || i >= len(tt) {
		return false
	}
	applyTaskState(tt[i].Task)
	playUI(openSound)
	app.section = 7
	app.currentTaskKind = 1
	app.currentTaskSection = 7
	app.lastTaskSection = 2
	saveSettings()
	saveDraftAutosave()
	initUndo040()
	startPageAnimation()
	updateInputVisibility()
	invalidate(app.hwnd)
	return true
}

// ---- Validation ------------------------------------------------------------

type ValidationIssue040 struct {
	Level   int
	Message string
} // 2 error, 1 warning

func validateTask040(t TaskState) []ValidationIssue040 {
	var out []ValidationIssue040
	addE := func(s string) { out = append(out, ValidationIssue040{2, s}) }
	addW := func(s string) { out = append(out, ValidationIssue040{1, s}) }
	if t.Action < 0 || t.Action > 4 {
		addE("Не выбрано финальное действие")
	}
	switch t.Mode {
	case 0:
		if t.DelayHours == 0 && t.DelayMinutes == 0 && t.DelaySeconds == 0 {
			addE("Таймер равен нулю")
		}
	case 1:
		if tm, err := parseExact(t.Exact); err != nil {
			addE("Некорректная дата и время")
		} else if !tm.After(time.Now()) {
			addW("Дата и время уже прошли")
		}
	case 2:
		if t.IdleMinutes < 1 {
			addE("Время простоя должно быть не меньше 1 секунды")
		}
	case 3:
		if strings.TrimSpace(t.WatchProcess) == "" {
			addE("Не выбран процесс для триггера")
		}
	case 4:
		if _, _, err := parseHHMM(t.Recurrence.TimeHHMM); err != nil {
			addE("Некорректное время расписания")
		}
	case 5:
		if len(t.Conditions) == 0 && !(t.TaskKind == 1 && len(t.Graph.Nodes) > 0) {
			addE("Для запуска по условиям добавьте условие")
		}
	}
	balance := 0
	for i, c := range t.Conditions {
		balance += c.OpenGroups
		balance -= c.CloseGroups
		if balance < 0 {
			addE(fmt.Sprintf("Условие %d: закрывающая скобка без открывающей", i+1))
			balance = 0
		}
		switch c.Type {
		case condFileStable:
			if strings.TrimSpace(c.Text) == "" {
				addE(fmt.Sprintf("Условие %d: не выбран файл", i+1))
			}
		case condFolderCount:
			if strings.TrimSpace(c.Text) == "" {
				addE(fmt.Sprintf("Условие %d: не выбрана папка", i+1))
			}
		case condProcessExit, condWindowExists, condWindowMissing, condWindowActive, condWindowTitle:
			if strings.TrimSpace(c.Text) == "" {
				addE(fmt.Sprintf("Условие %d: не задано окно/процесс", i+1))
			}
		}
	}
	if balance != 0 {
		addE("Несбалансированные скобки в условиях")
	}
	for i, s := range t.Steps {
		switch s.Type {
		case stepCloseProcesses:
			if len(s.Processes) == 0 {
				addW(fmt.Sprintf("Шаг %d: список процессов пуст", i+1))
			}
		case stepRunCommand:
			if strings.TrimSpace(s.Text) == "" {
				addE(fmt.Sprintf("Шаг %d: команда не задана", i+1))
			}
		case stepWait:
			if s.Value <= 0 {
				addE(fmt.Sprintf("Шаг %d: ожидание должно быть больше 0 секунд", i+1))
			}
		case stepSetVolume:
			if s.Value < 0 || s.Value > 100 {
				addE(fmt.Sprintf("Шаг %d: громкость должна быть 0–100%%", i+1))
			}
		}
	}
	out = append(out, detectLogicConflicts040(t.Conditions)...)
	return out
}

func detectLogicConflicts040(conds []AutomationCondition) []ValidationIssue040 {
	// Conservative contradiction detector for simple AND sequences in the same nesting depth.
	type bounds struct{ min, max *float64 }
	levels := map[string]bounds{}
	depth := 0
	for i, c := range conds {
		depth += c.OpenGroups
		if i > 0 && c.Logic == logicOR {
			levels = map[string]bounds{}
		}
		key := fmt.Sprintf("%d/%d", depth, c.Type)
		if conditionUsesThreshold(c.Type) {
			b := levels[key]
			v := c.Threshold
			if c.Compare > 0 {
				vv := v
				if b.min == nil || vv > *b.min {
					b.min = &vv
				}
			} else {
				vv := v
				if b.max == nil || vv < *b.max {
					b.max = &vv
				}
			}
			if b.min != nil && b.max != nil && *b.min > *b.max {
				return []ValidationIssue040{{2, fmt.Sprintf("Логически невозможные условия: %s одновременно ≥ %.0f и ≤ %.0f", conditionName(c.Type), *b.min, *b.max)}}
			}
			levels[key] = b
		}
		depth -= c.CloseGroups
		if depth < 0 {
			depth = 0
		}
	}
	return nil
}

func validateCurrentTaskBeforeSave040() bool {
	issues := validateTask040(captureTaskState())
	errs, warns := 0, 0
	var lines []string
	for _, i := range issues {
		if i.Level == 2 {
			errs++
		} else {
			warns++
		}
		if len(lines) < 6 {
			lines = append(lines, "• "+i.Message)
		}
	}
	if errs == 0 && warns == 0 {
		return true
	}
	title := fmt.Sprintf("Проверка задачи: ошибок %d, предупреждений %d", errs, warns)
	if errs > 0 {
		message(title, strings.Join(lines, "\n"), MB_OK|MB_ICONERROR)
		return false
	}
	r, _, _ := pMessageBoxW.Call(app.hwnd, uintptr(unsafe.Pointer(wstr(strings.Join(lines, "\n")+"\n\nСохранить всё равно?"))), uintptr(unsafe.Pointer(wstr(title))), MB_YESNO|MB_ICONQUESTION)
	return r == IDYES
}

// ---- Technical log ---------------------------------------------------------
var techLogMu040 sync.Mutex

func techLog040(line string) {
	techLogMu040.Lock()
	defer techLogMu040.Unlock()
	_ = os.MkdirAll(settingsDir(), 0755)
	p := technicalLogPath()
	if st, err := os.Stat(p); err == nil && st.Size() > 2*1024*1024 {
		_ = os.Rename(p, p+".1")
	}
	f, err := os.OpenFile(p, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err == nil {
		fmt.Fprintf(f, "%s %s\n", time.Now().Format("2006-01-02 15:04:05.000"), line)
		_ = f.Close()
	}
}

// ---- Mini mode / UI scale --------------------------------------------------
func applyMiniTopmost040() {
	if app.hwnd == 0 {
		return
	}
	z := hwndNotopmost
	if app.miniMode && app.settings.AlwaysOnTopMini {
		z = hwndTopmost
	}
	pSetWindowPos040.Call(app.hwnd, z, 0, 0, 0, 0, 0x0001|0x0002|0x0010) // NOSIZE|NOMOVE|NOACTIVATE
}
func normalizeV040Settings() {
	if app.settings.UIScale == 0 {
		app.settings.UIScale = 100
	}
	app.settings.UIScale = clampInt(app.settings.UIScale, 90, 125)
	if !app.settings.MiniShowTask && !app.settings.MiniShowCountdown && !app.settings.MiniShowStep && !app.settings.MiniShowMetrics && !app.settings.MiniLayoutMigrated {
		app.settings.MiniShowTask = true
		app.settings.MiniShowCountdown = true
		app.settings.MiniShowStep = true
	}
	if !app.settings.MiniLayoutMigrated {
		if app.settings.MiniShowMetrics {
			app.settings.MiniShowCPU = true
			app.settings.MiniShowGPU = true
			app.settings.MiniShowRAM = true
			app.settings.MiniShowNetwork = true
			app.settings.MiniShowDisk = true
		}
		app.settings.MiniShowProgress = true
		app.settings.MiniLayoutMigrated = true
	}
}

var lastAutosave040 time.Time

func maintenance040(now time.Time) {
	observeUndo040(now)
	if lastAutosave040.IsZero() || now.Sub(lastAutosave040) >= time.Second {
		lastAutosave040 = now
		saveDraftAutosave()
	}
}

var pGetKeyState040 = v040User32.NewProc("GetKeyState")

func keyDown040(vk uintptr) bool { r, _, _ := pGetKeyState040.Call(vk); return int16(r&0xffff) < 0 }
func handleKeyDown040(vk uintptr) bool {
	if handleGraphKeyboard(vk) {
		return true
	}
	ctrl := keyDown040(0x11)
	if ctrl {
		switch vk {
		case 'Z':
			return undoTask040()
		case 'Y':
			return redoTask040()
		case 'C':
			if app.selectedScenarioKind != 0 {
				return copyScenarioBlock040(app.selectedScenarioKind, app.selectedScenarioIndex)
			}
		case 'D':
			if app.selectedScenarioKind != 0 {
				return duplicateScenarioBlock040(app.selectedScenarioKind, app.selectedScenarioIndex)
			}
		case 'V':
			switch scenarioClipboard040.Kind {
			case 1, 3:
				return pasteScenarioBlock040(1, 9999)
			case 2, 4:
				return pasteScenarioBlock040(2, 9999)
			}
		}
	}
	return false
}

func taskStateFromSaved040(t SavedTask) TaskState {
	return TaskState{Action: t.Action, Mode: t.Mode, DelayHours: t.DelayHours, DelayMinutes: t.DelayMinutes, DelaySeconds: t.DelaySeconds, Exact: t.Exact, IdleMinutes: t.IdleMinutes, WatchProcess: t.WatchProcess, CloseBefore: t.CloseBefore, Processes: append([]string(nil), t.Processes...), WarningSeconds: t.WarningSeconds, Conditions: append([]AutomationCondition(nil), t.Conditions...), TriggerLogic: t.TriggerLogic, Steps: cloneActionSteps(t.Steps), Recurrence: t.Recurrence, TaskKind: t.TaskKind, Graph: cloneScenarioGraph(t.Graph)}
}
func validateTaskDialog040(t TaskState, saving bool) bool {
	issues := validateTask040(t)
	if t.TaskKind == 1 {
		g := ensureScenarioGraph(t.Graph, t)
		for _, gi := range validateScenarioGraph(g) {
			issues = append(issues, ValidationIssue040{Level: gi.Level, Message: gi.Message})
		}
	}
	errs, warns := 0, 0
	lines := []string{}
	for _, i := range issues {
		if i.Level == 2 {
			errs++
		} else {
			warns++
		}
		if len(lines) < 7 {
			lines = append(lines, "• "+i.Message)
		}
	}
	if len(issues) == 0 {
		return true
	}
	title := fmt.Sprintf("Проверка задачи · ошибок %d · предупреждений %d", errs, warns)
	if errs > 0 {
		message(title, strings.Join(lines, "\n"), MB_OK|MB_ICONERROR)
		return false
	}
	if !saving {
		message(title, strings.Join(lines, "\n"), MB_OK|MB_ICONINFORMATION)
		return true
	}
	r, _, _ := pMessageBoxW.Call(app.hwnd, uintptr(unsafe.Pointer(wstr(strings.Join(lines, "\n")+"\n\nСохранить всё равно?"))), uintptr(unsafe.Pointer(wstr(title))), MB_YESNO|MB_ICONQUESTION)
	return r == IDYES
}
