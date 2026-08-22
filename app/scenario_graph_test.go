//go:build windows

package main

import "testing"

func TestScenarioGraphMigratesLegacyTask(t *testing.T) {
	legacy := TaskState{
		TaskKind: 1, Mode: 5, Action: 4,
		Conditions: []AutomationCondition{{ID: "condition", Type: condCPU, Enabled: true}},
		Steps:      []ActionStep{{ID: "action", Type: stepNotify, Text: "done"}},
	}
	g := graphFromLegacy(legacy)
	if len(g.Nodes) != 3 || len(g.Edges) != 2 {
		t.Fatalf("migration produced %d nodes and %d edges", len(g.Nodes), len(g.Edges))
	}
	if reason := scenarioGraphValidationError(g); reason != "" {
		t.Fatalf("migrated graph is invalid: %s", reason)
	}
	state := graphLegacyState(g, TaskState{})
	if len(state.Conditions) != 1 || len(state.Steps) != 1 || state.Action != 4 || state.Mode != 5 {
		t.Fatalf("legacy projection lost task data: %#v", state)
	}
}

func TestScenarioGraphV2MergesOldTriggerAndCondition(t *testing.T) {
	trigger := newScenarioGraphNode(graphNodeTrigger, 0, 0)
	trigger.Conditions = nil
	condition := newScenarioGraphNode(graphNodeCondition, 200, 0)
	action := newScenarioGraphNode(graphNodeAction, 400, 0)
	old := ScenarioGraph{
		Version: 1,
		Nodes:   []ScenarioGraphNode{trigger, condition, action},
		Edges: []ScenarioGraphEdge{
			{ID: "a", From: trigger.ID, FromPort: graphPortNext, To: condition.ID},
			{ID: "b", From: condition.ID, FromPort: graphPortTrue, To: action.ID},
		},
		Zoom: 1,
	}
	got := ensureScenarioGraph(old, TaskState{})
	if got.Version != scenarioGraphVersion || len(got.Nodes) != 2 || len(got.Edges) != 1 {
		t.Fatalf("unexpected migrated graph: version=%d nodes=%d edges=%d", got.Version, len(got.Nodes), len(got.Edges))
	}
	merged := got.trigger()
	if merged == nil || len(merged.Conditions) != 1 || got.Edges[0].From != merged.ID || got.Edges[0].To != action.ID || got.Edges[0].FromPort != graphPortNext {
		t.Fatalf("old trigger/condition data was not merged: %#v", got)
	}
}

func TestScenarioGraphSupportsFanOutAndVariableLogicInputs(t *testing.T) {
	g := graphFromLegacy(TaskState{TaskKind: 1, Mode: 5, Action: 4})
	trigger := g.trigger()
	logic := newScenarioGraphNode(graphNodeLogic, 300, 100)
	logic.LogicOp = graphLogicOR
	other := newScenarioGraphNode(graphNodeTrigger, 80, 300)
	other.Mode = 5
	g.Nodes = append(g.Nodes, logic, other)
	g.connect(trigger.ID, graphPortNext, logic.ID)
	g.connect(other.ID, graphPortNext, logic.ID)
	if got := len(g.outgoingAll(trigger.ID, graphPortNext)); got != 2 {
		t.Fatalf("fan-out must retain both wires, got %d", got)
	}
	if reason := scenarioGraphValidationError(g); reason != "" {
		t.Fatalf("multi-input logic graph is invalid: %s", reason)
	}
}

func TestEvaluateGraphLogic(t *testing.T) {
	tests := []struct {
		op     int
		inputs []bool
		want   bool
	}{
		{graphLogicAND, []bool{true, true, true}, true},
		{graphLogicAND, []bool{true, false, true}, false},
		{graphLogicOR, []bool{false, false, true}, true},
		{graphLogicNOT, []bool{false}, true},
		{graphLogicXOR, []bool{true, true, true}, true},
		{graphLogicNAND, []bool{true, true}, false},
		{graphLogicNOR, []bool{false, false}, true},
	}
	for _, tt := range tests {
		if got := evaluateGraphLogic(tt.op, tt.inputs); got != tt.want {
			t.Fatalf("%s(%v) = %v, want %v", graphLogicName(tt.op), tt.inputs, got, tt.want)
		}
	}
}

func TestScenarioGraphRejectsCycle(t *testing.T) {
	g := graphFromLegacy(TaskState{TaskKind: 1, Mode: 0, DelayMinutes: 1, Action: 4})
	trigger := g.trigger()
	finish := g.finishNodes()[0]
	g.Edges = append(g.Edges, ScenarioGraphEdge{ID: "cycle", From: finish.ID, FromPort: graphPortNext, To: trigger.ID})
	if scenarioGraphValidationError(g) == "" {
		t.Fatal("expected cycle validation error")
	}
}

func TestScenarioGraphCloneIsDeep(t *testing.T) {
	g := graphFromLegacy(TaskState{TaskKind: 1, Conditions: []AutomationCondition{{ID: "condition", Type: condCPU}}, Steps: []ActionStep{{ID: "action", Type: stepWait}}})
	clone := cloneScenarioGraph(g)
	for i := range clone.Nodes {
		if len(clone.Nodes[i].Conditions) > 0 {
			clone.Nodes[i].Conditions[0].Text = "changed"
		}
		if len(clone.Nodes[i].Steps) > 0 {
			clone.Nodes[i].Steps[0].Text = "changed"
		}
	}
	for _, node := range g.Nodes {
		if len(node.Conditions) > 0 && node.Conditions[0].Text == "changed" {
			t.Fatal("condition slice was shared")
		}
		if len(node.Steps) > 0 && node.Steps[0].Text == "changed" {
			t.Fatal("step slice was shared")
		}
	}
}
