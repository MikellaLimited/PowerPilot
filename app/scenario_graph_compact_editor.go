//go:build windows

package main

import (
	"fmt"
	"strconv"
	"strings"
	"unsafe"
)

var (
	pSetParentGraphEditor = user32.NewProc("SetParent")
	pGetParentGraphEditor = user32.NewProc("GetParent")
)

func openGraphCompactEditor(nodeID string, item int) {
	node := ensureCurrentScenarioGraph().node(nodeID)
	if node == nil {
		return
	}
	selectOnlyGraphNode(nodeID)
	app.graphEditorOpen = true
	app.graphEditorNodeID = nodeID
	app.graphEditorItem = max(item, 0)
	app.graphContextOpen = false
	app.graphEditorSection = 0
	if node.Kind != graphNodeWait {
		openGraphFullEditor(node, item)
		invalidateScenarioGraphWindows()
		return
	}
	ensureGraphCompactTextControl()
	loadGraphCompactText()
	invalidateScenarioGraphWindows()
}

func openGraphFullEditor(node *ScenarioGraphNode, item int) {
	oldSection := app.section
	switch node.Kind {
	case graphNodeCondition:
		openConditionEditor(item)
		app.graphEditorSection = 8
	case graphNodeAction:
		openStepEditor(item)
		app.graphEditorSection = 9
	case graphNodeTrigger:
		loadGraphTriggerIntoLegacyEditor(node)
		app.graphEditorSection = 15
	case graphNodeFinish:
		app.graphEditorSection = 14
	}
	app.section = oldSection
	app.pageAnim = 1
	if app.graphEditorText != 0 {
		pShowWindow.Call(app.graphEditorText, SW_HIDE)
	}
}

func loadGraphTriggerIntoLegacyEditor(node *ScenarioGraphNode) {
	app.selectedMode = node.Mode
	if app.scenarioSavedDraft {
		t := &app.savedEditDraft
		t.Mode, t.DelayHours, t.DelayMinutes, t.DelaySeconds = node.Mode, node.DelayHours, node.DelayMins, node.DelaySecs
		t.Exact, t.IdleMinutes, t.WatchProcess, t.Recurrence = node.Exact, node.IdleSecs, node.Process, node.Recurrence
	} else {
		app.settings.Mode = node.Mode
		app.settings.DelayHours, app.settings.DelayMinutes, app.settings.DelaySeconds = node.DelayHours, node.DelayMins, node.DelaySecs
		app.settings.Exact, app.settings.IdleMinutes, app.settings.WatchProcess, app.settings.Recurrence = node.Exact, node.IdleSecs, node.Process, node.Recurrence
	}
	pSetWindowTextW.Call(app.edits[idDelayHours], uintptr(unsafe.Pointer(wstr(strconv.Itoa(node.DelayHours)))))
	pSetWindowTextW.Call(app.edits[idDelayMinutes], uintptr(unsafe.Pointer(wstr(strconv.Itoa(node.DelayMins)))))
	pSetWindowTextW.Call(app.edits[idDelaySeconds], uintptr(unsafe.Pointer(wstr(strconv.Itoa(node.DelaySecs)))))
	setExactFields(node.Exact)
	pSetWindowTextW.Call(app.edits[idIdleMinutes], uintptr(unsafe.Pointer(wstr(strconv.Itoa(max(node.IdleSecs, 1))))))
	pSetWindowTextW.Call(app.edits[idWatchProcess], uintptr(unsafe.Pointer(wstr(node.Process))))
	pSetWindowTextW.Call(app.edits[idScheduleTime], uintptr(unsafe.Pointer(wstr(node.Recurrence.TimeHHMM))))
}

