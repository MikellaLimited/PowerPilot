//go:build windows

package main

import "testing"

func TestGraphSessionUICopiesPrivateEditorState(t *testing.T) {
	source := App{}
	source.graphEditorNodeID = "node-a"
	source.graphSelectedEdgeID = "edge-a"
	source.conditionCatalogExpanded = true
	source.processFilter = 2
	source.pickerItems = []string{"alpha.exe"}
	source.section = 15

	destination := App{}
	copyGraphSessionUI(&destination, &source)
	if destination.graphEditorNodeID != source.graphEditorNodeID ||
		destination.graphSelectedEdgeID != source.graphSelectedEdgeID ||
		!destination.conditionCatalogExpanded || destination.processFilter != 2 ||
		len(destination.pickerItems) != 1 || destination.section != 15 {
		t.Fatalf("private editor state was not copied: %#v", destination)
	}
	if destination.hwnd != 0 || destination.settings.TaskKind != 0 {
		t.Fatal("shared main-window state must not be copied into an editor session")
	}
}

func TestGraphSessionUndoRedoRestoresGraph(t *testing.T) {
	graph := graphFromLegacy(TaskState{TaskKind: 1, Mode: 0, DelayMinutes: 5, Action: 4})
	session := &scenarioGraphSession{
		Graph:       cloneScenarioGraph(graph),
		Current:     cloneScenarioGraph(graph),
		Fingerprint: scenarioGraphFingerprint(graph),
	}
	previousSession := activeScenarioGraphSession
	activeScenarioGraphSession = session
	defer func() { activeScenarioGraphSession = previousSession }()

	originalX := session.Graph.Nodes[0].X
	session.Graph.Nodes[0].X += 125
	session.observeGraphChange()
	if !undoCurrentScenarioGraph() || session.Graph.Nodes[0].X != originalX {
		t.Fatalf("undo did not restore node position: got %.0f want %.0f", session.Graph.Nodes[0].X, originalX)
	}
	if !redoCurrentScenarioGraph() || session.Graph.Nodes[0].X != originalX+125 {
		t.Fatalf("redo did not restore changed position: got %.0f want %.0f", session.Graph.Nodes[0].X, originalX+125)
	}
}

func TestGraphSessionFindsAlreadyOpenSavedTask(t *testing.T) {
	previousSessions := scenarioGraphSessions
	previousSavedDraft, previousDraft := app.scenarioSavedDraft, app.savedEditDraft
	defer func() {
		scenarioGraphSessions = previousSessions
		app.scenarioSavedDraft, app.savedEditDraft = previousSavedDraft, previousDraft
	}()

	app.scenarioSavedDraft = true
	app.savedEditDraft.ID = "task-42"
	want := &scenarioGraphSession{HWND: 42, TargetID: "task-42", SavedTask: true}
	scenarioGraphSessions = map[uintptr]*scenarioGraphSession{want.HWND: want}
	if got := scenarioGraphSessionForCurrentTarget(); got != want {
		t.Fatalf("existing editor was not reused: got %#v want %#v", got, want)
	}
}

func TestDetachedDialogUsesEditorAsOwner(t *testing.T) {
	previousSession, previousMain := activeScenarioGraphSession, app.hwnd
	defer func() {
		activeScenarioGraphSession, app.hwnd = previousSession, previousMain
	}()

	app.hwnd = 10
	activeScenarioGraphSession = &scenarioGraphSession{HWND: 42}
	if got := dialogOwnerWindow(); got != 42 {
		t.Fatalf("detached dialog owner = %d, want editor window 42", got)
	}
	activeScenarioGraphSession = nil
	if got := dialogOwnerWindow(); got != 10 {
		t.Fatalf("main dialog owner = %d, want main window 10", got)
	}
}
