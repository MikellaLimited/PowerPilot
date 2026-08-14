//go:build windows

package main

import (
	"embed"
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

//go:embed payload/PowerPilot.exe payload/Uninstall.exe payload/PowerPilot.ico
var payload embed.FS

//go:embed assets/PowerPilot.ico
var iconData []byte

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	gdi32    = syscall.NewLazyDLL("gdi32.dll")

	pRegisterClassExW   = user32.NewProc("RegisterClassExW")
	pCreateWindowExW    = user32.NewProc("CreateWindowExW")
	pDefWindowProcW     = user32.NewProc("DefWindowProcW")
	pShowWindow         = user32.NewProc("ShowWindow")
	pUpdateWindow       = user32.NewProc("UpdateWindow")
	pGetMessageW        = user32.NewProc("GetMessageW")
	pTranslateMessage   = user32.NewProc("TranslateMessage")
	pDispatchMessageW   = user32.NewProc("DispatchMessageW")
	pPostQuitMessage    = user32.NewProc("PostQuitMessage")
	pPostMessageW       = user32.NewProc("PostMessageW")
	pLoadCursorW        = user32.NewProc("LoadCursorW")
	pLoadImageW         = user32.NewProc("LoadImageW")
	pDrawIconEx         = user32.NewProc("DrawIconEx")
	pSendMessageW       = user32.NewProc("SendMessageW")
	pMoveWindow         = user32.NewProc("MoveWindow")
	pSetWindowTextW     = user32.NewProc("SetWindowTextW")
	pGetWindowTextW     = user32.NewProc("GetWindowTextW")
	pGetWindowTextLen   = user32.NewProc("GetWindowTextLengthW")
	pGetClientRect      = user32.NewProc("GetClientRect")
	pDestroyWindow      = user32.NewProc("DestroyWindow")
	pBeginPaint         = user32.NewProc("BeginPaint")
	pEndPaint           = user32.NewProc("EndPaint")
	pInvalidateRect     = user32.NewProc("InvalidateRect")
	pSetTimer           = user32.NewProc("SetTimer")
	pKillTimer          = user32.NewProc("KillTimer")
	pSetProcessDPIAware = user32.NewProc("SetProcessDPIAware")

	pCreateSolidBrush       = gdi32.NewProc("CreateSolidBrush")
	pDeleteObject           = gdi32.NewProc("DeleteObject")
	pSetBkMode              = gdi32.NewProc("SetBkMode")
	pSetTextColor           = gdi32.NewProc("SetTextColor")
	pCreateFontW            = gdi32.NewProc("CreateFontW")
	pSelectObject           = gdi32.NewProc("SelectObject")
	pRoundRect              = gdi32.NewProc("RoundRect")
	pCreatePen              = gdi32.NewProc("CreatePen")
	pCreateCompatibleDC     = gdi32.NewProc("CreateCompatibleDC")
	pCreateCompatibleBitmap = gdi32.NewProc("CreateCompatibleBitmap")
	pDeleteDC               = gdi32.NewProc("DeleteDC")
	pBitBlt                 = gdi32.NewProc("BitBlt")
	pFillRect               = user32.NewProc("FillRect")
	pDrawTextW              = user32.NewProc("DrawTextW")

	pGetModuleHandleW = kernel32.NewProc("GetModuleHandleW")
)

const (
	WS_OVERLAPPEDWINDOW = 0x00CF0000
	WS_CAPTION          = 0x00C00000
	WS_SYSMENU          = 0x00080000
	WS_MINIMIZEBOX      = 0x00020000
	WS_CHILD            = 0x40000000
	WS_TABSTOP          = 0x00010000
	ES_LEFT             = 0x0000
	ES_AUTOHSCROLL      = 0x0080
	SW_SHOW             = 5
	SW_HIDE             = 0
	IDC_ARROW           = 32512
	WM_CREATE           = 0x0001
	WM_DESTROY          = 0x0002
	WM_SIZE             = 0x0005
	WM_PAINT            = 0x000F
	WM_CLOSE            = 0x0010
	WM_COMMAND          = 0x0111
	WM_TIMER            = 0x0113
	WM_LBUTTONDOWN      = 0x0201
	WM_ERASEBKGND       = 0x0014
	WM_SETFONT          = 0x0030
	WM_SETICON          = 0x0080
	WM_CTLCOLOREDIT     = 0x0133
	WM_APP              = 0x8000
	WM_INSTALL_UPDATE   = WM_APP + 10
	WM_INSTALL_DONE     = WM_APP + 11
	IMAGE_ICON          = 1
	LR_LOADFROMFILE     = 0x0010
	DI_NORMAL           = 0x0003
	TRANSPARENT         = 1
	DT_LEFT             = 0x0000
	DT_CENTER           = 0x0001
	DT_VCENTER          = 0x0004
	DT_SINGLELINE       = 0x0020
	DT_WORDBREAK        = 0x0010
	SRCCOPY             = 0x00CC0020
)

