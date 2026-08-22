//go:build windows

package main

import (
	"math"
)

type scenarioGraphClipboardData struct {
	Nodes []ScenarioGraphNode
	Edges []ScenarioGraphEdge
}

var scenarioGraphClipboard scenarioGraphClipboardData

func resetGraphInteraction() {
	app.graphSelectedNodes = map[string]bool{}
	app.graphSelectedNodeID, app.graphSelectedEdgeID = "", ""
	app.graphConnectingNodeID, app.graphConnectingPort = "", ""
	app.graphContextOpen, app.graphEditorOpen = false, false
	app.graphRightDown, app.graphMiddleDown, app.graphMarquee, app.graphDragging = false, false, false, false
}

func ensureGraphSelection() map[string]bool {
	if app.graphSelectedNodes == nil {
		app.graphSelectedNodes = map[string]bool{}
	}
	return app.graphSelectedNodes
}

func graphNodeSelected(id string) bool { return ensureGraphSelection()[id] }

func selectOnlyGraphNode(id string) {
	app.graphSelectedNodes = map[string]bool{}
	if id != "" {
		app.graphSelectedNodes[id] = true
	}
	app.graphSelectedNodeID = id
	app.graphSelectedEdgeID = ""
}

func toggleGraphNodeSelection(id string) {
	selected := ensureGraphSelection()
	if selected[id] {
		delete(selected, id)
	} else {
		selected[id] = true
	}
	app.graphSelectedNodeID = id
	app.graphSelectedEdgeID = ""
}

func selectedGraphNodeIDs() []string {
	out := []string{}
	for _, node := range ensureCurrentScenarioGraph().Nodes {
		if graphNodeSelected(node.ID) {
			out = append(out, node.ID)
		}
	}
	return out
}

func beginGraphNodeDrag(id string, x, y int32) {
	if keyDown040(0x11) {
		toggleGraphNodeSelection(id)
	} else if !graphNodeSelected(id) {
		selectOnlyGraphNode(id)
	}
	if !graphNodeSelected(id) {
		return
	}
	app.graphSelectedNodeID = id
	app.graphDragging = true
	app.graphLastMouseX, app.graphLastMouseY = x, y
	app.graphContextOpen = false
	pSetCapture.Call(scenarioGraphInputWindow())
}

func graphRectNormalized(x1, y1, x2, y2 int32) RECT {
	return RECT{min32(x1, x2), min32(y1, y2), max32(x1, x2), max32(y1, y2)}
}

func graphRectsIntersect(a, b RECT) bool {
	return a.Left <= b.Right && a.Right >= b.Left && a.Top <= b.Bottom && a.Bottom >= b.Top
}

func beginGraphMarquee(x, y int32) {
	app.graphMarquee = true
	app.graphMarqueeAdditive = keyDown040(0x11)
	app.graphMarqueeStartX, app.graphMarqueeStartY = x, y
	app.graphMarqueeX, app.graphMarqueeY = x, y
	if !app.graphMarqueeAdditive {
		selectOnlyGraphNode("")
	}
	app.graphContextOpen = false
	pSetCapture.Call(scenarioGraphInputWindow())
}

func updateGraphMarquee(x, y int32) {
	app.graphMarqueeX, app.graphMarqueeY = x, y
	r := graphRectNormalized(app.graphMarqueeStartX, app.graphMarqueeStartY, x, y)
	if !app.graphMarqueeAdditive {
		app.graphSelectedNodes = map[string]bool{}
	}
	g := ensureCurrentScenarioGraph()
	for _, node := range g.Nodes {
		if graphRectsIntersect(r, graphNodeScreenRect(g, node)) {
			ensureGraphSelection()[node.ID] = true
			app.graphSelectedNodeID = node.ID
		}
	}
	for _, edge := range g.Edges {
		if graphEdgeIntersectsRect(g, edge, r) {
			app.graphSelectedEdgeID = edge.ID
			break
		}
	}
}

func finishGraphMarquee() bool {
	if !app.graphMarquee {
		return false
	}
	app.graphMarquee = false
	pReleaseCapture.Call()
	invalidateScenarioGraphWindows()
	return true
}

