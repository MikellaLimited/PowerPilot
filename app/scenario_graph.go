//go:build windows

package main

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

const scenarioGraphVersion = 1

const (
	graphNodeTrigger = iota
	graphNodeCondition
	graphNodeAction
	graphNodeWait
	graphNodeFinish
)

const (
	graphPortNext  = "next"
	graphPortTrue  = "true"
	graphPortFalse = "false"
	graphPortError = "error"
)

type ScenarioGraph struct {
	Version int                 `json:"version"`
	Nodes   []ScenarioGraphNode `json:"nodes"`
	Edges   []ScenarioGraphEdge `json:"edges"`
	ViewX   float64             `json:"view_x,omitempty"`
	ViewY   float64             `json:"view_y,omitempty"`
	Zoom    float64             `json:"zoom,omitempty"`
}

type ScenarioGraphNode struct {
	ID         string                `json:"id"`
	Kind       int                   `json:"kind"`
	Title      string                `json:"title,omitempty"`
	X          float64               `json:"x"`
	Y          float64               `json:"y"`
	Mode       int                   `json:"mode,omitempty"`
	DelayHours int                   `json:"delay_hours,omitempty"`
	DelayMins  int                   `json:"delay_minutes,omitempty"`
	DelaySecs  int                   `json:"delay_seconds,omitempty"`
	Exact      string                `json:"exact,omitempty"`
	IdleSecs   int                   `json:"idle_seconds,omitempty"`
	Process    string                `json:"process,omitempty"`
	Recurrence RecurrenceSpec        `json:"recurrence,omitempty"`
	Conditions []AutomationCondition `json:"conditions,omitempty"`
	Logic      int                   `json:"logic,omitempty"`
	Steps      []ActionStep          `json:"steps,omitempty"`
	WaitSecs   int                   `json:"wait_seconds,omitempty"`
	Action     int                   `json:"action,omitempty"`
}

type ScenarioGraphEdge struct {
	ID       string `json:"id"`
	From     string `json:"from"`
	FromPort string `json:"from_port"`
	To       string `json:"to"`
}

type GraphValidationIssue struct {
	Level   int
	NodeID  string
	Message string
}

func cloneScenarioGraph(src ScenarioGraph) ScenarioGraph {
	out := src
	out.Nodes = append([]ScenarioGraphNode(nil), src.Nodes...)
	out.Edges = append([]ScenarioGraphEdge(nil), src.Edges...)
	for i := range out.Nodes {
		out.Nodes[i].Conditions = append([]AutomationCondition(nil), src.Nodes[i].Conditions...)
		out.Nodes[i].Steps = cloneActionSteps(src.Nodes[i].Steps)
	}
	return out
}

func graphNodeKindName(kind int) string {
	switch kind {
	case graphNodeTrigger:
		return "Триггер"
	case graphNodeCondition:
		return "Условия"
	case graphNodeAction:
		return "Действия"
	case graphNodeWait:
		return "Ожидание"
	case graphNodeFinish:
		return "Завершение"
	default:
		return "Блок"
	}
}

func graphNodePorts(kind int) []string {
	switch kind {
	case graphNodeTrigger, graphNodeWait:
		return []string{graphPortNext}
	case graphNodeCondition:
		return []string{graphPortTrue, graphPortFalse, graphPortError}
	case graphNodeAction:
		return []string{graphPortNext, graphPortError}
	}
	return nil
}

func graphPortName(port string) string {
	switch port {
	case graphPortTrue:
		return "Да"
	case graphPortFalse:
		return "Нет"
	case graphPortError:
		return "Ошибка"
	default:
		return "Далее"
	}
}