const idPathEdit = 101

type POINT struct{ X, Y int32 }
type RECT struct{ Left, Top, Right, Bottom int32 }
type MSG struct {
	Hwnd           uintptr
	Message        uint32
	WParam, LParam uintptr
	Time           uint32
	Pt             POINT
	LPrivate       uint32
}
type PAINTSTRUCT struct {
	Hdc                uintptr
	Erase              int32
	RcPaint            RECT
	Restore, IncUpdate int32
	Reserved           [32]byte
}
type WNDCLASSEX struct {
	CbSize                                   uint32
	Style                                    uint32
	LpfnWndProc                              uintptr
	CbClsExtra, CbWndExtra                   int32
	HInstance, HIcon, HCursor, HbrBackground uintptr
	LpszMenuName, LpszClassName              *uint16
	HIconSm                                  uintptr
}

type InstallerApp struct {
	hwnd                                                                uintptr
	pathEdit                                                            uintptr
	icon                                                                uintptr
	page                                                                int
	detectedPath                                                        string
	installPath                                                         string
	launch                                                              bool
	desktop                                                             bool
	installing                                                          bool
	installed                                                           bool
	failed                                                              string
	progress                                                            float64
	status                                                              string
	backRect, nextRect, cancelRect, browseRect, launchRect, desktopRect RECT
	mu                                                                  sync.Mutex
}

var app InstallerApp
var fontCache = map[int]uintptr{}
var editBrush uintptr

func wstr(s string) *uint16 { p, _ := syscall.UTF16PtrFromString(s); return p }
func loword(v uintptr) int  { return int(v & 0xffff) }

func main() {
	runtime.LockOSThread()
	pSetProcessDPIAware.Call()
	runUI()
}

func runUI() {
	app.detectedPath = detectInstallPath()
	if app.detectedPath != "" {
		app.installPath = app.detectedPath
	} else {
		app.installPath = filepath.Join(os.Getenv("LOCALAPPDATA"), "Programs", "PowerPilot")
	}
	loadIcon()
	hinst, _, _ := pGetModuleHandleW.Call(0)
	cls := wstr("PowerPilotWizard")
	cursor, _, _ := pLoadCursorW.Call(0, IDC_ARROW)
	wc := WNDCLASSEX{CbSize: uint32(unsafe.Sizeof(WNDCLASSEX{})), Style: 0x0003, LpfnWndProc: syscall.NewCallback(wndProc), HInstance: hinst, HIcon: app.icon, HCursor: cursor, LpszClassName: cls, HIconSm: app.icon}
	if r, _, _ := pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
		return
	}
	hwnd, _, _ := pCreateWindowExW.Call(0, uintptr(unsafe.Pointer(cls)), uintptr(unsafe.Pointer(wstr("Установка — PowerPilot"))), WS_CAPTION|WS_SYSMENU|WS_MINIMIZEBOX, 240, 150, 760, 520, 0, 0, hinst, 0)
	if hwnd == 0 {
		return
	}
	app.hwnd = hwnd
	if app.icon != 0 {
		pSendMessageW.Call(hwnd, WM_SETICON, 1, app.icon)
		pSendMessageW.Call(hwnd, WM_SETICON, 0, app.icon)
	}
	pShowWindow.Call(hwnd, SW_SHOW)
	pUpdateWindow.Call(hwnd)
	var m MSG
	for {
		r, _, _ := pGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			break
		}
		pTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		pDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
}

