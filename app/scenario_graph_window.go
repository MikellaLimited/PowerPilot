//go:build windows

package main

import (
	"fmt"
	"strconv"
	"strings"
	"syscall"
	"unsafe"
)

var (
	graphWindowClassRegistered bool
	pSetForegroundWindowGraph  = user32.NewProc("SetForegroundWindow")
	pSetBkColorGraph           = gdi32.NewProc("SetBkColor")
	scenarioGraphDetachedInput bool
	graphEditBrush             uintptr
	graphEditBrushColor        uint32
)

const scenarioGraphAnimationTimerID = 3

func scenarioGraphEditBrush() uintptr {
	color := surfaceButtonColor()
	if graphEditBrush == 0 || graphEditBrushColor != color {
		if graphEditBrush != 0 {
			pDeleteObject.Call(graphEditBrush)
		}
		graphEditBrush = solid(color)
		graphEditBrushColor = color
	}
	return graphEditBrush
}

func advanceDetachedEditorScroll() bool {
	if !app.graphEditorOpen || app.graphEditorSection != 4 || app.draggingScrollKind != 0 {
		return false
	}
	step := .24
	if app.settings.AnimationMode == 1 {
		step = .38
	}
	if app.settings.AnimationMode == 2 {
		step = 1
	}
	old := app.processScrollPx
	app.processScrollPx += (app.processScrollTarget - app.processScrollPx) * step
	if abs(app.processScrollPx-app.processScrollTarget) < .08 {
		app.processScrollPx = app.processScrollTarget
	}
	if old == app.processScrollPx {
		return false
	}
	oldSection := app.section
	app.section = 4
	updateScrollGeometry()
	app.section = oldSection
	return true
}

func openScenarioGraphWindow() {
	if session := scenarioGraphSessionForCurrentTarget(); session != nil {
		pShowWindow.Call(session.HWND, SW_RESTORE)
		pSetForegroundWindowGraph.Call(session.HWND)
		return
	}
	hinst, _, _ := pGetModuleHandleW.Call(0)
	className := wstr("PowerPilotScenarioGraphWindow")
	if !graphWindowClassRegistered {
		cursor, _, _ := pLoadCursorW.Call(0, IDC_ARROW)
		icon := app.appIcon
		if icon == 0 {
			icon, _, _ = pLoadIconW.Call(0, IDI_APPLICATION)
		}
		wc := WNDCLASSEX{CbSize: uint32(unsafe.Sizeof(WNDCLASSEX{})), Style: 0x000B, LpfnWndProc: syscall.NewCallback(scenarioGraphWindowProc), HInstance: hinst, HIcon: icon, HCursor: cursor, LpszClassName: className, HIconSm: icon}
		if registered, _, _ := pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); registered == 0 {
			message("PowerPilot", "Не удалось создать класс отдельного окна редактора.", MB_OK|MB_ICONERROR)
			return
		}
		graphWindowClassRegistered = true
	}

	width, height := scenarioGraphWindowSize()
	x, y := 80, 60
	var mainRect RECT
	if app.hwnd != 0 {
		if ok, _, _ := pGetWindowRect.Call(app.hwnd, uintptr(unsafe.Pointer(&mainRect))); ok != 0 {
			x = int(mainRect.Left) + 34
			y = int(mainRect.Top) + 34
		}
	}
	style := uintptr(WS_POPUP | WS_CAPTION | WS_THICKFRAME | WS_SYSMENU | WS_MINIMIZEBOX | WS_MAXIMIZEBOX | WS_CLIPCHILDREN | WS_CLIPSIBLINGS)
	title := wstr("PowerPilot — Редактор сценария")
	hwnd, _, _ := pCreateWindowExW.Call(WS_EX_APPWINDOW, uintptr(unsafe.Pointer(className)), uintptr(unsafe.Pointer(title)), style, uintptr(x), uintptr(y), uintptr(width), uintptr(height), 0, 0, hinst, 0)
	if hwnd == 0 {
		message("PowerPilot", "Не удалось открыть отдельное окно редактора.", MB_OK|MB_ICONERROR)
		return
	}
	targetID, saved := scenarioGraphTargetID()
	graph := cloneScenarioGraph(*ensureCurrentScenarioGraph())
	taskName := strings.TrimSpace(getText(app.edits[idTaskName]))
	if saved && strings.TrimSpace(app.savedEditDraft.Name) != "" {
		taskName = strings.TrimSpace(app.savedEditDraft.Name)
	}
	if taskName == "" {
		taskName = "Новая задача"
	}
	session := &scenarioGraphSession{HWND: hwnd, Graph: graph, TargetID: targetID, SavedTask: saved, TaskName: taskName, Current: cloneScenarioGraph(graph), Fingerprint: scenarioGraphFingerprint(graph)}
	copyGraphSessionUI(&session.UI, &app)
	session.UI.graphWindow = hwnd
	session.UI.graphSelectedNodes = map[string]bool{}
	session.UI.graphSelectedNodeID, session.UI.graphSelectedEdgeID = "", ""
	session.UI.graphEditorOpen, session.UI.graphContextOpen = false, false
	scenarioGraphSessions[hwnd] = session
	withScenarioGraphSession(hwnd, func() uintptr {
		ensureScenarioGraphNameEdit(taskName)
		return 0
	})
	app.graphWindow = hwnd
	pSetTimer.Call(hwnd, scenarioGraphAnimationTimerID, 10, 0)
	app.graphTitleHover = -1
	applyRoundedWindowCorners(hwnd)
	if app.appIcon != 0 {
		pSendMessageW.Call(hwnd, WM_SETICON, 1, app.appIcon)
		pSendMessageW.Call(hwnd, WM_SETICON, 0, app.appIcon)
	}
	pShowWindow.Call(hwnd, SW_SHOW)
	pUpdateWindow.Call(hwnd)
	pSetForegroundWindowGraph.Call(hwnd)
	layoutControls(app.hwnd)
	invalidateScenarioGraphWindows()
}