func graphEdgeIntersectsRect(g *ScenarioGraph, edge ScenarioGraphEdge, r RECT) bool {
	from, to := g.node(edge.From), g.node(edge.To)
	if from == nil || to == nil {
		return false
	}
	x1, y1 := graphOutputPoint(g, *from, edge.FromPort)
	x2, y2 := graphInputPoint(g, *to)
	mid := (x1 + x2) / 2
	if x2 < x1+42 {
		mid = x1 + 42
	}
	segments := [][4]float32{{x1, y1, mid, y1}, {mid, y1, mid, y2}, {mid, y2, x2, y2}}
	for _, s := range segments {
		bounds := RECT{int32(math.Min(float64(s[0]), float64(s[2]))) - 5, int32(math.Min(float64(s[1]), float64(s[3]))) - 5, int32(math.Max(float64(s[0]), float64(s[2]))) + 5, int32(math.Max(float64(s[1]), float64(s[3]))) + 5}
		if graphRectsIntersect(r, bounds) {
			return true
		}
	}
	return false
}

func graphPortColor(port string) uint32 {
	switch port {
	case graphPortTrue:
		return theme.success
	case graphPortFalse:
		return rgb(245, 184, 66)
	case graphPortError:
		return theme.danger
	default:
		return theme.accent2
	}
}

func graphEdgeHit(x, y int32) string {
	g := ensureCurrentScenarioGraph()
	for i := len(g.Edges) - 1; i >= 0; i-- {
		edge := g.Edges[i]
		from, to := g.node(edge.From), g.node(edge.To)
		if from == nil || to == nil {
			continue
		}
		x1, y1 := graphOutputPoint(g, *from, edge.FromPort)
		x2, y2 := graphInputPoint(g, *to)
		mid := (x1 + x2) / 2
		if x2 < x1+42 {
			mid = x1 + 42
		}
		if graphPointSegmentDistance(float64(x), float64(y), float64(x1), float64(y1), float64(mid), float64(y1)) <= 10 ||
			graphPointSegmentDistance(float64(x), float64(y), float64(mid), float64(y1), float64(mid), float64(y2)) <= 10 ||
			graphPointSegmentDistance(float64(x), float64(y), float64(mid), float64(y2), float64(x2), float64(y2)) <= 10 {
			return edge.ID
		}
	}
	return ""
}

func graphPointSegmentDistance(px, py, x1, y1, x2, y2 float64) float64 {
	dx, dy := x2-x1, y2-y1
	if dx == 0 && dy == 0 {
		return math.Hypot(px-x1, py-y1)
	}
	t := ((px-x1)*dx + (py-y1)*dy) / (dx*dx + dy*dy)
	t = math.Max(0, math.Min(1, t))
	return math.Hypot(px-(x1+t*dx), py-(y1+t*dy))
}

func selectGraphEdgeAt(x, y int32) bool {
	id := graphEdgeHit(x, y)
	if id == "" {
		return false
	}
	selectOnlyGraphNode("")
	app.graphSelectedEdgeID = id
	return true
}

func deleteGraphSelection() bool {
	g := ensureCurrentScenarioGraph()
	changed := false
	if app.graphSelectedEdgeID != "" {
		for i := len(g.Edges) - 1; i >= 0; i-- {
			if g.Edges[i].ID == app.graphSelectedEdgeID {
				g.Edges = append(g.Edges[:i], g.Edges[i+1:]...)
				changed = true
			}
		}
		app.graphSelectedEdgeID = ""
	}
	for _, id := range selectedGraphNodeIDs() {
		g.removeNode(id)
		changed = true
	}
	if changed {
		selectOnlyGraphNode("")
		persistCurrentScenarioGraph()
		layoutControls(app.hwnd)
		invalidateScenarioGraphWindows()
	}
	return changed
}

func deleteSelectedGraphEdge() bool {
	if app.graphSelectedEdgeID == "" {
		return false
	}
	g := ensureCurrentScenarioGraph()
	for i := len(g.Edges) - 1; i >= 0; i-- {
		if g.Edges[i].ID == app.graphSelectedEdgeID {
			g.Edges = append(g.Edges[:i], g.Edges[i+1:]...)
			app.graphSelectedEdgeID = ""
			persistCurrentScenarioGraph()
			invalidateScenarioGraphWindows()
			return true
		}
	}
	return false
}

