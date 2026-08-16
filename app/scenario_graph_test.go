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
	if len(g.Nodes) != 4 || len(g.Edges) != 3 {
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
