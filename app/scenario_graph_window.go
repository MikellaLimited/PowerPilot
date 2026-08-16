//go:build windows

package main

import (
	"syscall"
	"unsafe"
)

var (
	graphWindowClassRegistered bool
	pSetForegroundWindowGraph  = user32.NewProc("SetForegroundWindow")
	scenarioGraphDetachedInput bool
)

func openScenarioGraphWindow() {
	if app.graphWindow != 0 {
		pShowWindow.Call(app.graphWindow, SW_RESTORE)
		pSetForegroundWindowGraph.Call(app.graphWindow)
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
		wc := WNDCLASSEX{CbSize: uint32(unsafe.Sizeof(WNDCLASSEX{})), Style: 0x0003, LpfnWndProc: syscall.NewCallback(scenarioGraphWindowProc), HInstance: hinst, HIcon: icon, HCursor: cursor, LpszClassName: className, HIconSm: icon}
		if registered, _, _ := pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); registered == 0 {
			message("PowerPilot", "Не удалось создать класс отдельного окна редактора.", MB_OK|MB_ICONERROR)
			return
		}
		graphWindowClassRegistered = true
	}

	x, y, width, height := 80, 60, 1280, 820
	var mainRect RECT
	if app.hwnd != 0 {
		if ok, _, _ := pGetWindowRect.Call(app.hwnd, uintptr(unsafe.Pointer(&mainRect))); ok != 0 {
			x = int(mainRect.Left) + 34
			y = int(mainRect.Top) + 34
		}
	}
	style := uintptr(WS_OVERLAPPEDWINDOW | WS_CLIPCHILDREN | WS_CLIPSIBLINGS)
	title := wstr("PowerPilot — Редактор сценария")
	hwnd, _, _ := pCreateWindowExW.Call(WS_EX_APPWINDOW, uintptr(unsafe.Pointer(className)), uintptr(unsafe.Pointer(title)), style, uintptr(x), uintptr(y), uintptr(width), uintptr(height), 0, 0, hinst, 0)
	if hwnd == 0 {
		message("PowerPilot", "Не удалось открыть отдельное окно редактора.", MB_OK|MB_ICONERROR)
		return
	}
	app.graphWindow = hwnd
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
	if app.graphWindow != 0 {
		pInvalidateRect.Call(app.graphWindow, 0, 0)
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
	switch msg {
	case WM_GETMINMAXINFO:
		mmi := (*MINMAXINFO)(unsafe.Pointer(lParam))
		mmi.PtMinTrackSize = POINT{900, 580}
		return 0
	case WM_SIZE:
		pInvalidateRect.Call(hwnd, 0, 0)
		return 0
	case WM_PAINT:
		paintScenarioGraphWindow(hwnd)
		return 0
	case WM_ERASEBKGND:
		return 1
	case WM_MOUSEMOVE:
		x, y := clientPointToLogical040(int32(int16(loword(lParam))), int32(int16(hiword(lParam))))
		app.mouseX, app.mouseY = x, y
		handleScenarioGraphMouseMove(x, y)
		pInvalidateRect.Call(hwnd, 0, 0)
		return 0
	case WM_LBUTTONDOWN:
		x, y := clientPointToLogical040(int32(int16(loword(lParam))), int32(int16(hiword(lParam))))
		app.mouseX, app.mouseY = x, y
		scenarioGraphDetachedInput = true
		handleScenarioGraphClick(x, y)
		scenarioGraphDetachedInput = false
		pInvalidateRect.Call(hwnd, 0, 0)
		return 0
	case WM_LBUTTONUP:
		finishScenarioGraphPointer()
		pInvalidateRect.Call(hwnd, 0, 0)
		return 0
	case WM_MOUSEWHEEL:
		zoomScenarioGraph(int16((wParam >> 16) & 0xFFFF))
		return 0
	case WM_KEYDOWN:
		if handleKeyDown040(wParam) {
			invalidateScenarioGraphWindows()
			return 0
		}
	case WM_CLOSE:
		pDestroyWindow.Call(hwnd)
		return 0
	case WM_DESTROY:
		if app.graphWindow == hwnd {
			app.graphWindow = 0
			app.graphDragging, app.graphPanning = false, false
			app.graphDraggingNodeID = ""
			pReleaseCapture.Call()
			if app.hwnd != 0 {
				layoutControls(app.hwnd)
				invalidate(app.hwnd)
			}
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
	body := RECT{14, 14, logical.Right - 14, logical.Bottom - 14}
	layoutScenarioGraphEditor(body, true)
	if d2dBegin(hdc, physical) {
		d2dSetBaseScale040(float32(uiScaleFactor040()))
		d2dClear(theme.bg)
		roundFill(hdc, body, surfacePanelColor(), 18)
		drawScenarioGraphEditor(hdc, body, int(logical.Right), true)
		d2dEnd()
		return
	}
	d2dSetBaseScale040(1)
	fill(hdc, physical, theme.bg)
	roundFill(hdc, body, surfacePanelColor(), 18)
	drawScenarioGraphEditor(hdc, body, int(logical.Right), true)
}