func deleteSelectedGraphNodes() bool {
	ids := selectedGraphNodeIDs()
	if len(ids) == 0 {
		return false
	}
	g := ensureCurrentScenarioGraph()
	for _, id := range ids {
		g.removeNode(id)
	}
	selectOnlyGraphNode("")
	persistCurrentScenarioGraph()
	layoutControls(app.hwnd)
	invalidateScenarioGraphWindows()
	return true
}

func copyGraphSelection() bool {
	g := ensureCurrentScenarioGraph()
	selected := ensureGraphSelection()
	clip := scenarioGraphClipboardData{}
	for _, node := range g.Nodes {
		if selected[node.ID] {
			clip.Nodes = append(clip.Nodes, cloneScenarioGraph(ScenarioGraph{Nodes: []ScenarioGraphNode{node}}).Nodes[0])
		}
	}
	if len(clip.Nodes) == 0 {
		return false
	}
	for _, edge := range g.Edges {
		if selected[edge.From] && selected[edge.To] {
			clip.Edges = append(clip.Edges, edge)
		}
	}
	scenarioGraphClipboard = clip
	return true
}

func pasteGraphSelection() bool {
	if len(scenarioGraphClipboard.Nodes) == 0 {
		return false
	}
	g := ensureCurrentScenarioGraph()
	ids := map[string]string{}
	minX, minY := scenarioGraphClipboard.Nodes[0].X, scenarioGraphClipboard.Nodes[0].Y
	for _, node := range scenarioGraphClipboard.Nodes {
		minX, minY = math.Min(minX, node.X), math.Min(minY, node.Y)
	}
	z := g.Zoom
	if z <= 0 {
		z = 1
	}
	targetX := (float64(app.graphContextX-app.graphCanvasRect.Left) / z) - g.ViewX
	targetY := (float64(app.graphContextY-app.graphCanvasRect.Top) / z) - g.ViewY
	if !pointIn(app.graphCanvasRect, app.graphContextX, app.graphContextY) {
		targetX, targetY = minX+42, minY+42
	}
	selectOnlyGraphNode("")
	for _, source := range scenarioGraphClipboard.Nodes {
		node := cloneScenarioGraph(ScenarioGraph{Nodes: []ScenarioGraphNode{source}}).Nodes[0]
		oldID := node.ID
		node.ID = newAutomationID("node")
		node.X, node.Y = targetX+(source.X-minX), targetY+(source.Y-minY)
		for i := range node.Conditions {
			node.Conditions[i].ID = newAutomationID("cond")
		}
		for i := range node.Steps {
			node.Steps[i].ID = newAutomationID("step")
		}
		ids[oldID] = node.ID
		g.Nodes = append(g.Nodes, node)
		ensureGraphSelection()[node.ID] = true
		app.graphSelectedNodeID = node.ID
	}
	for _, source := range scenarioGraphClipboard.Edges {
		from, okFrom := ids[source.From]
		to, okTo := ids[source.To]
		if okFrom && okTo {
			g.Edges = append(g.Edges, ScenarioGraphEdge{ID: newAutomationID("edge"), From: from, FromPort: source.FromPort, To: to})
		}
	}
	persistCurrentScenarioGraph()
	return true
}

func cutGraphSelection() bool {
	if !copyGraphSelection() {
		return false
	}
	return deleteGraphSelection()
}

func duplicateGraphSelection() bool {
	if !copyGraphSelection() {
		return false
	}
	app.graphContextX, app.graphContextY = -10000, -10000
	return pasteGraphSelection()
}

func selectAllGraphNodes() {
	app.graphSelectedNodes = map[string]bool{}
	for _, node := range ensureCurrentScenarioGraph().Nodes {
		app.graphSelectedNodes[node.ID] = true
		app.graphSelectedNodeID = node.ID
	}
	app.graphSelectedEdgeID = ""
}

func beginGraphRightButton(x, y int32) {
	if app.graphEditorOpen {
		return
	}
	app.graphRightDown = true
	app.graphRightPanning = false
	app.graphRightStartX, app.graphRightStartY = x, y
	app.graphLastMouseX, app.graphLastMouseY = x, y
	app.graphContextOpen = false
}