func newScenarioGraphNode(kind int, x, y float64) ScenarioGraphNode {
	n := ScenarioGraphNode{ID: newAutomationID("node"), Kind: kind, X: x, Y: y, Logic: logicAND}
	switch kind {
	case graphNodeTrigger:
		n.Mode, n.DelayMins = 0, 30
	case graphNodeCondition:
		n.Conditions = []AutomationCondition{{ID: newAutomationID("cond"), Type: condCPU, Logic: logicAND, Compare: -1, Threshold: 10, HoldSeconds: 30, Enabled: true}}
	case graphNodeAction:
		n.Steps = []ActionStep{{ID: newAutomationID("step"), Type: stepNotify, Text: "Сценарий PowerPilot продолжает выполнение."}}
	case graphNodeWait:
		n.WaitSecs = 30
	case graphNodeFinish:
		n.Action = 4
	}
	return n
}

func graphFromLegacy(t TaskState) ScenarioGraph {
	trigger := newScenarioGraphNode(graphNodeTrigger, 60, 160)
	trigger.Mode = t.Mode
	trigger.DelayHours, trigger.DelayMins, trigger.DelaySecs = t.DelayHours, t.DelayMinutes, t.DelaySeconds
	trigger.Exact, trigger.IdleSecs, trigger.Process, trigger.Recurrence = t.Exact, t.IdleMinutes, t.WatchProcess, t.Recurrence
	finish := newScenarioGraphNode(graphNodeFinish, 800, 160)
	finish.Action = t.Action
	g := ScenarioGraph{Version: scenarioGraphVersion, Zoom: 1, Nodes: []ScenarioGraphNode{trigger}}
	previous := trigger.ID
	if len(t.Conditions) > 0 {
		cond := newScenarioGraphNode(graphNodeCondition, 310, 120)
		cond.Conditions = append([]AutomationCondition(nil), t.Conditions...)
		cond.Logic = t.TriggerLogic
		g.Nodes = append(g.Nodes, cond)
		g.Edges = append(g.Edges, ScenarioGraphEdge{ID: newAutomationID("edge"), From: previous, FromPort: graphPortNext, To: cond.ID})
		previous = cond.ID
	}
	if len(t.Steps) > 0 {
		action := newScenarioGraphNode(graphNodeAction, 560, 120)
		action.Steps = cloneActionSteps(t.Steps)
		g.Nodes = append(g.Nodes, action)
		port := graphPortNext
		if len(t.Conditions) > 0 {
			port = graphPortTrue
		}
		g.Edges = append(g.Edges, ScenarioGraphEdge{ID: newAutomationID("edge"), From: previous, FromPort: port, To: action.ID})
		previous = action.ID
	}
	g.Nodes = append(g.Nodes, finish)
	port := graphPortNext
	if len(t.Conditions) > 0 && len(t.Steps) == 0 {
		port = graphPortTrue
	}
	g.Edges = append(g.Edges, ScenarioGraphEdge{ID: newAutomationID("edge"), From: previous, FromPort: port, To: finish.ID})
	return g
}

func legacyTaskStateFromSettings(s Settings) TaskState {
	return TaskState{Action: s.Action, Mode: s.Mode, DelayHours: s.DelayHours, DelayMinutes: s.DelayMinutes, DelaySeconds: s.DelaySeconds,
		Exact: s.Exact, IdleMinutes: s.IdleMinutes, WatchProcess: s.WatchProcess, CloseBefore: s.CloseBefore,
		Processes: append([]string(nil), s.Processes...), WarningSeconds: s.WarningSeconds,
		Conditions: append([]AutomationCondition(nil), s.AdvancedConditions...), TriggerLogic: s.TriggerLogic,
		Steps: cloneActionSteps(s.ActionSteps), Recurrence: s.Recurrence, TaskKind: s.TaskKind}
}