func wndProc(hwnd uintptr, msg uint32, wParam, lParam uintptr) uintptr {
	switch msg {
	case WM_CREATE:
		editBrush = solid(rgb(255, 255, 255))
		createControls(hwnd)
		layout(hwnd)
		pSetTimer.Call(hwnd, 1, 120, 0)
		return 0
	case WM_SIZE:
		layout(hwnd)
		invalidate(hwnd)
		return 0
	case WM_PAINT:
		paint(hwnd)
		return 0
	case WM_ERASEBKGND:
		return 1
	case WM_LBUTTONDOWN:
		onClick(int32(int16(loword(lParam))), int32(int16((lParam>>16)&0xffff)))
		return 0
	case WM_COMMAND:
		invalidate(hwnd)
		return 0
	case WM_CTLCOLOREDIT:
		hdc := wParam
		pSetBkMode.Call(hdc, TRANSPARENT)
		pSetTextColor.Call(hdc, uintptr(rgb(25, 25, 25)))
		return editBrush
	case WM_TIMER:
		if app.installing {
			invalidate(hwnd)
		}
		return 0
	case WM_INSTALL_UPDATE:
		invalidate(hwnd)
		return 0
	case WM_INSTALL_DONE:
		app.installing = false
		app.installed = true
		app.page = 4
		pShowWindow.Call(app.pathEdit, SW_HIDE)
		invalidate(hwnd)
		return 0
	case WM_CLOSE:
		if !app.installing {
			pDestroyWindow.Call(hwnd)
		}
		return 0
	case WM_DESTROY:
		pKillTimer.Call(hwnd, 1)
		for _, f := range fontCache {
			pDeleteObject.Call(f)
		}
		if editBrush != 0 {
			pDeleteObject.Call(editBrush)
		}
		pPostQuitMessage.Call(0)
		return 0
	}
	r, _, _ := pDefWindowProcW.Call(hwnd, uintptr(msg), wParam, lParam)
	return r
}

func createControls(hwnd uintptr) {
	h, _, _ := pCreateWindowExW.Call(0, uintptr(unsafe.Pointer(wstr("EDIT"))), uintptr(unsafe.Pointer(wstr(app.installPath))), WS_CHILD|WS_TABSTOP|ES_LEFT|ES_AUTOHSCROLL, 0, 0, 100, 30, hwnd, idPathEdit, 0, 0)
	app.pathEdit = h
	pSendMessageW.Call(h, WM_SETFONT, createFont(15, 400), 1)
	pShowWindow.Call(h, SW_HIDE)
	app.launch = true
	app.desktop = true
}
func layout(hwnd uintptr) {
	var rc RECT
	pGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&rc)))
	w := int(rc.Right)
	h := int(rc.Bottom)
	app.backRect = RECT{int32(w - 350), int32(h - 58), int32(w - 250), int32(h - 24)}
	app.nextRect = RECT{int32(w - 240), int32(h - 58), int32(w - 130), int32(h - 24)}
	app.cancelRect = RECT{int32(w - 120), int32(h - 58), int32(w - 20), int32(h - 24)}
	app.browseRect = RECT{int32(w - 150), 190, int32(w - 40), 224}
	app.launchRect = RECT{246, 238, 268, 260}
	app.desktopRect = RECT{246, 276, 268, 298}
	if app.page == 1 {
		move(app.pathEdit, 246, 190, max(180, w-420), 34)
		pShowWindow.Call(app.pathEdit, SW_SHOW)
	} else {
		pShowWindow.Call(app.pathEdit, SW_HIDE)
	}
}

func paint(hwnd uintptr) {
	var ps PAINTSTRUCT
	hdc, _, _ := pBeginPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
	defer pEndPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
	var rc RECT
	pGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&rc)))
	w := int(rc.Right)
	h := int(rc.Bottom)
	if w < 1 || h < 1 {
		return
	}
	mem, _, _ := pCreateCompatibleDC.Call(hdc)
	bmp, _, _ := pCreateCompatibleBitmap.Call(hdc, uintptr(w), uintptr(h))
	old, _, _ := pSelectObject.Call(mem, bmp)
	drawWizard(mem, rc)
	pBitBlt.Call(hdc, 0, 0, uintptr(w), uintptr(h), mem, 0, 0, SRCCOPY)
	pSelectObject.Call(mem, old)
	pDeleteObject.Call(bmp)
	pDeleteDC.Call(mem)
}