func ensureScenarioGraphNameEdit(name string) {
	if app.graphWindow == 0 || app.graphNameEdit != 0 {
		return
	}
	hwnd, _, _ := pCreateWindowExW.Call(0, uintptr(unsafe.Pointer(wstr("EDIT"))), uintptr(unsafe.Pointer(wstr(name))), WS_CHILD|WS_VISIBLE|WS_TABSTOP|ES_CENTER, 0, 0, 260, 30, app.graphWindow, 9902, 0, 0)
	app.graphNameEdit = hwnd
	if hwnd != 0 {
		pSendMessageW.Call(hwnd, WM_SETFONT, app.font, 1)
		pSendMessageW.Call(hwnd, EM_SETCUEBANNER, 1, uintptr(unsafe.Pointer(wstr("Название задачи"))))
	}
}

func scenarioGraphWindowSize() (int, int) {
	if app.settings.GraphWindowSizeLocked {
		return graphWindowWidth(), graphWindowHeight()
	}
	switch app.settings.GraphWindowSize {
	case 1:
		return 1440, 900
	case 2:
		return 1600, 960
	default:
		return 1280, 820
	}
}

func layoutScenarioGraphLauncher(body RECT) {
	for i := range app.graphPaletteRects {
		app.graphPaletteRects[i] = RECT{}
	}
	app.graphCanvasRect = RECT{}
	cardW := minInt(650, int(body.Right-body.Left)-24)
	x := int(body.Left+body.Right)/2 - cardW/2
	y := int(body.Top) + 24
	cardH := minInt(320, max(250, int(body.Bottom-body.Top)-110))
	buttonY := y + cardH - 116
	app.graphDetachRect = RECT{int32(x + 28), int32(buttonY), int32(x + cardW - 28), int32(buttonY + 42)}
	app.previewRect = RECT{int32(x + 28), int32(buttonY + 52), int32(x + cardW - 28), int32(buttonY + 90)}
	if app.scenarioSavedDraft && app.section == 13 {
		app.savedScenarioNameRect = RECT{int32(x + 28), int32(y + 84), int32(x + cardW - 28), int32(y + 124)}
		move(app.edits[idTaskName], x+40, y+93, cardW-80, 22)
		pShowWindow.Call(app.edits[idTaskName], SW_SHOW)
		app.savedScenarioSaveRect = RECT{int32(x + 28), int32(y + cardH + 10), int32(x + cardW/2 - 6), int32(y + cardH + 48)}
		app.savedScenarioCancelRect = RECT{int32(x + cardW/2 + 6), int32(y + cardH + 10), int32(x + cardW - 28), int32(y + cardH + 48)}
		app.savedScenarioCheckRect = RECT{}
	} else {
		app.savedScenarioNameRect = RECT{}
		app.savedScenarioSaveRect, app.savedScenarioCancelRect, app.savedScenarioCheckRect = RECT{}, RECT{}, RECT{}
	}
}

