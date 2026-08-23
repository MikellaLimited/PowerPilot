//go:build windows

package main

import (
	"fmt"
	"math"
	"sort"
	"strings"
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

func scenarioGraphInteractionEnabled() bool {
	return app.section == 7 || app.section == 13 || (scenarioGraphDetachedInput && app.graphWindow != 0)
}

func currentScenarioGraph() *ScenarioGraph {
	if session := currentScenarioGraphSession(); session != nil {
		return &session.Graph
	}
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
	if app.settings.GraphAutoRemoveSingleJunction {
		pruneSingleInputJunctions(currentScenarioGraph())
	}
	if session := currentScenarioGraphSession(); session != nil {
		session.observeGraphChange()
		invalidateScenarioGraphWindows()
		return
	}
	if app.scenarioSavedDraft {
		invalidateScenarioGraphWindows()
		return
	}
	saveSettings()
	saveDraftAutosave()
	invalidateScenarioGraphWindows()
}

func selectedGraphNode() *ScenarioGraphNode {
	if app.graphSelectedNodeID == "" {
		return nil
	}
	return ensureCurrentScenarioGraph().node(app.graphSelectedNodeID)
}

func scenarioGraphInputWindow() uintptr {
	if app.graphWindow != 0 {
		return app.graphWindow
	}
	return app.hwnd
}

func syncCurrentGraphFromLegacy() {
	// Detached editors own their graph state. Their full-screen forms write
	// directly into the selected node, so copying the main window settings here
	// would leak values between independently opened editor windows.
	if currentScenarioGraphSession() != nil {
		persistCurrentScenarioGraph()
		return
	}
	g := ensureCurrentScenarioGraph()
	tr := g.trigger()
	if selected := selectedGraphNode(); selected != nil && selected.Kind == graphNodeTrigger {
		tr = selected
	}
	if tr != nil {
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

func layoutScenarioGraphEditor(body RECT, detached bool) {
	left := int(body.Left) + 14
	top := int(body.Top) + 58
	right := int(body.Right) - 14
	bottom := int(body.Bottom) - 14
	if !detached {
		app.savedScenarioNameRect, app.savedScenarioSaveRect, app.savedScenarioCancelRect, app.savedScenarioCheckRect = RECT{}, RECT{}, RECT{}, RECT{}
	}
	if !detached && app.section == 13 && app.scenarioSavedDraft {
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
	app.graphSettingsRect = RECT{body.Right - 58, body.Top + 12, body.Right - 14, body.Top + 46}
	app.graphSaveRect = RECT{body.Right - 166, body.Top + 12, body.Right - 68, body.Top + 46}
	app.previewRect = RECT{body.Right - 274, body.Top + 12, body.Right - 176, body.Top + 46}
	if !detached {
		app.graphDetachRect = RECT{body.Right - 310, body.Top + 12, body.Right - 136, body.Top + 46}
	}
	app.graphCanvasRect = RECT{int32(left), int32(top), int32(right), int32(bottom)}
	paletteTop := int(body.Top) + 14
	paletteLeft := left
	available := max(240, right-paletteLeft-350)
	paletteGap := 5
	paletteW := minInt(88, max(58, (available-paletteGap*3)/4))
	for i := range app.graphPaletteRects {
		x := paletteLeft + i*(paletteW+paletteGap)
		app.graphPaletteRects[i] = RECT{int32(x), int32(paletteTop), int32(x + paletteW), int32(paletteTop + 30)}
	}
	nameLeft := int(app.graphPaletteRects[len(app.graphPaletteRects)-1].Right) + 14
	nameRight := int(app.previewRect.Left) - 14
	if nameRight < nameLeft+180 {
		nameLeft = max(left, nameRight-180)
	}
	app.graphNameRect = RECT{int32(nameLeft), body.Top + 12, int32(nameRight), body.Top + 46}
	zoomWidths := []int{36, 36, 78}
	zoomX := right - 162
	for i, width := range zoomWidths {
		app.graphZoomRects[i] = RECT{int32(zoomX), int32(top + 8), int32(zoomX + width), int32(top + 40)}
		zoomX += width + 5
	}
}

func graphNodeHeight(n ScenarioGraphNode) float64 {
	count := 0
	switch n.Kind {
	case graphNodeTrigger:
		if strings.TrimSpace(graphTriggerSummary(n)) != "" {
			count++
		}
		count += len(n.Conditions)
	case graphNodeCondition:
		count = len(n.Conditions)
	case graphNodeAction:
		count = len(n.Steps)
	default:
		if strings.TrimSpace(graphNodeSummary(n)) != "" {
			count = 1
		}
	}
	return float64(62 + minInt(count, 4)*24)
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
	if n.Kind == graphNodeJunction || n.Kind == graphNodeLogic {
		w, h = 38*z, 38*z
	}
	return RECT{int32(math.Round(x)), int32(math.Round(y)), int32(math.Round(x + w)), int32(math.Round(y + h))}
}

func graphOutputPoint(g *ScenarioGraph, n ScenarioGraphNode, port string) (float32, float32) {
	r := graphNodeScreenRect(g, n)
	if n.Kind == graphNodeJunction || n.Kind == graphNodeLogic {
		return float32(r.Right), float32((r.Top + r.Bottom) / 2)
	}
	ports := graphNodePorts(n.Kind)
	idx := 0
	for i, p := range ports {
		if p == port {
			idx = i
		}
	}
	y := float32(r.Top) + float32(46+idx*22)*float32(g.Zoom)
	return float32(r.Right), y
}

func graphInputPoint(g *ScenarioGraph, n ScenarioGraphNode) (float32, float32) {
	r := graphNodeScreenRect(g, n)
	return float32(r.Left), float32((r.Top + r.Bottom) / 2)
}

func drawGraphConnection(x1, y1, x2, y2, stroke float32, color uint32) {
	mid := (x1 + x2) / 2
	if x2 < x1+42 {
		mid = x1 + 42
	}
	d2dDrawLine(x1, y1, mid, y1, stroke, color)
	d2dDrawLine(mid, y1, mid, y2, stroke, color)
	d2dDrawLine(mid, y2, x2, y2, stroke, color)
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
	case graphNodeLogic:
		return blendColor(surfaceButtonColor(), rgb(171, 106, 255), .24)
	case graphNodeJunction:
		return blendColor(surfaceButtonColor(), theme.accent2, .32)
	}
	return surfaceButtonColor()
}

func drawScenarioGraphEditor(hdc uintptr, body RECT, w int, detached bool) {
	g := ensureCurrentScenarioGraph()
	app.graphNodeHits = app.graphNodeHits[:0]
	app.graphPortHits = app.graphPortHits[:0]
	app.graphFunctionHits = app.graphFunctionHits[:0]
	roundFill(hdc, app.graphNameRect, surfaceButtonColor(), 9)
	if app.graphNameEdit != 0 {
		move(app.graphNameEdit, int(app.graphNameRect.Left)+8, int(app.graphNameRect.Top)+6, max(20, int(app.graphNameRect.Right-app.graphNameRect.Left)-16), 22)
		if app.graphEditorOpen || app.graphCloseConfirm || app.graphSettingsOpen {
			pShowWindow.Call(app.graphNameEdit, SW_HIDE)
		} else {
			pShowWindow.Call(app.graphNameEdit, SW_SHOW)
		}
	}
	drawButton(hdc, app.previewRect, "Проверить", false)
	drawButton(hdc, app.graphSaveRect, "Сохранить", true)
	gearHover := hoverAmount(app.graphSettingsRect)
	gearRect := expandRect(app.graphSettingsRect, int32(2*gearHover+.5))
	roundFill(hdc, gearRect, surfaceButtonColor(), 10)
	if gearHover > 0 && ui2d.active {
		d2dDrawRoundedOutline(gearRect, 10, 1.2, blendColor(theme.border, theme.accent2, .55))
	}
	iconInset := int32(8)
	d2dDrawSettingsIconRotated(RECT{gearRect.Left + iconInset, gearRect.Top + 3, gearRect.Right - iconInset, gearRect.Bottom - 3}, 150*gearHover)
	if !detached {
		drawButton(hdc, app.graphDetachRect, "Открыть отдельно", false)
	}
	labels := []string{"+ Усл.", "+ Действ.", "+ Пауза", "+ Финал"}
	for i, r := range app.graphPaletteRects {
		drawButton(hdc, r, labels[i], false)
	}
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
		if app.graphMarquee {
			marquee := graphRectNormalized(app.graphMarqueeStartX, app.graphMarqueeStartY, app.graphMarqueeX, app.graphMarqueeY)
			d2dFillRoundedOpacity(marquee, theme.accent2, 3, .16)
			d2dDrawRoundedOutline(marquee, 3, 1, theme.accent2)
		}
		for _, e := range g.Edges {
			from, to := g.node(e.From), g.node(e.To)
			if from == nil || to == nil {
				continue
			}
			x1, y1 := graphOutputPoint(g, *from, e.FromPort)
			x2, y2 := graphInputPoint(g, *to)
			c := graphPortColor(e.FromPort)
			stroke := float32(maxFloat(.7, 1.8*g.Zoom))
			if e.ID == app.graphSelectedEdgeID {
				stroke += 2
				c = blendColor(c, theme.text, .24)
			}
			drawGraphConnection(x1, y1, x2, y2, stroke, c)
		}
		if app.graphConnectingNodeID != "" {
			if n := g.node(app.graphConnectingNodeID); n != nil {
				x1, y1 := graphOutputPoint(g, *n, app.graphConnectingPort)
				drawGraphConnection(x1, y1, float32(app.mouseX), float32(app.mouseY), float32(maxFloat(.7, 1.8*g.Zoom)), graphPortColor(app.graphConnectingPort))
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
	if !detached && app.section == 13 && app.scenarioSavedDraft {
		drawText(hdc, "Название", int(app.savedScenarioNameRect.Left), int(app.savedScenarioNameRect.Top)-18, int(app.savedScenarioNameRect.Right-app.savedScenarioNameRect.Left), 16, 9, 500, theme.muted, DT_LEFT|DT_VCENTER|DT_SINGLELINE)
		roundFill(hdc, app.savedScenarioNameRect, surfaceButtonColor(), 9)
		drawButton(hdc, app.savedScenarioSaveRect, "Сохранить", true)
		drawButton(hdc, app.savedScenarioCancelRect, "Отмена", false)
		drawButton(hdc, app.savedScenarioCheckRect, "Проверка", false)
	}
	if !app.graphCloseConfirm {
		drawGraphContextMenu(hdc)
		drawGraphCompactEditor(hdc, body)
	} else {
		if app.graphEditorText != 0 {
			pShowWindow.Call(app.graphEditorText, SW_HIDE)
		}
		for _, edit := range graphEditorEdits {
			if edit != 0 {
				pShowWindow.Call(edit, SW_HIDE)
			}
		}
	}
	drawScenarioGraphCloseConfirm(hdc, body)
	drawScenarioGraphSettingsPanel(hdc, body)
}

func drawScenarioGraphSettingsPanel(hdc uintptr, body RECT) {
	if !app.graphSettingsOpen || app.graphCloseConfirm {
		return
	}
	if ui2d.active {
		d2dFillRoundedOpacity(body, rgb(0, 0, 0), 18, .38)
	}
	w, h := minInt(560, int(body.Right-body.Left)-48), 190
	x, y := int(body.Right)-w-24, int(body.Top)+60
	app.graphSettingsPanelRect = RECT{int32(x), int32(y), int32(x + w), int32(y + h)}
	roundFill(hdc, app.graphSettingsPanelRect, surfacePanelColor(), 16)
	drawText(hdc, "Настройки редактора", x+22, y+18, w-44, 28, 18, 700, theme.text, DT_LEFT|DT_VCENTER|DT_SINGLELINE)
	app.graphSettingsToggleRect = RECT{int32(x + 22), int32(y + 70), int32(x + 50), int32(y + 98)}
	drawToggle(hdc, app.graphSettingsToggleRect, app.settings.GraphAutoRemoveSingleJunction)
	drawText(hdc, "Упрощать одиночные соединения", x+62, y+66, w-84, 24, 12, 650, theme.text, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
	drawText(hdc, "Узел с одним входящим проводом автоматически заменяется прямым соединением.", x+62, y+92, w-84, 42, 10, 450, theme.muted, DT_LEFT|DT_VCENTER|DT_WORDBREAK)
}

func drawScenarioGraphCloseConfirm(hdc uintptr, body RECT) {
	if !app.graphCloseConfirm {
		return
	}
	if ui2d.active {
		d2dFillRoundedOpacity(body, rgb(0, 0, 0), 18, .52)
	} else {
		fill(hdc, body, blendColor(theme.bg, rgb(0, 0, 0), .48))
	}
	w := minInt(510, int(body.Right-body.Left)-48)
	h := 210
	x := int(body.Left+body.Right)/2 - w/2
	y := int(body.Top+body.Bottom)/2 - h/2
	app.graphCloseConfirmRect = RECT{int32(x), int32(y), int32(x + w), int32(y + h)}
	roundFill(hdc, app.graphCloseConfirmRect, surfacePanelColor(), 16)
	if ui2d.active {
		d2dDrawRoundedOutline(app.graphCloseConfirmRect, 16, 1.2, blendColor(theme.border, theme.accent2, .46))
	}
	app.graphCloseCancelRect = RECT{int32(x + w - 52), int32(y + 10), int32(x + w - 10), int32(y + 52)}
	roundFill(hdc, app.graphCloseCancelRect, surfaceButtonColor(), 10)
	if ui2d.active {
		cx := float32(app.graphCloseCancelRect.Left+app.graphCloseCancelRect.Right) / 2
		cy := float32(app.graphCloseCancelRect.Top+app.graphCloseCancelRect.Bottom) / 2
		d2dDrawLine(cx-6, cy-6, cx+6, cy+6, 1.8, theme.text)
		d2dDrawLine(cx+6, cy-6, cx-6, cy+6, 1.8, theme.text)
	} else {
		drawText(hdc, "×", int(app.graphCloseCancelRect.Left), int(app.graphCloseCancelRect.Top)-1, int(app.graphCloseCancelRect.Right-app.graphCloseCancelRect.Left), int(app.graphCloseCancelRect.Bottom-app.graphCloseCancelRect.Top), 19, 600, theme.text, DT_CENTER|DT_VCENTER|DT_SINGLELINE)
	}
	drawText(hdc, "Закрыть редактор?", x+24, y+22, w-76, 28, 19, 700, theme.text, DT_LEFT|DT_VCENTER|DT_SINGLELINE)
	drawText(hdc, "Сохрани изменения задачи или закрой окно без сохранения.", x+24, y+62, w-48, 48, 11, 450, theme.muted, DT_LEFT|DT_VCENTER|DT_WORDBREAK)
	buttonY := y + 142
	gap := 12
	bw := (w - 48 - gap) / 2
	app.graphCloseSaveRect = RECT{int32(x + 24), int32(buttonY), int32(x + 24 + bw), int32(buttonY + 44)}
	app.graphCloseDiscardRect = RECT{int32(x + 24 + bw + gap), int32(buttonY), int32(x + w - 24), int32(buttonY + 44)}
	drawButton(hdc, app.graphCloseSaveRect, "Сохранить", true)
	drawOutlinedButton(hdc, app.graphCloseDiscardRect, "Не сохранять", theme.danger)
}

func commitOpenGraphEditorBeforeClose() {
	if !app.graphEditorOpen {
		return
	}
	if app.graphEditorSection != 0 {
		commitGraphFullEditorDraft()
		closeGraphFullEditor()
		return
	}
	syncGraphCompactText()
	app.graphEditorOpen = false
	if app.graphEditorText != 0 {
		pShowWindow.Call(app.graphEditorText, SW_HIDE)
	}
	persistCurrentScenarioGraph()
}

func handleScenarioGraphCloseConfirm(x, y int32) bool {
	if !app.graphCloseConfirm {
		return false
	}
	if pointIn(app.graphCloseCancelRect, x, y) {
		app.graphCloseConfirm = false
		playUI(clickSound)
		invalidateScenarioGraphWindows()
		return true
	}
	session := currentScenarioGraphSession()
	if session == nil {
		return true
	}
	if pointIn(app.graphCloseSaveRect, x, y) {
		playUI(clickSound)
		commitOpenGraphEditorBeforeClose()
		if !saveScenarioGraphTaskSession(session) {
			return true
		}
		session.CloseApproved = true
		pSendMessageW.Call(session.HWND, WM_CLOSE, 0, 0)
		return true
	}
	if pointIn(app.graphCloseDiscardRect, x, y) {
		playUI(clickSound)
		session.Discard = true
		session.CloseApproved = true
		pSendMessageW.Call(session.HWND, WM_CLOSE, 0, 0)
		return true
	}
	return true
}

func drawScenarioGraphNode(hdc uintptr, g *ScenarioGraph, n ScenarioGraphNode) {
	r := graphNodeScreenRect(g, n)
	if r.Right < app.graphCanvasRect.Left || r.Left > app.graphCanvasRect.Right || r.Bottom < app.graphCanvasRect.Top || r.Top > app.graphCanvasRect.Bottom {
		return
	}
	selected := graphNodeSelected(n.ID)
	z := maxFloat(.1, g.Zoom)
	scaled := func(value, minimum int) int32 { return int32(max(minimum, int(float64(value)*z+.5))) }
	fillColor := graphNodeColor(n.Kind)
	if n.Kind == graphNodeJunction || n.Kind == graphNodeLogic {
		radius := (r.Right - r.Left) / 2
		if radius < 3 {
			radius = 3
		}
		roundFill(hdc, r, fillColor, radius)
		if ui2d.active {
			outline, stroke := theme.accent2, float32(1)
			if n.Kind == graphNodeLogic {
				outline = rgb(171, 106, 255)
			}
			if selected {
				stroke = 2.2
			}
			d2dDrawRoundedOutline(r, float32(radius), stroke, outline)
		}
		app.graphNodeHits = append(app.graphNodeHits, GraphNodeHit{ID: n.ID, Rect: r, Header: r})
		inX, inY := graphInputPoint(g, n)
		outX, outY := graphOutputPoint(g, n, graphPortNext)
		pr := scaled(5, 2)
		if pr < 2 {
			pr = 2
		}
		inRect := RECT{int32(inX) - pr, int32(inY) - pr, int32(inX) + pr, int32(inY) + pr}
		outRect := RECT{int32(outX) - pr, int32(outY) - pr, int32(outX) + pr, int32(outY) + pr}
		roundFill(hdc, inRect, theme.text, pr)
		roundFill(hdc, outRect, theme.accent2, pr)
		app.graphPortHits = append(app.graphPortHits, GraphPortHit{NodeID: n.ID, Input: true, Rect: inRect}, GraphPortHit{NodeID: n.ID, Port: graphPortNext, Rect: outRect})
		label := "•"
		if n.Kind == graphNodeLogic {
			label = graphLogicName(n.LogicOp)
		}
		drawText(hdc, label, int(r.Left), int(r.Top), int(r.Right-r.Left), int(r.Bottom-r.Top), max(3, int(8*z)), 700, theme.text, DT_CENTER|DT_VCENTER|DT_SINGLELINE)
		return
	}
	roundFill(hdc, r, fillColor, scaled(12, 3))
	if ui2d.active {
		outline := blendColor(theme.border, theme.text, .35)
		stroke := float32(1)
		if selected {
			outline, stroke = theme.accent2, 2.2
		}
		d2dDrawRoundedOutline(r, float32(scaled(12, 3)), stroke, outline)
	}
	headerH := scaled(34, 12)
	header := RECT{r.Left, r.Top, r.Right, r.Top + headerH}
	deleteR := RECT{}
	app.graphNodeHits = append(app.graphNodeHits, GraphNodeHit{ID: n.ID, Rect: r, Header: header, Delete: deleteR})
	titleInset, titleTop := scaled(12, 4), scaled(5, 2)
	title := graphNodeKindName(n.Kind)
	if n.Kind == graphNodeLogic {
		title = graphLogicName(n.LogicOp)
	}
	drawText(hdc, title, int(r.Left+titleInset), int(r.Top+titleTop), max(1, int(r.Right-r.Left-titleInset*2)), max(1, int(headerH-titleTop*2)), max(3, int(11*z)), 650, theme.text, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
	items := []string{}
	itemKind := 0
	switch n.Kind {
	case graphNodeTrigger:
		items = append(items, graphTriggerSummary(n))
		if len(n.Conditions) > 0 {
			items = append(items, conditionSummary(n.Conditions[0]))
		}
		itemKind = 1
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
		if len(n.Steps) > 0 {
			items = append(items, stepSummary(n.Steps[0]))
		}
	case graphNodeWait:
		itemKind = 3
		items = append(items, graphNodeSummary(n))
	default:
		items = append(items, graphNodeSummary(n))
	}
	// Empty condition blocks stay visually compact. Double-clicking their body
	// still opens the editor, but there is no fake condition row to target.
	rowY := r.Top + headerH + scaled(5, 2)
	rowH := scaled(22, 6)
	rowInset, textInset := scaled(9, 3), scaled(4, 1)
	for i := 0; i < len(items) && i < 4; i++ {
		rr := RECT{r.Left + rowInset, rowY + int32(i)*rowH, r.Right - rowInset, rowY + int32(i+1)*rowH - scaled(2, 1)}
		reserved := scaled(66, 20)
		textRight := rr.Right - reserved
		if textRight < rr.Left+scaled(28, 8) {
			textRight = rr.Right
		}
		drawText(hdc, items[i], int(rr.Left+textInset), int(rr.Top), max(1, int(textRight-rr.Left-textInset)), max(1, int(rr.Bottom-rr.Top)), max(3, int(9*z)), 450, theme.text, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
		if itemKind != 0 && !(n.Kind == graphNodeTrigger && i == 0) {
			index := i
			if n.Kind == graphNodeTrigger {
				index--
			}
			app.graphFunctionHits = append(app.graphFunctionHits, GraphFunctionHit{NodeID: n.ID, Kind: itemKind, Index: index, Rect: rr})
		}
	}
	inX, inY := graphInputPoint(g, n)
	portRadius := scaled(7, 2)
	inRect := RECT{int32(inX) - portRadius, int32(inY) - portRadius, int32(inX) + portRadius, int32(inY) + portRadius}
	roundFill(hdc, inRect, theme.text, portRadius)
	app.graphPortHits = append(app.graphPortHits, GraphPortHit{NodeID: n.ID, Input: true, Rect: inRect})
	for _, port := range graphNodePorts(n.Kind) {
		x, y := graphOutputPoint(g, n, port)
		pr := RECT{int32(x) - portRadius, int32(y) - portRadius, int32(x) + portRadius, int32(y) + portRadius}
		pc := graphPortColor(port)
		roundFill(hdc, pr, pc, portRadius)
		app.graphPortHits = append(app.graphPortHits, GraphPortHit{NodeID: n.ID, Port: port, Rect: pr})
		labelW, labelH := scaled(54, 18), scaled(18, 7)
		drawText(hdc, graphPortName(port), int(r.Right-labelW-scaled(12, 4)), int(y)-int(labelH)/2, int(labelW), int(labelH), max(3, int(8*z)), 550, pc, DT_RIGHT|DT_VCENTER|DT_SINGLELINE)
	}
}

func addGraphNode(kind int) {
	g := ensureCurrentScenarioGraph()
	if len(g.Nodes) >= 64 {
		showNotification("PowerPilot", "В одной схеме может быть не больше 64 блоков.")
		return
	}
	wx := (float64(app.graphCanvasRect.Right-app.graphCanvasRect.Left)/2)/g.Zoom - g.ViewX - 112
	wy := (float64(app.graphCanvasRect.Bottom-app.graphCanvasRect.Top)/2)/g.Zoom - g.ViewY - 55
	n := newScenarioGraphNode(kind, wx+float64(len(g.Nodes)%3)*28, wy+float64(len(g.Nodes)%4)*24)
	g.Nodes = append(g.Nodes, n)
	app.graphSelectedNodeID = n.ID
	persistCurrentScenarioGraph()
	if currentScenarioGraphSession() == nil {
		layoutControls(app.hwnd)
		invalidate(app.hwnd)
	}
}

func addGraphPaletteNode(index int) {
	kind := graphNodeTrigger
	switch index {
	case 0:
		kind = graphNodeTrigger
	case 1:
		kind = graphNodeAction
	case 2:
		kind = graphNodeWait
	case 3:
		kind = graphNodeFinish
	default:
		return
	}
	addGraphNode(kind)
}

func connectGraphWireToEdge(g *ScenarioGraph, edgeID, from, port string, x, y int32) bool {
	if edgeID == "" || from == "" {
		return false
	}
	for i, edge := range g.Edges {
		if edge.ID != edgeID {
			continue
		}
		z := g.Zoom
		if z <= 0 {
			z = 1
		}
		junction := newScenarioGraphNode(graphNodeJunction,
			float64(x-app.graphCanvasRect.Left)/z-g.ViewX-19,
			float64(y-app.graphCanvasRect.Top)/z-g.ViewY-19)
		oldTo := edge.To
		g.Edges = append(g.Edges[:i], g.Edges[i+1:]...)
		g.Nodes = append(g.Nodes, junction)
		g.connect(edge.From, graphPortNext, junction.ID)
		g.connect(from, port, junction.ID)
		g.connect(junction.ID, graphPortNext, oldTo)
		selectOnlyGraphNode(junction.ID)
		return true
	}
	return false
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
	if !scenarioGraphInteractionEnabled() {
		return false
	}
	g := ensureCurrentScenarioGraph()
	if handleScenarioGraphCloseConfirm(x, y) {
		return true
	}
	if app.graphSettingsOpen {
		if pointIn(app.graphSettingsToggleRect, x, y) {
			app.settings.GraphAutoRemoveSingleJunction = !app.settings.GraphAutoRemoveSingleJunction
			saveSettings()
			playUI(clickSound)
			invalidateScenarioGraphWindows()
			return true
		}
		if !pointIn(app.graphSettingsPanelRect, x, y) {
			app.graphSettingsOpen = false
			playUI(clickSound)
			invalidateScenarioGraphWindows()
		}
		return true
	}
	if currentScenarioGraphSession() != nil && pointIn(app.graphSettingsRect, x, y) {
		app.graphSettingsOpen = true
		playUI(openSound)
		invalidateScenarioGraphWindows()
		return true
	}
	if !scenarioGraphDetachedInput && pointIn(app.graphDetachRect, x, y) {
		if !closeScenarioGraphSessionsForCurrentTarget() {
			openScenarioGraphWindow()
		}
		return true
	}
	if handleGraphCompactEditorClick(x, y) || handleGraphContextClick(x, y) {
		return true
	}
	if pointIn(app.previewRect, x, y) {
		if currentScenarioGraphSession() != nil {
			issues := validateScenarioGraph(*g)
			if len(issues) == 0 {
				showNotification("PowerPilot", "Схема задачи проверена: ошибок нет.")
				playUI(successSound)
			} else {
				showNotification("PowerPilot", graphValidationText(issues))
			}
			return true
		}
		app.checkReturnSection = app.section
		app.section = 12
		playUI(openSound)
		startPageAnimation()
		updateInputVisibility()
		focusMainFromScenarioGraph()
		invalidateScenarioGraphWindows()
		return true
	}
	if currentScenarioGraphSession() != nil && pointIn(app.graphSaveRect, x, y) {
		if saveScenarioGraphTaskSession(currentScenarioGraphSession()) {
			playUI(successSound)
		}
		return true
	}
	for i, r := range app.graphPaletteRects {
		if pointIn(r, x, y) {
			addGraphPaletteNode(i)
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
				app.graphConnectingNodeID, app.graphConnectingPort = "", ""
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
	if app.graphConnectingNodeID != "" {
		if edgeID := graphEdgeHit(x, y); edgeID != "" && connectGraphWireToEdge(g, edgeID, app.graphConnectingNodeID, app.graphConnectingPort, x, y) {
			app.graphConnectingNodeID, app.graphConnectingPort = "", ""
			persistCurrentScenarioGraph()
			playUI(successSound)
			return true
		}
	}
	for _, h := range app.graphNodeHits {
		if pointIn(h.Add, x, y) {
			selectOnlyGraphNode(h.ID)
			if node := g.node(h.ID); node != nil && node.Kind == graphNodeTrigger {
				openGraphConditionEditor(h.ID, -1)
			} else {
				openGraphCompactEditor(h.ID, -1)
			}
			return true
		}
		if pointIn(h.Rect, x, y) {
			beginGraphNodeDrag(h.ID, x, y)
			return true
		}
	}
	if selectGraphEdgeAt(x, y) {
		invalidateScenarioGraphWindows()
		return true
	}
	if pointIn(app.graphCanvasRect, x, y) {
		app.graphConnectingNodeID, app.graphConnectingPort = "", ""
		beginGraphMarquee(x, y)
		invalidateScenarioGraphWindows()
		return true
	}
	return false
}

func handleScenarioGraphMouseMove(x, y int32) bool {
	if updateGraphMiddleButton(x, y) {
		return true
	}
	if updateGraphRightButton(x, y) {
		return true
	}
	if app.graphMarquee {
		updateGraphMarquee(x, y)
		invalidateScenarioGraphWindows()
		return true
	}
	if !app.graphDragging {
		return false
	}
	g := ensureCurrentScenarioGraph()
	dx, dy := float64(x-app.graphLastMouseX)/g.Zoom, float64(y-app.graphLastMouseY)/g.Zoom
	app.graphLastMouseX, app.graphLastMouseY = x, y
	if app.graphDragging {
		for _, id := range selectedGraphNodeIDs() {
			if n := g.node(id); n != nil {
				n.X += dx
				n.Y += dy
			}
		}
	}
	invalidateScenarioGraphWindows()
	return true
}

func finishScenarioGraphPointer() bool {
	if finishGraphMarquee() {
		return true
	}
	if !app.graphDragging {
		return false
	}
	app.graphDragging = false
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