func drawWizard(hdc uintptr, rc RECT) {
	w := int(rc.Right)
	h := int(rc.Bottom)
	fill(hdc, rc, rgb(245, 245, 245))
	fill(hdc, RECT{0, 0, 210, rc.Bottom - 78}, rgb(24, 57, 142))
	// classic wizard left visual panel
	for i := 0; i < 18; i++ {
		c := rgb(byte(20+i), byte(52+i), byte(135+i*3))
		fill(hdc, RECT{int32(i * 12), 0, int32(i*12 + 12), rc.Bottom - 78}, c)
	}
	if app.icon != 0 {
		pDrawIconEx.Call(hdc, 54, 72, app.icon, 96, 96, 0, 0, DI_NORMAL)
	}
	drawText(hdc, "POWERPILOT", 28, 190, 150, 32, 21, 700, rgb(255, 255, 255), DT_CENTER|DT_VCENTER|DT_SINGLELINE)
	drawText(hdc, "Setup Wizard", 28, 224, 150, 22, 13, 500, rgb(215, 226, 255), DT_CENTER|DT_VCENTER|DT_SINGLELINE)
	// white content panel + footer divider
	fill(hdc, RECT{210, 0, rc.Right, rc.Bottom - 78}, rgb(255, 255, 255))
	fill(hdc, RECT{0, rc.Bottom - 78, rc.Right, rc.Bottom}, rgb(241, 241, 241))
	fill(hdc, RECT{0, rc.Bottom - 79, rc.Right, rc.Bottom - 78}, rgb(200, 200, 200))

	switch app.page {
	case 0:
		drawWelcome(hdc, w, h)
	case 1:
		drawPath(hdc, w, h)
	case 2:
		drawReady(hdc, w, h)
	case 3:
		drawInstalling(hdc, w, h)
	case 4:
		drawFinish(hdc, w, h)
	}
	if app.page > 0 && app.page < 3 {
		drawButton(hdc, app.backRect, "< Назад", false, true)
	} else {
		drawButton(hdc, app.backRect, "< Назад", false, false)
	}
	next := "Далее >"
	if app.page == 2 {
		next = "Установить"
	}
	if app.page == 4 {
		next = "Готово"
	}
	enabled := !app.installing && app.page != 3
	drawButton(hdc, app.nextRect, next, true, enabled)
	drawButton(hdc, app.cancelRect, "Отмена", false, !app.installing && app.page != 4)
}