func drawScenarioGraphLauncher(hdc uintptr, body RECT) {
	g := ensureCurrentScenarioGraph()
	cardW := minInt(650, int(body.Right-body.Left)-24)
	cardH := minInt(320, max(250, int(body.Bottom-body.Top)-110))
	x := int(body.Left+body.Right)/2 - cardW/2
	y := int(body.Top) + 24
	card := RECT{int32(x), int32(y), int32(x + cardW), int32(y + cardH)}
	roundFill(hdc, card, surfacePanelColor(), 18)
	if ui2d.active {
		d2dDrawRoundedOutline(card, 18, 1, blendColor(theme.border, theme.accent2, .32))
	}
	drawText(hdc, "Продвинутая задача", x+28, y+22, cardW-56, 30, 21, 650, theme.text, DT_CENTER|DT_VCENTER|DT_SINGLELINE)
	issues := validateScenarioGraph(*g)
	summary := "Схема готова"
	if len(issues) > 0 {
		summary = graphValidationText(issues)
	}
	drawText(hdc, fmt.Sprintf("%d блоков · %d соединений", len(g.Nodes), len(g.Edges)), x+28, y+54, cardW-56, 22, 11, 550, theme.muted, DT_CENTER|DT_VCENTER|DT_SINGLELINE)
	if app.scenarioSavedDraft && app.section == 13 {
		drawText(hdc, "Название задачи", int(app.savedScenarioNameRect.Left), int(app.savedScenarioNameRect.Top)-19, int(app.savedScenarioNameRect.Right-app.savedScenarioNameRect.Left), 17, 10, 500, theme.muted, DT_LEFT|DT_VCENTER|DT_SINGLELINE)
		roundFill(hdc, app.savedScenarioNameRect, surfaceButtonColor(), 10)
	} else {
		drawText(hdc, "Название можно задать при сохранении задачи", x+28, y+87, cardW-56, 24, 11, 500, theme.muted, DT_CENTER|DT_VCENTER|DT_SINGLELINE)
	}
	openLabel := "Открыть редактор сценария"
	if scenarioGraphSessionForCurrentTarget() != nil {
		openLabel = "Закрыть и сохранить"
	}
	drawButton(hdc, app.graphDetachRect, openLabel, true)
	drawButton(hdc, app.previewRect, "Просмотр и проверка", false)
	drawText(hdc, summary, x+28, int(app.previewRect.Bottom)+4, cardW-56, 22, 10, 500, theme.muted, DT_CENTER|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
	if app.scenarioSavedDraft && app.section == 13 {
		drawButton(hdc, app.savedScenarioSaveRect, "Сохранить изменения", true)
		drawButton(hdc, app.savedScenarioCancelRect, "Отмена", false)
	}
}

func graphWindowWidth() int {
	if app.settings.GraphWindowWidth > 0 {
		return clampInt(app.settings.GraphWindowWidth, 900, 3840)
	}
	w, _ := scenarioGraphPresetSize(app.settings.GraphWindowSize)
	return w
}

func graphWindowHeight() int {
	if app.settings.GraphWindowHeight > 0 {
		return clampInt(app.settings.GraphWindowHeight, 760, 2160)
	}
	_, h := scenarioGraphPresetSize(app.settings.GraphWindowSize)
	return h
}

func scenarioGraphPresetSize(index int) (int, int) {
	switch index {
	case 1:
		return 1440, 900
	case 2:
		return 1600, 960
	default:
		return 1280, 820
	}
}

func applyGraphWindowSizeFields() {
	w := clampInt(parseInt(getText(app.edits[idGraphWidth]), graphWindowWidth()), 900, 3840)
	h := clampInt(parseInt(getText(app.edits[idGraphHeight]), graphWindowHeight()), 760, 2160)
	app.settings.GraphWindowWidth, app.settings.GraphWindowHeight = w, h
	app.settings.GraphWindowSize = -1
	setEditTextIfDifferent(idGraphWidth, strconv.Itoa(w))
	setEditTextIfDifferent(idGraphHeight, strconv.Itoa(h))
	saveSettings()
}

func resizeScenarioGraphWindow(width, height int) {
	if app.graphWindow == 0 {
		return
	}
	if zoomed, _, _ := pIsZoomed.Call(app.graphWindow); zoomed != 0 {
		pShowWindow.Call(app.graphWindow, SW_RESTORE)
	}
	var wr RECT
	if ok, _, _ := pGetWindowRect.Call(app.graphWindow, uintptr(unsafe.Pointer(&wr))); ok != 0 {
		pMoveWindow.Call(app.graphWindow, uintptr(wr.Left), uintptr(wr.Top), uintptr(width), uintptr(height), 1)
	}
}

func focusMainFromScenarioGraph() {
	if app.graphWindow == 0 || app.section == 7 || app.section == 13 {
		return
	}
	pShowWindow.Call(app.hwnd, SW_RESTORE)
	pSetForegroundWindowGraph.Call(app.hwnd)
}

func invalidateScenarioGraphWindows() {
	if app.hwnd != 0 {
		pInvalidateRect.Call(app.hwnd, 0, 0)
	}
	for hwnd := range scenarioGraphSessions {
		pInvalidateRect.Call(hwnd, 0, 0)
	}
}

func layoutScenarioGraphMainPlaceholder(body RECT) {
	app.previewRect = RECT{}
	app.graphDetachRect = RECT{body.Right - 210, body.Top + 16, body.Right - 16, body.Top + 54}
	app.savedScenarioNameRect, app.savedScenarioSaveRect, app.savedScenarioCancelRect, app.savedScenarioCheckRect = RECT{}, RECT{}, RECT{}, RECT{}
	if app.section != 13 || !app.scenarioSavedDraft {
		return
	}
	left, right := int(body.Left)+18, int(body.Right)-18
	footerY := int(body.Bottom) - 54
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
}

func drawScenarioGraphMainPlaceholder(hdc uintptr, body RECT) {
	drawText(hdc, "Редактор сценария открыт отдельно", int(body.Left)+32, int(body.Top)+72, int(body.Right-body.Left)-64, 34, 22, 650, theme.text, DT_CENTER|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
	drawText(hdc, "Можно растянуть его на весь экран или перенести на другой монитор.", int(body.Left)+48, int(body.Top)+112, int(body.Right-body.Left)-96, 48, 12, 450, theme.muted, DT_CENTER|DT_VCENTER|DT_WORDBREAK)
	drawButton(hdc, app.graphDetachRect, "Показать окно", true)
	if app.section == 13 && app.scenarioSavedDraft {
		drawText(hdc, "Название", int(app.savedScenarioNameRect.Left), int(app.savedScenarioNameRect.Top)-18, int(app.savedScenarioNameRect.Right-app.savedScenarioNameRect.Left), 16, 9, 500, theme.muted, DT_LEFT|DT_VCENTER|DT_SINGLELINE)
		roundFill(hdc, app.savedScenarioNameRect, surfaceButtonColor(), 9)
		drawButton(hdc, app.savedScenarioSaveRect, "Сохранить", true)
		drawButton(hdc, app.savedScenarioCancelRect, "Отмена", false)
		drawButton(hdc, app.savedScenarioCheckRect, "Проверка", false)
	}
}

func scenarioGraphWindowProc(hwnd uintptr, msg uint32, wParam, lParam uintptr) uintptr {
	session := scenarioGraphSessions[hwnd]
	if session == nil || activeScenarioGraphSession == session {
		return scenarioGraphWindowProcActive(hwnd, msg, wParam, lParam)
	}
	result := withScenarioGraphSession(hwnd, func() uintptr {
		return scenarioGraphWindowProcActive(hwnd, msg, wParam, lParam)
	})
	if session.Closed {
		if !session.Discard {
			saveScenarioGraphSession(session)
		}
		delete(scenarioGraphSessions, hwnd)
		app.graphWindow = 0
		for other := range scenarioGraphSessions {
			app.graphWindow = other
			break
		}
		if app.hwnd != 0 {
			layoutControls(app.hwnd)
			invalidate(app.hwnd)
		}
	}
	return result
}

func scenarioGraphWindowProcActive(hwnd uintptr, msg uint32, wParam, lParam uintptr) uintptr {
	switch msg {
	case WM_GETMINMAXINFO:
		mmi := (*MINMAXINFO)(unsafe.Pointer(lParam))
		mmi.PtMinTrackSize = POINT{900, 760}
		if monitor, _, _ := pMonitorFromWindow.Call(hwnd, MONITOR_DEFAULTTONEAREST); monitor != 0 {
			info := MONITORINFO{CbSize: uint32(unsafe.Sizeof(MONITORINFO{}))}
			if ok, _, _ := pGetMonitorInfoW.Call(monitor, uintptr(unsafe.Pointer(&info))); ok != 0 {
				mmi.PtMaxPosition = POINT{info.RcWork.Left - info.RcMonitor.Left, info.RcWork.Top - info.RcMonitor.Top}
				mmi.PtMaxSize = POINT{info.RcWork.Right - info.RcWork.Left, info.RcWork.Bottom - info.RcWork.Top}
			}
		}
		return 0
	case WM_SIZE:
		applyRoundedWindowCorners(hwnd)
		pInvalidateRect.Call(hwnd, 0, 0)
		return 0
	case WM_NCCALCSIZE:
		return 0
	case WM_NCACTIVATE:
		return 1
	case WM_NCHITTEST:
		return hitTestScenarioGraphWindow(hwnd, lParam)
	case WM_PAINT:
		paintScenarioGraphWindow(hwnd)
		return 0
	case WM_TIMER:
		if wParam == scenarioGraphAnimationTimerID {
			if animateConditionCatalog() || advanceDetachedEditorScroll() {
				pInvalidateRect.Call(hwnd, 0, 0)
			}
		}
		return 0
	case WM_ERASEBKGND:
		return 1
	case WM_CTLCOLOREDIT:
		pSetBkMode.Call(wParam, 2)
		pSetBkColorGraph.Call(wParam, uintptr(surfaceButtonColor()))
		pSetTextColor.Call(wParam, uintptr(theme.text))
		return scenarioGraphEditBrush()
	case WM_COMMAND:
		if lParam == app.graphNameEdit || lParam == app.graphEditorText || (app.graphEditorOpen && app.graphEditorSection != 0) {
			// Detached EDIT controls keep their text locally until Save/close. Native
			// focus and EN_CHANGE notifications must not touch the main window. In
			// particular, do not repaint the entire Direct2D surface for every typed
			// character: the EDIT already paints its own changed text and a parent
			// repaint makes the control visibly flash.
			return 0
		} else {
			onCommand(loword(wParam), hiword(wParam), lParam)
		}
		pInvalidateRect.Call(hwnd, 0, 0)
		return 0
	case WM_MOUSEMOVE:
		x, y := clientPointToLogical040(int32(int16(loword(lParam))), int32(int16(hiword(lParam))))
		app.mouseX, app.mouseY = x, y
		app.graphTitleHover = -1
		for i, r := range []RECT{app.graphTitleMinRect, app.graphTitleMaxRect, app.graphTitleCloseRect} {
			if pointIn(r, x, y) {
				app.graphTitleHover = i
			}
		}
		handleScenarioGraphMouseMove(x, y)
		pInvalidateRect.Call(hwnd, 0, 0)
		return 0
	case WM_LBUTTONDOWN:
		x, y := clientPointToLogical040(int32(int16(loword(lParam))), int32(int16(hiword(lParam))))
		app.mouseX, app.mouseY = x, y
		if pointIn(app.graphTitleCloseRect, x, y) {
			pSendMessageW.Call(hwnd, WM_CLOSE, 0, 0)
			return 0
		}
		if pointIn(app.graphTitleMinRect, x, y) {
			pShowWindow.Call(hwnd, SW_MINIMIZE)
			return 0
		}
		if pointIn(app.graphTitleMaxRect, x, y) {
			if zoomed, _, _ := pIsZoomed.Call(hwnd); zoomed != 0 {
				pShowWindow.Call(hwnd, SW_RESTORE)
			} else {
				pShowWindow.Call(hwnd, SW_MAXIMIZE)
			}
			return 0
		}
		scenarioGraphDetachedInput = true
		handleScenarioGraphClick(x, y)
		scenarioGraphDetachedInput = false
		pInvalidateRect.Call(hwnd, 0, 0)
		return 0
	case WM_LBUTTONDBLCLK:
		x, y := clientPointToLogical040(int32(int16(loword(lParam))), int32(int16(hiword(lParam))))
		app.mouseX, app.mouseY = x, y
		scenarioGraphDetachedInput = true
		handleGraphDoubleClick(x, y)
		scenarioGraphDetachedInput = false
		pInvalidateRect.Call(hwnd, 0, 0)
		return 0
	case WM_LBUTTONUP:
		finishScenarioGraphPointer()
		pInvalidateRect.Call(hwnd, 0, 0)
		return 0
	case WM_RBUTTONDOWN:
		x, y := clientPointToLogical040(int32(int16(loword(lParam))), int32(int16(hiword(lParam))))
		app.mouseX, app.mouseY = x, y
		beginGraphRightButton(x, y)
		return 0
	case WM_RBUTTONUP:
		x, y := clientPointToLogical040(int32(int16(loword(lParam))), int32(int16(hiword(lParam))))
		app.mouseX, app.mouseY = x, y
		finishGraphRightButton(x, y)
		return 0
	case WM_MBUTTONDOWN:
		x, y := clientPointToLogical040(int32(int16(loword(lParam))), int32(int16(hiword(lParam))))
		app.mouseX, app.mouseY = x, y
		if pointIn(app.graphCanvasRect, x, y) {
			beginGraphMiddleButton(x, y)
		}
		return 0
	case WM_MBUTTONUP:
		finishGraphMiddleButton()
		return 0
	case WM_MOUSEWHEEL:
		delta := int16((wParam >> 16) & 0xFFFF)
		if app.graphEditorOpen && app.graphEditorSection != 0 {
			// While a block editor is open, the wheel belongs to its active page
			// (not to canvas zoom). This also covers the process picker and its list.
			if app.graphEditorSection == 4 {
				step := 60.0 * float64(delta) / 120.0
				app.processScrollTarget = clampFloat(app.processScrollTarget-step, 0, scrollMaxPx(2))
			}
			pInvalidateRect.Call(hwnd, 0, 0)
		} else {
			zoomScenarioGraph(delta)
		}
		return 0
	case WM_KEYDOWN:
		scenarioGraphDetachedInput = true
		handled := handleKeyDown040(wParam)
		scenarioGraphDetachedInput = false
		if handled {
			invalidateScenarioGraphWindows()
			return 0
		}
	case WM_CLOSE:
		if session := currentScenarioGraphSession(); session != nil && !app.exiting && !session.CloseApproved {
			app.graphCloseConfirm = true
			if app.graphNameEdit != 0 {
				pShowWindow.Call(app.graphNameEdit, SW_HIDE)
			}
			for _, edit := range graphEditorEdits {
				if edit != 0 {
					pShowWindow.Call(edit, SW_HIDE)
				}
			}
			if app.graphEditorText != 0 {
				pShowWindow.Call(app.graphEditorText, SW_HIDE)
			}
			pInvalidateRect.Call(hwnd, 0, 0)
			return 0
		}
		if session := currentScenarioGraphSession(); session != nil && !session.Discard {
			syncScenarioGraphSessionName(session)
		}
		if app.graphEditorOpen {
			if app.graphEditorSection != 0 {
				commitGraphFullEditorDraft()
				closeGraphFullEditor()
			} else {
				syncGraphCompactText()
				persistCurrentScenarioGraph()
			}
		}
		pDestroyWindow.Call(hwnd)
		return 0
	case WM_DESTROY:
		pKillTimer.Call(hwnd, scenarioGraphAnimationTimerID)
		if app.graphEditorText != 0 {
			pDestroyWindow.Call(app.graphEditorText)
		}
		if app.graphNameEdit != 0 {
			pDestroyWindow.Call(app.graphNameEdit)
		}
		for _, edit := range graphEditorEdits {
			if edit != 0 {
				pDestroyWindow.Call(edit)
			}
		}
		app.graphEditorText = 0
		app.graphNameEdit = 0
		graphEditorEdits = nil
		app.graphEditorOpen = false
		app.graphDragging = false
		pReleaseCapture.Call()
		if session := currentScenarioGraphSession(); session != nil {
			session.Closed = true
		}
		return 0
	}
	r, _, _ := pDefWindowProcW.Call(hwnd, uintptr(msg), wParam, lParam)
	return r
}

func paintScenarioGraphWindow(hwnd uintptr) {
	var ps PAINTSTRUCT
	hdc, _, _ := pBeginPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
	defer pEndPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
	var physical RECT
	pGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&physical)))
	if physical.Right <= 0 || physical.Bottom <= 0 {
		return
	}
	logical := logicalClientRect040(physical)
	app.graphTitleBarRect = RECT{0, 0, logical.Right, 46}
	btnW := int32(46)
	app.graphTitleCloseRect = RECT{logical.Right - btnW, 0, logical.Right, 46}
	app.graphTitleMaxRect = RECT{logical.Right - btnW*2, 0, logical.Right - btnW, 46}
	app.graphTitleMinRect = RECT{logical.Right - btnW*3, 0, logical.Right - btnW*2, 46}
	body := RECT{14, 58, logical.Right - 14, logical.Bottom - 14}
	layoutScenarioGraphEditor(body, true)
	if d2dBegin(hdc, physical) {
		d2dSetBaseScale040(float32(uiScaleFactor040()))
		d2dClear(theme.bg)
		drawScenarioGraphTitleBar(hdc, logical, hwnd)
		roundFill(hdc, body, surfacePanelColor(), 18)
		drawScenarioGraphEditor(hdc, body, int(logical.Right), true)
		d2dEnd()
		return
	}
	d2dSetBaseScale040(1)
	fill(hdc, physical, theme.bg)
	drawScenarioGraphTitleBar(hdc, logical, hwnd)
	roundFill(hdc, body, surfacePanelColor(), 18)
	drawScenarioGraphEditor(hdc, body, int(logical.Right), true)
}

