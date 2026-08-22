//go:build windows

package main

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"time"
	"unsafe"
)

type scenarioGraphSession struct {
	HWND          uintptr
	Graph         ScenarioGraph
	TargetID      string
	SavedTask     bool
	TaskName      string
	Undo          []ScenarioGraph
	Redo          []ScenarioGraph
	Current       ScenarioGraph
	Fingerprint   string
	Applying      bool
	CloseApproved bool
	Discard       bool
	UI            App
	EditorEdits   map[int]uintptr
	Closed        bool
}

var scenarioGraphSessions = map[uintptr]*scenarioGraphSession{}
var activeScenarioGraphSession *scenarioGraphSession

func graphSessionOwnsField(name string) bool {
	prefixes := []string{
		"graph", "condition", "step", "editor", "blockEditor", "process", "recurrence",
		"picker",
		"whenField", "warningField", "timeField", "exactField", "modeRects", "powerPlanRects",
		"selectedMode", "selectedAction", "editingCondition", "editingStep", "mouseX", "mouseY",
		"pageAnim", "subRevealAnim", "section", "scenarioSavedDraft", "savedEditDraft",
		"currentTask", "confirmSystem", "draggingScrollKind", "pickRect",
	}
	for _, prefix := range prefixes {
		if len(name) >= len(prefix) && name[:len(prefix)] == prefix {
			return true
		}
	}
	return false
}

func copyGraphSessionUI(dst, src *App) {
	dv, sv := reflect.ValueOf(dst).Elem(), reflect.ValueOf(src).Elem()
	t := dv.Type()
	for i := 0; i < dv.NumField(); i++ {
		if !graphSessionOwnsField(t.Field(i).Name) {
			continue
		}
		// App intentionally keeps its Win32 state package-private. reflect marks
		// those fields read-only even inside this package, so address the two
		// concrete App values directly. Both values are local/addressable and the
		// source and destination field types are identical.
		dstField := reflect.NewAt(dv.Field(i).Type(), unsafe.Pointer(dv.Field(i).UnsafeAddr())).Elem()
		srcField := reflect.NewAt(sv.Field(i).Type(), unsafe.Pointer(sv.Field(i).UnsafeAddr())).Elem()
		dstField.Set(srcField)
	}
}

func withScenarioGraphSession(hwnd uintptr, fn func() uintptr) uintptr {
	session := scenarioGraphSessions[hwnd]
	if session == nil {
		return fn()
	}
	var mainUI App
	copyGraphSessionUI(&mainUI, &app)
	mainGraphEditorEdits := graphEditorEdits
	copyGraphSessionUI(&app, &session.UI)
	app.graphWindow = hwnd
	graphEditorEdits = session.EditorEdits
	activeScenarioGraphSession = session
	result := fn()
	copyGraphSessionUI(&session.UI, &app)
	session.EditorEdits = graphEditorEdits
	activeScenarioGraphSession = nil
	graphEditorEdits = mainGraphEditorEdits
	copyGraphSessionUI(&app, &mainUI)
	return result
}

func currentScenarioGraphSession() *scenarioGraphSession {
	return activeScenarioGraphSession
}

func scenarioGraphFingerprint(graph ScenarioGraph) string {
	data, _ := json.Marshal(graph)
	return string(data)
}

func (session *scenarioGraphSession) observeGraphChange() {
	if session == nil || session.Applying {
		return
	}
	fingerprint := scenarioGraphFingerprint(session.Graph)
	if fingerprint == session.Fingerprint {
		return
	}
	session.Undo = append(session.Undo, cloneScenarioGraph(session.Current))
	if len(session.Undo) > 100 {
		session.Undo = session.Undo[len(session.Undo)-100:]
	}
	session.Redo = nil
	session.Current = cloneScenarioGraph(session.Graph)
	session.Fingerprint = fingerprint
}

func undoCurrentScenarioGraph() bool {
	session := currentScenarioGraphSession()
	if session == nil || len(session.Undo) == 0 {
		return false
	}
	previous := session.Undo[len(session.Undo)-1]
	session.Undo = session.Undo[:len(session.Undo)-1]
	session.Redo = append(session.Redo, cloneScenarioGraph(session.Graph))
	session.Applying = true
	session.Graph = cloneScenarioGraph(previous)
	session.Current = cloneScenarioGraph(previous)
	session.Fingerprint = scenarioGraphFingerprint(previous)
	session.Applying = false
	selectOnlyGraphNode("")
	invalidateScenarioGraphWindows()
	return true
}

func redoCurrentScenarioGraph() bool {
	session := currentScenarioGraphSession()
	if session == nil || len(session.Redo) == 0 {
		return false
	}
	next := session.Redo[len(session.Redo)-1]
	session.Redo = session.Redo[:len(session.Redo)-1]
	session.Undo = append(session.Undo, cloneScenarioGraph(session.Graph))
	session.Applying = true
	session.Graph = cloneScenarioGraph(next)
	session.Current = cloneScenarioGraph(next)
	session.Fingerprint = scenarioGraphFingerprint(next)
	session.Applying = false
	selectOnlyGraphNode("")
	invalidateScenarioGraphWindows()
	return true
}

func syncScenarioGraphSessionName(session *scenarioGraphSession) {
	if session == nil || app.graphNameEdit == 0 {
		return
	}
	if name := strings.TrimSpace(getText(app.graphNameEdit)); name != "" {
		session.TaskName = name
	}
}