func beginGraphMiddleButton(x, y int32) {
	if app.graphEditorOpen {
		return
	}
	app.graphMiddleDown = true
	app.graphMiddlePanning = true
	app.graphMiddleStartX, app.graphMiddleStartY = x, y
	app.graphLastMouseX, app.graphLastMouseY = x, y
	app.graphContextOpen = false
	pSetCapture.Call(scenarioGraphInputWindow())
}

func updateGraphMiddleButton(x, y int32) bool {
	if !app.graphMiddleDown {
		return false
	}
	g := ensureCurrentScenarioGraph()
	g.ViewX += float64(x-app.graphLastMouseX) / g.Zoom
	g.ViewY += float64(y-app.graphLastMouseY) / g.Zoom
	app.graphLastMouseX, app.graphLastMouseY = x, y
	invalidateScenarioGraphWindows()
	return true
}

func finishGraphMiddleButton() bool {
	if !app.graphMiddleDown {
		return false
	}
	app.graphMiddleDown, app.graphMiddlePanning = false, false
	pReleaseCapture.Call()
	persistCurrentScenarioGraph()
	return true
}

func updateGraphRightButton(x, y int32) bool {
	return false
}

func finishGraphRightButton(x, y int32) {
	if !app.graphRightDown {
		return
	}
	app.graphRightDown = false
	nodeHit := false
	for _, hit := range app.graphNodeHits {
		if pointIn(hit.Rect, x, y) {
			nodeHit = true
			if !graphNodeSelected(hit.ID) {
				selectOnlyGraphNode(hit.ID)
			}
			break
		}
	}
	if !nodeHit {
		if edge := graphEdgeHit(x, y); edge != "" {
			selectOnlyGraphNode("")
			app.graphSelectedEdgeID = edge
		}
	}
	app.graphContextX, app.graphContextY = x, y
	app.graphContextOpen = true
	invalidateScenarioGraphWindows()
}

func graphContextActions() []string {
	if app.graphSelectedEdgeID != "" && len(selectedGraphNodeIDs()) > 0 {
		return []string{"Настроить соединение", "Копировать", "Вырезать", "Дублировать", "Удалить блоки", "Удалить соединение", "Вставить", "Выделить всё", "Вписать схему"}
	}
	if app.graphSelectedEdgeID != "" {
		return []string{"Настроить соединение", "Удалить соединение", "Вписать схему"}
	}
	if len(selectedGraphNodeIDs()) > 0 {
		return []string{"Копировать", "Вырезать", "Дублировать", "Удалить", "Вставить", "Выделить всё", "Вписать схему"}
	}
	return []string{"Вставить", "Выделить всё", "Вписать схему"}
}

func drawGraphContextMenu(hdc uintptr) {
	if !app.graphContextOpen {
		return
	}
	actions := graphContextActions()
	w, rowH := 190, 34
	x, y := int(app.graphContextX), int(app.graphContextY)
	if x+w > int(app.graphCanvasRect.Right) {
		x = int(app.graphCanvasRect.Right) - w - 6
	}
	if y+len(actions)*rowH+12 > int(app.graphCanvasRect.Bottom) {
		y = int(app.graphCanvasRect.Bottom) - len(actions)*rowH - 12
	}
	app.graphContextRect = RECT{int32(x), int32(y), int32(x + w), int32(y + len(actions)*rowH + 8)}
	roundFill(hdc, app.graphContextRect, surfacePanelColor(), 10)
	if ui2d.active {
		d2dDrawRoundedOutline(app.graphContextRect, 10, 1, blendColor(theme.border, theme.accent2, .34))
	}
	app.graphContextItemRects = app.graphContextItemRects[:0]
	for i, action := range actions {
		r := RECT{int32(x + 4), int32(y + 4 + i*rowH), int32(x + w - 4), int32(y + 4 + (i+1)*rowH)}
		app.graphContextItemRects = append(app.graphContextItemRects, r)
		if pointIn(r, app.mouseX, app.mouseY) {
			roundFill(hdc, r, blendColor(surfaceButtonColor(), theme.accent, .20), 7)
		}
		drawText(hdc, action, int(r.Left)+12, int(r.Top), int(r.Right-r.Left)-24, int(r.Bottom-r.Top), 11, 550, theme.text, DT_LEFT|DT_VCENTER|DT_SINGLELINE)
	}
}