func drawWelcome(hdc uintptr, w, h int) {
	drawText(hdc, "Вас приветствует мастер установки PowerPilot", 242, 52, w-280, 64, 24, 700, rgb(20, 20, 20), DT_LEFT|DT_WORDBREAK)
	drawText(hdc, "Эта программа установит PowerPilot 0.8.0 на ваш компьютер.", 242, 142, w-292, 48, 14, 400, rgb(55, 55, 55), DT_LEFT|DT_WORDBREAK)
	msg := "Существующая установка не найдена. Будет выполнена новая установка."
	if app.detectedPath != "" {
		msg = "Найдена существующая установка PowerPilot. Мастер предложит обновить её в той же папке."
	}
	drawText(hdc, msg, 242, 214, w-292, 64, 14, 500, rgb(40, 71, 145), DT_LEFT|DT_WORDBREAK)
	drawText(hdc, "Перед началом установки рекомендуется закрыть PowerPilot, если он сейчас запущен. Мастер сделает это автоматически при обновлении.", 242, 300, w-292, 78, 13, 400, rgb(70, 70, 70), DT_LEFT|DT_WORDBREAK)
	drawText(hdc, "Нажмите «Далее», чтобы продолжить.", 242, h-132, w-292, 28, 13, 500, rgb(40, 40, 40), DT_LEFT|DT_VCENTER|DT_SINGLELINE)
}
func drawPath(hdc uintptr, w, h int) {
	drawText(hdc, "Выбор папки установки", 242, 44, w-280, 38, 22, 700, rgb(20, 20, 20), DT_LEFT|DT_VCENTER|DT_SINGLELINE)
	drawText(hdc, "В какую папку вы хотите установить PowerPilot?", 242, 86, w-292, 32, 14, 400, rgb(55, 55, 55), DT_LEFT|DT_VCENTER|DT_SINGLELINE)
	drawText(hdc, "Программа будет установлена в следующую папку:", 242, 148, w-292, 24, 13, 400, rgb(55, 55, 55), DT_LEFT|DT_VCENTER|DT_SINGLELINE)
	drawButton(hdc, app.browseRect, "Обзор...", false, true)
	mode := "Новая установка"
	if app.detectedPath != "" {
		mode = "Обновление найденной установки"
	}
	drawText(hdc, mode, 242, 246, w-292, 24, 13, 600, rgb(40, 71, 145), DT_LEFT|DT_VCENTER|DT_SINGLELINE)
	drawText(hdc, "Мастер просканировал стандартные папки и запись установленной программы до показа этой страницы.", 242, 278, w-292, 60, 12, 400, rgb(90, 90, 90), DT_LEFT|DT_WORDBREAK)
}
func drawReady(hdc uintptr, w, h int) {
	drawText(hdc, "Готово к установке", 242, 44, w-280, 38, 22, 700, rgb(20, 20, 20), DT_LEFT|DT_VCENTER|DT_SINGLELINE)
	drawText(hdc, "Проверьте параметры перед продолжением.", 242, 88, w-292, 28, 14, 400, rgb(55, 55, 55), DT_LEFT|DT_VCENTER|DT_SINGLELINE)
	mode := "Новая установка"
	if fileExists(filepath.Join(app.installPath, "PowerPilot.exe")) {
		mode = "Обновление существующей установки"
	}
	drawText(hdc, "Режим: "+mode, 242, 142, w-292, 24, 13, 600, rgb(40, 71, 145), DT_LEFT|DT_VCENTER|DT_SINGLELINE)
	drawText(hdc, "Папка: "+app.installPath, 242, 174, w-292, 44, 13, 400, rgb(55, 55, 55), DT_LEFT|DT_WORDBREAK)
	drawCheck(hdc, app.launchRect, app.launch)
	drawText(hdc, "Запустить PowerPilot после установки", 278, 236, w-330, 26, 14, 400, rgb(35, 35, 35), DT_LEFT|DT_VCENTER|DT_SINGLELINE)
	drawCheck(hdc, app.desktopRect, app.desktop)
	drawText(hdc, "Создать ярлык на рабочем столе", 278, 274, w-330, 26, 14, 400, rgb(35, 35, 35), DT_LEFT|DT_VCENTER|DT_SINGLELINE)
	drawText(hdc, "Нажмите «Установить», чтобы начать.", 242, h-132, w-292, 28, 13, 500, rgb(40, 40, 40), DT_LEFT|DT_VCENTER|DT_SINGLELINE)
}
func drawInstalling(hdc uintptr, w, h int) {
	drawText(hdc, "Установка PowerPilot", 242, 44, w-280, 38, 22, 700, rgb(20, 20, 20), DT_LEFT|DT_VCENTER|DT_SINGLELINE)
	app.mu.Lock()
	progress := app.progress
	status := app.status
	failed := app.failed
	app.mu.Unlock()
	drawText(hdc, status, 242, 116, w-292, 34, 14, 400, rgb(55, 55, 55), DT_LEFT|DT_VCENTER|DT_SINGLELINE)
	bar := RECT{242, 174, int32(w - 48), 198}
	roundFill(hdc, bar, rgb(225, 225, 225), 4)
	if progress > 0 {
		bw := int32(float64(bar.Right-bar.Left) * progress)
		roundFill(hdc, RECT{bar.Left, bar.Top, bar.Left + bw, bar.Bottom}, rgb(44, 135, 220), 4)
	}
	drawText(hdc, fmt.Sprintf("%d%%", int(progress*100)), 242, 206, w-292, 24, 13, 600, rgb(40, 71, 145), DT_LEFT|DT_VCENTER|DT_SINGLELINE)
	if failed != "" {
		drawText(hdc, "Ошибка: "+failed, 242, 260, w-292, 64, 13, 500, rgb(180, 45, 45), DT_LEFT|DT_WORDBREAK)
	}
}
func drawFinish(hdc uintptr, w, h int) {
	drawText(hdc, "Установка PowerPilot завершена", 242, 52, w-280, 56, 23, 700, rgb(20, 20, 20), DT_LEFT|DT_WORDBREAK)
	drawText(hdc, "PowerPilot 0.8.0 установлен и готов к работе.", 242, 142, w-292, 42, 14, 400, rgb(55, 55, 55), DT_LEFT|DT_WORDBREAK)
	drawCheck(hdc, app.launchRect, app.launch)
	drawText(hdc, "Запустить PowerPilot сейчас", 278, 236, w-330, 26, 14, 400, rgb(35, 35, 35), DT_LEFT|DT_VCENTER|DT_SINGLELINE)
	drawText(hdc, "Нажмите «Готово», чтобы закрыть мастер.", 242, h-132, w-292, 28, 13, 500, rgb(40, 40, 40), DT_LEFT|DT_VCENTER|DT_SINGLELINE)
}

