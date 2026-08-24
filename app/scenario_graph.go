//go:build windows

package main

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

const scenarioGraphVersion = 4

const (
	graphNodeTrigger = iota
	graphNodeCondition
	graphNodeAction
	graphNodeWait
	graphNodeFinish
	graphNodeLogic
	graphNodeJunction
)

const (
	graphLogicAND = iota
	graphLogicOR
	graphLogicNOT
	graphLogicXOR
	graphLogicNAND
	graphLogicNOR
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
	LogicOp    int                   `json:"logic_op,omitempty"`
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
		return "Условия"
	case graphNodeCondition:
		return "Условия"
	case graphNodeAction:
		return "Действия"
	case graphNodeWait:
		return "Ожидание"
	case graphNodeFinish:
		return "Завершение"
	case graphNodeLogic:
		return "Логика"
	case graphNodeJunction:
		return "Соединение"
	default:
		return "Блок"
	}
}

func graphNodePorts(kind int) []string {
	switch kind {
	case graphNodeTrigger, graphNodeCondition, graphNodeAction, graphNodeWait, graphNodeLogic, graphNodeJunction:
		return []string{graphPortNext}
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
		n.Conditions = nil
	case graphNodeCondition:
		n.Conditions = []AutomationCondition{{ID: newAutomationID("cond"), Type: condCPU, Logic: logicAND, Compare: -1, Threshold: 10, HoldSeconds: 30, Enabled: true}}
	case graphNodeAction:
		n.Steps = []ActionStep{{ID: newAutomationID("step"), Type: stepNotify, Text: "Сценарий PowerPilot продолжает выполнение."}}
	case graphNodeWait:
		n.WaitSecs = 30
	case graphNodeFinish:
		n.Action = 4
	case graphNodeLogic:
		n.LogicOp = graphLogicAND
	}
	return n
}

func graphFromLegacy(t TaskState) ScenarioGraph {
	finish := newScenarioGraphNode(graphNodeFinish, 800, 160)
	finish.Action = t.Action
	g := ScenarioGraph{Version: scenarioGraphVersion, Zoom: 1}
	conditionCount := max(1, len(t.Conditions))
	triggerIDs := make([]string, 0, conditionCount)
	for i := 0; i < conditionCount; i++ {
		trigger := newScenarioGraphNode(graphNodeTrigger, 60, 80+float64(i)*130)
		trigger.Mode = t.Mode
		trigger.DelayHours, trigger.DelayMins, trigger.DelaySecs = t.DelayHours, t.DelayMinutes, t.DelaySeconds
		trigger.Exact, trigger.IdleSecs, trigger.Process, trigger.Recurrence = t.Exact, t.IdleMinutes, t.WatchProcess, t.Recurrence
		trigger.Conditions = nil
		if i < len(t.Conditions) {
			trigger.Conditions = []AutomationCondition{t.Conditions[i]}
		}
		g.Nodes = append(g.Nodes, trigger)
		triggerIDs = append(triggerIDs, trigger.ID)
	}
	previous := triggerIDs[0]
	if len(triggerIDs) > 1 {
		logic := newScenarioGraphNode(graphNodeLogic, 330, 160)
		if t.TriggerLogic == logicOR {
			logic.LogicOp = graphLogicOR
		}
		g.Nodes = append(g.Nodes, logic)
		for _, id := range triggerIDs {
			g.connect(id, graphPortNext, logic.ID)
		}
		previous = logic.ID
	}
	for i, step := range t.Steps {
		action := newScenarioGraphNode(graphNodeAction, 440+float64(i)*250, 120)
		action.Steps = []ActionStep{step}
		g.Nodes = append(g.Nodes, action)
		g.connect(previous, graphPortNext, action.ID)
		previous = action.ID
	}
	g.Nodes = append(g.Nodes, finish)
	g.connect(previous, graphPortNext, finish.ID)
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
	needsV4Migration := g.Version < 4
	if g.Version < 2 {
		g = migrateScenarioGraphV2(g)
	}
	if g.Version < 3 {
		g = migrateScenarioGraphV3(g)
	}
	if needsV4Migration {
		g = migrateScenarioGraphV4(g)
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
		g.Nodes[i].Conditions = pruneEmptyConditionGroups(g.Nodes[i].Conditions)
	}
	return g
}

// A group without any leaf condition has no predicate to evaluate. Older
// editor versions could save such placeholder groups; treating one as false
// silently blocked every downstream action. Remove empty groups from the
// outside in while preserving all groups that actually contain conditions.
func pruneEmptyConditionGroups(src []AutomationCondition) []AutomationCondition {
	out := append([]AutomationCondition(nil), src...)
	for {
		children := make(map[string]bool, len(out))
		for _, condition := range out {
			if condition.Enabled && condition.GroupID != "" {
				children[condition.GroupID] = true
			}
		}
		changed := false
		filtered := out[:0]
		for _, condition := range out {
			if condition.Type == condGroup && !children[condition.ID] {
				changed = true
				continue
			}
			filtered = append(filtered, condition)
		}
		out = filtered
		if !changed {
			return out
		}
	}
}

