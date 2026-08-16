//go:build windows

package main

import (
	"fmt"
	"math"
	"sort"
)

type GraphNodeHit struct {
	ID     string
	Rect   RECT
	Header RECT
	Delete RECT
	Add    RECT
}

type GraphPortHit struct {
	NodeID string
	Port   string
	Input  bool
	Rect   RECT
}

type GraphFunctionHit struct {
	NodeID string
	Kind   int // 1 condition, 2 action, 3 wait
	Index  int
	Rect   RECT
}

func currentScenarioGraph() *ScenarioGraph {
	if app.scenarioSavedDraft {
		return &app.savedEditDraft.Graph
	}
	return &app.settings.ScenarioGraph
}

func currentGraphLegacyState() TaskState {
	if app.scenarioSavedDraft {
		return taskStateFromSaved040(app.savedEditDraft)
	}
	return legacyTaskStateFromSettings(app.settings)
}

func ensureCurrentScenarioGraph() *ScenarioGraph {
	g := currentScenarioGraph()
	if g.Version <= 0 || len(g.Nodes) == 0 {
		*g = ensureScenarioGraph(*g, currentGraphLegacyState())
		persistCurrentScenarioGraph()
	}
	return g
}

func persistCurrentScenarioGraph() {
	if app.scenarioSavedDraft {
		return
	}
	saveSettings()
	saveDraftAutosave()
}

func selectedGraphNode() *ScenarioGraphNode {
	if app.graphSelectedNodeID == "" {
		return nil
	}
	return ensureCurrentScenarioGraph().node(app.graphSelectedNodeID)
}

func syncCurrentGraphFromLegacy() {
	g := ensureCurrentScenarioGraph()
	if tr := g.trigger(); tr != nil {
		if app.scenarioSavedDraft {
			t := app.savedEditDraft
			tr.Mode, tr.DelayHours, tr.DelayMins, tr.DelaySecs = t.Mode, t.DelayHours, t.DelayMinutes, t.DelaySeconds
			tr.Exact, tr.IdleSecs, tr.Process, tr.Recurrence = t.Exact, t.IdleMinutes, t.WatchProcess, t.Recurrence
		} else {
			tr.Mode, tr.DelayHours, tr.DelayMins, tr.DelaySecs = app.selectedMode, app.settings.DelayHours, app.settings.DelayMinutes, app.settings.DelaySeconds
			tr.Exact, tr.IdleSecs, tr.Process, tr.Recurrence = app.settings.Exact, app.settings.IdleMinutes, app.settings.WatchProcess, app.settings.Recurrence
		}
	}
	if n := selectedGraphNode(); n != nil && n.Kind == graphNodeFinish {
		if app.scenarioSavedDraft {
			n.Action = app.savedEditDraft.Action
		} else {
			n.Action = app.selectedAction
		}
	}
	persistCurrentScenarioGraph()
}

func syncLegacyFromCurrentGraph() TaskState {
	g := ensureCurrentScenarioGraph()
	base := currentGraphLegacyState()
	st := graphLegacyState(*g, base)
	if app.scenarioSavedDraft {
		draft := &app.savedEditDraft
		draft.TaskKind = 1
		draft.Action, draft.Mode = st.Action, st.Mode
		draft.DelayHours, draft.DelayMinutes, draft.DelaySeconds = st.DelayHours, st.DelayMinutes, st.DelaySeconds
		draft.Exact, draft.IdleMinutes, draft.WatchProcess, draft.WarningSeconds = st.Exact, st.IdleMinutes, st.WatchProcess, st.WarningSeconds
		draft.Conditions, draft.TriggerLogic, draft.Steps = append([]AutomationCondition(nil), st.Conditions...), st.TriggerLogic, cloneActionSteps(st.Steps)
		draft.Recurrence, draft.Graph = st.Recurrence, cloneScenarioGraph(*g)
	} else {
		app.selectedAction, app.selectedMode = st.Action, st.Mode
		app.settings.Action, app.settings.Mode = st.Action, st.Mode
		app.settings.DelayHours, app.settings.DelayMinutes, app.settings.DelaySeconds = st.DelayHours, st.DelayMinutes, st.DelaySeconds
		app.settings.Exact, app.settings.IdleMinutes, app.settings.WatchProcess = st.Exact, st.IdleMinutes, st.WatchProcess
		app.settings.AdvancedConditions, app.settings.ActionSteps = append([]AutomationCondition(nil), st.Conditions...), cloneActionSteps(st.Steps)
		app.settings.TriggerLogic, app.settings.Recurrence = st.TriggerLogic, st.Recurrence
		app.settings.ScenarioGraph = cloneScenarioGraph(*g)
		saveSettings()
	}
	return st
}