func onClick(x, y int32) {
	if app.installing {
		return
	}
	if app.page == 1 && pointIn(app.browseRect, x, y) {
		if d := browseFolder(getText(app.pathEdit)); d != "" {
			app.installPath = d
			pSetWindowTextW.Call(app.pathEdit, uintptr(unsafe.Pointer(wstr(d))))
			invalidate(app.hwnd)
		}
		return
	}
	if (app.page == 2 || app.page == 4) && pointIn(app.launchRect, x, y) {
		app.launch = !app.launch
		invalidate(app.hwnd)
		return
	}
	if app.page == 2 && pointIn(app.desktopRect, x, y) {
		app.desktop = !app.desktop
		invalidate(app.hwnd)
		return
	}
	if pointIn(app.backRect, x, y) && app.page > 0 && app.page < 3 {
		app.page--
		layout(app.hwnd)
		invalidate(app.hwnd)
		return
	}
	if pointIn(app.nextRect, x, y) {
		switch app.page {
		case 0:
			app.page = 1
			layout(app.hwnd)
			invalidate(app.hwnd)
		case 1:
			app.installPath = strings.TrimSpace(getText(app.pathEdit))
			if app.installPath == "" {
				return
			}
			app.page = 2
			layout(app.hwnd)
			invalidate(app.hwnd)
		case 2:
			startInstall()
		case 4:
			if app.launch {
				_ = exec.Command(filepath.Join(app.installPath, "PowerPilot.exe")).Start()
			}
			pDestroyWindow.Call(app.hwnd)
		}
		return
	}
	if pointIn(app.cancelRect, x, y) && app.page != 4 {
		pDestroyWindow.Call(app.hwnd)
		return
	}
}

func startInstall() {
	app.page = 3
	app.installing = true
	app.progress = 0
	app.status = "Подготовка…"
	layout(app.hwnd)
	invalidate(app.hwnd)
	go installWorker()
}
func installWorker() {
	setProgress(.08, "Закрытие запущенного PowerPilot…")
	_ = exec.Command("taskkill.exe", "/F", "/IM", "PowerPilot.exe").Run()
	time.Sleep(100 * time.Millisecond)
	if err := os.MkdirAll(app.installPath, 0755); err != nil {
		failInstall(err)
		return
	}
	setProgress(.22, "Копирование файлов…")
	files := map[string]string{"payload/PowerPilot.exe": "PowerPilot.exe", "payload/Uninstall.exe": "Uninstall.exe", "payload/PowerPilot.ico": "PowerPilot.ico"}
	for src, dst := range files {
		b, err := payload.ReadFile(src)
		if err != nil {
			failInstall(err)
			return
		}
		mode := os.FileMode(0644)
		if strings.HasSuffix(strings.ToLower(dst), ".exe") {
			mode = 0755
		}
		if err = os.WriteFile(filepath.Join(app.installPath, dst), b, mode); err != nil {
			failInstall(err)
			return
		}
	}
	setProgress(.55, "Создание ярлыков…")
	exe := filepath.Join(app.installPath, "PowerPilot.exe")
	ico := filepath.Join(app.installPath, "PowerPilot.ico")
	uninst := filepath.Join(app.installPath, "Uninstall.exe")
	if app.desktop {
		_ = createShortcut(filepath.Join(os.Getenv("USERPROFILE"), "Desktop", "PowerPilot.lnk"), exe, ico)
	}
	_ = createShortcut(filepath.Join(os.Getenv("APPDATA"), "Microsoft", "Windows", "Start Menu", "Programs", "PowerPilot.lnk"), exe, ico)
	setProgress(.78, "Регистрация приложения…")
	registerUninstall(app.installPath, exe, uninst)
	setProgress(1, "Готово")
	time.Sleep(180 * time.Millisecond)
	pPostMessageW.Call(app.hwnd, WM_INSTALL_DONE, 0, 0)
}
func setProgress(v float64, status string) {
	app.mu.Lock()
	app.progress = v
	app.status = status
	app.mu.Unlock()
	pPostMessageW.Call(app.hwnd, WM_INSTALL_UPDATE, 0, 0)
}
func failInstall(err error) {
	app.mu.Lock()
	app.failed = err.Error()
	app.status = "Установка не завершена"
	app.mu.Unlock()
	app.installing = false
	pPostMessageW.Call(app.hwnd, WM_INSTALL_UPDATE, 0, 0)
}

