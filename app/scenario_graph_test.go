//go:build windows

package main

import (
	"math"
	"reflect"
	"testing"
)

func TestScenarioGraphAlignedNodesHaveStraightConnection(t *testing.T) {
	oldCanvas := app.graphCanvasRect
	app.graphCanvasRect = RECT{Left: 10, Top: 20, Right: 1200, Bottom: 800}
	t.Cleanup(func() { app.graphCanvasRect = oldCanvas })

	g := ScenarioGraph{Zoom: 1}
	condition := newScenarioGraphNode(graphNodeCondition, 48, 96)
	action := newScenarioGraphNode(graphNodeAction, 336, 96)
	_, outputY := graphOutputPoint(&g, condition, graphPortNext)
	_, inputY := graphInputPoint(&g, action)
	if math.Abs(float64(outputY-inputY)) > .01 {
		t.Fatalf("aligned nodes produced a bent connection: output=%v input=%v", outputY, inputY)
	}
}

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

func TestScenarioGraphUsesOneFunctionPerBlock(t *testing.T) {
	legacy := TaskState{
		TaskKind: 1, Mode: 0, DelayMinutes: 5, Action: 4, TriggerLogic: logicAND,
		Conditions: []AutomationCondition{
			{ID: "cpu", Type: condCPU, Enabled: true},
			{ID: "gpu", Type: condGPU, Enabled: true},
			{ID: "disk", Type: condDisk, Enabled: true},
		},
		Steps: []ActionStep{
			{ID: "notify", Type: stepNotify, Text: "ready"},
			{ID: "wait", Type: stepWait, Value: 10},
		},
	}
	g := graphFromLegacy(legacy)
	triggers, actions, logicNodes := 0, 0, 0
	for _, node := range g.Nodes {
		switch node.Kind {
		case graphNodeTrigger:
			triggers++
			if len(node.Conditions) > 1 {
				t.Fatalf("conditions node contains %d functions", len(node.Conditions))
			}
		case graphNodeAction:
			actions++
			if len(node.Steps) != 1 {
				t.Fatalf("action node contains %d functions", len(node.Steps))
			}
		case graphNodeLogic:
			logicNodes++
		}
	}
	if triggers != 3 || actions != 2 || logicNodes != 1 {
		t.Fatalf("unexpected graph shape: conditions=%d actions=%d logic=%d", triggers, actions, logicNodes)
	}
	state := graphLegacyState(g, TaskState{})
	if len(state.Conditions) != 3 || len(state.Steps) != 2 {
		t.Fatalf("legacy projection lost separate functions: %#v", state)
	}
}