func ensureGraphCompactTextControl() {
	if app.graphWindow == 0 || app.graphEditorText != 0 {
		return
	}
	hwnd, _, _ := pCreateWindowExW.Call(0, uintptr(unsafe.Pointer(wstr("EDIT"))), uintptr(unsafe.Pointer(wstr(""))), WS_CHILD|WS_VISIBLE|WS_BORDER|ES_LEFT, 0, 0, 100, 30, app.graphWindow, 9901, 0, 0)
	app.graphEditorText = hwnd
	if hwnd != 0 && app.font != 0 {
		pSendMessageW.Call(hwnd, WM_SETFONT, app.font, 1)
	}
}

func graphCompactTextRelevant(node *ScenarioGraphNode) bool {
	return node != nil && ((node.Kind == graphNodeTrigger && (node.Mode == 1 || node.Mode == 3 || node.Mode == 4)) || (node.Kind == graphNodeCondition && len(node.Conditions) > 0) || (node.Kind == graphNodeAction && len(node.Steps) > 0))
}

func loadGraphCompactText() {
	if app.graphEditorText == 0 {
		return
	}
	node := ensureCurrentScenarioGraph().node(app.graphEditorNodeID)
	value := ""
	if node != nil {
		switch node.Kind {
		case graphNodeTrigger:
			switch node.Mode {
			case 1:
				value = node.Exact
			case 3:
				value = node.Process
			case 4:
				value = node.Recurrence.TimeHHMM
			}
		case graphNodeCondition:
			if len(node.Conditions) > 0 {
				value = node.Conditions[clampInt(app.graphEditorItem, 0, len(node.Conditions)-1)].Text
			}
		case graphNodeAction:
			if len(node.Steps) > 0 {
				value = node.Steps[clampInt(app.graphEditorItem, 0, len(node.Steps)-1)].Text
			}
		}
	}
	pSetWindowTextW.Call(app.graphEditorText, uintptr(unsafe.Pointer(wstr(value))))
}

func syncGraphCompactText() {
	if app.graphEditorText == 0 {
		return
	}
	node := ensureCurrentScenarioGraph().node(app.graphEditorNodeID)
	if !graphCompactTextRelevant(node) {
		return
	}
	value := strings.TrimSpace(getText(app.graphEditorText))
	switch node.Kind {
	case graphNodeTrigger:
		switch node.Mode {
		case 1:
			node.Exact = value
		case 3:
			node.Process = value
		case 4:
			node.Recurrence.TimeHHMM = value
		}
	case graphNodeCondition:
		if len(node.Conditions) > 0 {
			node.Conditions[clampInt(app.graphEditorItem, 0, len(node.Conditions)-1)].Text = value
		}
	case graphNodeAction:
		if len(node.Steps) > 0 {
			node.Steps[clampInt(app.graphEditorItem, 0, len(node.Steps)-1)].Text = value
		}
	}
}

func handleGraphDoubleClick(x, y int32) bool {
	if app.section != 7 && app.section != 13 {
		return false
	}
	if app.graphWindow != 0 && !scenarioGraphDetachedInput {
		return false
	}
	for _, function := range app.graphFunctionHits {
		if pointIn(function.Rect, x, y) {
			openGraphCompactEditor(function.NodeID, function.Index)
			return true
		}
	}
	for _, hit := range app.graphNodeHits {
		if pointIn(hit.Rect, x, y) {
			app.graphDragging = false
			pReleaseCapture.Call()
			openGraphCompactEditor(hit.ID, 0)
			return true
		}
	}
	return false
}