func layoutScenarioGraphEditor(body RECT) {
	if !app.graphExpandedOnce && app.hwnd != 0 && !app.settings.LockMinimumSize && !app.settings.LockCurrentSize {
		app.graphExpandedOnce = true
		pShowWindow.Call(app.hwnd, SW_MAXIMIZE)
	}
	left := int(body.Left) + 14
	top := int(body.Top) + 58
	right := int(body.Right) - 14
	bottom := int(body.Bottom) - 14
	app.savedScenarioNameRect, app.savedScenarioSaveRect, app.savedScenarioCancelRect, app.savedScenarioCheckRect = RECT{}, RECT{}, RECT{}, RECT{}
	if app.section == 13 && app.scenarioSavedDraft {
		footerY := bottom - 38
		nameW := minInt(205, max(145, (right-left)*31/100))
		app.savedScenarioNameRect = RECT{int32(left), int32(footerY), int32(left + nameW), int32(footerY + 36)}
		move(app.edits[idTaskName], left+9, footerY+8, nameW-18, 20)
		if app.confirmDiscardScenario {
			pShowWindow.Call(app.edits[idTaskName], SW_HIDE)
		} else {
			pShowWindow.Call(app.edits[idTaskName], SW_SHOW)
		}
		btnLeft, gap := left+nameW+8, 7
		btnW := max(76, (right-btnLeft-gap*2)/3)
		app.savedScenarioSaveRect = RECT{int32(btnLeft), int32(footerY), int32(btnLeft + btnW), int32(footerY + 36)}
		app.savedScenarioCancelRect = RECT{int32(btnLeft + btnW + gap), int32(footerY), int32(btnLeft + btnW*2 + gap), int32(footerY + 36)}
		app.savedScenarioCheckRect = RECT{int32(btnLeft + btnW*2 + gap*2), int32(footerY), int32(right), int32(footerY + 36)}
		bottom = footerY - 12
	}
	app.previewRect = RECT{body.Right - 126, body.Top + 12, body.Right - 14, body.Top + 46}
	paletteW := 142
	app.graphCanvasRect = RECT{int32(left + paletteW + 10), int32(top), int32(right), int32(bottom)}
	for i := range app.graphPaletteRects {
		y := top + i*42
		app.graphPaletteRects[i] = RECT{int32(left), int32(y), int32(left + paletteW), int32(y + 34)}
	}
	for i := range app.graphZoomRects {
		x := right - 116 + i*40
		app.graphZoomRects[i] = RECT{int32(x), int32(top + 8), int32(x + 34), int32(top + 40)}
	}
}

func graphNodeHeight(n ScenarioGraphNode) float64 {
	count := 1
	if n.Kind == graphNodeCondition {
		count = max(len(n.Conditions), 1)
	}
	if n.Kind == graphNodeAction {
		count = max(len(n.Steps), 1)
	}
	return float64(74 + minInt(count, 4)*24)
}

func graphNodeScreenRect(g *ScenarioGraph, n ScenarioGraphNode) RECT {
	z := g.Zoom
	if z <= 0 {
		z = 1
	}
	x := float64(app.graphCanvasRect.Left) + (n.X+g.ViewX)*z
	y := float64(app.graphCanvasRect.Top) + (n.Y+g.ViewY)*z
	w := 224.0 * z
	h := graphNodeHeight(n) * z
	return RECT{int32(math.Round(x)), int32(math.Round(y)), int32(math.Round(x + w)), int32(math.Round(y + h))}
}

func graphOutputPoint(g *ScenarioGraph, n ScenarioGraphNode, port string) (float32, float32) {
	r := graphNodeScreenRect(g, n)
	ports := graphNodePorts(n.Kind)
	idx := 0
	for i, p := range ports {
		if p == port {
			idx = i
		}
	}
	y := float32(r.Top + 46 + int32(idx*22))
	return float32(r.Right), y
}