func handleGraphContextClick(x, y int32) bool {
	if !app.graphContextOpen {
		return false
	}
	actions := graphContextActions()
	for i, r := range app.graphContextItemRects {
		if !pointIn(r, x, y) || i >= len(actions) {
			continue
		}
		switch actions[i] {
		case "Копировать":
			copyGraphSelection()
		case "Вырезать":
			cutGraphSelection()
		case "Дублировать":
			duplicateGraphSelection()
		case "Удалить":
			deleteGraphSelection()
		case "Удалить блоки":
			deleteSelectedGraphNodes()
		case "Удалить соединение":
			deleteSelectedGraphEdge()
		case "Настроить соединение":
			editSelectedGraphConnection(app.graphContextX, app.graphContextY)
		case "Вставить":
			pasteGraphSelection()
		case "Выделить всё":
			selectAllGraphNodes()
		case "Вписать схему":
			fitScenarioGraph()
		}
		app.graphContextOpen = false
		invalidateScenarioGraphWindows()
		return true
	}
	app.graphContextOpen = false
	return pointIn(app.graphContextRect, x, y)
}

func editSelectedGraphConnection(x, y int32) bool {
	g := ensureCurrentScenarioGraph()
	for i, edge := range g.Edges {
		if edge.ID != app.graphSelectedEdgeID {
			continue
		}
		// If the wire already goes through a compact logic/junction node, edit
		// that node instead of inserting a second marker on top of it.
		for _, id := range []string{edge.From, edge.To} {
			if node := g.node(id); node != nil && (node.Kind == graphNodeLogic || node.Kind == graphNodeJunction) {
				selectOnlyGraphNode(node.ID)
				openGraphCompactEditor(node.ID, 0)
				return true
			}
		}
		z := g.Zoom
		if z <= 0 {
			z = 1
		}
		junction := newScenarioGraphNode(graphNodeJunction,
			float64(x-app.graphCanvasRect.Left)/z-g.ViewX-19,
			float64(y-app.graphCanvasRect.Top)/z-g.ViewY-19)
		g.Edges = append(g.Edges[:i], g.Edges[i+1:]...)
		g.Nodes = append(g.Nodes, junction)
		g.connect(edge.From, edge.FromPort, junction.ID)
		g.connect(junction.ID, graphPortNext, edge.To)
		selectOnlyGraphNode(junction.ID)
		persistCurrentScenarioGraph()
		openGraphCompactEditor(junction.ID, 0)
		return true
	}
	return false
}

func handleGraphKeyboard(vk uintptr) bool {
	if !scenarioGraphInteractionEnabled() {
		return false
	}
	if app.graphEditorOpen {
		if vk == 0x1B {
			if app.graphEditorSection != 0 {
				closeGraphFullEditor()
				return true
			}
			syncGraphCompactText()
			app.graphEditorOpen = false
			if app.graphEditorText != 0 {
				pShowWindow.Call(app.graphEditorText, SW_HIDE)
			}
			persistCurrentScenarioGraph()
			return true
		}
		// Native EDIT controls handle their own Ctrl+Z. If the modal editor
		// itself has focus, swallow the shortcut instead of falling through to
		// the unrelated main-window undo manager.
		return keyDown040(0x11) && (vk == 'Z' || vk == 'Y')
	}
	if app.graphContextOpen && vk == 0x1B {
		app.graphContextOpen = false
		invalidateScenarioGraphWindows()
		return true
	}
	ctrl := keyDown040(0x11)
	if vk == 0x2E {
		return deleteGraphSelection()
	}
	if !ctrl {
		return false
	}
	switch vk {
	case 'Z':
		return undoCurrentScenarioGraph()
	case 'Y':
		return redoCurrentScenarioGraph()
	case 'A':
		selectAllGraphNodes()
	case 'C':
		return copyGraphSelection()
	case 'X':
		return cutGraphSelection()
	case 'V':
		return pasteGraphSelection()
	case 'D':
		return duplicateGraphSelection()
	default:
		return false
	}
	invalidateScenarioGraphWindows()
	return true
}