func scenarioGraphTargetID() (string, bool) {
	if app.scenarioSavedDraft && app.savedEditDraft.ID != "" {
		return app.savedEditDraft.ID, true
	}
	return "", false
}

func scenarioGraphSessionForCurrentTarget() *scenarioGraphSession {
	id, saved := scenarioGraphTargetID()
	for _, session := range scenarioGraphSessions {
		if session.TargetID == id && session.SavedTask == saved {
			return session
		}
	}
	return nil
}

func saveScenarioGraphSession(session *scenarioGraphSession) {
	if session == nil {
		return
	}
	graph := ensureScenarioGraph(cloneScenarioGraph(session.Graph), TaskState{})
	if session.SavedTask {
		for i := range app.settings.SavedTasks {
			if app.settings.SavedTasks[i].ID != session.TargetID {
				continue
			}
			state := graphLegacyState(graph, taskStateFromSaved040(app.settings.SavedTasks[i]))
			applyTaskStateToSavedGraph(&app.settings.SavedTasks[i], state, graph)
			if strings.TrimSpace(session.TaskName) != "" {
				app.settings.SavedTasks[i].Name = strings.TrimSpace(session.TaskName)
			}
			if app.savedEditDraft.ID == session.TargetID {
				app.savedEditDraft = app.settings.SavedTasks[i]
			}
			break
		}
	} else {
		state := graphLegacyState(graph, legacyTaskStateFromSettings(app.settings))
		app.settings.ScenarioGraph = graph
		app.settings.Action, app.settings.Mode = state.Action, state.Mode
		app.settings.DelayHours, app.settings.DelayMinutes, app.settings.DelaySeconds = state.DelayHours, state.DelayMinutes, state.DelaySeconds
		app.settings.Exact, app.settings.IdleMinutes, app.settings.WatchProcess = state.Exact, state.IdleMinutes, state.WatchProcess
		app.settings.AdvancedConditions = append([]AutomationCondition(nil), state.Conditions...)
		app.settings.ActionSteps = cloneActionSteps(state.Steps)
		app.settings.TriggerLogic, app.settings.Recurrence = state.TriggerLogic, state.Recurrence
	}
	saveSettings()
	saveDraftAutosave()
	rebuildSavedFilter()
	if app.hwnd != 0 {
		if currentScenarioGraphSession() == nil {
			layoutControls(app.hwnd)
		}
		invalidate(app.hwnd)
	}
}

func saveScenarioGraphTaskSession(session *scenarioGraphSession) bool {
	if session == nil {
		return false
	}
	syncScenarioGraphSessionName(session)
	graph := ensureScenarioGraph(cloneScenarioGraph(session.Graph), TaskState{})
	if reason := scenarioGraphValidationError(graph); reason != "" {
		showNotification("PowerPilot", "Схему пока нельзя сохранить: "+reason)
		return false
	}
	if session.SavedTask {
		saveScenarioGraphSession(session)
		showNotification("PowerPilot", "Изменения задачи сохранены.")
		return true
	}
	state := graphLegacyState(graph, legacyTaskStateFromSettings(app.settings))
	name := strings.TrimSpace(session.TaskName)
	if name == "" || name == "Новая задача" {
		name = fmt.Sprintf("Задача %d", len(app.settings.SavedTasks)+1)
	}
	task := SavedTask{ID: fmt.Sprintf("%d", time.Now().UnixNano()), Name: name, TaskKind: 1}
	applyTaskStateToSavedGraph(&task, state, graph)
	app.settings.SavedTasks = append(app.settings.SavedTasks, task)
	session.TargetID, session.SavedTask, session.TaskName = task.ID, true, task.Name
	saveSettings()
	maintainWakeTimer(time.Now())
	appendHistory("SAVE", task.Name)
	rebuildSavedFilter()
	app.savedScroll, app.savedScrollPx, app.savedScrollTarget = 0, 0, 0
	if app.hwnd != 0 {
		if currentScenarioGraphSession() == nil {
			layoutControls(app.hwnd)
		}
		invalidate(app.hwnd)
	}
	showNotification("PowerPilot", "Задача сохранена: "+task.Name)
	return true
}

func applyTaskStateToSavedGraph(task *SavedTask, state TaskState, graph ScenarioGraph) {
	if task == nil {
		return
	}
	task.Action, task.Mode = state.Action, state.Mode
	task.DelayHours, task.DelayMinutes, task.DelaySeconds = state.DelayHours, state.DelayMinutes, state.DelaySeconds
	task.Exact, task.IdleMinutes, task.WatchProcess = state.Exact, state.IdleMinutes, state.WatchProcess
	task.Conditions = append([]AutomationCondition(nil), state.Conditions...)
	task.Steps = cloneActionSteps(state.Steps)
	task.WarningSeconds = state.WarningSeconds
	task.TaskKind = 1
	task.TriggerLogic, task.Recurrence, task.Graph = state.TriggerLogic, state.Recurrence, graph
}

func closeScenarioGraphSessionsForCurrentTarget() bool {
	session := scenarioGraphSessionForCurrentTarget()
	if session == nil {
		return false
	}
	pSendMessageW.Call(session.HWND, WM_CLOSE, 0, 0)
	return true
}

func closeAllScenarioGraphSessions() {
	handles := make([]uintptr, 0, len(scenarioGraphSessions))
	for hwnd := range scenarioGraphSessions {
		handles = append(handles, hwnd)
	}
	for _, hwnd := range handles {
		pSendMessageW.Call(hwnd, WM_CLOSE, 0, 0)
	}
}