func TestScenarioGraphV2MergesOldTriggerAndCondition(t *testing.T) {
	trigger := newScenarioGraphNode(graphNodeTrigger, 0, 0)
	trigger.Conditions = nil
	condition := newScenarioGraphNode(graphNodeCondition, 200, 0)
	condition.Conditions[0].Threshold = 25
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

func TestScenarioGraphV4RemovesGeneratedDefaultCondition(t *testing.T) {
	trigger := ScenarioGraphNode{ID: "trigger", Kind: graphNodeTrigger, Conditions: []AutomationCondition{{ID: "generated", Type: condCPU, Compare: -1, Threshold: 10, HoldSeconds: 30, Enabled: true}}}
	g := ensureScenarioGraph(ScenarioGraph{Version: 3, Zoom: 1, Nodes: []ScenarioGraphNode{trigger}}, TaskState{})
	if len(g.Nodes[0].Conditions) != 0 {
		t.Fatalf("generated default condition survived migration: %#v", g.Nodes[0].Conditions)
	}
}

func TestPruneSingleInputJunctionReconnectsWire(t *testing.T) {
	from := newScenarioGraphNode(graphNodeTrigger, 0, 0)
	junction := newScenarioGraphNode(graphNodeJunction, 100, 0)
	to := newScenarioGraphNode(graphNodeAction, 200, 0)
	g := ScenarioGraph{Version: scenarioGraphVersion, Nodes: []ScenarioGraphNode{from, junction, to}}
	g.connect(from.ID, graphPortNext, junction.ID)
	g.connect(junction.ID, graphPortNext, to.ID)
	if !pruneSingleInputJunctions(&g) || len(g.Nodes) != 2 || len(g.Edges) != 1 || g.Edges[0].From != from.ID || g.Edges[0].To != to.ID {
		t.Fatalf("junction was not simplified: %#v", g)
	}
}

func TestSecondWireToRegularInputCreatesJunction(t *testing.T) {
	first := newScenarioGraphNode(graphNodeTrigger, 0, 0)
	second := newScenarioGraphNode(graphNodeTrigger, 0, 120)
	target := newScenarioGraphNode(graphNodeAction, 300, 0)
	g := ScenarioGraph{Version: scenarioGraphVersion, Zoom: 1, Nodes: []ScenarioGraphNode{first, second, target}}
	if !connectGraphWireToInput(&g, first.ID, graphPortNext, target.ID) {
		t.Fatal("first wire was not connected")
	}
	if !connectGraphWireToInput(&g, second.ID, graphPortNext, target.ID) {
		t.Fatal("second wire was not connected")
	}
	junctions := 0
	junctionID := ""
	for _, node := range g.Nodes {
		if node.Kind == graphNodeJunction {
			junctions++
			junctionID = node.ID
			if math.Mod(node.Y+19, 24) != 0 {
				t.Fatalf("junction port is not aligned to the wire grid: y=%v", node.Y)
			}
		}
	}
	if junctions != 1 {
		t.Fatalf("expected one visible junction, got %d", junctions)
	}
	incoming, outgoing := 0, 0
	for _, edge := range g.Edges {
		if edge.To == junctionID {
			incoming++
		}
		if edge.From == junctionID && edge.To == target.ID {
			outgoing++
		}
		if edge.To == target.ID && edge.From != junctionID {
			t.Fatalf("regular input still has a hidden direct wire: %#v", edge)
		}
	}
	if incoming != 2 || outgoing != 1 {
		t.Fatalf("wrong junction topology: incoming=%d outgoing=%d graph=%#v", incoming, outgoing, g)
	}
}

func TestNotRequiresExactlyOneInput(t *testing.T) {
	first := newScenarioGraphNode(graphNodeTrigger, 0, 0)
	second := newScenarioGraphNode(graphNodeTrigger, 0, 120)
	not := newScenarioGraphNode(graphNodeLogic, 240, 0)
	not.LogicOp = graphLogicNOT
	finish := newScenarioGraphNode(graphNodeFinish, 480, 0)
	finish.Action = 4
	g := ScenarioGraph{Version: scenarioGraphVersion, Zoom: 1, Nodes: []ScenarioGraphNode{first, second, not, finish}}
	g.connect(first.ID, graphPortNext, not.ID)
	g.connect(second.ID, graphPortNext, not.ID)
	g.connect(not.ID, graphPortNext, finish.ID)
	if scenarioGraphValidationError(g) == "" {
		t.Fatal("NOT with two inputs must be rejected instead of silently ignoring one")
	}
}

func TestEmptyConditionGroupDoesNotBlockActions(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())
	trigger := newScenarioGraphNode(graphNodeTrigger, 0, 0)
	trigger.Conditions = []AutomationCondition{{ID: "empty-group", Type: condGroup, Enabled: true}}
	action := newScenarioGraphNode(graphNodeAction, 200, 0)
	action.Steps = []ActionStep{
		{ID: "notify", Type: stepNotify, Text: "test"},
		{ID: "monitor-off", Type: stepMonitorOff},
		{ID: "monitor-on", Type: stepMonitorOn},
	}
	finish := newScenarioGraphNode(graphNodeFinish, 400, 0)
	finish.Action = 4
	g := ScenarioGraph{Version: scenarioGraphVersion, Zoom: 1, Nodes: []ScenarioGraphNode{trigger, action, finish}}
	g.connect(trigger.ID, graphPortNext, action.ID)
	g.connect(action.ID, graphPortNext, finish.ID)
	g = ensureScenarioGraph(g, TaskState{})
	if len(g.Nodes[0].Conditions) != 0 {
		t.Fatalf("empty placeholder group was not removed: %#v", g.Nodes[0].Conditions)
	}
	called := false
	result, finished := executeScenarioGraphWithStepRunner(Schedule{action: 0, graph: g}, func(schedule Schedule) bool {
		called = true
		if len(schedule.steps) != 3 || schedule.steps[0].Type != stepNotify || schedule.steps[1].Type != stepMonitorOff || schedule.steps[2].Type != stepMonitorOn {
			t.Fatalf("wrong actions dispatched: %#v", schedule.steps)
		}
		return true
	})
	if !called || !finished || result != 4 {
		t.Fatalf("actions were not dispatched through the graph: called=%v finished=%v result=%d", called, finished, result)
	}
}

func TestScenarioSessionOwnsEveryLayoutRect(t *testing.T) {
	typeOfApp := reflect.TypeOf(App{})
	rectType := reflect.TypeOf(RECT{})
	for i := 0; i < typeOfApp.NumField(); i++ {
		field := typeOfApp.Field(i)
		isGeometry := field.Type == rectType || (field.Type.Kind() == reflect.Array && field.Type.Elem() == rectType)
		if isGeometry && !graphSessionOwnsField(field) {
			t.Fatalf("layout field %s is shared between windows", field.Name)
		}
	}
}