// Early editor builds silently attached this exact CPU condition to every new
// trigger. It looked like an empty condition in the node, but blocked the whole
// graph at runtime. Remove only that generated signature during migration.
func migrateScenarioGraphV4(g ScenarioGraph) ScenarioGraph {
	for i := range g.Nodes {
		n := &g.Nodes[i]
		if n.Kind != graphNodeTrigger || len(n.Conditions) != 1 {
			continue
		}
		c := n.Conditions[0]
		if c.Type == condCPU && c.Compare == -1 && c.Threshold == 10 && c.HoldSeconds == 30 &&
			c.Enabled && strings.TrimSpace(c.Text) == "" && strings.TrimSpace(c.GroupID) == "" {
			n.Conditions = nil
		}
	}
	g.Version = scenarioGraphVersion
	return g
}

func migrateScenarioGraphV3(g ScenarioGraph) ScenarioGraph {
	original := append([]ScenarioGraphNode(nil), g.Nodes...)
	for _, snapshot := range original {
		node := g.node(snapshot.ID)
		if node == nil {
			continue
		}
		if node.Kind == graphNodeTrigger && len(node.Conditions) > 1 {
			base := *node
			conditions := append([]AutomationCondition(nil), node.Conditions...)
			node.Conditions = []AutomationCondition{conditions[0]}
			outgoing := g.outgoingAll(node.ID, graphPortNext)
			kept := g.Edges[:0]
			for _, edge := range g.Edges {
				if edge.From != node.ID || edge.FromPort != graphPortNext {
					kept = append(kept, edge)
				}
			}
			g.Edges = kept
			logic := newScenarioGraphNode(graphNodeLogic, base.X+260, base.Y)
			if base.Logic == logicOR {
				logic.LogicOp = graphLogicOR
			}
			g.Nodes = append(g.Nodes, logic)
			g.connect(node.ID, graphPortNext, logic.ID)
			for i := 1; i < len(conditions); i++ {
				branch := base
				branch.ID = newAutomationID("node")
				branch.Y += float64(i) * 130
				branch.Conditions = []AutomationCondition{conditions[i]}
				g.Nodes = append(g.Nodes, branch)
				g.connect(branch.ID, graphPortNext, logic.ID)
			}
			for _, edge := range outgoing {
				g.connect(logic.ID, graphPortNext, edge.To)
			}
		}
		if node = g.node(snapshot.ID); node != nil && node.Kind == graphNodeAction && len(node.Steps) > 1 {
			base := *node
			steps := cloneActionSteps(node.Steps)
			node.Steps = []ActionStep{steps[0]}
			outgoing := g.outgoingAll(node.ID, graphPortNext)
			kept := g.Edges[:0]
			for _, edge := range g.Edges {
				if edge.From != node.ID || edge.FromPort != graphPortNext {
					kept = append(kept, edge)
				}
			}
			g.Edges = kept
			previous := node.ID
			for i := 1; i < len(steps); i++ {
				next := newScenarioGraphNode(graphNodeAction, base.X+float64(i)*250, base.Y)
				next.Steps = []ActionStep{steps[i]}
				g.Nodes = append(g.Nodes, next)
				g.connect(previous, graphPortNext, next.ID)
				previous = next.ID
			}
			for _, edge := range outgoing {
				g.connect(previous, graphPortNext, edge.To)
			}
		}
	}
	g.Version = scenarioGraphVersion
	return g
}