func ensureScenarioGraph(g ScenarioGraph, legacy TaskState) ScenarioGraph {
	if g.Version <= 0 || len(g.Nodes) == 0 {
		return graphFromLegacy(legacy)
	}
	g.Version = scenarioGraphVersion
	if g.Zoom < .45 || g.Zoom > 2.2 {
		g.Zoom = 1
	}
	for i := range g.Nodes {
		if g.Nodes[i].ID == "" {
			g.Nodes[i].ID = newAutomationID("node")
		}
		if g.Nodes[i].Kind == graphNodeWait && g.Nodes[i].WaitSecs <= 0 {
			g.Nodes[i].WaitSecs = 30
		}
	}
	return g
}

func (g *ScenarioGraph) nodeIndex(id string) int {
	for i := range g.Nodes {
		if g.Nodes[i].ID == id {
			return i
		}
	}
	return -1
}

func (g *ScenarioGraph) node(id string) *ScenarioGraphNode {
	if i := g.nodeIndex(id); i >= 0 {
		return &g.Nodes[i]
	}
	return nil
}

func (g *ScenarioGraph) trigger() *ScenarioGraphNode {
	for i := range g.Nodes {
		if g.Nodes[i].Kind == graphNodeTrigger {
			return &g.Nodes[i]
		}
	}
	return nil
}

func (g *ScenarioGraph) finishNodes() []ScenarioGraphNode {
	out := []ScenarioGraphNode{}
	for _, n := range g.Nodes {
		if n.Kind == graphNodeFinish {
			out = append(out, n)
		}
	}
	return out
}

func (g *ScenarioGraph) outgoing(from, port string) *ScenarioGraphEdge {
	for i := range g.Edges {
		if g.Edges[i].From == from && g.Edges[i].FromPort == port {
			return &g.Edges[i]
		}
	}
	return nil
}

func (g *ScenarioGraph) connect(from, port, to string) {
	if from == "" || to == "" || from == to || g.node(from) == nil || g.node(to) == nil {
		return
	}
	for i := range g.Edges {
		if g.Edges[i].From == from && g.Edges[i].FromPort == port {
			g.Edges[i].To = to
			return
		}
	}
	g.Edges = append(g.Edges, ScenarioGraphEdge{ID: newAutomationID("edge"), From: from, FromPort: port, To: to})
}

func (g *ScenarioGraph) removeNode(id string) {
	i := g.nodeIndex(id)
	if i < 0 {
		return
	}
	g.Nodes = append(g.Nodes[:i], g.Nodes[i+1:]...)
	edges := g.Edges[:0]
	for _, e := range g.Edges {
		if e.From != id && e.To != id {
			edges = append(edges, e)
		}
	}
	g.Edges = edges
}

