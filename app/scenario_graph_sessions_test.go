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