func migrateScenarioGraphV2(g ScenarioGraph) ScenarioGraph {
	merged := map[string]string{}
	for _, edge := range g.Edges {
		from, to := g.node(edge.From), g.node(edge.To)
		if from != nil && to != nil && from.Kind == graphNodeTrigger && to.Kind == graphNodeCondition && edge.FromPort == graphPortNext && len(from.Conditions) == 0 {
			from.Conditions = append([]AutomationCondition(nil), to.Conditions...)
			from.Logic = to.Logic
			merged[to.ID] = from.ID
		}
	}
	nodes := g.Nodes[:0]
	for i := range g.Nodes {
		n := g.Nodes[i]
		if merged[n.ID] != "" {
			continue
		}
		if n.Kind == graphNodeCondition {
			n.Kind = graphNodeTrigger
			n.Mode = 5 // an old standalone condition becomes an immediate gate
		}
		nodes = append(nodes, n)
	}
	g.Nodes = nodes
	edges := make([]ScenarioGraphEdge, 0, len(g.Edges))
	seen := map[string]bool{}
	for _, edge := range g.Edges {
		if merged[edge.To] != "" {
			continue
		}
		if replacement := merged[edge.From]; replacement != "" {
			if edge.FromPort == graphPortFalse || edge.FromPort == graphPortError {
				continue
			}
			edge.From = replacement
		}
		if edge.FromPort == graphPortFalse || edge.FromPort == graphPortError {
			continue
		}
		edge.FromPort = graphPortNext
		key := edge.From + "\x00" + edge.To
		if edge.From == edge.To || seen[key] {
			continue
		}
		seen[key] = true
		edges = append(edges, edge)
	}
	g.Edges = edges
	g.Version = scenarioGraphVersion
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

func (g *ScenarioGraph) outgoingAll(from, port string) []ScenarioGraphEdge {
	out := []ScenarioGraphEdge{}
	for _, edge := range g.Edges {
		if edge.From == from && edge.FromPort == port {
			out = append(out, edge)
		}
	}
	return out
}

func (g *ScenarioGraph) connect(from, port, to string) {
	if from == "" || to == "" || from == to || g.node(from) == nil || g.node(to) == nil {
		return
	}
	for i := range g.Edges {
		if g.Edges[i].From == from && g.Edges[i].FromPort == port && g.Edges[i].To == to {
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

func pruneSingleInputJunctions(g *ScenarioGraph) bool {
	if g == nil {
		return false
	}
	changed := false
	for {
		removed := false
		for _, node := range append([]ScenarioGraphNode(nil), g.Nodes...) {
			if node.Kind != graphNodeJunction {
				continue
			}
			incoming, outgoing := []ScenarioGraphEdge{}, []ScenarioGraphEdge{}
			for _, edge := range g.Edges {
				if edge.To == node.ID {
					incoming = append(incoming, edge)
				}
				if edge.From == node.ID {
					outgoing = append(outgoing, edge)
				}
			}
			if len(incoming) != 1 {
				continue
			}
			source := incoming[0]
			g.removeNode(node.ID)
			for _, edge := range outgoing {
				g.connect(source.From, source.FromPort, edge.To)
			}
			removed, changed = true, true
			break
		}
		if !removed {
			break
		}
	}
	return changed
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
		if n.Kind == graphNodeLogic && (n.LogicOp < graphLogicAND || n.LogicOp > graphLogicNOR) {
			issues = append(issues, GraphValidationIssue{2, n.ID, "В логическом блоке выбрана неизвестная операция"})
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
	if triggers == 0 {
		issues = append(issues, GraphValidationIssue{2, "", "Добавьте хотя бы один блок «Условия»"})
	}
	if finishes == 0 {
		issues = append(issues, GraphValidationIssue{2, "", "Добавьте хотя бы один блок завершения"})
	}
	adj := map[string][]string{}
	incoming := map[string]int{}
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
		adj[e.From] = append(adj[e.From], e.To)
		incoming[e.To]++
	}
	for _, n := range g.Nodes {
		if n.Kind == graphNodeLogic {
			need := 2
			if n.LogicOp == graphLogicNOT {
				need = 1
			}
			if incoming[n.ID] < need {
				issues = append(issues, GraphValidationIssue{1, n.ID, fmt.Sprintf("Логическому блоку нужно минимум %d входа", need)})
			}
			if n.LogicOp == graphLogicNOT && incoming[n.ID] > 1 {
				issues = append(issues, GraphValidationIssue{2, n.ID, "Логический блок «НЕ» принимает ровно один вход"})
			}
		}
	}
	triggerIDs := []string{}
	for _, n := range g.Nodes {
		if n.Kind == graphNodeTrigger {
			triggerIDs = append(triggerIDs, n.ID)
		}
	}
	if len(triggerIDs) > 0 {
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
		for _, triggerID := range triggerIDs {
			visit(triggerID)
		}
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
		case graphNodeTrigger:
			out.Conditions = append(out.Conditions, n.Conditions...)
			out.TriggerLogic = n.Logic
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
		if len(n.Conditions) == 0 {
			return graphTriggerSummary(n)
		}
		return fmt.Sprintf("%s · %d усл.", graphTriggerSummary(n), len(n.Conditions))
	case graphNodeCondition:
		if len(n.Conditions) == 0 {
			return ""
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
	case graphNodeLogic:
		return graphLogicName(n.LogicOp)
	case graphNodeJunction:
		return "Слияние проводов"
	}
	return ""
}

func graphLogicName(op int) string {
	names := []string{"И", "ИЛИ", "НЕ", "Исключающее ИЛИ", "НЕ-И", "НЕ-ИЛИ"}
	if op >= 0 && op < len(names) {
		return names[op]
	}
	return "Логика"
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

// executeScenarioGraph evaluates the acyclic diagram as a data-flow graph.
// Every wire carries a boolean signal. This permits fan-out, fan-in and
// variable-input logic blocks without inventing hidden execution branches.
func executeScenarioGraph(s Schedule) (int, bool) {
	return executeScenarioGraphWithStepRunner(s, executeScenarioSteps)
}

func executeScenarioGraphWithStepRunner(s Schedule, runSteps func(Schedule) bool) (int, bool) {
	g := cloneScenarioGraph(s.graph)
	if scenarioGraphValidationError(g) != "" {
		return s.action, false
	}
	nodes := map[string]*ScenarioGraphNode{}
	indegree := map[string]int{}
	incoming := map[string][]string{}
	adj := map[string][]string{}
	for i := range g.Nodes {
		nodes[g.Nodes[i].ID] = &g.Nodes[i]
		indegree[g.Nodes[i].ID] = 0
	}
	for _, edge := range g.Edges {
		if nodes[edge.From] == nil || nodes[edge.To] == nil {
			continue
		}
		adj[edge.From] = append(adj[edge.From], edge.To)
		incoming[edge.To] = append(incoming[edge.To], edge.From)
		indegree[edge.To]++
	}
	queue := make([]string, 0, len(g.Nodes))
	for _, node := range g.Nodes {
		if indegree[node.ID] == 0 {
			queue = append(queue, node.ID)
		}
	}
	topo := make([]string, 0, len(g.Nodes))
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		topo = append(topo, id)
		for _, to := range adj[id] {
			indegree[to]--
			if indegree[to] == 0 {
				queue = append(queue, to)
			}
		}
	}
	if len(topo) != len(g.Nodes) {
		return s.action, false
	}
	signals := map[string]bool{}
	resultAction, finished := s.action, false
	for _, id := range topo {
		node := nodes[id]
		inputs := make([]bool, 0, len(incoming[id]))
		for _, from := range incoming[id] {
			inputs = append(inputs, signals[from])
		}
		inputAny := len(inputs) == 0 && node.Kind == graphNodeTrigger
		for _, value := range inputs {
			inputAny = inputAny || value
		}
		appendRunHistory("GRAPH_NODE", graphNodeKindName(node.Kind)+" · "+graphNodeSummary(*node), s.runID)
		switch node.Kind {
		case graphNodeTrigger:
			ok, _ := evaluateAutomationConditions(pruneEmptyConditionGroups(node.Conditions))
			signals[id] = inputAny && ok
			appendRunHistory("GRAPH_SIGNAL", fmt.Sprintf("%s: вход=%t, условие=%t, выход=%t", graphNodeKindName(node.Kind), inputAny, ok, signals[id]), s.runID)
		case graphNodeCondition:
			ok, _ := evaluateAutomationConditions(pruneEmptyConditionGroups(node.Conditions))
			signals[id] = inputAny && ok
			appendRunHistory("GRAPH_SIGNAL", fmt.Sprintf("%s: вход=%t, условие=%t, выход=%t", graphNodeKindName(node.Kind), inputAny, ok, signals[id]), s.runID)
		case graphNodeLogic:
			signals[id] = evaluateGraphLogic(node.LogicOp, inputs)
		case graphNodeJunction:
			signals[id] = inputAny
		case graphNodeAction:
			if !inputAny {
				signals[id] = false
				appendRunHistory("GRAPH_SKIP", "Действие пропущено: входной сигнал не получен · "+graphNodeSummary(*node), s.runID)
				continue
			}
			stepSchedule := s
			stepSchedule.steps = cloneActionSteps(node.Steps)
			signals[id] = runSteps(stepSchedule)
			appendRunHistory("GRAPH_ACTION", fmt.Sprintf("%s · результат=%t", graphNodeSummary(*node), signals[id]), s.runID)
		case graphNodeWait:
			if inputAny {
				time.Sleep(time.Duration(max(node.WaitSecs, 1)) * time.Second)
			}
			signals[id] = inputAny
		case graphNodeFinish:
			if inputAny && !finished {
				resultAction, finished = node.Action, true
			}
			signals[id] = inputAny
		default:
			signals[id] = false
		}
	}
	return resultAction, finished
}

func evaluateGraphLogic(op int, inputs []bool) bool {
	switch op {
	case graphLogicAND, graphLogicNAND:
		value := len(inputs) > 0
		for _, input := range inputs {
			value = value && input
		}
		if op == graphLogicNAND {
			return !value
		}
		return value
	case graphLogicOR, graphLogicNOR:
		value := false
		for _, input := range inputs {
			value = value || input
		}
		if op == graphLogicNOR {
			return !value
		}
		return value
	case graphLogicNOT:
		return len(inputs) > 0 && !inputs[0]
	case graphLogicXOR:
		value := false
		for _, input := range inputs {
			value = value != input
		}
		return value
	}
	return false
}