func validateScenarioGraph(g ScenarioGraph) []GraphValidationIssue {
	issues := []GraphValidationIssue{}
	if len(g.Nodes) == 0 {
		return []GraphValidationIssue{{Level: 2, Message: "Схема не содержит блоков"}}
	}
	ids := map[string]ScenarioGraphNode{}
	triggers := 0
	finishes := 0
	for _, n := range g.Nodes {
		if n.ID == "" || ids[n.ID].ID != "" {
			issues = append(issues, GraphValidationIssue{2, n.ID, "У блока отсутствует уникальный идентификатор"})
		}
		ids[n.ID] = n
		if n.Kind == graphNodeTrigger {
			triggers++
		}
		if n.Kind == graphNodeFinish {
			finishes++
		}
		if n.Kind == graphNodeCondition && len(n.Conditions) == 0 {
			issues = append(issues, GraphValidationIssue{1, n.ID, "Блок условий пуст"})
		}
		if n.Kind == graphNodeAction && len(n.Steps) == 0 {
			issues = append(issues, GraphValidationIssue{1, n.ID, "Блок действий пуст"})
		}
		if n.Kind == graphNodeWait && n.WaitSecs <= 0 {
			issues = append(issues, GraphValidationIssue{2, n.ID, "Время ожидания должно быть больше нуля"})
		}
		if n.Kind == graphNodeFinish && (n.Action < 0 || n.Action > 4) {
			issues = append(issues, GraphValidationIssue{2, n.ID, "В блоке завершения не выбрано действие"})
		}
	}
	if triggers != 1 {
		issues = append(issues, GraphValidationIssue{2, "", "В первой версии схемы должен быть ровно один триггер"})
	}
	if finishes == 0 {
		issues = append(issues, GraphValidationIssue{2, "", "Добавьте хотя бы один блок завершения"})
	}
	outgoing := map[string]bool{}
	adj := map[string][]string{}
	for _, e := range g.Edges {
		if ids[e.From].ID == "" || ids[e.To].ID == "" {
			issues = append(issues, GraphValidationIssue{2, e.From, "Соединение указывает на отсутствующий блок"})
			continue
		}
		validPort := false
		for _, port := range graphNodePorts(ids[e.From].Kind) {
			if port == e.FromPort {
				validPort = true
				break
			}
		}
		if !validPort {
			issues = append(issues, GraphValidationIssue{2, e.From, "Соединение использует недоступный выход блока"})
		}
		key := e.From + "\x00" + e.FromPort
		if outgoing[key] {
			issues = append(issues, GraphValidationIssue{2, e.From, "Один выход соединён с несколькими блоками"})
		}
		outgoing[key] = true
		adj[e.From] = append(adj[e.From], e.To)
	}
	var triggerID string
	for _, n := range g.Nodes {
		if n.Kind == graphNodeTrigger {
			triggerID = n.ID
		}
	}
	if triggerID != "" {
		reachable := map[string]bool{}
		var visit func(string)
		visit = func(id string) {
			if reachable[id] {
				return
			}
			reachable[id] = true
			for _, next := range adj[id] {
				visit(next)
			}
		}
		visit(triggerID)
		for _, n := range g.Nodes {
			if !reachable[n.ID] {
				issues = append(issues, GraphValidationIssue{1, n.ID, "Блок недостижим от триггера"})
			}
		}
	}
	state := map[string]int{}
	var cycle func(string) bool
	cycle = func(id string) bool {
		state[id] = 1
		for _, next := range adj[id] {
			if state[next] == 1 || (state[next] == 0 && cycle(next)) {
				return true
			}
		}
		state[id] = 2
		return false
	}
	for id := range ids {
		if state[id] == 0 && cycle(id) {
			issues = append(issues, GraphValidationIssue{2, id, "Циклы появятся вместе с безопасным блоком повтора; произвольный цикл запрещён"})
			break
		}
	}
	return issues
}

func graphLegacyState(g ScenarioGraph, base TaskState) TaskState {
	out := base
	if tr := g.trigger(); tr != nil {
		out.Mode = tr.Mode
		out.DelayHours, out.DelayMinutes, out.DelaySeconds = tr.DelayHours, tr.DelayMins, tr.DelaySecs
		out.Exact, out.IdleMinutes, out.WatchProcess, out.Recurrence = tr.Exact, tr.IdleSecs, tr.Process, tr.Recurrence
	}
	nodes := append([]ScenarioGraphNode(nil), g.Nodes...)
	sort.SliceStable(nodes, func(i, j int) bool {
		if nodes[i].X == nodes[j].X {
			return nodes[i].Y < nodes[j].Y
		}
		return nodes[i].X < nodes[j].X
	})
	out.Conditions = nil
	out.Steps = nil
	for _, n := range nodes {
		switch n.Kind {
		case graphNodeCondition:
			out.Conditions = append(out.Conditions, n.Conditions...)
			out.TriggerLogic = n.Logic
		case graphNodeAction:
			out.Steps = append(out.Steps, cloneActionSteps(n.Steps)...)
		case graphNodeFinish:
			out.Action = n.Action
		}
	}
	return out
}