func drawScenarioGraphTitleBar(hdc uintptr, rc RECT, hwnd uintptr) {
	bar := blendColor(theme.bg, surfacePanelColor(), .74)
	fill(hdc, app.graphTitleBarRect, bar)
	d2dDrawAppIcon(RECT{14, 10, 40, 36})
	drawText(hdc, "PowerPilot — Редактор сценария", 48, 0, int(rc.Right)-210, 46, 14, 650, theme.text, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
	buttons := []RECT{app.graphTitleMinRect, app.graphTitleMaxRect, app.graphTitleCloseRect}
	for i, r := range buttons {
		color := bar
		if app.graphTitleHover == i {
			color = blendColor(bar, surfaceButtonColor(), .82)
		}
		if i == 2 && app.graphTitleHover == i {
			color = theme.danger
		}
		fill(hdc, r, color)
		drawScenarioGraphCaptionGlyph(hdc, i, r, hwnd)
	}
}

func drawScenarioGraphCaptionGlyph(hdc uintptr, kind int, r RECT, hwnd uintptr) {
	cx, cy := float32(r.Left+r.Right)/2, float32(r.Top+r.Bottom)/2
	if ui2d.active {
		switch kind {
		case 0:
			d2dDrawLine(cx-6, cy+3, cx+6, cy+3, 1.35, theme.text)
		case 1:
			if zoomed, _, _ := pIsZoomed.Call(hwnd); zoomed != 0 {
				d2dDrawRectOutline(RECT{int32(cx - 4), int32(cy - 6), int32(cx + 6), int32(cy + 4)}, 1.2, theme.text)
				d2dDrawRectOutline(RECT{int32(cx - 6), int32(cy - 4), int32(cx + 4), int32(cy + 6)}, 1.2, theme.text)
			} else {
				d2dDrawRectOutline(RECT{int32(cx - 5), int32(cy - 5), int32(cx + 5), int32(cy + 5)}, 1.3, theme.text)
			}
		case 2:
			d2dDrawLine(cx-5, cy-5, cx+5, cy+5, 1.35, theme.text)
			d2dDrawLine(cx+5, cy-5, cx-5, cy+5, 1.35, theme.text)
		}
		return
	}
	glyphs := []string{"—", "□", "×"}
	drawText(hdc, glyphs[kind], int(r.Left), int(r.Top), int(r.Right-r.Left), int(r.Bottom-r.Top), 14, 500, theme.text, DT_CENTER|DT_VCENTER|DT_SINGLELINE)
}

func hitTestScenarioGraphWindow(hwnd uintptr, lParam uintptr) uintptr {
	pt := POINT{X: int32(int16(loword(lParam))), Y: int32(int16(hiword(lParam)))}
	pScreenToClient.Call(hwnd, uintptr(unsafe.Pointer(&pt)))
	var rc RECT
	pGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&rc)))
	if zoomed, _, _ := pIsZoomed.Call(hwnd); zoomed == 0 && !app.settings.GraphWindowSizeLocked {
		const edge int32 = 7
		left, right := pt.X < edge, pt.X >= rc.Right-edge
		top, bottom := pt.Y < edge, pt.Y >= rc.Bottom-edge
		switch {
		case top && left:
			return HTTOPLEFT
		case top && right:
			return HTTOPRIGHT
		case bottom && left:
			return HTBOTTOMLEFT
		case bottom && right:
			return HTBOTTOMRIGHT
		case left:
			return HTLEFT
		case right:
			return HTRIGHT
		case top:
			return HTTOP
		case bottom:
			return HTBOTTOM
		}
	}
	lx, ly := clientPointToLogical040(pt.X, pt.Y)
	for _, r := range []RECT{app.graphTitleMinRect, app.graphTitleMaxRect, app.graphTitleCloseRect} {
		if pointIn(r, lx, ly) {
			return HTCLIENT
		}
	}
	if pointIn(app.graphTitleBarRect, lx, ly) {
		return HTCAPTION
	}
	return HTCLIENT
}