func drawGraphCompactEditor(hdc uintptr, body RECT) {
	if !app.graphEditorOpen {
		return
	}
	if app.graphEditorSection != 0 {
		drawGraphFullEditor(hdc, body)
		return
	}
	node := ensureCurrentScenarioGraph().node(app.graphEditorNodeID)
	if node == nil {
		app.graphEditorOpen = false
		return
	}
	w := minInt(560, int(body.Right-body.Left)-52)
	h := 430
	x := int(body.Left+body.Right)/2 - w/2
	y := int(body.Top+body.Bottom)/2 - h/2
	app.graphEditorRect = RECT{int32(x), int32(y), int32(x + w), int32(y + h)}
	if ui2d.active {
		d2dFillRoundedOpacity(body, rgb(0, 0, 0), 18, .38)
	}
	roundFill(hdc, app.graphEditorRect, surfacePanelColor(), 15)
	if ui2d.active {
		d2dDrawRoundedOutline(app.graphEditorRect, 15, 1.4, blendColor(theme.border, theme.accent2, .50))
	}
	app.graphEditorCloseRect = RECT{int32(x + w - 42), int32(y + 10), int32(x + w - 10), int32(y + 42)}
	drawText(hdc, "Редактор блока · "+graphNodeKindName(node.Kind), x+18, y+12, w-72, 28, 17, 650, theme.text, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
	drawButton(hdc, app.graphEditorCloseRect, "×", false)
	drawText(hdc, graphNodeSummary(*node), x+18, y+48, w-36, 30, 11, 450, theme.muted, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
	for i := range app.graphEditorActionRects {
		app.graphEditorActionRects[i] = RECT{}
	}
	contentX, contentY := x+18, y+92
	contentW := w - 36
	switch node.Kind {
	case graphNodeTrigger:
		names := []string{"Таймер", "Дата и время", "Простой", "Процесс", "Расписание", "Сразу"}
		gap := 8
		bw := (contentW - gap*2) / 3
		for i, name := range names {
			row, col := i/3, i%3
			r := RECT{int32(contentX + col*(bw+gap)), int32(contentY + row*46), int32(contentX + col*(bw+gap) + bw), int32(contentY + row*46 + 38)}
			app.graphEditorActionRects[i] = r
			drawSelectableButton(hdc, r, name, node.Mode == i)
		}
		app.graphEditorActionRects[6] = RECT{int32(contentX), int32(contentY + 104), int32(contentX + contentW/2 - 4), int32(contentY + 144)}
		app.graphEditorActionRects[7] = RECT{int32(contentX + contentW/2 + 4), int32(contentY + 104), int32(contentX + contentW), int32(contentY + 144)}
		drawButton(hdc, app.graphEditorActionRects[6], "− 1 минута", false)
		drawButton(hdc, app.graphEditorActionRects[7], "+ 1 минута", false)
	case graphNodeFinish:
		names := []string{"Выключение", "Перезагрузка", "Сон", "Гибернация", "Завершить задачу"}
		for i, name := range names {
			r := RECT{int32(contentX), int32(contentY + i*40), int32(contentX + contentW), int32(contentY + i*40 + 34)}
			app.graphEditorActionRects[i] = r
			drawSelectableButton(hdc, r, name, node.Action == i)
		}
	case graphNodeWait:
		app.graphEditorActionRects[0] = RECT{int32(contentX), int32(contentY + 44), int32(contentX + contentW/2 - 4), int32(contentY + 88)}
		app.graphEditorActionRects[1] = RECT{int32(contentX + contentW/2 + 4), int32(contentY + 44), int32(contentX + contentW), int32(contentY + 88)}
		drawText(hdc, fmt.Sprintf("Ожидание: %d секунд", node.WaitSecs), contentX, contentY, contentW, 32, 15, 650, theme.text, DT_CENTER|DT_VCENTER|DT_SINGLELINE)
		drawButton(hdc, app.graphEditorActionRects[0], "− 10 секунд", false)
		drawButton(hdc, app.graphEditorActionRects[1], "+ 10 секунд", false)
	case graphNodeCondition, graphNodeAction:
		count := len(node.Conditions)
		if node.Kind == graphNodeAction {
			count = len(node.Steps)
		}
		if count == 0 {
			app.graphEditorItem = 0
		} else {
			app.graphEditorItem = clampInt(app.graphEditorItem, 0, count-1)
		}
		summary := "Функций пока нет"
		if count > 0 && node.Kind == graphNodeCondition {
			summary = conditionSummary(node.Conditions[app.graphEditorItem])
		} else if count > 0 {
			summary = stepSummary(node.Steps[app.graphEditorItem])
		}
		drawText(hdc, fmt.Sprintf("Функция %d из %d", minInt(app.graphEditorItem+1, count), count), contentX, contentY, contentW, 22, 11, 600, theme.muted, DT_CENTER|DT_VCENTER|DT_SINGLELINE)
		drawText(hdc, summary, contentX+12, contentY+28, contentW-24, 34, 13, 650, theme.text, DT_CENTER|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
		labels := []string{"‹ Тип", "Тип ›", "− Значение", "+ Значение", "+ Функция", "Удалить функцию", "← Предыдущая", "Следующая →"}
		gap := 8
		bw := (contentW - gap) / 2
		for i, label := range labels {
			row, col := i/2, i%2
			r := RECT{int32(contentX + col*(bw+gap)), int32(contentY + 74 + row*42), int32(contentX + col*(bw+gap) + bw), int32(contentY + 74 + row*42 + 34)}
			app.graphEditorActionRects[i] = r
			drawButton(hdc, r, label, i == 4)
		}
	}
	if app.graphEditorText != 0 {
		if graphCompactTextRelevant(node) {
			label := "Текст / параметр"
			if node.Kind == graphNodeTrigger {
				label = "Дата, процесс или время расписания"
			}
			drawText(hdc, label, contentX+8, y+h-84, contentW-16, 18, 10, 500, theme.muted, DT_LEFT|DT_VCENTER|DT_SINGLELINE)
			move(app.graphEditorText, contentX+8, y+h-58, contentW-16, 28)
			pShowWindow.Call(app.graphEditorText, SW_SHOW)
		} else {
			pShowWindow.Call(app.graphEditorText, SW_HIDE)
		}
	}
	drawText(hdc, "Изменения применяются сразу · двойной щелчок открывает этот редактор", x+18, y+h-28, w-36, 18, 9, 450, theme.muted, DT_CENTER|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
}

func graphFullEditorInputIDs(section int) []int {
	switch section {
	case 8:
		return []int{idCondThreshold, idCondHold, idCondText, idCondDelay}
	case 9:
		return []int{idStepValue, idStepText, idStepRetries, idStepDelay}
	case 15:
		return []int{idDelayHours, idDelayMinutes, idDelaySeconds, idExactDay, idExactMonth, idExactYear, idExactHour, idExactMinute, idIdleMinutes, idWatchProcess, idScheduleTime}
	}
	return nil
}

func graphLegacyEditorBody() (RECT, int) {
	var physical RECT
	pGetClientRect.Call(app.hwnd, uintptr(unsafe.Pointer(&physical)))
	logical := logicalClientRect040(physical)
	w, h := int(logical.Right), int(logical.Bottom)
	top := 110
	if app.graphEditorSection == 4 && app.processPickerMode != 1 {
		top = 184
	}
	return RECT{20, int32(top), int32(w - 20), int32(h - 20)}, w
}

func prepareGraphFullEditorLayout(body RECT) (RECT, int) {
	legacy, legacyW := graphLegacyEditorBody()
	panelW, panelH := int(legacy.Right-legacy.Left), int(legacy.Bottom-legacy.Top)
	x := int(body.Left+body.Right)/2 - panelW/2
	y := int(body.Top+body.Bottom)/2 - panelH/2
	if x < int(body.Left)+16 {
		x = int(body.Left) + 16
	}
	if y < int(body.Top)+16 {
		y = int(body.Top) + 16
	}
	app.graphEditorRect = RECT{int32(x), int32(y), int32(x + panelW), int32(y + panelH)}
	app.graphEditorLegacyBody = legacy
	app.graphEditorDX, app.graphEditorDY = int32(x)-legacy.Left, int32(y)-legacy.Top
	oldSection := app.section
	app.section = app.graphEditorSection
	layoutControls(app.hwnd)
	app.section = oldSection
	positionGraphFullEditorInputs()
	return legacy, legacyW
}

func positionGraphFullEditorInputs() {
	for _, id := range graphFullEditorInputIDs(app.graphEditorSection) {
		h := app.edits[id]
		if h == 0 {
			continue
		}
		visible, _, _ := pIsWindowVisible.Call(h)
		var wr RECT
		pGetWindowRect.Call(h, uintptr(unsafe.Pointer(&wr)))
		pt := POINT{X: wr.Left, Y: wr.Top}
		parent, _, _ := pGetParentGraphEditor.Call(h)
		if parent != 0 {
			pScreenToClient.Call(parent, uintptr(unsafe.Pointer(&pt)))
		}
		pSetParentGraphEditor.Call(h, app.graphWindow)
		dx, dy := scaledInt040(int(app.graphEditorDX)), scaledInt040(int(app.graphEditorDY))
		pMoveWindow.Call(h, uintptr(int(pt.X)+dx), uintptr(int(pt.Y)+dy), uintptr(wr.Right-wr.Left), uintptr(wr.Bottom-wr.Top), 1)
		if visible != 0 {
			pShowWindow.Call(h, SW_SHOW)
		} else {
			pShowWindow.Call(h, SW_HIDE)
		}
	}
}

func drawGraphFullEditor(hdc uintptr, body RECT) {
	legacy, legacyW := prepareGraphFullEditorLayout(body)
	if ui2d.active {
		d2dFillRoundedOpacity(body, rgb(0, 0, 0), 18, .38)
	}
	roundFill(hdc, app.graphEditorRect, surfacePanelColor(), 18)
	if ui2d.active {
		d2dDrawRoundedOutline(app.graphEditorRect, 18, 1.2, blendColor(theme.border, theme.accent2, .42))
		d2dSetTranslation(float32(app.graphEditorDX), float32(app.graphEditorDY))
	}
	switch app.graphEditorSection {
	case 4:
		drawProcessesPage(hdc, legacy, legacyW)
	case 8:
		drawConditionEditor(hdc, legacy, legacyW)
	case 9:
		drawStepEditor(hdc, legacy, legacyW)
	case 14:
		drawBlockActionEditor(hdc, legacy, legacyW)
	case 15:
		drawBlockWhenEditor(hdc, legacy, legacyW)
	}
	if app.confirmSystemMode != 0 {
		drawSystemProcessConfirmation(hdc, RECT{0, 0, int32(legacyW), legacy.Bottom + 20})
	}
	if ui2d.active {
		d2dSetTranslation(0, 0)
	}
}

func closeGraphFullEditor() {
	for _, id := range graphFullEditorInputIDs(app.graphEditorSection) {
		if h := app.edits[id]; h != 0 {
			pShowWindow.Call(h, SW_HIDE)
			pSetParentGraphEditor.Call(h, app.hwnd)
		}
	}
	app.graphEditorOpen = false
	app.graphEditorSection = 0
	app.editingCondition, app.editingStep = -1, -1
	layoutControls(app.hwnd)
	invalidateScenarioGraphWindows()
}

func handleGraphFullEditorClick(x, y int32) bool {
	if app.graphEditorSection == 0 {
		return false
	}
	if !pointIn(app.graphEditorRect, x, y) {
		return true
	}
	localX, localY := x-app.graphEditorDX, y-app.graphEditorDY
	graphSection := app.section
	app.section = app.graphEditorSection
	onClick(localX, localY)
	resultSection := app.section
	app.section = graphSection
	if resultSection == 7 || resultSection == 13 {
		closeGraphFullEditor()
	} else if resultSection == 4 || resultSection == 8 || resultSection == 9 || resultSection == 14 || resultSection == 15 {
		app.graphEditorSection = resultSection
	}
	invalidateScenarioGraphWindows()
	return true
}

func handleGraphCompactEditorClick(x, y int32) bool {
	if !app.graphEditorOpen {
		return false
	}
	if app.graphEditorSection != 0 {
		return handleGraphFullEditorClick(x, y)
	}
	if pointIn(app.graphEditorCloseRect, x, y) || !pointIn(app.graphEditorRect, x, y) {
		syncGraphCompactText()
		app.graphEditorOpen = false
		if app.graphEditorText != 0 {
			pShowWindow.Call(app.graphEditorText, SW_HIDE)
		}
		persistCurrentScenarioGraph()
		invalidateScenarioGraphWindows()
		return true
	}
	node := ensureCurrentScenarioGraph().node(app.graphEditorNodeID)
	if node == nil {
		app.graphEditorOpen = false
		return true
	}
	for i, r := range app.graphEditorActionRects {
		if !pointIn(r, x, y) {
			continue
		}
		syncGraphCompactText()
		switch node.Kind {
		case graphNodeTrigger:
			if i < 6 {
				node.Mode = i
			} else if i == 6 {
				node.DelayMins = max(0, node.DelayMins-1)
			} else if i == 7 {
				node.DelayMins++
			}
		case graphNodeFinish:
			if i < 5 {
				node.Action = i
			}
		case graphNodeWait:
			if i == 0 {
				node.WaitSecs = max(1, node.WaitSecs-10)
			} else if i == 1 {
				node.WaitSecs += 10
			}
		case graphNodeCondition:
			handleCompactConditionAction(node, i)
		case graphNodeAction:
			handleCompactStepAction(node, i)
		}
		persistCurrentScenarioGraph()
		loadGraphCompactText()
		return true
	}
	return true
}

func handleCompactConditionAction(node *ScenarioGraphNode, action int) {
	if action == 4 {
		node.Conditions = append(node.Conditions, AutomationCondition{ID: newAutomationID("cond"), Type: condCPU, Logic: logicAND, Compare: -1, Threshold: 10, HoldSeconds: 30, Enabled: true})
		app.graphEditorItem = len(node.Conditions) - 1
		return
	}
	if len(node.Conditions) == 0 {
		return
	}
	i := clampInt(app.graphEditorItem, 0, len(node.Conditions)-1)
	switch action {
	case 0:
		node.Conditions[i].Type = (node.Conditions[i].Type + condDrivePresent) % (condDrivePresent + 1)
	case 1:
		node.Conditions[i].Type = (node.Conditions[i].Type + 1) % (condDrivePresent + 1)
	case 2:
		node.Conditions[i].Threshold -= 5
	case 3:
		node.Conditions[i].Threshold += 5
	case 5:
		node.Conditions = append(node.Conditions[:i], node.Conditions[i+1:]...)
		app.graphEditorItem = max(0, i-1)
	case 6:
		app.graphEditorItem = max(0, i-1)
	case 7:
		app.graphEditorItem = minInt(len(node.Conditions)-1, i+1)
	}
}

func handleCompactStepAction(node *ScenarioGraphNode, action int) {
	if action == 4 {
		node.Steps = append(node.Steps, ActionStep{ID: newAutomationID("step"), Type: stepWait, Value: 10})
		app.graphEditorItem = len(node.Steps) - 1
		return
	}
	if len(node.Steps) == 0 {
		return
	}
	i := clampInt(app.graphEditorItem, 0, len(node.Steps)-1)
	switch action {
	case 0:
		node.Steps[i].Type = (node.Steps[i].Type + stepProcessPriority) % (stepProcessPriority + 1)
	case 1:
		node.Steps[i].Type = (node.Steps[i].Type + 1) % (stepProcessPriority + 1)
	case 2:
		node.Steps[i].Value = max(0, node.Steps[i].Value-5)
	case 3:
		node.Steps[i].Value += 5
	case 5:
		node.Steps = append(node.Steps[:i], node.Steps[i+1:]...)
		app.graphEditorItem = max(0, i-1)
	case 6:
		app.graphEditorItem = max(0, i-1)
	case 7:
		app.graphEditorItem = minInt(len(node.Steps)-1, i+1)
	}
}