func graphNodeSummary(n ScenarioGraphNode) string {
	if strings.TrimSpace(n.Title) != "" {
		return n.Title
	}
	switch n.Kind {
	case graphNodeTrigger:
		return graphTriggerSummary(n)
	case graphNodeCondition:
		if len(n.Conditions) == 0 {
			return "Добавьте условия"
		}
		if len(n.Conditions) == 1 {
			return conditionSummary(n.Conditions[0])
		}
		return fmt.Sprintf("%d условий", len(n.Conditions))
	case graphNodeAction:
		if len(n.Steps) == 0 {
			return "Добавьте действия"
		}
		if len(n.Steps) == 1 {
			return stepSummary(n.Steps[0])
		}
		return fmt.Sprintf("%d действий", len(n.Steps))
	case graphNodeWait:
		return "Пауза " + formatDuration(time.Duration(max(n.WaitSecs, 1))*time.Second)
	case graphNodeFinish:
		return powerActionName(n.Action)
	}
	return ""
}

func graphTriggerSummary(n ScenarioGraphNode) string {
	switch n.Mode {
	case 0:
		return fmt.Sprintf("Таймер %02d:%02d:%02d", n.DelayHours, n.DelayMins, n.DelaySecs)
	case 1:
		return "Дата и время · " + n.Exact
	case 2:
		return fmt.Sprintf("Простой %d сек", max(n.IdleSecs, 1))
	case 3:
		return "После процесса · " + n.Process
	case 4:
		return "Расписание · " + n.Recurrence.TimeHHMM
	case 5:
		return "Сразу после запуска"
	}
	return "Триггер"
}

func scenarioGraphValidationError(g ScenarioGraph) string {
	for _, issue := range validateScenarioGraph(g) {
		if issue.Level >= 2 {
			return issue.Message
		}
	}
	return ""
}

// executeScenarioGraph walks the connected blocks instead of flattening the
// diagram into the old condition/action lists. This keeps branches meaningful:
// a condition selects Да/Нет, while a failed action may follow Ошибка.
func executeScenarioGraph(s Schedule) (int, bool) {
	g := cloneScenarioGraph(s.graph)
	trigger := g.trigger()
	if trigger == nil {
		return s.action, false
	}
	next := func(nodeID, port string) string {
		if edge := g.outgoing(nodeID, port); edge != nil {
			return edge.To
		}
		return ""
	}
	nodeID := next(trigger.ID, graphPortNext)
	for visited := 0; nodeID != "" && visited < 256; visited++ {
		node := g.node(nodeID)
		if node == nil {
			return s.action, false
		}
		appendRunHistory("GRAPH_NODE", graphNodeKindName(node.Kind)+" · "+graphNodeSummary(*node), s.runID)
		switch node.Kind {
		case graphNodeTrigger:
			nodeID = next(node.ID, graphPortNext)
		case graphNodeCondition:
			ok, _ := evaluateAutomationConditions(node.Conditions)
			if ok {
				nodeID = next(node.ID, graphPortTrue)
				continue
			}
			if branch := next(node.ID, graphPortFalse); branch != "" {
				nodeID = branch
				continue
			}
			// A condition with only a Да output behaves like the legacy scheduler:
			// it waits instead of treating a temporary false value as an error.
			for !ok {
				time.Sleep(250 * time.Millisecond)
				ok, _ = evaluateAutomationConditions(node.Conditions)
			}
			nodeID = next(node.ID, graphPortTrue)
		case graphNodeAction:
			stepSchedule := s
			stepSchedule.steps = cloneActionSteps(node.Steps)
			if executeScenarioSteps(stepSchedule) {
				nodeID = next(node.ID, graphPortNext)
			} else if branch := next(node.ID, graphPortError); branch != "" {
				nodeID = branch
			} else {
				return s.action, false
			}
		case graphNodeWait:
			time.Sleep(time.Duration(max(node.WaitSecs, 1)) * time.Second)
			nodeID = next(node.ID, graphPortNext)
		case graphNodeFinish:
			return node.Action, true
		default:
			return s.action, false
		}
	}
	return s.action, false
}