func graphInputPoint(g *ScenarioGraph, n ScenarioGraphNode) (float32, float32) {
	r := graphNodeScreenRect(g, n)
	return float32(r.Left), float32((r.Top + r.Bottom) / 2)
}

func drawGraphConnection(x1, y1, x2, y2 float32, color uint32) {
	mid := (x1 + x2) / 2
	if x2 < x1+42 {
		mid = x1 + 42
	}
	d2dDrawLine(x1, y1, mid, y1, 1.8, color)
	d2dDrawLine(mid, y1, mid, y2, 1.8, color)
	d2dDrawLine(mid, y2, x2, y2, 1.8, color)
}

func graphNodeColor(kind int) uint32 {
	switch kind {
	case graphNodeTrigger:
		return blendColor(surfaceButtonColor(), theme.accent2, .24)
	case graphNodeCondition:
		return blendColor(surfaceButtonColor(), theme.accent, .22)
	case graphNodeAction:
		return blendColor(surfaceButtonColor(), theme.success, .16)
	case graphNodeWait:
		return blendColor(surfaceButtonColor(), theme.muted, .12)
	case graphNodeFinish:
		return blendColor(surfaceButtonColor(), theme.danger, .14)
	}
	return surfaceButtonColor()
}

func drawScenarioGraphEditor(hdc uintptr, body RECT, w int) {
	g := ensureCurrentScenarioGraph()
	app.graphNodeHits = app.graphNodeHits[:0]
	app.graphPortHits = app.graphPortHits[:0]
	app.graphFunctionHits = app.graphFunctionHits[:0]
	drawText(hdc, "Редактор сценария", int(body.Left)+18, int(body.Top)+14, 290, 30, 20, 650, theme.text, DT_LEFT|DT_VCENTER|DT_SINGLELINE)
	drawText(hdc, "Перетаскивай блоки и соединяй выходы со входами", int(body.Left)+306, int(body.Top)+16, int(body.Right-body.Left)-510, 26, 11, 400, theme.muted, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
	drawButton(hdc, app.previewRect, "Проверить", false)
	labels := []string{"+ Триггер", "+ Условия", "+ Действия", "+ Ожидание", "+ Завершение"}
	for i, r := range app.graphPaletteRects {
		drawButton(hdc, r, labels[i], false)
	}
	drawText(hdc, "Блоки", int(app.graphPaletteRects[0].Left), int(app.graphPaletteRects[0].Top)-28, 120, 20, 12, 650, theme.muted, DT_LEFT|DT_VCENTER|DT_SINGLELINE)
	canvas := app.graphCanvasRect
	roundFill(hdc, canvas, blendColor(theme.bg, surfacePanelColor(), .36), 14)
	if ui2d.active {
		d2dPushClip(canvas)
		grid := blendColor(theme.border, theme.muted, .14)
		step := int32(max(28, int(48*g.Zoom)))
		ox := canvas.Left + int32(math.Mod(g.ViewX*g.Zoom, float64(step)))
		oy := canvas.Top + int32(math.Mod(g.ViewY*g.Zoom, float64(step)))
		for x := ox; x < canvas.Right; x += step {
			d2dDrawLine(float32(x), float32(canvas.Top), float32(x), float32(canvas.Bottom), .55, grid)
		}
		for y := oy; y < canvas.Bottom; y += step {
			d2dDrawLine(float32(canvas.Left), float32(y), float32(canvas.Right), float32(y), .55, grid)
		}
		for _, e := range g.Edges {
			from, to := g.node(e.From), g.node(e.To)
			if from == nil || to == nil {
				continue
			}
			x1, y1 := graphOutputPoint(g, *from, e.FromPort)
			x2, y2 := graphInputPoint(g, *to)
			c := blendColor(theme.border, theme.accent2, .62)
			if e.FromPort == graphPortError {
				c = blendColor(theme.border, theme.danger, .70)
			}
			drawGraphConnection(x1, y1, x2, y2, c)
		}
		if app.graphConnectingNodeID != "" {
			if n := g.node(app.graphConnectingNodeID); n != nil {
				x1, y1 := graphOutputPoint(g, *n, app.graphConnectingPort)
				drawGraphConnection(x1, y1, float32(app.mouseX), float32(app.mouseY), theme.accent2)
			}
		}
		for _, n := range g.Nodes {
			drawScenarioGraphNode(hdc, g, n)
		}
		d2dPopClip()
	}
	for i, label := range []string{"−", "+", "Вписать"} {
		drawButton(hdc, app.graphZoomRects[i], label, false)
	}
	issues := validateScenarioGraph(*g)
	app.graphValidation = issues
	errs, warns := 0, 0
	for _, it := range issues {
		if it.Level == 2 {
			errs++
		} else {
			warns++
		}
	}
	status := fmt.Sprintf("Блоков: %d · связей: %d · масштаб: %d%%", len(g.Nodes), len(g.Edges), int(g.Zoom*100+.5))
	if errs > 0 || warns > 0 {
		status += fmt.Sprintf(" · ошибок: %d · предупреждений: %d", errs, warns)
	}
	drawText(hdc, status, int(canvas.Left)+12, int(canvas.Bottom)-27, int(canvas.Right-canvas.Left)-24, 20, 10, 500, theme.muted, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
	if app.section == 13 && app.scenarioSavedDraft {
		drawText(hdc, "Название", int(app.savedScenarioNameRect.Left), int(app.savedScenarioNameRect.Top)-18, int(app.savedScenarioNameRect.Right-app.savedScenarioNameRect.Left), 16, 9, 500, theme.muted, DT_LEFT|DT_VCENTER|DT_SINGLELINE)
		roundFill(hdc, app.savedScenarioNameRect, surfaceButtonColor(), 9)
		drawButton(hdc, app.savedScenarioSaveRect, "Сохранить", true)
		drawButton(hdc, app.savedScenarioCancelRect, "Отмена", false)
		drawButton(hdc, app.savedScenarioCheckRect, "Проверка", false)
	}
}

func drawScenarioGraphNode(hdc uintptr, g *ScenarioGraph, n ScenarioGraphNode) {
	r := graphNodeScreenRect(g, n)
	if r.Right < app.graphCanvasRect.Left || r.Left > app.graphCanvasRect.Right || r.Bottom < app.graphCanvasRect.Top || r.Top > app.graphCanvasRect.Bottom {
		return
	}
	selected := n.ID == app.graphSelectedNodeID
	fillColor := graphNodeColor(n.Kind)
	roundFill(hdc, r, fillColor, int32(max(7, int(12*g.Zoom))))
	if ui2d.active {
		outline := blendColor(theme.border, theme.text, .35)
		stroke := float32(1)
		if selected {
			outline, stroke = theme.accent2, 2.2
		}
		d2dDrawRoundedOutline(r, float32(max(7, int(12*g.Zoom))), stroke, outline)
	}
	headerH := int32(max(26, int(34*g.Zoom)))
	header := RECT{r.Left, r.Top, r.Right, r.Top + headerH}
	deleteR := RECT{r.Right - 28, r.Top + 5, r.Right - 7, r.Top + 26}
	addR := RECT{r.Left + 8, r.Bottom - 26, r.Right - 8, r.Bottom - 6}
	app.graphNodeHits = append(app.graphNodeHits, GraphNodeHit{ID: n.ID, Rect: r, Header: header, Delete: deleteR, Add: addR})
	drawText(hdc, graphNodeKindName(n.Kind), int(r.Left)+12, int(r.Top)+5, int(r.Right-r.Left)-48, int(headerH)-8, max(9, int(11*g.Zoom)), 650, theme.text, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
	drawText(hdc, "×", int(deleteR.Left), int(deleteR.Top), int(deleteR.Right-deleteR.Left), int(deleteR.Bottom-deleteR.Top), 14, 500, theme.muted, DT_CENTER|DT_VCENTER|DT_SINGLELINE)
	items := []string{}
	itemKind := 0
	switch n.Kind {
	case graphNodeCondition:
		itemKind = 1
		for _, c := range n.Conditions {
			if c.Type == condGroup {
				items = append(items, "Составная группа")
			} else {
				items = append(items, conditionSummary(c))
			}
		}
	case graphNodeAction:
		itemKind = 2
		for _, st := range n.Steps {
			items = append(items, stepSummary(st))
		}
	case graphNodeWait:
		itemKind = 3
		items = append(items, graphNodeSummary(n))
	default:
		items = append(items, graphNodeSummary(n))
	}
	if len(items) == 0 {
		items = []string{"Нажми +, чтобы добавить функцию"}
	}
	rowY := r.Top + headerH + 5
	rowH := int32(max(18, int(22*g.Zoom)))
	for i := 0; i < len(items) && i < 4; i++ {
		rr := RECT{r.Left + 9, rowY + int32(i)*rowH, r.Right - 9, rowY + int32(i+1)*rowH - 2}
		drawText(hdc, items[i], int(rr.Left)+4, int(rr.Top), int(rr.Right-rr.Left)-8, int(rr.Bottom-rr.Top), max(8, int(9*g.Zoom)), 450, theme.text, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
		if itemKind != 0 {
			app.graphFunctionHits = append(app.graphFunctionHits, GraphFunctionHit{NodeID: n.ID, Kind: itemKind, Index: i, Rect: rr})
		}
	}
	if n.Kind == graphNodeCondition || n.Kind == graphNodeAction {
		drawText(hdc, "+ Добавить функцию", int(addR.Left), int(addR.Top), int(addR.Right-addR.Left), int(addR.Bottom-addR.Top), max(8, int(9*g.Zoom)), 600, theme.accent2, DT_CENTER|DT_VCENTER|DT_SINGLELINE)
	}
	inX, inY := graphInputPoint(g, n)
	inRect := RECT{int32(inX) - 7, int32(inY) - 7, int32(inX) + 7, int32(inY) + 7}
	if n.Kind != graphNodeTrigger {
		roundFill(hdc, inRect, theme.text, 7)
		app.graphPortHits = append(app.graphPortHits, GraphPortHit{NodeID: n.ID, Input: true, Rect: inRect})
	}
	for _, port := range graphNodePorts(n.Kind) {
		x, y := graphOutputPoint(g, n, port)
		pr := RECT{int32(x) - 7, int32(y) - 7, int32(x) + 7, int32(y) + 7}
		pc := theme.accent2
		if port == graphPortError {
			pc = theme.danger
		}
		roundFill(hdc, pr, pc, 7)
		app.graphPortHits = append(app.graphPortHits, GraphPortHit{NodeID: n.ID, Port: port, Rect: pr})
		drawText(hdc, graphPortName(port), int(r.Right)-66, int(y)-9, 54, 18, max(7, int(8*g.Zoom)), 550, theme.muted, DT_RIGHT|DT_VCENTER|DT_SINGLELINE)
	}
}

func addGraphNode(kind int) {
	g := ensureCurrentScenarioGraph()
	if len(g.Nodes) >= 64 {
		showNotification("PowerPilot", "В одной схеме может быть не больше 64 блоков.")
		return
	}
	if kind == graphNodeTrigger && g.trigger() != nil {
		showNotification("PowerPilot", "Первая версия редактора поддерживает один блок-триггер.")
		return
	}
	wx := (float64(app.graphCanvasRect.Right-app.graphCanvasRect.Left)/2)/g.Zoom - g.ViewX - 112
	wy := (float64(app.graphCanvasRect.Bottom-app.graphCanvasRect.Top)/2)/g.Zoom - g.ViewY - 55
	n := newScenarioGraphNode(kind, wx+float64(len(g.Nodes)%3)*28, wy+float64(len(g.Nodes)%4)*24)
	g.Nodes = append(g.Nodes, n)
	app.graphSelectedNodeID = n.ID
	persistCurrentScenarioGraph()
	layoutControls(app.hwnd)
	invalidate(app.hwnd)
}

func fitScenarioGraph() {
	g := ensureCurrentScenarioGraph()
	if len(g.Nodes) == 0 {
		return
	}
	minX, minY := g.Nodes[0].X, g.Nodes[0].Y
	maxX, maxY := minX+224, minY+graphNodeHeight(g.Nodes[0])
	for _, n := range g.Nodes[1:] {
		minX, minY = math.Min(minX, n.X), math.Min(minY, n.Y)
		maxX, maxY = math.Max(maxX, n.X+224), math.Max(maxY, n.Y+graphNodeHeight(n))
	}
	cw := float64(app.graphCanvasRect.Right-app.graphCanvasRect.Left) - 80
	ch := float64(app.graphCanvasRect.Bottom-app.graphCanvasRect.Top) - 80
	g.Zoom = clampFloat(math.Min(cw/math.Max(maxX-minX, 1), ch/math.Max(maxY-minY, 1)), .45, 1.35)
	g.ViewX = 40/g.Zoom - minX
	g.ViewY = 50/g.Zoom - minY
}

func handleScenarioGraphClick(x, y int32) bool {
	if app.section != 7 && app.section != 13 {
		return false
	}
	g := ensureCurrentScenarioGraph()
	for i, r := range app.graphPaletteRects {
		if pointIn(r, x, y) {
			addGraphNode(i)
			playUI(successSound)
			return true
		}
	}
	for i, r := range app.graphZoomRects {
		if pointIn(r, x, y) {
			if i == 0 {
				g.Zoom = clampFloat(g.Zoom-.1, .45, 2.2)
			} else if i == 1 {
				g.Zoom = clampFloat(g.Zoom+.1, .45, 2.2)
			} else {
				fitScenarioGraph()
			}
			persistCurrentScenarioGraph()
			invalidate(app.hwnd)
			return true
		}
	}
	for _, p := range app.graphPortHits {
		if !pointIn(p.Rect, x, y) {
			continue
		}
		if p.Input {
			if app.graphConnectingNodeID != "" {
				g.connect(app.graphConnectingNodeID, app.graphConnectingPort, p.NodeID)
				app.graphConnectingNodeID, app.graphConnectingPort = "", ""
				persistCurrentScenarioGraph()
				playUI(successSound)
			}
		} else {
			if app.graphConnectingNodeID == p.NodeID && app.graphConnectingPort == p.Port {
				for i := len(g.Edges) - 1; i >= 0; i-- {
					if g.Edges[i].From == p.NodeID && g.Edges[i].FromPort == p.Port {
						g.Edges = append(g.Edges[:i], g.Edges[i+1:]...)
					}
				}
				app.graphConnectingNodeID, app.graphConnectingPort = "", ""
				persistCurrentScenarioGraph()
				invalidate(app.hwnd)
				return true
			}
			app.graphConnectingNodeID, app.graphConnectingPort = p.NodeID, p.Port
			app.graphSelectedNodeID = p.NodeID
			playUI(clickSound)
		}
		invalidate(app.hwnd)
		return true
	}
	for _, f := range app.graphFunctionHits {
		if !pointIn(f.Rect, x, y) {
			continue
		}
		app.graphSelectedNodeID = f.NodeID
		if f.Kind == 1 {
			openConditionEditor(f.Index)
		} else if f.Kind == 2 {
			openStepEditor(f.Index)
		} else if f.Kind == 3 {
			if n := selectedGraphNode(); n != nil {
				values := []int{10, 30, 60, 300, 600}
				next := values[0]
				for i, v := range values {
					if n.WaitSecs == v {
						next = values[(i+1)%len(values)]
					}
				}
				n.WaitSecs = next
				persistCurrentScenarioGraph()
			}
		}
		return true
	}
	for _, h := range app.graphNodeHits {
		if pointIn(h.Delete, x, y) {
			g.removeNode(h.ID)
			if app.graphSelectedNodeID == h.ID {
				app.graphSelectedNodeID = ""
			}
			persistCurrentScenarioGraph()
			layoutControls(app.hwnd)
			invalidate(app.hwnd)
			return true
		}
		if pointIn(h.Add, x, y) {
			app.graphSelectedNodeID = h.ID
			n := selectedGraphNode()
			if n != nil && n.Kind == graphNodeCondition {
				openConditionEditor(-1)
			} else if n != nil && n.Kind == graphNodeAction {
				openStepEditor(-1)
			}
			return true
		}
		if pointIn(h.Header, x, y) {
			app.graphSelectedNodeID = h.ID
			app.graphDraggingNodeID, app.graphDragging = h.ID, true
			app.graphLastMouseX, app.graphLastMouseY = x, y
			pSetCapture.Call(app.hwnd)
			return true
		}
		if pointIn(h.Rect, x, y) {
			app.graphSelectedNodeID = h.ID
			n := selectedGraphNode()
			if n != nil && n.Kind == graphNodeTrigger {
				if app.scenarioSavedDraft {
					loadScenarioWhenInputs()
				}
				app.section = 15
			} else if n != nil && n.Kind == graphNodeFinish {
				app.section = 14
			}
			startPageAnimation()
			updateInputVisibility()
			invalidate(app.hwnd)
			return true
		}
	}
	if pointIn(app.graphCanvasRect, x, y) {
		app.graphSelectedNodeID = ""
		app.graphConnectingNodeID, app.graphConnectingPort = "", ""
		app.graphPanning = true
		app.graphLastMouseX, app.graphLastMouseY = x, y
		pSetCapture.Call(app.hwnd)
		invalidate(app.hwnd)
		return true
	}
	return false
}

func handleScenarioGraphMouseMove(x, y int32) bool {
	if !app.graphDragging && !app.graphPanning {
		return false
	}
	g := ensureCurrentScenarioGraph()
	dx, dy := float64(x-app.graphLastMouseX)/g.Zoom, float64(y-app.graphLastMouseY)/g.Zoom
	app.graphLastMouseX, app.graphLastMouseY = x, y
	if app.graphDragging {
		if n := g.node(app.graphDraggingNodeID); n != nil {
			n.X += dx
			n.Y += dy
		}
	} else if app.graphPanning {
		g.ViewX += dx
		g.ViewY += dy
	}
	invalidate(app.hwnd)
	return true
}

func finishScenarioGraphPointer() bool {
	if !app.graphDragging && !app.graphPanning {
		return false
	}
	app.graphDragging, app.graphPanning = false, false
	app.graphDraggingNodeID = ""
	pReleaseCapture.Call()
	persistCurrentScenarioGraph()
	return true
}

func zoomScenarioGraph(delta int16) {
	g := ensureCurrentScenarioGraph()
	if delta > 0 {
		g.Zoom = clampFloat(g.Zoom+.1, .45, 2.2)
	} else {
		g.Zoom = clampFloat(g.Zoom-.1, .45, 2.2)
	}
	persistCurrentScenarioGraph()
	invalidate(app.hwnd)
}

func graphValidationText(issues []GraphValidationIssue) string {
	if len(issues) == 0 {
		return "Схема готова к запуску"
	}
	sort.SliceStable(issues, func(i, j int) bool { return issues[i].Level > issues[j].Level })
	lines := []string{}
	for i, issue := range issues {
		if i >= 8 {
			break
		}
		prefix := "Предупреждение: "
		if issue.Level == 2 {
			prefix = "Ошибка: "
		}
		lines = append(lines, prefix+issue.Message)
	}
	return stringsJoin040(lines, "\n")
}

func stringsJoin040(values []string, sep string) string {
	out := ""
	for i, value := range values {
		if i > 0 {
			out += sep
		}
		out += value
	}
	return out
}

func graphDiagnosticLines(dry bool) []DiagnosticLine {
	g := ensureCurrentScenarioGraph()
	issues := validateScenarioGraph(*g)
	lines := []DiagnosticLine{{diagInfo, "Граф сценария", fmt.Sprintf("%d блоков · %d соединений", len(g.Nodes), len(g.Edges))}}
	for _, issue := range issues {
		level := diagWait
		if issue.Level == 2 {
			level = diagError
		}
		lines = append(lines, DiagnosticLine{level, "Проверка схемы", issue.Message})
	}
	for _, n := range g.Nodes {
		level := diagInfo
		detail := graphNodeSummary(n)
		if n.Kind == graphNodeCondition {
			ok, reason := evaluateAutomationConditions(n.Conditions)
			if ok {
				level = diagOK
			} else {
				level = diagWait
				detail = reason
			}
		}
		if dry && (n.Kind == graphNodeAction || n.Kind == graphNodeFinish) {
			detail += " · будет пропущено в тесте"
		}
		lines = append(lines, DiagnosticLine{level, graphNodeKindName(n.Kind), detail})
	}
	return lines
}