func detectInstallPath() string {
	candidates := []string{}
	key := `HKCU\Software\Microsoft\Windows\CurrentVersion\Uninstall\PowerPilot`
	cmd := exec.Command("reg.exe", "query", key, "/v", "InstallLocation")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if b, err := cmd.CombinedOutput(); err == nil {
		for _, line := range strings.Split(string(b), "\n") {
			if strings.Contains(line, "InstallLocation") {
				parts := strings.Fields(line)
				if len(parts) >= 3 {
					candidates = append(candidates, strings.Join(parts[2:], " "))
				}
			}
		}
	}
	candidates = append(candidates, filepath.Join(os.Getenv("LOCALAPPDATA"), "Programs", "PowerPilot"), filepath.Join(os.Getenv("ProgramFiles"), "PowerPilot"))
	if p := os.Getenv("ProgramFiles(x86)"); p != "" {
		candidates = append(candidates, filepath.Join(p, "PowerPilot"))
	}
	for _, p := range candidates {
		p = strings.TrimSpace(p)
		if p != "" && fileExists(filepath.Join(p, "PowerPilot.exe")) {
			return p
		}
	}
	return ""
}
func browseFolder(initial string) string {
	if initial == "" {
		initial = filepath.Join(os.Getenv("LOCALAPPDATA"), "Programs")
	}
	esc := func(s string) string { return strings.ReplaceAll(s, "'", "''") }
	script := fmt.Sprintf(`$sh=New-Object -ComObject Shell.Application;$f=$sh.BrowseForFolder(0,'Выберите папку установки PowerPilot',0,'%s');if($f){$f.Self.Path}`, esc(initial))
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
func createShortcut(path, target, icon string) error {
	if path == "" {
		return nil
	}
	_ = os.MkdirAll(filepath.Dir(path), 0755)
	esc := func(s string) string { return strings.ReplaceAll(s, "'", "''") }
	script := fmt.Sprintf(`$ws=New-Object -ComObject WScript.Shell;$s=$ws.CreateShortcut('%s');$s.TargetPath='%s';$s.WorkingDirectory='%s';$s.IconLocation='%s,0';$s.Description='PowerPilot — управление питанием Windows';$s.Save()`, esc(path), esc(target), esc(filepath.Dir(target)), esc(icon))
	c := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-WindowStyle", "Hidden", "-Command", script)
	c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return c.Run()
}
func registerUninstall(dir, exe, uninst string) {
	key := `HKCU\Software\Microsoft\Windows\CurrentVersion\Uninstall\PowerPilot`
	vals := [][]string{{"DisplayName", "PowerPilot"}, {"DisplayVersion", "0.8.1"}, {"Publisher", "PowerPilot Project"}, {"InstallLocation", dir}, {"DisplayIcon", exe}, {"UninstallString", `"` + uninst + `"`}, {"NoModify", "1"}, {"NoRepair", "1"}}
	for _, v := range vals {
		typ := "REG_SZ"
		if v[0] == "NoModify" || v[0] == "NoRepair" {
			typ = "REG_DWORD"
		}
		c := exec.Command("reg.exe", "add", key, "/v", v[0], "/t", typ, "/d", v[1], "/f")
		c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		_ = c.Run()
	}
}

func loadIcon() {
	dir := filepath.Join(os.TempDir(), "PowerPilot-setup")
	_ = os.MkdirAll(dir, 0755)
	p := filepath.Join(dir, "PowerPilot.ico")
	_ = os.WriteFile(p, iconData, 0644)
	app.icon, _, _ = pLoadImageW.Call(0, uintptr(unsafe.Pointer(wstr(p))), IMAGE_ICON, 96, 96, LR_LOADFROMFILE)
}
func move(h uintptr, x, y, w, hgt int) {
	if h != 0 {
		pMoveWindow.Call(h, uintptr(x), uintptr(y), uintptr(w), uintptr(hgt), 1)
	}
}
func getText(h uintptr) string {
	n, _, _ := pGetWindowTextLen.Call(h)
	buf := make([]uint16, int(n)+1)
	pGetWindowTextW.Call(h, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return syscall.UTF16ToString(buf)
}
func pointIn(r RECT, x, y int32) bool {
	return x >= r.Left && x < r.Right && y >= r.Top && y < r.Bottom
}
func invalidate(h uintptr)     { pInvalidateRect.Call(h, 0, 0) }
func fileExists(p string) bool { _, e := os.Stat(p); return e == nil }
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
func rgb(r, g, b byte) uint32 { return uint32(r) | uint32(g)<<8 | uint32(b)<<16 }
func solid(c uint32) uintptr  { b, _, _ := pCreateSolidBrush.Call(uintptr(c)); return b }
func fill(hdc uintptr, r RECT, c uint32) {
	b := solid(c)
	pFillRect.Call(hdc, uintptr(unsafe.Pointer(&r)), b)
	pDeleteObject.Call(b)
}
func roundFill(hdc uintptr, r RECT, c uint32, radius int32) {
	b := solid(c)
	old, _, _ := pSelectObject.Call(hdc, b)
	pen, _, _ := pCreatePen.Call(5, 1, uintptr(c))
	op, _, _ := pSelectObject.Call(hdc, pen)
	pRoundRect.Call(hdc, uintptr(r.Left), uintptr(r.Top), uintptr(r.Right), uintptr(r.Bottom), uintptr(radius*2), uintptr(radius*2))
	pSelectObject.Call(hdc, old)
	pSelectObject.Call(hdc, op)
	pDeleteObject.Call(b)
	pDeleteObject.Call(pen)
}
func createFont(size, weight int) uintptr {
	key := size*1000 + weight
	if f := fontCache[key]; f != 0 {
		return f
	}
	h, _, _ := pCreateFontW.Call(uintptr(int64(-size)), 0, 0, 0, uintptr(weight), 0, 0, 0, 1, 0, 0, 5, 0, uintptr(unsafe.Pointer(wstr("Segoe UI"))))
	fontCache[key] = h
	return h
}
func drawText(hdc uintptr, text string, x, y, w, h, size, weight int, color uint32, flags uint32) {
	font := createFont(size, weight)
	old, _, _ := pSelectObject.Call(hdc, font)
	pSetBkMode.Call(hdc, TRANSPARENT)
	pSetTextColor.Call(hdc, uintptr(color))
	r := RECT{int32(x), int32(y), int32(x + w), int32(y + h)}
	u := syscall.StringToUTF16(text)
	pDrawTextW.Call(hdc, uintptr(unsafe.Pointer(&u[0])), ^uintptr(0), uintptr(unsafe.Pointer(&r)), uintptr(flags))
	pSelectObject.Call(hdc, old)
}
func drawButton(hdc uintptr, r RECT, text string, primary, enabled bool) {
	bg := rgb(234, 234, 234)
	txt := rgb(25, 25, 25)
	if primary {
		bg = rgb(225, 241, 252)
	}
	if !enabled {
		bg = rgb(239, 239, 239)
		txt = rgb(150, 150, 150)
	}
	roundFill(hdc, r, bg, 4)
	drawText(hdc, text, int(r.Left), int(r.Top), int(r.Right-r.Left), int(r.Bottom-r.Top), 13, 500, txt, DT_CENTER|DT_VCENTER|DT_SINGLELINE)
}
func drawCheck(hdc uintptr, r RECT, on bool) {
	fill(hdc, r, rgb(255, 255, 255))
	border := rgb(120, 120, 120)
	pen, _, _ := pCreatePen.Call(0, 1, uintptr(border))
	old, _, _ := pSelectObject.Call(hdc, pen)
	b := solid(rgb(255, 255, 255))
	ob, _, _ := pSelectObject.Call(hdc, b)
	pRoundRect.Call(hdc, uintptr(r.Left), uintptr(r.Top), uintptr(r.Right), uintptr(r.Bottom), 3, 3)
	pSelectObject.Call(hdc, old)
	pSelectObject.Call(hdc, ob)
	pDeleteObject.Call(pen)
	pDeleteObject.Call(b)
	if on {
		inner := RECT{r.Left + 5, r.Top + 5, r.Right - 5, r.Bottom - 5}
		roundFill(hdc, inner, rgb(44, 135, 220), 2)
	}
}
