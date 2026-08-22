//go:build windows

package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

// PowerPilot 0.4.0 - Win32 + Direct2D/DirectWrite, no external runtime.

const appVersion = "0.8.3"
const normalMinClientW = 640
const normalMinClientH = 650
const miniClientW = 500
const miniClientH = 176

//go:embed assets/click.wav
var clickSound []byte

//go:embed assets/open.wav
var openSound []byte

//go:embed assets/success.wav
var successSound []byte

//go:embed assets/settings.ico
var settingsIconData []byte

//go:embed assets/settings.png
var settingsPNGData []byte

//go:embed assets/bell.png
var bellPNGData []byte

//go:embed assets/paste.png
var pastePNGData []byte

//go:embed assets/paste-all.png
var pasteAllPNGData []byte

//go:embed assets/copy.png
var copyPNGData []byte

//go:embed assets/delete.png
var deletePNGData []byte

//go:embed assets/pause.png
var pausePNGData []byte

//go:embed assets/play.png
var playPNGData []byte

//go:embed assets/notification-clear.png
var notificationClearPNGData []byte

//go:embed assets/notification-read.png
var notificationReadPNGData []byte

//go:embed assets/caption-close.png
var captionClosePNGData []byte

//go:embed assets/caption-fullscreen.png
var captionFullscreenPNGData []byte

//go:embed assets/caption-minimize.png
var captionMinimizePNGData []byte

//go:embed assets/caption-mini.png
var captionMiniPNGData []byte

//go:embed assets/caption-exit-mini.png
var captionExitMiniPNGData []byte

//go:embed assets/caption-pin.png
var captionPinPNGData []byte

//go:embed assets/caption-restore.png
var captionRestorePNGData []byte

//go:embed assets/PowerPilot.ico
var appIconData []byte

//go:embed assets/PowerPilot.png
var appPNGData []byte

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	gdi32    = syscall.NewLazyDLL("gdi32.dll")
	shell32  = syscall.NewLazyDLL("shell32.dll")
	powrprof = syscall.NewLazyDLL("powrprof.dll")
	winmm    = syscall.NewLazyDLL("winmm.dll")
	advapi32 = syscall.NewLazyDLL("advapi32.dll")

	pRegisterClassExW           = user32.NewProc("RegisterClassExW")
	pCreateWindowExW            = user32.NewProc("CreateWindowExW")
	pDefWindowProcW             = user32.NewProc("DefWindowProcW")
	pShowWindow                 = user32.NewProc("ShowWindow")
	pUpdateWindow               = user32.NewProc("UpdateWindow")
	pAnimateWindow              = user32.NewProc("AnimateWindow")
	pGetWindowLongPtrW          = user32.NewProc("GetWindowLongPtrW")
	pSetWindowLongPtrW          = user32.NewProc("SetWindowLongPtrW")
	pSetLayeredWindowAttributes = user32.NewProc("SetLayeredWindowAttributes")
	pGetMessageW                = user32.NewProc("GetMessageW")
	pTranslateMessage           = user32.NewProc("TranslateMessage")
	pDispatchMessageW           = user32.NewProc("DispatchMessageW")
	pPostQuitMessage            = user32.NewProc("PostQuitMessage")
	pBeginPaint                 = user32.NewProc("BeginPaint")
	pEndPaint                   = user32.NewProc("EndPaint")
	pGetClientRect              = user32.NewProc("GetClientRect")
	pInvalidateRect             = user32.NewProc("InvalidateRect")
	pSetTimer                   = user32.NewProc("SetTimer")
	pKillTimer                  = user32.NewProc("KillTimer")
	pSetWindowTextW             = user32.NewProc("SetWindowTextW")
	pGetWindowTextW             = user32.NewProc("GetWindowTextW")
	pGetWindowTextLen           = user32.NewProc("GetWindowTextLengthW")
	pSendMessageW               = user32.NewProc("SendMessageW")
	pSetWindowPos               = user32.NewProc("SetWindowPos")
	pMoveWindow                 = user32.NewProc("MoveWindow")
	pGetCursorPos               = user32.NewProc("GetCursorPos")
	pScreenToClient             = user32.NewProc("ScreenToClient")
	pTrackMouseEvent            = user32.NewProc("TrackMouseEvent")
	pLoadCursorW                = user32.NewProc("LoadCursorW")
	pLoadIconW                  = user32.NewProc("LoadIconW")
	pLoadImageW                 = user32.NewProc("LoadImageW")
	pDrawIconEx                 = user32.NewProc("DrawIconEx")
	pMessageBoxW                = user32.NewProc("MessageBoxW")
	pDestroyWindow              = user32.NewProc("DestroyWindow")
	pEnableWindow               = user32.NewProc("EnableWindow")
	pIsWindowVisible            = user32.NewProc("IsWindowVisible")
	pSetFocus                   = user32.NewProc("SetFocus")
	pGetFocus                   = user32.NewProc("GetFocus")
	pSetCapture                 = user32.NewProc("SetCapture")
	pReleaseCapture             = user32.NewProc("ReleaseCapture")
	pGetLastInputInfo           = user32.NewProc("GetLastInputInfo")
	pEnumWindows                = user32.NewProc("EnumWindows")
	pGetWindowThreadProcessId   = user32.NewProc("GetWindowThreadProcessId")
	pPostMessageW               = user32.NewProc("PostMessageW")
	pSetProcessDPIAware         = user32.NewProc("SetProcessDPIAware")
	pGetWindowRect              = user32.NewProc("GetWindowRect")
	pMonitorFromWindow          = user32.NewProc("MonitorFromWindow")
	pGetMonitorInfoW            = user32.NewProc("GetMonitorInfoW")
	pIsZoomed                   = user32.NewProc("IsZoomed")
	pIsIconic                   = user32.NewProc("IsIconic")
	pRegisterHotKey             = user32.NewProc("RegisterHotKey")
	pUnregisterHotKey           = user32.NewProc("UnregisterHotKey")

	pCreateSolidBrush       = gdi32.NewProc("CreateSolidBrush")
	pDeleteObject           = gdi32.NewProc("DeleteObject")
	pSetBkMode              = gdi32.NewProc("SetBkMode")
	pSetTextColor           = gdi32.NewProc("SetTextColor")
	pCreateFontW            = gdi32.NewProc("CreateFontW")
	pSelectObject           = gdi32.NewProc("SelectObject")
	pRoundRect              = gdi32.NewProc("RoundRect")
	pFillRect               = user32.NewProc("FillRect")
	pDrawTextW              = user32.NewProc("DrawTextW")
	pCreatePen              = gdi32.NewProc("CreatePen")
	pCreateCompatibleDC     = gdi32.NewProc("CreateCompatibleDC")
	pCreateCompatibleBitmap = gdi32.NewProc("CreateCompatibleBitmap")
	pDeleteDC               = gdi32.NewProc("DeleteDC")
	pBitBlt                 = gdi32.NewProc("BitBlt")
	pStretchBlt             = gdi32.NewProc("StretchBlt")
	pSetStretchBltMode      = gdi32.NewProc("SetStretchBltMode")

	pGetModuleHandleW           = kernel32.NewProc("GetModuleHandleW")
	pGetTickCount64             = kernel32.NewProc("GetTickCount64")
	pOpenProcess                = kernel32.NewProc("OpenProcess")
	pTerminateProcess           = kernel32.NewProc("TerminateProcess")
	pCloseHandle                = kernel32.NewProc("CloseHandle")
	pCreateToolhelp32Snapshot   = kernel32.NewProc("CreateToolhelp32Snapshot")
	pProcess32FirstW            = kernel32.NewProc("Process32FirstW")
	pProcess32NextW             = kernel32.NewProc("Process32NextW")
	pQueryFullProcessImageNameW = kernel32.NewProc("QueryFullProcessImageNameW")
	pProcessIdToSessionId       = kernel32.NewProc("ProcessIdToSessionId")

	pShellNotifyIconW = shell32.NewProc("Shell_NotifyIconW")
	pSetSuspendState  = powrprof.NewProc("SetSuspendState")
	pPlaySoundW       = winmm.NewProc("PlaySoundW")
)

const (
	WS_OVERLAPPEDWINDOW = 0x00CF0000
	WS_POPUP            = 0x80000000
	WS_CAPTION          = 0x00C00000
	WS_THICKFRAME       = 0x00040000
	WS_SYSMENU          = 0x00080000
	WS_MINIMIZEBOX      = 0x00020000
	WS_MAXIMIZEBOX      = 0x00010000
	WS_VISIBLE          = 0x10000000
	WS_CHILD            = 0x40000000
	WS_TABSTOP          = 0x00010000
	WS_BORDER           = 0x00800000
	WS_EX_CLIENTEDGE    = 0x00000200
	WS_EX_APPWINDOW     = 0x00040000
	WS_EX_TOOLWINDOW    = 0x00000080
	WS_EX_LAYERED       = 0x00080000
	WS_CLIPCHILDREN     = 0x02000000
	WS_CLIPSIBLINGS     = 0x04000000
	ES_LEFT             = 0x0000
	ES_CENTER           = 0x0001
	ES_NUMBER           = 0x2000
	BS_PUSHBUTTON       = 0x00000000
	BS_AUTOCHECKBOX     = 0x00000003
	BS_FLAT             = 0x00008000
	CBS_DROPDOWNLIST    = 0x0003
	LBS_EXTENDEDSEL     = 0x0800
	LBS_NOTIFY          = 0x0001
	WS_VSCROLL          = 0x00200000

	WM_CREATE          = 0x0001
	WM_DESTROY         = 0x0002
	WM_SIZE            = 0x0005
	WM_GETMINMAXINFO   = 0x0024
	WM_NCHITTEST       = 0x0084
	WM_NCCALCSIZE      = 0x0083
	WM_NCACTIVATE      = 0x0086
	WM_PAINT           = 0x000F
	WM_CLOSE           = 0x0010
	WM_COMMAND         = 0x0111
	WM_INPUTLANGCHANGE = 0x0051
	WM_HOTKEY          = 0x0312
	WM_SYSCOMMAND      = 0x0112
	WM_TIMER           = 0x0113
	WM_KEYDOWN         = 0x0100
	WM_MOUSEMOVE       = 0x0200
	WM_LBUTTONDOWN     = 0x0201
	WM_LBUTTONUP       = 0x0202
	WM_ERASEBKGND      = 0x0014
	WM_MOUSELEAVE      = 0x02A3
	WM_MOUSEWHEEL      = 0x020A
	WM_SETFONT         = 0x0030
	WM_SETICON         = 0x0080
	WM_CTLCOLORSTATIC  = 0x0138
	WM_CTLCOLOREDIT    = 0x0133
	WM_CTLCOLORBTN     = 0x0135
	WM_APP             = 0x8000

	SW_SHOW     = 5
	SW_HIDE     = 0
	SW_RESTORE  = 9
	AW_ACTIVATE = 0x00020000
	AW_HIDE     = 0x00010000
	AW_BLEND    = 0x00080000
	SW_MINIMIZE = 6
	SW_MAXIMIZE = 3

	SC_MINIMIZE = 0xF020
	SC_MAXIMIZE = 0xF030
	SC_RESTORE  = 0xF120
	GWL_EXSTYLE = -20
	LWA_ALPHA   = 0x00000002

	BN_CLICKED      = 0
	CBN_SELCHANGE   = 1
	EN_SETFOCUS     = 0x0100
	EN_KILLFOCUS    = 0x0200
	EN_CHANGE       = 0x0300
	EM_GETSEL       = 0x00B0
	EM_SETLIMITTEXT = 0x00C5
	EM_SETSEL       = 0x00B1
	EM_SETCUEBANNER = 0x1501

	MB_OK              = 0x00000000
	MB_ICONERROR       = 0x00000010
	MB_ICONINFORMATION = 0x00000040
	MB_YESNO           = 0x00000004
	MB_ICONQUESTION    = 0x00000020
	MB_ICONWARNING     = 0x00000030
	IDYES              = 6

	TRANSPARENT     = 1
	DT_LEFT         = 0x00000000
	DT_CENTER       = 0x00000001
	DT_RIGHT        = 0x00000002
	DT_VCENTER      = 0x00000004
	DT_WORDBREAK    = 0x00000010
	DT_SINGLELINE   = 0x00000020
	DT_END_ELLIPSIS = 0x00008000

	COLOR_WINDOW    = 5
	IDC_ARROW       = 32512
	IDI_APPLICATION = 32512
	IMAGE_ICON      = 1
	LR_LOADFROMFILE = 0x0010
	DI_NORMAL       = 0x0003
	SND_ASYNC       = 0x0001
	SND_NODEFAULT   = 0x0002
	SND_MEMORY      = 0x0004

	PROCESS_TERMINATE                 = 0x0001
	PROCESS_QUERY_LIMITED_INFORMATION = 0x1000
	TH32CS_SNAPPROCESS                = 0x00000002
	INVALID_HANDLE_VALUE              = ^uintptr(0)

	WM_USER        = 0x0400
	LB_ADDSTRING   = 0x0180
	LB_GETSELCOUNT = 0x0190
	LB_GETSELITEMS = 0x0191
	LB_GETTEXT     = 0x0189
	LB_GETTEXTLEN  = 0x018A
	LB_SETSEL      = 0x0185
	CB_ADDSTRING   = 0x0143
	CB_SETCURSEL   = 0x014E
	CB_GETCURSEL   = 0x0147

	NIM_ADD     = 0x00000000
	NIM_MODIFY  = 0x00000001
	NIM_DELETE  = 0x00000002
	NIF_MESSAGE = 0x00000001
	NIF_ICON    = 0x00000002
	NIF_TIP     = 0x00000004

	WM_TRAY                 = WM_APP + 1
	WM_HISTORY_CHANGED      = WM_APP + 2
	WM_RESOURCE_UPDATED     = WM_APP + 3
	WM_NOTIFICATION_CHANGED = WM_APP + 4
	WM_RBUTTONUP            = 0x0205
	WM_RBUTTONDOWN          = 0x0204
	WM_MBUTTONDOWN          = 0x0207
	WM_MBUTTONUP            = 0x0208
	WM_LBUTTONDBLCLK        = 0x0203

	MF_STRING       = 0x00000000
	MF_POPUP        = 0x00000010
	MF_SEPARATOR    = 0x00000800
	MOD_ALT         = 0x0001
	MOD_CONTROL     = 0x0002
	MOD_NOREPEAT    = 0x4000
	TPM_RIGHTBUTTON = 0x0002
	TPM_BOTTOMALIGN = 0x0020
	SRCCOPY         = 0x00CC0020
	HALFTONE        = 4

	SPI_SETCLIENTAREAANIMATION = 0x1043
	HTCLIENT                   = 1
	HTCAPTION                  = 2
	HTLEFT                     = 10
	HTRIGHT                    = 11
	HTTOP                      = 12
	HTTOPLEFT                  = 13
	HTTOPRIGHT                 = 14
	HTBOTTOM                   = 15
	HTBOTTOMLEFT               = 16
	HTBOTTOMRIGHT              = 17
	SWP_NOZORDER               = 0x0004
	SWP_NOACTIVATE             = 0x0010
	MONITOR_DEFAULTTONEAREST   = 0x00000002
)

const (
	idDelayHours     = 101
	idDelayMinutes   = 102
	idDelaySeconds   = 103
	idExact          = 104
	idIdleMinutes    = 105
	idWatchProcess   = 106
	idCloseProcesses = 107
	idPickProcesses  = 108
	idStart          = 109
	idCancel         = 110
	idPostpone       = 111
	idAutoStart      = 112
	idMinTray        = 113
	idWarning        = 114
	idTaskName       = 115
	idScheduleTime   = 116
	idCondThreshold  = 117
	idCondHold       = 118
	idCondText       = 119
	idStepValue      = 120
	idStepText       = 121
	idSafetyIdle     = 122
	idExactDay       = 123
	idExactMonth     = 124
	idExactYear      = 125
	idExactHour      = 126
	idExactMinute    = 127
	idSoundVolume    = 128
	idSavedSearch    = 129
	idHistorySearch  = 130
	idStepRetries    = 131
	idStepDelay      = 132
	idWakeLead       = 133
	idCondDelay      = 134
	idResourceSearch = 135
	idTimelineTicks  = 136
	idGraphWidth     = 137
	idGraphHeight    = 138
)

type POINT struct{ X, Y int32 }
type RECT struct{ Left, Top, Right, Bottom int32 }
type MINMAXINFO struct {
	PtReserved     POINT
	PtMaxSize      POINT
	PtMaxPosition  POINT
	PtMinTrackSize POINT
	PtMaxTrackSize POINT
}

type MONITORINFO struct {
	CbSize    uint32
	RcMonitor RECT
	RcWork    RECT
	DwFlags   uint32
}

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
type TRACKMOUSEEVENT struct {
	CbSize      uint32
	DwFlags     uint32
	HwndTrack   uintptr
	DwHoverTime uint32
}
type LASTINPUTINFO struct {
	CbSize uint32
	DwTime uint32
}
type PROCESSENTRY32 struct {
	Size            uint32
	CntUsage        uint32
	ProcessID       uint32
	DefaultHeapID   uintptr
	ModuleID        uint32
	CntThreads      uint32
	ParentProcessID uint32
	PcPriClassBase  int32
	Flags           uint32
	ExeFile         [260]uint16
}
type NOTIFYICONDATA struct {
	CbSize               uint32
	HWnd                 uintptr
	UID                  uint32
	UFlags               uint32
	UCallbackMessage     uint32
	HIcon                uintptr
	SzTip                [128]uint16
	DwState, DwStateMask uint32
	SzInfo               [256]uint16
	UTimeoutOrVersion    uint32
	SzInfoTitle          [64]uint16
	DwInfoFlags          uint32
	GuidItem             [16]byte
	HBalloonIcon         uintptr
}

type Theme struct{ bg, panel, panel2, border, text, muted, accent, accent2, danger, success uint32 }

var darkTheme = Theme{rgb(14, 17, 23), rgb(21, 26, 35), rgb(29, 35, 46), rgb(50, 60, 76), rgb(238, 242, 249), rgb(151, 163, 184), rgb(92, 116, 255), rgb(124, 144, 255), rgb(238, 78, 91), rgb(73, 198, 128)}
var lightTheme = Theme{rgb(239, 243, 250), rgb(250, 252, 255), rgb(230, 236, 247), rgb(198, 207, 222), rgb(30, 36, 48), rgb(92, 104, 124), rgb(79, 103, 238), rgb(99, 124, 245), rgb(211, 66, 76), rgb(45, 166, 103)}
var theme = darkTheme

type SavedTask struct {
	ID             string                `json:"id"`
	Name           string                `json:"name"`
	Action         int                   `json:"action"`
	Mode           int                   `json:"mode"`
	DelayHours     int                   `json:"delay_hours"`
	DelayMinutes   int                   `json:"delay_minutes"`
	DelaySeconds   int                   `json:"delay_seconds"`
	Exact          string                `json:"exact"`
	IdleMinutes    int                   `json:"idle_minutes"`
	WatchProcess   string                `json:"watch_process"`
	CloseBefore    bool                  `json:"close_before"`
	Processes      []string              `json:"processes"`
	WarningSeconds int                   `json:"warning_seconds"`
	Conditions     []AutomationCondition `json:"conditions,omitempty"`
	TriggerLogic   int                   `json:"trigger_logic"`
	Steps          []ActionStep          `json:"steps,omitempty"`
	Recurrence     RecurrenceSpec        `json:"recurrence"`
	LastRunKey     string                `json:"last_run_key,omitempty"`
	Favorite       bool                  `json:"favorite,omitempty"`
	Paused         bool                  `json:"paused,omitempty"`
	TaskKind       int                   `json:"task_kind,omitempty"` // 0 simple, 1 block
	Graph          ScenarioGraph         `json:"graph,omitempty"`
}

type Settings struct {
	Action                    int                   `json:"action"`
	Mode                      int                   `json:"mode"`
	DelayHours                int                   `json:"delay_hours"`
	DelayMinutes              int                   `json:"delay_minutes"`
	DelaySeconds              int                   `json:"delay_seconds"`
	Exact                     string                `json:"exact"`
	IdleMinutes               int                   `json:"idle_minutes"`
	WatchProcess              string                `json:"watch_process"`
	CloseBefore               bool                  `json:"close_before"`
	Processes                 []string              `json:"processes"`
	AutoStart                 bool                  `json:"auto_start"`
	MinimizeToTray            bool                  `json:"minimize_to_tray"`
	WarningSeconds            int                   `json:"warning_seconds"`
	Sounds                    bool                  `json:"sounds"`
	SoundVolume               int                   `json:"sound_volume"`
	Notifications             bool                  `json:"notifications"`
	WakeScheduledTasks        bool                  `json:"wake_scheduled_tasks"`
	WakeLeadMinutes           int                   `json:"wake_lead_minutes"`
	ThemeMode                 int                   `json:"theme_mode"`
	Background                int                   `json:"background"`
	SurfaceStyle              int                   `json:"surface_style"`
	AnimationMode             int                   `json:"animation_mode"`
	LockMinimumSize           bool                  `json:"lock_minimum_size"`
	LockCurrentSize           bool                  `json:"lock_current_size"`
	LockedWindowW             int                   `json:"locked_window_w,omitempty"`
	LockedWindowH             int                   `json:"locked_window_h,omitempty"`
	AdvancedConditions        []AutomationCondition `json:"advanced_conditions,omitempty"`
	TriggerLogic              int                   `json:"trigger_logic"`
	ActionSteps               []ActionStep          `json:"action_steps,omitempty"`
	Recurrence                RecurrenceSpec        `json:"recurrence"`
	SafetyFullscreen          bool                  `json:"safety_fullscreen"`
	SafetyRecentInput         bool                  `json:"safety_recent_input"`
	SafetyIdleMinutes         int                   `json:"safety_idle_minutes"`
	SafetyProcesses           []string              `json:"safety_processes"`
	ShowSystemProcesses       bool                  `json:"show_system_processes"`
	HideZeroResourceProcesses bool                  `json:"hide_zero_resource_processes"`
	ConfirmedSystemProcesses  []string              `json:"confirmed_system_processes,omitempty"`
	TaskKind                  int                   `json:"task_kind,omitempty"` // 0 simple, 1 advanced
	AlwaysOnTopMini           bool                  `json:"always_on_top_mini"`
	MiniShowTask              bool                  `json:"mini_show_task"`
	MiniShowCountdown         bool                  `json:"mini_show_countdown"`
	MiniShowStep              bool                  `json:"mini_show_step"`
	MiniShowMetrics           bool                  `json:"mini_show_metrics"`
	MiniSize                  int                   `json:"mini_size"`
	UIScale                   int                   `json:"ui_scale"`
	ResourceRefreshMS         int                   `json:"resource_refresh_ms"`
	ResourceTimelineMode      int                   `json:"resource_timeline_mode,omitempty"` // 0 clock time, 1 relative to current sample
	ResourceTimelineTicks     int                   `json:"resource_timeline_ticks,omitempty"`
	GraphWindowSize           int                   `json:"graph_window_size,omitempty"`
	GraphWindowWidth          int                   `json:"graph_window_width,omitempty"`
	GraphWindowHeight         int                   `json:"graph_window_height,omitempty"`
	GraphWindowSizeLocked     bool                  `json:"graph_window_size_locked,omitempty"`
	IdleSecondsMigrated       bool                  `json:"idle_seconds_migrated,omitempty"`
	GlobalHotkeys             bool                  `json:"global_hotkeys"`
	TemperatureAutoUpdate     bool                  `json:"temperature_auto_update"`
	SavedTasks                []SavedTask           `json:"saved_tasks"`
	ScenarioGraph             ScenarioGraph         `json:"scenario_graph,omitempty"`
}

type HistoryItem struct {
	When   string
	Kind   string
	Detail string
	RunID  string
}

// ProcessInfo is deliberately conservative: if any running instance of an executable
// looks like a Windows/service process, the executable name is treated as system-wide.
// That matters because taskkill /IM acts by image name and could otherwise hit a protected instance.
type ProcessInfo struct {
	Name      string
	PID       uint32
	System    bool
	ImagePath string
}

type Schedule struct {
	active           bool
	action           int
	mode             int
	target           time.Time
	idleThreshold    time.Duration
	watchProcess     string
	started          time.Time
	total            time.Duration
	warned           bool
	conditions       []AutomationCondition
	triggerLogic     int
	steps            []ActionStep
	closeBefore      bool
	processes        []string
	warningSeconds   int
	sourceTaskID     string
	sourceTaskName   string
	safetyNotice     bool
	runID            string
	triggerLogged    bool
	conditionsLogged bool
	lastWaitDetail   string
	lastWaitLog      time.Time
	graph            ScenarioGraph
}

type App struct {
	hwnd                                   uintptr
	edits                                  map[int]uintptr
	buttons                                map[int]uintptr
	font, fontSmall, fontLarge, inlineFont uintptr
	settings                               Settings
	schedule                               Schedule
	selectedAction                         int
	selectedMode                           int
	hoverAction                            int
	hoverMode                              int
	hoverTitle                             int
	actionAnim                             [4]float64
	modeAnim                               [6]float64
	mouseTracking                          bool
	mouseX, mouseY                         int32
	hoverAnim                              float64
	hoverSeen                              bool
	hoverKey                               int64
	hoverRect                              RECT
	tooltipRect                            RECT
	tooltipText                            string
	tooltipSince                           time.Time
	settingsHoverAnim                      float64
	status                                 string
	countdown                              string
	progress                               float64
	miniMode                               bool
	normalWindowRect                       RECT
	titleBarRect                           RECT
	miniBtnRect                            RECT
	minBtnRect                             RECT
	maxBtnRect                             RECT
	closeBtnRect                           RECT
	miniCancelRect                         RECT
	miniPostponeRect                       RECT
	actionRects                            [4]RECT
	modeRects                              [6]RECT
	timeFieldRects                         [3]RECT
	exactFieldRects                        [5]RECT
	whenFieldRect                          RECT
	warningFieldRect                       RECT
	chainRects                             [3]RECT
	taskTabRect                            RECT
	taskMoreRect                           RECT
	monitorTabRect                         RECT
	resourceMoreRect                       RECT
	resourceAdvancedMenuRect               RECT
	resourceStatsMenuRect                  RECT
	currentTaskMenuRect                    RECT
	blockTaskTabRect                       RECT
	savedTabRect                           RECT
	settingsBtnRect                        RECT
	notificationBtnRect                    RECT
	notificationPanelRect                  RECT
	notificationMarkReadRect               RECT
	notificationClearRect                  RECT
	notificationUnreadOnlyRect             RECT
	notificationRows                       [6]RECT
	notificationRowIndices                 [6]int
	notificationReadRects                  [6]RECT
	notificationListClip                   RECT
	notificationScrollTrack                RECT
	notificationScrollThumb                RECT
	notificationScrollPx                   float64
	notificationScrollTarget               float64
	notificationScrollMax                  float64
	confirmClearNotifications              bool
	notificationConfirmRect                RECT
	notificationConfirmYesRect             RECT
	notificationConfirmNoRect              RECT
	notificationPanelOpen                  bool
	notificationUnreadOnly                 bool
	notificationBellHover                  bool
	notificationBellBurstStarted           time.Time
	notificationBellLastUnread             int
	closeBeforeRect                        RECT
	pickRect                               RECT
	startRect                              RECT
	saveTaskRect                           RECT
	cancelRect                             RECT
	postponeRect                           RECT
	checkPopupRect                         RECT
	checkTestRect                          RECT
	checkDiagRect                          RECT
	checkBackRect                          RECT
	saveConfirmRect                        RECT
	saveBackRect                           RECT
	autoRect                               RECT
	trayRect                               RECT
	soundsRect                             RECT
	volumeTrackRect                        RECT
	volumeKnobRect                         RECT
	volumeValueRect                        RECT
	lockMinimumRect                        RECT
	lockCurrentRect                        RECT
	wakeScheduledRect                      RECT
	wakeLeadFieldRect                      RECT
	hotkeysRect                            RECT
	settingsTabs                           [7]RECT
	settingsSectionRects                   [2]RECT
	settingsCategory                       int
	resourceTimelineModeRects              [2]RECT
	resourceTimelineTicksTrackRect         RECT
	resourceTimelineTicksKnobRect          RECT
	resourceTimelineTicksValueRect         RECT
	settingsSubpage                        int
	settingsContentTop                     int
	settingsScrollTrack                    RECT
	settingsScrollThumb                    RECT
	settingsScrollPx                       float64
	settingsScrollTarget                   float64
	settingsScrollMax                      float64
	themeRects                             [3]RECT
	backgroundRects                        [6]RECT
	surfaceRects                           [5]RECT
	animationRects                         [3]RECT
	notificationsRect                      RECT
	recurrenceKindRects                    [3]RECT
	recurrenceDayRects                     [7]RECT
	recurrenceEnabledRect                  RECT
	scenarioRect                           RECT
	taskKindRects                          [3]RECT
	scenarioBackRect                       RECT
	triggerLogicRect                       RECT
	blockWhenRect                          RECT
	blockActionRect                        RECT
	blockActionChoiceRects                 [5]RECT
	confirmDiscardScenario                 bool
	confirmDiscardRect                     RECT
	confirmDiscardYesRect                  RECT
	confirmDiscardNoRect                   RECT
	blockProcessesRect                     RECT
	conditionRows                          [10]RECT
	conditionLogicRects                    [10]RECT
	conditionDeleteRects                   [10]RECT
	conditionUpRects                       [10]RECT
	conditionDownRects                     [10]RECT
	addConditionRect                       RECT
	addConditionGroupRect                  RECT
	stepRows                               [10]RECT
	stepDeleteRects                        [10]RECT
	stepUpRects                            [10]RECT
	stepDownRects                          [10]RECT
	addStepRect                            RECT
	dryRunRect                             RECT
	diagnosticsRect                        RECT
	conditionDragRects                     [10]RECT
	conditionCollapseRects                 [10]RECT
	stepDragRects                          [10]RECT
	draggingScenarioKind                   int
	draggingScenarioIndex                  int
	draggingScenarioTarget                 int
	draggingScenarioParentID               string
	draggingScenarioIntoGroup              bool
	draggingScenarioY                      int32
	dragGapAnim                            float64
	scenarioScrollPx                       float64
	scenarioScrollTarget                   float64
	scenarioListClip                       RECT
	scenarioScrollTrack                    RECT
	scenarioScrollThumb                    RECT
	scenarioFirst                          int
	scenarioVisible                        int
	conditionRowIndices                    [10]int
	stepRowIndices                         [10]int
	editorTypeRects                        [21]RECT
	editorCompareRects                     [2]RECT
	editorSaveRect                         RECT
	editorCancelRect                       RECT
	editorBrowseRect                       RECT
	conditionOpenGroupRect                 RECT
	conditionCloseGroupRect                RECT
	conditionDelayFieldRect                RECT
	conditionMoreRect                      RECT
	conditionCatalogExpanded               bool
	conditionCatalogAnim                   float64
	conditionCatalogTarget                 float64
	conditionCatalogFrom                   float64
	conditionCatalogStarted                time.Time
	conditionCatalogAnimating              bool
	conditionCatalogBaseMoreY              int32
	conditionCatalogExtraFullH             int32
	processClearRect                       RECT
	editorClearRect                        RECT
	savedEditClearRect                     RECT
	stepErrorRects                         [3]RECT
	powerPlanRects                         [3]RECT
	stepDelayFieldRect                     RECT
	stepRetryFieldRect                     RECT
	editingCondition                       int
	editingStep                            int
	conditionDraft                         AutomationCondition
	stepDraft                              ActionStep
	stepTypeRects                          [11]RECT
	historyFilterRects                     [4]RECT
	historyFilter                          int
	dataRects                              [6]RECT
	appUpdateRect                          RECT
	appUpdateActionRect                    RECT
	temperatureAutoUpdateRect              RECT
	temperatureUpdateActionRect            RECT
	safetyFullscreenRect                   RECT
	safetyRecentRect                       RECT
	safetyProcessesRect                    RECT
	showSystemProcessesRect                RECT
	hideZeroResourceProcessesRect          RECT
	processFilterRects                     [3]RECT
	processFilter                          int
	confirmSystemMode                      int
	pendingSystemProcess                   string
	confirmSystemOverlayRect               RECT
	confirmSystemYesRect                   RECT
	confirmSystemNoRect                    RECT
	confirmSystemUnlockAt                  time.Time
	processPickerMode                      int
	processReturnSection                   int
	lastAutoThemeLight                     bool
	lastAutoThemeCheck                     time.Time
	historyRows                            [12]RECT
	historyPrevRect                        RECT
	historyNextRect                        RECT
	historyScrollTrack                     RECT
	historyScrollThumb                     RECT
	historyClearRect                       RECT
	historySearchRect                      RECT
	historySelected                        int
	historyDetailBackRect                  RECT
	historyDetailOpen                      bool
	historyDetailItem                      HistoryItem
	historyDetailRows                      [12]RECT
	historyDetailScrollTrack               RECT
	historyDetailScrollThumb               RECT
	historyDetailListClip                  RECT
	historyDetailScrollPx                  float64
	historyDetailScrollTarget              float64
	historyDetailVisible                   int
	confirmClearHistory                    bool
	confirmClearOverlayRect                RECT
	confirmClearYesRect                    RECT
	confirmClearNoRect                     RECT
	historyScroll                          int
	historyScrollPx                        float64
	historyScrollTarget                    float64
	historyVisible                         int
	historyListClip                        RECT
	historyItems                           []HistoryItem
	historyFiltered                        []HistoryItem
	historyFilterCache                     int
	historyCacheValid                      bool
	draggingVolume                         bool
	draggingTimelineTicks                  bool
	draggingScrollKind                     int
	dragScrollGrabOffset                   float64
	lastPreviewVolume                      int
	processRows                            [16]RECT
	processPrevRect                        RECT
	processNextRect                        RECT
	processScrollTrack                     RECT
	processScrollThumb                     RECT
	processDoneRect                        RECT
	savedRows                              [12]RECT
	savedFavoriteRects                     [12]RECT
	savedRunRects                          [12]RECT
	savedPauseRects                        [12]RECT
	savedMenuButtonRects                   [12]RECT
	savedPrevRect                          RECT
	savedNextRect                          RECT
	savedScrollTrack                       RECT
	savedScrollThumb                       RECT
	savedScroll                            int
	savedScrollPx                          float64
	savedScrollTarget                      float64
	savedVisible                           int
	savedListClip                          RECT
	savedSearchRect                        RECT
	savedFilteredIndices                   []int
	savedSearchText                        string
	historySearchText                      string
	savedSearchPlaceholder                 bool
	historySearchPlaceholder               bool
	savedMenuOpenIdx                       int
	savedPopupRect                         RECT
	savedPopupPauseRect                    RECT
	savedPopupEditRect                     RECT
	savedPopupDuplicateRect                RECT
	savedPopupDeleteRect                   RECT
	editingSavedIdx                        int
	savedEditDraft                         SavedTask
	savedEditActionRects                   [5]RECT
	savedEditModeRects                     [6]RECT
	savedEditCloseRect                     RECT
	savedEditProcessRect                   RECT
	savedEditSaveRect                      RECT
	savedEditCancelRect                    RECT
	savedEditKindRects                     [2]RECT
	savedEditScenarioRect                  RECT
	savedEditWarningRect                   RECT
	savedScenarioNameRect                  RECT
	savedScenarioSaveRect                  RECT
	savedScenarioCancelRect                RECT
	savedScenarioCheckRect                 RECT
	scenarioSavedDraft                     bool
	scenarioReturnSection                  int
	checkReturnSection                     int
	blockEditorBackRect                    RECT
	blockEditorDoneRect                    RECT
	confirmDeleteIdx                       int
	confirmOverlayRect                     RECT
	confirmDeleteYesRect                   RECT
	confirmDeleteNoRect                    RECT
	section                                int
	lastTaskSection                        int
	taskMenuOpen                           bool
	resourceMenuOpen                       bool
	createTaskMenuOpen                     bool
	currentTaskSection                     int
	currentTaskKind                        int
	settingsWnd                            uintptr
	appIcon                                uintptr
	settingsIcon                           uintptr
	processScroll                          int
	processScrollPx                        float64
	processScrollTarget                    float64
	processVisible                         int
	processListClip                        RECT
	processPicker                          uintptr
	processList                            uintptr
	pickerItems                            []string
	pickerAll                              []ProcessInfo
	pickerSystem                           map[string]bool
	undoRect                               RECT
	redoRect                               RECT
	previewRect                            RECT
	templatesRect                          RECT
	pasteConditionRect                     RECT
	pasteStepRect                          RECT
	copyConditionsGroupRect                RECT
	copyStepsGroupRect                     RECT
	conditionCopyRects                     [10]RECT
	conditionDuplicateRects                [10]RECT
	stepCopyRects                          [10]RECT
	stepDuplicateRects                     [10]RECT
	selectedScenarioKind                   int
	selectedScenarioIndex                  int
	templateRects                          [6]RECT
	templateBackRect                       RECT
	previewBackRect                        RECT
	previewRows                            [18]RECT
	diagnosticNextRect                     RECT
	diagnosticRestartRect                  RECT
	diagnosticRunRect                      RECT
	dryRunStep                             int
	miniAlwaysTopRect                      RECT
	miniOptionRects                        [4]RECT
	miniSizeRects                          [3]RECT
	uiScaleRects                           [4]RECT
	resourceCardRects                      [6]RECT
	resourceRefreshRects                   [5]RECT
	settingsResourceRefreshRects           [5]RECT
	resourceGraphRect                      RECT
	resourceDiskRects                      []RECT
	resourceDiskSelected                   int
	resourceSelected                       int
	resourceProcRows                       [18]RECT
	resourceProcSortRects                  [6]RECT
	resourceProcessSearchRect              RECT
	resourceProcessSearchText              string
	resourceProcessSearchPlaceholder       bool
	resourceAdvancedTabRects               [2]RECT
	resourceAdvancedView                   int
	resourceSensorTypeRects                [8]RECT
	resourceSensorView                     int
	resourceSensorExpanded                 map[string]bool
	temperatureUpdateLastCheck             time.Time
	resourceTempProviderRect               RECT
	resourceTempAdminRect                  RECT
	resourceProcScrollTrack                RECT
	resourceProcScrollThumb                RECT
	resourceProcScrollPx                   float64
	resourceProcScrollTarget               float64
	resourceProcListClip                   RECT
	resourceProcSort                       int
	resourceProcSortDesc                   bool
	resourceStatsPeriodRects               [8]RECT
	resourceStatsPeriod                    int
	resourceStatsViewRects                 [5]RECT
	resourceStatsView                      int
	resourceStatsGraphRects                [6]RECT
	resourceStatsGraphMode                 int
	resourceStatsSortRects                 [6]RECT
	resourceStatsSort                      int
	resourceStatsSortDesc                  bool
	conditionGroupCollapsed                map[string]bool
	graphCanvasRect                        RECT
	graphPaletteRects                      [5]RECT
	graphZoomRects                         [3]RECT
	graphNodeHits                          []GraphNodeHit
	graphPortHits                          []GraphPortHit
	graphFunctionHits                      []GraphFunctionHit
	graphSelectedNodeID                    string
	graphConnectingNodeID                  string
	graphConnectingPort                    string
	graphDragging                          bool
	graphLastMouseX                        int32
	graphLastMouseY                        int32
	graphValidation                        []GraphValidationIssue
	graphWindow                            uintptr
	graphDetachRect                        RECT
	graphTitleBarRect                      RECT
	graphTitleMinRect                      RECT
	graphTitleMaxRect                      RECT
	graphTitleCloseRect                    RECT
	graphTitleHover                        int
	graphWindowSizeRects                   [3]RECT
	graphWindowWidthRect                   RECT
	graphWindowHeightRect                  RECT
	graphWindowLockRect                    RECT
	graphSelectedNodes                     map[string]bool
	graphSelectedEdgeID                    string
	graphMarquee                           bool
	graphMarqueeAdditive                   bool
	graphMarqueeStartX                     int32
	graphMarqueeStartY                     int32
	graphMarqueeX                          int32
	graphMarqueeY                          int32
	graphRightDown                         bool
	graphRightPanning                      bool
	graphRightStartX                       int32
	graphRightStartY                       int32
	graphMiddleDown                        bool
	graphMiddlePanning                     bool
	graphMiddleStartX                      int32
	graphMiddleStartY                      int32
	graphContextOpen                       bool
	graphContextRect                       RECT
	graphContextItemRects                  []RECT
	graphContextX                          int32
	graphContextY                          int32
	graphEditorOpen                        bool
	graphEditorNodeID                      string
	graphEditorItem                        int
	graphEditorSection                     int
	graphEditorLegacyBody                  RECT
	graphEditorDX                          int32
	graphEditorDY                          int32
	graphEditorRect                        RECT
	graphEditorCloseRect                   RECT
	graphEditorActionRects                 [10]RECT
	graphEditorText                        uintptr
	mu                                     sync.Mutex
	exiting                                bool
	pageAnim                               float64
	pageAnimStarted                        time.Time
	pageAnimShownInputs                    bool
	subRevealAnim                          float64
	savedMenuAnim                          float64
	savedMenuTarget                        float64
	savedMenuPendingClose                  bool
	diagnosticLines                        []DiagnosticLine
	diagnosticTitle                        string
	diagnosticBackRect                     RECT
	diagnosticRefreshRect                  RECT
	diagnosticMode                         int // 1 dry run, 2 live diagnostics
	diagnosticLastRefresh                  time.Time
}

type SettingsView struct {
	hwnd      uintptr
	autoRect  RECT
	trayRect  RECT
	closeRect RECT
}

var app App
var settingsView SettingsView
var fontCache = map[int]uintptr{}
var controlBrush uintptr
var editAnimOffsetX, editAnimOffsetY int
var suppressEditVisibilityDuringLayout bool
var drawingInteractiveSurface bool
var drawingTaskNavigationMenu bool
var drawingNotificationPanel bool

func rgb(r, g, b byte) uint32 { return uint32(r) | uint32(g)<<8 | uint32(b)<<16 }
func wstr(s string) *uint16   { p, _ := syscall.UTF16PtrFromString(s); return p }
func loword(v uintptr) int    { return int(v & 0xFFFF) }
func hiword(v uintptr) int    { return int((v >> 16) & 0xFFFF) }

func main() {
	if handleTemperatureAdminCommand() {
		return
	}
	runtime.LockOSThread()
	pSetProcessDPIAware.Call()
	if !acquireSingleInstance() {
		return
	}
	app.edits = map[int]uintptr{}
	app.resourceSensorExpanded = map[string]bool{}
	app.confirmDeleteIdx = -1
	app.historySelected = -1
	app.savedMenuOpenIdx = -1
	app.editingSavedIdx = -1
	app.editingCondition = -1
	app.editingStep = -1
	app.pageAnim = 1
	app.savedMenuAnim = 0
	app.savedMenuTarget = 0
	app.buttons = map[int]uintptr{}
	existed := fileExists(settingsPath())
	app.settings = loadSettings()
	if existed && !app.settings.IdleSecondsMigrated {
		if app.settings.IdleMinutes > 0 {
			app.settings.IdleMinutes *= 60
		}
		for i := range app.settings.SavedTasks {
			if app.settings.SavedTasks[i].Mode == 2 && app.settings.SavedTasks[i].IdleMinutes > 0 {
				app.settings.SavedTasks[i].IdleMinutes *= 60
			}
		}
		app.settings.IdleSecondsMigrated = true
		saveSettings()
	}
	if !existed {
		app.settings.IdleSecondsMigrated = true
		app.settings.MinimizeToTray = true
		app.settings.CloseBefore = true
		app.settings.Sounds = true
		app.settings.Notifications = true
		app.settings.SoundVolume = 65
		app.settings.DelayMinutes = 30
		app.settings.Recurrence.TimeHHMM = "23:00"
		app.settings.Recurrence.Enabled = true
		app.settings.SafetyIdleMinutes = 5
		app.settings.WakeLeadMinutes = 1
		app.settings.TemperatureAutoUpdate = true
	}
	normalizeSettings()
	normalizeV040Settings()
	startupRecovery040()
	normalizeSettings()
	normalizeV040Settings()
	markRunning040()
	// Every new PowerPilot session starts the current task's "Дата и время"
	// from the actual launch moment. It stays fixed for the lifetime of this
	// process until the user edits it, instead of carrying yesterday's value.
	app.settings.Exact = time.Now().Format("02.01.2006 15:04")
	if app.settings.WarningSeconds == 0 {
		app.settings.WarningSeconds = 60
	}
	applyTheme()
	app.lastAutoThemeLight = systemUsesLightTheme()
	loadEmbeddedIcons()
	loadNotificationCenter()
	app.selectedAction = app.settings.Action
	app.selectedMode = app.settings.Mode
	// startupRecovery040() restores currentTaskKind/currentTaskSection from draft.json.
	// Do not overwrite that recovered editor location with the generic settings value.
	// On a first launch (no draft), fall back to settings.TaskKind as before.
	if _, ok := loadDraftAutosave(); !ok {
		app.currentTaskKind = app.settings.TaskKind
	}
	app.settings.TaskKind = app.currentTaskKind
	if app.currentTaskKind == 1 {
		app.currentTaskSection = 7
	} else if app.currentTaskSection < 0 || app.currentTaskSection > 2 {
		app.currentTaskSection = 0
	}
	if app.selectedAction >= 0 && app.selectedAction < len(app.actionAnim) {
		app.actionAnim[app.selectedAction] = 1
	}
	if app.selectedMode >= 0 && app.selectedMode < len(app.modeAnim) {
		app.modeAnim[app.selectedMode] = 1
	}
	initUndo040()
	runUI()
}

func runUI() {
	hinst, _, _ := pGetModuleHandleW.Call(0)
	cls := wstr("PowerPilotNativeWindow")
	cursor, _, _ := pLoadCursorW.Call(0, IDC_ARROW)
	icon := app.appIcon
	if icon == 0 {
		icon, _, _ = pLoadIconW.Call(0, IDI_APPLICATION)
	}
	wc := WNDCLASSEX{CbSize: uint32(unsafe.Sizeof(WNDCLASSEX{})), Style: 0x000B, LpfnWndProc: syscall.NewCallback(wndProc), HInstance: hinst, HIcon: icon, HCursor: cursor, LpszClassName: cls, HIconSm: icon}
	if r, _, _ := pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
		panic("RegisterClassEx failed")
	}
	title := wstr("PowerPilot")
	windowStyle := uintptr(WS_POPUP | WS_CAPTION | WS_THICKFRAME | WS_SYSMENU | WS_MINIMIZEBOX | WS_MAXIMIZEBOX | WS_CLIPCHILDREN | WS_CLIPSIBLINGS)
	minW040, minH040 := normalMinPhysical040()
	initialW, initialH := int(minW040), int(minH040)
	if app.settings.LockCurrentSize {
		initialW = max(initialW, app.settings.LockedWindowW)
		initialH = max(initialH, app.settings.LockedWindowH)
	}
	hwnd, _, _ := pCreateWindowExW.Call(WS_EX_APPWINDOW, uintptr(unsafe.Pointer(cls)), uintptr(unsafe.Pointer(title)), windowStyle, 120, 80, uintptr(initialW), uintptr(initialH), 0, 0, hinst, 0)
	if hwnd == 0 {
		panic("CreateWindowEx failed")
	}
	app.hwnd = hwnd
	applyRoundedWindowCorners(hwnd)
	maintainWakeTimer(time.Now())
	if app.appIcon != 0 {
		pSendMessageW.Call(hwnd, WM_SETICON, 1, app.appIcon)
		pSendMessageW.Call(hwnd, WM_SETICON, 0, app.appIcon)
	}
	if app.settings.AnimationMode != 2 {
		oldEx := prepareWindowOpacity(hwnd, 0)
		pShowWindow.Call(hwnd, SW_SHOW)
		pUpdateWindow.Call(hwnd)
		d := time.Duration(animationWindowDuration()+25) * time.Millisecond
		animateWindowOpacity(hwnd, 0, 255, d)
		restoreWindowOpacityStyle(hwnd, oldEx)
	} else {
		pShowWindow.Call(hwnd, SW_SHOW)
		pUpdateWindow.Call(hwnd)
	}
	addTrayIcon()
	applyGlobalHotkeys()
	var msg MSG
	for {
		r, _, _ := pGetMessageW.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if int32(r) <= 0 {
			break
		}
		pTranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
		pDispatchMessageW.Call(uintptr(unsafe.Pointer(&msg)))
	}
}

func wndProc(hwnd uintptr, msg uint32, wParam, lParam uintptr) uintptr {
	switch msg {
	case WM_CREATE:
		controlBrush = solid(surfaceButtonColor())
		createControls(hwnd)
		layoutControls(hwnd)
		startMetricSampler()
		if temperatureProviderInstalled() {
			app.temperatureUpdateLastCheck = time.Now()
			checkTemperatureProviderUpdatesAsync(false)
		}
		checkPowerPilotUpdatesAsync(false)
		pSetTimer.Call(hwnd, 1, 250, 0)
		pSetTimer.Call(hwnd, 2, 10, 0)
		return 0
	case WM_SIZE:
		if enforceMinimumClientSize(hwnd) {
			return 0
		}
		applyRoundedWindowCorners(hwnd)
		layoutControls(hwnd)
		invalidate(hwnd)
		return 0
	case WM_GETMINMAXINFO:
		mmi := (*MINMAXINFO)(unsafe.Pointer(lParam))
		if app.miniMode {
			mw, mh := miniClientSize040()
			mmi.PtMinTrackSize = POINT{mw, mh}
			mmi.PtMaxTrackSize = POINT{mw, mh}
		} else {
			if app.settings.LockMinimumSize {
				mw, mh := normalMinPhysical040()
				mmi.PtMinTrackSize = POINT{mw, mh}
				mmi.PtMaxTrackSize = POINT{mw, mh}
			} else if app.settings.LockCurrentSize {
				mw, mh := normalMinPhysical040()
				lw := max32(mw, int32(app.settings.LockedWindowW))
				lh := max32(mh, int32(app.settings.LockedWindowH))
				mmi.PtMinTrackSize = POINT{lw, lh}
				mmi.PtMaxTrackSize = POINT{lw, lh}
			} else {
				mw, mh := normalMinPhysical040()
				mmi.PtMinTrackSize = POINT{mw, mh}
			}
			// Borderless windows otherwise tend to maximize under the taskbar.
			mon, _, _ := pMonitorFromWindow.Call(hwnd, MONITOR_DEFAULTTONEAREST)
			if mon != 0 {
				mi := MONITORINFO{CbSize: uint32(unsafe.Sizeof(MONITORINFO{}))}
				if ok, _, _ := pGetMonitorInfoW.Call(mon, uintptr(unsafe.Pointer(&mi))); ok != 0 {
					mmi.PtMaxPosition = POINT{mi.RcWork.Left - mi.RcMonitor.Left, mi.RcWork.Top - mi.RcMonitor.Top}
					if !app.settings.LockMinimumSize && !app.settings.LockCurrentSize {
						mmi.PtMaxSize = POINT{mi.RcWork.Right - mi.RcWork.Left, mi.RcWork.Bottom - mi.RcWork.Top}
					}
				}
			}
		}
		return 0
	case WM_NCCALCSIZE:
		// The whole window is client area. This removes the native Windows caption completely.
		return 0
	case WM_NCACTIVATE:
		// Prevent DefWindowProc from painting a native active/inactive caption frame.
		return 1
	case WM_NCHITTEST:
		return hitTestWindow(hwnd, lParam)
	case WM_PAINT:
		paint(hwnd)
		return 0
	case WM_ERASEBKGND:
		return 1
	case WM_MOUSEMOVE:
		x, y := clientPointToLogical040(int32(int16(loword(lParam))), int32(int16(hiword(lParam))))
		onMouseMove(hwnd, x, y)
		return 0
	case WM_MOUSEWHEEL:
		delta := int16((wParam >> 16) & 0xFFFF)
		if app.section == 7 || app.section == 13 {
			zoomScenarioGraph(delta)
			return 0
		}
		queueSmoothScroll(delta)
		return 0
	case WM_MOUSELEAVE:
		app.hoverAction = -1
		app.hoverMode = -1
		app.hoverTitle = -1
		app.hoverSeen = false
		app.mouseX, app.mouseY = -10000, -10000
		app.mouseTracking = false
		app.notificationBellHover = false
		app.tooltipRect = RECT{}
		app.tooltipText = ""
		app.tooltipSince = time.Time{}
		invalidate(hwnd)
		return 0
	case WM_LBUTTONDOWN:
		x, y := clientPointToLogical040(int32(int16(loword(lParam))), int32(int16(hiword(lParam))))
		clearEditFocusForCanvasClick(x, y)
		onClick(x, y)
		return 0
	case WM_LBUTTONDBLCLK:
		x, y := clientPointToLogical040(int32(int16(loword(lParam))), int32(int16(hiword(lParam))))
		if handleGraphDoubleClick(x, y) {
			return 0
		}
	case WM_RBUTTONDOWN:
		if app.section == 7 || app.section == 13 {
			x, y := clientPointToLogical040(int32(int16(loword(lParam))), int32(int16(hiword(lParam))))
			beginGraphRightButton(x, y)
			return 0
		}
	case WM_RBUTTONUP:
		if app.section == 7 || app.section == 13 {
			x, y := clientPointToLogical040(int32(int16(loword(lParam))), int32(int16(hiword(lParam))))
			finishGraphRightButton(x, y)
			return 0
		}
	case WM_MBUTTONDOWN:
		if app.section == 7 || app.section == 13 {
			x, y := clientPointToLogical040(int32(int16(loword(lParam))), int32(int16(hiword(lParam))))
			if pointIn(app.graphCanvasRect, x, y) {
				beginGraphMiddleButton(x, y)
				return 0
			}
		}
	case WM_MBUTTONUP:
		if finishGraphMiddleButton() {
			return 0
		}
	case WM_LBUTTONUP:
		if finishScenarioGraphPointer() {
			return 0
		}
		if app.draggingScenarioKind != 0 {
			finishScenarioDrag()
			pReleaseCapture.Call()
		}
		if app.draggingScrollKind != 0 {
			app.draggingScrollKind = 0
			pReleaseCapture.Call()
		}
		if app.draggingVolume {
			app.draggingVolume = false
			pReleaseCapture.Call()
			saveSettings()
		}
		if app.draggingTimelineTicks {
			app.draggingTimelineTicks = false
			pReleaseCapture.Call()
			saveSettings()
		}
		return 0
	case WM_INPUTLANGCHANGE:
		// Windows can transiently move focus away from a child EDIT while switching
		// keyboard layouts (Alt+Shift / Win+Space). Search should remain type-ready.
		focus, _, _ := pGetFocus.Call()
		keep := focus == app.edits[idSavedSearch] || focus == app.edits[idHistorySearch] || focus == app.edits[idResourceSearch]
		r, _, _ := pDefWindowProcW.Call(hwnd, uintptr(msg), wParam, lParam)
		if keep && focus != 0 {
			pSetFocus.Call(focus)
		}
		return r
	case WM_COMMAND:
		onCommand(loword(wParam), hiword(wParam), lParam)
		return 0
	case WM_KEYDOWN:
		if handleKeyDown040(wParam) {
			return 0
		}
	case WM_HOTKEY:
		switch int(wParam) {
		case 1:
			showMain()
		case 2:
			if app.schedule.active {
				cancelSchedule(true)
			}
		}
		return 0
	case WM_TIMER:
		if wParam == 1 {
			tick()
			maybeCheckPowerPilotUpdates()
			if temperatureProviderInstalled() && (app.temperatureUpdateLastCheck.IsZero() || time.Since(app.temperatureUpdateLastCheck) >= 30*time.Minute) {
				temperatureProviderState.RLock()
				busy := temperatureProviderState.Checking || temperatureProviderState.Installing
				temperatureProviderState.RUnlock()
				if !busy {
					app.temperatureUpdateLastCheck = time.Now()
					checkTemperatureProviderUpdatesAsync(false)
				}
			}
			if app.section == 11 && app.diagnosticMode == 2 && time.Since(app.diagnosticLastRefresh) >= 750*time.Millisecond {
				app.diagnosticLines = buildDiagnosticReport(false)
				app.diagnosticLastRefresh = time.Now()
				invalidate(hwnd)
			}
		} else if wParam == 2 {
			animate()
		}
		return 0
	case WM_CTLCOLOREDIT:
		hdc := wParam
		pSetBkMode.Call(hdc, TRANSPARENT)
		color := inputTextRevealColor()
		if (lParam == app.edits[idSavedSearch] && app.savedSearchPlaceholder) ||
			(lParam == app.edits[idHistorySearch] && app.historySearchPlaceholder) ||
			(lParam == app.edits[idResourceSearch] && app.resourceProcessSearchPlaceholder) {
			color = theme.muted
		}
		pSetTextColor.Call(hdc, uintptr(color))
		return controlBrush
	case WM_CTLCOLORSTATIC, WM_CTLCOLORBTN:
		hdc := wParam
		pSetBkMode.Call(hdc, TRANSPARENT)
		pSetTextColor.Call(hdc, uintptr(theme.text))
		return controlBrush
	case WM_RESOURCE_UPDATED:
		if app.section == 18 || app.section == 19 || app.section == 20 {
			if app.section == 19 {
				updateScrollGeometry()
			}
			invalidate(hwnd)
		}
		return 0
	case WM_HISTORY_CHANGED:
		loadHistoryItems()
		if app.section == 3 && (app.settingsSubpage == 2 || app.settingsSubpage == 3) {
			layoutControls(hwnd)
			invalidate(hwnd)
		}
		return 0
	case WM_NOTIFICATION_CHANGED:
		unread := notificationUnreadCount()
		if unread > app.notificationBellLastUnread && app.settings.AnimationMode != 2 {
			app.notificationBellBurstStarted = time.Now()
		}
		app.notificationBellLastUnread = unread
		if app.notificationPanelOpen {
			layoutControls(hwnd)
		}
		invalidate(hwnd)
		return 0
	case WM_TRAY:
		if lParam == WM_LBUTTONDBLCLK {
			showMain()
		} else if lParam == WM_RBUTTONUP {
			showTrayMenu()
		}
		return 0
	case WM_CLOSE:
		if app.settings.MinimizeToTray && !app.exiting {
			if app.settings.AnimationMode != 2 {
				pAnimateWindow.Call(hwnd, 110, AW_BLEND|AW_HIDE)
			} else {
				pShowWindow.Call(hwnd, SW_HIDE)
			}
			return 0
		}
		app.exiting = true
		saveDraftAutosave()
		saveSettings()
		markGracefulExit040()
		removeTrayIcon()
		if app.settings.AnimationMode != 2 {
			pAnimateWindow.Call(hwnd, 120, AW_BLEND|AW_HIDE)
		}
		pDestroyWindow.Call(hwnd)
		return 0
	case WM_DESTROY:
		if app.graphWindow != 0 {
			if app.graphEditorOpen {
				syncGraphCompactText()
				persistCurrentScenarioGraph()
			}
			graphWindow := app.graphWindow
			app.graphWindow = 0
			pDestroyWindow.Call(graphWindow)
		}
		pKillTimer.Call(hwnd, 1)
		pKillTimer.Call(hwnd, 2)
		stopMetricSampler()
		closeWakeTimer()
		for _, f := range fontCache {
			if f != 0 {
				pDeleteObject.Call(f)
			}
		}
		fontCache = map[int]uintptr{}
		if controlBrush != 0 {
			pDeleteObject.Call(controlBrush)
			controlBrush = 0
		}
		removeTrayIcon()
		pUnregisterHotKey.Call(hwnd, 1)
		pUnregisterHotKey.Call(hwnd, 2)
		d2dReleaseAll()
		releaseSingleInstance()
		pPostQuitMessage.Call(0)
		return 0
	}
	r, _, _ := pDefWindowProcW.Call(hwnd, uintptr(msg), wParam, lParam)
	return r
}

func createControls(hwnd uintptr) {
	sc040 := uiScaleFactor040()
	app.font = createFont(max(9, int(15*sc040+.5)), 400)
	app.fontSmall = createFont(max(8, int(13*sc040+.5)), 400)
	app.fontLarge = createFont(max(12, int(20*sc040+.5)), 600)
	app.inlineFont = createFont(max(8, int(12*sc040+.5)), 600)
	edit := func(id int, text string, numeric bool) uintptr {
		style := uintptr(WS_CHILD | WS_TABSTOP | ES_CENTER)
		if id == idSavedSearch || id == idHistorySearch || id == idResourceSearch {
			// Search is free-form text: align the caret and typed text to the left.
			style = uintptr(WS_CHILD | WS_TABSTOP | ES_LEFT)
		}
		if numeric {
			style |= ES_NUMBER
		}
		h, _, _ := pCreateWindowExW.Call(0, uintptr(unsafe.Pointer(wstr("EDIT"))), uintptr(unsafe.Pointer(wstr(text))), style, 0, 0, 80, 34, hwnd, uintptr(id), 0, 0)
		inputFont := app.font
		if numeric {
			inputFont = app.fontSmall
		}
		pSendMessageW.Call(h, WM_SETFONT, inputFont, 1)
		app.edits[id] = h
		return h
	}
	edit(idDelayHours, strconv.Itoa(app.settings.DelayHours), true)
	edit(idDelayMinutes, strconv.Itoa(app.settings.DelayMinutes), true)
	edit(idDelaySeconds, strconv.Itoa(app.settings.DelaySeconds), true)
	edit(idExact, app.settings.Exact, false) // compatibility shadow; split fields are shown to the user.
	exactParts := splitExactParts(app.settings.Exact)
	edit(idExactDay, exactParts[0], true)
	edit(idExactMonth, exactParts[1], true)
	edit(idExactYear, exactParts[2], true)
	edit(idExactHour, exactParts[3], true)
	edit(idExactMinute, exactParts[4], true)
	for _, pair := range []struct{ id, limit int }{{idExactDay, 2}, {idExactMonth, 2}, {idExactYear, 4}, {idExactHour, 2}, {idExactMinute, 2}} {
		pSendMessageW.Call(app.edits[pair.id], EM_SETLIMITTEXT, uintptr(pair.limit), 0)
	}
	edit(idIdleMinutes, strconv.Itoa(max(app.settings.IdleMinutes, 30)), true)
	edit(idWatchProcess, app.settings.WatchProcess, false)
	pSendMessageW.Call(app.edits[idWatchProcess], EM_SETCUEBANNER, 1, uintptr(unsafe.Pointer(wstr("Введите имя процесса…"))))
	edit(idWarning, strconv.Itoa(max(app.settings.WarningSeconds, 60)), true)
	edit(idScheduleTime, app.settings.Recurrence.TimeHHMM, false)
	edit(idCondThreshold, "10", false)
	edit(idCondHold, "30", true)
	edit(idCondText, "", false)
	edit(idStepValue, "10", true)
	edit(idStepText, "", false)
	pSendMessageW.Call(app.edits[idCondText], EM_SETCUEBANNER, 1, uintptr(unsafe.Pointer(wstr("Введите значение или выберите…"))))
	pSendMessageW.Call(app.edits[idStepText], EM_SETCUEBANNER, 1, uintptr(unsafe.Pointer(wstr("Введите параметр…"))))
	edit(idSafetyIdle, strconv.Itoa(max(app.settings.SafetyIdleMinutes, 5)), true)
	edit(idSoundVolume, strconv.Itoa(clampInt(app.settings.SoundVolume, 0, 100)), true)
	pSendMessageW.Call(app.edits[idSoundVolume], EM_SETLIMITTEXT, 3, 0)
	// Keep the search hint as actual edit text. The native Win32 cue banner is
	// barely visible with custom dark edit colours on some Windows builds.
	app.savedSearchPlaceholder = true
	app.historySearchPlaceholder = true
	app.resourceProcessSearchPlaceholder = true
	edit(idSavedSearch, "Поиск по сохранённым задачам", false)
	edit(idHistorySearch, "Поиск по истории", false)
	edit(idResourceSearch, "Поиск по процессам", false)
	edit(idTimelineTicks, strconv.Itoa(resourceTimelineTickCount()), true)
	pSendMessageW.Call(app.edits[idTimelineTicks], EM_SETLIMITTEXT, 2, 0)
	edit(idGraphWidth, strconv.Itoa(graphWindowWidth()), true)
	edit(idGraphHeight, strconv.Itoa(graphWindowHeight()), true)
	pSendMessageW.Call(app.edits[idGraphWidth], EM_SETLIMITTEXT, 4, 0)
	pSendMessageW.Call(app.edits[idGraphHeight], EM_SETLIMITTEXT, 4, 0)
	edit(idStepRetries, "2", true)
	edit(idStepDelay, "0", true)
	edit(idWakeLead, strconv.Itoa(max(app.settings.WakeLeadMinutes, 1)), true)
	// Inline numeric settings are part of the sentence, so keep their typography identical.
	for _, id := range []int{idSafetyIdle, idWakeLead, idWarning, idIdleMinutes} {
		pSendMessageW.Call(app.edits[id], WM_SETFONT, app.inlineFont, 1)
	}
	edit(idCondDelay, "0", true)
	// Task names use a left-aligned field because they are free-form text.
	hName, _, _ := pCreateWindowExW.Call(0, uintptr(unsafe.Pointer(wstr("EDIT"))), uintptr(unsafe.Pointer(wstr(""))), WS_CHILD|WS_TABSTOP|ES_LEFT, 0, 0, 240, 36, hwnd, uintptr(idTaskName), 0, 0)
	pSendMessageW.Call(hName, WM_SETFONT, app.font, 1)
	app.edits[idTaskName] = hName
	// Open the draft the user was actually editing before the previous exit.
	// Advanced drafts return directly to the block scheme; simple drafts return
	// to their last Action/When/Additional page.
	if app.currentTaskKind == 1 {
		app.section = 7
		app.lastTaskSection = 2
		app.currentTaskSection = 7
	} else {
		sec := app.currentTaskSection
		if sec < 0 || sec > 2 {
			sec = 0
		}
		app.section = sec
		app.lastTaskSection = sec
		app.currentTaskSection = sec
	}
	updateInputVisibility()
}

func layoutControls(hwnd uintptr) {
	var rc RECT
	pGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&rc)))
	rc = logicalClientRect040(rc)
	w, h := int(rc.Right), int(rc.Bottom)
	pad, gap := 20, 10
	contentW := max(1, w-pad*2)
	innerPad := 18
	innerLeft := pad + innerPad
	innerRight := w - pad - innerPad
	innerContentW := max(1, innerRight-innerLeft)
	editAnimOffsetX, editAnimOffsetY = currentEditAnimationOffset()

	if !suppressEditVisibilityDuringLayout {
		for _, hnd := range app.edits {
			pShowWindow.Call(hnd, SW_HIDE)
		}
	}

	app.titleBarRect = RECT{0, 0, rc.Right, 46}
	btnW := 46
	app.closeBtnRect = RECT{int32(w - btnW), 0, int32(w), 46}
	app.maxBtnRect = RECT{int32(w - btnW*2), 0, int32(w - btnW), 46}
	app.minBtnRect = RECT{int32(w - btnW*3), 0, int32(w - btnW*2), 46}
	app.miniBtnRect = RECT{int32(w - btnW*4), 0, int32(w - btnW*3), 46}

	if app.miniMode {
		// Mini title-bar order: pin → exit mini mode → minimize → close.
		// Keep logical button identities intact and only swap their screen rectangles.
		app.minBtnRect, app.maxBtnRect = app.maxBtnRect, app.minBtnRect
		app.taskTabRect, app.taskMoreRect, app.monitorTabRect, app.resourceMoreRect, app.currentTaskMenuRect, app.blockTaskTabRect, app.savedTabRect, app.resourceAdvancedMenuRect, app.resourceStatsMenuRect, app.notificationBtnRect, app.settingsBtnRect = RECT{}, RECT{}, RECT{}, RECT{}, RECT{}, RECT{}, RECT{}, RECT{}, RECT{}, RECT{}, RECT{}
		for i := range app.chainRects {
			app.chainRects[i] = RECT{}
		}
		btnY := h - 50
		btnGap := 12
		btnW := (w - 40 - btnGap) / 2
		if btnW < 150 {
			btnW = 150
		}
		app.miniCancelRect = RECT{20, int32(btnY), int32(20 + btnW), int32(btnY + 38)}
		app.miniPostponeRect = RECT{int32(20 + btnW + btnGap), int32(btnY), int32(w - 20), int32(btnY + 38)}
		return
	}

	navY := 54
	// Split navigation buttons: the label and ⋮ are one visual control with
	// two hit zones. There is no gap between the two halves.
	app.taskTabRect = RECT{int32(pad), int32(navY), int32(pad + 104), int32(navY + 38)}
	app.taskMoreRect = RECT{int32(pad + 104), int32(navY), int32(pad + 142), int32(navY + 38)}
	resX := pad + 154
	app.monitorTabRect = RECT{int32(resX), int32(navY), int32(resX + 112), int32(navY + 38)}
	app.resourceMoreRect = RECT{int32(resX + 112), int32(navY), int32(resX + 150), int32(navY + 38)}
	app.currentTaskMenuRect, app.blockTaskTabRect, app.savedTabRect = RECT{}, RECT{}, RECT{}
	app.resourceAdvancedMenuRect, app.resourceStatsMenuRect = RECT{}, RECT{}
	for i := range app.taskKindRects {
		app.taskKindRects[i] = RECT{}
	}
	if app.taskMenuOpen {
		menuY := navY + 44
		menuH, menuGap := 32, 4
		mainW := max(142, max(uiTextWidth("Сохранённые задачи", 12, 600), uiTextWidth("Создать задачу", 12, 600))+22)
		subW := max(112, max(uiTextWidth("Из шаблона", 12, 600), max(uiTextWidth("Продвинутая", 12, 600), uiTextWidth("Простая", 12, 600)))+18)
		app.blockTaskTabRect = RECT{int32(pad), int32(menuY), int32(pad + mainW), int32(menuY + menuH)}
		savedY := menuY + menuH + menuGap
		app.savedTabRect = RECT{int32(pad), int32(savedY), int32(pad + mainW), int32(savedY + menuH)}
		if app.createTaskMenuOpen {
			subX := pad + mainW + 6
			app.taskKindRects[0] = RECT{int32(subX), int32(menuY), int32(subX + subW), int32(menuY + menuH)}
			app.taskKindRects[1] = RECT{int32(subX), int32(menuY + menuH + menuGap), int32(subX + subW), int32(menuY + menuH*2 + menuGap)}
			app.taskKindRects[2] = RECT{int32(subX), int32(menuY + (menuH+menuGap)*2), int32(subX + subW), int32(menuY + menuH*3 + menuGap*2)}
		}
	}
	if app.resourceMenuOpen {
		menuY := navY + 44
		menuH, menuGap := 32, 4
		menuW := max(176, max(uiTextWidth("Продвинутый монитор", 12, 600), uiTextWidth("Статистика за период", 12, 600))+22)
		app.resourceAdvancedMenuRect = RECT{int32(resX), int32(menuY), int32(resX + menuW), int32(menuY + menuH)}
		app.resourceStatsMenuRect = RECT{int32(resX), int32(menuY + menuH + menuGap), int32(resX + menuW), int32(menuY + menuH*2 + menuGap)}
	}
	app.settingsBtnRect = RECT{int32(w - 62), int32(navY - 1), int32(w - 20), int32(navY + 41)}
	app.notificationBtnRect = RECT{int32(w - 112), int32(navY - 1), int32(w - 70), int32(navY + 41)}
	for i := range app.notificationRows {
		app.notificationRows[i] = RECT{}
		app.notificationRowIndices[i] = -1
		app.notificationReadRects[i] = RECT{}
	}
	app.notificationPanelRect = RECT{}
	app.notificationMarkReadRect = RECT{}
	app.notificationClearRect = RECT{}
	app.notificationUnreadOnlyRect = RECT{}
	app.notificationListClip = RECT{}
	app.notificationScrollTrack, app.notificationScrollThumb = RECT{}, RECT{}
	if app.notificationPanelOpen {
		panelW := minInt(390, max(320, w-44))
		panelRight := int(app.settingsBtnRect.Right)
		panelLeft := panelRight - panelW
		if panelLeft < 18 {
			panelLeft = 18
			panelRight = panelLeft + panelW
		}
		panelTop := navY + 48
		panelH := 386
		if panelTop+panelH > h-18 {
			panelH = max(282, h-panelTop-18)
		}
		app.notificationPanelRect = RECT{int32(panelLeft), int32(panelTop), int32(panelRight), int32(panelTop + panelH)}
		headerY := panelTop + 48
		controlX := panelLeft + 16
		unreadW, controlGap, clearW := 104, 6, 34
		markW := max(72, panelRight-panelLeft-32-unreadW-clearW-controlGap*2)
		app.notificationUnreadOnlyRect = RECT{int32(controlX), int32(headerY), int32(controlX + unreadW), int32(headerY + 30)}
		controlX += unreadW + controlGap
		app.notificationMarkReadRect = RECT{int32(controlX), int32(headerY), int32(controlX + markW), int32(headerY + 30)}
		controlX += markW + controlGap
		app.notificationClearRect = RECT{int32(controlX), int32(headerY), int32(controlX + clearW), int32(headerY + 30)}
		listTop := headerY + 40
		rowH, rowGap := 50, 7
		stride := rowH + rowGap
		listBottom := panelTop + panelH - 16
		items := notificationItemsSnapshot(app.notificationUnreadOnly)
		viewH := max(1, listBottom-listTop)
		contentH := max(0, len(items)*stride-rowGap)
		app.notificationScrollMax = float64(max(0, contentH-viewH))
		app.notificationScrollPx = clampFloat(app.notificationScrollPx, 0, app.notificationScrollMax)
		app.notificationScrollTarget = clampFloat(app.notificationScrollTarget, 0, app.notificationScrollMax)
		first, rem := int(app.notificationScrollPx)/stride, int(app.notificationScrollPx)%stride
		rowRight := panelRight - 14
		if app.notificationScrollMax > 0 {
			rowRight -= 14
		}
		app.notificationListClip = RECT{int32(panelLeft + 14), int32(listTop), int32(rowRight), int32(listBottom)}
		app.notificationScrollTrack = RECT{int32(panelRight - 10), int32(listTop), int32(panelRight - 5), int32(listBottom)}
		app.notificationScrollThumb = scrollThumbRectPixels(app.notificationScrollTrack, contentH, viewH, app.notificationScrollPx)
		for slot := range app.notificationRows {
			idx := first + slot
			if idx >= len(items) {
				break
			}
			y := listTop - rem + slot*stride
			app.notificationRowIndices[slot] = idx
			app.notificationRows[slot] = RECT{int32(panelLeft + 14), int32(y), int32(rowRight), int32(y + rowH)}
			app.notificationReadRects[slot] = RECT{int32(rowRight - 32), int32(y + 11), int32(rowRight - 6), int32(y + 37)}
		}
	}

	showChain := showsTaskChain(app.section)
	if showChain {
		chainY := 110
		chainW := (contentW - gap*2) / 3
		for i := 0; i < 3; i++ {
			x := pad + i*(chainW+gap)
			app.chainRects[i] = RECT{int32(x), int32(chainY), int32(x + chainW), int32(chainY + 58)}
		}
	} else {
		for i := range app.chainRects {
			app.chainRects[i] = RECT{}
		}
	}

	bodyTop := 110
	if showChain {
		bodyTop = 184
	}
	bodyBottom := h - 20
	if hasTaskFooter(app.section) {
		bodyBottom = h - 154
	}
	if bodyBottom < bodyTop+250 {
		bodyBottom = bodyTop + 250
	}

	// Action cards.
	if app.section == 0 {
		cols := 4
		if w < 920 {
			cols = 2
		}
		cardGap, cardH := 12, 72
		cardW := (innerContentW - cardGap*(cols-1)) / cols
		for i := 0; i < 4; i++ {
			row, col := i/cols, i%cols
			x := innerLeft + col*(cardW+cardGap)
			y := bodyTop + 54 + row*(cardH+cardGap)
			app.actionRects[i] = RECT{int32(x), int32(y), int32(x + cardW), int32(y + cardH)}
		}
	}

	// "When" page: five modes including recurring schedule. Geometry comes from ui_system.go.
	if app.section == 1 {
		uiResetFieldRects()
		cols := 5
		if w < 1060 {
			cols = 3
		}
		cardGap, cardH := uiMetricsDefault.GapM, 48
		cardW := (innerContentW - cardGap*(cols-1)) / cols
		for i := 0; i < 5; i++ {
			row, col := i/cols, i%cols
			x := innerLeft + col*(cardW+cardGap)
			y := bodyTop + 54 + row*(cardH+cardGap)
			app.modeRects[i] = RECT{int32(x), int32(y), int32(x + cardW), int32(y + cardH)}
		}
		app.modeRects[5] = RECT{}
		uiLayoutWhenFields(app.selectedMode, app.modeRects[:5], innerLeft, innerRight, true)
	}

	if app.section == 2 {
		// This page belongs only to the Simple task. Task type is selected in the top navigation.
		// Do not clear taskKindRects here: they belong to the top navigation overlay.
		contentY := bodyTop + 70
		app.closeBeforeRect = RECT{int32(innerLeft), int32(contentY), int32(innerLeft + 28), int32(contentY + 28)}
		app.pickRect = RECT{int32(innerLeft), int32(contentY + 50), int32(minInt(innerRight, innerLeft+230)), int32(contentY + 90)}
		_, app.warningFieldRect, _ = uiInlineNumberLayout("Предупреждение за", "секунд", innerLeft, contentY+116, innerRight, 3)
		uiPlaceInlineNumberEdit(idWarning, app.warningFieldRect)
		app.scenarioRect = RECT{}
	}

	// Resources module.
	if app.section == 18 {
		layoutResourceMonitor(bodyTop, bodyBottom, innerLeft, innerRight)
	}
	if app.section == 19 {
		layoutAdvancedResourceMonitor(bodyTop, bodyBottom, innerLeft, innerRight)
	}
	if app.section == 20 {
		layoutResourceStatistics(bodyTop, bodyBottom, innerLeft, innerRight)
	}

	// Settings pages.
	if app.section == 3 {
		tabY := bodyTop + 58
		tabGap := 6
		tabW := (innerContentW - tabGap*6) / 7
		for i := range app.settingsTabs {
			x := innerLeft + i*(tabW+tabGap)
			app.settingsTabs[i] = RECT{int32(x), int32(tabY), int32(x + tabW), int32(tabY + 48)}
		}
		headerContentY := tabY + 60
		for i := range app.settingsSectionRects {
			app.settingsSectionRects[i] = RECT{}
		}
		if app.settingsCategory == 1 || app.settingsCategory == 5 {
			sectionGap := 8
			sectionW := (innerContentW - sectionGap) / 2
			for i := range app.settingsSectionRects {
				x := innerLeft + i*(sectionW+sectionGap)
				app.settingsSectionRects[i] = RECT{int32(x), int32(headerContentY), int32(x + sectionW), int32(headerContentY + 34)}
			}
			headerContentY += 56
		}
		virtualHeight := settingsVirtualContentHeight()
		viewportBottom := bodyBottom - 34
		app.settingsScrollMax = float64(max(0, headerContentY+virtualHeight-viewportBottom))
		if app.settingsSubpage == 2 {
			app.settingsScrollMax = 0
		}
		app.settingsScrollPx = clampFloat(app.settingsScrollPx, 0, app.settingsScrollMax)
		app.settingsScrollTarget = clampFloat(app.settingsScrollTarget, 0, app.settingsScrollMax)
		contentY := headerContentY - int(app.settingsScrollPx)
		app.settingsContentTop = contentY
		app.settingsScrollTrack = RECT{int32(innerRight - 7), int32(headerContentY), int32(innerRight - 2), int32(viewportBottom)}
		viewH := max(1, viewportBottom-headerContentY)
		app.settingsScrollThumb = scrollThumbRectPixels(app.settingsScrollTrack, viewH+int(app.settingsScrollMax), viewH, app.settingsScrollPx)
		settingsRight := innerRight
		if app.settingsScrollMax > 0 {
			settingsRight -= 18
		}
		settingsContentW := settingsRight - innerLeft
		switch app.settingsSubpage {
		case 0:
			row0 := uiSettingsRowTop(contentY, 0)
			row1 := uiSettingsRowTop(contentY, 1)
			row2 := uiSettingsRowTop(contentY, 2)
			row3 := uiSettingsRowTop(contentY, 3)
			row4 := uiSettingsRowTop(contentY, 4)
			row5 := uiSettingsRowTop(contentY, 5)
			row6 := uiSettingsRowTop(contentY, 6)
			row7 := uiSettingsRowTop(contentY, 7)
			app.autoRect = RECT{int32(innerLeft), int32(row0), int32(innerLeft + 28), int32(row0 + 28)}
			app.trayRect = RECT{int32(innerLeft), int32(row1), int32(innerLeft + 28), int32(row1 + 28)}
			app.notificationsRect = RECT{int32(innerLeft), int32(row2), int32(innerLeft + 28), int32(row2 + 28)}
			app.soundsRect, app.volumeTrackRect, app.volumeKnobRect, app.volumeValueRect = RECT{}, RECT{}, RECT{}, RECT{}
			app.lockMinimumRect = RECT{int32(innerLeft), int32(row3), int32(innerLeft + 28), int32(row3 + 28)}
			app.lockCurrentRect = RECT{int32(innerLeft), int32(row4), int32(innerLeft + 28), int32(row4 + 28)}
			app.wakeScheduledRect = RECT{int32(innerLeft), int32(row5), int32(innerLeft + 28), int32(row5 + 28)}
			app.hotkeysRect = RECT{int32(innerLeft), int32(row6), int32(innerLeft + 28), int32(row6 + 28)}
			for i := range app.settingsResourceRefreshRects {
				app.settingsResourceRefreshRects[i] = RECT{}
			}
			app.hideZeroResourceProcessesRect = RECT{int32(innerLeft), int32(row7), int32(innerLeft + 28), int32(row7 + 28)}
			lineX := int(app.wakeScheduledRect.Right) + 12
			_, app.wakeLeadFieldRect, _ = uiInlineNumberLayout("Пробуждать ПК по расписанию за", "мин", lineX, uiInlineSentenceY(row5), settingsRight, 2)
			uiPlaceInlineNumberEdit(idWakeLead, app.wakeLeadFieldRect)
		case 1:
			rowY := contentY + 26
			tw := (settingsContentW - 20) / 3
			for i := 0; i < 3; i++ {
				x := innerLeft + i*(tw+10)
				app.themeRects[i] = RECT{int32(x), int32(rowY), int32(x + tw), int32(rowY + 50)}
			}
			bgY := rowY + 86
			bgCols := 3
			bw := (settingsContentW - 20) / bgCols
			for i := 0; i < 6; i++ {
				row, col := i/bgCols, i%bgCols
				x := innerLeft + col*(bw+10)
				y := bgY + row*54
				app.backgroundRects[i] = RECT{int32(x), int32(y), int32(x + bw), int32(y + 44)}
			}
			surfY := bgY + 126
			sw := (settingsContentW - 40) / 5
			for i := 0; i < 5; i++ {
				x := innerLeft + i*(sw+10)
				app.surfaceRects[i] = RECT{int32(x), int32(surfY), int32(x + sw), int32(surfY + 46)}
			}
			animY := surfY + 78
			animW := (settingsContentW - 20) / 3
			for i := 0; i < 3; i++ {
				x := innerLeft + i*(animW+10)
				app.animationRects[i] = RECT{int32(x), int32(animY), int32(x + animW), int32(animY + 44)}
			}
		case 2:
			if app.historyDetailOpen {
				app.historyDetailBackRect = RECT{int32(innerLeft), int32(contentY), int32(innerLeft + 118), int32(contentY + 34)}
				listTop := contentY + 72
				listBottom := bodyBottom - 12
				rowH, rowGap := 58, 7
				stride := rowH + rowGap
				viewH := max(1, listBottom-listTop)
				items := historyDetailItems()
				contentH := max(0, len(items)*stride-rowGap)
				maxPx := float64(max(0, contentH-viewH))
				app.historyDetailScrollPx = clampFloat(app.historyDetailScrollPx, 0, maxPx)
				app.historyDetailScrollTarget = clampFloat(app.historyDetailScrollTarget, 0, maxPx)
				first, rem := 0, 0
				if stride > 0 {
					first = int(app.historyDetailScrollPx) / stride
					rem = int(app.historyDetailScrollPx) % stride
				}
				visible := minInt(len(app.historyDetailRows), viewH/stride+3)
				if visible < 1 {
					visible = 1
				}
				app.historyDetailVisible = visible
				for i := range app.historyDetailRows {
					app.historyDetailRows[i] = RECT{}
				}
				rowRight := innerRight - 18
				for i := 0; i < visible; i++ {
					y := listTop - rem + i*stride
					app.historyDetailRows[i] = RECT{int32(innerLeft), int32(y), int32(rowRight), int32(y + rowH)}
					_ = first
				}
				app.historyDetailListClip = RECT{int32(innerLeft), int32(listTop), int32(rowRight), int32(listBottom)}
				app.historyDetailScrollTrack = RECT{int32(innerRight - 10), int32(listTop), int32(innerRight - 4), int32(listBottom)}
				app.historyDetailScrollThumb = scrollThumbRectPixels(app.historyDetailScrollTrack, contentH, viewH, app.historyDetailScrollPx)
			} else {
				filterY := contentY
				fw := (innerContentW - 24) / 4
				for i := 0; i < 4; i++ {
					x := innerLeft + i*(fw+8)
					app.historyFilterRects[i] = RECT{int32(x), int32(filterY), int32(x + fw), int32(filterY + 34)}
				}
				app.historySearchRect = RECT{int32(innerLeft), int32(filterY + 44), int32(innerRight - 18), int32(filterY + 78)}
				move(app.edits[idHistorySearch], innerLeft+8, filterY+52, innerContentW-42, 18)
				if app.notificationPanelOpen || app.taskMenuOpen || app.resourceMenuOpen {
					pShowWindow.Call(app.edits[idHistorySearch], SW_HIDE)
				} else {
					pShowWindow.Call(app.edits[idHistorySearch], SW_SHOW)
				}
				listTop := filterY + 90
				listBottom := bodyBottom - 86
				rowH, rowGap := 48, 6
				stride := rowH + rowGap
				viewH := max(1, listBottom-listTop)
				items := filteredHistoryItems()
				contentH := max(0, len(items)*stride-rowGap)
				maxPx := float64(max(0, contentH-viewH))
				app.historyScrollPx = clampFloat(app.historyScrollPx, 0, maxPx)
				app.historyScrollTarget = clampFloat(app.historyScrollTarget, 0, maxPx)
				first := 0
				rem := 0
				if stride > 0 {
					first = int(app.historyScrollPx) / stride
					rem = int(app.historyScrollPx) % stride
				}
				app.historyScroll = first
				visible := minInt(len(app.historyRows), viewH/stride+3)
				if visible < 1 {
					visible = 1
				}
				app.historyVisible = visible
				for i := range app.historyRows {
					app.historyRows[i] = RECT{}
				}
				rowRight := innerRight - 18
				for i := 0; i < visible; i++ {
					y := listTop - rem + i*stride
					app.historyRows[i] = RECT{int32(innerLeft), int32(y), int32(rowRight), int32(y + rowH)}
				}
				app.historyListClip = RECT{int32(innerLeft), int32(listTop), int32(rowRight), int32(listBottom)}
				app.historyPrevRect, app.historyNextRect = RECT{}, RECT{}
				app.historyScrollTrack = RECT{int32(innerRight - 10), int32(listTop), int32(innerRight - 4), int32(listBottom)}
				app.historyScrollThumb = scrollThumbRectPixels(app.historyScrollTrack, contentH, viewH, app.historyScrollPx)
				app.historyClearRect = RECT{int32(innerLeft), int32(bodyBottom - 76), int32(innerLeft + 120), int32(bodyBottom - 40)}
			}
		case 3:
			for i := range app.resourceTimelineModeRects {
				app.resourceTimelineModeRects[i] = RECT{}
			}
		case 4:
			for i := range app.dataRects {
				app.dataRects[i] = RECT{}
			}
			if app.settingsCategory == 4 {
				app.dataRects[5] = RECT{int32(innerLeft), int32(contentY + 20), int32(settingsRight), int32(contentY + 110)}
				updateY := int(app.dataRects[5].Bottom) + 14
				app.appUpdateRect = RECT{int32(innerLeft), int32(updateY), int32(settingsRight), int32(updateY + 90)}
				actionW := minInt(166, max(126, settingsContentW/3))
				app.temperatureUpdateActionRect = RECT{app.dataRects[5].Right - int32(actionW+12), app.dataRects[5].Top + 25, app.dataRects[5].Right - 12, app.dataRects[5].Bottom - 25}
				app.appUpdateActionRect = RECT{app.appUpdateRect.Right - int32(actionW+12), app.appUpdateRect.Top + 25, app.appUpdateRect.Right - 12, app.appUpdateRect.Bottom - 25}
				autoY := int(app.appUpdateRect.Bottom) + 14
				app.temperatureAutoUpdateRect = RECT{int32(innerLeft), int32(autoY), int32(settingsRight), int32(autoY + 48)}
			} else {
				bw := (settingsContentW - 12) / 2
				bh := 68
				for i := 0; i < 5; i++ {
					row, col := i/2, i%2
					x := innerLeft + col*(bw+12)
					y := contentY + 20 + row*(bh+12)
					app.dataRects[i] = RECT{int32(x), int32(y), int32(x + bw), int32(y + bh)}
				}
				app.appUpdateRect, app.appUpdateActionRect, app.temperatureAutoUpdateRect, app.temperatureUpdateActionRect = RECT{}, RECT{}, RECT{}, RECT{}
			}
		case 5:
			row0 := uiSettingsRowTop(contentY, 0)
			row1 := uiSettingsRowTop(contentY, 1)
			row2 := uiSettingsRowTop(contentY, 2)
			row3 := uiSettingsRowTop(contentY, 3)
			app.safetyFullscreenRect = RECT{int32(innerLeft), int32(row0), int32(innerLeft + 28), int32(row0 + 28)}
			app.safetyRecentRect = RECT{int32(innerLeft), int32(row1), int32(innerLeft + 28), int32(row1 + 28)}
			lineX := int(app.safetyRecentRect.Right) + 12
			_, app.whenFieldRect, _ = uiInlineNumberLayout("Считать неактивным после", "мин", lineX, int(app.safetyRecentRect.Top)-3, settingsRight, 2)
			uiPlaceInlineNumberEdit(idSafetyIdle, app.whenFieldRect)
			app.showSystemProcessesRect = RECT{int32(innerLeft), int32(row2), int32(innerLeft + 28), int32(row2 + 28)}
			app.safetyProcessesRect = RECT{int32(innerLeft), int32(row3), int32(minInt(settingsRight, innerLeft+280)), int32(row3 + 42)}
		case 6:
			app.soundsRect = RECT{int32(innerLeft), int32(contentY + 10), int32(innerLeft + 28), int32(contentY + 38)}
			trackLeft := innerLeft + 4
			valueW := 66
			app.volumeValueRect = RECT{int32(settingsRight - valueW), int32(contentY + 88), int32(settingsRight), int32(contentY + 116)}
			trackRight := int(app.volumeValueRect.Left) - 18
			trackY := contentY + 98
			app.volumeTrackRect = RECT{int32(trackLeft), int32(trackY), int32(trackRight), int32(trackY + 8)}
			knobX := trackLeft + (trackRight-trackLeft)*app.settings.SoundVolume/100
			app.volumeKnobRect = RECT{int32(knobX - 8), int32(trackY - 5), int32(knobX + 8), int32(trackY + 13)}
			move(app.edits[idSoundVolume], int(app.volumeValueRect.Left)+4, int(app.volumeValueRect.Top)+5, valueW-28, 18)
			pShowWindow.Call(app.edits[idSoundVolume], SW_SHOW)
		case 7:
			// Keep the sticky sub-navigation clear of the first section title.
			row0 := contentY + 34
			app.miniAlwaysTopRect = RECT{int32(innerLeft), int32(row0), int32(innerLeft + 28), int32(row0 + 28)}
			miniY := row0 + 72
			miniGap := 8
			miniW := (settingsContentW - miniGap*3) / 4
			for i := 0; i < 4; i++ {
				x := innerLeft + i*(miniW+miniGap)
				app.miniOptionRects[i] = RECT{int32(x), int32(miniY), int32(x + miniW), int32(miniY + 38)}
			}
			sizeY := miniY + 86
			sizeGap := 8
			sizeW := (settingsContentW - sizeGap*2) / 3
			for i := 0; i < 3; i++ {
				x := innerLeft + i*(sizeW+sizeGap)
				app.miniSizeRects[i] = RECT{int32(x), int32(sizeY), int32(x + sizeW), int32(sizeY + 38)}
			}
			scaleY := sizeY + 86
			scaleGap := 8
			scaleW := (settingsContentW - scaleGap*3) / 4
			for i := 0; i < 4; i++ {
				x := innerLeft + i*(scaleW+scaleGap)
				app.uiScaleRects[i] = RECT{int32(x), int32(scaleY), int32(x + scaleW), int32(scaleY + 38)}
			}
			graphSizeY := scaleY + 126
			graphSizeGap := 8
			graphSizeW := (settingsContentW - graphSizeGap*2) / 3
			for i := range app.graphWindowSizeRects {
				x := innerLeft + i*(graphSizeW+graphSizeGap)
				app.graphWindowSizeRects[i] = RECT{int32(x), int32(graphSizeY), int32(x + graphSizeW), int32(graphSizeY + 38)}
			}
			manualY := graphSizeY + 54
			fieldW := minInt(112, (settingsContentW-190)/2)
			app.graphWindowWidthRect = RECT{int32(innerLeft), int32(manualY), int32(innerLeft + fieldW), int32(manualY + 36)}
			app.graphWindowHeightRect = RECT{int32(innerLeft + fieldW + 34), int32(manualY), int32(innerLeft + fieldW*2 + 34), int32(manualY + 36)}
			app.graphWindowLockRect = RECT{int32(innerLeft + fieldW*2 + 46), int32(manualY), int32(settingsRight), int32(manualY + 36)}
			move(app.edits[idGraphWidth], int(app.graphWindowWidthRect.Left)+6, int(app.graphWindowWidthRect.Top)+7, int(app.graphWindowWidthRect.Right-app.graphWindowWidthRect.Left)-12, 20)
			move(app.edits[idGraphHeight], int(app.graphWindowHeightRect.Left)+6, int(app.graphWindowHeightRect.Top)+7, int(app.graphWindowHeightRect.Right-app.graphWindowHeightRect.Left)-12, 20)
			for _, id := range []int{idGraphWidth, idGraphHeight} {
				pShowWindow.Call(app.edits[id], SW_SHOW)
			}
			timelineY := graphSizeY + 154
			selectorGap := 8
			selectorW := (settingsContentW - selectorGap) / 2
			for i := range app.resourceTimelineModeRects {
				x := innerLeft + i*(selectorW+selectorGap)
				app.resourceTimelineModeRects[i] = RECT{int32(x), int32(timelineY + 24), int32(x + selectorW), int32(timelineY + 60)}
			}
			tickY := timelineY + 104
			valueW := 54
			app.resourceTimelineTicksValueRect = RECT{int32(settingsRight - valueW), int32(tickY - 10), int32(settingsRight), int32(tickY + 20)}
			app.resourceTimelineTicksTrackRect = RECT{int32(innerLeft + 18), int32(tickY), int32(settingsRight - valueW - 20), int32(tickY + 8)}
			ticks := resourceTimelineTickCount()
			knobX := int(app.resourceTimelineTicksTrackRect.Left) + (int(app.resourceTimelineTicksTrackRect.Right-app.resourceTimelineTicksTrackRect.Left))*(ticks-2)/10
			app.resourceTimelineTicksKnobRect = RECT{int32(knobX - 8), int32(tickY - 5), int32(knobX + 8), int32(tickY + 13)}
			move(app.edits[idTimelineTicks], int(app.resourceTimelineTicksValueRect.Left)+4, int(app.resourceTimelineTicksValueRect.Top)+5, int(app.resourceTimelineTicksValueRect.Right-app.resourceTimelineTicksValueRect.Left)-8, 20)
			if app.resourceTimelineTicksValueRect.Bottom > int32(headerContentY) && app.resourceTimelineTicksValueRect.Top < int32(viewportBottom) {
				pShowWindow.Call(app.edits[idTimelineTicks], SW_SHOW)
			}
		}
		for _, item := range []struct {
			id int
			r  RECT
		}{{idWakeLead, app.wakeLeadFieldRect}, {idSafetyIdle, app.whenFieldRect}, {idSoundVolume, app.volumeValueRect}, {idTimelineTicks, app.resourceTimelineTicksValueRect}, {idGraphWidth, app.graphWindowWidthRect}, {idGraphHeight, app.graphWindowHeightRect}} {
			if item.r.Right > item.r.Left && (item.r.Bottom <= int32(headerContentY) || item.r.Top >= int32(viewportBottom)) {
				pShowWindow.Call(app.edits[item.id], SW_HIDE)
			}
		}
	}

	if app.section == 4 {
		for i := range app.processFilterRects {
			app.processFilterRects[i] = RECT{}
		}
		listTop := bodyTop + 58
		if app.settings.ShowSystemProcesses {
			filterY := bodyTop + 54
			filterGap := 8
			filterW := (innerContentW - filterGap*2) / 3
			for i := 0; i < 3; i++ {
				x := innerLeft + i*(filterW+filterGap)
				app.processFilterRects[i] = RECT{int32(x), int32(filterY), int32(x + filterW), int32(filterY + 36)}
			}
			listTop = bodyTop + 104
		}
		listBottom := bodyBottom - 54
		rowH, rowGap := 34, 4
		stride := rowH + rowGap
		viewH := max(1, listBottom-listTop)
		for i := range app.processRows {
			app.processRows[i] = RECT{}
		}
		contentH := max(0, len(app.pickerItems)*stride-rowGap)
		maxPx := float64(max(0, contentH-viewH))
		app.processScrollPx = clampFloat(app.processScrollPx, 0, maxPx)
		app.processScrollTarget = clampFloat(app.processScrollTarget, 0, maxPx)
		first, rem := 0, 0
		if stride > 0 {
			first = int(app.processScrollPx) / stride
			rem = int(app.processScrollPx) % stride
		}
		app.processScroll = first
		visible := minInt(len(app.processRows), viewH/stride+3)
		if visible < 1 {
			visible = 1
		}
		app.processVisible = visible
		rowRight := innerRight - 18
		for i := 0; i < visible; i++ {
			y := listTop - rem + i*stride
			app.processRows[i] = RECT{int32(innerLeft), int32(y), int32(rowRight), int32(y + rowH)}
		}
		app.processListClip = RECT{int32(innerLeft), int32(listTop), int32(rowRight), int32(listBottom)}
		app.processPrevRect, app.processNextRect = RECT{}, RECT{}
		app.processScrollTrack = RECT{int32(innerRight - 10), int32(listTop), int32(innerRight - 4), int32(listBottom)}
		app.processScrollThumb = scrollThumbRectPixels(app.processScrollTrack, contentH, viewH, app.processScrollPx)
		app.processDoneRect = RECT{int32(innerRight - 140), int32(bodyBottom - 42), int32(innerRight), int32(bodyBottom - 4)}
	}
	if app.section == 5 {
		rebuildSavedFilter()
		// The task navigation is an overlay. Opening it must not reflow the Saved page
		// (especially the search field), otherwise the input visibly jumps down.
		savedSearchTop := bodyTop + 52
		app.savedSearchRect = RECT{int32(innerLeft), int32(savedSearchTop), int32(innerRight - 18), int32(savedSearchTop + 34)}
		move(app.edits[idSavedSearch], innerLeft+8, savedSearchTop+8, innerContentW-42, 18)
		// A native Win32 EDIT is its own child HWND and therefore paints above our
		// Direct2D task-navigation popup regardless of draw order. Hide only the
		// native text layer while the Task menu is open; the Direct2D search surface
		// stays in place underneath and returns immediately when the popup closes.
		if app.taskMenuOpen || app.resourceMenuOpen || app.notificationPanelOpen {
			pShowWindow.Call(app.edits[idSavedSearch], SW_HIDE)
		} else {
			pShowWindow.Call(app.edits[idSavedSearch], SW_SHOW)
		}
		listTop := savedSearchTop + 46
		listBottom := bodyBottom - 10
		rowH, rowGap := 68, 8
		stride := rowH + rowGap
		viewH := max(1, listBottom-listTop)
		for i := range app.savedRows {
			app.savedRows[i], app.savedFavoriteRects[i], app.savedRunRects[i], app.savedPauseRects[i], app.savedMenuButtonRects[i] = RECT{}, RECT{}, RECT{}, RECT{}, RECT{}
		}
		contentH := max(0, len(app.savedFilteredIndices)*stride-rowGap)
		maxPx := float64(max(0, contentH-viewH))
		app.savedScrollPx = clampFloat(app.savedScrollPx, 0, maxPx)
		app.savedScrollTarget = clampFloat(app.savedScrollTarget, 0, maxPx)
		first, rem := 0, 0
		if stride > 0 {
			first = int(app.savedScrollPx) / stride
			rem = int(app.savedScrollPx) % stride
		}
		app.savedScroll = first
		visible := minInt(len(app.savedRows), viewH/stride+3)
		if visible < 1 {
			visible = 1
		}
		app.savedVisible = visible
		rowRight := innerRight - 18
		for i := 0; i < visible; i++ {
			y := listTop - rem + i*stride
			app.savedRows[i] = RECT{int32(innerLeft), int32(y), int32(rowRight), int32(y + rowH)}
			app.savedMenuButtonRects[i] = RECT{int32(rowRight - 48), int32(y + 14), int32(rowRight - 12), int32(y + 54)}
			app.savedPauseRects[i] = RECT{int32(rowRight - 90), int32(y + 14), int32(rowRight - 54), int32(y + 54)}
			app.savedRunRects[i] = RECT{int32(rowRight - 196), int32(y + 14), int32(rowRight - 100), int32(y + 54)}
			app.savedFavoriteRects[i] = RECT{int32(rowRight - 232), int32(y + 18), int32(rowRight - 204), int32(y + 50)}
		}
		app.savedListClip = RECT{int32(innerLeft), int32(listTop), int32(rowRight), int32(listBottom)}
		app.savedPrevRect, app.savedNextRect = RECT{}, RECT{}
		app.savedScrollTrack = RECT{int32(innerRight - 10), int32(listTop), int32(innerRight - 4), int32(listBottom)}
		app.savedScrollThumb = scrollThumbRectPixels(app.savedScrollTrack, contentH, viewH, app.savedScrollPx)
		if app.savedMenuOpenIdx >= 0 {
			local := savedLocalForUnderlying(app.savedMenuOpenIdx)
			if local >= 0 && local < app.savedVisible {
				btn := app.savedMenuButtonRects[local]
				menuW, menuH := 170, 118
				x := int(btn.Right) - menuW
				y := int(btn.Bottom) + 5
				if y+menuH > bodyBottom {
					y = int(btn.Top) - menuH - 5
				}
				app.savedPopupRect = RECT{int32(x), int32(y), int32(x + menuW), int32(y + menuH)}
				app.savedPopupPauseRect = RECT{}
				app.savedPopupEditRect = RECT{int32(x + 6), int32(y + 6), int32(x + menuW - 6), int32(y + 38)}
				app.savedPopupDuplicateRect = RECT{int32(x + 6), int32(y + 42), int32(x + menuW - 6), int32(y + 74)}
				app.savedPopupDeleteRect = RECT{int32(x + 6), int32(y + 78), int32(x + menuW - 6), int32(y + 110)}
			}
		}
	}
	if app.section == 6 {
		fw := minInt(480, innerContentW)
		app.whenFieldRect = RECT{int32(innerLeft), int32(bodyTop + 86), int32(innerLeft + fw), int32(bodyTop + 130)}
		move(app.edits[idTaskName], innerLeft+10, bodyTop+99, fw-20, 22)
		pShowWindow.Call(app.edits[idTaskName], SW_SHOW)
		app.saveConfirmRect = RECT{int32(innerLeft), int32(bodyTop + 164), int32(innerLeft + 170), int32(bodyTop + 206)}
		app.saveBackRect = RECT{int32(innerLeft + 182), int32(bodyTop + 164), int32(innerLeft + 332), int32(bodyTop + 206)}
	}

	if app.section == 10 {
		app.whenFieldRect = RECT{}
		for i := range app.timeFieldRects {
			app.timeFieldRects[i] = RECT{}
		}
		for i := range app.exactFieldRects {
			app.exactFieldRects[i] = RECT{}
		}
		startY := bodyTop + 54
		kindGap := 10
		kindW := minInt(150, (innerContentW-kindGap)/2)
		app.savedEditKindRects[0] = RECT{int32(innerLeft), int32(startY), int32(innerLeft + kindW), int32(startY + 32)}
		app.savedEditKindRects[1] = RECT{int32(innerLeft + kindW + kindGap), int32(startY), int32(innerLeft + kindW*2 + kindGap), int32(startY + 32)}
		actionY := startY + 42
		cardGap := 8
		actionCount := 4
		if app.savedEditDraft.TaskKind == 1 {
			actionCount = 5
		}
		cardW := (innerContentW - cardGap*(actionCount-1)) / actionCount
		for i := range app.savedEditActionRects {
			app.savedEditActionRects[i] = RECT{}
		}
		for i := 0; i < actionCount; i++ {
			x := innerLeft + i*(cardW+cardGap)
			app.savedEditActionRects[i] = RECT{int32(x), int32(actionY), int32(x + cardW), int32(actionY + 36)}
		}
		modeY := actionY + 46
		modeGap := 7
		for i := range app.savedEditModeRects {
			app.savedEditModeRects[i] = RECT{}
		}
		fieldY := modeY + 42
		if app.savedEditDraft.TaskKind == 1 {
			modeW := (innerContentW - modeGap*2) / 3
			for i := 0; i < 6; i++ {
				row, col := i/3, i%3
				x := innerLeft + col*(modeW+modeGap)
				y := modeY + row*38
				app.savedEditModeRects[i] = RECT{int32(x), int32(y), int32(x + modeW), int32(y + 32)}
			}
			fieldY = modeY + 80
		} else {
			modeW := (innerContentW - modeGap*4) / 5
			for i := 0; i < 5; i++ {
				x := innerLeft + i*(modeW+modeGap)
				app.savedEditModeRects[i] = RECT{int32(x), int32(modeY), int32(x + modeW), int32(modeY + 32)}
			}
		}
		move(app.edits[idTaskName], innerLeft+6, fieldY+22, minInt(350, innerContentW)-12, 21)
		pShowWindow.Call(app.edits[idTaskName], SW_SHOW)
		for i := range app.recurrenceKindRects {
			app.recurrenceKindRects[i] = RECT{}
		}
		for i := range app.recurrenceDayRects {
			app.recurrenceDayRects[i] = RECT{}
		}
		app.savedEditProcessRect = RECT{}
		app.savedEditClearRect = RECT{}
		savedWhenFormY := fieldY + 40
		uiLayoutWhenFieldsAt(app.savedEditDraft.Mode, savedWhenFormY, innerLeft, innerRight, false)
		if app.savedEditDraft.Mode == 3 {
			app.savedEditProcessRect = app.pickRect
			app.savedEditClearRect = app.processClearRect
			app.pickRect = RECT{}
			app.processClearRect = RECT{}
		}
		lowerY := uiWhenContentBottom(savedWhenFormY) + uiMetricsDefault.GapL
		if app.savedEditDraft.TaskKind == 0 {
			app.savedEditCloseRect = RECT{int32(innerLeft), int32(lowerY), int32(innerLeft + 26), int32(lowerY + 26)}
			if app.savedEditDraft.Mode != 3 {
				app.savedEditProcessRect = RECT{int32(innerLeft), int32(lowerY + 34), int32(minInt(innerRight, innerLeft+210)), int32(lowerY + 70)}
			}
		} else {
			app.savedEditCloseRect = RECT{}
			if app.savedEditDraft.Mode != 3 {
				app.savedEditProcessRect = RECT{}
			}
		}
		warnY := lowerY + 82
		if app.savedEditDraft.TaskKind == 1 {
			warnY = lowerY + 26
		}
		_, app.savedEditWarningRect, _ = uiInlineNumberLayout("Предупреждение за", "сек.", innerLeft, warnY+3, innerRight, 3)
		uiPlaceInlineNumberEdit(idWarning, app.savedEditWarningRect)
		app.savedEditScenarioRect = RECT{}
		if app.savedEditDraft.TaskKind == 1 {
			sy := warnY + uiMetricsDefault.CompactFieldH + 10
			app.savedEditScenarioRect = RECT{int32(innerLeft), int32(sy), int32(minInt(innerRight, innerLeft+220)), int32(sy + 36)}
		}
		app.savedEditSaveRect = RECT{int32(innerLeft), int32(bodyBottom - 50), int32(innerLeft + 144), int32(bodyBottom - 12)}
		app.savedEditCancelRect = RECT{int32(innerLeft + 156), int32(bodyBottom - 50), int32(innerLeft + 300), int32(bodyBottom - 12)}
	}

	// Block task flowchart. Saved-task editing uses the same renderer with an isolated draft.
	if app.section == 7 || app.section == 13 {
		graphBody := RECT{int32(innerLeft), int32(bodyTop), int32(innerRight), int32(bodyBottom)}
		if app.graphWindow == 0 {
			layoutScenarioGraphEditor(graphBody, false)
		} else {
			layoutScenarioGraphMainPlaceholder(graphBody)
		}
	}
	if false && (app.section == 7 || app.section == 13) {
		app.scenarioBackRect = RECT{}
		app.triggerLogicRect = RECT{}
		toolX := innerLeft
		app.undoRect = RECT{int32(toolX), int32(bodyTop + 16), int32(toolX + 42), int32(bodyTop + 50)}
		app.redoRect = RECT{int32(toolX + 48), int32(bodyTop + 16), int32(toolX + 90), int32(bodyTop + 50)}
		// Templates are created from Task → Create task → From template, not from
		// inside an already-open advanced scenario. This also frees header space.
		app.templatesRect = RECT{}
		// Preview replaces the obsolete global trigger-logic switch.
		app.previewRect = RECT{int32(innerRight - 112), int32(bodyTop + 16), int32(innerRight), int32(bodyTop + 50)}
		app.dryRunRect, app.diagnosticsRect = RECT{}, RECT{}
		nodeW := minInt(320, innerContentW-100)
		if nodeW < 220 {
			nodeW = max(180, innerContentW-32)
		}
		centerX := innerLeft + innerContentW/2
		app.blockWhenRect = RECT{int32(centerX - nodeW/2), int32(bodyTop + 72), int32(centerX + nodeW/2), int32(bodyTop + 122)}
		colGap := 18
		// Reserve room for the list scrollbar so row actions are never clipped by the list clip.
		rightLimit := innerRight - 18
		columnsW := max(1, rightLimit-innerLeft)
		colW := (columnsW - colGap) / 2
		leftX := innerLeft
		rightX := innerLeft + colW + colGap
		listY := bodyTop + 136
		app.blockProcessesRect = RECT{} // Advanced tasks close processes through ordinary +Step blocks.
		stride := 33
		rowH := 29
		for i := range app.conditionRows {
			app.conditionRows[i], app.conditionLogicRects[i], app.conditionDeleteRects[i], app.conditionDragRects[i], app.conditionCollapseRects[i] = RECT{}, RECT{}, RECT{}, RECT{}, RECT{}
		}
		for i := range app.stepRows {
			app.stepRows[i], app.stepDeleteRects[i], app.stepDragRects[i] = RECT{}, RECT{}, RECT{}
		}
		finalY := bodyBottom - 58
		if app.section == 13 {
			finalY = bodyBottom - 114
		}
		addY := finalY - 42
		listTop := listY + 24
		listBottom := addY - 8
		if listBottom < listTop+rowH {
			listBottom = listTop + rowH
		}
		app.scenarioListClip = RECT{int32(leftX), int32(listTop), int32(rightLimit), int32(listBottom)}
		app.scenarioScrollTrack = RECT{int32(innerRight - 7), int32(listTop), int32(innerRight - 2), int32(listBottom)}
		conditions := currentScenarioConditions()
		visibleConditions := visibleScenarioConditionIndices(conditions)
		maxCount := max(len(visibleConditions), len(currentScenarioSteps()))
		contentH := max(0, maxCount*stride-(stride-rowH))
		viewH := max(1, listBottom-listTop)
		maxPx := float64(max(0, contentH-viewH))
		app.scenarioScrollPx = clampFloat(app.scenarioScrollPx, 0, maxPx)
		app.scenarioScrollTarget = clampFloat(app.scenarioScrollTarget, 0, maxPx)
		first, rem := 0, 0
		if stride > 0 {
			first = int(app.scenarioScrollPx) / stride
			rem = int(app.scenarioScrollPx) % stride
		}
		app.scenarioFirst = first
		visible := clampInt(viewH/stride+2, 1, len(app.conditionRows))
		app.scenarioVisible = visible
		for i := range app.conditionRowIndices {
			app.conditionRowIndices[i], app.stepRowIndices[i] = -1, -1
		}
		for slot := 0; slot < visible; slot++ {
			dataIdx := -1
			y := listTop - rem + slot*stride
			if first+slot < len(visibleConditions) {
				dataIdx = visibleConditions[first+slot]
				app.conditionRowIndices[slot] = dataIdx
				depth := scenarioConditionDepth(conditions, dataIdx)
				rowLeft := leftX + depth*12
				app.conditionRows[slot] = RECT{int32(rowLeft), int32(y), int32(leftX + colW), int32(y + rowH)}
				dragLeft := rowLeft + 3
				if conditions[dataIdx].Type == condGroup {
					app.conditionCollapseRects[slot] = RECT{int32(rowLeft + 3), int32(y + 3), int32(rowLeft + 22), int32(y + rowH - 3)}
					dragLeft = rowLeft + 22
				}
				app.conditionDragRects[slot] = RECT{int32(dragLeft), int32(y + 1), int32(rowLeft + 41), int32(y + rowH - 1)}
				app.conditionLogicRects[slot] = RECT{int32(rowLeft + 41), int32(y + 4), int32(rowLeft + 80), int32(y + rowH - 4)}
				app.conditionDeleteRects[slot] = RECT{int32(leftX + colW - 29), int32(y + 3), int32(leftX + colW - 5), int32(y + rowH - 3)}
				app.conditionDuplicateRects[slot] = RECT{int32(leftX + colW - 57), int32(y + 3), int32(leftX + colW - 33), int32(y + rowH - 3)}
				app.conditionCopyRects[slot] = RECT{int32(leftX + colW - 85), int32(y + 3), int32(leftX + colW - 61), int32(y + rowH - 3)}
			}
			stepIdx := first + slot
			if stepIdx < len(currentScenarioSteps()) {
				app.stepRowIndices[slot] = stepIdx
				app.stepRows[slot] = RECT{int32(rightX), int32(y), int32(rightX + colW), int32(y + rowH)}
				app.stepDragRects[slot] = RECT{int32(rightX + 3), int32(y + 1), int32(rightX + 37), int32(y + rowH - 1)}
				app.stepDeleteRects[slot] = RECT{int32(rightX + colW - 29), int32(y + 3), int32(rightX + colW - 5), int32(y + rowH - 3)}
				app.stepDuplicateRects[slot] = RECT{int32(rightX + colW - 57), int32(y + 3), int32(rightX + colW - 33), int32(y + rowH - 3)}
				app.stepCopyRects[slot] = RECT{int32(rightX + colW - 85), int32(y + 3), int32(rightX + colW - 61), int32(y + rowH - 3)}
			}
		}
		app.scenarioScrollThumb = scrollThumbRectPixels(app.scenarioScrollTrack, contentH, viewH, app.scenarioScrollPx)
		gapBtn := 6
		half := (colW - gapBtn) / 2
		app.addConditionRect = RECT{int32(leftX), int32(addY), int32(leftX + half), int32(addY + 32)}
		app.addConditionGroupRect = RECT{int32(leftX + half + gapBtn), int32(addY), int32(leftX + colW), int32(addY + 32)}
		app.addStepRect = RECT{int32(rightX), int32(addY), int32(rightX + colW), int32(addY + 32)}
		// Clipboard actions live beside the corresponding column heading.
		iconY := listY - 5
		app.copyConditionsGroupRect = RECT{int32(leftX + colW - 28), int32(iconY), int32(leftX + colW), int32(iconY + 26)}
		app.pasteConditionRect = RECT{int32(leftX + colW - 60), int32(iconY), int32(leftX + colW - 32), int32(iconY + 26)}
		app.copyStepsGroupRect = RECT{int32(rightX + colW - 28), int32(iconY), int32(rightX + colW), int32(iconY + 26)}
		app.pasteStepRect = RECT{int32(rightX + colW - 60), int32(iconY), int32(rightX + colW - 32), int32(iconY + 26)}
		app.blockActionRect = RECT{int32(centerX - nodeW/2), int32(finalY), int32(centerX + nodeW/2), int32(finalY + 46)}
		app.savedScenarioNameRect, app.savedScenarioSaveRect, app.savedScenarioCancelRect, app.savedScenarioCheckRect = RECT{}, RECT{}, RECT{}, RECT{}
		if app.section == 13 {
			footerY := bodyBottom - 48
			nameW := minInt(205, max(145, innerContentW*31/100))
			app.savedScenarioNameRect = RECT{int32(innerLeft), int32(footerY), int32(innerLeft + nameW), int32(footerY + 36)}
			move(app.edits[idTaskName], innerLeft+9, footerY+8, nameW-18, 20)
			if app.confirmDiscardScenario {
				pShowWindow.Call(app.edits[idTaskName], SW_HIDE)
			} else {
				pShowWindow.Call(app.edits[idTaskName], SW_SHOW)
			}
			btnLeft := innerLeft + nameW + 8
			btnGap := 7
			btnW := max(76, (innerRight-btnLeft-btnGap*2)/3)
			app.savedScenarioSaveRect = RECT{int32(btnLeft), int32(footerY), int32(btnLeft + btnW), int32(footerY + 36)}
			app.savedScenarioCancelRect = RECT{int32(btnLeft + btnW + btnGap), int32(footerY), int32(btnLeft + btnW*2 + btnGap), int32(footerY + 36)}
			app.savedScenarioCheckRect = RECT{int32(btnLeft + btnW*2 + btnGap*2), int32(footerY), int32(innerRight), int32(footerY + 36)}
		}
	}

	if app.section == 16 {
		app.templateBackRect = RECT{int32(innerLeft), int32(bodyTop + 18), int32(innerLeft + 110), int32(bodyTop + 52)}
		gap := 10
		cols := 2
		tw := (innerContentW - gap) / cols
		startY := bodyTop + 78
		for i := 0; i < len(app.templateRects); i++ {
			row, col := i/cols, i%cols
			x := innerLeft + col*(tw+gap)
			y := startY + row*72
			app.templateRects[i] = RECT{int32(x), int32(y), int32(x + tw), int32(y + 60)}
		}
	}
	if app.section == 17 {
		app.previewBackRect = RECT{int32(innerLeft), int32(bodyTop + 18), int32(innerLeft + 110), int32(bodyTop + 52)}
		startY := bodyTop + 78
		rowH := 38
		gap := 7
		for i := range app.previewRows {
			app.previewRows[i] = RECT{}
		}
		count := 1 + len(currentScenarioConditions()) + len(currentScenarioSteps()) + 1
		if count > len(app.previewRows) {
			count = len(app.previewRows)
		}
		for i := 0; i < count; i++ {
			y := startY + i*(rowH+gap)
			app.previewRows[i] = RECT{int32(innerLeft + 40), int32(y), int32(innerRight - 40), int32(y + rowH)}
		}
	}

	if app.section == 14 {
		app.blockEditorBackRect = RECT{int32(innerLeft), int32(bodyTop + 18), int32(innerLeft + 110), int32(bodyTop + 52)}
		cardGap := 12
		cardW := (innerContentW - cardGap) / 2
		cardH := 62
		startY := bodyTop + 88
		for i := 0; i < 4; i++ {
			row, col := i/2, i%2
			x := innerLeft + col*(cardW+cardGap)
			y := startY + row*(cardH+cardGap)
			app.blockActionChoiceRects[i] = RECT{int32(x), int32(y), int32(x + cardW), int32(y + cardH)}
		}
		lastY := startY + 2*(cardH+cardGap)
		app.blockActionChoiceRects[4] = RECT{int32(innerLeft), int32(lastY), int32(innerRight), int32(lastY + cardH)}
	}
	if app.section == 15 {
		app.whenFieldRect = RECT{}
		app.pickRect = RECT{}
		for i := range app.timeFieldRects {
			app.timeFieldRects[i] = RECT{}
		}
		for i := range app.exactFieldRects {
			app.exactFieldRects[i] = RECT{}
		}
		app.blockEditorBackRect = RECT{int32(innerLeft), int32(bodyTop + 18), int32(innerLeft + 110), int32(bodyTop + 52)}
		app.blockEditorDoneRect = RECT{int32(innerRight - 120), int32(bodyTop + 18), int32(innerRight), int32(bodyTop + 52)}
		cols := 3
		gap := 10
		cw := (innerContentW - gap*2) / 3
		startY := bodyTop + 82
		for i := 0; i < 6; i++ {
			row, col := i/cols, i%cols
			x := innerLeft + col*(cw+gap)
			y := startY + row*46
			app.modeRects[i] = RECT{int32(x), int32(y), int32(x + cw), int32(y + 38)}
		}
		uiLayoutWhenFields(app.selectedMode, app.modeRects[:6], innerLeft, innerRight, false)
	}

	if app.section == 8 {
		// Core conditions stay visible. Extra conditions reveal vertically and the
		// expand/collapse button travels with the bottom edge of the revealed list.
		for i := range app.editorTypeRects {
			app.editorTypeRects[i] = RECT{}
		}
		startY := bodyTop + 52
		basic := []int{condCPU, condGPU, condNetwork, condDisk, condFileStable, condProcessExit}
		basicCols, basicGap := 3, 7
		basicW := (innerContentW - basicGap*(basicCols-1)) / basicCols
		for pos, typ := range basic {
			row, col := pos/basicCols, pos%basicCols
			x := innerLeft + col*(basicW+basicGap)
			y := startY + row*34
			app.editorTypeRects[typ] = RECT{int32(x), int32(y), int32(x + basicW), int32(y + 29)}
		}
		baseMoreY := startY + 2*34 + 2
		extra := []int{condWindowExists, condWindowMissing, condWindowActive, condWindowTitle, condAudioSilent, condBatteryPercent, condACPower, condDiskFree, condFolderCount, condProcessCPU, condProcessGPU, condProcessRAM, condInternet, condFullscreen, condDrivePresent}
		cols, gap := 5, 6
		cw := (innerContentW - gap*(cols-1)) / cols
		extraY := baseMoreY + 2
		rows := (len(extra) + cols - 1) / cols
		extraFullH := rows*32 + 4
		app.conditionCatalogBaseMoreY = int32(baseMoreY)
		app.conditionCatalogExtraFullH = int32(extraFullH)
		// The time-based animation driver already applies easing. Do not ease twice.
		t := clampFloat(app.conditionCatalogAnim, 0, 1)
		// Keep the extra-condition geometry stable for the entire lifetime of the editor.
		// Visibility is controlled only by the animated clip edge below; creating/clearing
		// these rectangles at the closing threshold caused the last-frame flash.
		for pos, typ := range extra {
			row, col := pos/cols, pos%cols
			x := innerLeft + col*(cw+gap)
			y := extraY + row*32
			app.editorTypeRects[typ] = RECT{int32(x), int32(y), int32(x + cw), int32(y + 27)}
		}
		buttonY := baseMoreY + int(float64(extraFullH)*t)
		app.conditionMoreRect = RECT{int32(innerLeft), int32(buttonY), int32(minInt(innerRight, innerLeft+230)), int32(buttonY + 30)}
		fieldY := buttonY + 36
		boxW := 82
		thresholdX := innerLeft
		holdX := innerLeft + boxW + 12
		if !conditionUsesThreshold(app.conditionDraft.Type) {
			holdX = innerLeft
		}
		app.whenFieldRect = RECT{int32(thresholdX), int32(fieldY + 24), int32(thresholdX + boxW), int32(fieldY + 54)}
		app.warningFieldRect = RECT{int32(holdX), int32(fieldY + 24), int32(holdX + boxW), int32(fieldY + 54)}
		compareX := max(innerLeft+190, holdX+boxW+16)
		for i := 0; i < 2; i++ {
			x := compareX + i*62
			app.editorCompareRects[i] = RECT{int32(x), int32(fieldY + 22), int32(x + 52), int32(fieldY + 56)}
		}
		if app.conditionDraft.Type == condACPower || app.conditionDraft.Type == condInternet || app.conditionDraft.Type == condFullscreen || app.conditionDraft.Type == condDrivePresent {
			buttonW := minInt(112, max(84, (innerContentW-110)/3))
			app.editorCompareRects[0] = RECT{int32(innerLeft), int32(fieldY + 22), int32(innerLeft + buttonW), int32(fieldY + 56)}
			app.editorCompareRects[1] = RECT{int32(innerLeft + buttonW + 8), int32(fieldY + 22), int32(innerLeft + buttonW*2 + 8), int32(fieldY + 56)}
			holdX = innerLeft + buttonW*2 + 24
			app.warningFieldRect = RECT{int32(holdX), int32(fieldY + 24), int32(minInt(innerRight, holdX+boxW)), int32(fieldY + 54)}
		}
		if conditionUsesThreshold(app.conditionDraft.Type) {
			move(app.edits[idCondThreshold], thresholdX+4, fieldY+30, boxW-8, 18)
			if !suppressEditVisibilityDuringLayout {
				pShowWindow.Call(app.edits[idCondThreshold], SW_SHOW)
			}
		}
		move(app.edits[idCondHold], holdX+4, fieldY+30, boxW-8, 18)
		if !suppressEditVisibilityDuringLayout {
			pShowWindow.Call(app.edits[idCondHold], SW_SHOW)
		}
		textY := fieldY + 66
		needsBrowse := app.conditionDraft.Type == condFileStable || app.conditionDraft.Type == condFolderCount || app.conditionDraft.Type == condDiskFree || app.conditionDraft.Type == condDrivePresent || app.conditionDraft.Type == condProcessExit || app.conditionDraft.Type == condAudioSilent || app.conditionDraft.Type == condProcessCPU || app.conditionDraft.Type == condProcessGPU || app.conditionDraft.Type == condProcessRAM
		textRight := innerRight
		if needsBrowse {
			textRight = innerRight - 164
		}
		app.timeFieldRects[0] = RECT{int32(innerLeft), int32(textY + 22), int32(textRight), int32(textY + 56)}
		move(app.edits[idCondText], innerLeft+8, textY+27, max(40, textRight-innerLeft-16), 22)
		if !suppressEditVisibilityDuringLayout {
			pShowWindow.Call(app.edits[idCondText], SW_SHOW)
		}
		if needsBrowse {
			app.editorBrowseRect = RECT{int32(innerRight - 154), int32(textY + 22), int32(innerRight - 36), int32(textY + 56)}
			app.editorClearRect = RECT{int32(innerRight - 30), int32(textY + 24), int32(innerRight), int32(textY + 54)}
		} else {
			app.editorBrowseRect, app.editorClearRect = RECT{}, RECT{}
		}
		groupY := textY + 66
		app.conditionOpenGroupRect = RECT{}
		app.conditionCloseGroupRect = RECT{}
		delayX := innerLeft + 110
		app.conditionDelayFieldRect = RECT{int32(delayX), int32(groupY + 2), int32(minInt(innerRight, delayX+54)), int32(groupY + 32)}
		move(app.edits[idCondDelay], int(app.conditionDelayFieldRect.Left)+4, int(app.conditionDelayFieldRect.Top)+6, int(app.conditionDelayFieldRect.Right-app.conditionDelayFieldRect.Left)-8, 18)
		if !suppressEditVisibilityDuringLayout {
			pShowWindow.Call(app.edits[idCondDelay], SW_SHOW)
		}
		app.editorSaveRect = RECT{int32(innerLeft), int32(bodyBottom - 56), int32(innerLeft + 160), int32(bodyBottom - 16)}
		app.editorCancelRect = RECT{int32(innerLeft + 172), int32(bodyBottom - 56), int32(innerLeft + 322), int32(bodyBottom - 16)}
	}
	if app.section == 9 {
		// Five columns keep the action editor to two selector rows even on the
		// minimum window. That leaves enough vertical space for error policy,
		// pause-after controls and the fixed Save/Cancel footer without overlap.
		cols := 5
		cg := 7
		cw := (innerContentW - cg*(cols-1)) / cols
		startY := bodyTop + 64
		for i := range app.stepTypeRects {
			app.stepTypeRects[i] = RECT{}
		}
		visibleStepTypes := 10 // Mute/Unmute lives inside the Volume editor.
		for i := 0; i < visibleStepTypes; i++ {
			row, col := i/cols, i%cols
			x := innerLeft + col*(cw+cg)
			y := startY + row*40
			app.stepTypeRects[i] = RECT{int32(x), int32(y), int32(x + cw), int32(y + 33)}
		}

		rows := (visibleStepTypes + cols - 1) / cols
		fieldY := startY + rows*40 + 8
		contentBottom := fieldY + 76
		app.whenFieldRect = RECT{}
		app.timeFieldRects[0] = RECT{}
		app.editorBrowseRect = RECT{}
		for i := range app.powerPlanRects {
			app.powerPlanRects[i] = RECT{}
		}
		if app.stepDraft.Type == stepCloseProcesses {
			app.editorBrowseRect = RECT{int32(innerLeft), int32(fieldY + 28), int32(minInt(innerRight, innerLeft+220)), int32(fieldY + 66)}
			contentBottom = fieldY + 66
		} else if app.stepDraft.Type == stepWait {
			app.whenFieldRect = RECT{int32(innerLeft), int32(fieldY + 32), int32(innerLeft + 132), int32(fieldY + 68)}
			move(app.edits[idStepValue], innerLeft+7, fieldY+42, 118, 18)
			pShowWindow.Call(app.edits[idStepValue], SW_SHOW)
			contentBottom = fieldY + 68
		} else if app.stepDraft.Type == stepSetVolume || app.stepDraft.Type == stepMute {
			pg := 8
			pw := (innerContentW - pg*2) / 3
			for i := 0; i < 3; i++ {
				x := innerLeft + i*(pw+pg)
				app.powerPlanRects[i] = RECT{int32(x), int32(fieldY + 30), int32(x + pw), int32(fieldY + 68)}
			}
			contentBottom = fieldY + 68
			if app.stepDraft.Type == stepSetVolume {
				app.whenFieldRect = RECT{int32(innerLeft), int32(fieldY + 96), int32(innerLeft + 146), int32(fieldY + 132)}
				move(app.edits[idStepValue], innerLeft+7, fieldY+106, 132, 18)
				pShowWindow.Call(app.edits[idStepValue], SW_SHOW)
				contentBottom = fieldY + 132
			}
		} else if app.stepDraft.Type == stepPowerPlan {
			pg := 8
			pw := (innerContentW - pg*2) / 3
			for i := 0; i < 3; i++ {
				x := innerLeft + i*(pw+pg)
				app.powerPlanRects[i] = RECT{int32(x), int32(fieldY + 30), int32(x + pw), int32(fieldY + 68)}
			}
			contentBottom = fieldY + 68
		} else if app.stepDraft.Type == stepProcessPriority {
			fieldW := max(180, innerContentW-132)
			app.timeFieldRects[0] = RECT{int32(innerLeft), int32(fieldY + 28), int32(innerLeft + fieldW), int32(fieldY + 64)}
			move(app.edits[idStepText], innerLeft+8, fieldY+37, fieldW-16, 20)
			pShowWindow.Call(app.edits[idStepText], SW_SHOW)
			app.editorBrowseRect = RECT{int32(innerLeft + fieldW + 8), int32(fieldY + 28), int32(innerRight), int32(fieldY + 64)}
			pg := 8
			pw := (innerContentW - pg*2) / 3
			for i := 0; i < 3; i++ {
				xx := innerLeft + i*(pw+pg)
				app.powerPlanRects[i] = RECT{int32(xx), int32(fieldY + 82), int32(xx + pw), int32(fieldY + 116)}
			}
			contentBottom = fieldY + 116
		} else if app.stepDraft.Type == stepRunCommand || app.stepDraft.Type == stepNotify {
			app.timeFieldRects[0] = RECT{int32(innerLeft), int32(fieldY + 28), int32(innerRight), int32(fieldY + 72)}
			move(app.edits[idStepText], innerLeft+9, fieldY+39, innerContentW-18, 24)
			pShowWindow.Call(app.edits[idStepText], SW_SHOW)
			if app.stepDraft.Type == stepRunCommand {
				app.editorBrowseRect = RECT{int32(innerRight - 118), int32(fieldY + 88), int32(innerRight), int32(fieldY + 124)}
				contentBottom = fieldY + 124
			} else {
				contentBottom = fieldY + 72
			}
		}
		errorY := max(fieldY+118, contentBottom+16)
		errGap := 8
		errW := (innerContentW - errGap*2) / 3
		for i := 0; i < 3; i++ {
			x := innerLeft + i*(errW+errGap)
			app.stepErrorRects[i] = RECT{int32(x), int32(errorY), int32(x + errW), int32(errorY + 32)}
		}
		app.stepRetryFieldRect = RECT{int32(innerLeft), int32(errorY + 46), int32(innerLeft + 92), int32(errorY + 76)}
		delayX := innerLeft
		if app.stepDraft.OnError == 2 {
			delayX = innerLeft + 112
		}
		app.stepDelayFieldRect = RECT{int32(delayX), int32(errorY + 46), int32(delayX + 92), int32(errorY + 76)}
		if app.stepDraft.OnError == 2 {
			move(app.edits[idStepRetries], int(app.stepRetryFieldRect.Left)+6, int(app.stepRetryFieldRect.Top)+7, int(app.stepRetryFieldRect.Right-app.stepRetryFieldRect.Left)-12, 18)
			pShowWindow.Call(app.edits[idStepRetries], SW_SHOW)
		}
		move(app.edits[idStepDelay], int(app.stepDelayFieldRect.Left)+6, int(app.stepDelayFieldRect.Top)+7, int(app.stepDelayFieldRect.Right-app.stepDelayFieldRect.Left)-12, 18)
		pShowWindow.Call(app.edits[idStepDelay], SW_SHOW)
		app.editorSaveRect = RECT{int32(innerLeft), int32(bodyBottom - 56), int32(innerLeft + 160), int32(bodyBottom - 16)}
		app.editorCancelRect = RECT{int32(innerLeft + 172), int32(bodyBottom - 56), int32(innerLeft + 322), int32(bodyBottom - 16)}
	}

	if app.section == 11 {
		app.diagnosticBackRect = RECT{int32(innerLeft), int32(bodyTop + 18), int32(innerLeft + 120), int32(bodyTop + 54)}
		app.diagnosticRefreshRect = RECT{int32(innerRight - 154), int32(bodyTop + 18), int32(innerRight), int32(bodyTop + 54)}
		app.diagnosticNextRect, app.diagnosticRestartRect, app.diagnosticRunRect = RECT{}, RECT{}, RECT{}
		if app.diagnosticMode == 1 {
			y := bodyBottom - 48
			bw := (innerContentW - 20) / 3
			app.diagnosticRestartRect = RECT{int32(innerLeft), int32(y), int32(innerLeft + bw), int32(y + 36)}
			app.diagnosticNextRect = RECT{int32(innerLeft + bw + 10), int32(y), int32(innerLeft + bw*2 + 10), int32(y + 36)}
			app.diagnosticRunRect = RECT{int32(innerLeft + bw*2 + 20), int32(y), int32(innerRight), int32(y + 36)}
		}
	}
	if app.section == 12 {
		app.checkBackRect = RECT{int32(innerLeft), int32(bodyTop + 18), int32(innerLeft + 120), int32(bodyTop + 54)}
		cardY := bodyTop + 94
		gap := 16
		cw := (innerContentW - gap) / 2
		app.checkTestRect = RECT{int32(innerLeft), int32(cardY), int32(innerLeft + cw), int32(cardY + 124)}
		app.checkDiagRect = RECT{int32(innerLeft + cw + gap), int32(cardY), int32(innerRight), int32(cardY + 124)}
	}

	// Footer for the basic task pages and the scenario editor.
	if hasTaskFooter(app.section) {
		footerY := h - 134
		btnGap := 10
		available := contentW - btnGap*3
		widths := []int{140, 150, 130, 140}
		total := 0
		for _, x := range widths {
			total += x
		}
		if total > available {
			each := available / 4
			for i := range widths {
				widths[i] = each
			}
		}
		x := pad
		app.startRect = RECT{int32(x), int32(footerY), int32(x + widths[0]), int32(footerY + 40)}
		x += widths[0] + btnGap
		app.saveTaskRect = RECT{int32(x), int32(footerY), int32(x + widths[1]), int32(footerY + 40)}
		x += widths[1] + btnGap
		app.cancelRect = RECT{int32(x), int32(footerY), int32(x + widths[2]), int32(footerY + 40)}
		x += widths[2] + btnGap
		app.postponeRect = RECT{int32(x), int32(footerY), int32(minInt(w-pad, x+widths[3])), int32(footerY + 40)}
	}
	// Native EDIT values are positioned in the same layout pass and remain visible during
	// Direct2D transitions. Modal sheets are custom Direct2D content, so child EDIT windows
	// must be explicitly hidden while a modal is on top (Win32 children otherwise punch
	// through the painted overlay due to their separate HWND z-order).
	if modalOverlayActive() || app.notificationPanelOpen {
		hideNativeInputs()
	}
}

func resourceTimelineTickCount() int {
	if app.settings.ResourceTimelineTicks < 2 || app.settings.ResourceTimelineTicks > 12 {
		return 6
	}
	return app.settings.ResourceTimelineTicks
}

func settingsVirtualContentHeight() int {
	switch app.settingsSubpage {
	case 0:
		return 8*uiMetricsDefault.SettingsRowStep + 40
	case 1:
		return 390
	case 2:
		return 0
	case 3:
		return 370
	case 4:
		if app.settingsCategory == 4 {
			return 430
		}
		return 275
	case 5:
		return 320
	case 6:
		return 150
	case 7:
		return 770
	}
	return 0
}

func modalOverlayActive() bool {
	return app.confirmSystemMode != 0 || app.confirmClearHistory || app.confirmClearNotifications || app.confirmDeleteIdx >= 0
}

func hideNativeInputs() {
	for _, h := range app.edits {
		if h != 0 {
			pShowWindow.Call(h, SW_HIDE)
		}
	}
}

func hideConditionEditorInputs() {
	for _, id := range []int{idCondThreshold, idCondHold, idCondText, idCondDelay} {
		if h := app.edits[id]; h != 0 {
			pShowWindow.Call(h, SW_HIDE)
		}
	}
}

func positionConditionEditorInputs(show bool) {
	if app.section != 8 {
		return
	}
	showControl := func(id int, visible bool) {
		h := app.edits[id]
		if h == 0 {
			return
		}
		if show && visible {
			pShowWindow.Call(h, SW_SHOW)
			pInvalidateRect.Call(h, 0, 0)
		} else {
			pShowWindow.Call(h, SW_HIDE)
		}
	}
	if conditionUsesThreshold(app.conditionDraft.Type) {
		r := app.whenFieldRect
		move(app.edits[idCondThreshold], int(r.Left)+4, int(r.Top)+6, max(1, int(r.Right-r.Left)-8), 18)
	}
	r := app.warningFieldRect
	move(app.edits[idCondHold], int(r.Left)+4, int(r.Top)+6, max(1, int(r.Right-r.Left)-8), 18)
	tr := app.timeFieldRects[0]
	move(app.edits[idCondText], int(tr.Left)+8, int(tr.Top)+5, max(40, int(tr.Right-tr.Left)-16), 22)
	dr := app.conditionDelayFieldRect
	move(app.edits[idCondDelay], int(dr.Left)+4, int(dr.Top)+6, max(1, int(dr.Right-dr.Left)-8), 18)
	showControl(idCondThreshold, conditionUsesThreshold(app.conditionDraft.Type))
	showControl(idCondHold, true)
	showControl(idCondText, true)
	showControl(idCondDelay, true)
}

func isTaskSection(section int) bool {
	if section == 4 {
		return app.processPickerMode != 1
	}
	// Simple task pages + advanced task editor and its child pages.
	return section == 0 || section == 1 || section == 2 || section == 6 || section == 7 || section == 8 || section == 9 || section == 11 || section == 12 || section == 14 || section == 15 || section == 16 || section == 17
}
func hasTaskFooter(section int) bool {
	return section == 0 || section == 1 || section == 2 || section == 7
}
func showsTaskChain(section int) bool {
	if section == 4 {
		return app.processPickerMode != 1
	}
	return section == 0 || section == 1 || section == 2 || section == 6
}

func closeSavedMenuImmediate() {
	app.savedMenuOpenIdx = -1
	app.savedMenuAnim = 0
	app.savedMenuTarget = 0
	app.savedMenuPendingClose = false
	app.savedPopupRect = RECT{}
	app.savedPopupPauseRect = RECT{}
	app.savedPopupEditRect = RECT{}
	app.savedPopupDuplicateRect = RECT{}
	app.savedPopupDeleteRect = RECT{}
}

func resetHoverState() {
	app.hoverKey = 0
	app.hoverRect = RECT{}
	app.hoverAnim = 0
}

func startPageAnimation() {
	if app.section != 5 {
		closeSavedMenuImmediate()
	}
	resetHoverState()
	app.pageAnimShownInputs = true
	if app.settings.AnimationMode == 2 {
		app.pageAnim = 1
		app.pageAnimStarted = time.Time{}
		return
	}
	app.pageAnim = 0
	app.pageAnimStarted = time.Now()
}

func startSubReveal() {
	resetHoverState()
	if app.settings.AnimationMode == 2 {
		app.subRevealAnim = 1
		return
	}
	app.subRevealAnim = 0
}

func rememberCurrentTaskLocation() {
	switch app.section {
	case 0, 1, 2:
		app.currentTaskKind = 0
		app.currentTaskSection = app.section
	case 7, 8, 9, 14, 15:
		app.currentTaskKind = 1
		app.currentTaskSection = 7
	}
	app.settings.TaskKind = app.currentTaskKind
}

func resumeCurrentTask() {
	if app.section == 10 {
		restoreCurrentInputTexts()
		app.editingSavedIdx = -1
	} else {
		syncFields()
	}
	app.settings.TaskKind = app.currentTaskKind
	if app.currentTaskKind == 1 {
		app.section = 7
		app.lastTaskSection = 2
	} else {
		sec := app.currentTaskSection
		if sec < 0 || sec > 2 {
			sec = 0
		}
		app.section = sec
		app.lastTaskSection = sec
	}
	app.taskMenuOpen, app.createTaskMenuOpen = false, false
	saveSettings()
	playUI(openSound)
	startPageAnimation()
	updateInputVisibility()
	invalidate(app.hwnd)
}

func subRevealOffset() int {
	t := clampFloat(app.subRevealAnim, 0, 1)
	e := 1 - (1-t)*(1-t)*(1-t)
	return int((1 - e) * 7)
}

func currentEditAnimationOffset() (int, int) {
	if app.settings.AnimationMode == 2 {
		return 0, 0
	}
	if app.pageAnim < 1 {
		return int((1 - pageEase()) * 20), 0
	}
	switch app.section {
	case 1, 3, 8, 9, 10:
		if app.subRevealAnim < 1 {
			return 0, subRevealOffset()
		}
	}
	return 0, 0
}

func shiftVisibleEditsForAnimation() {
	nx, ny := currentEditAnimationOffset()
	dx, dy := nx-editAnimOffsetX, ny-editAnimOffsetY
	if dx == 0 && dy == 0 || app.hwnd == 0 {
		editAnimOffsetX, editAnimOffsetY = nx, ny
		return
	}
	for _, h := range app.edits {
		if h == 0 {
			continue
		}
		if v, _, _ := pIsWindowVisible.Call(h); v == 0 {
			continue
		}
		var wr RECT
		if ok, _, _ := pGetWindowRect.Call(h, uintptr(unsafe.Pointer(&wr))); ok == 0 {
			continue
		}
		pt := POINT{X: wr.Left, Y: wr.Top}
		pScreenToClient.Call(app.hwnd, uintptr(unsafe.Pointer(&pt)))
		sc := uiScaleFactor040()
		pMoveWindow.Call(h, uintptr(int(pt.X)+int(float64(dx)*sc)), uintptr(int(pt.Y)+int(float64(dy)*sc)), uintptr(wr.Right-wr.Left), uintptr(wr.Bottom-wr.Top), 1)
	}
	editAnimOffsetX, editAnimOffsetY = nx, ny
}

func pageEase() float64 {
	t := clampFloat(app.pageAnim, 0, 1)
	// Smoothstep deliberately starts more gently than the old cubic ease-out.
	// It removes the visible first-frame "kick" when switching pages.
	return t * t * (3 - 2*t)
}

func inputTextRevealAmount() float64 {
	if app.settings.AnimationMode == 2 {
		return 1
	}
	a := 1.0
	if app.pageAnim < 1 {
		a = pageEase()
	}
	if app.subRevealAnim < 1 {
		t := clampFloat(app.subRevealAnim, 0, 1)
		sub := 1 - (1-t)*(1-t)
		if sub < a {
			a = sub
		}
	}
	return clampFloat(a, 0, 1)
}

func inputTextRevealColor() uint32 {
	return blendColor(surfaceButtonColor(), theme.text, inputTextRevealAmount())
}

func invalidateVisibleEdits() {
	for _, h := range app.edits {
		if h == 0 {
			continue
		}
		if v, _, _ := pIsWindowVisible.Call(h); v != 0 {
			pInvalidateRect.Call(h, 0, 1)
		}
	}
}

func updateInputVisibility() {
	if app.hwnd != 0 {
		layoutControls(app.hwnd)
	}
}

func paint(hwnd uintptr) {
	var ps PAINTSTRUCT
	hdc, _, _ := pBeginPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
	defer pEndPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
	var rc RECT
	pGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&rc)))
	if rc.Right <= rc.Left || rc.Bottom <= rc.Top {
		return
	}

	// Direct2D already owns a buffered render target. Do not allocate/copy a full
	// GDI bitmap on every frame: that 0.6.6 workaround added measurable latency and
	// was not the source of the catalogue flash. The actual issue was full Win32
	// relayout on every animation tick; 0.6.9 isolates that geometry and terminal-frame input restore instead.
	physicalRC := rc
	logicalRC := logicalClientRect040(rc)
	if d2dBegin(hdc, physicalRC) {
		d2dSetBaseScale040(float32(uiScaleFactor040()))
		drawMainToDC(hdc, logicalRC)
		d2dEnd()
		return
	}
	d2dSetBaseScale040(1)
	fill(hdc, physicalRC, theme.bg)
	drawMainToDC(hdc, physicalRC)
}

func drawMainToDC(hdc uintptr, rc RECT) {
	app.hoverSeen = false
	w := int(rc.Right)
	h := int(rc.Bottom)
	pad := 20
	drawBackground(hdc, rc)
	drawCustomTitleBar(hdc, rc)
	if app.miniMode {
		drawMiniMode(hdc, rc)
		return
	}

	// Main navigation: the label opens the default/current page, the adjacent ellipsis owns secondary actions.
	fill(hdc, RECT{0, 46, rc.Right, 102}, surfacePanelColor())
	taskActive := app.section != 3 && app.section != 18 && app.section != 19 && app.section != 20
	resourceActive := app.section == 18 || app.section == 19 || app.section == 20
	drawSplitNavButton(hdc, app.taskTabRect, app.taskMoreRect, "Планировщик", taskActive, app.taskMenuOpen)
	drawSplitNavButton(hdc, app.monitorTabRect, app.resourceMoreRect, "Ресурсы", resourceActive, app.resourceMenuOpen)
	drawNotificationButton(hdc)
	settingsColor := surfaceButtonColor()
	if app.section == 3 {
		settingsColor = blendColor(surfaceButtonColor(), theme.accent, .48)
	}
	gearHover := hoverAmount(app.settingsBtnRect)
	gearRect := expandRect(app.settingsBtnRect, int32(3*gearHover+.5))
	if gearHover > 0 {
		settingsColor = blendColor(settingsColor, theme.accent2, .10*gearHover)
	}
	roundFill(hdc, gearRect, settingsColor, 12)
	if gearHover > 0 && app.section != 3 && ui2d.active {
		d2dDrawRoundedOutline(gearRect, 12, float32(1.0+0.5*gearHover), blendColor(theme.border, theme.accent2, .55))
	}
	iconX := app.settingsBtnRect.Left + (app.settingsBtnRect.Right-app.settingsBtnRect.Left-28)/2
	iconY := app.settingsBtnRect.Top + (app.settingsBtnRect.Bottom-app.settingsBtnRect.Top-28)/2
	d2dDrawSettingsIconRotated(RECT{iconX, iconY, iconX + 28, iconY + 28}, 150*app.settingsHoverAnim)

	showChain := showsTaskChain(app.section)
	if showChain {
		labels := []string{"Действие", "Когда", "Дополнительно"}
		summaries := []string{actionSummary(), whenSummary(), extraSummary()}
		for i := 0; i < 3; i++ {
			r := app.chainRects[i]
			c := surfacePanelColor()
			activeChain := app.section == i || (app.section == 4 && ((app.processPickerMode == 0 && i == 2) || (app.processPickerMode == 2 && i == 1))) || (app.section == 6 && i == app.lastTaskSection) || ((app.section == 7 || app.section == 8 || app.section == 9) && i == 2)
			if activeChain {
				c = blendColor(surfacePanelColor(), theme.accent, .22)
			}
			rv, hv := hoverCardRect(r)
			if hv > 0 {
				c = blendColor(c, theme.accent2, .06*hv)
			}
			roundFill(hdc, rv, c, 14)
			if hv > 0 && !activeChain && ui2d.active {
				d2dDrawRoundedOutline(rv, 14, float32(1+0.4*hv), blendColor(theme.border, theme.accent2, .42))
			}
			drawText(hdc, labels[i], int(r.Left)+14, int(r.Top)+8, int(r.Right-r.Left)-28, 17, 12, 650, theme.accent2, DT_LEFT|DT_VCENTER|DT_SINGLELINE)
			drawText(hdc, summaries[i], int(r.Left)+14, int(r.Top)+28, int(r.Right-r.Left)-28, 20, 14, 500, theme.text, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
			if i < 2 {
				drawText(hdc, "›", int(r.Right)-2, int(r.Top)+14, 14, 24, 20, 500, theme.muted, DT_CENTER|DT_VCENTER|DT_SINGLELINE)
			}
		}
	}

	bodyTop := 110
	if showChain {
		bodyTop = 184
	}
	bodyBottom := h - 20
	if hasTaskFooter(app.section) {
		bodyBottom = h - 154
	}
	body := RECT{int32(pad), int32(bodyTop), int32(w - pad), int32(bodyBottom)}
	if ui2d.active && app.pageAnim < 1 {
		shift := float32((1 - pageEase()) * 26)
		d2dSetTranslation(shift, 0)
	}
	roundFill(hdc, body, surfacePanelColor(), 18)
	switch app.section {
	case 0:
		drawActionPage(hdc, body, w)
	case 1:
		drawWhenPage(hdc, body, w)
	case 2:
		drawExtraPage(hdc, body, w)
	case 3:
		drawSettingsPage(hdc, body, w)
	case 4:
		drawProcessesPage(hdc, body, w)
	case 5:
		drawSavedTasksPage(hdc, body, w)
	case 6:
		drawSaveTaskPage(hdc, body, w)
	case 7:
		drawScenarioPage(hdc, body, w)
	case 8:
		drawConditionEditor(hdc, body, w)
	case 9:
		drawStepEditor(hdc, body, w)
	case 10:
		drawSavedTaskEditor(hdc, body, w)
	case 11:
		drawDiagnosticPage(hdc, body, w)
	case 12:
		drawCheckPage(hdc, body, w)
	case 13:
		drawScenarioPage(hdc, body, w)
	case 14:
		drawBlockActionEditor(hdc, body, w)
	case 15:
		drawBlockWhenEditor(hdc, body, w)
	case 16:
		drawTemplates040(hdc, body)
	case 17:
		drawScenarioPreview040(hdc, body)
	case 18:
		drawResourceMonitor(hdc, body, w)
	case 19:
		drawAdvancedResourceMonitor(hdc, body, w)
	case 20:
		drawResourceStatistics(hdc, body, w)
	}

	if hasTaskFooter(app.section) {
		drawButton(hdc, app.startRect, "Запустить", true)
		drawButton(hdc, app.saveTaskRect, "Сохранить задачу", false)
		drawButton(hdc, app.cancelRect, "Отменить", false)
		if app.schedule.active {
			drawButton(hdc, app.postponeRect, "+10 минут", false)
		} else {
			drawButton(hdc, app.postponeRect, "Проверка", false)
		}
		statusY := h - 78
		statusColor := theme.muted
		if app.schedule.active {
			statusColor = theme.success
		}
		drawText(hdc, app.statusOrDefault(), pad, statusY, w-pad*2, 18, 12, 600, statusColor, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
		drawText(hdc, app.countdownOrDefault(), pad, statusY+18, 220, 36, 25, 450, theme.text, DT_LEFT|DT_VCENTER|DT_SINGLELINE)
		barLeft := pad + 240
		bar := RECT{int32(barLeft), int32(statusY + 32), int32(w - pad), int32(statusY + 41)}
		roundFill(hdc, bar, surfaceButtonColor(), 5)
		if app.progress > 0 {
			bw := int32(float64(bar.Right-bar.Left) * app.progress)
			if bw > 0 {
				roundFill(hdc, RECT{bar.Left, bar.Top, bar.Left + bw, bar.Bottom}, theme.accent, 5)
			}
		}
	}
	if ui2d.active {
		d2dResetTransform()
	}
	drawTaskNavigationMenu(hdc)
	drawResourceNavigationMenu(hdc)
	drawNotificationPanel(hdc)
	if app.confirmSystemMode != 0 {
		drawSystemProcessConfirmation(hdc, rc)
	}
}

func drawSplitNavButton(hdc uintptr, labelRect, moreRect RECT, label string, active, menuOpen bool) {
	group := unionRect(labelRect, moreRect)
	if group.Right <= group.Left {
		return
	}
	base := surfaceButtonColor()
	if active {
		base = blendColor(base, theme.accent, .58)
	}
	// One rounded body; only the hovered half gets a subtle overlay.
	roundFill(hdc, group, base, 10)
	labelHover := hoverAmount(labelRect)
	moreHover := hoverAmount(moreRect)
	if labelHover > 0 && !active {
		roundFill(hdc, labelRect, blendColor(base, theme.accent2, .10*labelHover), 9)
	}
	if moreHover > 0 || menuOpen {
		moreColor := blendColor(base, theme.accent2, .10*maxFloat(moreHover, boolFloat(menuOpen)))
		roundFill(hdc, moreRect, moreColor, 9)
	}
	if ui2d.active {
		d2dDrawLine(float32(moreRect.Left), float32(group.Top+7), float32(moreRect.Left), float32(group.Bottom-7), .7, blendColor(theme.border, theme.muted, .24))
	}
	drawText(hdc, label, int(labelRect.Left), int(labelRect.Top), int(labelRect.Right-labelRect.Left), int(labelRect.Bottom-labelRect.Top), 12, 650, theme.text, DT_CENTER|DT_VCENTER|DT_SINGLELINE)
	drawText(hdc, "⋮", int(moreRect.Left), int(moreRect.Top), int(moreRect.Right-moreRect.Left), int(moreRect.Bottom-moreRect.Top), 14, 700, theme.text, DT_CENTER|DT_VCENTER|DT_SINGLELINE)
}

func boolFloat(v bool) float64 {
	if v {
		return 1
	}
	return 0
}
func drawTaskNavigationMenu(hdc uintptr) {
	if !app.taskMenuOpen {
		return
	}
	mainPanel, subPanel := taskMenuPanelRects()
	roundFill(hdc, mainPanel, surfacePanelColor(), 10)
	if app.createTaskMenuOpen && subPanel.Right > subPanel.Left {
		roundFill(hdc, subPanel, surfacePanelColor(), 10)
	}
	drawingTaskNavigationMenu = true
	drawMenuButton(hdc, app.blockTaskTabRect, "Создать задачу", false)
	drawMenuButton(hdc, app.savedTabRect, "Сохранённые задачи", false)
	if app.createTaskMenuOpen {
		drawMenuButton(hdc, app.taskKindRects[0], "Простая", app.settings.TaskKind == 0 && isTaskSection(app.section))
		drawMenuButton(hdc, app.taskKindRects[1], "Продвинутая", app.settings.TaskKind == 1 && isTaskSection(app.section))
		drawMenuButton(hdc, app.taskKindRects[2], "Из шаблона", app.section == 16)
	}
	drawingTaskNavigationMenu = false
}

// Task-navigation dropdowns deliberately do not grow outside their hit rectangles.
// This keeps the visual button and clickable area identical and removes the old
// "looks clickable but sometimes does nothing" edge around the pop-up animation.
func drawMenuButton(hdc uintptr, r RECT, text string, active bool) {
	if r.Right <= r.Left || r.Bottom <= r.Top {
		return
	}
	h := hoverAmount(r)
	c := surfaceButtonColor()
	if active {
		c = theme.accent
	} else if h > 0 {
		c = blendColor(c, theme.accent2, .10*h)
	}
	drawingInteractiveSurface = true
	roundFill(hdc, r, c, 9)
	drawingInteractiveSurface = false
	if h > 0 && !active && ui2d.active {
		d2dDrawRoundedOutline(r, 9, float32(1+0.35*h), blendColor(theme.border, theme.accent2, .48))
	}
	drawText(hdc, text, int(r.Left), int(r.Top), int(r.Right-r.Left), int(r.Bottom-r.Top), 12, 650, theme.text, DT_CENTER|DT_VCENTER|DT_SINGLELINE)
}

func taskMenuPanelRects() (RECT, RECT) {
	if !app.taskMenuOpen {
		return RECT{}, RECT{}
	}
	main := unionRect(app.blockTaskTabRect, app.savedTabRect)
	if main.Right > main.Left {
		main = expandRect(main, 4)
	}
	sub := RECT{}
	if app.createTaskMenuOpen {
		sub = unionRect(unionRect(app.taskKindRects[0], app.taskKindRects[1]), app.taskKindRects[2])
		if sub.Right > sub.Left {
			sub = expandRect(sub, 4)
		}
	}
	return main, sub
}

func pointInTaskMenuPopup(x, y int32) bool {
	main, sub := taskMenuPanelRects()
	return pointIn(main, x, y) || pointIn(sub, x, y)
}

func drawResourceNavigationMenu(hdc uintptr) {
	if !app.resourceMenuOpen {
		return
	}
	panel := unionRect(app.resourceAdvancedMenuRect, app.resourceStatsMenuRect)
	if panel.Right > panel.Left {
		roundFill(hdc, expandRect(panel, 4), surfacePanelColor(), 10)
	}
	drawingTaskNavigationMenu = true
	drawMenuButton(hdc, app.resourceAdvancedMenuRect, "Продвинутый монитор", app.section == 19)
	drawMenuButton(hdc, app.resourceStatsMenuRect, "Статистика за период", app.section == 20)
	drawingTaskNavigationMenu = false
}

func resourceMenuPanelRect() RECT {
	if !app.resourceMenuOpen {
		return RECT{}
	}
	r := unionRect(app.resourceAdvancedMenuRect, app.resourceStatsMenuRect)
	if r.Right > r.Left {
		r = expandRect(r, 4)
	}
	return r
}

func unionRect(a, b RECT) RECT {
	if a.Right <= a.Left {
		return b
	}
	if b.Right <= b.Left {
		return a
	}
	return RECT{min32(a.Left, b.Left), min32(a.Top, b.Top), max32(a.Right, b.Right), max32(a.Bottom, b.Bottom)}
}
func min32(a, b int32) int32 {
	if a < b {
		return a
	}
	return b
}
func max32(a, b int32) int32 {
	if a > b {
		return a
	}
	return b
}

func drawBackground(hdc uintptr, rc RECT) {
	fill(hdc, rc, theme.bg)
	if !ui2d.active {
		return
	}
	w, h := float32(rc.Right), float32(rc.Bottom)
	switch app.settings.Background {
	case 1: // soft glow
		c1 := blendColor(theme.bg, theme.accent, .10)
		c2 := blendColor(theme.bg, theme.accent2, .06)
		d2dFillEllipse(w*.82, h*.18, w*.34, h*.30, c1)
		d2dFillEllipse(w*.18, h*.86, w*.28, h*.22, c2)
	case 2: // subtle grid
		grid := blendColor(theme.bg, theme.border, .32)
		for x := float32(0); x < w; x += 32 {
			d2dDrawLine(x, 0, x, h, 1, grid)
		}
		for y := float32(0); y < h; y += 32 {
			d2dDrawLine(0, y, w, y, 1, grid)
		}
	case 3: // quiet star field
		star := blendColor(theme.bg, theme.accent2, .48)
		for i := 0; i < 42; i++ {
			x := float32((i*97 + 31) % max(1, int(w)))
			y := float32((i*53 + 17) % max(1, int(h)))
			r := float32(1.2)
			if i%7 == 0 {
				r = 2.0
			}
			d2dFillEllipse(x, y, r, r, star)
		}
	case 4: // aurora: broad, low-contrast coloured veils
		a := blendColor(theme.bg, theme.accent, .13)
		b := blendColor(theme.bg, theme.accent2, .10)
		c := blendColor(theme.bg, theme.success, .07)
		d2dFillEllipse(w*.18, h*.05, w*.55, h*.20, a)
		d2dFillEllipse(w*.64, h*.18, w*.60, h*.18, b)
		d2dFillEllipse(w*.44, h*.96, w*.50, h*.17, c)
	case 5: // nebula: layered translucent colour clouds
		d2dFillEllipseOpacity(w*.18, h*.22, w*.34, h*.27, blendColor(theme.accent, theme.bg, .32), .30)
		d2dFillEllipseOpacity(w*.73, h*.28, w*.39, h*.30, blendColor(theme.accent2, theme.bg, .26), .26)
		d2dFillEllipseOpacity(w*.48, h*.78, w*.46, h*.24, blendColor(theme.success, theme.bg, .20), .18)
		d2dFillEllipseOpacity(w*.42, h*.48, w*.24, h*.18, theme.text, .035)
	}
}

func drawCustomTitleBar(hdc uintptr, rc RECT) {
	bar := blendColor(theme.bg, surfacePanelColor(), .74)
	fill(hdc, app.titleBarRect, bar)
	d2dDrawAppIcon(RECT{14, 10, 40, 36})
	drawText(hdc, "PowerPilot", 48, 0, 180, 46, 14, 650, theme.text, DT_LEFT|DT_VCENTER|DT_SINGLELINE)
	buttons := []RECT{app.miniBtnRect, app.minBtnRect, app.maxBtnRect, app.closeBtnRect}
	for i, r := range buttons {
		c := bar
		if app.hoverTitle == i {
			c = blendColor(bar, surfaceButtonColor(), .82)
		}
		if i == 3 && app.hoverTitle == 3 {
			c = theme.danger
		}
		fill(hdc, r, c)
		drawCaptionGlyph(hdc, i, r)
	}
}

func drawCaptionGlyph(hdc uintptr, kind int, r RECT) {
	cx := float32(r.Left+r.Right) / 2
	cy := float32(r.Top+r.Bottom) / 2
	c := theme.text
	if ui2d.active {
		// Keep the compact vector caption set; only the pin uses the supplied bitmap.
		if kind == 0 && app.miniMode {
			size := int32(20)
			x := r.Left + (r.Right-r.Left-size)/2
			y := r.Top + (r.Bottom-r.Top-size)/2
			if d2dDrawCaptionIcon(5, RECT{x, y, x + size, y + size}) {
				return
			}
		}
		switch kind {
		case 0:
			if app.miniMode {
				// Push-pin: dedicated Always on Top control, visually close to 📌.
				pinColor := c
				if app.settings.AlwaysOnTopMini {
					pinColor = theme.accent2
				}
				cap := RECT{int32(cx - 5), int32(cy - 6), int32(cx + 5), int32(cy - 2)}
				d2dDrawRoundedOutline(cap, 2, 1.35, pinColor)
				d2dDrawLine(cx-3.5, cy-1.5, cx+3.5, cy-1.5, 1.35, pinColor)
				d2dDrawLine(cx, cy-1.5, cx, cy+5.5, 1.35, pinColor)
				d2dDrawLine(cx, cy+5.5, cx-1.5, cy+3.4, 1.15, pinColor)
			} else {
				box := RECT{int32(cx - 6), int32(cy - 5), int32(cx + 6), int32(cy + 5)}
				d2dDrawRoundedOutline(box, 1.5, 1.25, c)
				d2dDrawLine(cx-3.5, cy-1, cx+3.5, cy-1, 1.25, c)
				d2dDrawLine(cx-3.5, cy+2.5, cx+1.5, cy+2.5, 1.25, c)
			}
		case 1: // minimize
			d2dDrawLine(cx-6, cy+3, cx+6, cy+3, 1.35, c)
		case 2: // maximize / restore; in mini mode this is explicit "expand back".
			if app.miniMode {
				// Two outward corners are more explicit than the ordinary maximize square.
				d2dDrawLine(cx-6, cy-1, cx-6, cy-6, 1.3, c)
				d2dDrawLine(cx-6, cy-6, cx-1, cy-6, 1.3, c)
				d2dDrawLine(cx+6, cy+1, cx+6, cy+6, 1.3, c)
				d2dDrawLine(cx+1, cy+6, cx+6, cy+6, 1.3, c)
				d2dDrawLine(cx-3, cy-3, cx+3, cy+3, 1.2, c)
			} else if z, _, _ := pIsZoomed.Call(app.hwnd); z != 0 {
				d2dDrawRectOutline(RECT{int32(cx - 4), int32(cy - 6), int32(cx + 6), int32(cy + 4)}, 1.2, c)
				d2dDrawRectOutline(RECT{int32(cx - 6), int32(cy - 4), int32(cx + 4), int32(cy + 6)}, 1.2, c)
			} else {
				d2dDrawRectOutline(RECT{int32(cx - 5), int32(cy - 5), int32(cx + 5), int32(cy + 5)}, 1.3, c)
			}
		case 3: // close
			d2dDrawLine(cx-5, cy-5, cx+5, cy+5, 1.35, c)
			d2dDrawLine(cx+5, cy-5, cx-5, cy+5, 1.35, c)
		}
		return
	}
	glyphs := []string{"▣", "—", "□", "×"}
	if kind >= 0 && kind < len(glyphs) {
		drawText(hdc, glyphs[kind], int(r.Left), int(r.Top), int(r.Right-r.Left), int(r.Bottom-r.Top), 14, 500, c, DT_CENTER|DT_VCENTER|DT_SINGLELINE)
	}
}

func drawMiniMode(hdc uintptr, rc RECT) {
	pad := 20
	w := int(rc.Right - rc.Left)
	leftW := max(160, minInt(260, (w-pad*2-18)/2))
	rightX := pad + leftW + 18
	if app.settings.MiniShowTask {
		status := app.statusOrDefault()
		if !app.schedule.active {
			status = "Нет активной задачи"
		}
		drawText(hdc, status, pad, 51, leftW, 16, 10, 600, theme.muted, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
	}
	if app.settings.MiniShowCountdown {
		drawText(hdc, app.countdownOrDefault(), pad, 67, leftW, 32, 23, 500, theme.text, DT_LEFT|DT_VCENTER|DT_SINGLELINE)
	}
	if app.settings.MiniShowStep {
		vs := getExecVisual040()
		step := "Ожидание условия"
		if vs.Running && vs.Current >= 0 {
			steps := currentScenarioSteps()
			if vs.Current < len(steps) {
				step = stepSummary(steps[vs.Current])
			}
		} else if app.schedule.active {
			step = "Задача активна"
		} else {
			step = "Нет активного шага"
		}
		drawText(hdc, "Шаг · "+step, rightX, 51, max(80, w-rightX-20), 18, 10, 600, theme.text, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
	}
	if app.settings.MiniShowMetrics {
		m := metricsSnapshot()
		metric := fmt.Sprintf("CPU %.0f%% · Сеть %.0f КБ/с", m.CPU, m.NetworkKBps)
		drawText(hdc, metric, rightX, 72, max(80, w-rightX-20), 18, 9, 500, theme.muted, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
	}
	if app.progress > 0 {
		bar := RECT{int32(rightX), 92, rc.Right - 20, 98}
		roundFill(hdc, bar, surfaceButtonColor(), 3)
		bw := int32(float64(bar.Right-bar.Left) * app.progress)
		if bw > 0 {
			roundFill(hdc, RECT{bar.Left, bar.Top, bar.Left + bw, bar.Bottom}, theme.accent, 3)
		}
	}
	drawButton(hdc, app.miniCancelRect, "Отменить", false)
	drawButton(hdc, app.miniPostponeRect, "+10 минут", true)
}

func drawActionPage(hdc uintptr, body RECT, w int) {
	drawText(hdc, "Выберите действие", int(body.Left)+18, int(body.Top)+16, 300, 26, 19, 650, theme.text, DT_LEFT|DT_VCENTER|DT_SINGLELINE)
	names := []string{"Выключение", "Перезагрузка", "Сон", "Гибернация"}
	icons := []string{"⏻", "↻", "☾", "▣"}
	for i := 0; i < 4; i++ {
		r := app.actionRects[i]
		c := blendColor(surfaceButtonColor(), theme.accent, app.actionAnim[i])
		rv, hv := hoverCardRect(r)
		if hv > 0 {
			c = blendColor(c, theme.accent2, .08*hv)
		}
		roundFill(hdc, rv, c, 14)
		if hv > 0 && i != app.selectedAction && ui2d.active {
			d2dDrawRoundedOutline(rv, 14, float32(1+0.5*hv), blendColor(theme.border, theme.accent2, .48))
		}
		iconW := 42
		drawText(hdc, icons[i], int(r.Left)+14, int(r.Top), iconW, int(r.Bottom-r.Top), 22, 600, theme.text, DT_CENTER|DT_VCENTER|DT_SINGLELINE)
		drawText(hdc, names[i], int(r.Left)+14+iconW+8, int(r.Top), int(r.Right-r.Left)-iconW-42, int(r.Bottom-r.Top), 14, 650, theme.text, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
	}
}

func drawWhenPage(hdc uintptr, body RECT, w int) {
	drawText(hdc, "Когда выполнить", int(body.Left)+18, int(body.Top)+16, 260, 26, 19, 650, theme.text, DT_LEFT|DT_VCENTER|DT_SINGLELINE)
	modes := []string{"Таймер", "Дата и время", "Простой", "После процесса", "Расписание"}
	for i := 0; i < 5; i++ {
		r := app.modeRects[i]
		c := blendColor(surfaceButtonColor(), theme.accent, app.modeAnim[i])
		rv, hv := hoverCardRect(r)
		if hv > 0 {
			c = blendColor(c, theme.accent2, .08*hv)
		}
		roundFill(hdc, rv, c, 11)
		if hv > 0 && i != app.selectedMode && ui2d.active {
			d2dDrawRoundedOutline(rv, 11, float32(1+0.45*hv), blendColor(theme.border, theme.accent2, .45))
		}
		drawText(hdc, modes[i], int(r.Left), int(r.Top), int(r.Right-r.Left), int(r.Bottom-r.Top), 12, 600, theme.text, DT_CENTER|DT_VCENTER|DT_SINGLELINE)
	}
	formReveal := ui2d.active && app.pageAnim >= 1 && app.subRevealAnim < 1
	if formReveal {
		d2dSetTranslation(0, float32(subRevealOffset()))
	}
	label := ""
	switch app.selectedMode {
	case 0:
		label = "Укажите задержку"
	case 1:
		label = "Укажите дату и время"
	case 2:
		label = "Секунд без активности"
	case 3:
		label = "Процесс, завершения которого нужно дождаться"
	case 4:
		label = "Время запуска расписания · ЧЧ:ММ"
	}
	uiDrawWhenFieldChrome(hdc, app.selectedMode, app.modeRects[:5], body, label)
	if app.selectedMode == 3 {
		drawButton(hdc, app.pickRect, "Выбрать процесс", false)
		if strings.TrimSpace(app.settings.WatchProcess) != "" {
			drawSmallGlyphButton(hdc, app.processClearRect, "×", theme.danger)
		}
	}
	if app.selectedMode == 4 {
		kindNames := []string{"Каждый день", "Будни", "Выбранные дни"}
		for i, r := range app.recurrenceKindRects {
			c := surfaceButtonColor()
			active := app.settings.Recurrence.Kind == i
			if active {
				c = blendColor(c, theme.accent, .36)
			}
			rv, hv := hoverCardRect(r)
			if hv > 0 && !active {
				c = blendColor(c, theme.accent2, .07*hv)
			}
			roundFill(hdc, rv, c, 10)
			if hv > 0 && !active && ui2d.active {
				d2dDrawRoundedOutline(rv, 10, float32(1+.4*hv), blendColor(theme.border, theme.accent2, .44))
			}
			drawText(hdc, kindNames[i], int(r.Left), int(r.Top), int(r.Right-r.Left), int(r.Bottom-r.Top), 12, 600, theme.text, DT_CENTER|DT_VCENTER|DT_SINGLELINE)
		}
		dayNames := []string{"Пн", "Вт", "Ср", "Чт", "Пт", "Сб", "Вс"}
		for i, r := range app.recurrenceDayRects {
			active := app.settings.Recurrence.Days[i]
			if app.settings.Recurrence.Kind == 1 {
				active = i < 5
			}
			if app.settings.Recurrence.Kind == 0 {
				active = true
			}
			// Standard PowerPilot hover/pop-up language. We call this the
			// "hover-акцент": slight lift + outline without changing hitbox.
			drawSelectableButton(hdc, r, dayNames[i], active)
		}
		if app.recurrenceEnabledRect.Right > app.recurrenceEnabledRect.Left {
			drawToggle(hdc, app.recurrenceEnabledRect, app.settings.Recurrence.Enabled)
			drawText(hdc, "Автозапуск по расписанию", int(app.recurrenceEnabledRect.Right)+12, int(app.recurrenceEnabledRect.Top), int(body.Right-app.recurrenceEnabledRect.Right)-24, 28, 11, 550, theme.text, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
		}
	}
	if formReveal {
		d2dResetTransform()
	}
}

func drawExtraPage(hdc uintptr, body RECT, w int) {
	drawText(hdc, "Параметры простой задачи", int(body.Left)+18, int(body.Top)+16, 360, 26, 19, 650, theme.text, DT_LEFT|DT_VCENTER|DT_SINGLELINE)
	contentY := int(body.Top) + 70
	drawToggle(hdc, app.closeBeforeRect, app.settings.CloseBefore)
	drawText(hdc, "Закрывать выбранные процессы перед действием", int(app.closeBeforeRect.Right)+12, contentY, int(body.Right-app.closeBeforeRect.Right)-28, 26, 13, 500, theme.text, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
	drawButton(hdc, app.pickRect, "Выбрать процессы", false)
	drawText(hdc, "Закрыть "+processCountPhrase(len(app.settings.Processes)), int(app.pickRect.Right)+16, int(app.pickRect.Top), max(120, int(body.Right-app.pickRect.Right)-30), int(app.pickRect.Bottom-app.pickRect.Top), 12, 500, theme.muted, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
	pr, fr, sr := uiInlineNumberLayout("Предупреждение за", "секунд", int(body.Left)+18, contentY+116, int(body.Right)-18, 3)
	uiDrawInlineNumber(hdc, "Предупреждение за", "секунд", pr, fr, sr)
	drawText(hdc, "Для условий, цепочек и дополнительных шагов используйте вкладку «Продвинутая» сверху.", int(body.Left)+18, contentY+166, int(body.Right-body.Left)-36, 36, 10, 400, theme.muted, DT_LEFT|DT_VCENTER)
}

func drawSettingsPage(hdc uintptr, body RECT, w int) {
	drawText(hdc, "Настройки", int(body.Left)+18, int(body.Top)+16, 240, 28, 20, 650, theme.text, DT_LEFT|DT_VCENTER|DT_SINGLELINE)
	tabNames := []string{"Общие", "Вид и интерфейс", "Звук", "Защита", "Компоненты и обновления", "Данные", "История"}
	for i, r := range app.settingsTabs {
		c := surfaceButtonColor()
		if i == app.settingsCategory {
			c = blendColor(c, theme.accent, .58)
		}
		rv, hv := hoverCardRect(r)
		if hv > 0 {
			c = blendColor(c, theme.accent2, .07*hv)
		}
		roundFill(hdc, rv, c, 9)
		if hv > 0 && i != app.settingsCategory && ui2d.active {
			d2dDrawRoundedOutline(rv, 9, float32(1+0.4*hv), blendColor(theme.border, theme.accent2, .42))
		}
		fontSize := 8
		flags := uint32(DT_CENTER | DT_VCENTER | DT_WORDBREAK)
		if uiTextWidth(tabNames[i], 10, 650)+12 <= int(r.Right-r.Left) {
			fontSize = 10
			flags = DT_CENTER | DT_VCENTER | DT_SINGLELINE
		} else if uiTextWidth(tabNames[i], 9, 650)+10 <= int(r.Right-r.Left) {
			fontSize = 9
			flags = DT_CENTER | DT_VCENTER | DT_SINGLELINE
		}
		drawText(hdc, tabNames[i], int(r.Left)+3, int(r.Top)+2, int(r.Right-r.Left)-6, int(r.Bottom-r.Top)-4, fontSize, 650, theme.text, flags)
	}
	if app.settingsCategory == 1 {
		drawSelectableButton(hdc, app.settingsSectionRects[0], "Оформление", app.settingsSubpage == 1)
		drawSelectableButton(hdc, app.settingsSectionRects[1], "Интерфейс", app.settingsSubpage == 7)
	} else if app.settingsCategory == 5 {
		drawSelectableButton(hdc, app.settingsSectionRects[0], "Управление данными", app.settingsSubpage == 4)
		drawSelectableButton(hdc, app.settingsSectionRects[1], "Статистика", app.settingsSubpage == 3)
	}
	settingsClip := RECT{body.Left, int32(app.settingsContentTop + int(app.settingsScrollPx)), body.Right - 12, body.Bottom - 32}
	if ui2d.active {
		d2dPushClip(settingsClip)
	}
	settingsReveal := ui2d.active && app.pageAnim >= 1 && app.subRevealAnim < 1
	if settingsReveal {
		d2dSetTranslation(0, float32(subRevealOffset()))
	}
	switch app.settingsSubpage {
	case 0:
		drawGeneralSettings(hdc, body)
	case 1:
		drawAppearanceSettings(hdc, body)
	case 2:
		drawHistorySettings(hdc, body)
	case 3:
		drawStatisticsSettings(hdc, body)
	case 4:
		if app.settingsCategory == 4 {
			drawComponentsSettings(hdc, body)
		} else {
			drawDataSettings(hdc, body)
		}
	case 5:
		drawSafetySettings(hdc, body)
	case 6:
		drawSoundSettings(hdc, body)
	case 7:
		drawInterfaceSettings040(hdc, body)
	}
	if settingsReveal {
		d2dResetTransform()
	}
	if ui2d.active {
		d2dPopClip()
	}
	if app.settingsScrollMax > 0 {
		drawScrollBar(hdc, app.settingsScrollTrack, app.settingsScrollThumb)
	}
	versionY := int(body.Bottom) - 28
	drawText(hdc, "PowerPilot "+currentPowerPilotVersion(), int(body.Left)+20, versionY, int(body.Right-body.Left)-40, 20, 11, 550, theme.muted, DT_RIGHT|DT_VCENTER|DT_SINGLELINE)
	if app.confirmClearHistory {
		drawHistoryClearConfirmation(hdc, body)
	}
}

func drawGeneralSettings(hdc uintptr, body RECT) {
	baseY := app.settingsContentTop
	items := []struct {
		r           RECT
		enabled     bool
		title, desc string
	}{
		{app.autoRect, app.settings.AutoStart, "Запускать вместе с Windows", "PowerPilot запускается при входе в систему."},
		{app.trayRect, app.settings.MinimizeToTray, "Сворачивать в трей при закрытии", "Закрытие скрывает окно, не отменяя задачу."},
		{app.notificationsRect, app.settings.Notifications, "Уведомления Windows", "Предупреждения, автозапуски и защитные блокировки."},
	}
	for i, it := range items {
		y := baseY + i*uiMetricsDefault.SettingsRowStep
		drawToggle(hdc, it.r, it.enabled)
		drawText(hdc, it.title, int(it.r.Right)+12, y-2, int(body.Right-it.r.Right)-30, 19, 13, 600, theme.text, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
		drawText(hdc, it.desc, int(it.r.Right)+12, y+17, int(body.Right-it.r.Right)-30, 16, 10, 400, theme.muted, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
	}
	drawToggle(hdc, app.lockMinimumRect, app.settings.LockMinimumSize)
	drawText(hdc, "Зафиксировать минимальный размер окна", int(app.lockMinimumRect.Right)+12, int(app.lockMinimumRect.Top)-2, int(body.Right-app.lockMinimumRect.Right)-28, 19, 12, 600, theme.text, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
	drawText(hdc, "Окно остаётся в минимальном размере PowerPilot.", int(app.lockMinimumRect.Right)+12, int(app.lockMinimumRect.Top)+16, int(body.Right-app.lockMinimumRect.Right)-28, 16, 10, 400, theme.muted, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
	drawToggle(hdc, app.lockCurrentRect, app.settings.LockCurrentSize)
	drawText(hdc, "Зафиксировать текущий размер окна", int(app.lockCurrentRect.Right)+12, int(app.lockCurrentRect.Top)-2, int(body.Right-app.lockCurrentRect.Right)-28, 19, 12, 600, theme.text, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
	drawText(hdc, "Текущие ширина и высота сохраняются и блокируют resize.", int(app.lockCurrentRect.Right)+12, int(app.lockCurrentRect.Top)+16, int(body.Right-app.lockCurrentRect.Right)-28, 16, 10, 400, theme.muted, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
	drawToggle(hdc, app.wakeScheduledRect, app.settings.WakeScheduledTasks)
	lineX := int(app.wakeScheduledRect.Right) + 12
	pr, fr, sr := uiInlineNumberLayout("Пробуждать ПК по расписанию за", "мин", lineX, int(app.wakeScheduledRect.Top)+3, int(body.Right)-18, 2)
	uiDrawInlineNumber(hdc, "Пробуждать ПК по расписанию за", "мин", pr, fr, sr)
	drawText(hdc, "Wake timer Windows", lineX, int(app.wakeScheduledRect.Top)+24, int(body.Right)-lineX-24, 15, 9, 400, theme.muted, DT_LEFT|DT_VCENTER|DT_SINGLELINE)
	drawToggle(hdc, app.hotkeysRect, app.settings.GlobalHotkeys)
	drawText(hdc, "Глобальные горячие клавиши", int(app.hotkeysRect.Right)+12, int(app.hotkeysRect.Top)-2, int(body.Right-app.hotkeysRect.Right)-28, 19, 12, 600, theme.text, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
	drawText(hdc, "Ctrl+Alt+P — открыть PowerPilot · Ctrl+Alt+X — отменить активную задачу", int(app.hotkeysRect.Right)+12, int(app.hotkeysRect.Top)+16, int(body.Right-app.hotkeysRect.Right)-28, 16, 9, 400, theme.muted, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
	drawToggle(hdc, app.hideZeroResourceProcessesRect, app.settings.HideZeroResourceProcesses)
	drawText(hdc, "Скрывать процессы без обнаруженного потребления ресурсов", int(app.hideZeroResourceProcessesRect.Right)+12, int(app.hideZeroResourceProcessesRect.Top)-2, int(body.Right-app.hideZeroResourceProcessesRect.Right)-28, 19, 11, 600, theme.text, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
	drawText(hdc, "Применяется к продвинутому монитору процессов.", int(app.hideZeroResourceProcessesRect.Right)+12, int(app.hideZeroResourceProcessesRect.Top)+16, int(body.Right-app.hideZeroResourceProcessesRect.Right)-28, 16, 9, 400, theme.muted, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
}

func drawSoundSettings(hdc uintptr, body RECT) {
	baseY := app.settingsContentTop + 6
	drawToggle(hdc, app.soundsRect, app.settings.Sounds)
	drawText(hdc, "Звуки интерфейса", int(app.soundsRect.Right)+12, int(app.soundsRect.Top)-2, int(body.Right-app.soundsRect.Right)-30, 20, 13, 600, theme.text, DT_LEFT|DT_VCENTER|DT_SINGLELINE)
	drawText(hdc, "Короткие звуки выбора, переходов и выполнения.", int(app.soundsRect.Right)+12, int(app.soundsRect.Top)+17, int(body.Right-app.soundsRect.Right)-30, 16, 10, 400, theme.muted, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
	drawText(hdc, "Громкость интерфейса", int(body.Left)+18, baseY+64, 220, 20, 13, 600, theme.text, DT_LEFT|DT_VCENTER|DT_SINGLELINE)
	if app.volumeTrackRect.Right > app.volumeTrackRect.Left {
		roundFill(hdc, app.volumeTrackRect, surfaceButtonColor(), 4)
		filled := app.volumeTrackRect
		filled.Right = app.volumeKnobRect.Left + (app.volumeKnobRect.Right-app.volumeKnobRect.Left)/2
		if filled.Right > filled.Left {
			roundFill(hdc, filled, theme.accent, 4)
		}
		d2dFillEllipse(float32((app.volumeKnobRect.Left+app.volumeKnobRect.Right)/2), float32((app.volumeKnobRect.Top+app.volumeKnobRect.Bottom)/2), 9, 9, theme.text)
		d2dFillEllipse(float32((app.volumeKnobRect.Left+app.volumeKnobRect.Right)/2), float32((app.volumeKnobRect.Top+app.volumeKnobRect.Bottom)/2), 5, 5, theme.accent)
	}
	if app.volumeValueRect.Right > app.volumeValueRect.Left {
		roundFill(hdc, app.volumeValueRect, surfaceButtonColor(), 8)
		drawText(hdc, "%", int(app.volumeValueRect.Right)-22, int(app.volumeValueRect.Top), 18, int(app.volumeValueRect.Bottom-app.volumeValueRect.Top), 11, 600, theme.accent2, DT_CENTER|DT_VCENTER|DT_SINGLELINE)
	}
}

func drawInterfaceSettings040(hdc uintptr, body RECT) {
	drawText(hdc, "Мини-режим", int(body.Left)+18, int(app.miniAlwaysTopRect.Top)-26, 220, 20, 13, 650, theme.text, DT_LEFT|DT_VCENTER|DT_SINGLELINE)
	drawToggle(hdc, app.miniAlwaysTopRect, app.settings.AlwaysOnTopMini)
	drawText(hdc, "Поверх остальных окон в мини-режиме", int(app.miniAlwaysTopRect.Right)+12, int(app.miniAlwaysTopRect.Top)-1, int(body.Right-app.miniAlwaysTopRect.Right)-30, 20, 12, 600, theme.text, DT_LEFT|DT_VCENTER|DT_SINGLELINE)
	drawText(hdc, "Что показывать", int(body.Left)+18, int(app.miniOptionRects[0].Top)-26, 200, 20, 12, 650, theme.text, DT_LEFT|DT_VCENTER|DT_SINGLELINE)
	names := []string{"Задача", "Таймер", "Текущий шаг", "Метрики"}
	vals := []bool{app.settings.MiniShowTask, app.settings.MiniShowCountdown, app.settings.MiniShowStep, app.settings.MiniShowMetrics}
	for i, r := range app.miniOptionRects {
		c := surfaceButtonColor()
		if vals[i] {
			c = blendColor(c, theme.accent, .48)
		}
		rv, hv := hoverCardRect(r)
		if hv > 0 && !vals[i] {
			c = blendColor(c, theme.accent2, .08*hv)
		}
		roundFill(hdc, rv, c, 9)
		drawText(hdc, names[i], int(r.Left), int(r.Top), int(r.Right-r.Left), int(r.Bottom-r.Top), 10, 650, theme.text, DT_CENTER|DT_VCENTER|DT_SINGLELINE)
	}
	drawText(hdc, "Размер мини-приложения", int(body.Left)+18, int(app.miniSizeRects[0].Top)-26, 240, 20, 12, 650, theme.text, DT_LEFT|DT_VCENTER|DT_SINGLELINE)
	sizeLabels := []string{"Компактный", "Обычный", "Крупный"}
	sizeValues := []int{90, 100, 120}
	for i, r := range app.miniSizeRects {
		c := surfaceButtonColor()
		if miniScalePercent040() == sizeValues[i] {
			c = blendColor(c, theme.accent, .48)
		}
		rv, hv := hoverCardRect(r)
		if hv > 0 && miniScalePercent040() != sizeValues[i] {
			c = blendColor(c, theme.accent2, .08*hv)
		}
		roundFill(hdc, rv, c, 9)
		drawText(hdc, sizeLabels[i], int(r.Left), int(r.Top), int(r.Right-r.Left), int(r.Bottom-r.Top), 10, 650, theme.text, DT_CENTER|DT_VCENTER|DT_SINGLELINE)
	}
	drawText(hdc, "Масштаб интерфейса", int(body.Left)+18, int(app.uiScaleRects[0].Top)-26, 240, 20, 12, 650, theme.text, DT_LEFT|DT_VCENTER|DT_SINGLELINE)
	scales := []int{90, 100, 110, 125}
	for i, r := range app.uiScaleRects {
		c := surfaceButtonColor()
		if app.settings.UIScale == scales[i] {
			c = blendColor(c, theme.accent, .48)
		}
		rv, hv := hoverCardRect(r)
		if hv > 0 && app.settings.UIScale != scales[i] {
			c = blendColor(c, theme.accent2, .08*hv)
		}
		roundFill(hdc, rv, c, 9)
		drawText(hdc, fmt.Sprintf("%d%%", scales[i]), int(r.Left), int(r.Top), int(r.Right-r.Left), int(r.Bottom-r.Top), 11, 650, theme.text, DT_CENTER|DT_VCENTER|DT_SINGLELINE)
	}
	drawText(hdc, "Масштаб меняет весь интерфейс PowerPilot, включая поля ввода и размеры минимального окна.", int(body.Left)+18, int(app.uiScaleRects[0].Bottom)+16, int(body.Right-body.Left)-36, 34, 10, 400, theme.muted, DT_LEFT|DT_VCENTER)
	drawText(hdc, "Размер отдельного редактора", int(body.Left)+18, int(app.graphWindowSizeRects[0].Top)-26, 280, 20, 12, 650, theme.text, DT_LEFT|DT_VCENTER|DT_SINGLELINE)
	graphSizes := []string{"1280 × 820", "1440 × 900", "1600 × 960"}
	for i, r := range app.graphWindowSizeRects {
		drawSelectableButton(hdc, r, graphSizes[i], app.settings.GraphWindowSize == i)
	}
	drawText(hdc, "×", int(app.graphWindowWidthRect.Right)+7, int(app.graphWindowWidthRect.Top), 20, int(app.graphWindowWidthRect.Bottom-app.graphWindowWidthRect.Top), 13, 650, theme.muted, DT_CENTER|DT_VCENTER|DT_SINGLELINE)
	roundFill(hdc, app.graphWindowWidthRect, surfaceButtonColor(), 8)
	roundFill(hdc, app.graphWindowHeightRect, surfaceButtonColor(), 8)
	lockLabel := "Зафиксировать размер"
	if app.settings.GraphWindowSizeLocked {
		lockLabel = "Размер зафиксирован"
	}
	drawSelectableButton(hdc, app.graphWindowLockRect, lockLabel, app.settings.GraphWindowSizeLocked)
	timelineTitleY := int(app.resourceTimelineModeRects[0].Top) - 24
	drawText(hdc, "Временная шкала ресурсов", int(body.Left)+18, timelineTitleY, int(body.Right-body.Left)-36, 18, 12, 650, theme.text, DT_LEFT|DT_VCENTER|DT_SINGLELINE)
	modeNames := []string{"Реальное время", "Относительно: −время → 0"}
	for i, r := range app.resourceTimelineModeRects {
		drawSelectableButton(hdc, r, modeNames[i], app.settings.ResourceTimelineMode == i)
	}
	drawText(hdc, "Количество отметок шкалы", int(body.Left)+18, int(app.resourceTimelineTicksTrackRect.Top)-30, 250, 20, 12, 650, theme.text, DT_LEFT|DT_VCENTER|DT_SINGLELINE)
	if app.resourceTimelineTicksTrackRect.Right > app.resourceTimelineTicksTrackRect.Left {
		roundFill(hdc, app.resourceTimelineTicksTrackRect, surfaceButtonColor(), 4)
		filled := app.resourceTimelineTicksTrackRect
		filled.Right = app.resourceTimelineTicksKnobRect.Left + (app.resourceTimelineTicksKnobRect.Right-app.resourceTimelineTicksKnobRect.Left)/2
		roundFill(hdc, filled, theme.accent, 4)
		d2dFillEllipse(float32((app.resourceTimelineTicksKnobRect.Left+app.resourceTimelineTicksKnobRect.Right)/2), float32((app.resourceTimelineTicksKnobRect.Top+app.resourceTimelineTicksKnobRect.Bottom)/2), 9, 9, theme.text)
		d2dFillEllipse(float32((app.resourceTimelineTicksKnobRect.Left+app.resourceTimelineTicksKnobRect.Right)/2), float32((app.resourceTimelineTicksKnobRect.Top+app.resourceTimelineTicksKnobRect.Bottom)/2), 5, 5, theme.accent)
		roundFill(hdc, app.resourceTimelineTicksValueRect, surfaceButtonColor(), 8)
	}
}

func drawAppearanceSettings(hdc uintptr, body RECT) {
	contentY := app.settingsContentTop
	drawText(hdc, "Тема", int(body.Left)+18, contentY, 120, 18, 12, 650, theme.text, DT_LEFT|DT_VCENTER|DT_SINGLELINE)
	names := []string{"Тёмная ☾", "Светлая ☀", "Системная ◐"}
	for i, r := range app.themeRects {
		c := surfaceButtonColor()
		if app.settings.ThemeMode == i {
			c = blendColor(c, theme.accent, .36)
		}
		rv, hv := hoverCardRect(r)
		if hv > 0 {
			c = blendColor(c, theme.accent2, .06*hv)
		}
		roundFill(hdc, rv, c, 11)
		if hv > 0 && app.settings.ThemeMode != i && ui2d.active {
			d2dDrawRoundedOutline(rv, 11, float32(1+0.4*hv), blendColor(theme.border, theme.accent2, .42))
		}
		drawText(hdc, names[i], int(r.Left), int(r.Top), int(r.Right-r.Left), int(r.Bottom-r.Top), 12, 650, theme.text, DT_CENTER|DT_VCENTER|DT_SINGLELINE)
	}
	bgTitleY := int(app.themeRects[0].Bottom) + 12
	drawText(hdc, "Фон", int(body.Left)+18, bgTitleY, 100, 18, 12, 650, theme.text, DT_LEFT|DT_VCENTER|DT_SINGLELINE)
	bgNames := []string{"Однотонный", "Сияние", "Сетка", "Звёзды", "Аврора", "Туманность"}
	for i, r := range app.backgroundRects {
		c := surfaceButtonColor()
		if app.settings.Background == i {
			c = blendColor(c, theme.accent, .28)
		}
		rv, hv := hoverCardRect(r)
		if hv > 0 {
			c = blendColor(c, theme.accent2, .07*hv)
		}
		roundFill(hdc, rv, c, 11)
		if hv > 0 && app.settings.Background != i && ui2d.active {
			d2dDrawRoundedOutline(rv, 11, float32(1+0.4*hv), blendColor(theme.border, theme.accent2, .42))
		}
		drawText(hdc, bgNames[i], int(r.Left), int(r.Top), int(r.Right-r.Left), int(r.Bottom-r.Top), 11, 650, theme.text, DT_CENTER|DT_VCENTER|DT_SINGLELINE)
		preview := RECT{r.Left + 8, r.Top + 7, r.Left + 38, r.Bottom - 7}
		drawBackgroundSample(hdc, preview, i)
	}
	surfaceTitleY := int(maxRectBottom(app.backgroundRects[:])) + 8
	drawText(hdc, "Области и панели", int(body.Left)+18, surfaceTitleY, 180, 18, 12, 650, theme.text, DT_LEFT|DT_VCENTER|DT_SINGLELINE)
	sn := []string{"Классические", "Мягкие", "Акцентные", "Стеклянные", "Жидкое стекло"}
	for i, r := range app.surfaceRects {
		c := surfaceButtonColor()
		if app.settings.SurfaceStyle == i {
			c = blendColor(c, theme.accent, .28)
		}
		rv, hv := hoverCardRect(r)
		if hv > 0 {
			c = blendColor(c, theme.accent2, .06*hv)
		}
		roundFill(hdc, rv, c, 10)
		if hv > 0 && app.settings.SurfaceStyle != i && ui2d.active {
			d2dDrawRoundedOutline(rv, 10, float32(1+0.4*hv), blendColor(theme.border, theme.accent2, .42))
		}
		drawText(hdc, sn[i], int(r.Left), int(r.Top), int(r.Right-r.Left), int(r.Bottom-r.Top), 11, 650, theme.text, DT_CENTER|DT_VCENTER|DT_SINGLELINE)
	}
	animTitleY := int(maxRectBottom(app.surfaceRects[:])) + 8
	drawText(hdc, "Анимации", int(body.Left)+18, animTitleY, 130, 18, 12, 650, theme.text, DT_LEFT|DT_VCENTER|DT_SINGLELINE)
	an := []string{"Полные", "Умеренные", "Отключены"}
	for i, r := range app.animationRects {
		c := surfaceButtonColor()
		if app.settings.AnimationMode == i {
			c = blendColor(c, theme.accent, .28)
		}
		rv, hv := hoverCardRect(r)
		if hv > 0 {
			c = blendColor(c, theme.accent2, .06*hv)
		}
		roundFill(hdc, rv, c, 10)
		if hv > 0 && app.settings.AnimationMode != i && ui2d.active {
			d2dDrawRoundedOutline(rv, 10, float32(1+0.4*hv), blendColor(theme.border, theme.accent2, .42))
		}
		drawText(hdc, an[i], int(r.Left), int(r.Top), int(r.Right-r.Left), int(r.Bottom-r.Top), 11, 650, theme.text, DT_CENTER|DT_VCENTER|DT_SINGLELINE)
	}
}

func surfacePanelPreviewColor(style int) uint32 {
	switch style {
	case 1:
		return blendColor(theme.bg, theme.panel2, .62)
	case 2:
		return blendColor(theme.panel, theme.accent, .10)
	case 3:
		return blendColor(theme.bg, theme.panel2, .48)
	case 4:
		return blendColor(theme.bg, theme.panel2, .54)
	default:
		return theme.panel
	}
}

func drawBackgroundSample(hdc uintptr, r RECT, mode int) {
	roundFill(hdc, r, theme.bg, 8)
	if !ui2d.active {
		return
	}
	switch mode {
	case 1:
		if ui2d.active {
			d2dPushClip(r)
		}
		cx := float32((r.Left + r.Right) / 2)
		cy := float32((r.Top + r.Bottom) / 2)
		d2dFillEllipse(cx, cy, 12, 12, blendColor(theme.bg, theme.accent, .20))
		d2dFillEllipse(cx+3, cy-3, 7, 7, blendColor(theme.bg, theme.accent2, .16))
		if ui2d.active {
			d2dPopClip()
		}
	case 2:
		c := blendColor(theme.bg, theme.border, .55)
		for x := float32(r.Left + 6); x < float32(r.Right); x += 10 {
			d2dDrawLine(x, float32(r.Top), x, float32(r.Bottom), 1, c)
		}
		for y := float32(r.Top + 6); y < float32(r.Bottom); y += 10 {
			d2dDrawLine(float32(r.Left), y, float32(r.Right), y, 1, c)
		}
	case 3:
		c := blendColor(theme.bg, theme.accent2, .7)
		for i := 0; i < 8; i++ {
			x := float32(int(r.Left) + 6 + (i*13)%max(1, int(r.Right-r.Left)-10))
			y := float32(int(r.Top) + 5 + (i*17)%max(1, int(r.Bottom-r.Top)-8))
			d2dFillEllipse(x, y, 1.2, 1.2, c)
		}
	case 4:
		d2dPushClip(r)
		w := float32(r.Right - r.Left)
		h := float32(r.Bottom - r.Top)
		d2dFillEllipse(float32(r.Left)+w*.18, float32(r.Top)+h*.28, w*.58, h*.36, blendColor(theme.bg, theme.accent, .24))
		d2dFillEllipse(float32(r.Left)+w*.78, float32(r.Top)+h*.62, w*.54, h*.30, blendColor(theme.bg, theme.accent2, .20))
		d2dPopClip()
	case 5:
		d2dPushClip(r)
		w := float32(r.Right - r.Left)
		h := float32(r.Bottom - r.Top)
		d2dFillEllipseOpacity(float32(r.Left)+w*.20, float32(r.Top)+h*.30, w*.48, h*.38, theme.accent, .24)
		d2dFillEllipseOpacity(float32(r.Left)+w*.76, float32(r.Top)+h*.58, w*.44, h*.36, theme.accent2, .20)
		d2dFillEllipseOpacity(float32(r.Left)+w*.48, float32(r.Top)+h*.80, w*.38, h*.25, theme.success, .12)
		d2dPopClip()
	}
}

func drawStatisticsSettings(hdc uintptr, body RECT) {
	items := app.historyItems
	starts, execs, cancels, errors, saves, autos := 0, 0, 0, 0, 0, 0
	actions := [4]int{}
	for _, it := range items {
		switch it.Kind {
		case "START":
			starts++
		case "EXECUTE":
			execs++
			parseHistoryAction(it.Detail, &actions)
		case "CANCEL":
			cancels++
		case "ERROR":
			errors++
		case "SAVE":
			saves++
		case "AUTO_START":
			autos++
		}
	}
	baseY := app.settingsContentTop + 8
	innerLeft := int(body.Left) + 18
	innerW := int(body.Right-body.Left) - 36
	gap := 12
	cardW := (innerW - gap*2) / 3
	vals := []struct {
		title string
		value string
		sub   string
	}{{"Выполнено", fmt.Sprint(execs), "успешных действий"}, {"Отменено", fmt.Sprint(cancels), "активных задач"}, {"Автозапуски", fmt.Sprint(autos), "по расписанию"}, {"Запуски", fmt.Sprint(starts), "ручных задач"}, {"Сохранено", fmt.Sprint(saves), "сценариев"}, {"Ошибки", fmt.Sprint(errors), "зафиксировано"}}
	for i, v := range vals {
		row, col := i/3, i%3
		x := innerLeft + col*(cardW+gap)
		y := baseY + row*92
		r := RECT{int32(x), int32(y), int32(x + cardW), int32(y + 80)}
		roundFill(hdc, r, surfaceButtonColor(), 12)
		drawText(hdc, v.title, x+14, y+10, cardW-28, 17, 11, 600, theme.muted, DT_LEFT|DT_VCENTER|DT_SINGLELINE)
		drawText(hdc, v.value, x+14, y+28, cardW-28, 28, 22, 700, theme.text, DT_LEFT|DT_VCENTER|DT_SINGLELINE)
		drawText(hdc, v.sub, x+14, y+56, cardW-28, 14, 10, 400, theme.muted, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
	}
	chartY := baseY + 198
	drawText(hdc, "Действия питания", innerLeft, chartY, 200, 20, 13, 650, theme.text, DT_LEFT|DT_VCENTER|DT_SINGLELINE)
	names := []string{"Выключение", "Перезагрузка", "Сон", "Гибернация"}
	maxV := 1
	for _, v := range actions {
		if v > maxV {
			maxV = v
		}
	}
	for i, n := range names {
		y := chartY + 30 + i*30
		drawText(hdc, n, innerLeft, y, 120, 18, 11, 500, theme.muted, DT_LEFT|DT_VCENTER|DT_SINGLELINE)
		track := RECT{int32(innerLeft + 128), int32(y + 5), int32(int(body.Right) - 90), int32(y + 13)}
		roundFill(hdc, track, surfaceButtonColor(), 4)
		if actions[i] > 0 {
			fillR := track
			fillR.Right = track.Left + int32(float64(track.Right-track.Left)*float64(actions[i])/float64(maxV))
			roundFill(hdc, fillR, theme.accent, 4)
		}
		drawText(hdc, fmt.Sprint(actions[i]), int(body.Right)-78, y, 50, 18, 11, 650, theme.text, DT_RIGHT|DT_VCENTER|DT_SINGLELINE)
	}
}

func parseHistoryAction(detail string, dst *[4]int) {
	for i := 0; i < 4; i++ {
		if strings.Contains(detail, fmt.Sprintf("action=%d", i)) {
			dst[i]++
			return
		}
	}
}

func drawDataSettings(hdc uintptr, body RECT) {
	titles := []string{"Экспорт задач", "Импорт задач", "Создать резервную копию", "Восстановить резервную копию", "Технический лог"}
	desc := []string{"Сохранить сценарии в .pptasks", "Добавить задачи из .pptasks или JSON", "Настройки + задачи + история в .ppbackup", "Восстановить настройки, задачи и историю", "Открыть PowerPilot.log для диагностики"}
	for i := 0; i < len(titles); i++ {
		drawSettingsActionCard(hdc, app.dataRects[i], titles[i], desc[i])
	}
}

func drawSettingsActionCard(hdc uintptr, r RECT, title, sub string) {
	if r.Right <= r.Left {
		return
	}
	c := surfaceButtonColor()
	rv, hv := hoverCardRect(r)
	if hv > 0 {
		c = blendColor(c, theme.accent2, .07*hv)
	}
	roundFill(hdc, rv, c, 12)
	if hv > 0 && ui2d.active {
		d2dDrawRoundedOutline(rv, 12, float32(1+0.4*hv), blendColor(theme.border, theme.accent2, .42))
	}
	drawText(hdc, title, int(r.Left)+14, int(r.Top)+8, int(r.Right-r.Left)-28, 19, 12, 650, theme.text, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
	drawText(hdc, sub, int(r.Left)+14, int(r.Top)+28, int(r.Right-r.Left)-28, int(r.Bottom-r.Top)-34, 9, 400, theme.muted, DT_LEFT|DT_VCENTER|DT_WORDBREAK|DT_END_ELLIPSIS)
}

func drawSettingsUpdateCard(hdc uintptr, r, action RECT, title, sub, actionLabel string, busy bool) {
	if r.Right <= r.Left {
		return
	}
	roundFill(hdc, r, surfaceButtonColor(), 12)
	if ui2d.active {
		d2dDrawRoundedOutline(r, 12, 1, blendColor(theme.border, theme.accent2, .22))
	}
	textRight := int(action.Left) - 12
	textW := max(90, textRight-int(r.Left)-14)
	drawText(hdc, title, int(r.Left)+14, int(r.Top)+10, textW, 21, 12, 650, theme.text, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
	drawText(hdc, sub, int(r.Left)+14, int(r.Top)+34, textW, int(r.Bottom-r.Top)-42, 9, 400, theme.muted, DT_LEFT|DT_VCENTER|DT_WORDBREAK|DT_END_ELLIPSIS)
	if busy {
		drawDisabledButton(hdc, action, actionLabel)
	} else {
		drawButton(hdc, action, actionLabel, false)
	}
}

func drawComponentsSettings(hdc uintptr, body RECT) {
	_, providerStatus := temperatureProviderStatus()
	sensorAction, sensorBusy := temperatureProviderActionLabel()
	drawSettingsUpdateCard(hdc, app.dataRects[5], app.temperatureUpdateActionRect, "Аппаратные датчики", providerStatus, sensorAction, sensorBusy)
	title, sub := powerPilotUpdateCard()
	updateAction, updateBusy := powerPilotUpdateActionLabel()
	drawSettingsUpdateCard(hdc, app.appUpdateRect, app.appUpdateActionRect, title, sub, updateAction, updateBusy)
	textW := int(app.temperatureAutoUpdateRect.Right - app.temperatureAutoUpdateRect.Left)
	drawText(hdc, "Проверка обновлений датчиков", int(app.temperatureAutoUpdateRect.Left), int(app.temperatureAutoUpdateRect.Top)-1, textW, 19, 11, 650, theme.text, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
	drawText(hdc, "Автоматически при каждом запуске PowerPilot и затем каждые 30 минут. Установка найденного обновления запускается вручную и может запросить UAC.", int(app.temperatureAutoUpdateRect.Left), int(app.temperatureAutoUpdateRect.Top)+18, textW, 34, 9, 400, theme.muted, DT_LEFT|DT_VCENTER|DT_WORDBREAK)
	infoY := int(app.temperatureAutoUpdateRect.Bottom) + 36
	drawText(hdc, "Аппаратные показатели в «Ресурсы → Продвинутый монитор → Датчики» появляются после установки провайдера. Температуры также используются в обычных карточках ресурсов; для низкоуровневого доступа CPU и платы применяется PawnIO.", int(app.temperatureAutoUpdateRect.Left), infoY, textW, 38, 10, 400, theme.muted, DT_LEFT|DT_VCENTER|DT_WORDBREAK)
}

func drawSafetySettings(hdc uintptr, body RECT) {
	drawToggle(hdc, app.safetyFullscreenRect, app.settings.SafetyFullscreen)
	drawText(hdc, "Не выполнять действие поверх полноэкранного приложения", int(app.safetyFullscreenRect.Right)+12, int(app.safetyFullscreenRect.Top)-2, int(body.Right-app.safetyFullscreenRect.Right)-30, 20, 12, 600, theme.text, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
	drawText(hdc, "Полезно для игр, видео и презентаций.", int(app.safetyFullscreenRect.Right)+12, int(app.safetyFullscreenRect.Top)+17, int(body.Right-app.safetyFullscreenRect.Right)-30, 16, 10, 400, theme.muted, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)

	drawToggle(hdc, app.safetyRecentRect, app.settings.SafetyRecentInput)
	lineX := int(app.safetyRecentRect.Right) + 12
	pr, fr, sr := uiInlineNumberLayout("Считать неактивным после", "мин", lineX, int(app.safetyRecentRect.Top)-3, int(body.Right)-18, 2)
	uiDrawInlineNumber(hdc, "Считать неактивным после", "мин", pr, fr, sr)
	drawText(hdc, "Не выполнять задачу, пока пользователь активен.", lineX, int(app.safetyRecentRect.Top)+20, int(body.Right)-lineX-18, 16, 9, 400, theme.muted, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)

	drawToggle(hdc, app.showSystemProcessesRect, app.settings.ShowSystemProcesses)
	drawText(hdc, "Отображать системные процессы", int(app.showSystemProcessesRect.Right)+12, int(app.showSystemProcessesRect.Top)-2, int(body.Right-app.showSystemProcessesRect.Right)-30, 20, 12, 600, theme.text, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)

	// Protected-process selection intentionally comes after every safety toggle.
	drawButton(hdc, app.safetyProcessesRect, "Защищённые процессы", false)
	drawText(hdc, processCountPhrase(len(app.settings.SafetyProcesses)), int(app.safetyProcessesRect.Right)+14, int(app.safetyProcessesRect.Top), 120, int(app.safetyProcessesRect.Bottom-app.safetyProcessesRect.Top), 11, 500, theme.muted, DT_LEFT|DT_VCENTER|DT_SINGLELINE)
	drawText(hdc, "Если защитное правило сработало, задача ждёт снятия блокировки и затем продолжает.", int(body.Left)+18, int(app.safetyProcessesRect.Bottom)+16, int(body.Right-body.Left)-36, 34, 10, 400, theme.muted, DT_LEFT|DT_VCENTER)
}

func currentScenarioConditions() []AutomationCondition {
	if n := selectedGraphNode(); n != nil && n.Kind == graphNodeCondition {
		return n.Conditions
	}
	if app.scenarioSavedDraft {
		return app.savedEditDraft.Conditions
	}
	return app.settings.AdvancedConditions
}

func scenarioConditionDepth(conds []AutomationCondition, idx int) int {
	if idx < 0 || idx >= len(conds) {
		return 0
	}
	groups := make(map[string]AutomationCondition)
	for _, c := range conds {
		if c.Type == condGroup {
			groups[c.ID] = c
		}
	}
	depth := 0
	parent := conds[idx].GroupID
	seen := map[string]bool{}
	for parent != "" && depth < 4 && !seen[parent] {
		seen[parent] = true
		g, ok := groups[parent]
		if !ok {
			break
		}
		depth++
		parent = g.GroupID
	}
	return depth
}

func visibleScenarioConditionIndices(conds []AutomationCondition) []int {
	if app.conditionGroupCollapsed == nil {
		app.conditionGroupCollapsed = map[string]bool{}
	}
	groups := make(map[string]AutomationCondition)
	for _, c := range conds {
		if c.Type == condGroup {
			groups[c.ID] = c
		}
	}
	visible := make([]int, 0, len(conds))
	for i, c := range conds {
		hidden := false
		for parent, seen := c.GroupID, map[string]bool{}; parent != "" && !seen[parent]; {
			seen[parent] = true
			if app.conditionGroupCollapsed[parent] {
				hidden = true
				break
			}
			g, ok := groups[parent]
			if !ok {
				break
			}
			parent = g.GroupID
		}
		if !hidden {
			visible = append(visible, i)
		}
	}
	return visible
}

func scenarioGroupDescendantCount(conds []AutomationCondition, groupID string) int {
	if groupID == "" {
		return 0
	}
	parents := make(map[string]string)
	for _, c := range conds {
		if c.Type == condGroup {
			parents[c.ID] = c.GroupID
		}
	}
	count := 0
	for _, c := range conds {
		if c.ID == groupID {
			continue
		}
		for parent, seen := c.GroupID, map[string]bool{}; parent != "" && !seen[parent]; parent = parents[parent] {
			seen[parent] = true
			if parent == groupID {
				count++
				break
			}
		}
	}
	return count
}

func conditionGroupWouldCycle(conds []AutomationCondition, groupID, parentID string) bool {
	if groupID == "" || parentID == "" {
		return false
	}
	if groupID == parentID {
		return true
	}
	parents := make(map[string]string)
	for _, c := range conds {
		if c.Type == condGroup {
			parents[c.ID] = c.GroupID
		}
	}
	for p, seen := parentID, map[string]bool{}; p != "" && !seen[p]; p = parents[p] {
		if p == groupID {
			return true
		}
		seen[p] = true
	}
	return false
}
func currentScenarioSteps() []ActionStep {
	if n := selectedGraphNode(); n != nil && n.Kind == graphNodeAction {
		return n.Steps
	}
	if app.scenarioSavedDraft {
		return app.savedEditDraft.Steps
	}
	return app.settings.ActionSteps
}
func setCurrentScenarioConditions(v []AutomationCondition) {
	if n := selectedGraphNode(); n != nil && n.Kind == graphNodeCondition {
		n.Conditions = v
		persistCurrentScenarioGraph()
		return
	}
	if app.scenarioSavedDraft {
		app.savedEditDraft.Conditions = v
	} else {
		app.settings.AdvancedConditions = v
		saveSettings()
	}
}
func setCurrentScenarioSteps(v []ActionStep) {
	if n := selectedGraphNode(); n != nil && n.Kind == graphNodeAction {
		n.Steps = v
		persistCurrentScenarioGraph()
		return
	}
	if app.scenarioSavedDraft {
		app.savedEditDraft.Steps = v
	} else {
		app.settings.ActionSteps = v
		saveSettings()
	}
}
func currentScenarioCloseBefore() bool {
	if app.scenarioSavedDraft {
		return app.savedEditDraft.CloseBefore
	}
	return app.settings.CloseBefore
}
func currentScenarioProcesses() []string {
	if app.scenarioSavedDraft {
		return app.savedEditDraft.Processes
	}
	return app.settings.Processes
}
func currentScenarioTriggerLogic() int {
	if app.scenarioSavedDraft {
		return app.savedEditDraft.TriggerLogic
	}
	return app.settings.TriggerLogic
}
func setCurrentScenarioTriggerLogic(v int) {
	if app.scenarioSavedDraft {
		app.savedEditDraft.TriggerLogic = v
	} else {
		app.settings.TriggerLogic = v
		saveSettings()
	}
}
func currentScenarioMode() int {
	if tr := ensureCurrentScenarioGraph().trigger(); tr != nil {
		return tr.Mode
	}
	if app.scenarioSavedDraft {
		return app.savedEditDraft.Mode
	}
	return app.selectedMode
}
func currentScenarioAction() int {
	if n := selectedGraphNode(); n != nil && n.Kind == graphNodeFinish {
		return n.Action
	}
	if app.scenarioSavedDraft {
		return app.savedEditDraft.Action
	}
	return app.selectedAction
}
func currentScenarioRecurrence() RecurrenceSpec {
	if tr := ensureCurrentScenarioGraph().trigger(); tr != nil {
		return tr.Recurrence
	}
	if app.scenarioSavedDraft {
		return app.savedEditDraft.Recurrence
	}
	return app.settings.Recurrence
}
func loadScenarioWhenInputs() {
	if !app.scenarioSavedDraft {
		return
	}
	t := app.savedEditDraft
	app.selectedMode = t.Mode
	pSetWindowTextW.Call(app.edits[idDelayHours], uintptr(unsafe.Pointer(wstr(strconv.Itoa(t.DelayHours)))))
	pSetWindowTextW.Call(app.edits[idDelayMinutes], uintptr(unsafe.Pointer(wstr(strconv.Itoa(t.DelayMinutes)))))
	pSetWindowTextW.Call(app.edits[idDelaySeconds], uintptr(unsafe.Pointer(wstr(strconv.Itoa(t.DelaySeconds)))))
	setExactFields(t.Exact)
	pSetWindowTextW.Call(app.edits[idIdleMinutes], uintptr(unsafe.Pointer(wstr(strconv.Itoa(max(t.IdleMinutes, 1))))))
	pSetWindowTextW.Call(app.edits[idWatchProcess], uintptr(unsafe.Pointer(wstr(t.WatchProcess))))
	pSetWindowTextW.Call(app.edits[idScheduleTime], uintptr(unsafe.Pointer(wstr(t.Recurrence.TimeHHMM))))
}
func syncScenarioWhenFields() {
	if !app.scenarioSavedDraft {
		syncFields()
		return
	}
	t := &app.savedEditDraft
	t.Mode = app.selectedMode
	t.DelayHours = parseInt(getText(app.edits[idDelayHours]), t.DelayHours)
	t.DelayMinutes = parseInt(getText(app.edits[idDelayMinutes]), t.DelayMinutes)
	t.DelaySeconds = parseInt(getText(app.edits[idDelaySeconds]), t.DelaySeconds)
	t.Exact = exactFromFields()
	t.IdleMinutes = parseInt(getText(app.edits[idIdleMinutes]), max(t.IdleMinutes, 1))
	t.WatchProcess = strings.TrimSpace(getText(app.edits[idWatchProcess]))
	if v := strings.TrimSpace(getText(app.edits[idScheduleTime])); v != "" {
		t.Recurrence.TimeHHMM = v
	}
}

func currentScenarioActionSummary() string {
	if !app.scenarioSavedDraft {
		return actionSummary()
	}
	n := []string{"Выключение", "Перезагрузка", "Сон", "Гибернация", "Завершить задачу"}
	if app.savedEditDraft.Action >= 0 && app.savedEditDraft.Action < len(n) {
		return n[app.savedEditDraft.Action]
	}
	return "Действие"
}
func currentScenarioWhenSummary() string {
	if !app.scenarioSavedDraft {
		return whenSummary()
	}
	t := app.savedEditDraft
	switch t.Mode {
	case 0:
		return fmt.Sprintf("Таймер %02d:%02d:%02d", t.DelayHours, t.DelayMinutes, t.DelaySeconds)
	case 1:
		return t.Exact
	case 2:
		return fmt.Sprintf("Простой %d сек", t.IdleMinutes)
	case 3:
		if t.WatchProcess != "" {
			return "После " + t.WatchProcess
		}
		return "После процесса"
	case 4:
		if t.Recurrence.TimeHHMM != "" {
			return "Расписание · " + t.Recurrence.TimeHHMM
		}
		return "Расписание"
	case 5:
		return "По условиям"
	}
	return "Когда"
}

func offsetRectXY(r RECT, dx, dy int32) RECT {
	return RECT{r.Left + dx, r.Top + dy, r.Right + dx, r.Bottom + dy}
}
func scenarioDragOffset(kind, idx int) int32 {
	if app.draggingScenarioKind != kind || app.dragGapAnim <= .001 || idx == app.draggingScenarioIndex {
		return 0
	}
	from, to := app.draggingScenarioIndex, app.draggingScenarioTarget
	shift := int32(10*app.dragGapAnim + .5)
	if from < to && idx > from && idx <= to {
		return -shift
	}
	if from > to && idx >= to && idx < from {
		return shift
	}
	return 0
}

func drawScenarioPage(hdc uintptr, body RECT, w int) {
	if app.graphWindow != 0 {
		drawScenarioGraphMainPlaceholder(hdc, body)
		if app.confirmDiscardScenario {
			drawScenarioDiscardConfirm(hdc, body)
		}
		return
	}
	drawScenarioGraphEditor(hdc, body, w, false)
	if app.confirmDiscardScenario {
		drawScenarioDiscardConfirm(hdc, body)
	}
	return
	/*
		if app.scenarioBackRect.Right > app.scenarioBackRect.Left {
			drawButton(hdc, app.scenarioBackRect, "← Назад", false)
		}
		if !app.scenarioSavedDraft && undoAvailable040() {
			drawButton(hdc, app.undoRect, "↶", false)
		} else {
			drawDisabledButton(hdc, app.undoRect, "↶")
		}
		if !app.scenarioSavedDraft && redoAvailable040() {
			drawButton(hdc, app.redoRect, "↷", false)
		} else {
			drawDisabledButton(hdc, app.redoRect, "↷")
		}
		drawButton(hdc, app.previewRect, "Просмотр", false)
		titleX := int(app.redoRect.Right) + 16
		if titleX < int(body.Left)+18 {
			titleX = int(body.Left) + 18
		}
		titleW := int(app.previewRect.Left) - titleX - 14
		// Never force the heading wider than the actual free space. The previous
		// 160px minimum made it paint under the trigger-logic button on narrow windows.
		if titleW >= 72 {
			drawText(hdc, "Схема задачи", titleX, int(body.Top)+18, titleW, 28, 18, 650, theme.text, DT_CENTER|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
		}

		if app.scenarioSavedDraft && app.section == 13 {
			drawText(hdc, "Название", int(app.savedScenarioNameRect.Left), int(app.savedScenarioNameRect.Top)-18, int(app.savedScenarioNameRect.Right-app.savedScenarioNameRect.Left), 16, 9, 500, theme.muted, DT_LEFT|DT_VCENTER|DT_SINGLELINE)
			roundFill(hdc, app.savedScenarioNameRect, surfaceButtonColor(), 9)
			drawButton(hdc, app.savedScenarioSaveRect, "Сохранить", true)
			drawButton(hdc, app.savedScenarioCancelRect, "Отмена", false)
			drawButton(hdc, app.savedScenarioCheckRect, "Проверка", false)
		}

		// Primary trigger node.
		r := app.blockWhenRect
		rv, hv := hoverCardRect(r)
		c := blendColor(surfaceButtonColor(), theme.accent, .16)
		if hv > 0 {
			c = blendColor(c, theme.accent2, .08*hv)
		}
		roundFill(hdc, rv, c, 13)
		if hv > 0 && ui2d.active {
			d2dDrawRoundedOutline(rv, 13, float32(1+.35*hv), blendColor(theme.border, theme.accent2, .44))
		}
		drawText(hdc, "КОГДА", int(r.Left)+14, int(r.Top)+6, int(r.Right-r.Left)-28, 14, 9, 700, theme.accent2, DT_LEFT|DT_VCENTER|DT_SINGLELINE)
		drawText(hdc, currentScenarioWhenSummary(), int(r.Left)+14, int(r.Top)+21, int(r.Right-r.Left)-28, 24, 12, 600, theme.text, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)

		innerLeft := int(body.Left) + 18
		innerW := int(body.Right-body.Left) - 36
		colGap := 18
		colW := (innerW - colGap) / 2
		rightX := innerLeft + colW + colGap
		listTitleY := int(app.blockWhenRect.Bottom) + 12
		// Connector from trigger into both branches.
		midX := (app.blockWhenRect.Left + app.blockWhenRect.Right) / 2
		roundFill(hdc, RECT{midX - 1, app.blockWhenRect.Bottom, midX + 1, int32(listTitleY - 3)}, blendColor(theme.border, theme.accent2, .40), 1)
		drawText(hdc, "↓", int(midX)-10, listTitleY-11, 20, 18, 12, 650, theme.muted, DT_CENTER|DT_VCENTER|DT_SINGLELINE)
		drawText(hdc, "УСЛОВИЯ", innerLeft, listTitleY, colW, 18, 10, 700, theme.muted, DT_CENTER|DT_VCENTER|DT_SINGLELINE)
		drawText(hdc, "ДЕЙСТВИЯ", rightX, listTitleY, colW, 18, 10, 700, theme.muted, DT_CENTER|DT_VCENTER|DT_SINGLELINE)

		conds := currentScenarioConditions()
		steps := currentScenarioSteps()
		if ui2d.active && app.scenarioListClip.Right > app.scenarioListClip.Left {
			d2dPushClip(app.scenarioListClip)
		}
		for i, baseR := range app.conditionRows {
			dataIdx := app.conditionRowIndices[i]
			if dataIdx < 0 || dataIdx >= len(conds) || baseR.Right <= baseR.Left {
				continue
			}
			dy := scenarioDragOffset(1, dataIdx)
			r := offsetRectXY(baseR, 0, dy)
			dragR := offsetRectXY(app.conditionDragRects[i], 0, dy)
			logicR := offsetRectXY(app.conditionLogicRects[i], 0, dy)
			delR := offsetRectXY(app.conditionDeleteRects[i], 0, dy)
			dupR := offsetRectXY(app.conditionDuplicateRects[i], 0, dy)
			cnd := conds[dataIdx]
			depth := scenarioConditionDepth(conds, dataIdx)
			for level := 0; level < depth; level++ {
				branchX := app.scenarioListClip.Left + 7 + int32(level*12)
				branchColor := blendColor(theme.border, theme.accent2, .48)
				d2dDrawLine(float32(branchX), float32(r.Top-4), float32(branchX), float32(r.Bottom), 1.1, branchColor)
				d2dDrawLine(float32(branchX), float32((r.Top+r.Bottom)/2), float32(r.Left+4), float32((r.Top+r.Bottom)/2), 1.1, branchColor)
			}
			rv, hv := hoverCardRect(r)
			cc := surfaceButtonColor()
			if cnd.Type == condGroup {
				cc = blendColor(cc, theme.accent, .18)
			}
			if hv > 0 {
				cc = blendColor(cc, theme.accent2, .06*hv)
			}
			roundFill(hdc, rv, cc, 10)
			if app.draggingScenarioKind == 1 && app.draggingScenarioIntoGroup && app.draggingScenarioTarget == dataIdx && ui2d.active {
				d2dDrawRoundedOutline(rv, 10, 2, theme.accent2)
			}
			if cnd.Type == condGroup {
				arrow := "▾"
				if app.conditionGroupCollapsed[cnd.ID] {
					arrow = "▸"
				}
				collapseR := offsetRectXY(app.conditionCollapseRects[i], 0, dy)
				drawText(hdc, arrow, int(collapseR.Left), int(collapseR.Top), int(collapseR.Right-collapseR.Left), int(collapseR.Bottom-collapseR.Top), 13, 700, theme.accent2, DT_CENTER|DT_VCENTER|DT_SINGLELINE)
			}
			drawText(hdc, "≡", int(dragR.Left), int(dragR.Top), int(dragR.Right-dragR.Left), int(dragR.Bottom-dragR.Top), 14, 700, theme.muted, DT_CENTER|DT_VCENTER|DT_SINGLELINE)
			textX := int(r.Left) + 38
			if dataIdx > 0 {
				logic := "И"
				if cnd.Logic == logicOR {
					logic = "ИЛИ"
				}
				drawOutlinedButton(hdc, logicR, logic, theme.accent2)
				textX = int(logicR.Right) + 6
			}
			label := conditionSummary(cnd)
			if cnd.Type == condGroup {
				label = fmt.Sprintf("Составное условие · %d", scenarioGroupDescendantCount(conds, cnd.ID))
			}
			drawText(hdc, label, textX, int(r.Top), int(r.Right)-textX-66, int(r.Bottom-r.Top), 10, 550, theme.text, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
			drawScenarioIconButton(hdc, dupR, scenarioIconCopy)
			drawScenarioIconButton(hdc, delR, scenarioIconDelete)
			if i+1 < len(app.conditionRows) && app.conditionRowIndices[i+1] == dataIdx+1 && app.conditionRows[i+1].Right > app.conditionRows[i+1].Left {
				nextIdx := app.conditionRowIndices[i+1]
				nextR := offsetRectXY(app.conditionRows[i+1], 0, scenarioDragOffset(1, nextIdx))
				cx := (r.Left + r.Right) / 2
				roundFill(hdc, RECT{cx - 1, r.Bottom, cx + 1, nextR.Top}, blendColor(theme.border, theme.accent2, .35), 1)
			}
		}
		for i, baseR := range app.stepRows {
			dataIdx := app.stepRowIndices[i]
			if dataIdx < 0 || dataIdx >= len(steps) || baseR.Right <= baseR.Left {
				continue
			}
			dy := scenarioDragOffset(2, dataIdx)
			r := offsetRectXY(baseR, 0, dy)
			dragR := offsetRectXY(app.stepDragRects[i], 0, dy)
			delR := offsetRectXY(app.stepDeleteRects[i], 0, dy)
			dupR := offsetRectXY(app.stepDuplicateRects[i], 0, dy)
			st := steps[dataIdx]
			rv, hv := hoverCardRect(r)
			cc := surfaceButtonColor()
			if hv > 0 {
				cc = blendColor(cc, theme.accent2, .06*hv)
			}
			roundFill(hdc, rv, cc, 10)
			drawText(hdc, "≡", int(dragR.Left), int(dragR.Top), int(dragR.Right-dragR.Left), int(dragR.Bottom-dragR.Top), 15, 700, theme.muted, DT_CENTER|DT_VCENTER|DT_SINGLELINE)
			drawText(hdc, stepSummary(st), int(r.Left)+38, int(r.Top), int(r.Right-r.Left)-100, int(r.Bottom-r.Top), 10, 550, theme.text, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
			drawScenarioIconButton(hdc, dupR, scenarioIconCopy)
			drawScenarioIconButton(hdc, delR, scenarioIconDelete)
			if i+1 < len(app.stepRows) && app.stepRowIndices[i+1] == dataIdx+1 && app.stepRows[i+1].Right > app.stepRows[i+1].Left {
				nextIdx := app.stepRowIndices[i+1]
				nextR := offsetRectXY(app.stepRows[i+1], 0, scenarioDragOffset(2, nextIdx))
				cx := (r.Left + r.Right) / 2
				roundFill(hdc, RECT{cx - 1, r.Bottom, cx + 1, nextR.Top}, blendColor(theme.border, theme.accent2, .35), 1)
			}
		}
		if ui2d.active && app.scenarioListClip.Right > app.scenarioListClip.Left {
			d2dPopClip()
		}
		drawScrollBar(hdc, app.scenarioScrollTrack, app.scenarioScrollThumb)
		drawButton(hdc, app.addConditionRect, "+ Условие", false)
		drawButton(hdc, app.addConditionGroupRect, "Составное условие", false)
		drawScenarioIconButton(hdc, app.pasteConditionRect, scenarioIconPaste)
		drawScenarioIconButton(hdc, app.copyConditionsGroupRect, scenarioIconPasteAll)
		drawButton(hdc, app.addStepRect, "+ Шаг", false)
		drawScenarioIconButton(hdc, app.pasteStepRect, scenarioIconPaste)
		drawScenarioIconButton(hdc, app.copyStepsGroupRect, scenarioIconPasteAll)

		// Both branches converge into the final power action.
		fr := app.blockActionRect
		rv, hv = hoverCardRect(fr)
		fc := blendColor(surfaceButtonColor(), theme.accent, .16)
		if hv > 0 {
			fc = blendColor(fc, theme.accent2, .08*hv)
		}
		roundFill(hdc, rv, fc, 13)
		drawText(hdc, "ФИНАЛЬНОЕ ДЕЙСТВИЕ", int(fr.Left)+12, int(fr.Top)+5, int(fr.Right-fr.Left)-24, 13, 8, 700, theme.accent2, DT_LEFT|DT_VCENTER|DT_SINGLELINE)
		drawText(hdc, currentScenarioActionSummary(), int(fr.Left)+12, int(fr.Top)+19, int(fr.Right-fr.Left)-24, 22, 12, 650, theme.text, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)

		if app.draggingScenarioKind != 0 && ui2d.active {
			var base RECT
			if app.draggingScenarioKind == 1 {
				for slot, idx := range app.conditionRowIndices {
					if idx == app.draggingScenarioIndex {
						base = app.conditionRows[slot]
						break
					}
				}
			} else if app.draggingScenarioKind == 2 {
				for slot, idx := range app.stepRowIndices {
					if idx == app.draggingScenarioIndex {
						base = app.stepRows[slot]
						break
					}
				}
			}
			if base.Right > base.Left {
				if app.draggingScenarioKind == 1 && app.draggingScenarioParentID == "" {
					x := app.scenarioListClip.Left + 3
					d2dDrawLine(float32(x), float32(app.scenarioListClip.Top+4), float32(x), float32(app.scenarioListClip.Bottom-4), 2, theme.accent2)
				}
				hh := (base.Bottom - base.Top) / 2
				cy := app.draggingScenarioY
				ghost := RECT{base.Left, cy - hh, base.Right, cy + hh}
				roundFill(hdc, ghost, blendColor(surfaceButtonColor(), theme.accent, .34), 10)
				d2dDrawRoundedOutline(ghost, 10, 1.6, theme.accent2)
			}
		}
		drawScenarioTooltip(hdc, body)
	*/
	if app.confirmDiscardScenario {
		drawScenarioDiscardConfirm(hdc, body)
	}
}

func drawScenarioDiscardConfirm(hdc uintptr, body RECT) {
	fill(hdc, body, blendColor(theme.bg, rgb(0, 0, 0), .42))
	w := minInt(410, int(body.Right-body.Left)-32)
	h := 174
	x := int(body.Left+body.Right)/2 - w/2
	y := int(body.Top+body.Bottom)/2 - h/2
	app.confirmDiscardRect = RECT{int32(x), int32(y), int32(x + w), int32(y + h)}
	roundFill(hdc, app.confirmDiscardRect, surfacePanelColor(), 16)
	if ui2d.active {
		d2dDrawRoundedOutline(app.confirmDiscardRect, 16, 1, blendColor(theme.border, theme.accent2, .35))
	}
	drawText(hdc, "Отменить изменения?", x+20, y+18, w-40, 28, 18, 650, theme.text, DT_CENTER|DT_VCENTER|DT_SINGLELINE)
	drawText(hdc, "Несохранённые изменения задачи будут потеряны.", x+24, y+53, w-48, 42, 11, 450, theme.muted, DT_CENTER|DT_VCENTER|DT_WORDBREAK)
	btnY := y + 116
	app.confirmDiscardNoRect = RECT{int32(x + 20), int32(btnY), int32(x + w/2 - 6), int32(btnY + 40)}
	app.confirmDiscardYesRect = RECT{int32(x + w/2 + 6), int32(btnY), int32(x + w - 20), int32(btnY + 40)}
	drawButton(hdc, app.confirmDiscardNoRect, "Продолжить", false)
	drawOutlinedButton(hdc, app.confirmDiscardYesRect, "Отменить изменения", theme.danger)
}

func drawBlockActionEditor(hdc uintptr, body RECT, w int) {
	drawButton(hdc, app.blockEditorBackRect, "← Назад", false)
	drawText(hdc, "Финальное действие", int(body.Left)+146, int(body.Top)+18, int(body.Right-body.Left)-292, 28, 19, 650, theme.text, DT_CENTER|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
	names := []string{"Выключение", "Перезагрузка", "Сон", "Гибернация", "Завершить задачу"}
	for i, r := range app.blockActionChoiceRects {
		drawSelectableButton(hdc, r, names[i], currentScenarioAction() == i)
	}
}

func drawBlockWhenEditor(hdc uintptr, body RECT, w int) {
	drawButton(hdc, app.blockEditorBackRect, "← Назад", false)
	drawButton(hdc, app.blockEditorDoneRect, "Готово", true)
	drawText(hdc, "Когда запускать", int(body.Left)+146, int(body.Top)+18, int(body.Right-body.Left)-300, 28, 19, 650, theme.text, DT_CENTER|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
	names := []string{"Таймер", "Дата и время", "Простой", "После процесса", "Расписание", "По условиям"}
	for i, r := range app.modeRects {
		drawSelectableButton(hdc, r, names[i], app.selectedMode == i)
	}
	label := ""
	switch app.selectedMode {
	case 0:
		label = "Задержка"
	case 1:
		label = "Дата и время"
	case 2:
		label = "Секунд без активности"
	case 3:
		label = "Процесс"
	case 4:
		label = "Расписание"
	case 5:
		label = "Запуск определяется блоками условий ниже"
	}
	uiDrawWhenFieldChrome(hdc, app.selectedMode, app.modeRects[:6], body, label)
	if app.selectedMode == 3 {
		drawButton(hdc, app.pickRect, "Выбрать процесс", false)
		proc := app.settings.WatchProcess
		if app.scenarioSavedDraft {
			proc = app.savedEditDraft.WatchProcess
		}
		if strings.TrimSpace(proc) != "" {
			drawSmallGlyphButton(hdc, app.processClearRect, "×", theme.danger)
		}
	}
	if app.selectedMode == 4 {
		rec := currentScenarioRecurrence()
		kn := []string{"Каждый день", "Будни", "Выбранные дни"}
		for i, r := range app.recurrenceKindRects {
			if r.Right > r.Left {
				drawSelectableButton(hdc, r, kn[i], rec.Kind == i)
			}
		}
		dn := []string{"Пн", "Вт", "Ср", "Чт", "Пт", "Сб", "Вс"}
		for i, r := range app.recurrenceDayRects {
			if r.Right > r.Left {
				active := rec.Days[i]
				if rec.Kind == 0 {
					active = true
				}
				if rec.Kind == 1 {
					active = i < 5
				}
				drawSelectableButton(hdc, r, dn[i], active)
			}
		}
	}
}

func drawCheckPage(hdc uintptr, body RECT, w int) {
	drawButton(hdc, app.checkBackRect, "← Назад", false)
	drawText(hdc, "Проверка задачи", int(body.Left)+154, int(body.Top)+18, int(body.Right-body.Left)-172, 30, 19, 650, theme.text, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
	// Two explicit tools rather than hidden/static reports.
	r := app.checkTestRect
	rv, hv := hoverCardRect(r)
	c := surfaceButtonColor()
	if hv > 0 {
		c = blendColor(c, theme.accent2, .08*hv)
	}
	roundFill(hdc, rv, c, 16)
	if hv > 0 && ui2d.active {
		d2dDrawRoundedOutline(rv, 16, float32(1+.4*hv), blendColor(theme.border, theme.accent2, .48))
	}
	drawText(hdc, "Тест сценария", int(r.Left)+18, int(r.Top)+18, int(r.Right-r.Left)-36, 26, 16, 650, theme.text, DT_LEFT|DT_VCENTER|DT_SINGLELINE)
	drawText(hdc, "Безопасно проверяет триггер, условия и шаги. Ничего не закрывает и не выключает.", int(r.Left)+18, int(r.Top)+50, int(r.Right-r.Left)-36, 48, 11, 400, theme.muted, DT_LEFT|DT_VCENTER)
	r = app.checkDiagRect
	rv, hv = hoverCardRect(r)
	c = surfaceButtonColor()
	if hv > 0 {
		c = blendColor(c, theme.accent2, .08*hv)
	}
	roundFill(hdc, rv, c, 16)
	if hv > 0 && ui2d.active {
		d2dDrawRoundedOutline(rv, 16, float32(1+.4*hv), blendColor(theme.border, theme.accent2, .48))
	}
	drawText(hdc, "Диагностика", int(r.Left)+18, int(r.Top)+18, int(r.Right-r.Left)-36, 26, 16, 650, theme.text, DT_LEFT|DT_VCENTER|DT_SINGLELINE)
	drawText(hdc, "Показывает в реальном времени, что именно удерживает активную задачу и почему.", int(r.Left)+18, int(r.Top)+50, int(r.Right-r.Left)-36, 48, 11, 400, theme.muted, DT_LEFT|DT_VCENTER)
}

func drawDiagnosticPage(hdc uintptr, body RECT, w int) {
	drawButton(hdc, app.diagnosticBackRect, "← Назад", false)
	refreshLabel := "Обновить"
	if app.diagnosticMode == 1 {
		refreshLabel = "Повторить тест"
	}
	drawButton(hdc, app.diagnosticRefreshRect, refreshLabel, false)
	drawText(hdc, app.diagnosticTitle, int(body.Left)+154, int(body.Top)+18, int(body.Right-body.Left)-172, 30, 19, 650, theme.text, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
	y := int(body.Top) + 72
	limit := len(app.diagnosticLines)
	bottomPad := 12
	if app.diagnosticMode == 1 {
		if app.dryRunStep <= 0 {
			app.dryRunStep = 1
		}
		limit = minInt(limit, app.dryRunStep)
		bottomPad = 58
	}
	for i, ln := range app.diagnosticLines {
		if i >= limit {
			break
		}
		if y+46 > int(body.Bottom)-bottomPad {
			break
		}
		r := RECT{body.Left + 18, int32(y), body.Right - 18, int32(y + 42)}
		c := surfaceButtonColor()
		badge := theme.muted
		mark := "•"
		if ln.Level == diagOK {
			badge = theme.success
			mark = "✓"
		}
		if ln.Level == diagWait {
			badge = theme.accent2
			mark = "…"
		}
		if ln.Level == diagError {
			badge = theme.danger
			mark = "!"
		}
		roundFill(hdc, r, c, 10)
		drawText(hdc, mark, int(r.Left)+8, int(r.Top), 28, 42, 15, 700, badge, DT_CENTER|DT_VCENTER|DT_SINGLELINE)
		drawText(hdc, ln.Title, int(r.Left)+42, int(r.Top)+4, int(r.Right-r.Left)-54, 17, 11, 650, theme.text, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
		drawText(hdc, ln.Detail, int(r.Left)+42, int(r.Top)+21, int(r.Right-r.Left)-54, 16, 10, 400, theme.muted, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
		y += 48
		_ = i
	}
	if app.diagnosticMode == 1 {
		drawButton(hdc, app.diagnosticRestartRect, "Сначала", false)
		next := "Следующий блок"
		if app.dryRunStep >= len(app.diagnosticLines) {
			next = "Готово"
		}
		drawButton(hdc, app.diagnosticNextRect, next, true)
		drawButton(hdc, app.diagnosticRunRect, "Показать всё", false)
	}
}

func drawSmallGlyphButton(hdc uintptr, r RECT, text string, c uint32) {
	if r.Right <= r.Left {
		return
	}
	drawOutlinedButton(hdc, r, text, c)
}

func drawScenarioIconButton(hdc uintptr, r RECT, kind int) {
	if r.Right <= r.Left || r.Bottom <= r.Top {
		return
	}
	c := surfaceButtonColor()
	h := hoverAmount(r)
	if h > 0 {
		c = blendColor(c, theme.accent2, .10*h)
	}
	roundFill(hdc, r, c, 7)
	if h > 0 && ui2d.active {
		d2dDrawRoundedOutline(r, 7, 1, blendColor(theme.border, theme.accent2, .60))
	}
	size := int32(21)
	x := r.Left + (r.Right-r.Left-size)/2
	y := r.Top + (r.Bottom-r.Top-size)/2
	d2dDrawScenarioIcon(kind, RECT{x, y, x + size, y + size})
}

func scenarioTooltipAt(x, y int32) (RECT, string) {
	if app.notificationPanelOpen && !app.confirmClearNotifications {
		if pointIn(app.notificationClearRect, x, y) {
			return app.notificationClearRect, "Очистить уведомления"
		}
		items := notificationItemsSnapshot(app.notificationUnreadOnly)
		for slot, r := range app.notificationReadRects {
			idx := app.notificationRowIndices[slot]
			if idx >= 0 && idx < len(items) && items[idx].Unread && pointIn(r, x, y) {
				return r, "Пометить прочитанным"
			}
		}
		return RECT{}, ""
	}
	if app.section != 7 && app.section != 13 {
		return RECT{}, ""
	}
	targets := []struct {
		r    RECT
		text string
	}{
		{app.pasteConditionRect, "Вставить условие"},
		{app.copyConditionsGroupRect, "Копировать все условия"},
		{app.pasteStepRect, "Вставить действие"},
		{app.copyStepsGroupRect, "Копировать все действия"},
	}
	for slot, idx := range app.conditionRowIndices {
		if idx >= 0 {
			targets = append(targets, struct {
				r    RECT
				text string
			}{app.conditionDuplicateRects[slot], "Копировать условие"})
		}
	}
	for slot, idx := range app.stepRowIndices {
		if idx >= 0 {
			targets = append(targets, struct {
				r    RECT
				text string
			}{app.stepDuplicateRects[slot], "Копировать действие"})
		}
	}
	for _, target := range targets {
		if pointIn(target.r, x, y) {
			return target.r, target.text
		}
	}
	return RECT{}, ""
}

func drawScenarioTooltip(hdc uintptr, body RECT) {
	if app.tooltipText == "" || time.Since(app.tooltipSince) < time.Second || !pointIn(app.tooltipRect, app.mouseX, app.mouseY) {
		return
	}
	w := max(140, len([]rune(app.tooltipText))*7+24)
	x := int(app.tooltipRect.Left+app.tooltipRect.Right)/2 - w/2
	x = clampInt(x, int(body.Left)+8, int(body.Right)-w-8)
	y := int(app.tooltipRect.Bottom) + 8
	if y+34 > int(body.Bottom)-8 {
		y = int(app.tooltipRect.Top) - 42
	}
	r := RECT{int32(x), int32(y), int32(x + w), int32(y + 34)}
	roundFill(hdc, r, blendColor(surfacePanelColor(), theme.border, .30), 8)
	if ui2d.active {
		d2dDrawRoundedOutline(r, 8, 1, blendColor(theme.border, theme.accent2, .35))
	}
	drawText(hdc, app.tooltipText, x+10, y, w-20, 34, 10, 550, theme.text, DT_CENTER|DT_VCENTER|DT_SINGLELINE)
}
func metricFmt(v float64, unit string) string {
	if v < 0 {
		return "н/д"
	}
	return fmt.Sprintf("%.0f%s", v, unit)
}

func basicConditionType(t int) bool {
	switch t {
	case condCPU, condGPU, condNetwork, condDisk, condFileStable, condProcessExit:
		return true
	default:
		return false
	}
}

func conditionCatalogExtraHeight() int32 {
	// 15 additional condition buttons in a 5-column grid: 3 rows * 32 px + 4 px.
	return 100
}

func shiftRectY(r RECT, dy int32) RECT {
	if r.Right <= r.Left || r.Bottom <= r.Top || dy == 0 {
		return r
	}
	r.Top += dy
	r.Bottom += dy
	return r
}

func shiftConditionEditorDynamicRects(dy int32) {
	if dy == 0 {
		return
	}
	app.conditionMoreRect = shiftRectY(app.conditionMoreRect, dy)
	app.whenFieldRect = shiftRectY(app.whenFieldRect, dy)
	app.warningFieldRect = shiftRectY(app.warningFieldRect, dy)
	for i := range app.editorCompareRects {
		app.editorCompareRects[i] = shiftRectY(app.editorCompareRects[i], dy)
	}
	app.timeFieldRects[0] = shiftRectY(app.timeFieldRects[0], dy)
	app.editorBrowseRect = shiftRectY(app.editorBrowseRect, dy)
	app.editorClearRect = shiftRectY(app.editorClearRect, dy)
	app.conditionOpenGroupRect = shiftRectY(app.conditionOpenGroupRect, dy)
	app.conditionCloseGroupRect = shiftRectY(app.conditionCloseGroupRect, dy)
	app.conditionDelayFieldRect = shiftRectY(app.conditionDelayFieldRect, dy)
}

func drawConditionEditor(hdc uintptr, body RECT, w int) {
	drawText(hdc, "Условие сценария", int(body.Left)+18, int(body.Top)+16, int(body.Right-body.Left)-36, 28, 19, 650, theme.text, DT_CENTER|DT_VCENTER|DT_SINGLELINE)
	names := []string{"CPU", "GPU", "Сеть", "Диск", "Файл завершён", "Процесс завершён", "Окно есть", "Окно закрыто", "Окно активно", "Заголовок", "Нет звука", "Батарея", "Питание", "Свободно на диске", "Файлы в папке", "CPU процесса", "GPU процесса", "RAM процесса", "Интернет", "Полный экран", "Диск/устройство"}
	basicSet := map[int]bool{condCPU: true, condGPU: true, condNetwork: true, condDisk: true, condFileStable: true, condProcessExit: true}
	for i, r := range app.editorTypeRects {
		if r.Right > r.Left && basicSet[i] {
			drawSelectableButton(hdc, r, names[i], app.conditionDraft.Type == i)
		}
	}
	// Extra conditions always exist geometrically and are revealed only through the
	// moving clip edge. This avoids a close-only final-frame disappearance/flicker.
	extraTop := int32(0)
	for i, r := range app.editorTypeRects {
		if r.Right > r.Left && !basicSet[i] {
			if extraTop == 0 || r.Top < extraTop {
				extraTop = r.Top
			}
		}
	}
	if extraTop != 0 {
		clip := RECT{body.Left + 18, extraTop, body.Right - 18, app.conditionMoreRect.Top}
		if clip.Bottom > clip.Top {
			if ui2d.active {
				d2dPushClip(clip)
			}
			for i, r := range app.editorTypeRects {
				if r.Right > r.Left && !basicSet[i] {
					drawSelectableButton(hdc, r, names[i], app.conditionDraft.Type == i)
				}
			}
			if ui2d.active {
				d2dPopClip()
			}
		}
	}
	moreText := "Дополнительные условия ↓"
	if app.conditionCatalogExpanded {
		moreText = "Дополнительные условия ↑"
	}
	drawButton(hdc, app.conditionMoreRect, moreText, false)
	reveal := ui2d.active && app.pageAnim >= 1 && app.subRevealAnim < 1
	if reveal {
		d2dSetTranslation(0, float32(subRevealOffset()))
	}
	// The controls below follow the animated expand button. Do not scan the full
	// extra-condition geometry here: doing so made the form jump to its final
	// expanded position on the first animation frame and caused visible blinking.
	fieldY := int(app.conditionMoreRect.Bottom) + 6
	if conditionUsesThreshold(app.conditionDraft.Type) {
		thresholdLabel := "Порог"
		switch app.conditionDraft.Type {
		case condCPU, condGPU, condDisk, condBatteryPercent, condProcessCPU, condProcessGPU:
			thresholdLabel = "Порог, %"
		case condNetwork:
			thresholdLabel = "Порог, КБ/с"
		case condProcessRAM:
			thresholdLabel = "Порог, МБ"
		case condDiskFree:
			thresholdLabel = "Свободно, ГБ"
		case condFolderCount:
			thresholdLabel = "Количество"
		}
		drawText(hdc, thresholdLabel, int(body.Left)+18, fieldY, 120, 18, 11, 500, theme.muted, DT_LEFT|DT_VCENTER|DT_SINGLELINE)
		roundFill(hdc, app.whenFieldRect, surfaceButtonColor(), 9)
		drawText(hdc, "Непрерывно, сек", int(app.warningFieldRect.Left), fieldY, 130, 18, 11, 500, theme.muted, DT_LEFT|DT_VCENTER|DT_SINGLELINE)
		roundFill(hdc, app.warningFieldRect, surfaceButtonColor(), 9)
		comps := []string{"≤", "≥"}
		for i, r := range app.editorCompareRects {
			want := -1
			if i == 1 {
				want = 1
			}
			drawSelectableButton(hdc, r, comps[i], app.conditionDraft.Compare == want)
		}
	} else if app.conditionDraft.Type == condACPower || app.conditionDraft.Type == condInternet || app.conditionDraft.Type == condFullscreen || app.conditionDraft.Type == condDrivePresent {
		label, leftOpt, rightOpt := "Источник питания", "От батареи", "От сети"
		if app.conditionDraft.Type == condInternet {
			label, leftOpt, rightOpt = "Подключение к интернету", "Нет сети", "Есть сеть"
		} else if app.conditionDraft.Type == condFullscreen {
			label, leftOpt, rightOpt = "Полноэкранное приложение", "Нет", "Есть"
		} else if app.conditionDraft.Type == condDrivePresent {
			label, leftOpt, rightOpt = "Диск / устройство", "Отключён", "Подключён"
		}
		drawText(hdc, label, int(body.Left)+18, fieldY, 200, 18, 11, 500, theme.muted, DT_LEFT|DT_VCENTER|DT_SINGLELINE)
		drawSelectableButton(hdc, app.editorCompareRects[0], leftOpt, app.conditionDraft.Compare <= 0)
		drawSelectableButton(hdc, app.editorCompareRects[1], rightOpt, app.conditionDraft.Compare > 0)
		drawText(hdc, "Непрерывно, сек", int(app.warningFieldRect.Left), fieldY, 130, 18, 11, 500, theme.muted, DT_LEFT|DT_VCENTER|DT_SINGLELINE)
		roundFill(hdc, app.warningFieldRect, surfaceButtonColor(), 9)
	} else {
		drawText(hdc, "Непрерывно / стабильно, сек", int(app.warningFieldRect.Left), fieldY, 190, 18, 11, 500, theme.muted, DT_LEFT|DT_VCENTER|DT_SINGLELINE)
		roundFill(hdc, app.warningFieldRect, surfaceButtonColor(), 9)
	}
	textY := fieldY + 66
	label := "Путь к файлу или папке"
	switch app.conditionDraft.Type {
	case condProcessExit:
		label = "Имя процесса"
	case condWindowExists, condWindowMissing, condWindowActive:
		label = "Название окна или процесса"
	case condWindowTitle:
		label = "Фрагмент заголовка окна"
	case condAudioSilent:
		label = "Процесс со звуком (пусто = весь системный звук)"
	case condDiskFree:
		label = "Диск или путь (пусто = C:\\)"
	case condFolderCount:
		label = "Путь к папке"
	case condProcessCPU, condProcessGPU, condProcessRAM:
		label = "Имя процесса"
	case condBatteryPercent, condACPower, condInternet, condFullscreen:
		label = "Примечание (необязательно)"
	case condDrivePresent:
		label = "Буква диска или путь"
	default:
		if conditionUsesThreshold(app.conditionDraft.Type) {
			label = "Примечание (необязательно)"
		}
	}
	drawText(hdc, label, int(body.Left)+18, textY, 260, 18, 11, 500, theme.muted, DT_LEFT|DT_VCENTER|DT_SINGLELINE)
	roundFill(hdc, app.timeFieldRects[0], surfaceButtonColor(), 9)
	if app.conditionDraft.Type == condFileStable {
		drawButton(hdc, app.editorBrowseRect, "Обзор…", false)
	} else if app.conditionDraft.Type == condFolderCount || app.conditionDraft.Type == condDiskFree || app.conditionDraft.Type == condDrivePresent {
		drawButton(hdc, app.editorBrowseRect, "Выбрать диск/папку…", false)
	} else if app.conditionDraft.Type == condProcessExit || app.conditionDraft.Type == condAudioSilent || app.conditionDraft.Type == condProcessCPU || app.conditionDraft.Type == condProcessGPU || app.conditionDraft.Type == condProcessRAM {
		drawButton(hdc, app.editorBrowseRect, "Выбрать процесс", false)
		if strings.TrimSpace(app.conditionDraft.Text) != "" {
			drawSmallGlyphButton(hdc, app.editorClearRect, "×", theme.danger)
		}
	}
	drawText(hdc, "Пауза после", int(body.Left)+18, int(app.conditionDelayFieldRect.Top), 92, int(app.conditionDelayFieldRect.Bottom-app.conditionDelayFieldRect.Top), 9, 500, theme.muted, DT_LEFT|DT_VCENTER|DT_SINGLELINE)
	roundFill(hdc, app.conditionDelayFieldRect, surfaceButtonColor(), 8)
	if app.conditionCatalogAnimating {
		// Native EDIT HWNDs are intentionally hidden while the catalog moves. Paint
		// their current values into the Direct2D composition so there is no blank/flash
		// frame and every visible element moves as one surface.
		if conditionUsesThreshold(app.conditionDraft.Type) {
			drawText(hdc, strings.TrimSpace(getText(app.edits[idCondThreshold])), int(app.whenFieldRect.Left), int(app.whenFieldRect.Top), int(app.whenFieldRect.Right-app.whenFieldRect.Left), int(app.whenFieldRect.Bottom-app.whenFieldRect.Top), 15, 400, theme.text, DT_CENTER|DT_VCENTER|DT_SINGLELINE)
		}
		drawText(hdc, strings.TrimSpace(getText(app.edits[idCondHold])), int(app.warningFieldRect.Left), int(app.warningFieldRect.Top), int(app.warningFieldRect.Right-app.warningFieldRect.Left), int(app.warningFieldRect.Bottom-app.warningFieldRect.Top), 13, 400, theme.text, DT_CENTER|DT_VCENTER|DT_SINGLELINE)
		if app.timeFieldRects[0].Right > app.timeFieldRects[0].Left {
			drawText(hdc, getText(app.edits[idCondText]), int(app.timeFieldRects[0].Left)+8, int(app.timeFieldRects[0].Top), max(1, int(app.timeFieldRects[0].Right-app.timeFieldRects[0].Left)-16), int(app.timeFieldRects[0].Bottom-app.timeFieldRects[0].Top), 15, 400, theme.text, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
		}
		drawText(hdc, strings.TrimSpace(getText(app.edits[idCondDelay])), int(app.conditionDelayFieldRect.Left), int(app.conditionDelayFieldRect.Top), int(app.conditionDelayFieldRect.Right-app.conditionDelayFieldRect.Left), int(app.conditionDelayFieldRect.Bottom-app.conditionDelayFieldRect.Top), 13, 400, theme.text, DT_CENTER|DT_VCENTER|DT_SINGLELINE)
	}
	if reveal {
		d2dResetTransform()
	}
	// Footer owns an opaque strip so no moving/expanded condition control can ever
	// show through behind Save/Cancel on the minimum window size.
	fill(hdc, RECT{body.Left + 1, app.editorSaveRect.Top - 8, body.Right - 1, body.Bottom - 1}, surfacePanelColor())
	// Footer buttons stay static, so their visual bounds and hitboxes are always identical.
	drawButton(hdc, app.editorSaveRect, "Сохранить", true)
	drawButton(hdc, app.editorCancelRect, "Отмена", false)
}

func drawStepEditor(hdc uintptr, body RECT, w int) {
	drawText(hdc, "Шаг продвинутой задачи", int(body.Left)+18, int(body.Top)+16, int(body.Right-body.Left)-36, 28, 19, 650, theme.text, DT_CENTER|DT_VCENTER|DT_SINGLELINE)
	types := []int{stepCloseProcesses, stepWait, stepRunCommand, stepNotify, stepMonitorOff, stepMonitorOn, stepSetVolume, stepLockWorkstation, stepPowerPlan, stepProcessPriority}
	names := []string{"Закрыть процессы", "Подождать", "Запустить команду", "Уведомление", "Монитор выкл.", "Монитор вкл.", "Громкость", "Блокировка ПК", "План питания", "Приоритет процесса"}
	for i := 0; i < len(types); i++ {
		active := app.stepDraft.Type == types[i]
		if types[i] == stepSetVolume && app.stepDraft.Type == stepMute {
			active = true
		}
		drawSelectableButton(hdc, app.stepTypeRects[i], names[i], active)
	}
	reveal := ui2d.active && app.pageAnim >= 1 && app.subRevealAnim < 1
	if reveal {
		d2dSetTranslation(0, float32(subRevealOffset()))
	}

	fieldY := int(app.stepTypeRects[9].Bottom) + 11
	switch app.stepDraft.Type {
	case stepCloseProcesses:
		drawText(hdc, "Процессы этого шага", int(body.Left)+18, fieldY, 220, 18, 11, 500, theme.muted, DT_LEFT|DT_VCENTER|DT_SINGLELINE)
		drawButton(hdc, app.editorBrowseRect, "Выбрать процессы", false)
		drawText(hdc, processCountPhrase(len(app.stepDraft.Processes)), int(app.editorBrowseRect.Right)+12, int(app.editorBrowseRect.Top), max(100, int(body.Right-app.editorBrowseRect.Right)-28), int(app.editorBrowseRect.Bottom-app.editorBrowseRect.Top), 10, 500, theme.muted, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
	case stepWait:
		drawText(hdc, "Секунды ожидания", int(body.Left)+18, fieldY, 180, 18, 11, 500, theme.muted, DT_LEFT|DT_VCENTER|DT_SINGLELINE)
		if app.whenFieldRect.Right > app.whenFieldRect.Left {
			roundFill(hdc, app.whenFieldRect, surfaceButtonColor(), 9)
		}
	case stepSetVolume, stepMute:
		drawText(hdc, "Громкость", int(body.Left)+18, fieldY, 220, 18, 11, 500, theme.muted, DT_LEFT|DT_VCENTER|DT_SINGLELINE)
		audioModes := []string{"Задать %", "Выключить звук", "Включить звук"}
		for i, rr := range app.powerPlanRects {
			active := (i == 0 && app.stepDraft.Type == stepSetVolume) || (i == 1 && app.stepDraft.Type == stepMute && app.stepDraft.Value != 0) || (i == 2 && app.stepDraft.Type == stepMute && app.stepDraft.Value == 0)
			drawSelectableButton(hdc, rr, audioModes[i], active)
		}
		if app.stepDraft.Type == stepSetVolume && app.whenFieldRect.Right > app.whenFieldRect.Left {
			drawText(hdc, "Уровень, %", int(body.Left)+18, int(app.whenFieldRect.Top)-20, 120, 18, 10, 500, theme.muted, DT_LEFT|DT_VCENTER|DT_SINGLELINE)
			roundFill(hdc, app.whenFieldRect, surfaceButtonColor(), 9)
		}
	case stepLockWorkstation:
		drawText(hdc, "Windows будет заблокирован, сценарий продолжит работу в фоне.", int(body.Left)+18, fieldY+20, int(body.Right-body.Left)-36, 36, 11, 500, theme.muted, DT_LEFT|DT_VCENTER)
	case stepPowerPlan:
		drawText(hdc, "План электропитания", int(body.Left)+18, fieldY, 220, 18, 11, 500, theme.muted, DT_LEFT|DT_VCENTER|DT_SINGLELINE)
		planNames := []string{"Энергосбережение", "Сбалансированный", "Высокая производительность"}
		for i, rr := range app.powerPlanRects {
			drawSelectableButton(hdc, rr, planNames[i], clampInt(app.stepDraft.Value, 0, 2) == i)
		}
	case stepProcessPriority:
		drawText(hdc, "Процесс и приоритет", int(body.Left)+18, fieldY, 220, 18, 11, 500, theme.muted, DT_LEFT|DT_VCENTER|DT_SINGLELINE)
		if app.timeFieldRects[0].Right > app.timeFieldRects[0].Left {
			roundFill(hdc, app.timeFieldRects[0], surfaceButtonColor(), 9)
		}
		drawButton(hdc, app.editorBrowseRect, "Выбрать…", false)
		prioNames := []string{"Низкий", "Обычный", "Высокий"}
		for i, rr := range app.powerPlanRects {
			drawSelectableButton(hdc, rr, prioNames[i], clampInt(app.stepDraft.Value, 0, 2) == i)
		}
	case stepRunCommand:
		drawText(hdc, "Команда или программа", int(body.Left)+18, fieldY, 220, 18, 11, 500, theme.muted, DT_LEFT|DT_VCENTER|DT_SINGLELINE)
		if app.timeFieldRects[0].Right > app.timeFieldRects[0].Left {
			roundFill(hdc, app.timeFieldRects[0], surfaceButtonColor(), 9)
		}
		if app.editorBrowseRect.Right > app.editorBrowseRect.Left {
			drawButton(hdc, app.editorBrowseRect, "Выбрать файл…", false)
		}
	case stepNotify:
		drawText(hdc, "Текст уведомления", int(body.Left)+18, fieldY, 220, 18, 11, 500, theme.muted, DT_LEFT|DT_VCENTER|DT_SINGLELINE)
		if app.timeFieldRects[0].Right > app.timeFieldRects[0].Left {
			roundFill(hdc, app.timeFieldRects[0], surfaceButtonColor(), 9)
		}
	}

	drawText(hdc, "При ошибке", int(body.Left)+18, int(app.stepErrorRects[0].Top)-22, 150, 18, 11, 500, theme.muted, DT_LEFT|DT_VCENTER|DT_SINGLELINE)
	errNames := []string{"Продолжить", "Остановить", "Повторить"}
	for i, r := range app.stepErrorRects {
		drawSelectableButton(hdc, r, errNames[i], app.stepDraft.OnError == i)
	}
	if app.stepDraft.OnError == 2 {
		drawText(hdc, "Повторов", int(app.stepRetryFieldRect.Left), int(app.stepRetryFieldRect.Top)-20, 90, 18, 10, 500, theme.muted, DT_LEFT|DT_VCENTER|DT_SINGLELINE)
		roundFill(hdc, app.stepRetryFieldRect, surfaceButtonColor(), 8)
	}
	drawText(hdc, "Пауза после, сек", int(app.stepDelayFieldRect.Left), int(app.stepDelayFieldRect.Top)-20, 150, 18, 10, 500, theme.muted, DT_LEFT|DT_VCENTER|DT_SINGLELINE)
	roundFill(hdc, app.stepDelayFieldRect, surfaceButtonColor(), 8)
	if reveal {
		d2dResetTransform()
	}
	// Footer owns its own surface. Combined with the compact two-row action picker
	// this prevents pause/error controls from appearing behind Save/Cancel at high DPI.
	fill(hdc, RECT{body.Left + 1, app.editorSaveRect.Top - 8, body.Right - 1, body.Bottom - 1}, surfacePanelColor())
	// Footer buttons remain static so hover and click hitboxes exactly match the painted buttons.
	drawButton(hdc, app.editorSaveRect, "Сохранить", true)
	drawButton(hdc, app.editorCancelRect, "Отмена", false)
}

func drawHistorySettings(hdc uintptr, body RECT) {
	if app.historyDetailOpen {
		drawHistoryDetail(hdc, body)
		return
	}
	filterNames := []string{"Все", "Задачи", "Автоматизация", "Ошибки"}
	for i, r := range app.historyFilterRects {
		c := surfaceButtonColor()
		if app.historyFilter == i {
			c = blendColor(c, theme.accent, .32)
		}
		rv, hv := hoverCardRect(r)
		if hv > 0 {
			c = blendColor(c, theme.accent2, .07*hv)
		}
		roundFill(hdc, rv, c, 8)
		if hv > 0 && app.historyFilter != i && ui2d.active {
			d2dDrawRoundedOutline(rv, 8, float32(1+0.4*hv), blendColor(theme.border, theme.accent2, .42))
		}
		drawText(hdc, filterNames[i], int(r.Left), int(r.Top), int(r.Right-r.Left), int(r.Bottom-r.Top), 10, 600, theme.text, DT_CENTER|DT_VCENTER|DT_SINGLELINE)
	}
	roundFill(hdc, app.historySearchRect, surfaceButtonColor(), 9)
	items := filteredHistoryItems()
	if len(items) == 0 {
		drawText(hdc, "По этому фильтру записей пока нет.", int(body.Left)+18, int(app.historyFilterRects[0].Bottom)+34, int(body.Right-body.Left)-36, 30, 12, 500, theme.muted, DT_LEFT|DT_VCENTER|DT_SINGLELINE)
		drawOutlinedButton(hdc, app.historyClearRect, "Очистить", theme.danger)
		return
	}
	if ui2d.active {
		d2dPushClip(app.historyListClip)
	}
	for i, r := range app.historyRows {
		if i >= app.historyVisible || r.Right <= r.Left || r.Bottom <= app.historyListClip.Top || r.Top >= app.historyListClip.Bottom {
			continue
		}
		idx := app.historyScroll + i
		if idx >= len(items) {
			continue
		}
		it := items[idx]
		c := surfaceButtonColor()
		if it.Kind == "ERROR" {
			c = blendColor(c, theme.danger, .10)
		}
		roundFill(hdc, r, c, 10)
		drawText(hdc, it.When, int(r.Left)+14, int(r.Top)+5, 145, 17, 10, 550, theme.muted, DT_LEFT|DT_VCENTER|DT_SINGLELINE)
		titleColor := theme.text
		if it.Kind == "ERROR" {
			titleColor = theme.danger
		}
		drawText(hdc, historyKindTitle(it.Kind), int(r.Left)+164, int(r.Top)+5, int(r.Right-r.Left)-178, 17, 11, 650, titleColor, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
		if it.Detail != "" {
			drawText(hdc, historyDisplayDetail(it.Detail), int(r.Left)+14, int(r.Top)+26, int(r.Right-r.Left)-28, 16, 10, 400, theme.muted, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
		}
	}
	if ui2d.active {
		d2dPopClip()
	}
	drawScrollBar(hdc, app.historyScrollTrack, app.historyScrollThumb)
	drawOutlinedButton(hdc, app.historyClearRect, "Очистить", theme.danger)
}

func historyDetailItems() []HistoryItem {
	if !app.historyDetailOpen {
		return nil
	}
	run := app.historyDetailItem.RunID
	if run == "" {
		return []HistoryItem{app.historyDetailItem}
	}
	out := make([]HistoryItem, 0, 16)
	for _, it := range app.historyItems {
		if it.RunID == run {
			out = append(out, it)
		}
	}
	// Main history is newest-first; a run trace is easier to read chronologically.
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

func drawHistoryDetail(hdc uintptr, body RECT) {
	drawButton(hdc, app.historyDetailBackRect, "← Назад", false)
	it := app.historyDetailItem
	title := historyKindTitle(it.Kind)
	if it.RunID != "" {
		title += " · запуск " + shortenMiddle(it.RunID, 22)
	}
	drawText(hdc, title, int(app.historyDetailBackRect.Right)+16, int(app.historyDetailBackRect.Top), int(body.Right-app.historyDetailBackRect.Right)-34, 34, 14, 650, theme.text, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
	items := historyDetailItems()
	first := 0
	if len(app.historyDetailRows) > 0 {
		stride := 65
		first = int(app.historyDetailScrollPx) / stride
	}
	if ui2d.active {
		d2dPushClip(app.historyDetailListClip)
	}
	for i, r := range app.historyDetailRows {
		idx := first + i
		if idx >= len(items) || r.Right <= r.Left || r.Bottom <= app.historyDetailListClip.Top || r.Top >= app.historyDetailListClip.Bottom {
			continue
		}
		h := items[idx]
		c := surfaceButtonColor()
		if h.Kind == "ERROR" || h.Kind == "STEP_STOP" {
			c = blendColor(c, theme.danger, .10)
		}
		if h.Kind == "EXECUTE" || h.Kind == "CONDITIONS_OK" {
			c = blendColor(c, theme.success, .07)
		}
		roundFill(hdc, r, c, 10)
		drawText(hdc, h.When, int(r.Left)+14, int(r.Top)+6, 145, 17, 10, 550, theme.muted, DT_LEFT|DT_VCENTER|DT_SINGLELINE)
		drawText(hdc, historyKindTitle(h.Kind), int(r.Left)+164, int(r.Top)+6, int(r.Right-r.Left)-178, 17, 11, 650, theme.text, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
		detail := historyDisplayDetail(h.Detail)
		if detail == "" {
			detail = "—"
		}
		drawText(hdc, detail, int(r.Left)+14, int(r.Top)+29, int(r.Right-r.Left)-28, 22, 10, 400, theme.muted, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
	}
	if ui2d.active {
		d2dPopClip()
	}
	drawScrollBar(hdc, app.historyDetailScrollTrack, app.historyDetailScrollThumb)
}

func filteredHistoryItems() []HistoryItem {
	q := strings.ToLower(strings.TrimSpace(app.historySearchText))
	if q == "" && app.historyCacheValid && app.historyFilterCache == app.historyFilter {
		return app.historyFiltered
	}
	app.historyFilterCache = app.historyFilter
	app.historyCacheValid = q == ""
	out := make([]HistoryItem, 0, len(app.historyItems))
	for _, it := range app.historyItems {
		keep := app.historyFilter == 0
		switch app.historyFilter {
		case 1:
			keep = it.Kind == "START" || it.Kind == "CANCEL" || it.Kind == "EXECUTE" || it.Kind == "SAVE" || it.Kind == "LOAD" || it.Kind == "DELETE" || it.Kind == "EDIT" || it.Kind == "DRYRUN"
		case 2:
			keep = it.Kind == "AUTO_START" || it.Kind == "STEP" || it.Kind == "STEP_STOP" || it.Kind == "TRIGGER" || it.Kind == "CONDITIONS_OK" || it.Kind == "WAIT" || it.Kind == "EXPORT" || it.Kind == "IMPORT" || it.Kind == "BACKUP" || it.Kind == "RESTORE" || it.Kind == "SAFETY" || it.Kind == "WAKE_ARM"
		case 3:
			keep = it.Kind == "ERROR"
		}
		if keep && q != "" {
			hay := strings.ToLower(it.When + " " + historyKindTitle(it.Kind) + " " + historyDisplayDetail(it.Detail) + " " + it.RunID)
			keep = strings.Contains(hay, q)
		}
		if keep {
			out = append(out, it)
		}
	}
	app.historyFiltered = out
	return app.historyFiltered
}

func invalidateHistoryFilterCache() {
	app.historyCacheValid = false
	app.historyFiltered = nil
}

func drawHistoryClearConfirmation(hdc uintptr, body RECT) {
	// In-window confirmation, consistent with saved-task deletion confirmation.
	fill(hdc, body, blendColor(theme.bg, surfacePanelColor(), .44))
	boxW := minInt(440, int(body.Right-body.Left)-48)
	if boxW < 300 {
		boxW = int(body.Right-body.Left) - 24
	}
	boxH := 174
	x := int(body.Left) + (int(body.Right-body.Left)-boxW)/2
	y := int(body.Top) + (int(body.Bottom-body.Top)-boxH)/2
	app.confirmClearOverlayRect = RECT{int32(x), int32(y), int32(x + boxW), int32(y + boxH)}
	roundFill(hdc, app.confirmClearOverlayRect, surfaceButtonColor(), 16)
	drawText(hdc, "Очистить всю историю?", x+20, y+18, boxW-40, 26, 18, 650, theme.text, DT_LEFT|DT_VCENTER|DT_SINGLELINE)
	drawText(hdc, "Записи о запусках, отменах и сохранённых задачах будут удалены.", x+20, y+52, boxW-40, 44, 12, 400, theme.muted, DT_LEFT|DT_VCENTER)
	btnY := y + 116
	app.confirmClearNoRect = RECT{int32(x + 20), int32(btnY), int32(x + boxW/2 - 6), int32(btnY + 40)}
	app.confirmClearYesRect = RECT{int32(x + boxW/2 + 6), int32(btnY), int32(x + boxW - 20), int32(btnY + 40)}
	drawButton(hdc, app.confirmClearNoRect, "Отмена", false)
	drawOutlinedButton(hdc, app.confirmClearYesRect, "Очистить", theme.danger)
}

func historyKindTitle(kind string) string {
	switch kind {
	case "START":
		return "Задача запущена"
	case "CANCEL":
		return "Задача отменена"
	case "POSTPONE":
		return "Задача отложена"
	case "EXECUTE":
		return "Действие выполнено"
	case "AUTO_START":
		return "Автозапуск по расписанию"
	case "STEP":
		return "Шаг сценария"
	case "SAFETY":
		return "Защитное правило"
	case "ERROR":
		return "Ошибка"
	case "EXPORT":
		return "Экспорт задач"
	case "IMPORT":
		return "Импорт задач"
	case "BACKUP":
		return "Резервная копия"
	case "RESTORE":
		return "Восстановление"
	case "SAVE":
		return "Задача сохранена"
	case "LOAD":
		return "Сохранённая задача загружена"
	case "DELETE":
		return "Сохранённая задача удалена"
	case "EDIT":
		return "Сохранённая задача изменена"
	case "DRYRUN":
		return "Тестовый прогон"
	case "STEP_STOP":
		return "Сценарий остановлен"
	case "WAKE_ARM":
		return "Пробуждение запланировано"
	case "TRIGGER":
		return "Основной триггер выполнен"
	case "CONDITIONS_OK":
		return "Условия выполнены"
	case "WAIT":
		return "Ожидание условия"
	default:
		return kind
	}
}

func drawSavedTasksPage(hdc uintptr, body RECT, w int) {
	drawText(hdc, "Сохранённые задачи", int(body.Left)+18, int(body.Top)+12, 320, 28, 20, 650, theme.text, DT_LEFT|DT_VCENTER|DT_SINGLELINE)
	roundFill(hdc, app.savedSearchRect, surfaceButtonColor(), 9)
	if len(app.settings.SavedTasks) == 0 {
		drawText(hdc, "Пока ничего не сохранено. Настройте задачу и нажмите «Сохранить задачу».", int(body.Left)+18, int(body.Top)+78, int(body.Right-body.Left)-36, 44, 13, 400, theme.muted, DT_LEFT|DT_VCENTER)
		return
	}
	if len(app.savedFilteredIndices) == 0 {
		drawText(hdc, "По этому запросу сохранённых задач не найдено.", int(body.Left)+18, int(body.Top)+104, int(body.Right-body.Left)-36, 34, 12, 500, theme.muted, DT_LEFT|DT_VCENTER)
		return
	}
	if ui2d.active {
		d2dPushClip(app.savedListClip)
	}
	for i, r := range app.savedRows {
		if i >= app.savedVisible || r.Right <= r.Left || r.Bottom <= app.savedListClip.Top || r.Top >= app.savedListClip.Bottom {
			continue
		}
		idx := savedUnderlyingIndex(i)
		if idx < 0 || idx >= len(app.settings.SavedTasks) {
			continue
		}
		t := app.settings.SavedTasks[idx]
		roundFill(hdc, r, surfaceButtonColor(), 12)
		rightReserve := 256
		nameColor := theme.text
		if t.Paused {
			nameColor = theme.muted
		}
		drawText(hdc, t.Name, int(r.Left)+16, int(r.Top)+8, int(r.Right-r.Left)-rightReserve, 20, 14, 650, nameColor, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
		drawText(hdc, savedTaskSummary(t), int(r.Left)+16, int(r.Top)+34, int(r.Right-r.Left)-rightReserve, 18, 11, 400, theme.muted, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
		fav := "☆"
		if t.Favorite {
			fav = "★"
		}
		drawText(hdc, fav, int(app.savedFavoriteRects[i].Left), int(app.savedFavoriteRects[i].Top)-1, int(app.savedFavoriteRects[i].Right-app.savedFavoriteRects[i].Left), int(app.savedFavoriteRects[i].Bottom-app.savedFavoriteRects[i].Top), 18, 650, func() uint32 {
			if t.Favorite {
				return theme.accent2
			}
			return theme.muted
		}(), DT_CENTER|DT_VCENTER|DT_SINGLELINE)
		running := app.schedule.active && app.schedule.sourceTaskID == t.ID
		if running {
			drawOutlinedButton(hdc, app.savedRunRects[i], "Остановить", theme.danger)
		} else {
			drawButton(hdc, app.savedRunRects[i], "Запустить", true)
		}
		pauseIcon := scenarioIconPause
		if t.Paused {
			pauseIcon = scenarioIconPlay
		}
		drawScenarioIconButton(hdc, app.savedPauseRects[i], pauseIcon)
		// Vertical ellipsis: menu, not a destructive action by itself.
		roundFill(hdc, app.savedMenuButtonRects[i], surfacePanelColor(), 9)
		drawText(hdc, "⋮", int(app.savedMenuButtonRects[i].Left), int(app.savedMenuButtonRects[i].Top)-2, int(app.savedMenuButtonRects[i].Right-app.savedMenuButtonRects[i].Left), int(app.savedMenuButtonRects[i].Bottom-app.savedMenuButtonRects[i].Top), 20, 650, theme.text, DT_CENTER|DT_VCENTER|DT_SINGLELINE)
	}
	if ui2d.active {
		d2dPopClip()
	}
	drawScrollBar(hdc, app.savedScrollTrack, app.savedScrollThumb)
	if app.savedMenuOpenIdx >= 0 && app.savedPopupRect.Right > app.savedPopupRect.Left && app.savedMenuAnim > .01 {
		full := app.savedPopupRect
		ease := 1 - (1-app.savedMenuAnim)*(1-app.savedMenuAnim)*(1-app.savedMenuAnim)
		animBottom := full.Top + int32(float64(full.Bottom-full.Top)*ease)
		popup := RECT{full.Left, full.Top, full.Right, animBottom}
		roundFill(hdc, popup, surfacePanelColor(), 12)
		if ui2d.active {
			d2dPushClip(popup)
		}
		drawButton(hdc, app.savedPopupEditRect, "Редактировать", false)
		drawButton(hdc, app.savedPopupDuplicateRect, "Создать копию", false)
		drawOutlinedButton(hdc, app.savedPopupDeleteRect, "Удалить", theme.danger)
		if ui2d.active {
			d2dPopClip()
		}
	}
	if app.confirmDeleteIdx >= 0 && app.confirmDeleteIdx < len(app.settings.SavedTasks) {
		drawDeleteConfirmation(hdc, body, app.settings.SavedTasks[app.confirmDeleteIdx])
	}
}

func drawSavedTaskEditor(hdc uintptr, body RECT, w int) {
	drawText(hdc, "Редактирование сохранённой задачи", int(body.Left)+18, int(body.Top)+14, int(body.Right-body.Left)-36, 26, 19, 650, theme.text, DT_CENTER|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
	kindNames := []string{"Простая", "Продвинутая"}
	for i, r := range app.savedEditKindRects {
		drawSelectableButton(hdc, r, kindNames[i], app.savedEditDraft.TaskKind == i)
	}
	actions := []string{"Выключение", "Перезагрузка", "Сон", "Гибернация", "Завершить"}
	for i, r := range app.savedEditActionRects {
		if r.Right > r.Left {
			drawSelectableButton(hdc, r, actions[i], app.savedEditDraft.Action == i)
		}
	}
	modes := []string{"Таймер", "Дата/время", "Простой", "После процесса", "Расписание", "По условиям"}
	for i, r := range app.savedEditModeRects {
		if r.Right > r.Left {
			drawSelectableButton(hdc, r, modes[i], app.savedEditDraft.Mode == i)
		}
	}
	savedReveal := ui2d.active && app.pageAnim >= 1 && app.subRevealAnim < 1
	if savedReveal {
		d2dSetTranslation(0, float32(subRevealOffset()))
	}
	fieldY := int(maxRectBottom(app.savedEditModeRects[:])) + 10
	drawText(hdc, "Название", int(body.Left)+18, fieldY, 150, 16, 10, 500, theme.muted, DT_LEFT|DT_VCENTER|DT_SINGLELINE)
	uiDrawWhenFieldChrome(hdc, app.savedEditDraft.Mode, app.savedEditModeRects[:], body, "")
	if app.savedEditDraft.Mode == 2 && app.whenFieldRect.Right > app.whenFieldRect.Left {
		drawText(hdc, "сек", int(app.whenFieldRect.Right)+7, int(app.whenFieldRect.Top), 34, int(app.whenFieldRect.Bottom-app.whenFieldRect.Top), 10, 500, theme.muted, DT_LEFT|DT_VCENTER|DT_SINGLELINE)
	}
	if app.savedEditDraft.Mode == 3 {
		drawButton(hdc, app.savedEditProcessRect, "Выбрать процесс", false)
		if strings.TrimSpace(app.savedEditDraft.WatchProcess) != "" {
			drawSmallGlyphButton(hdc, app.savedEditClearRect, "×", theme.danger)
		}
	}
	if app.savedEditDraft.Mode == 4 {
		kindNames := []string{"Каждый день", "Будни", "Выбранные дни"}
		for i, r := range app.recurrenceKindRects {
			if r.Right > r.Left {
				drawSelectableButton(hdc, r, kindNames[i], app.savedEditDraft.Recurrence.Kind == i)
			}
		}
		dayNames := []string{"Пн", "Вт", "Ср", "Чт", "Пт", "Сб", "Вс"}
		for i, r := range app.recurrenceDayRects {
			if r.Right > r.Left {
				active := app.savedEditDraft.Recurrence.Days[i]
				if app.savedEditDraft.Recurrence.Kind == 0 {
					active = true
				}
				if app.savedEditDraft.Recurrence.Kind == 1 {
					active = i < 5
				}
				drawSelectableButton(hdc, r, dayNames[i], active)
			}
		}
	}
	if app.savedEditDraft.TaskKind == 0 {
		drawToggle(hdc, app.savedEditCloseRect, app.savedEditDraft.CloseBefore)
		drawText(hdc, "Закрывать процессы перед финальным действием", int(app.savedEditCloseRect.Right)+10, int(app.savedEditCloseRect.Top), int(body.Right)-int(app.savedEditCloseRect.Right)-28, 26, 10, 550, theme.text, DT_LEFT|DT_VCENTER|DT_SINGLELINE)
		if app.savedEditDraft.Mode != 3 {
			drawButton(hdc, app.savedEditProcessRect, fmt.Sprintf("Процессы: %d", len(app.savedEditDraft.Processes)), false)
		}
	}
	pr, fr, sr := uiInlineNumberLayout("Предупреждение за", "сек.", int(body.Left)+18, int(app.savedEditWarningRect.Top), int(body.Right)-18, 3)
	uiDrawInlineNumber(hdc, "Предупреждение за", "сек.", pr, fr, sr)
	if app.savedEditDraft.TaskKind == 1 && app.savedEditScenarioRect.Right > app.savedEditScenarioRect.Left {
		drawButton(hdc, app.savedEditScenarioRect, "Редактировать блок-схему…", true)
		drawText(hdc, fmt.Sprintf("%d условий · %d шагов", len(app.savedEditDraft.Conditions), len(app.savedEditDraft.Steps)), int(app.savedEditScenarioRect.Right)+12, int(app.savedEditScenarioRect.Top), max(90, int(body.Right-app.savedEditScenarioRect.Right)-26), int(app.savedEditScenarioRect.Bottom-app.savedEditScenarioRect.Top), 10, 500, theme.muted, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
	}
	drawButton(hdc, app.savedEditSaveRect, "Сохранить", true)
	drawButton(hdc, app.savedEditCancelRect, "Отмена", false)
	if savedReveal {
		d2dResetTransform()
	}
}

func drawSaveTaskPage(hdc uintptr, body RECT, w int) {
	drawText(hdc, "Сохранить задачу", int(body.Left)+18, int(body.Top)+18, 300, 28, 20, 650, theme.text, DT_LEFT|DT_VCENTER|DT_SINGLELINE)
	drawText(hdc, "Имя сохранённой задачи", int(body.Left)+18, int(body.Top)+70, 260, 18, 12, 500, theme.muted, DT_LEFT|DT_VCENTER|DT_SINGLELINE)
	kind := "Простая"
	if app.currentTaskKind == 1 {
		kind = "Продвинутая"
	}
	drawText(hdc, kind, int(body.Left)+18, int(body.Top)+142, int(body.Right-body.Left)-36, 22, 12, 500, theme.muted, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
	drawButton(hdc, app.saveConfirmRect, "Сохранить", true)
	drawButton(hdc, app.saveBackRect, "Назад", false)
}

func drawProcessesPage(hdc uintptr, body RECT, w int) {
	title := "Выбор процессов для закрытия"
	selectedList := app.settings.Processes
	if app.processPickerMode == 1 {
		title = "Защищённые процессы"
		selectedList = app.settings.SafetyProcesses
	}
	if app.processPickerMode == 2 {
		title = "Процесс, завершения которого ждать"
		if app.settings.WatchProcess != "" {
			selectedList = []string{app.settings.WatchProcess}
		} else {
			selectedList = nil
		}
	}
	if app.processPickerMode == 3 {
		title = "Выберите процесс для условия"
		if app.conditionDraft.Text != "" {
			selectedList = []string{app.conditionDraft.Text}
		} else {
			selectedList = nil
		}
	}
	if app.processPickerMode == 4 {
		title = "Процессы сохранённой задачи"
		selectedList = app.savedEditDraft.Processes
	}
	if app.processPickerMode == 5 {
		title = "Процесс сохранённой задачи"
		if app.savedEditDraft.WatchProcess != "" {
			selectedList = []string{app.savedEditDraft.WatchProcess}
		} else {
			selectedList = nil
		}
	}
	if app.processPickerMode == 7 {
		title = "Процесс для изменения приоритета"
		if app.stepDraft.Text != "" {
			selectedList = []string{app.stepDraft.Text}
		} else {
			selectedList = nil
		}
	}
	if app.processPickerMode == 6 {
		title = "Процессы шага"
		selectedList = app.stepDraft.Processes
	}
	drawText(hdc, title, int(body.Left)+18, int(body.Top)+16, 340, 26, 19, 650, theme.text, DT_LEFT|DT_VCENTER|DT_SINGLELINE)
	if app.settings.ShowSystemProcesses {
		filterNames := []string{"Все процессы", "Системные", "Несистемные"}
		for i, r := range app.processFilterRects {
			active := app.processFilter == i
			drawSelectableButton(hdc, r, filterNames[i], active)
		}
	}
	selected := map[string]bool{}
	for _, n := range selectedList {
		selected[strings.ToLower(n)] = true
	}
	if ui2d.active {
		d2dPushClip(app.processListClip)
	}
	for i, r := range app.processRows {
		if i >= app.processVisible || r.Right <= r.Left || r.Bottom <= app.processListClip.Top || r.Top >= app.processListClip.Bottom {
			continue
		}
		idx := app.processScroll + i
		if idx >= len(app.pickerItems) {
			continue
		}
		n := app.pickerItems[idx]
		isSys := app.pickerSystem[strings.ToLower(n)]
		c := surfaceButtonColor()
		if selected[strings.ToLower(n)] {
			c = blendColor(c, theme.accent, .30)
		}
		rv, hv := hoverCardRect(r)
		if hv > 0 {
			c = blendColor(c, theme.accent2, .06*hv)
		}
		roundFill(hdc, rv, c, 9)
		if hv > 0 && !selected[strings.ToLower(n)] && ui2d.active {
			d2dDrawRoundedOutline(rv, 9, float32(1+0.35*hv), blendColor(theme.border, theme.accent2, .40))
		}
		sq := RECT{r.Left + 10, r.Top + 7, r.Left + 30, r.Top + 27}
		drawToggle(hdc, sq, selected[strings.ToLower(n)])
		textW := int(r.Right-r.Left) - 50
		if isSys {
			drawText(hdc, "Системный", int(r.Right)-90, int(r.Top), 78, int(r.Bottom-r.Top), 10, 600, theme.danger, DT_RIGHT|DT_VCENTER|DT_SINGLELINE)
			textW -= 88
		}
		drawText(hdc, n, int(r.Left)+40, int(r.Top), textW, int(r.Bottom-r.Top), 13, 500, theme.text, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
	}
	if ui2d.active {
		d2dPopClip()
	}
	drawScrollBar(hdc, app.processScrollTrack, app.processScrollThumb)
	drawButton(hdc, app.processDoneRect, "Готово", true)
}

func scrollThumbRect(track RECT, total, visible, offset int) RECT {
	if total <= 0 || visible <= 0 {
		return RECT{}
	}
	return scrollThumbRectPixels(track, total, visible, float64(offset))
}

func scrollThumbRectPixels(track RECT, contentHeight, viewportHeight int, offset float64) RECT {
	if track.Right <= track.Left || track.Bottom <= track.Top || contentHeight <= viewportHeight || viewportHeight <= 0 {
		return RECT{}
	}
	h := int(track.Bottom - track.Top)
	thumbH := max(26, int(float64(h)*float64(viewportHeight)/float64(contentHeight)))
	if thumbH > h {
		thumbH = h
	}
	maxOffset := float64(max(1, contentHeight-viewportHeight))
	travel := max(0, h-thumbH)
	top := int(track.Top) + int(float64(travel)*clampFloat(offset, 0, maxOffset)/maxOffset)
	return RECT{track.Left - 2, int32(top), track.Right + 2, int32(top + thumbH)}
}

func drawScrollBar(hdc uintptr, track, thumb RECT) {
	if thumb.Right <= thumb.Left || thumb.Bottom <= thumb.Top {
		return
	}
	roundFill(hdc, track, blendColor(surfacePanelColor(), theme.border, .34), 3)
	roundFill(hdc, thumb, blendColor(surfaceButtonColor(), theme.accent2, .28), 5)
}

func queueSmoothScroll(wheelDelta int16) {
	if wheelDelta == 0 {
		return
	}
	step := 60.0 * float64(wheelDelta) / 120.0
	switch {
	case app.notificationPanelOpen && pointIn(app.notificationPanelRect, app.mouseX, app.mouseY):
		app.notificationScrollTarget = clampFloat(app.notificationScrollTarget-step, 0, app.notificationScrollMax)
	case app.section == 3 && app.settingsSubpage != 2:
		app.settingsScrollTarget = clampFloat(app.settingsScrollTarget-step, 0, app.settingsScrollMax)
	case app.section == 3 && app.settingsSubpage == 2 && app.historyDetailOpen:
		maxPx := scrollMaxPx(4)
		app.historyDetailScrollTarget = clampFloat(app.historyDetailScrollTarget-step, 0, maxPx)
	case app.section == 3 && app.settingsSubpage == 2:
		maxPx := scrollMaxPx(1)
		app.historyScrollTarget = clampFloat(app.historyScrollTarget-step, 0, maxPx)
	case app.section == 4:
		maxPx := scrollMaxPx(2)
		app.processScrollTarget = clampFloat(app.processScrollTarget-step, 0, maxPx)
	case app.section == 7 || app.section == 13:
		maxPx := scrollMaxPx(5)
		app.scenarioScrollTarget = clampFloat(app.scenarioScrollTarget-step, 0, maxPx)
	case app.section == 5:
		maxPx := scrollMaxPx(3)
		app.savedScrollTarget = clampFloat(app.savedScrollTarget-step, 0, maxPx)
		if app.savedMenuOpenIdx >= 0 {
			app.savedMenuTarget = 0
			app.savedMenuPendingClose = true
		}
	case app.section == 19:
		maxPx := scrollMaxPx(6)
		app.resourceProcScrollTarget = clampFloat(app.resourceProcScrollTarget-step, 0, maxPx)
	}
}

func scrollMaxPx(kind int) float64 {
	var total, stride int
	var clip RECT
	switch kind {
	case 1:
		total, stride, clip = len(filteredHistoryItems()), 54, app.historyListClip
	case 2:
		total, stride, clip = len(app.pickerItems), 38, app.processListClip
	case 3:
		total, stride, clip = len(app.savedFilteredIndices), 76, app.savedListClip
	case 4:
		total, stride, clip = len(historyDetailItems()), 65, app.historyDetailListClip
	case 5:
		total, stride, clip = max(len(visibleScenarioConditionIndices(currentScenarioConditions())), len(currentScenarioSteps())), 33, app.scenarioListClip
	case 6:
		total, stride, clip = advancedResourceItemCount(), 43, app.resourceProcListClip
	case 7:
		return app.settingsScrollMax
	case 8:
		return app.notificationScrollMax
	default:
		return 0
	}
	viewH := max(1, int(clip.Bottom-clip.Top))
	contentH := max(0, total*stride-(stride-(stride-6)))
	// Correct the gap component for each list.
	if kind == 2 {
		contentH = max(0, total*38-4)
	}
	if kind == 3 {
		contentH = max(0, total*76-8)
	}
	if kind == 1 {
		contentH = max(0, total*54-6)
	}
	if kind == 4 {
		contentH = max(0, total*65-7)
	}
	if kind == 5 {
		contentH = max(0, total*33-4)
	}
	if kind == 6 {
		contentH = max(0, total*43-4)
	}
	return float64(max(0, contentH-viewH))
}

func beginScrollbarInteraction(x, y int32) bool {
	kind := 0
	track, thumb := RECT{}, RECT{}
	switch {
	case app.notificationPanelOpen && app.notificationScrollMax > 0:
		kind, track, thumb = 8, app.notificationScrollTrack, app.notificationScrollThumb
	case app.section == 3 && app.settingsSubpage != 2 && app.settingsScrollMax > 0:
		kind, track, thumb = 7, app.settingsScrollTrack, app.settingsScrollThumb
	case app.section == 3 && app.settingsSubpage == 2 && app.historyDetailOpen:
		kind, track, thumb = 4, app.historyDetailScrollTrack, app.historyDetailScrollThumb
	case app.section == 3 && app.settingsSubpage == 2:
		kind, track, thumb = 1, app.historyScrollTrack, app.historyScrollThumb
	case app.section == 4:
		kind, track, thumb = 2, app.processScrollTrack, app.processScrollThumb
	case app.section == 5:
		kind, track, thumb = 3, app.savedScrollTrack, app.savedScrollThumb
	case app.section == 7 || app.section == 13:
		kind, track, thumb = 5, app.scenarioScrollTrack, app.scenarioScrollThumb
	case app.section == 19:
		kind, track, thumb = 6, app.resourceProcScrollTrack, app.resourceProcScrollThumb
	}
	if kind == 0 || (!pointIn(track, x, y) && !pointIn(thumb, x, y)) {
		return false
	}
	if pointIn(thumb, x, y) {
		app.draggingScrollKind = kind
		app.dragScrollGrabOffset = float64(y - thumb.Top)
		pSetCapture.Call(app.hwnd)
		return true
	}
	// Track click recenters the thumb and then smoothly glides there.
	setScrollTargetFromY(kind, float64(y), float64(thumb.Bottom-thumb.Top)/2)
	return true
}

func setScrollTargetFromY(kind int, y, grabOffset float64) {
	var track, thumb RECT
	switch kind {
	case 1:
		track, thumb = app.historyScrollTrack, app.historyScrollThumb
	case 2:
		track, thumb = app.processScrollTrack, app.processScrollThumb
	case 3:
		track, thumb = app.savedScrollTrack, app.savedScrollThumb
	case 4:
		track, thumb = app.historyDetailScrollTrack, app.historyDetailScrollThumb
	case 5:
		track, thumb = app.scenarioScrollTrack, app.scenarioScrollThumb
	case 6:
		track, thumb = app.resourceProcScrollTrack, app.resourceProcScrollThumb
	case 7:
		track, thumb = app.settingsScrollTrack, app.settingsScrollThumb
	case 8:
		track, thumb = app.notificationScrollTrack, app.notificationScrollThumb
	default:
		return
	}
	trackH := float64(max(1, int(track.Bottom-track.Top)))
	thumbH := float64(max(1, int(thumb.Bottom-thumb.Top)))
	travel := maxFloat(1, trackH-thumbH)
	pos := clampFloat(y-float64(track.Top)-grabOffset, 0, travel)
	target := pos / travel * scrollMaxPx(kind)
	switch kind {
	case 1:
		app.historyScrollTarget = target
	case 4:
		app.historyDetailScrollTarget = target
	case 2:
		app.processScrollTarget = target
	case 3:
		app.savedScrollTarget = target
		app.savedMenuTarget = 0
		app.savedMenuPendingClose = true
	case 5:
		app.scenarioScrollTarget = target
	case 6:
		app.resourceProcScrollTarget = target
	case 7:
		app.settingsScrollTarget = target
	case 8:
		app.notificationScrollTarget = target
	}
}

func dragScrollbarTo(y int32) {
	if app.draggingScrollKind == 0 {
		return
	}
	setScrollTargetFromY(app.draggingScrollKind, float64(y), app.dragScrollGrabOffset)
	switch app.draggingScrollKind {
	case 1:
		app.historyScrollPx = app.historyScrollTarget
	case 4:
		app.historyDetailScrollPx = app.historyDetailScrollTarget
	case 2:
		app.processScrollPx = app.processScrollTarget
	case 3:
		app.savedScrollPx = app.savedScrollTarget
	case 5:
		app.scenarioScrollPx = app.scenarioScrollTarget
	case 6:
		app.resourceProcScrollPx = app.resourceProcScrollTarget
	case 7:
		app.settingsScrollPx = app.settingsScrollTarget
	case 8:
		app.notificationScrollPx = app.notificationScrollTarget
	}
	updateScrollGeometry()
	invalidate(app.hwnd)
}

func updateScrollGeometry() {
	if app.notificationPanelOpen {
		layoutControls(app.hwnd)
		return
	}
	if app.section == 7 || app.section == 13 || (app.section == 3 && app.settingsSubpage != 2) {
		layoutControls(app.hwnd)
		return
	}
	// Lightweight list-only relayout used by inertial scrolling. Avoids touching every
	// native EDIT control on each animation frame.
	if app.section == 3 && app.settingsSubpage == 2 && app.historyDetailOpen && app.historyDetailListClip.Bottom > app.historyDetailListClip.Top {
		stride, rowH := 65, 58
		rem := int(app.historyDetailScrollPx) % stride
		for i := range app.historyDetailRows {
			y := int(app.historyDetailListClip.Top) - rem + i*stride
			app.historyDetailRows[i] = RECT{app.historyDetailListClip.Left, int32(y), app.historyDetailListClip.Right, int32(y + rowH)}
		}
		contentH := max(0, len(historyDetailItems())*stride-7)
		viewH := max(1, int(app.historyDetailListClip.Bottom-app.historyDetailListClip.Top))
		app.historyDetailScrollThumb = scrollThumbRectPixels(app.historyDetailScrollTrack, contentH, viewH, app.historyDetailScrollPx)
	}
	if app.section == 3 && app.settingsSubpage == 2 && app.historyListClip.Bottom > app.historyListClip.Top {
		stride, rowH := 54, 48
		first, rem := int(app.historyScrollPx)/stride, int(app.historyScrollPx)%stride
		app.historyScroll = first
		for i := range app.historyRows {
			y := int(app.historyListClip.Top) - rem + i*stride
			app.historyRows[i] = RECT{app.historyListClip.Left, int32(y), app.historyListClip.Right, int32(y + rowH)}
		}
		contentH := max(0, len(filteredHistoryItems())*stride-6)
		viewH := max(1, int(app.historyListClip.Bottom-app.historyListClip.Top))
		app.historyScrollThumb = scrollThumbRectPixels(app.historyScrollTrack, contentH, viewH, app.historyScrollPx)
	}
	if app.section == 4 && app.processListClip.Bottom > app.processListClip.Top {
		stride, rowH := 38, 34
		first, rem := int(app.processScrollPx)/stride, int(app.processScrollPx)%stride
		app.processScroll = first
		for i := range app.processRows {
			y := int(app.processListClip.Top) - rem + i*stride
			app.processRows[i] = RECT{app.processListClip.Left, int32(y), app.processListClip.Right, int32(y + rowH)}
		}
		contentH := max(0, len(app.pickerItems)*stride-4)
		viewH := max(1, int(app.processListClip.Bottom-app.processListClip.Top))
		app.processScrollThumb = scrollThumbRectPixels(app.processScrollTrack, contentH, viewH, app.processScrollPx)
	}
	if app.section == 19 && app.resourceProcListClip.Bottom > app.resourceProcListClip.Top {
		stride, rowH := 43, 39
		rem := int(app.resourceProcScrollPx) % stride
		for i := range app.resourceProcRows {
			y := int(app.resourceProcListClip.Top) - rem + i*stride
			app.resourceProcRows[i] = RECT{app.resourceProcListClip.Left, int32(y), app.resourceProcListClip.Right, int32(y + rowH)}
		}
		contentH := max(0, advancedResourceItemCount()*stride-4)
		viewH := max(1, int(app.resourceProcListClip.Bottom-app.resourceProcListClip.Top))
		app.resourceProcScrollThumb = scrollThumbRectPixels(app.resourceProcScrollTrack, contentH, viewH, app.resourceProcScrollPx)
	}
	if app.section == 5 && app.savedListClip.Bottom > app.savedListClip.Top {
		stride, rowH := 76, 68
		first, rem := int(app.savedScrollPx)/stride, int(app.savedScrollPx)%stride
		app.savedScroll = first
		rowRight := app.savedListClip.Right
		for i := range app.savedRows {
			y := int(app.savedListClip.Top) - rem + i*stride
			app.savedRows[i] = RECT{app.savedListClip.Left, int32(y), rowRight, int32(y + rowH)}
			app.savedMenuButtonRects[i] = RECT{rowRight - 48, int32(y + 14), rowRight - 12, int32(y + 54)}
			app.savedPauseRects[i] = RECT{rowRight - 90, int32(y + 14), rowRight - 54, int32(y + 54)}
			app.savedRunRects[i] = RECT{rowRight - 196, int32(y + 14), rowRight - 100, int32(y + 54)}
			app.savedFavoriteRects[i] = RECT{rowRight - 232, int32(y + 18), rowRight - 204, int32(y + 50)}
		}
		contentH := max(0, len(app.savedFilteredIndices)*stride-8)
		viewH := max(1, int(app.savedListClip.Bottom-app.savedListClip.Top))
		app.savedScrollThumb = scrollThumbRectPixels(app.savedScrollTrack, contentH, viewH, app.savedScrollPx)
		if app.savedMenuOpenIdx >= 0 {
			local := savedLocalForUnderlying(app.savedMenuOpenIdx)
			if local >= 0 && local < len(app.savedMenuButtonRects) {
				btn := app.savedMenuButtonRects[local]
				menuW, menuH := int32(170), int32(118)
				x, y := btn.Right-menuW, btn.Bottom+5
				if y+menuH > app.savedListClip.Bottom {
					y = btn.Top - menuH - 5
				}
				app.savedPopupRect = RECT{x, y, x + menuW, y + menuH}
				app.savedPopupPauseRect = RECT{}
				app.savedPopupEditRect = RECT{x + 6, y + 6, x + menuW - 6, y + 38}
				app.savedPopupDuplicateRect = RECT{x + 6, y + 42, x + menuW - 6, y + 74}
				app.savedPopupDeleteRect = RECT{x + 6, y + 78, x + menuW - 6, y + 110}
			}
		}
	}
}

func clearEditFocusForCanvasClick(x, y int32) {
	// The rounded search chrome is slightly larger than the native EDIT. Treat the
	// whole visual field as an input target so clicking its padding keeps the caret.
	if app.section == 19 && app.resourceAdvancedView == 0 && pointIn(app.resourceProcessSearchRect, x, y) {
		pSetFocus.Call(app.edits[idResourceSearch])
		return
	}
	if app.section == 5 && pointIn(app.savedSearchRect, x, y) {
		pSetFocus.Call(app.edits[idSavedSearch])
		return
	}
	if app.section == 3 && app.settingsSubpage == 2 && pointIn(app.historySearchRect, x, y) {
		pSetFocus.Call(app.edits[idHistorySearch])
		return
	}
	focus, _, _ := pGetFocus.Call()
	if focus == 0 {
		return
	}
	isEdit := false
	for _, h := range app.edits {
		if h == focus {
			isEdit = true
			break
		}
	}
	if !isEdit {
		return
	}
	// A WM_LBUTTONDOWN delivered to the parent means the press began outside the child EDIT.
	// Thus clearing focus here does not interfere with selecting text by dragging out of an edit.
	pSetFocus.Call(app.hwnd)
	_ = x
	_ = y
}

func rectHoverKey(r RECT) int64 {
	return int64(uint64(uint32(r.Left))<<48 ^ uint64(uint32(r.Top))<<32 ^ uint64(uint32(r.Right))<<16 ^ uint64(uint32(r.Bottom)))
}

func expandRect(r RECT, d int32) RECT {
	if d <= 0 {
		return r
	}
	return RECT{r.Left - d, r.Top - d, r.Right + d, r.Bottom + d}
}

func hoverAmount(r RECT) float64 {
	if r.Right <= r.Left || r.Bottom <= r.Top {
		return 0
	}
	// An open task dropdown owns the pointer over its visual panel.  Underlying
	// page controls must not receive hover through the menu.
	if !drawingTaskNavigationMenu && ((app.taskMenuOpen && pointInTaskMenuPopup(app.mouseX, app.mouseY)) || (app.resourceMenuOpen && pointIn(resourceMenuPanelRect(), app.mouseX, app.mouseY))) {
		return 0
	}
	if !drawingNotificationPanel && app.notificationPanelOpen && pointIn(app.notificationPanelRect, app.mouseX, app.mouseY) {
		return 0
	}
	k := rectHoverKey(r)
	inside := pointIn(r, app.mouseX, app.mouseY)
	if inside {
		app.hoverSeen = true
		if app.hoverKey != k {
			app.hoverKey = k
			app.hoverRect = r
			// Start with a tiny visible response on the very first paint so hover never feels delayed.
			app.hoverAnim = .12
		} else {
			app.hoverRect = r
		}
	}
	if app.hoverKey != k {
		return 0
	}
	t := clampFloat(app.hoverAnim, 0, 1)
	// Smoothstep keeps the pop-up responsive without the old ease-out jump.
	return t * t * (3 - 2*t)
}

func hoverCardRect(r RECT) (RECT, float64) {
	h := hoverAmount(r)
	// Four pixels gives Direct2D enough intermediate positions to read as a real pop-up instead
	// of the old 0/1/2 px stepping.
	return expandRect(r, int32(4*h+.5)), h
}

func drawSelectableButton(hdc uintptr, r RECT, text string, active bool) {
	if r.Right <= r.Left || r.Bottom <= r.Top {
		return
	}
	h := hoverAmount(r)
	rv := expandRect(r, int32(4*h+.5))
	c := surfaceButtonColor()
	if active {
		c = theme.accent
	}
	if h > 0 {
		c = blendColor(c, theme.accent2, .08*h)
	}
	drawingInteractiveSurface = true
	roundFill(hdc, rv, c, 10)
	drawingInteractiveSurface = false
	if h > 0 && !active && ui2d.active {
		d2dDrawRoundedOutline(rv, 10, float32(1+0.5*h), blendColor(theme.border, theme.accent2, .50))
	}
	drawText(hdc, text, int(r.Left), int(r.Top), int(r.Right-r.Left), int(r.Bottom-r.Top), 13, 650, theme.text, DT_CENTER|DT_VCENTER|DT_SINGLELINE)
}

func drawCompactSortButton(hdc uintptr, r RECT, text string, active bool) {
	if r.Right <= r.Left || r.Bottom <= r.Top {
		return
	}
	h := hoverAmount(r)
	rv := expandRect(r, int32(h+.5))
	c := surfaceButtonColor()
	if active {
		c = blendColor(c, theme.accent, .72)
	}
	if h > 0 {
		c = blendColor(c, theme.accent2, .07*h)
	}
	roundFill(hdc, rv, c, 7)
	if h > 0 && !active && ui2d.active {
		d2dDrawRoundedOutline(rv, 7, 1, blendColor(theme.border, theme.accent2, .42))
	}
	drawText(hdc, text, int(r.Left)+2, int(r.Top), int(r.Right-r.Left)-4, int(r.Bottom-r.Top), 9, 650, theme.text, DT_CENTER|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
}

func drawButton(hdc uintptr, r RECT, text string, accent bool) {
	if r.Right <= r.Left || r.Bottom <= r.Top {
		return
	}
	h := hoverAmount(r)
	rv := expandRect(r, int32(4*h+.5))
	c := surfaceButtonColor()
	if accent {
		c = theme.accent
	}
	if h > 0 {
		c = blendColor(c, theme.accent2, .08*h)
	}
	drawingInteractiveSurface = true
	roundFill(hdc, rv, c, 10)
	drawingInteractiveSurface = false
	if h > 0 && ui2d.active {
		d2dDrawRoundedOutline(rv, 10, float32(1+0.5*h), blendColor(theme.border, theme.accent2, .50))
	}
	drawText(hdc, text, int(r.Left), int(r.Top), int(r.Right-r.Left), int(r.Bottom-r.Top), 13, 650, theme.text, DT_CENTER|DT_VCENTER|DT_SINGLELINE)
}

func drawDisabledButton(hdc uintptr, r RECT, text string) {
	if r.Right <= r.Left || r.Bottom <= r.Top {
		return
	}
	c := blendColor(surfaceButtonColor(), theme.panel, .35)
	roundFill(hdc, r, c, 10)
	drawText(hdc, text, int(r.Left), int(r.Top), int(r.Right-r.Left), int(r.Bottom-r.Top), 13, 650, blendColor(theme.muted, theme.panel, .20), DT_CENTER|DT_VCENTER|DT_SINGLELINE)
}

func drawOutlinedButton(hdc uintptr, r RECT, text string, border uint32) {
	if r.Right <= r.Left || r.Bottom <= r.Top {
		return
	}
	h := hoverAmount(r)
	rv := expandRect(r, int32(4*h+.5))
	bc := border
	if h > 0 {
		bc = blendColor(border, theme.text, .14*h)
	}
	roundFill(hdc, rv, bc, 10)
	inner := RECT{rv.Left + 2, rv.Top + 2, rv.Right - 2, rv.Bottom - 2}
	roundFill(hdc, inner, surfaceButtonColor(), 8)
	if h > 0 && ui2d.active {
		d2dDrawRoundedOutline(rv, 10, float32(1+0.45*h), bc)
	}
	drawText(hdc, text, int(r.Left), int(r.Top), int(r.Right-r.Left), int(r.Bottom-r.Top), 13, 650, border, DT_CENTER|DT_VCENTER|DT_SINGLELINE)
}

func beginSystemConfirmation(mode int, name string) {
	app.confirmSystemMode = mode
	app.pendingSystemProcess = name
	if mode == 1 {
		app.confirmSystemUnlockAt = time.Now().Add(3 * time.Second)
	} else {
		app.confirmSystemUnlockAt = time.Time{}
	}
	playUI(openSound)
	updateInputVisibility()
	invalidate(app.hwnd)
}

func drawSystemProcessConfirmation(hdc uintptr, rc RECT) {
	fill(hdc, RECT{0, 46, rc.Right, rc.Bottom}, blendColor(theme.bg, theme.panel, .52))
	boxW := minInt(500, int(rc.Right)-56)
	if boxW < 320 {
		boxW = int(rc.Right) - 24
	}
	boxH := 210
	x := (int(rc.Right) - boxW) / 2
	y := 96 + (int(rc.Bottom)-96-boxH)/2
	app.confirmSystemOverlayRect = RECT{int32(x), int32(y), int32(x + boxW), int32(y + boxH)}
	roundFill(hdc, app.confirmSystemOverlayRect, surfacePanelColor(), 18)
	title := "Показать системные процессы?"
	body := "Системные процессы принадлежат Windows или службам. Их завершение может привести к потере данных, выходу из системы или нестабильной работе Windows."
	confirm := "Показать"
	locked := false
	if app.confirmSystemMode == 1 && !app.confirmSystemUnlockAt.IsZero() {
		rem := time.Until(app.confirmSystemUnlockAt)
		if rem > 0 {
			locked = true
			confirm = fmt.Sprintf("Показать (%.1f)", rem.Seconds())
		} else {
			// Unlock in the paint path too so a cursor already resting on the button
			// immediately sees its enabled hover state on the exact zero frame.
			app.confirmSystemUnlockAt = time.Time{}
			app.hoverKey = 0
			app.hoverRect = RECT{}
			app.hoverAnim = 0
		}
	}
	if app.confirmSystemMode == 2 {
		title = "Взаимодействовать с системным процессом?"
		body = fmt.Sprintf("%s помечен PowerPilot как системный процесс. Выбирайте его только если точно понимаете последствия.", app.pendingSystemProcess)
		confirm = "Продолжить"
	}
	drawText(hdc, title, x+22, y+20, boxW-44, 26, 18, 700, theme.text, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
	drawText(hdc, body, x+22, y+56, boxW-44, 78, 11, 450, theme.muted, DT_LEFT|DT_VCENTER)
	app.confirmSystemNoRect = RECT{int32(x + 22), int32(y + 150), int32(x + boxW/2 - 6), int32(y + 190)}
	app.confirmSystemYesRect = RECT{int32(x + boxW/2 + 6), int32(y + 150), int32(x + boxW - 22), int32(y + 190)}
	drawButton(hdc, app.confirmSystemNoRect, "Отмена", false)
	if locked {
		roundFill(hdc, app.confirmSystemYesRect, blendColor(surfaceButtonColor(), theme.panel2, .35), 10)
		drawText(hdc, confirm, int(app.confirmSystemYesRect.Left), int(app.confirmSystemYesRect.Top), int(app.confirmSystemYesRect.Right-app.confirmSystemYesRect.Left), int(app.confirmSystemYesRect.Bottom-app.confirmSystemYesRect.Top), 13, 650, theme.muted, DT_CENTER|DT_VCENTER|DT_SINGLELINE)
	} else {
		drawOutlinedButton(hdc, app.confirmSystemYesRect, confirm, theme.danger)
	}
}

func drawDeleteConfirmation(hdc uintptr, body RECT, task SavedTask) {
	// In-window confirmation sheet, intentionally modal without opening another Win32 window.
	fill(hdc, body, blendColor(theme.bg, theme.panel, .48))
	boxW := minInt(430, int(body.Right-body.Left)-48)
	if boxW < 300 {
		boxW = int(body.Right-body.Left) - 24
	}
	boxH := 176
	x := int(body.Left) + (int(body.Right-body.Left)-boxW)/2
	y := int(body.Top) + (int(body.Bottom-body.Top)-boxH)/2
	app.confirmOverlayRect = RECT{int32(x), int32(y), int32(x + boxW), int32(y + boxH)}
	roundFill(hdc, app.confirmOverlayRect, surfaceButtonColor(), 16)
	drawText(hdc, "Удалить сохранённую задачу?", x+20, y+18, boxW-40, 26, 18, 650, theme.text, DT_LEFT|DT_VCENTER|DT_SINGLELINE)
	drawText(hdc, task.Name, x+20, y+52, boxW-40, 24, 13, 600, theme.muted, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
	drawText(hdc, "Это действие нельзя отменить.", x+20, y+80, boxW-40, 20, 12, 400, theme.muted, DT_LEFT|DT_VCENTER|DT_SINGLELINE)
	btnY := y + 118
	app.confirmDeleteNoRect = RECT{int32(x + 20), int32(btnY), int32(x + boxW/2 - 6), int32(btnY + 40)}
	app.confirmDeleteYesRect = RECT{int32(x + boxW/2 + 6), int32(btnY), int32(x + boxW - 20), int32(btnY + 40)}
	drawButton(hdc, app.confirmDeleteNoRect, "Отмена", false)
	drawOutlinedButton(hdc, app.confirmDeleteYesRect, "Удалить", theme.danger)
}

func animateConditionCatalog() bool {
	if !app.conditionCatalogAnimating {
		return false
	}
	// Time-based catalogue animation. Geometry is recalculated from one authoritative
	// progress value, but native EDIT HWNDs remain hidden for the whole transition.
	duration := 360 * time.Millisecond
	if app.settings.AnimationMode == 1 {
		duration = 280 * time.Millisecond
	}
	if app.settings.AnimationMode == 2 {
		duration = time.Millisecond
	}
	p := float64(time.Since(app.conditionCatalogStarted)) / float64(duration)
	p = clampFloat(p, 0, 1)
	// smootherstep: zero velocity and zero acceleration at both ends.
	e := p * p * p * (p*(p*6-15) + 10)
	app.conditionCatalogAnim = app.conditionCatalogFrom + (app.conditionCatalogTarget-app.conditionCatalogFrom)*e
	if app.section == 8 || (app.graphWindow != 0 && app.graphEditorSection == 8) {
		newTop := app.conditionCatalogBaseMoreY + int32(float64(app.conditionCatalogExtraFullH)*app.conditionCatalogAnim)
		shiftConditionEditorDynamicRects(newTop - app.conditionMoreRect.Top)
	}
	if p >= 1 {
		app.conditionCatalogAnim = app.conditionCatalogTarget
		app.conditionCatalogAnimating = false
		if app.section == 8 {
			positionConditionEditorInputs(true)
		}
	}
	return true
}

func animate() {
	changed := false
	for i := 0; i < 4; i++ {
		ta := 0.0
		if i == app.selectedAction {
			ta = 1
		} else if i == app.hoverAction && app.section == 0 {
			ta = .20
		}
		na := ta
		switch app.settings.AnimationMode {
		case 0:
			na = app.actionAnim[i] + (ta-app.actionAnim[i])*.32
		case 1:
			na = app.actionAnim[i] + (ta-app.actionAnim[i])*.55
		case 2:
			na = ta
		}
		if abs(ta-na) < .01 {
			na = ta
		}
		if na != app.actionAnim[i] {
			changed = true
			app.actionAnim[i] = na
		}
	}
	for i := 0; i < 5; i++ {
		tm := 0.0
		if i == app.selectedMode {
			tm = 1
		} else if i == app.hoverMode && app.section == 1 {
			tm = .20
		}
		nm := tm
		switch app.settings.AnimationMode {
		case 0:
			nm = app.modeAnim[i] + (tm-app.modeAnim[i])*.32
		case 1:
			nm = app.modeAnim[i] + (tm-app.modeAnim[i])*.55
		case 2:
			nm = tm
		}
		if abs(tm-nm) < .01 {
			nm = tm
		}
		if nm != app.modeAnim[i] {
			changed = true
			app.modeAnim[i] = nm
		}
	}
	if app.pageAnim < 1 {
		duration := 230 * time.Millisecond
		if app.settings.AnimationMode == 1 {
			duration = 165 * time.Millisecond
		}
		if app.pageAnimStarted.IsZero() {
			app.pageAnimStarted = time.Now()
		}
		p := float64(time.Since(app.pageAnimStarted)) / float64(duration)
		if p >= 1 {
			app.pageAnim = 1
			app.pageAnimStarted = time.Time{}
		} else if p > 0 {
			app.pageAnim = p
		}
		changed = true
	}
	if app.subRevealAnim < 1 {
		step := .30
		if app.settings.AnimationMode == 1 {
			step = .46
		}
		if app.settings.AnimationMode == 2 {
			step = 1
		}
		old := app.subRevealAnim
		app.subRevealAnim += (1 - app.subRevealAnim) * step
		if app.subRevealAnim > .995 {
			app.subRevealAnim = 1
		}
		if old != app.subRevealAnim {
			changed = true
		}
	}

	if animateConditionCatalog() {
		changed = true
	}

	// Generic hover pop-up / outline animation. The active rectangle is tracked explicitly,
	// so moving quickly between controls cannot inherit or "stick" another control's outline.
	hoverTarget := 0.0
	if app.hoverKey != 0 && pointIn(app.hoverRect, app.mouseX, app.mouseY) {
		hoverTarget = 1
	}
	hoverStep := .34
	if app.settings.AnimationMode == 1 {
		hoverStep = .46
	}
	if app.settings.AnimationMode == 2 {
		hoverStep = 1
	}
	oldHover := app.hoverAnim
	app.hoverAnim += (hoverTarget - app.hoverAnim) * hoverStep
	if abs(app.hoverAnim-hoverTarget) < .01 {
		app.hoverAnim = hoverTarget
	}
	if app.hoverAnim != oldHover {
		changed = true
	}
	if hoverTarget == 0 && app.hoverAnim == 0 {
		app.hoverKey = 0
		app.hoverRect = RECT{}
	}
	// Keep the lightweight animation timer repainting only until a delayed
	// scenario tooltip reaches its one-second reveal point.
	if app.tooltipText != "" && pointIn(app.tooltipRect, app.mouseX, app.mouseY) && time.Since(app.tooltipSince) < 1020*time.Millisecond {
		changed = true
	}

	gearTarget := 0.0
	if !app.miniMode && pointIn(app.settingsBtnRect, app.mouseX, app.mouseY) {
		gearTarget = 1
	}
	gearStep := .24
	if app.settings.AnimationMode == 1 {
		gearStep = .36
	}
	if app.settings.AnimationMode == 2 {
		gearStep = 1
	}
	oldGear := app.settingsHoverAnim
	app.settingsHoverAnim += (gearTarget - app.settingsHoverAnim) * gearStep
	if abs(app.settingsHoverAnim-gearTarget) < .008 {
		app.settingsHoverAnim = gearTarget
	}
	if app.settingsHoverAnim != oldGear {
		changed = true
	}

	// Notification bell burst: a short, self-contained effect that only repaints
	// while particles are actually moving. It never becomes a permanent idle animation.
	if !app.notificationBellBurstStarted.IsZero() {
		if time.Since(app.notificationBellBurstStarted) < 560*time.Millisecond && app.settings.AnimationMode != 2 {
			changed = true
		} else {
			app.notificationBellBurstStarted = time.Time{}
		}
	}

	// Pixel-based inertial list scrolling. Only the visible list is integrated on each frame;
	// this avoids useless geometry work and keeps wheel motion smooth on large histories.
	if app.draggingScrollKind == 0 {
		scrollStep := .24
		if app.settings.AnimationMode == 1 {
			scrollStep = .38
		}
		if app.settings.AnimationMode == 2 {
			scrollStep = 1
		}
		scrolled := false
		switch {
		case app.notificationPanelOpen:
			old := app.notificationScrollPx
			app.notificationScrollPx += (app.notificationScrollTarget - app.notificationScrollPx) * scrollStep
			if abs(app.notificationScrollPx-app.notificationScrollTarget) < .08 {
				app.notificationScrollPx = app.notificationScrollTarget
			}
			scrolled = old != app.notificationScrollPx
		case app.section == 3 && app.settingsSubpage != 2:
			old := app.settingsScrollPx
			app.settingsScrollPx += (app.settingsScrollTarget - app.settingsScrollPx) * scrollStep
			if abs(app.settingsScrollPx-app.settingsScrollTarget) < .08 {
				app.settingsScrollPx = app.settingsScrollTarget
			}
			scrolled = old != app.settingsScrollPx
		case app.section == 3 && app.settingsSubpage == 2 && app.historyDetailOpen:
			old := app.historyDetailScrollPx
			app.historyDetailScrollPx += (app.historyDetailScrollTarget - app.historyDetailScrollPx) * scrollStep
			if abs(app.historyDetailScrollPx-app.historyDetailScrollTarget) < .08 {
				app.historyDetailScrollPx = app.historyDetailScrollTarget
			}
			scrolled = old != app.historyDetailScrollPx
		case app.section == 3 && app.settingsSubpage == 2:
			old := app.historyScrollPx
			app.historyScrollPx += (app.historyScrollTarget - app.historyScrollPx) * scrollStep
			if abs(app.historyScrollPx-app.historyScrollTarget) < .08 {
				app.historyScrollPx = app.historyScrollTarget
			}
			scrolled = old != app.historyScrollPx
		case app.graphWindow != 0 && app.graphEditorOpen && app.graphEditorSection == 4:
			old := app.processScrollPx
			app.processScrollPx += (app.processScrollTarget - app.processScrollPx) * scrollStep
			if abs(app.processScrollPx-app.processScrollTarget) < .08 {
				app.processScrollPx = app.processScrollTarget
			}
			scrolled = old != app.processScrollPx
		case app.section == 4:
			old := app.processScrollPx
			app.processScrollPx += (app.processScrollTarget - app.processScrollPx) * scrollStep
			if abs(app.processScrollPx-app.processScrollTarget) < .08 {
				app.processScrollPx = app.processScrollTarget
			}
			scrolled = old != app.processScrollPx
		case app.section == 7 || app.section == 13:
			old := app.scenarioScrollPx
			app.scenarioScrollPx += (app.scenarioScrollTarget - app.scenarioScrollPx) * scrollStep
			if abs(app.scenarioScrollPx-app.scenarioScrollTarget) < .08 {
				app.scenarioScrollPx = app.scenarioScrollTarget
			}
			scrolled = old != app.scenarioScrollPx
		case app.section == 19:
			old := app.resourceProcScrollPx
			app.resourceProcScrollPx += (app.resourceProcScrollTarget - app.resourceProcScrollPx) * scrollStep
			if abs(app.resourceProcScrollPx-app.resourceProcScrollTarget) < .08 {
				app.resourceProcScrollPx = app.resourceProcScrollTarget
			}
			scrolled = old != app.resourceProcScrollPx
		case app.section == 5:
			old := app.savedScrollPx
			app.savedScrollPx += (app.savedScrollTarget - app.savedScrollPx) * scrollStep
			if abs(app.savedScrollPx-app.savedScrollTarget) < .08 {
				app.savedScrollPx = app.savedScrollTarget
			}
			scrolled = old != app.savedScrollPx
		}
		if scrolled {
			if app.graphWindow != 0 && app.graphEditorOpen && app.graphEditorSection == 4 {
				oldSection := app.section
				app.section = 4
				updateScrollGeometry()
				app.section = oldSection
			} else {
				updateScrollGeometry()
			}
			changed = true
		}
	}

	if abs(app.savedMenuAnim-app.savedMenuTarget) > .005 {
		// Slightly longer than the generic button hover so the popup reads as an intentional reveal.
		step := .13
		if app.settings.AnimationMode == 1 {
			step = .22
		}
		if app.settings.AnimationMode == 2 {
			step = 1
		}
		app.savedMenuAnim += (app.savedMenuTarget - app.savedMenuAnim) * step
		if abs(app.savedMenuAnim-app.savedMenuTarget) < .015 {
			app.savedMenuAnim = app.savedMenuTarget
		}
		changed = true
		if app.savedMenuAnim == 0 && app.savedMenuPendingClose {
			app.savedMenuPendingClose = false
			app.savedMenuOpenIdx = -1
			layoutControls(app.hwnd)
		}
	}
	if app.confirmSystemMode == 1 && !app.confirmSystemUnlockAt.IsZero() {
		if time.Now().Before(app.confirmSystemUnlockAt) {
			changed = true
		} else {
			app.confirmSystemUnlockAt = time.Time{}
			changed = true // force one final repaint so hover immediately switches to the enabled button
		}
	}
	dragTarget := 0.0
	if app.draggingScenarioKind != 0 {
		dragTarget = 1
	}
	if abs(app.dragGapAnim-dragTarget) > .005 {
		app.dragGapAnim += (dragTarget - app.dragGapAnim) * .28
		if abs(app.dragGapAnim-dragTarget) < .01 {
			app.dragGapAnim = dragTarget
		}
		changed = true
	}
	if changed {
		if app.pageAnim < 1 || app.subRevealAnim < 1 {
			shiftVisibleEditsForAnimation()
			invalidateVisibleEdits()
		}
		if app.graphWindow != 0 {
			invalidateScenarioGraphWindows()
		} else {
			invalidate(app.hwnd)
		}
	}
}

func onMouseMove(hwnd uintptr, x, y int32) {
	app.mouseX, app.mouseY = x, y
	if handleScenarioGraphMouseMove(x, y) {
		return
	}
	tooltipRect, tooltipText := scenarioTooltipAt(x, y)
	if tooltipRect != app.tooltipRect || tooltipText != app.tooltipText {
		app.tooltipRect = tooltipRect
		app.tooltipText = tooltipText
		if tooltipText == "" {
			app.tooltipSince = time.Time{}
		} else {
			app.tooltipSince = time.Now()
		}
	}
	bellInside := !app.miniMode && pointIn(app.notificationBtnRect, x, y)
	if bellInside && !app.notificationBellHover && app.settings.AnimationMode != 2 {
		app.notificationBellBurstStarted = time.Now()
	}
	app.notificationBellHover = bellInside
	if app.draggingScenarioKind != 0 {
		app.draggingScenarioY = y
		// Edge auto-scroll makes long block schemes draggable beyond the currently visible rows.
		if app.scenarioListClip.Bottom > app.scenarioListClip.Top {
			maxPx := scrollMaxPx(5)
			if y < app.scenarioListClip.Top+22 {
				app.scenarioScrollTarget = clampFloat(app.scenarioScrollTarget-14, 0, maxPx)
			}
			if y > app.scenarioListClip.Bottom-22 {
				app.scenarioScrollTarget = clampFloat(app.scenarioScrollTarget+14, 0, maxPx)
			}
		}
		updateScenarioDragTarget(x, y)
		invalidate(hwnd)
	}
	if app.draggingScrollKind != 0 {
		dragScrollbarTo(y)
	}
	if app.draggingVolume && app.section == 3 && app.settingsSubpage == 6 {
		setVolumeFromX(x, true)
	}
	if app.draggingTimelineTicks && app.section == 3 && app.settingsSubpage == 7 {
		setTimelineTicksFromX(x)
	}
	if !app.mouseTracking {
		t := TRACKMOUSEEVENT{CbSize: uint32(unsafe.Sizeof(TRACKMOUSEEVENT{})), DwFlags: 0x2, HwndTrack: hwnd}
		pTrackMouseEvent.Call(uintptr(unsafe.Pointer(&t)))
		app.mouseTracking = true
	}
	ha, hm := -1, -1
	menuOwnsPointer := (app.taskMenuOpen && pointInTaskMenuPopup(x, y)) || (app.resourceMenuOpen && pointIn(resourceMenuPanelRect(), x, y))
	if !menuOwnsPointer && app.section == 0 && !app.miniMode {
		for i, r := range app.actionRects {
			if pointIn(r, x, y) {
				ha = i
			}
		}
	}
	if !menuOwnsPointer && app.section == 1 && !app.miniMode {
		for i, r := range app.modeRects {
			if pointIn(r, x, y) {
				hm = i
			}
		}
	}
	ht := -1
	titleRects := []RECT{app.miniBtnRect, app.minBtnRect, app.maxBtnRect, app.closeBtnRect}
	for i, r := range titleRects {
		if pointIn(r, x, y) {
			ht = i
		}
	}
	changedHover := ha != app.hoverAction || hm != app.hoverMode || ht != app.hoverTitle
	app.hoverAction, app.hoverMode, app.hoverTitle = ha, hm, ht
	// Generic Direct2D controls are discovered during paint; repaint on pointer motion so
	// hover transitions start and stop immediately even when moving between adjacent buttons.
	if changedHover || app.draggingScrollKind == 0 {
		invalidate(hwnd)
	}
}

func openConditionEditor(idx int) {
	app.currentTaskKind = 1
	app.currentTaskSection = 7
	app.editingCondition = idx
	conds := currentScenarioConditions()
	if idx >= 0 && idx < len(conds) {
		app.conditionDraft = conds[idx]
	} else {
		app.conditionDraft = AutomationCondition{ID: newAutomationID("cond"), Type: condCPU, Logic: logicAND, Compare: -1, Threshold: 10, HoldSeconds: 30, Enabled: true}
	}
	if app.conditionDraft.Type > condProcessExit {
		app.conditionCatalogExpanded = true
		app.conditionCatalogTarget = 1
		app.conditionCatalogAnim = 1
	}
	pSetWindowTextW.Call(app.edits[idCondThreshold], uintptr(unsafe.Pointer(wstr(fmt.Sprintf("%.0f", app.conditionDraft.Threshold)))))
	pSetWindowTextW.Call(app.edits[idCondHold], uintptr(unsafe.Pointer(wstr(strconv.Itoa(max(app.conditionDraft.HoldSeconds, 0))))))
	pSetWindowTextW.Call(app.edits[idCondText], uintptr(unsafe.Pointer(wstr(app.conditionDraft.Text))))
	pSetWindowTextW.Call(app.edits[idCondDelay], uintptr(unsafe.Pointer(wstr(strconv.Itoa(max(app.conditionDraft.DelayAfter, 0))))))
	app.section = 8
	playUI(openSound)
	startPageAnimation()
	updateInputVisibility()
	invalidate(app.hwnd)
}
func saveConditionDraft() {
	if v, err := strconv.ParseFloat(strings.ReplaceAll(strings.TrimSpace(getText(app.edits[idCondThreshold])), ",", "."), 64); err == nil {
		app.conditionDraft.Threshold = v
	}
	app.conditionDraft.HoldSeconds = parseInt(getText(app.edits[idCondHold]), 0)
	app.conditionDraft.Text = strings.TrimSpace(getText(app.edits[idCondText]))
	app.conditionDraft.DelayAfter = clampInt(parseInt(getText(app.edits[idCondDelay]), app.conditionDraft.DelayAfter), 0, 3600)
	app.conditionDraft.Enabled = true
	app.conditionDraft.OpenGroups = 0
	app.conditionDraft.CloseGroups = 0
	if app.conditionDraft.ID == "" {
		app.conditionDraft.ID = newAutomationID("cond")
	}
	list := append([]AutomationCondition(nil), currentScenarioConditions()...)
	if app.editingCondition >= 0 && app.editingCondition < len(list) {
		list[app.editingCondition] = app.conditionDraft
	} else if len(list) < 12 {
		list = append(list, app.conditionDraft)
	}
	setCurrentScenarioConditions(list)
	resetConditionRuntimes()
	if app.scenarioSavedDraft {
		app.section = 13
	} else {
		app.section = 7
	}
	app.editingCondition = -1
	playUI(successSound)
	startPageAnimation()
	updateInputVisibility()
	invalidate(app.hwnd)
}
func openStepEditor(idx int) {
	app.currentTaskKind = 1
	app.currentTaskSection = 7
	app.editingStep = idx
	steps := currentScenarioSteps()
	if idx >= 0 && idx < len(steps) {
		app.stepDraft = steps[idx]
		app.stepDraft.Processes = append([]string(nil), steps[idx].Processes...)
	} else {
		app.stepDraft = ActionStep{ID: newAutomationID("step"), Type: stepWait, Value: 10}
	}
	pSetWindowTextW.Call(app.edits[idStepValue], uintptr(unsafe.Pointer(wstr(strconv.Itoa(max(app.stepDraft.Value, 0))))))
	pSetWindowTextW.Call(app.edits[idStepText], uintptr(unsafe.Pointer(wstr(app.stepDraft.Text))))
	pSetWindowTextW.Call(app.edits[idStepRetries], uintptr(unsafe.Pointer(wstr(strconv.Itoa(max(app.stepDraft.Retries, 0))))))
	pSetWindowTextW.Call(app.edits[idStepDelay], uintptr(unsafe.Pointer(wstr(strconv.Itoa(max(app.stepDraft.DelayAfter, 0))))))
	app.section = 9
	playUI(openSound)
	startPageAnimation()
	updateInputVisibility()
	invalidate(app.hwnd)
}
func saveStepDraft() {
	if app.stepDraft.Type == stepWait || app.stepDraft.Type == stepSetVolume {
		app.stepDraft.Value = parseInt(getText(app.edits[idStepValue]), app.stepDraft.Value)
	}
	if app.stepDraft.Type == stepRunCommand || app.stepDraft.Type == stepNotify || app.stepDraft.Type == stepProcessPriority {
		app.stepDraft.Text = strings.TrimSpace(getText(app.edits[idStepText]))
	}
	app.stepDraft.Retries = clampInt(parseInt(getText(app.edits[idStepRetries]), app.stepDraft.Retries), 0, 10)
	app.stepDraft.DelayAfter = clampInt(parseInt(getText(app.edits[idStepDelay]), app.stepDraft.DelayAfter), 0, 3600)
	if app.stepDraft.ID == "" {
		app.stepDraft.ID = newAutomationID("step")
	}
	list := cloneActionSteps(currentScenarioSteps())
	if app.editingStep >= 0 && app.editingStep < len(list) {
		list[app.editingStep] = app.stepDraft
	} else if len(list) < 12 {
		list = append(list, app.stepDraft)
	}
	setCurrentScenarioSteps(list)
	if app.scenarioSavedDraft {
		app.section = 13
	} else {
		app.section = 7
	}
	app.editingStep = -1
	playUI(successSound)
	startPageAnimation()
	updateInputVisibility()
	invalidate(app.hwnd)
}
func moveCondition(idx, delta int) {
	j := idx + delta
	if idx < 0 || j < 0 || idx >= len(app.settings.AdvancedConditions) || j >= len(app.settings.AdvancedConditions) {
		return
	}
	a := app.settings.AdvancedConditions
	a[idx], a[j] = a[j], a[idx]
	if j == 0 {
		a[j].Logic = logicAND
	}
	saveSettings()
	invalidate(app.hwnd)
}
func moveStep(idx, delta int) {
	j := idx + delta
	if idx < 0 || j < 0 || idx >= len(app.settings.ActionSteps) || j >= len(app.settings.ActionSteps) {
		return
	}
	a := app.settings.ActionSteps
	a[idx], a[j] = a[j], a[idx]
	saveSettings()
	invalidate(app.hwnd)
}
func removeCondition(idx int) {
	list := append([]AutomationCondition(nil), currentScenarioConditions()...)
	if idx < 0 || idx >= len(list) {
		return
	}
	removed := list[idx]
	if removed.Type == condGroup {
		for i := range list {
			if list[i].GroupID == removed.ID {
				list[i].GroupID = removed.GroupID
			}
		}
	}
	list = append(list[:idx], list[idx+1:]...)
	if len(list) > 0 {
		list[0].Logic = logicAND
	}
	setCurrentScenarioConditions(list)
	resetConditionRuntimes()
	invalidate(app.hwnd)
}
func removeStep(idx int) {
	list := cloneActionSteps(currentScenarioSteps())
	if idx < 0 || idx >= len(list) {
		return
	}
	list = append(list[:idx], list[idx+1:]...)
	setCurrentScenarioSteps(list)
	invalidate(app.hwnd)
}

func savedScenarioHasChanges() bool {
	if app.editingSavedIdx < 0 || app.editingSavedIdx >= len(app.settings.SavedTasks) {
		return false
	}
	draft := app.savedEditDraft
	draft.Name = strings.TrimSpace(getText(app.edits[idTaskName]))
	return !reflect.DeepEqual(draft, app.settings.SavedTasks[app.editingSavedIdx])
}

func closeSavedScenarioEditor() {
	app.confirmDiscardScenario = false
	app.editingSavedIdx = -1
	app.scenarioSavedDraft = false
	app.section = 5
	restoreCurrentInputTexts()
	playUI(openSound)
	startPageAnimation()
	updateInputVisibility()
	invalidate(app.hwnd)
}

func onClick(x, y int32) {
	// Custom caption controls.
	if pointIn(app.closeBtnRect, x, y) {
		pSendMessageW.Call(app.hwnd, WM_CLOSE, 0, 0)
		return
	}
	if pointIn(app.minBtnRect, x, y) {
		minimizeMainWindowAnimated()
		return
	}
	if pointIn(app.maxBtnRect, x, y) {
		if app.miniMode {
			setMiniMode(false)
			return
		}
		if z, _, _ := pIsZoomed.Call(app.hwnd); z != 0 {
			showMainWindowStateAnimated(SW_RESTORE)
		} else {
			showMainWindowStateAnimated(SW_MAXIMIZE)
		}
		return
	}
	if pointIn(app.miniBtnRect, x, y) {
		if app.miniMode {
			app.settings.AlwaysOnTopMini = !app.settings.AlwaysOnTopMini
			saveSettings()
			applyMiniTopmost040()
			playUI(clickSound)
			invalidate(app.hwnd)
			return
		}
		setMiniMode(true)
		return
	}
	if app.miniMode {
		if pointIn(app.miniCancelRect, x, y) {
			playUI(clickSound)
			cancelSchedule(true)
		} else if pointIn(app.miniPostponeRect, x, y) {
			playUI(clickSound)
			postpone10()
		}
		return
	}

	if handleNotificationCenterClick(x, y) {
		return
	}

	if app.confirmSystemMode != 0 {
		if pointIn(app.confirmSystemYesRect, x, y) {
			if app.confirmSystemMode == 1 && !app.confirmSystemUnlockAt.IsZero() && time.Now().Before(app.confirmSystemUnlockAt) {
				return
			}
			mode := app.confirmSystemMode
			name := app.pendingSystemProcess
			app.confirmSystemMode = 0
			app.confirmSystemUnlockAt = time.Time{}
			app.pendingSystemProcess = ""
			if mode == 1 {
				app.settings.ShowSystemProcesses = true
				if name == "__SYSTEM_TAB__" {
					app.processFilter = 1
				}
				saveSettings()
				if app.section == 4 {
					refreshProcessFilter()
				}
			} else if mode == 2 && name != "" {
				toggleSelectedProcess(name)
				if processSelectedInCurrentPicker(name) {
					rememberConfirmedSystemProcess(name)
				} else {
					forgetConfirmedSystemProcess(name)
				}
				saveSettings()
			}
			playUI(successSound)
			updateInputVisibility()
			invalidate(app.hwnd)
			return
		}
		if pointIn(app.confirmSystemNoRect, x, y) || !pointIn(app.confirmSystemOverlayRect, x, y) {
			app.confirmSystemMode = 0
			app.confirmSystemUnlockAt = time.Time{}
			app.pendingSystemProcess = ""
			playUI(clickSound)
			updateInputVisibility()
			invalidate(app.hwnd)
		}
		return
	}

	if app.confirmClearHistory {
		if pointIn(app.confirmClearYesRect, x, y) {
			clearHistory()
			app.confirmClearHistory = false
			playUI(successSound)
			updateInputVisibility()
			invalidate(app.hwnd)
			return
		}
		if pointIn(app.confirmClearNoRect, x, y) || !pointIn(app.confirmClearOverlayRect, x, y) {
			app.confirmClearHistory = false
			playUI(clickSound)
			updateInputVisibility()
			invalidate(app.hwnd)
		}
		return
	}

	if beginScrollbarInteraction(x, y) {
		return
	}

	// App-level navigation. The main Task button always returns to the task that is
	// actually being edited now (simple or advanced), never to a hard-coded simple page.
	if pointIn(app.taskTabRect, x, y) {
		if app.currentTaskKind == 1 && app.section == 7 {
			return
		}
		if app.currentTaskKind == 0 && (app.section == 0 || app.section == 1 || app.section == 2) {
			return
		}
		app.resourceMenuOpen = false
		resumeCurrentTask()
		return
	}
	if pointIn(app.taskMoreRect, x, y) {
		app.taskMenuOpen = !app.taskMenuOpen
		app.resourceMenuOpen = false
		if !app.taskMenuOpen {
			app.createTaskMenuOpen = false
		}
		app.hoverKey, app.hoverAnim, app.hoverRect = 0, 0, RECT{}
		playUI(clickSound)
		layoutControls(app.hwnd)
		invalidate(app.hwnd)
		return
	}
	if pointIn(app.monitorTabRect, x, y) {
		if app.section == 18 {
			return
		}
		rememberCurrentTaskLocation()
		if app.section == 10 {
			restoreCurrentInputTexts()
			app.editingSavedIdx = -1
		} else if isTaskSection(app.section) {
			syncFields()
		}
		app.taskMenuOpen, app.createTaskMenuOpen, app.resourceMenuOpen = false, false, false
		app.section = 18
		playUI(openSound)
		startPageAnimation()
		updateInputVisibility()
		layoutControls(app.hwnd)
		invalidate(app.hwnd)
		return
	}
	if pointIn(app.resourceMoreRect, x, y) {
		app.resourceMenuOpen = !app.resourceMenuOpen
		app.taskMenuOpen, app.createTaskMenuOpen = false, false
		app.hoverKey, app.hoverAnim, app.hoverRect = 0, 0, RECT{}
		playUI(clickSound)
		layoutControls(app.hwnd)
		invalidate(app.hwnd)
		return
	}
	if app.resourceMenuOpen && pointIn(app.resourceAdvancedMenuRect, x, y) {
		rememberCurrentTaskLocation()
		app.section = 19
		app.resourceMenuOpen = false
		playUI(openSound)
		startPageAnimation()
		updateInputVisibility()
		layoutControls(app.hwnd)
		invalidate(app.hwnd)
		return
	}
	if app.resourceMenuOpen && pointIn(app.resourceStatsMenuRect, x, y) {
		rememberCurrentTaskLocation()
		app.section = 20
		app.resourceMenuOpen = false
		playUI(openSound)
		startPageAnimation()
		updateInputVisibility()
		layoutControls(app.hwnd)
		invalidate(app.hwnd)
		return
	}
	if app.taskMenuOpen && pointIn(app.blockTaskTabRect, x, y) {
		app.createTaskMenuOpen = !app.createTaskMenuOpen
		app.hoverKey, app.hoverAnim, app.hoverRect = 0, 0, RECT{}
		playUI(clickSound)
		layoutControls(app.hwnd)
		invalidate(app.hwnd)
		return
	}
	if app.taskMenuOpen && pointIn(app.savedTabRect, x, y) {
		rememberCurrentTaskLocation()
		if app.section == 10 {
			restoreCurrentInputTexts()
			app.editingSavedIdx = -1
		} else if isTaskSection(app.section) {
			syncFields()
		}
		app.section = 5
		app.savedScroll, app.savedScrollPx, app.savedScrollTarget = 0, 0, 0
		app.confirmDeleteIdx = -1
		app.taskMenuOpen, app.createTaskMenuOpen = false, false
		playUI(openSound)
		startPageAnimation()
		updateInputVisibility()
		invalidate(app.hwnd)
		return
	}
	if app.taskMenuOpen && app.createTaskMenuOpen {
		if pointIn(app.taskKindRects[0], x, y) {
			if app.section == 10 {
				restoreCurrentInputTexts()
				app.editingSavedIdx = -1
			} else if isTaskSection(app.section) {
				syncFields()
			}
			app.settings.TaskKind = 0
			app.currentTaskKind = 0
			app.currentTaskSection = 0
			app.scenarioSavedDraft = false
			app.section = 0
			app.lastTaskSection = 0
			app.taskMenuOpen, app.createTaskMenuOpen = false, false
			saveSettings()
			playUI(openSound)
			startPageAnimation()
			updateInputVisibility()
			invalidate(app.hwnd)
			return
		}
		if pointIn(app.taskKindRects[1], x, y) {
			if app.section == 10 {
				restoreCurrentInputTexts()
				app.editingSavedIdx = -1
			} else if isTaskSection(app.section) {
				syncFields()
			}
			app.settings.TaskKind = 1
			app.currentTaskKind = 1
			app.currentTaskSection = 7
			app.scenarioSavedDraft = false
			app.section = 7
			app.lastTaskSection = 2
			app.taskMenuOpen, app.createTaskMenuOpen = false, false
			saveSettings()
			playUI(openSound)
			startPageAnimation()
			updateInputVisibility()
			invalidate(app.hwnd)
			return
		}
		if pointIn(app.taskKindRects[2], x, y) {
			rememberCurrentTaskLocation()
			if app.section == 10 {
				restoreCurrentInputTexts()
				app.editingSavedIdx = -1
			} else if isTaskSection(app.section) {
				syncFields()
			}
			app.scenarioSavedDraft = false
			app.section = 16
			app.taskMenuOpen, app.createTaskMenuOpen = false, false
			playUI(openSound)
			startPageAnimation()
			updateInputVisibility()
			invalidate(app.hwnd)
			return
		}
	}
	if app.taskMenuOpen {
		if pointInTaskMenuPopup(x, y) {
			return
		}
		app.taskMenuOpen, app.createTaskMenuOpen = false, false
		app.hoverKey, app.hoverAnim, app.hoverRect = 0, 0, RECT{}
		layoutControls(app.hwnd)
		invalidate(app.hwnd)
	}
	if app.resourceMenuOpen {
		if pointIn(resourceMenuPanelRect(), x, y) {
			return
		}
		app.resourceMenuOpen = false
		app.hoverKey, app.hoverAnim, app.hoverRect = 0, 0, RECT{}
		layoutControls(app.hwnd)
		invalidate(app.hwnd)
	}
	if pointIn(app.settingsBtnRect, x, y) {
		if app.section == 3 {
			return
		}
		rememberCurrentTaskLocation()
		if app.section == 10 {
			restoreCurrentInputTexts()
			app.editingSavedIdx = -1
		} else {
			syncFields()
		}
		app.section = 3
		playUI(openSound)
		startPageAnimation()
		updateInputVisibility()
		invalidate(app.hwnd)
		return
	}
	if isTaskSection(app.section) {
		for i, r := range app.chainRects {
			if pointIn(r, x, y) {
				if app.section == i {
					return
				}
				app.section = i
				app.lastTaskSection = i
				app.currentTaskKind = 0
				app.currentTaskSection = i
				playUI(openSound)
				if i == 2 {
					syncFields()
				}
				startPageAnimation()
				updateInputVisibility()
				invalidate(app.hwnd)
				return
			}
		}
	}

	if app.section == 0 {
		for i, r := range app.actionRects {
			if pointIn(r, x, y) {
				app.selectedAction = i
				app.settings.Action = i
				saveSettings()
				playUI(clickSound)
				invalidate(app.hwnd)
				return
			}
		}
	}
	if app.section == 1 {
		for i, r := range app.modeRects {
			if pointIn(r, x, y) {
				if app.selectedMode == i {
					return
				}
				app.selectedMode = i
				app.settings.Mode = i
				saveSettings()
				playUI(clickSound)
				startSubReveal()
				updateInputVisibility()
				invalidate(app.hwnd)
				return
			}
		}
		if app.selectedMode == 3 && pointIn(app.pickRect, x, y) {
			if app.scenarioSavedDraft {
				openProcessPicker(5)
			} else {
				openProcessPicker(2)
			}
			return
		}
		if app.selectedMode == 3 && pointIn(app.processClearRect, x, y) {
			app.settings.WatchProcess = ""
			setEditTextIfDifferent(idWatchProcess, "")
			saveSettings()
			playUI(clickSound)
			invalidate(app.hwnd)
			return
		}
		if app.selectedMode == 4 {
			for i, r := range app.recurrenceKindRects {
				if pointIn(r, x, y) {
					if app.scenarioSavedDraft {
						app.savedEditDraft.Recurrence.Kind = i
					} else {
						app.settings.Recurrence.Kind = i
						saveSettings()
					}
					playUI(clickSound)
					invalidate(app.hwnd)
					return
				}
			}
			recKind := app.settings.Recurrence.Kind
			if app.scenarioSavedDraft {
				recKind = app.savedEditDraft.Recurrence.Kind
			}
			if recKind == 2 {
				for i, r := range app.recurrenceDayRects {
					if pointIn(r, x, y) {
						if app.scenarioSavedDraft {
							app.savedEditDraft.Recurrence.Days[i] = !app.savedEditDraft.Recurrence.Days[i]
						} else {
							app.settings.Recurrence.Days[i] = !app.settings.Recurrence.Days[i]
							saveSettings()
						}
						playUI(clickSound)
						invalidate(app.hwnd)
						return
					}
				}
			}
			if pointIn(app.recurrenceEnabledRect, x, y) {
				app.settings.Recurrence.Enabled = !app.settings.Recurrence.Enabled
				saveSettings()
				playUI(clickSound)
				invalidate(app.hwnd)
				return
			}
		}
	}
	if app.section == 2 {
		if pointIn(app.closeBeforeRect, x, y) {
			app.settings.CloseBefore = !app.settings.CloseBefore
			saveSettings()
			playUI(clickSound)
			invalidate(app.hwnd)
			return
		}
		if pointIn(app.pickRect, x, y) {
			openProcessPicker(0)
			return
		}
	}

	if app.section == 18 {
		for i, r := range app.resourceCardRects {
			if pointIn(r, x, y) {
				if app.resourceSelected != i {
					app.resourceSelected = i
					playUI(clickSound)
					invalidate(app.hwnd)
				}
				return
			}
		}
		if app.resourceSelected == 3 {
			for i, r := range app.resourceDiskRects {
				if pointIn(r, x, y) {
					app.resourceDiskSelected = i
					playUI(clickSound)
					invalidate(app.hwnd)
					return
				}
			}
		}
		intervals := []int{250, 500, 1000, 2000, 5000}
		for i, r := range app.resourceRefreshRects {
			if pointIn(r, x, y) {
				app.settings.ResourceRefreshMS = intervals[i]
				setMetricRefreshInterval(intervals[i])
				saveSettings()
				playUI(clickSound)
				invalidate(app.hwnd)
				return
			}
		}
	}
	if app.section == 19 {
		for i, r := range app.resourceAdvancedTabRects {
			if pointIn(r, x, y) {
				if app.resourceAdvancedView != i {
					app.resourceAdvancedView = i
					app.resourceProcScrollPx, app.resourceProcScrollTarget = 0, 0
					layoutControls(app.hwnd)
					playUI(clickSound)
					invalidate(app.hwnd)
				}
				return
			}
		}
		if app.resourceAdvancedView == 1 {
			for i := 0; i < len(resourceSensorCategories); i++ {
				r := app.resourceSensorTypeRects[i]
				if pointIn(r, x, y) {
					if app.resourceSensorView != i {
						app.resourceSensorView = i
						app.resourceProcScrollPx, app.resourceProcScrollTarget = 0, 0
						layoutControls(app.hwnd)
						playUI(clickSound)
						invalidate(app.hwnd)
					}
					return
				}
			}

			rows := hardwareSensorDisplayRows()
			first := int(app.resourceProcScrollPx) / 43
			for slot, r := range app.resourceProcRows {
				idx := first + slot
				if idx < 0 || idx >= len(rows) || !pointIn(r, x, y) {
					continue
				}
				row := rows[idx]
				if row.IsGroup {
					if app.resourceSensorExpanded == nil {
						app.resourceSensorExpanded = map[string]bool{}
					}
					app.resourceSensorExpanded[row.GroupKey] = !app.resourceSensorExpanded[row.GroupKey]
					maxPx := resourceProcessScrollMax()
					app.resourceProcScrollPx = clampFloat(app.resourceProcScrollPx, 0, maxPx)
					app.resourceProcScrollTarget = clampFloat(app.resourceProcScrollTarget, 0, maxPx)
					layoutControls(app.hwnd)
					playUI(clickSound)
					invalidate(app.hwnd)
				}
				return
			}
		}
		if app.resourceAdvancedView == 0 {
			for i, r := range app.resourceProcSortRects {
				if pointIn(r, x, y) {
					if app.resourceProcSort == i {
						app.resourceProcSortDesc = !app.resourceProcSortDesc
					} else {
						app.resourceProcSort = i
						app.resourceProcSortDesc = true
					}
					app.resourceProcScrollPx, app.resourceProcScrollTarget = 0, 0
					layoutControls(app.hwnd)
					playUI(clickSound)
					invalidate(app.hwnd)
					return
				}
			}
		}
	}
	if app.section == 20 {
		for i, r := range app.resourceStatsPeriodRects {
			if pointIn(r, x, y) {
				if app.resourceStatsPeriod != i {
					app.resourceStatsPeriod = i
					playUI(clickSound)
					invalidate(app.hwnd)
				}
				return
			}
		}
		for i, r := range app.resourceStatsViewRects {
			if pointIn(r, x, y) {
				if app.resourceStatsView != i {
					app.resourceStatsView = i
					app.resourceStatsSort = 0
					app.resourceStatsSortDesc = false
					if i < 3 && app.resourceStatsPeriod > 4 {
						app.resourceStatsPeriod = 4
					}
					layoutControls(app.hwnd)
					playUI(clickSound)
					invalidate(app.hwnd)
				}
				return
			}
		}
		for i, r := range app.resourceStatsGraphRects {
			if pointIn(r, x, y) {
				if app.resourceStatsGraphMode != i {
					app.resourceStatsGraphMode = i
					playUI(clickSound)
					invalidate(app.hwnd)
				}
				return
			}
		}
		if app.resourceStatsView >= 2 {
			for i, r := range app.resourceStatsSortRects {
				if pointIn(r, x, y) {
					if app.resourceStatsSort == i {
						app.resourceStatsSortDesc = !app.resourceStatsSortDesc
					} else {
						app.resourceStatsSort = i
						app.resourceStatsSortDesc = i != 0
					}
					playUI(clickSound)
					invalidate(app.hwnd)
					return
				}
			}
		}
	}

	if app.section == 3 {
		for i, r := range app.settingsTabs {
			if pointIn(r, x, y) {
				if app.settingsCategory == i {
					return
				}
				syncFields()
				app.settingsCategory = i
				pages := []int{0, 1, 6, 5, 4, 4, 2}
				app.settingsSubpage = pages[i]
				app.settingsScrollPx, app.settingsScrollTarget = 0, 0
				app.confirmClearHistory = false
				if app.settingsSubpage != 2 {
					app.historyDetailOpen = false
					app.historySelected = -1
				}
				if app.settingsSubpage == 2 || app.settingsSubpage == 3 {
					loadHistoryItems()
				}
				playUI(openSound)
				startSubReveal()
				layoutControls(app.hwnd)
				invalidate(app.hwnd)
				return
			}
		}
		if app.settingsCategory == 1 {
			pages := []int{1, 7}
			for i, r := range app.settingsSectionRects {
				if pointIn(r, x, y) && app.settingsSubpage != pages[i] {
					app.settingsSubpage = pages[i]
					app.settingsScrollPx, app.settingsScrollTarget = 0, 0
					playUI(openSound)
					startSubReveal()
					layoutControls(app.hwnd)
					invalidate(app.hwnd)
					return
				}
			}
		} else if app.settingsCategory == 5 {
			pages := []int{4, 3}
			for i, r := range app.settingsSectionRects {
				if pointIn(r, x, y) && app.settingsSubpage != pages[i] {
					app.settingsSubpage = pages[i]
					app.settingsScrollPx, app.settingsScrollTarget = 0, 0
					if pages[i] == 3 {
						loadHistoryItems()
					}
					playUI(openSound)
					startSubReveal()
					layoutControls(app.hwnd)
					invalidate(app.hwnd)
					return
				}
			}
		}
		if app.settingsSubpage != 2 && (y < app.settingsScrollTrack.Top || y > app.settingsScrollTrack.Bottom) {
			return
		}
		switch app.settingsSubpage {
		case 0:
			if pointIn(app.lockMinimumRect, x, y) {
				app.settings.LockMinimumSize = !app.settings.LockMinimumSize
				if app.settings.LockMinimumSize {
					app.settings.LockCurrentSize = false
					app.settings.LockedWindowW = normalMinClientW
					app.settings.LockedWindowH = normalMinClientH
					resizeWindowClient(normalMinClientW, normalMinClientH)
				}
				saveSettings()
				playUI(clickSound)
				invalidate(app.hwnd)
				return
			}
			if pointIn(app.lockCurrentRect, x, y) {
				app.settings.LockCurrentSize = !app.settings.LockCurrentSize
				if app.settings.LockCurrentSize {
					app.settings.LockMinimumSize = false
					var cr RECT
					pGetClientRect.Call(app.hwnd, uintptr(unsafe.Pointer(&cr)))
					app.settings.LockedWindowW = max(normalMinClientW, int(cr.Right-cr.Left))
					app.settings.LockedWindowH = max(normalMinClientH, int(cr.Bottom-cr.Top))
				}
				saveSettings()
				playUI(clickSound)
				invalidate(app.hwnd)
				return
			}
			volumeHit := RECT{app.volumeTrackRect.Left, app.volumeTrackRect.Top - 10, app.volumeTrackRect.Right, app.volumeTrackRect.Bottom + 10}
			if pointIn(volumeHit, x, y) || pointIn(app.volumeKnobRect, x, y) {
				app.draggingVolume = true
				pSetCapture.Call(app.hwnd)
				setVolumeFromX(x, true)
				return
			}
			if pointIn(app.hotkeysRect, x, y) {
				app.settings.GlobalHotkeys = !app.settings.GlobalHotkeys
				applyGlobalHotkeys()
				saveSettings()
				playUI(clickSound)
				invalidate(app.hwnd)
				return
			}
			if pointIn(app.hideZeroResourceProcessesRect, x, y) {
				app.settings.HideZeroResourceProcesses = !app.settings.HideZeroResourceProcesses
				saveSettings()
				playUI(clickSound)
				layoutControls(app.hwnd)
				invalidate(app.hwnd)
				return
			}
			if pointIn(app.autoRect, x, y) {
				app.settings.AutoStart = !app.settings.AutoStart
				setAutoStart(app.settings.AutoStart)
				saveSettings()
				playUI(clickSound)
				invalidate(app.hwnd)
				return
			}
			if pointIn(app.trayRect, x, y) {
				app.settings.MinimizeToTray = !app.settings.MinimizeToTray
				saveSettings()
				playUI(clickSound)
				invalidate(app.hwnd)
				return
			}
			if pointIn(app.soundsRect, x, y) {
				app.settings.Sounds = !app.settings.Sounds
				saveSettings()
				playUI(clickSound)
				invalidate(app.hwnd)
				return
			}
			if pointIn(app.notificationsRect, x, y) {
				app.settings.Notifications = !app.settings.Notifications
				saveSettings()
				playUI(clickSound)
				if app.settings.Notifications {
					showNotification("PowerPilot", "Уведомления включены.")
				}
				invalidate(app.hwnd)
				return
			}
			if pointIn(app.wakeScheduledRect, x, y) {
				app.settings.WakeScheduledTasks = !app.settings.WakeScheduledTasks
				syncFields()
				saveSettings()
				maintainWakeTimer(time.Now())
				playUI(clickSound)
				invalidate(app.hwnd)
				return
			}
		case 6:
			volumeHit := RECT{app.volumeTrackRect.Left, app.volumeTrackRect.Top - 10, app.volumeTrackRect.Right, app.volumeTrackRect.Bottom + 10}
			if pointIn(volumeHit, x, y) || pointIn(app.volumeKnobRect, x, y) {
				app.draggingVolume = true
				pSetCapture.Call(app.hwnd)
				setVolumeFromX(x, true)
				return
			}
			if pointIn(app.soundsRect, x, y) {
				app.settings.Sounds = !app.settings.Sounds
				saveSettings()
				playUI(clickSound)
				invalidate(app.hwnd)
				return
			}
		case 7:
			for i, r := range app.graphWindowSizeRects {
				if pointIn(r, x, y) {
					app.settings.GraphWindowSize = i
					widths, heights := []int{1280, 1440, 1600}, []int{820, 900, 960}
					app.settings.GraphWindowWidth, app.settings.GraphWindowHeight = widths[i], heights[i]
					setEditTextIfDifferent(idGraphWidth, strconv.Itoa(widths[i]))
					setEditTextIfDifferent(idGraphHeight, strconv.Itoa(heights[i]))
					saveSettings()
					playUI(clickSound)
					invalidate(app.hwnd)
					return
				}
			}
			if pointIn(app.graphWindowLockRect, x, y) {
				applyGraphWindowSizeFields()
				app.settings.GraphWindowSizeLocked = !app.settings.GraphWindowSizeLocked
				saveSettings()
				if app.graphWindow != 0 && app.settings.GraphWindowSizeLocked {
					resizeScenarioGraphWindow(app.settings.GraphWindowWidth, app.settings.GraphWindowHeight)
				}
				playUI(clickSound)
				invalidate(app.hwnd)
				return
			}
			for i, r := range app.resourceTimelineModeRects {
				if pointIn(r, x, y) {
					app.settings.ResourceTimelineMode = i
					saveSettings()
					playUI(clickSound)
					invalidate(app.hwnd)
					return
				}
			}
			timelineHit := RECT{app.resourceTimelineTicksTrackRect.Left, app.resourceTimelineTicksTrackRect.Top - 10, app.resourceTimelineTicksTrackRect.Right, app.resourceTimelineTicksTrackRect.Bottom + 10}
			if pointIn(timelineHit, x, y) || pointIn(app.resourceTimelineTicksKnobRect, x, y) {
				app.draggingTimelineTicks = true
				pSetCapture.Call(app.hwnd)
				setTimelineTicksFromX(x)
				return
			}
			if pointIn(app.miniAlwaysTopRect, x, y) {
				app.settings.AlwaysOnTopMini = !app.settings.AlwaysOnTopMini
				saveSettings()
				applyMiniTopmost040()
				playUI(clickSound)
				invalidate(app.hwnd)
				return
			}
			for i, r := range app.miniOptionRects {
				if !pointIn(r, x, y) {
					continue
				}
				switch i {
				case 0:
					app.settings.MiniShowTask = !app.settings.MiniShowTask
				case 1:
					app.settings.MiniShowCountdown = !app.settings.MiniShowCountdown
				case 2:
					app.settings.MiniShowStep = !app.settings.MiniShowStep
				case 3:
					app.settings.MiniShowMetrics = !app.settings.MiniShowMetrics
				}
				saveSettings()
				playUI(clickSound)
				invalidate(app.hwnd)
				return
			}
			sizeValues := []int{90, 100, 120}
			for i, r := range app.miniSizeRects {
				if pointIn(r, x, y) {
					app.settings.MiniSize = sizeValues[i]
					saveSettings()
					if app.miniMode {
						mw, mh := miniClientSize040()
						var wr RECT
						pGetWindowRect.Call(app.hwnd, uintptr(unsafe.Pointer(&wr)))
						pSetWindowPos.Call(app.hwnd, 0, uintptr(wr.Left), uintptr(wr.Top), uintptr(mw), uintptr(mh), SWP_NOZORDER|SWP_NOACTIVATE)
						layoutControls(app.hwnd)
					}
					playUI(clickSound)
					invalidate(app.hwnd)
					return
				}
			}
			scales := []int{90, 100, 110, 125}
			for i, r := range app.uiScaleRects {
				if pointIn(r, x, y) {
					applyUIScaleChange040(scales[i])
					playUI(clickSound)
					return
				}
			}
		case 1:
			for i, r := range app.themeRects {
				if pointIn(r, x, y) {
					app.settings.ThemeMode = i
					applyTheme()
					app.lastAutoThemeLight = systemUsesLightTheme()
					saveSettings()
					playUI(clickSound)
					invalidate(app.hwnd)
					return
				}
			}
			for i, r := range app.backgroundRects {
				if pointIn(r, x, y) {
					app.settings.Background = i
					saveSettings()
					playUI(clickSound)
					invalidate(app.hwnd)
					return
				}
			}
			for i, r := range app.surfaceRects {
				if pointIn(r, x, y) {
					app.settings.SurfaceStyle = i
					refreshControlBrush()
					saveSettings()
					playUI(clickSound)
					invalidate(app.hwnd)
					return
				}
			}
			for i, r := range app.animationRects {
				if pointIn(r, x, y) {
					app.settings.AnimationMode = i
					saveSettings()
					playUI(clickSound)
					invalidate(app.hwnd)
					return
				}
			}
		case 2:
			if app.historyDetailOpen {
				if pointIn(app.historyDetailBackRect, x, y) {
					app.historyDetailOpen = false
					app.historySelected = -1
					app.historyDetailScrollPx, app.historyDetailScrollTarget = 0, 0
					playUI(openSound)
					startSubReveal()
					updateInputVisibility()
					invalidate(app.hwnd)
				}
				return
			}
			for i, r := range app.historyFilterRects {
				if pointIn(r, x, y) {
					app.historyFilter = i
					invalidateHistoryFilterCache()
					app.historyScroll = 0
					app.historyScrollPx = 0
					app.historyScrollTarget = 0
					playUI(clickSound)
					invalidate(app.hwnd)
					return
				}
			}
			items := filteredHistoryItems()
			if pointIn(app.historyScrollTrack, x, y) || pointIn(app.historyScrollThumb, x, y) {
				maxScroll := max(0, len(items)-app.historyVisible)
				trackH := max(1, int(app.historyScrollTrack.Bottom-app.historyScrollTrack.Top))
				pos := clampInt(int(y-app.historyScrollTrack.Top), 0, trackH)
				app.historyScroll = clampInt(pos*maxScroll/trackH, 0, maxScroll)
				layoutControls(app.hwnd)
				invalidate(app.hwnd)
				return
			}
			if pointIn(app.historyPrevRect, x, y) {
				app.historyScroll -= app.historyVisible
				if app.historyScroll < 0 {
					app.historyScroll = 0
					app.historyScrollPx = 0
					app.historyScrollTarget = 0
				}
				playUI(clickSound)
				invalidate(app.hwnd)
				return
			}
			if pointIn(app.historyNextRect, x, y) {
				if app.historyScroll+app.historyVisible < len(items) {
					app.historyScroll += app.historyVisible
				}
				playUI(clickSound)
				invalidate(app.hwnd)
				return
			}
			for i, r := range app.historyRows {
				if !pointIn(app.historyListClip, x, y) || !pointIn(r, x, y) {
					continue
				}
				idx := app.historyScroll + i
				if idx >= 0 && idx < len(items) {
					app.historyDetailItem = items[idx]
					app.historySelected = idx
					app.historyDetailOpen = true
					app.historyDetailScrollPx, app.historyDetailScrollTarget = 0, 0
					playUI(openSound)
					startSubReveal()
					updateInputVisibility()
					invalidate(app.hwnd)
					return
				}
			}
			if pointIn(app.historyClearRect, x, y) {
				app.confirmClearHistory = true
				playUI(openSound)
				updateInputVisibility()
				invalidate(app.hwnd)
				return
			}
		case 3:
		case 4:
			if app.settingsCategory == 4 {
				if pointIn(app.temperatureUpdateActionRect, x, y) {
					playUI(clickSound)
					handleTemperatureProviderUpdateAction()
					return
				}
				if pointIn(app.appUpdateActionRect, x, y) {
					playUI(clickSound)
					handlePowerPilotUpdateAction()
					return
				}
				return
			}
			for i, r := range app.dataRects {
				if pointIn(r, x, y) {
					playUI(clickSound)
					switch i {
					case 0:
						exportTasks()
					case 1:
						importTasks()
					case 2:
						createBackup()
					case 3:
						restoreBackup()
					case 4:
						_ = exec.Command("notepad.exe", technicalLogPath()).Start()
					}
					return
				}
			}
		case 5:
			if pointIn(app.safetyFullscreenRect, x, y) {
				app.settings.SafetyFullscreen = !app.settings.SafetyFullscreen
				saveSettings()
				playUI(clickSound)
				invalidate(app.hwnd)
				return
			}
			if pointIn(app.safetyRecentRect, x, y) {
				app.settings.SafetyRecentInput = !app.settings.SafetyRecentInput
				syncFields()
				saveSettings()
				playUI(clickSound)
				invalidate(app.hwnd)
				return
			}
			if pointIn(app.safetyProcessesRect, x, y) {
				syncFields()
				openProcessPicker(1)
				return
			}
			if pointIn(app.showSystemProcessesRect, x, y) {
				if app.settings.ShowSystemProcesses {
					app.settings.ShowSystemProcesses = false
					app.settings.ConfirmedSystemProcesses = nil
					if app.section == 4 {
						refreshProcessFilter()
					}
					saveSettings()
					playUI(clickSound)
				} else {
					beginSystemConfirmation(1, "")
				}
				invalidate(app.hwnd)
				return
			}
		}
	}

	if app.section == 4 {
		for i, r := range app.processFilterRects {
			if pointIn(r, x, y) {
				if app.processFilter == i {
					return
				}
				if i == 1 && !app.settings.ShowSystemProcesses {
					beginSystemConfirmation(1, "__SYSTEM_TAB__")
					return
				}
				app.processFilter = i
				refreshProcessFilter()
				playUI(clickSound)
				invalidate(app.hwnd)
				return
			}
		}
		if pointIn(app.processScrollTrack, x, y) || pointIn(app.processScrollThumb, x, y) {
			maxScroll := max(0, len(app.pickerItems)-app.processVisible)
			trackH := max(1, int(app.processScrollTrack.Bottom-app.processScrollTrack.Top))
			pos := clampInt(int(y-app.processScrollTrack.Top), 0, trackH)
			app.processScroll = clampInt(pos*maxScroll/trackH, 0, maxScroll)
			layoutControls(app.hwnd)
			invalidate(app.hwnd)
			return
		}
		for i, r := range app.processRows {
			if pointIn(app.processListClip, x, y) && pointIn(r, x, y) {
				idx := app.processScroll + i
				if idx < len(app.pickerItems) {
					name := app.pickerItems[idx]
					if app.pickerSystem[strings.ToLower(name)] {
						beginSystemConfirmation(2, name)
					} else {
						toggleSelectedProcess(name)
						playUI(clickSound)
					}
					invalidate(app.hwnd)
				}
				return
			}
		}
		if pointIn(app.processPrevRect, x, y) {
			app.processScroll -= app.processVisible
			if app.processScroll < 0 {
				app.processScroll = 0
				app.processScrollPx = 0
				app.processScrollTarget = 0
			}
			playUI(clickSound)
			invalidate(app.hwnd)
			return
		}
		if pointIn(app.processNextRect, x, y) {
			if app.processScroll+app.processVisible < len(app.pickerItems) {
				app.processScroll += app.processVisible
			}
			playUI(clickSound)
			invalidate(app.hwnd)
			return
		}
		if pointIn(app.processDoneRect, x, y) {
			if app.processPickerMode != 3 && app.processPickerMode != 4 && app.processPickerMode != 5 && app.processPickerMode != 6 {
				saveSettings()
			}
			ret := app.processReturnSection
			if app.processPickerMode == 1 {
				app.section = 3
				app.settingsCategory = 3
				app.settingsSubpage = 5
			} else if app.processPickerMode == 3 {
				app.section = 8
				pSetWindowTextW.Call(app.edits[idCondText], uintptr(unsafe.Pointer(wstr(app.conditionDraft.Text))))
			} else if ret >= 0 {
				app.section = ret
			} else {
				app.section = 2
				app.lastTaskSection = 2
			}
			playUI(openSound)
			startPageAnimation()
			updateInputVisibility()
			invalidate(app.hwnd)
			return
		}
	}

	if app.section == 5 {
		if pointIn(app.savedScrollTrack, x, y) || pointIn(app.savedScrollThumb, x, y) {
			maxScroll := max(0, len(app.savedFilteredIndices)-app.savedVisible)
			trackH := max(1, int(app.savedScrollTrack.Bottom-app.savedScrollTrack.Top))
			pos := clampInt(int(y-app.savedScrollTrack.Top), 0, trackH)
			app.savedScroll = clampInt(pos*maxScroll/trackH, 0, maxScroll)
			app.savedMenuOpenIdx = -1
			app.savedMenuAnim = 0
			app.savedMenuTarget = 0
			layoutControls(app.hwnd)
			invalidate(app.hwnd)
			return
		}
		if app.confirmDeleteIdx >= 0 {
			if pointIn(app.confirmDeleteYesRect, x, y) {
				idx := app.confirmDeleteIdx
				app.confirmDeleteIdx = -1
				deleteSavedTask(idx)
				playUI(successSound)
				updateInputVisibility()
				invalidate(app.hwnd)
				return
			}
			if pointIn(app.confirmDeleteNoRect, x, y) || !pointIn(app.confirmOverlayRect, x, y) {
				app.confirmDeleteIdx = -1
				playUI(clickSound)
				updateInputVisibility()
				invalidate(app.hwnd)
			}
			return
		}
		if app.savedMenuOpenIdx >= 0 {
			if pointIn(app.savedPopupEditRect, x, y) {
				idx := app.savedMenuOpenIdx
				app.savedMenuOpenIdx = -1
				openSavedTaskEditor(idx)
				return
			}
			if pointIn(app.savedPopupDuplicateRect, x, y) {
				idx := app.savedMenuOpenIdx
				app.savedMenuOpenIdx = -1
				duplicateSavedTask(idx)
				playUI(successSound)
				layoutControls(app.hwnd)
				invalidate(app.hwnd)
				return
			}
			if pointIn(app.savedPopupDeleteRect, x, y) {
				app.confirmDeleteIdx = app.savedMenuOpenIdx
				app.savedMenuOpenIdx = -1
				playUI(openSound)
				updateInputVisibility()
				invalidate(app.hwnd)
				return
			}
			if !pointIn(app.savedPopupRect, x, y) {
				app.savedMenuTarget = 0
				app.savedMenuPendingClose = true
				invalidate(app.hwnd)
				// Continue: click may target another row action.
			}
		}
		for i, r := range app.savedRows {
			if !pointIn(app.savedListClip, x, y) {
				continue
			}
			idx := savedUnderlyingIndex(i)
			if idx < 0 || idx >= len(app.settings.SavedTasks) {
				continue
			}
			if pointIn(app.savedFavoriteRects[i], x, y) {
				toggleFavoriteTask(idx)
				playUI(clickSound)
				layoutControls(app.hwnd)
				invalidate(app.hwnd)
				return
			}
			if pointIn(app.savedRunRects[i], x, y) {
				startOrStopSavedTask(idx)
				playUI(clickSound)
				invalidate(app.hwnd)
				return
			}
			if pointIn(app.savedPauseRects[i], x, y) {
				toggleSavedTaskPaused(idx)
				playUI(clickSound)
				layoutControls(app.hwnd)
				invalidate(app.hwnd)
				return
			}
			if pointIn(app.savedMenuButtonRects[i], x, y) {
				if app.savedMenuOpenIdx == idx {
					// Repeated click always closes the existing popup; never spawn/restart it while closing.
					app.savedMenuTarget = 0
					app.savedMenuPendingClose = true
				} else {
					app.savedMenuOpenIdx = idx
					app.savedMenuAnim = 0
					app.savedMenuTarget = 1
					app.savedMenuPendingClose = false
				}
				playUI(clickSound)
				layoutControls(app.hwnd)
				invalidate(app.hwnd)
				return
			}
			if pointIn(r, x, y) {
				// Management lives in the explicit Run button and ⋮ menu.
				return
			}
		}
		if pointIn(app.savedPrevRect, x, y) {
			app.savedScroll -= app.savedVisible
			if app.savedScroll < 0 {
				app.savedScroll = 0
				app.savedScrollPx = 0
				app.savedScrollTarget = 0
			}
			app.savedMenuOpenIdx = -1
			playUI(clickSound)
			layoutControls(app.hwnd)
			invalidate(app.hwnd)
			return
		}
		if pointIn(app.savedNextRect, x, y) {
			if app.savedScroll+app.savedVisible < len(app.savedFilteredIndices) {
				app.savedScroll += app.savedVisible
			}
			app.savedMenuOpenIdx = -1
			playUI(clickSound)
			layoutControls(app.hwnd)
			invalidate(app.hwnd)
			return
		}
	}

	if app.section == 10 {
		for i, r := range app.savedEditKindRects {
			if pointIn(r, x, y) {
				if app.savedEditDraft.TaskKind == i {
					return
				}
				app.savedEditDraft.TaskKind = i
				playUI(clickSound)
				startSubReveal()
				updateInputVisibility()
				invalidate(app.hwnd)
				return
			}
		}
		for i, r := range app.savedEditActionRects {
			if pointIn(r, x, y) {
				app.savedEditDraft.Action = i
				playUI(clickSound)
				invalidate(app.hwnd)
				return
			}
		}
		for i, r := range app.savedEditModeRects {
			if pointIn(r, x, y) {
				if app.savedEditDraft.Mode == i {
					return
				}
				app.savedEditDraft.Mode = i
				playUI(clickSound)
				startSubReveal()
				updateInputVisibility()
				invalidate(app.hwnd)
				return
			}
		}
		if app.savedEditDraft.Mode == 4 {
			for i, r := range app.recurrenceKindRects {
				if pointIn(r, x, y) {
					app.savedEditDraft.Recurrence.Kind = i
					playUI(clickSound)
					invalidate(app.hwnd)
					return
				}
			}
			if app.savedEditDraft.Recurrence.Kind == 2 {
				for i, r := range app.recurrenceDayRects {
					if pointIn(r, x, y) {
						app.savedEditDraft.Recurrence.Days[i] = !app.savedEditDraft.Recurrence.Days[i]
						playUI(clickSound)
						invalidate(app.hwnd)
						return
					}
				}
			}
		}
		if pointIn(app.savedEditScenarioRect, x, y) && app.savedEditDraft.TaskKind == 1 {
			app.scenarioSavedDraft = true
			app.scenarioReturnSection = 10
			app.section = 13
			playUI(openSound)
			startPageAnimation()
			updateInputVisibility()
			invalidate(app.hwnd)
			return
		}
		if pointIn(app.savedEditCloseRect, x, y) {
			app.savedEditDraft.CloseBefore = !app.savedEditDraft.CloseBefore
			playUI(clickSound)
			invalidate(app.hwnd)
			return
		}
		if app.savedEditDraft.Mode == 3 && pointIn(app.savedEditClearRect, x, y) {
			app.savedEditDraft.WatchProcess = ""
			setEditTextIfDifferent(idWatchProcess, "")
			playUI(clickSound)
			invalidate(app.hwnd)
			return
		}
		if pointIn(app.savedEditProcessRect, x, y) {
			if app.savedEditDraft.Mode == 3 {
				openProcessPicker(5)
			} else {
				openProcessPicker(4)
			}
			return
		}
		if pointIn(app.savedEditSaveRect, x, y) {
			saveSavedTaskEditor()
			return
		}
		if pointIn(app.savedEditCancelRect, x, y) {
			app.editingSavedIdx = -1
			app.scenarioSavedDraft = false
			app.section = 5
			restoreCurrentInputTexts()
			playUI(openSound)
			startPageAnimation()
			updateInputVisibility()
			invalidate(app.hwnd)
			return
		}
	}

	if app.section == 6 {
		if pointIn(app.saveConfirmRect, x, y) {
			saveCurrentTask()
			return
		}
		if pointIn(app.saveBackRect, x, y) {
			app.section = app.lastTaskSection
			playUI(openSound)
			startPageAnimation()
			updateInputVisibility()
			invalidate(app.hwnd)
			return
		}
	}

	if app.section == 16 {
		if pointIn(app.templateBackRect, x, y) {
			resumeCurrentTask()
			app.taskMenuOpen = true
			app.createTaskMenuOpen = true
			layoutControls(app.hwnd)
			invalidate(app.hwnd)
			return
		}
		for i, r := range app.templateRects {
			if pointIn(r, x, y) {
				applyTemplate040(i)
				return
			}
		}
	}
	if app.section == 17 {
		if pointIn(app.previewBackRect, x, y) {
			if app.scenarioSavedDraft {
				app.section = 13
			} else {
				app.section = 7
			}
			playUI(openSound)
			startPageAnimation()
			updateInputVisibility()
			invalidate(app.hwnd)
			return
		}
	}

	if app.section == 14 {
		if pointIn(app.blockEditorBackRect, x, y) {
			if app.scenarioSavedDraft {
				app.section = 13
			} else {
				app.section = 7
			}
			playUI(openSound)
			startPageAnimation()
			updateInputVisibility()
			invalidate(app.hwnd)
			return
		}
		for i, r := range app.blockActionChoiceRects {
			if pointIn(r, x, y) {
				if app.scenarioSavedDraft {
					app.savedEditDraft.Action = i
				} else {
					app.selectedAction = i
					app.settings.Action = i
					saveSettings()
				}
				if n := selectedGraphNode(); n != nil && n.Kind == graphNodeFinish {
					n.Action = i
					persistCurrentScenarioGraph()
				}
				playUI(successSound)
				if app.scenarioSavedDraft {
					app.section = 13
				} else {
					app.section = 7
				}
				startPageAnimation()
				updateInputVisibility()
				invalidate(app.hwnd)
				return
			}
		}
	}
	if app.section == 15 {
		if pointIn(app.blockEditorBackRect, x, y) || pointIn(app.blockEditorDoneRect, x, y) {
			syncScenarioWhenFields()
			syncCurrentGraphFromLegacy()
			if app.scenarioSavedDraft {
				app.section = 13
			} else {
				app.section = 7
			}
			playUI(openSound)
			startPageAnimation()
			updateInputVisibility()
			invalidate(app.hwnd)
			return
		}
		for i, r := range app.modeRects {
			if pointIn(r, x, y) {
				if app.selectedMode == i {
					return
				}
				syncScenarioWhenFields()
				app.selectedMode = i
				if app.scenarioSavedDraft {
					app.savedEditDraft.Mode = i
				} else {
					app.settings.Mode = i
					saveSettings()
				}
				startSubReveal()
				updateInputVisibility()
				invalidate(app.hwnd)
				return
			}
		}
		if app.selectedMode == 3 && pointIn(app.pickRect, x, y) {
			openProcessPicker(2)
			return
		}
		if app.selectedMode == 3 && pointIn(app.processClearRect, x, y) {
			if app.scenarioSavedDraft {
				app.savedEditDraft.WatchProcess = ""
			} else {
				app.settings.WatchProcess = ""
				saveSettings()
			}
			setEditTextIfDifferent(idWatchProcess, "")
			playUI(clickSound)
			invalidate(app.hwnd)
			return
		}
		if app.selectedMode == 4 {
			for i, r := range app.recurrenceKindRects {
				if pointIn(r, x, y) {
					if app.scenarioSavedDraft {
						app.savedEditDraft.Recurrence.Kind = i
					} else {
						app.settings.Recurrence.Kind = i
						saveSettings()
					}
					invalidate(app.hwnd)
					return
				}
			}
			recKind := app.settings.Recurrence.Kind
			if app.scenarioSavedDraft {
				recKind = app.savedEditDraft.Recurrence.Kind
			}
			if recKind == 2 {
				for i, r := range app.recurrenceDayRects {
					if pointIn(r, x, y) {
						if app.scenarioSavedDraft {
							app.savedEditDraft.Recurrence.Days[i] = !app.savedEditDraft.Recurrence.Days[i]
						} else {
							app.settings.Recurrence.Days[i] = !app.settings.Recurrence.Days[i]
							saveSettings()
						}
						invalidate(app.hwnd)
						return
					}
				}
			}
		}
	}

	if app.section == 7 || app.section == 13 {
		if handleScenarioGraphClick(x, y) {
			return
		}
		if app.confirmDiscardScenario {
			if pointIn(app.confirmDiscardYesRect, x, y) {
				closeSavedScenarioEditor()
			} else if pointIn(app.confirmDiscardNoRect, x, y) || !pointIn(app.confirmDiscardRect, x, y) {
				app.confirmDiscardScenario = false
				updateInputVisibility()
				invalidate(app.hwnd)
			}
			return
		}
		if app.section == 13 && app.scenarioSavedDraft {
			if pointIn(app.savedScenarioSaveRect, x, y) {
				saveSavedTaskEditor()
				return
			}
			if pointIn(app.savedScenarioCancelRect, x, y) {
				if savedScenarioHasChanges() {
					app.confirmDiscardScenario = true
					updateInputVisibility()
					invalidate(app.hwnd)
				} else {
					closeSavedScenarioEditor()
				}
				return
			}
			if pointIn(app.savedScenarioCheckRect, x, y) {
				app.checkReturnSection = 13
				app.section = 12
				playUI(openSound)
				startPageAnimation()
				updateInputVisibility()
				invalidate(app.hwnd)
				return
			}
		}
		if pointIn(app.undoRect, x, y) {
			if !app.scenarioSavedDraft && undoAvailable040() {
				undoTask040()
			}
			return
		}
		if pointIn(app.redoRect, x, y) {
			if !app.scenarioSavedDraft && redoAvailable040() {
				redoTask040()
			}
			return
		}
		if app.graphWindow == 0 && pointIn(app.previewRect, x, y) {
			app.checkReturnSection = app.section
			app.section = 12
			playUI(openSound)
			startPageAnimation()
			updateInputVisibility()
			invalidate(app.hwnd)
			return
		}
		if pointIn(app.scenarioBackRect, x, y) {
			if app.scenarioSavedDraft {
				app.section = 10
			} else {
				app.section = 7
				return
			}
			app.lastTaskSection = 2
			playUI(openSound)
			startPageAnimation()
			updateInputVisibility()
			invalidate(app.hwnd)
			return
		}
		if pointIn(app.blockWhenRect, x, y) {
			if app.scenarioSavedDraft {
				loadScenarioWhenInputs()
			}
			app.section = 15
			playUI(openSound)
			startPageAnimation()
			updateInputVisibility()
			invalidate(app.hwnd)
			return
		}
		if pointIn(app.blockActionRect, x, y) {
			app.section = 14
			playUI(openSound)
			startPageAnimation()
			updateInputVisibility()
			invalidate(app.hwnd)
			return
		}
		if pointIn(app.blockProcessesRect, x, y) {
			if app.scenarioSavedDraft {
				openProcessPicker(4)
			} else {
				openProcessPicker(0)
			}
			return
		}
		if pointIn(app.triggerLogicRect, x, y) {
			v := currentScenarioTriggerLogic()
			if v == logicAND {
				v = logicOR
			} else {
				v = logicAND
			}
			setCurrentScenarioTriggerLogic(v)
			playUI(clickSound)
			invalidate(app.hwnd)
			return
		}
		conds := currentScenarioConditions()
		for slot, r := range app.conditionRows {
			i := app.conditionRowIndices[slot]
			if i < 0 || i >= len(conds) || r.Right <= r.Left || r.Bottom <= app.scenarioListClip.Top || r.Top >= app.scenarioListClip.Bottom {
				continue
			}
			if conds[i].Type == condGroup && pointIn(app.conditionCollapseRects[slot], x, y) {
				if app.conditionGroupCollapsed == nil {
					app.conditionGroupCollapsed = map[string]bool{}
				}
				app.conditionGroupCollapsed[conds[i].ID] = !app.conditionGroupCollapsed[conds[i].ID]
				app.scenarioScrollPx, app.scenarioScrollTarget = 0, 0
				playUI(clickSound)
				layoutControls(app.hwnd)
				invalidate(app.hwnd)
				return
			}
			if pointIn(app.conditionDragRects[slot], x, y) {
				app.selectedScenarioKind, app.selectedScenarioIndex = 1, i
				app.draggingScenarioKind, app.draggingScenarioIndex, app.draggingScenarioTarget = 1, i, i
				app.draggingScenarioParentID = conds[i].GroupID
				app.draggingScenarioIntoGroup = false
				app.draggingScenarioY = y
				app.dragGapAnim = .12
				pSetCapture.Call(app.hwnd)
				return
			}
			if i > 0 && pointIn(app.conditionLogicRects[slot], x, y) {
				list := append([]AutomationCondition(nil), conds...)
				if list[i].Logic == logicAND {
					list[i].Logic = logicOR
				} else {
					list[i].Logic = logicAND
				}
				setCurrentScenarioConditions(list)
				playUI(clickSound)
				invalidate(app.hwnd)
				return
			}
			if pointIn(app.conditionDuplicateRects[slot], x, y) {
				copyScenarioBlock040(1, i)
				playUI(clickSound)
				invalidate(app.hwnd)
				return
			}
			if pointIn(app.conditionDeleteRects[slot], x, y) {
				removeCondition(i)
				return
			}
			if pointIn(r, x, y) {
				app.selectedScenarioKind, app.selectedScenarioIndex = 1, i
				if conds[i].Type != condGroup {
					openConditionEditor(i)
				} else {
					if app.conditionGroupCollapsed == nil {
						app.conditionGroupCollapsed = map[string]bool{}
					}
					app.conditionGroupCollapsed[conds[i].ID] = !app.conditionGroupCollapsed[conds[i].ID]
					layoutControls(app.hwnd)
					invalidate(app.hwnd)
				}
				return
			}
		}
		steps := currentScenarioSteps()
		for slot, r := range app.stepRows {
			i := app.stepRowIndices[slot]
			if i < 0 || i >= len(steps) || r.Right <= r.Left || r.Bottom <= app.scenarioListClip.Top || r.Top >= app.scenarioListClip.Bottom {
				continue
			}
			if pointIn(app.stepDragRects[slot], x, y) {
				app.selectedScenarioKind, app.selectedScenarioIndex = 2, i
				app.draggingScenarioKind, app.draggingScenarioIndex, app.draggingScenarioTarget = 2, i, i
				app.draggingScenarioY = y
				app.dragGapAnim = .12
				pSetCapture.Call(app.hwnd)
				return
			}
			if pointIn(app.stepDuplicateRects[slot], x, y) {
				copyScenarioBlock040(2, i)
				playUI(clickSound)
				invalidate(app.hwnd)
				return
			}
			if pointIn(app.stepDeleteRects[slot], x, y) {
				removeStep(i)
				return
			}
			if pointIn(r, x, y) {
				app.selectedScenarioKind, app.selectedScenarioIndex = 2, i
				openStepEditor(i)
				return
			}
		}
		if pointIn(app.pasteConditionRect, x, y) {
			if !pasteScenarioBlock040(1, len(currentScenarioConditions())) {
				showNotification("PowerPilot", "Сначала скопируйте условие.")
			}
			return
		}
		if pointIn(app.copyConditionsGroupRect, x, y) {
			if !copyScenarioGroup040(1) {
				showNotification("PowerPilot", "Нет условий для копирования.")
			}
			playUI(clickSound)
			invalidate(app.hwnd)
			return
		}
		if pointIn(app.pasteStepRect, x, y) {
			if !pasteScenarioBlock040(2, len(currentScenarioSteps())) {
				showNotification("PowerPilot", "Сначала скопируйте шаг.")
			}
			return
		}
		if pointIn(app.copyStepsGroupRect, x, y) {
			if !copyScenarioGroup040(2) {
				showNotification("PowerPilot", "Нет шагов для копирования.")
			}
			playUI(clickSound)
			invalidate(app.hwnd)
			return
		}
		if pointIn(app.addConditionRect, x, y) {
			if len(currentScenarioConditions()) < 12 {
				openConditionEditor(-1)
			} else {
				showNotification("PowerPilot", "Максимум 12 условий в одной продвинутой задаче.")
			}
			return
		}
		if pointIn(app.addConditionGroupRect, x, y) {
			if len(currentScenarioConditions()) < 12 {
				list := append([]AutomationCondition(nil), currentScenarioConditions()...)
				list = append(list, AutomationCondition{ID: newAutomationID("group"), Type: condGroup, Logic: logicAND, Enabled: true})
				setCurrentScenarioConditions(list)
				playUI(successSound)
				layoutControls(app.hwnd)
				invalidate(app.hwnd)
			} else {
				showNotification("PowerPilot", "Максимум 12 условий и групп в одной задаче.")
			}
			return
		}
		if pointIn(app.addStepRect, x, y) {
			if len(currentScenarioSteps()) < 12 {
				openStepEditor(-1)
			} else {
				showNotification("PowerPilot", "Максимум 12 шагов в одной продвинутой задаче.")
			}
			return
		}
	}
	if app.section == 11 {
		if pointIn(app.diagnosticBackRect, x, y) {
			app.section = 12
			playUI(openSound)
			startPageAnimation()
			updateInputVisibility()
			invalidate(app.hwnd)
			return
		}
		if app.diagnosticMode == 1 {
			if pointIn(app.diagnosticRestartRect, x, y) {
				app.dryRunStep = 1
				playUI(clickSound)
				invalidate(app.hwnd)
				return
			}
			if pointIn(app.diagnosticNextRect, x, y) {
				if app.dryRunStep < len(app.diagnosticLines) {
					app.dryRunStep++
				}
				playUI(clickSound)
				invalidate(app.hwnd)
				return
			}
			if pointIn(app.diagnosticRunRect, x, y) {
				app.dryRunStep = len(app.diagnosticLines)
				playUI(clickSound)
				invalidate(app.hwnd)
				return
			}
		}
		if pointIn(app.diagnosticRefreshRect, x, y) {
			app.diagnosticLines = buildDiagnosticReport(app.diagnosticMode == 1)
			app.diagnosticLastRefresh = time.Now()
			if app.diagnosticMode == 1 {
				appendHistory("DRYRUN", fmt.Sprintf("%d условий · %d шагов", len(app.settings.AdvancedConditions), len(app.settings.ActionSteps)))
			}
			playUI(clickSound)
			invalidate(app.hwnd)
			return
		}
	}
	if app.section == 12 {
		if pointIn(app.checkBackRect, x, y) {
			if app.checkReturnSection > 0 {
				app.section = app.checkReturnSection
				app.checkReturnSection = 0
			} else {
				app.section = app.lastTaskSection
				if app.section < 0 || app.section > 2 {
					app.section = 2
				}
			}
			playUI(openSound)
			startPageAnimation()
			updateInputVisibility()
			invalidate(app.hwnd)
			return
		}
		if pointIn(app.checkTestRect, x, y) {
			app.diagnosticMode = 1
			app.diagnosticLines = buildDiagnosticReport(true)
			app.dryRunStep = 1
			app.diagnosticTitle = "Тестовый прогон"
			app.diagnosticLastRefresh = time.Now()
			appendHistory("DRYRUN", fmt.Sprintf("%d условий · %d шагов", len(app.settings.AdvancedConditions), len(app.settings.ActionSteps)))
			app.section = 11
			playUI(openSound)
			startPageAnimation()
			updateInputVisibility()
			invalidate(app.hwnd)
			return
		}
		if pointIn(app.checkDiagRect, x, y) {
			app.diagnosticMode = 2
			app.diagnosticLines = buildDiagnosticReport(false)
			app.diagnosticTitle = "Почему задача ждёт?"
			app.diagnosticLastRefresh = time.Now()
			app.section = 11
			playUI(openSound)
			startPageAnimation()
			updateInputVisibility()
			invalidate(app.hwnd)
			return
		}
	}
	if app.section == 8 {
		if pointIn(app.conditionMoreRect, x, y) {
			app.conditionCatalogExpanded = !app.conditionCatalogExpanded
			if app.conditionCatalogExpanded {
				app.conditionCatalogTarget = 1
			} else {
				app.conditionCatalogTarget = 0
			}
			playUI(clickSound)
			if scenarioGraphDetachedInput {
				// The detached editor owns a separate paint/layout cycle. Commit the final
				// catalogue geometry immediately instead of relying on the hidden main
				// window to advance or finish this local transition.
				app.conditionCatalogAnim = app.conditionCatalogTarget
				app.conditionCatalogFrom = app.conditionCatalogTarget
				app.conditionCatalogAnimating = false
			} else if app.settings.AnimationMode == 2 {
				app.conditionCatalogAnim = app.conditionCatalogTarget
				app.conditionCatalogAnimating = false
				layoutControls(app.hwnd)
				updateInputVisibility()
			} else {
				app.conditionCatalogFrom = app.conditionCatalogAnim
				app.conditionCatalogStarted = time.Now()
				app.conditionCatalogAnimating = true
				hideConditionEditorInputs()
			}
			invalidate(app.hwnd)
			return
		}
		for i, r := range app.editorTypeRects {
			// Extra buttons are laid out at their final coordinates and clipped while the
			// catalog opens. The still-clipped portion must not be clickable.
			if !basicConditionType(i) && r.Bottom > app.conditionMoreRect.Top {
				continue
			}
			if pointIn(r, x, y) {
				if app.conditionDraft.Type == i {
					return
				}
				app.conditionDraft.Text = strings.TrimSpace(getText(app.edits[idCondText]))
				app.conditionDraft.Type = i
				if conditionUsesThreshold(i) && app.conditionDraft.Threshold == 0 {
					app.conditionDraft.Threshold = 10
				}
				if i == condFileStable && app.conditionDraft.HoldSeconds < 5 {
					app.conditionDraft.HoldSeconds = 10
				}
				playUI(clickSound)
				startSubReveal()
				updateInputVisibility()
				invalidate(app.hwnd)
				return
			}
		}
		for i, r := range app.editorCompareRects {
			if pointIn(r, x, y) {
				if i == 0 {
					app.conditionDraft.Compare = -1
				} else {
					app.conditionDraft.Compare = 1
				}
				playUI(clickSound)
				invalidate(app.hwnd)
				return
			}
		}
		if pointIn(app.conditionOpenGroupRect, x, y) {
			app.conditionDraft.OpenGroups = (app.conditionDraft.OpenGroups + 1) % 4
			playUI(clickSound)
			invalidate(app.hwnd)
			return
		}
		if pointIn(app.conditionCloseGroupRect, x, y) {
			app.conditionDraft.CloseGroups = (app.conditionDraft.CloseGroups + 1) % 4
			playUI(clickSound)
			invalidate(app.hwnd)
			return
		}
		if pointIn(app.editorBrowseRect, x, y) && app.conditionDraft.Type == condFileStable {
			if p := chooseOpenFile("Выберите файл для отслеживания", []string{"Все файлы (*.*)", "*.*"}); p != "" {
				pSetWindowTextW.Call(app.edits[idCondText], uintptr(unsafe.Pointer(wstr(p))))
			}
			return
		}
		if pointIn(app.editorBrowseRect, x, y) && (app.conditionDraft.Type == condFolderCount || app.conditionDraft.Type == condDiskFree || app.conditionDraft.Type == condDrivePresent) {
			if p := chooseFolder("Выберите папку или диск"); p != "" {
				pSetWindowTextW.Call(app.edits[idCondText], uintptr(unsafe.Pointer(wstr(p))))
			}
			return
		}
		if pointIn(app.editorBrowseRect, x, y) && (app.conditionDraft.Type == condProcessExit || app.conditionDraft.Type == condAudioSilent || app.conditionDraft.Type == condProcessCPU || app.conditionDraft.Type == condProcessGPU || app.conditionDraft.Type == condProcessRAM) {
			app.conditionDraft.Text = strings.TrimSpace(getText(app.edits[idCondText]))
			openProcessPicker(3)
			return
		}
		if pointIn(app.editorClearRect, x, y) && (app.conditionDraft.Type == condProcessExit || app.conditionDraft.Type == condAudioSilent || app.conditionDraft.Type == condProcessCPU || app.conditionDraft.Type == condProcessGPU || app.conditionDraft.Type == condProcessRAM) {
			app.conditionDraft.Text = ""
			setEditTextIfDifferent(idCondText, "")
			playUI(clickSound)
			invalidate(app.hwnd)
			return
		}
		if pointIn(app.editorSaveRect, x, y) {
			saveConditionDraft()
			return
		}
		if pointIn(app.editorCancelRect, x, y) {
			if app.scenarioSavedDraft {
				app.section = 13
			} else {
				app.section = 7
			}
			app.editingCondition = -1
			playUI(openSound)
			startPageAnimation()
			updateInputVisibility()
			invalidate(app.hwnd)
			return
		}
	}
	if app.section == 9 {
		for i, r := range app.stepErrorRects {
			if pointIn(r, x, y) {
				if app.stepDraft.OnError == i {
					return
				}
				app.stepDraft.OnError = i
				playUI(clickSound)
				startSubReveal()
				updateInputVisibility()
				invalidate(app.hwnd)
				return
			}
		}
		stepTypes := []int{stepCloseProcesses, stepWait, stepRunCommand, stepNotify, stepMonitorOff, stepMonitorOn, stepSetVolume, stepLockWorkstation, stepPowerPlan, stepProcessPriority}
		for i := 0; i < len(stepTypes); i++ {
			r := app.stepTypeRects[i]
			if pointIn(r, x, y) {
				t := stepTypes[i]
				if app.stepDraft.Type == t {
					return
				}
				if app.stepDraft.Type == stepWait || app.stepDraft.Type == stepSetVolume {
					app.stepDraft.Value = parseInt(getText(app.edits[idStepValue]), app.stepDraft.Value)
				}
				if app.stepDraft.Type == stepRunCommand || app.stepDraft.Type == stepNotify || app.stepDraft.Type == stepProcessPriority {
					app.stepDraft.Text = strings.TrimSpace(getText(app.edits[idStepText]))
				}
				app.stepDraft.Type = t
				pSetWindowTextW.Call(app.edits[idStepValue], uintptr(unsafe.Pointer(wstr(strconv.Itoa(max(0, app.stepDraft.Value))))))
				pSetWindowTextW.Call(app.edits[idStepText], uintptr(unsafe.Pointer(wstr(app.stepDraft.Text))))
				playUI(clickSound)
				startSubReveal()
				updateInputVisibility()
				invalidate(app.hwnd)
				return
			}
		}
		if app.stepDraft.Type == stepSetVolume || app.stepDraft.Type == stepMute {
			for i, rr := range app.powerPlanRects {
				if !pointIn(rr, x, y) {
					continue
				}
				switch i {
				case 0:
					app.stepDraft.Type = stepSetVolume
					if app.stepDraft.Value < 0 || app.stepDraft.Value > 100 {
						app.stepDraft.Value = 50
					}
					setEditTextIfDifferent(idStepValue, strconv.Itoa(clampInt(app.stepDraft.Value, 0, 100)))
				case 1:
					app.stepDraft.Type, app.stepDraft.Value = stepMute, 1
				case 2:
					app.stepDraft.Type, app.stepDraft.Value = stepMute, 0
				}
				playUI(clickSound)
				startSubReveal()
				updateInputVisibility()
				invalidate(app.hwnd)
				return
			}
		}
		if app.stepDraft.Type == stepPowerPlan {
			for i, rr := range app.powerPlanRects {
				if pointIn(rr, x, y) {
					app.stepDraft.Value = i
					playUI(clickSound)
					invalidate(app.hwnd)
					return
				}
			}
		}
		if app.stepDraft.Type == stepProcessPriority {
			for i, rr := range app.powerPlanRects {
				if pointIn(rr, x, y) {
					app.stepDraft.Value = i
					playUI(clickSound)
					invalidate(app.hwnd)
					return
				}
			}
		}
		if pointIn(app.editorBrowseRect, x, y) && app.stepDraft.Type == stepCloseProcesses {
			openProcessPicker(6)
			return
		}
		if pointIn(app.editorBrowseRect, x, y) && app.stepDraft.Type == stepProcessPriority {
			app.stepDraft.Text = strings.TrimSpace(getText(app.edits[idStepText]))
			openProcessPicker(7)
			return
		}
		if pointIn(app.editorBrowseRect, x, y) && app.stepDraft.Type == stepRunCommand {
			if p := chooseOpenFile("Выберите программу или скрипт", []string{"Программы и скрипты (*.exe;*.bat;*.cmd;*.ps1)", "*.exe;*.bat;*.cmd;*.ps1", "Все файлы (*.*)", "*.*"}); p != "" {
				pSetWindowTextW.Call(app.edits[idStepText], uintptr(unsafe.Pointer(wstr(`"`+p+`"`))))
			}
			return
		}
		if pointIn(app.editorSaveRect, x, y) {
			saveStepDraft()
			return
		}
		if pointIn(app.editorCancelRect, x, y) {
			if app.scenarioSavedDraft {
				app.section = 13
			} else {
				app.section = 7
			}
			app.editingStep = -1
			playUI(openSound)
			startPageAnimation()
			updateInputVisibility()
			invalidate(app.hwnd)
			return
		}
	}

	if hasTaskFooter(app.section) && pointIn(app.startRect, x, y) {
		playUI(clickSound)
		startSchedule()
		return
	}
	if hasTaskFooter(app.section) && pointIn(app.saveTaskRect, x, y) {
		syncFields()
		app.section = 6
		suggested := actionSummary() + " — " + whenSummary()
		pSetWindowTextW.Call(app.edits[idTaskName], uintptr(unsafe.Pointer(wstr(suggested))))
		playUI(openSound)
		startPageAnimation()
		updateInputVisibility()
		invalidate(app.hwnd)
		return
	}
	if hasTaskFooter(app.section) && pointIn(app.cancelRect, x, y) {
		playUI(clickSound)
		cancelSchedule(true)
		return
	}
	if hasTaskFooter(app.section) && pointIn(app.postponeRect, x, y) {
		playUI(clickSound)
		if app.schedule.active {
			postpone10()
		} else {
			app.lastTaskSection = app.section
			app.checkReturnSection = app.section
			app.section = 12
			startPageAnimation()
			updateInputVisibility()
			invalidate(app.hwnd)
		}
		return
	}
}
func onCommand(id, code int, lParam uintptr) {
	if code == EN_SETFOCUS {
		suppressEditVisibilityDuringLayout = true
		defer func() { suppressEditVisibilityDuringLayout = false }()
		if id == idSavedSearch && app.savedSearchPlaceholder {
			app.savedSearchPlaceholder = false
			pSetWindowTextW.Call(app.edits[idSavedSearch], uintptr(unsafe.Pointer(wstr(""))))
		}
		if id == idHistorySearch && app.historySearchPlaceholder {
			app.historySearchPlaceholder = false
			pSetWindowTextW.Call(app.edits[idHistorySearch], uintptr(unsafe.Pointer(wstr(""))))
		}
		if id == idResourceSearch && app.resourceProcessSearchPlaceholder {
			app.resourceProcessSearchPlaceholder = false
			pSetWindowTextW.Call(app.edits[idResourceSearch], uintptr(unsafe.Pointer(wstr(""))))
		}
		invalidate(app.hwnd)
		return
	}
	if code == EN_KILLFOCUS {
		if id == idGraphWidth || id == idGraphHeight {
			applyGraphWindowSizeFields()
		}
		if id == idSavedSearch && strings.TrimSpace(getText(app.edits[idSavedSearch])) == "" {
			app.savedSearchPlaceholder = true
			pSetWindowTextW.Call(app.edits[idSavedSearch], uintptr(unsafe.Pointer(wstr("Поиск по сохранённым задачам"))))
		}
		if id == idHistorySearch && strings.TrimSpace(getText(app.edits[idHistorySearch])) == "" {
			app.historySearchPlaceholder = true
			pSetWindowTextW.Call(app.edits[idHistorySearch], uintptr(unsafe.Pointer(wstr("Поиск по истории"))))
		}
		if id == idResourceSearch && strings.TrimSpace(getText(app.edits[idResourceSearch])) == "" {
			app.resourceProcessSearchPlaceholder = true
			pSetWindowTextW.Call(app.edits[idResourceSearch], uintptr(unsafe.Pointer(wstr("Поиск по процессам"))))
		}
		invalidate(app.hwnd)
		return
	}
	if code == EN_CHANGE {
		if id == idSavedSearch {
			if app.savedSearchPlaceholder {
				app.savedSearchText = ""
			} else {
				app.savedSearchText = getText(app.edits[idSavedSearch])
			}
			rebuildSavedFilter()
			app.savedScroll = 0
			app.savedScrollPx, app.savedScrollTarget = 0, 0
			layoutControls(app.hwnd)
			// Layout may hide/show child EDITs; restore keyboard focus and caret so typing is continuous.
			if !app.savedSearchPlaceholder {
				pSetFocus.Call(app.edits[idSavedSearch])
				pSendMessageW.Call(app.edits[idSavedSearch], EM_SETSEL, ^uintptr(0), ^uintptr(0))
			}
		}
		if id == idHistorySearch {
			if app.historySearchPlaceholder {
				app.historySearchText = ""
			} else {
				app.historySearchText = getText(app.edits[idHistorySearch])
			}
			invalidateHistoryFilterCache()
			app.historyScroll = 0
			app.historyScrollPx, app.historyScrollTarget = 0, 0
			layoutControls(app.hwnd)
			if !app.historySearchPlaceholder {
				pSetFocus.Call(app.edits[idHistorySearch])
				pSendMessageW.Call(app.edits[idHistorySearch], EM_SETSEL, ^uintptr(0), ^uintptr(0))
			}
		}
		if id == idResourceSearch {
			if app.resourceProcessSearchPlaceholder {
				app.resourceProcessSearchText = ""
			} else {
				app.resourceProcessSearchText = getText(app.edits[idResourceSearch])
			}
			app.resourceProcScrollPx, app.resourceProcScrollTarget = 0, 0
			layoutControls(app.hwnd)
			if !app.resourceProcessSearchPlaceholder {
				pSetFocus.Call(app.edits[idResourceSearch])
				pSendMessageW.Call(app.edits[idResourceSearch], EM_SETSEL, ^uintptr(0), ^uintptr(0))
			}
		}
		if id == idTimelineTicks {
			txt := strings.TrimSpace(getText(app.edits[idTimelineTicks]))
			if txt != "" {
				if raw, err := strconv.Atoi(txt); err == nil {
					v := clampInt(raw, 2, 12)
					if raw != v {
						setEditTextIfDifferent(idTimelineTicks, strconv.Itoa(v))
					}
					if v != app.settings.ResourceTimelineTicks {
						app.settings.ResourceTimelineTicks = v
						saveSettings()
						layoutControls(app.hwnd)
						pSetFocus.Call(app.edits[idTimelineTicks])
						pSendMessageW.Call(app.edits[idTimelineTicks], EM_SETSEL, ^uintptr(0), ^uintptr(0))
						invalidate(app.hwnd)
					}
				}
			}
		}
		if id == idSoundVolume {
			txt := strings.TrimSpace(getText(app.edits[idSoundVolume]))
			if txt != "" {
				if raw, err := strconv.Atoi(txt); err == nil {
					v := clampInt(raw, 0, 100)
					if raw != v {
						setEditTextIfDifferent(idSoundVolume, strconv.Itoa(v))
					}
					if v != app.settings.SoundVolume {
						app.settings.SoundVolume = v
						repositionVolumeKnob()
						saveSettings()
					}
				}
			}
		}
		invalidate(app.hwnd)
	}
	_ = lParam
}

func resizeWindowClient(w, h int) {
	if app.hwnd == 0 {
		return
	}
	if z, _, _ := pIsZoomed.Call(app.hwnd); z != 0 {
		pShowWindow.Call(app.hwnd, SW_RESTORE)
	}
	w = max(normalMinClientW, w)
	h = max(normalMinClientH, h)
	w, h = scaledInt040(w), scaledInt040(h)
	var wr RECT
	if ok, _, _ := pGetWindowRect.Call(app.hwnd, uintptr(unsafe.Pointer(&wr))); ok == 0 {
		return
	}
	pSetWindowPos.Call(app.hwnd, 0, uintptr(wr.Left), uintptr(wr.Top), uintptr(w), uintptr(h), SWP_NOZORDER|SWP_NOACTIVATE)
}

func enforceMinimumClientSize(hwnd uintptr) bool {
	if hwnd == 0 || app.miniMode {
		return false
	}
	if z, _, _ := pIsZoomed.Call(hwnd); z != 0 && !app.settings.LockMinimumSize && !app.settings.LockCurrentSize {
		return false
	}
	var rc RECT
	pGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&rc)))
	w, h := int(rc.Right-rc.Left), int(rc.Bottom-rc.Top)
	mw, mh := normalMinPhysical040()
	tw, th := max(int(mw), w), max(int(mh), h)
	if app.settings.LockMinimumSize {
		tw, th = int(mw), int(mh)
	} else if app.settings.LockCurrentSize {
		tw = max(int(mw), app.settings.LockedWindowW)
		th = max(int(mh), app.settings.LockedWindowH)
	}
	if tw == w && th == h {
		return false
	}
	var wr RECT
	pGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&wr)))
	pSetWindowPos.Call(hwnd, 0, uintptr(wr.Left), uintptr(wr.Top), uintptr(tw), uintptr(th), SWP_NOZORDER|SWP_NOACTIVATE)
	return true
}

func hitTestWindow(hwnd uintptr, lParam uintptr) uintptr {
	pt := POINT{X: int32(int16(loword(lParam))), Y: int32(int16(hiword(lParam)))}
	pScreenToClient.Call(hwnd, uintptr(unsafe.Pointer(&pt)))
	var rc RECT
	pGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&rc)))

	// Manual resize hit-testing is necessary because the native non-client frame is removed.
	if !app.miniMode && !app.settings.LockMinimumSize && !app.settings.LockCurrentSize {
		if z, _, _ := pIsZoomed.Call(hwnd); z == 0 {
			const edge int32 = 7
			left := pt.X >= 0 && pt.X < edge
			right := pt.X >= rc.Right-edge && pt.X < rc.Right
			top := pt.Y >= 0 && pt.Y < edge
			bottom := pt.Y >= rc.Bottom-edge && pt.Y < rc.Bottom
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
	}

	// Caption buttons are laid out in logical UI coordinates.
	lx, ly := clientPointToLogical040(pt.X, pt.Y)
	// Caption buttons must remain client hit targets so our custom renderer owns them.
	for _, r := range []RECT{app.miniBtnRect, app.minBtnRect, app.maxBtnRect, app.closeBtnRect} {
		if pointIn(r, lx, ly) {
			return HTCLIENT
		}
	}
	if pointIn(app.titleBarRect, lx, ly) {
		return HTCAPTION
	}
	return HTCLIENT
}

func setMiniMode(enabled bool) {
	if app.miniMode == enabled {
		return
	}
	if enabled {
		if z, _, _ := pIsZoomed.Call(app.hwnd); z != 0 {
			pShowWindow.Call(app.hwnd, SW_RESTORE)
		}
		var r RECT
		pGetWindowRect.Call(app.hwnd, uintptr(unsafe.Pointer(&r)))
		app.normalWindowRect = r
		app.miniMode = true
		layoutControls(app.hwnd)
		mw, mh := miniClientSize040()
		pSetWindowPos.Call(app.hwnd, 0, uintptr(r.Left), uintptr(r.Top), uintptr(mw), uintptr(mh), SWP_NOZORDER|SWP_NOACTIVATE)
		applyMiniTopmost040()
	} else {
		app.miniMode = false
		r := app.normalWindowRect
		if r.Right <= r.Left || r.Bottom <= r.Top {
			r = RECT{120, 80, 1240, 840}
		}
		applyMiniTopmost040()
		pSetWindowPos.Call(app.hwnd, 0, uintptr(r.Left), uintptr(r.Top), uintptr(r.Right-r.Left), uintptr(r.Bottom-r.Top), SWP_NOZORDER|SWP_NOACTIVATE)
		layoutControls(app.hwnd)
	}
	playUI(openSound)
	invalidate(app.hwnd)
}

func surfacePanelColor() uint32 {
	switch app.settings.SurfaceStyle {
	case 1: // softer surfaces
		return blendColor(theme.bg, theme.panel2, .62)
	case 2: // subtly accent-tinted surfaces
		return blendColor(theme.panel, theme.accent, .10)
	case 3: // glass-like surface, still opaque for a stable Win32 window
		return blendColor(theme.bg, theme.panel2, .48)
	case 4: // Liquid Glass base tint; Direct2D adds translucency/specular optics in roundFill.
		return blendColor(theme.bg, theme.panel2, .54)
	default:
		return theme.panel
	}
}

func surfaceButtonColor() uint32 {
	switch app.settings.SurfaceStyle {
	case 1:
		return blendColor(theme.bg, theme.panel2, .80)
	case 2:
		return blendColor(theme.panel2, theme.accent, .08)
	case 3:
		return blendColor(theme.bg, theme.panel2, .66)
	case 4:
		return blendColor(theme.bg, theme.panel2, .58)
	default:
		return theme.panel2
	}
}

func surfaceBorderColor() uint32 {
	switch app.settings.SurfaceStyle {
	case 1:
		return blendColor(theme.border, theme.bg, .20)
	case 2:
		return blendColor(theme.border, theme.accent, .24)
	case 3:
		return blendColor(theme.border, theme.text, .16)
	case 4:
		return blendColor(theme.border, theme.text, .36)
	default:
		return theme.border
	}
}

func refreshControlBrush() {
	if controlBrush != 0 {
		pDeleteObject.Call(controlBrush)
		controlBrush = 0
	}
	if app.hwnd != 0 {
		controlBrush = solid(surfaceButtonColor())
	}
}

func applyTheme() {
	mode := app.settings.ThemeMode
	if mode == 2 {
		if systemUsesLightTheme() {
			theme = lightTheme
		} else {
			theme = darkTheme
		}
	} else if mode == 1 {
		theme = lightTheme
	} else {
		theme = darkTheme
	}
	refreshControlBrush()
}

func loadHistoryItems() {
	app.historyItems = nil
	invalidateHistoryFilterCache()
	b, err := os.ReadFile(historyPath())
	if err != nil {
		return
	}
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 3)
		it := HistoryItem{}
		if len(parts) > 0 {
			it.When = parts[0]
		}
		if len(parts) > 1 {
			it.Kind = parts[1]
		}
		if len(parts) > 2 {
			it.Detail = parts[2]
			it.RunID = historyRunID(it.Detail)
		}
		app.historyItems = append(app.historyItems, it)
		if len(app.historyItems) >= 1000 {
			break
		}
	}
	if app.historyScroll >= len(app.historyItems) {
		app.historyScroll = 0
		app.historyScrollPx = 0
		app.historyScrollTarget = 0
	}
}

func clearHistory() {
	_ = os.Remove(historyPath())
	app.historyItems = nil
	invalidateHistoryFilterCache()
	app.historyScroll = 0
	app.historyScrollPx = 0
	app.historyScrollTarget = 0
}

func pointIn(r RECT, x, y int32) bool {
	return x >= r.Left && x < r.Right && y >= r.Top && y < r.Bottom
}
func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func actionSummary() string {
	names := []string{"Выключение", "Перезагрузка", "Сон", "Гибернация", "Завершить задачу"}
	if app.selectedAction >= 0 && app.selectedAction < len(names) {
		return names[app.selectedAction]
	}
	return "Не выбрано"
}
func whenSummary() string {
	switch app.selectedMode {
	case 0:
		return fmt.Sprintf("Таймер %02d:%02d:%02d", parseInt(getText(app.edits[idDelayHours]), app.settings.DelayHours), parseInt(getText(app.edits[idDelayMinutes]), app.settings.DelayMinutes), parseInt(getText(app.edits[idDelaySeconds]), app.settings.DelaySeconds))
	case 1:
		v := exactFromFields()
		if strings.TrimSpace(v) == "" {
			v = app.settings.Exact
		}
		return v
	case 2:
		return fmt.Sprintf("Простой %d сек", parseInt(getText(app.edits[idIdleMinutes]), app.settings.IdleMinutes))
	case 3:
		v := strings.TrimSpace(getText(app.edits[idWatchProcess]))
		if v == "" {
			v = app.settings.WatchProcess
		}
		if v == "" {
			v = "Процесс"
		}
		return v
	case 4:
		return recurrenceSummary(app.settings.Recurrence)
	case 5:
		return "По условиям"
	}
	return "Не задано"
}
func processCountPhrase(n int) string {
	word := "процессов"
	n10, n100 := n%10, n%100
	if n10 == 1 && n100 != 11 {
		word = "процесс"
	} else if n10 >= 2 && n10 <= 4 && !(n100 >= 12 && n100 <= 14) {
		word = "процесса"
	}
	return fmt.Sprintf("%d %s", n, word)
}
func extraSummary() string {
	if !app.settings.CloseBefore {
		return "Не закрывать процессы"
	}
	return "Закрыть " + processCountPhrase(len(app.settings.Processes))
}
func drawToggle(hdc uintptr, r RECT, enabled bool) {
	// Toggle uses the same immediate hover language as buttons: a small pop-up plus outline.
	// Hit-testing remains on the original rectangle, so the animation never changes click geometry.
	rv, hv := hoverCardRect(r)
	outer := surfaceButtonColor()
	if enabled {
		outer = blendColor(surfaceButtonColor(), theme.accent, .88)
	} else if hv > 0 {
		outer = blendColor(outer, theme.accent2, .08*hv)
	}
	roundFill(hdc, rv, outer, 8)
	if hv > 0 && ui2d.active {
		d2dDrawRoundedOutline(rv, 8, float32(1+0.35*hv), blendColor(theme.border, theme.accent2, .46))
	}
	sz := int32(10)
	cx := (rv.Left + rv.Right) / 2
	cy := (rv.Top + rv.Bottom) / 2
	inner := RECT{cx - sz/2, cy - sz/2, cx + sz/2, cy + sz/2}
	innerColor := theme.border
	if enabled {
		innerColor = theme.text
	}
	roundFill(hdc, inner, innerColor, 3)
}

func savedTaskSummary(t SavedTask) string {
	prefix := ""
	if t.Paused {
		prefix = "На паузе · "
	}
	if t.TaskKind == 1 {
		return prefix + "Продвинутая"
	}
	return prefix + "Простая"
}

func saveCurrentTask() {
	syncFields()
	if app.settings.TaskKind == 1 {
		syncCurrentGraphFromLegacy()
		syncLegacyFromCurrentGraph()
	}
	if !validateTaskDialog040(captureTaskState(), true) {
		return
	}
	name := strings.TrimSpace(getText(app.edits[idTaskName]))
	if name == "" {
		name = fmt.Sprintf("Задача %d", len(app.settings.SavedTasks)+1)
	}
	conds := append([]AutomationCondition(nil), app.settings.AdvancedConditions...)
	steps := cloneActionSteps(app.settings.ActionSteps)
	if app.settings.TaskKind == 0 {
		conds, steps = nil, nil
	}
	closeBefore := app.settings.CloseBefore
	processes := append([]string(nil), app.settings.Processes...)
	if app.settings.TaskKind == 1 {
		closeBefore = false
		processes = nil
	}
	t := SavedTask{
		ID: fmt.Sprintf("%d", time.Now().UnixNano()), Name: name,
		Action: app.selectedAction, Mode: app.selectedMode,
		DelayHours: app.settings.DelayHours, DelayMinutes: app.settings.DelayMinutes, DelaySeconds: app.settings.DelaySeconds,
		Exact: app.settings.Exact, IdleMinutes: app.settings.IdleMinutes, WatchProcess: app.settings.WatchProcess,
		CloseBefore: closeBefore, Processes: processes, WarningSeconds: app.settings.WarningSeconds,
		Conditions: conds, TriggerLogic: app.settings.TriggerLogic,
		Steps: steps, Recurrence: app.settings.Recurrence, TaskKind: app.settings.TaskKind,
		Graph: cloneScenarioGraph(app.settings.ScenarioGraph),
	}
	app.settings.SavedTasks = append(app.settings.SavedTasks, t)
	saveSettings()
	maintainWakeTimer(time.Now())
	appendHistory("SAVE", t.Name)
	pSetWindowTextW.Call(app.edits[idTaskName], uintptr(unsafe.Pointer(wstr(""))))
	app.section = 5
	app.savedScroll = 0
	app.savedScrollPx = 0
	app.savedScrollTarget = 0
	playUI(successSound)
	startPageAnimation()
	updateInputVisibility()
	invalidate(app.hwnd)
}

func loadSavedTask(t SavedTask) {
	app.selectedAction = t.Action
	app.selectedMode = t.Mode
	app.settings.Action = t.Action
	app.settings.Mode = t.Mode
	app.settings.DelayHours = t.DelayHours
	app.settings.DelayMinutes = t.DelayMinutes
	app.settings.DelaySeconds = t.DelaySeconds
	app.settings.Exact = t.Exact
	app.settings.IdleMinutes = t.IdleMinutes
	app.settings.WatchProcess = t.WatchProcess
	app.settings.CloseBefore = t.CloseBefore
	app.settings.Processes = append([]string(nil), t.Processes...)
	app.settings.WarningSeconds = t.WarningSeconds
	app.settings.AdvancedConditions = append([]AutomationCondition(nil), t.Conditions...)
	app.settings.TriggerLogic = t.TriggerLogic
	app.settings.ActionSteps = cloneActionSteps(t.Steps)
	app.settings.Recurrence = t.Recurrence
	app.settings.ScenarioGraph = ensureScenarioGraph(cloneScenarioGraph(t.Graph), taskStateFromSaved040(t))
	app.settings.TaskKind = t.TaskKind
	pSetWindowTextW.Call(app.edits[idDelayHours], uintptr(unsafe.Pointer(wstr(strconv.Itoa(t.DelayHours)))))
	pSetWindowTextW.Call(app.edits[idDelayMinutes], uintptr(unsafe.Pointer(wstr(strconv.Itoa(t.DelayMinutes)))))
	pSetWindowTextW.Call(app.edits[idDelaySeconds], uintptr(unsafe.Pointer(wstr(strconv.Itoa(t.DelaySeconds)))))
	pSetWindowTextW.Call(app.edits[idExact], uintptr(unsafe.Pointer(wstr(t.Exact))))
	setExactFields(t.Exact)
	pSetWindowTextW.Call(app.edits[idIdleMinutes], uintptr(unsafe.Pointer(wstr(strconv.Itoa(max(t.IdleMinutes, 1))))))
	pSetWindowTextW.Call(app.edits[idWatchProcess], uintptr(unsafe.Pointer(wstr(t.WatchProcess))))
	pSetWindowTextW.Call(app.edits[idWarning], uintptr(unsafe.Pointer(wstr(strconv.Itoa(max(t.WarningSeconds, 0))))))
	pSetWindowTextW.Call(app.edits[idScheduleTime], uintptr(unsafe.Pointer(wstr(t.Recurrence.TimeHHMM))))
	for i := range app.actionAnim {
		app.actionAnim[i] = 0
		app.modeAnim[i] = 0
	}
	if app.selectedAction >= 0 && app.selectedAction < 4 {
		app.actionAnim[app.selectedAction] = 1
	}
	if app.selectedMode >= 0 && app.selectedMode < len(app.modeAnim) {
		app.modeAnim[app.selectedMode] = 1
	}
	if t.TaskKind == 1 {
		resetGraphInteraction()
		app.section = 7
		app.lastTaskSection = 2
	} else {
		app.section = 0
		app.lastTaskSection = 0
	}
	saveSettings()
	appendHistory("LOAD", t.Name)
	updateInputVisibility()
	invalidate(app.hwnd)
}

func startOrStopSavedTask(idx int) {
	if idx < 0 || idx >= len(app.settings.SavedTasks) {
		return
	}
	t := app.settings.SavedTasks[idx]
	if app.schedule.active {
		if app.schedule.sourceTaskID == t.ID {
			cancelSchedule(true)
			app.status = "Сохранённая задача остановлена: " + t.Name
			invalidate(app.hwnd)
			return
		}
		message("PowerPilot", "Сначала остановите уже активную задачу.", MB_OK|MB_ICONINFORMATION)
		return
	}
	now := time.Now()
	conds := append([]AutomationCondition(nil), t.Conditions...)
	steps := cloneActionSteps(t.Steps)
	graph := ScenarioGraph{}
	if t.TaskKind == 1 {
		graph = ensureScenarioGraph(cloneScenarioGraph(t.Graph), taskStateFromSaved040(t))
		if reason := scenarioGraphValidationError(graph); reason != "" {
			message("Ошибка схемы", reason, MB_OK|MB_ICONERROR)
			return
		}
		conds = nil
		steps = nil
	}
	closeBefore := false
	processes := []string(nil)
	if t.TaskKind == 0 {
		conds = nil
		steps = nil
		closeBefore = t.CloseBefore
		processes = append([]string(nil), t.Processes...)
	}
	s := Schedule{active: true, action: t.Action, mode: t.Mode, started: now, sourceTaskID: t.ID, sourceTaskName: t.Name, runID: newRunID(),
		conditions: conds, triggerLogic: t.TriggerLogic,
		steps: steps, closeBefore: closeBefore, processes: processes, warningSeconds: t.WarningSeconds, graph: graph}
	switch t.Mode {
	case 0:
		d := time.Duration(t.DelayHours)*time.Hour + time.Duration(t.DelayMinutes)*time.Minute + time.Duration(t.DelaySeconds)*time.Second
		if d <= 0 {
			message("Ошибка", "У сохранённой задачи таймер должен быть больше нуля.", MB_OK|MB_ICONERROR)
			return
		}
		s.target = now.Add(d)
		s.total = d
	case 1:
		tm, err := parseExact(t.Exact)
		if err != nil || !tm.After(now) {
			message("Ошибка", "Дата и время сохранённой задачи уже прошли или некорректны.", MB_OK|MB_ICONERROR)
			return
		}
		s.target = tm
		s.total = tm.Sub(now)
	case 2:
		secs := max(t.IdleMinutes, 1)
		s.idleThreshold = time.Duration(secs) * time.Second
	case 3:
		pn := strings.TrimSpace(t.WatchProcess)
		if pn == "" {
			message("Ошибка", "В сохранённой задаче не выбран процесс.", MB_OK|MB_ICONERROR)
			return
		}
		if !processRunning(pn) {
			message("Ошибка", "Выбранный процесс сейчас не запущен.", MB_OK|MB_ICONERROR)
			return
		}
		s.watchProcess = pn
	case 4:
		tm, err := nextOccurrence(t.Recurrence, now)
		if err != nil {
			message("Ошибка", "Не удалось определить следующий запуск расписания.", MB_OK|MB_ICONERROR)
			return
		}
		s.target = tm
		s.total = tm.Sub(now)
	case 5:
		if t.TaskKind != 1 || (len(graph.Nodes) == 0 && len(conds) == 0) {
			message("Ошибка", "В сохранённой продвинутой задаче нет условий.", MB_OK|MB_ICONERROR)
			return
		}
	}
	app.schedule = s
	resetConditionRuntimes()
	app.status = "Сохранённая задача активна: " + t.Name
	app.progress = 0
	appendRunHistory("START", "saved="+t.Name, s.runID)
	playUI(successSound)
	showNotification("PowerPilot", "Запущена сохранённая задача: "+t.Name)
	tick()
	invalidate(app.hwnd)
}

func openSavedTaskEditor(idx int) {
	if idx < 0 || idx >= len(app.settings.SavedTasks) {
		return
	}
	app.editingSavedIdx = idx
	app.savedEditDraft = app.settings.SavedTasks[idx]
	app.savedEditDraft.Processes = append([]string(nil), app.settings.SavedTasks[idx].Processes...)
	app.savedEditDraft.Conditions = append([]AutomationCondition(nil), app.settings.SavedTasks[idx].Conditions...)
	app.savedEditDraft.Steps = cloneActionSteps(app.settings.SavedTasks[idx].Steps)
	app.savedEditDraft.Graph = ensureScenarioGraph(cloneScenarioGraph(app.settings.SavedTasks[idx].Graph), taskStateFromSaved040(app.settings.SavedTasks[idx]))
	pSetWindowTextW.Call(app.edits[idTaskName], uintptr(unsafe.Pointer(wstr(app.savedEditDraft.Name))))
	pSetWindowTextW.Call(app.edits[idDelayHours], uintptr(unsafe.Pointer(wstr(strconv.Itoa(app.savedEditDraft.DelayHours)))))
	pSetWindowTextW.Call(app.edits[idDelayMinutes], uintptr(unsafe.Pointer(wstr(strconv.Itoa(app.savedEditDraft.DelayMinutes)))))
	pSetWindowTextW.Call(app.edits[idDelaySeconds], uintptr(unsafe.Pointer(wstr(strconv.Itoa(app.savedEditDraft.DelaySeconds)))))
	setExactFields(app.savedEditDraft.Exact)
	pSetWindowTextW.Call(app.edits[idIdleMinutes], uintptr(unsafe.Pointer(wstr(strconv.Itoa(max(app.savedEditDraft.IdleMinutes, 1))))))
	pSetWindowTextW.Call(app.edits[idWatchProcess], uintptr(unsafe.Pointer(wstr(app.savedEditDraft.WatchProcess))))
	pSetWindowTextW.Call(app.edits[idScheduleTime], uintptr(unsafe.Pointer(wstr(app.savedEditDraft.Recurrence.TimeHHMM))))
	pSetWindowTextW.Call(app.edits[idWarning], uintptr(unsafe.Pointer(wstr(strconv.Itoa(max(app.savedEditDraft.WarningSeconds, 0))))))
	app.savedMenuOpenIdx = -1
	if app.savedEditDraft.TaskKind == 1 {
		resetGraphInteraction()
		app.scenarioSavedDraft = true
		app.scenarioReturnSection = 5
		app.section = 13
	} else {
		app.scenarioSavedDraft = false
		app.section = 10
	}
	playUI(openSound)
	startPageAnimation()
	updateInputVisibility()
	invalidate(app.hwnd)
}

func saveSavedTaskEditor() {
	idx := app.editingSavedIdx
	if idx < 0 || idx >= len(app.settings.SavedTasks) {
		return
	}
	if app.scenarioSavedDraft && app.savedEditDraft.TaskKind == 1 {
		syncCurrentGraphFromLegacy()
		syncLegacyFromCurrentGraph()
	}
	t := app.savedEditDraft
	t.Name = strings.TrimSpace(getText(app.edits[idTaskName]))
	if t.Name == "" {
		t.Name = app.settings.SavedTasks[idx].Name
	}
	if app.scenarioSavedDraft && t.TaskKind == 1 {
		if !validateTaskDialog040(taskStateFromSaved040(t), true) {
			return
		}
		app.settings.SavedTasks[idx] = t
		saveSettings()
		maintainWakeTimer(time.Now())
		appendHistory("EDIT", t.Name)
		app.editingSavedIdx = -1
		app.scenarioSavedDraft = false
		app.section = 5
		app.savedScroll, app.savedScrollPx, app.savedScrollTarget = 0, 0, 0
		restoreCurrentInputTexts()
		playUI(successSound)
		startPageAnimation()
		updateInputVisibility()
		invalidate(app.hwnd)
		return
	}
	t.DelayHours = parseInt(getText(app.edits[idDelayHours]), 0)
	t.DelayMinutes = parseInt(getText(app.edits[idDelayMinutes]), 0)
	t.DelaySeconds = parseInt(getText(app.edits[idDelaySeconds]), 0)
	t.Exact = exactFromFields()
	t.IdleMinutes = parseInt(getText(app.edits[idIdleMinutes]), 30)
	t.WatchProcess = strings.TrimSpace(getText(app.edits[idWatchProcess]))
	if v := strings.TrimSpace(getText(app.edits[idScheduleTime])); v != "" {
		t.Recurrence.TimeHHMM = v
	}
	t.WarningSeconds = clampInt(parseInt(getText(app.edits[idWarning]), t.WarningSeconds), 0, 86400)
	if !validateTaskDialog040(taskStateFromSaved040(t), true) {
		return
	}
	app.settings.SavedTasks[idx] = t
	saveSettings()
	maintainWakeTimer(time.Now())
	appendHistory("EDIT", t.Name)
	app.editingSavedIdx = -1
	app.scenarioSavedDraft = false
	app.section = 5
	app.savedScroll = 0
	app.savedScrollPx = 0
	app.savedScrollTarget = 0
	restoreCurrentInputTexts()
	playUI(successSound)
	startPageAnimation()
	updateInputVisibility()
	invalidate(app.hwnd)
}

func restoreCurrentInputTexts() {
	pSetWindowTextW.Call(app.edits[idDelayHours], uintptr(unsafe.Pointer(wstr(strconv.Itoa(app.settings.DelayHours)))))
	pSetWindowTextW.Call(app.edits[idDelayMinutes], uintptr(unsafe.Pointer(wstr(strconv.Itoa(app.settings.DelayMinutes)))))
	pSetWindowTextW.Call(app.edits[idDelaySeconds], uintptr(unsafe.Pointer(wstr(strconv.Itoa(app.settings.DelaySeconds)))))
	setExactFields(app.settings.Exact)
	pSetWindowTextW.Call(app.edits[idIdleMinutes], uintptr(unsafe.Pointer(wstr(strconv.Itoa(max(app.settings.IdleMinutes, 1))))))
	pSetWindowTextW.Call(app.edits[idWatchProcess], uintptr(unsafe.Pointer(wstr(app.settings.WatchProcess))))
	pSetWindowTextW.Call(app.edits[idScheduleTime], uintptr(unsafe.Pointer(wstr(app.settings.Recurrence.TimeHHMM))))
	pSetWindowTextW.Call(app.edits[idTaskName], uintptr(unsafe.Pointer(wstr(""))))
}

func duplicateSavedTask(idx int) {
	if idx < 0 || idx >= len(app.settings.SavedTasks) {
		return
	}
	src := app.settings.SavedTasks[idx]
	cp := src
	cp.ID = newAutomationID("task")
	cp.Name = strings.TrimSpace(src.Name) + " — копия"
	cp.Processes = append([]string(nil), src.Processes...)
	cp.Conditions = append([]AutomationCondition(nil), src.Conditions...)
	for i := range cp.Conditions {
		cp.Conditions[i].ID = newAutomationID("cond")
	}
	cp.Steps = cloneActionSteps(src.Steps)
	for i := range cp.Steps {
		cp.Steps[i].ID = newAutomationID("step")
	}
	cp.Graph = cloneScenarioGraph(src.Graph)
	cp.LastRunKey = ""
	// A recurring copy starts paused so duplicating a task cannot silently create
	// a second automatic run at the same time as the original.
	if cp.Mode == 4 && cp.Recurrence.Enabled {
		cp.Paused = true
	}
	app.settings.SavedTasks = append(app.settings.SavedTasks, cp)
	saveSettings()
	appendHistory("SAVE", "Копия: "+cp.Name)
}

func toggleSavedTaskPaused(idx int) {
	if idx < 0 || idx >= len(app.settings.SavedTasks) {
		return
	}
	t := &app.settings.SavedTasks[idx]
	t.Paused = !t.Paused
	state := "Возобновлена"
	if t.Paused {
		state = "Приостановлена"
	}
	saveSettings()
	maintainWakeTimer(time.Now())
	rebuildSavedFilter()
	appendHistory("EDIT", state+" задача: "+t.Name)
	app.status = state + " задача: " + t.Name
}

func deleteSavedTask(idx int) {
	if idx < 0 || idx >= len(app.settings.SavedTasks) {
		return
	}
	name := app.settings.SavedTasks[idx].Name
	app.settings.SavedTasks = append(app.settings.SavedTasks[:idx], app.settings.SavedTasks[idx+1:]...)
	if app.savedScroll >= len(app.settings.SavedTasks) {
		app.savedScroll = 0
		app.savedScrollPx = 0
		app.savedScrollTarget = 0
	}
	saveSettings()
	maintainWakeTimer(time.Now())
	appendHistory("DELETE", name)
}

func openSettingsWindow() {
	app.section = 3
	playUI(openSound)
	startPageAnimation()
	updateInputVisibility()
	invalidate(app.hwnd)
}
func openProcessPicker(mode int) {
	app.pickerAll = listProcessInfos()
	app.processReturnSection = app.section
	app.processPickerMode = mode
	if app.settings.ShowSystemProcesses {
		app.processFilter = 0
	} else {
		app.processFilter = 2
	}
	refreshProcessFilter()
	app.section = 4
	playUI(openSound)
	startPageAnimation()
	updateInputVisibility()
	invalidate(app.hwnd)
}
func toggleSelectedProcess(name string) {
	target := strings.ToLower(name)
	if app.processPickerMode == 2 {
		if strings.EqualFold(app.settings.WatchProcess, name) {
			app.settings.WatchProcess = ""
		} else {
			app.settings.WatchProcess = name
		}
		pSetWindowTextW.Call(app.edits[idWatchProcess], uintptr(unsafe.Pointer(wstr(app.settings.WatchProcess))))
		saveSettings()
		return
	}
	if app.processPickerMode == 3 {
		if strings.EqualFold(app.conditionDraft.Text, name) {
			app.conditionDraft.Text = ""
		} else {
			app.conditionDraft.Text = name
		}
		pSetWindowTextW.Call(app.edits[idCondText], uintptr(unsafe.Pointer(wstr(app.conditionDraft.Text))))
		return
	}
	if app.processPickerMode == 5 {
		if strings.EqualFold(app.savedEditDraft.WatchProcess, name) {
			app.savedEditDraft.WatchProcess = ""
		} else {
			app.savedEditDraft.WatchProcess = name
		}
		pSetWindowTextW.Call(app.edits[idWatchProcess], uintptr(unsafe.Pointer(wstr(app.savedEditDraft.WatchProcess))))
		return
	}
	if app.processPickerMode == 7 {
		if strings.EqualFold(app.stepDraft.Text, name) {
			app.stepDraft.Text = ""
		} else {
			app.stepDraft.Text = name
		}
		pSetWindowTextW.Call(app.edits[idStepText], uintptr(unsafe.Pointer(wstr(app.stepDraft.Text))))
		return
	}
	if app.processPickerMode == 6 {
		list := append([]string(nil), app.stepDraft.Processes...)
		out := make([]string, 0, len(list)+1)
		found := false
		for _, n := range list {
			if strings.EqualFold(n, name) {
				found = true
				continue
			}
			out = append(out, n)
		}
		if !found {
			out = append(out, name)
		}
		app.stepDraft.Processes = out
		return
	}
	list := app.settings.Processes
	if app.processPickerMode == 4 {
		list = app.savedEditDraft.Processes
	}
	if app.processPickerMode == 1 {
		list = app.settings.SafetyProcesses
	}
	out := make([]string, 0, len(list)+1)
	found := false
	for _, n := range list {
		if strings.ToLower(n) == target {
			found = true
			continue
		}
		out = append(out, n)
	}
	if !found {
		out = append(out, name)
	}
	if app.processPickerMode == 1 {
		app.settings.SafetyProcesses = out
	} else if app.processPickerMode == 4 {
		app.savedEditDraft.Processes = out
	} else {
		app.settings.Processes = out
	}
	if app.processPickerMode != 4 {
		saveSettings()
	}
}

func processSelectedInCurrentPicker(name string) bool {
	contains := func(list []string) bool {
		for _, n := range list {
			if strings.EqualFold(n, name) {
				return true
			}
		}
		return false
	}
	switch app.processPickerMode {
	case 1:
		return contains(app.settings.SafetyProcesses)
	case 2:
		return strings.EqualFold(app.settings.WatchProcess, name)
	case 3:
		return strings.EqualFold(app.conditionDraft.Text, name)
	case 4:
		return contains(app.savedEditDraft.Processes)
	case 5:
		return strings.EqualFold(app.savedEditDraft.WatchProcess, name)
	case 6:
		return contains(app.stepDraft.Processes)
	case 7:
		return strings.EqualFold(app.stepDraft.Text, name)
	default:
		return contains(app.settings.Processes)
	}
}
func systemProcessConfirmed(name string) bool {
	for _, n := range app.settings.ConfirmedSystemProcesses {
		if strings.EqualFold(n, name) {
			return true
		}
	}
	return false
}
func rememberConfirmedSystemProcess(name string) {
	if systemProcessConfirmed(name) {
		return
	}
	app.settings.ConfirmedSystemProcesses = append(app.settings.ConfirmedSystemProcesses, name)
}
func forgetConfirmedSystemProcess(name string) {
	out := app.settings.ConfirmedSystemProcesses[:0]
	for _, n := range app.settings.ConfirmedSystemProcesses {
		if !strings.EqualFold(n, name) {
			out = append(out, n)
		}
	}
	app.settings.ConfirmedSystemProcesses = out
}

func playUI(data []byte) {
	if !app.settings.Sounds || len(data) == 0 || app.settings.SoundVolume <= 0 {
		return
	}
	buf := volumeAdjustedSound(data, app.settings.SoundVolume)
	if len(buf) == 0 {
		return
	}
	pPlaySoundW.Call(uintptr(unsafe.Pointer(&buf[0])), 0, SND_ASYNC|SND_MEMORY|SND_NODEFAULT)
}

func setEditTextIfDifferent(id int, value string) {
	h := app.edits[id]
	if h == 0 || getText(h) == value {
		return
	}
	pSetWindowTextW.Call(h, uintptr(unsafe.Pointer(wstr(value))))
}

func repositionVolumeKnob() {
	r := app.volumeTrackRect
	if r.Right <= r.Left {
		return
	}
	vol := clampInt(app.settings.SoundVolume, 0, 100)
	cx := int(r.Left) + int(r.Right-r.Left)*vol/100
	app.volumeKnobRect = RECT{int32(cx - 8), r.Top - 5, int32(cx + 8), r.Bottom + 5}
}

func setVolumeFromX(x int32, preview bool) {
	r := app.volumeTrackRect
	if r.Right <= r.Left {
		return
	}
	px := int(x)
	if px < int(r.Left) {
		px = int(r.Left)
	}
	if px > int(r.Right) {
		px = int(r.Right)
	}
	vol := (px - int(r.Left)) * 100 / max(1, int(r.Right-r.Left))
	if vol < 0 {
		vol = 0
	}
	if vol > 100 {
		vol = 100
	}
	changed := vol != app.settings.SoundVolume
	app.settings.SoundVolume = vol
	repositionVolumeKnob()
	setEditTextIfDifferent(idSoundVolume, strconv.Itoa(vol))
	if changed && preview && app.settings.Sounds && absInt(vol-app.lastPreviewVolume) >= 6 {
		app.lastPreviewVolume = vol
		playUI(clickSound)
	}
	invalidate(app.hwnd)
}

func setTimelineTicksFromX(x int32) {
	r := app.resourceTimelineTicksTrackRect
	if r.Right <= r.Left {
		return
	}
	px := clampInt(int(x), int(r.Left), int(r.Right))
	ticks := 2 + (px-int(r.Left))*10/max(1, int(r.Right-r.Left))
	ticks = clampInt(ticks, 2, 12)
	if ticks == app.settings.ResourceTimelineTicks {
		return
	}
	app.settings.ResourceTimelineTicks = ticks
	setEditTextIfDifferent(idTimelineTicks, strconv.Itoa(ticks))
	layoutControls(app.hwnd)
	invalidate(app.hwnd)
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
func loadEmbeddedIcons() {
	dir := filepath.Join(os.TempDir(), "PowerPilot-assets")
	_ = os.MkdirAll(dir, 0755)
	appPath := filepath.Join(dir, "PowerPilot.ico")
	settingsPath := filepath.Join(dir, "settings.ico")
	_ = os.WriteFile(appPath, appIconData, 0644)
	_ = os.WriteFile(settingsPath, settingsIconData, 0644)
	app.appIcon, _, _ = pLoadImageW.Call(0, uintptr(unsafe.Pointer(wstr(appPath))), IMAGE_ICON, 32, 32, LR_LOADFROMFILE)
	app.settingsIcon, _, _ = pLoadImageW.Call(0, uintptr(unsafe.Pointer(wstr(settingsPath))), IMAGE_ICON, 24, 24, LR_LOADFROMFILE)
	if app.hwnd != 0 && app.appIcon != 0 {
		pSendMessageW.Call(app.hwnd, WM_SETICON, 1, app.appIcon)
		pSendMessageW.Call(app.hwnd, WM_SETICON, 0, app.appIcon)
	}
}

func startSchedule() {
	if app.schedule.active {
		message("PowerPilot", "Сначала отмените текущую задачу.", MB_OK|MB_ICONINFORMATION)
		return
	}
	syncFields()
	graph := ScenarioGraph{}
	if app.settings.TaskKind == 1 {
		syncCurrentGraphFromLegacy()
		state := syncLegacyFromCurrentGraph()
		graph = ensureScenarioGraph(cloneScenarioGraph(state.Graph), state)
		if reason := scenarioGraphValidationError(graph); reason != "" {
			message("Ошибка схемы", reason, MB_OK|MB_ICONERROR)
			return
		}
	}
	now := time.Now()
	conds := append([]AutomationCondition(nil), app.settings.AdvancedConditions...)
	steps := cloneActionSteps(app.settings.ActionSteps)
	closeBefore := false
	processes := []string(nil)
	if app.settings.TaskKind == 0 {
		conds = nil
		steps = nil
		closeBefore = app.settings.CloseBefore
		processes = append([]string(nil), app.settings.Processes...)
	}
	if len(graph.Nodes) > 0 {
		conds = nil
		steps = nil
	}
	s := Schedule{active: true, action: app.selectedAction, mode: app.selectedMode, started: now, runID: newRunID(),
		conditions: conds, triggerLogic: app.settings.TriggerLogic,
		steps: steps, closeBefore: closeBefore, processes: processes, warningSeconds: app.settings.WarningSeconds, graph: graph}
	switch app.selectedMode {
	case 0:
		d := time.Duration(app.settings.DelayHours)*time.Hour + time.Duration(app.settings.DelayMinutes)*time.Minute + time.Duration(app.settings.DelaySeconds)*time.Second
		if d <= 0 {
			message("Ошибка", "Таймер должен быть больше нуля.", MB_OK|MB_ICONERROR)
			return
		}
		s.target = now.Add(d)
		s.total = d
	case 1:
		t, err := parseExact(app.settings.Exact)
		if err != nil {
			message("Ошибка", "Введите дату и время в формате ДД.ММ.ГГГГ ЧЧ:ММ.", MB_OK|MB_ICONERROR)
			return
		}
		if !t.After(now) {
			message("Ошибка", "Выбранное время уже прошло.", MB_OK|MB_ICONERROR)
			return
		}
		s.target = t
		s.total = t.Sub(now)
	case 2:
		if app.settings.IdleMinutes < 1 {
			app.settings.IdleMinutes = 30
		}
		s.idleThreshold = time.Duration(app.settings.IdleMinutes) * time.Second
	case 3:
		pn := strings.TrimSpace(app.settings.WatchProcess)
		if pn == "" {
			message("Ошибка", "Укажите имя процесса.", MB_OK|MB_ICONERROR)
			return
		}
		if !processRunning(pn) {
			message("Ошибка", "Этот процесс сейчас не запущен.", MB_OK|MB_ICONERROR)
			return
		}
		s.watchProcess = pn
	case 4:
		if _, _, err := parseHHMM(app.settings.Recurrence.TimeHHMM); err != nil {
			message("Ошибка", "Введите время расписания в формате ЧЧ:ММ.", MB_OK|MB_ICONERROR)
			return
		}
		t, err := nextOccurrence(app.settings.Recurrence, now)
		if err != nil {
			message("Ошибка", "Не удалось найти следующий запуск расписания.", MB_OK|MB_ICONERROR)
			return
		}
		s.target = t
		s.total = t.Sub(now)
	case 5:
		if app.settings.TaskKind != 1 || (len(graph.Nodes) == 0 && len(conds) == 0) {
			message("Ошибка", "Для запуска по условиям добавьте хотя бы одно условие в продвинутой задаче.", MB_OK|MB_ICONERROR)
			return
		}
	}
	app.schedule = s
	resetConditionRuntimes()
	app.status = "Задача активна"
	app.progress = 0
	appendRunHistory("START", fmt.Sprintf("action=%d mode=%d", s.action, s.mode), s.runID)
	saveSettings()
	playUI(successSound)
	showNotification("PowerPilot", "Задача запущена: "+actionSummary()+" · "+whenSummary())
	tick()
	invalidate(app.hwnd)
}
func cancelSchedule(log bool) {
	if !app.schedule.active {
		return
	}
	runID := app.schedule.runID
	app.schedule = Schedule{}
	app.status = "Задача отменена"
	app.countdown = "00:00:00"
	app.progress = 0
	if log {
		appendRunHistory("CANCEL", "Отменено пользователем", runID)
	}
	invalidate(app.hwnd)
}
func postpone10() {
	if !app.schedule.active {
		return
	}
	if app.schedule.mode == 0 || app.schedule.mode == 1 || app.schedule.mode == 4 {
		app.schedule.target = app.schedule.target.Add(10 * time.Minute)
		app.schedule.total = app.schedule.target.Sub(app.schedule.started)
		app.schedule.warned = false
		app.status = "Отложено на 10 минут"
		appendRunHistory("POSTPONE", "Отложено на 10 минут", app.schedule.runID)
		showNotification("PowerPilot", "Задача отложена на 10 минут.")
	} else {
		message("PowerPilot", "Отсрочка доступна для таймера, точного времени и расписания.", MB_OK|MB_ICONINFORMATION)
	}
}

func tick() {
	now := time.Now()
	maintenance040(now)
	maintainWakeTimer(now)
	// Saved recurring tasks are checked even when no manual task is active.
	if !app.schedule.active {
		autoSavedScheduleTick(now)
	}
	if app.settings.ThemeMode == 2 && (app.lastAutoThemeCheck.IsZero() || now.Sub(app.lastAutoThemeCheck) >= 2*time.Second) {
		app.lastAutoThemeCheck = now
		light := systemUsesLightTheme()
		if light != app.lastAutoThemeLight {
			app.lastAutoThemeLight = light
			applyTheme()
			invalidate(app.hwnd)
		}
	}
	if !app.schedule.active {
		return
	}
	baseReady := false
	switch app.schedule.mode {
	case 0, 1, 4:
		rem := app.schedule.target.Sub(now)
		if rem < 0 {
			rem = 0
		}
		app.countdown = formatDuration(rem)
		if app.schedule.total > 0 {
			app.progress = 1 - rem.Seconds()/app.schedule.total.Seconds()
			if app.progress < 0 {
				app.progress = 0
			}
			if app.progress > 1 {
				app.progress = 1
			}
		}
		warningSeconds := app.schedule.warningSeconds
		if warningSeconds < 0 {
			warningSeconds = 0
		}
		warning := time.Duration(warningSeconds) * time.Second
		if warning > 0 && rem <= warning && !app.schedule.warned {
			app.schedule.warned = true
			app.status = "Скоро выполнится действие — можно отменить"
			showNotification("PowerPilot", fmt.Sprintf("%s через %s", actionSummary(), formatDuration(rem)))
		}
		baseReady = !app.schedule.target.After(now)
	case 2:
		idle := getIdleDuration()
		app.countdown = "Простой: " + formatDuration(idle)
		app.progress = float64(idle) / float64(app.schedule.idleThreshold)
		if app.progress > 1 {
			app.progress = 1
		}
		app.status = "Ожидаю простоя компьютера"
		baseReady = idle >= app.schedule.idleThreshold
	case 3:
		app.countdown = "ОЖИДАНИЕ"
		app.status = "Жду завершения " + app.schedule.watchProcess
		baseReady = !processRunning(app.schedule.watchProcess)
	case 5:
		app.countdown = "УСЛОВИЯ"
		app.status = "Ожидаю выполнения условий"
		baseReady = true
	}
	condReady, detail := evaluateAutomationConditions(app.schedule.conditions)
	if app.schedule.mode != 5 && baseReady && !app.schedule.triggerLogged {
		app.schedule.triggerLogged = true
		appendRunHistory("TRIGGER", "Основной триггер выполнен: "+whenSummary(), app.schedule.runID)
	}
	if len(app.schedule.conditions) > 0 && condReady && !app.schedule.conditionsLogged {
		app.schedule.conditionsLogged = true
		appendRunHistory("CONDITIONS_OK", "Дополнительные условия выполнены", app.schedule.runID)
	}
	ready := baseReady
	if app.schedule.mode == 5 && len(app.schedule.graph.Nodes) > 0 {
		ready = baseReady
	} else if app.schedule.mode == 5 {
		// «По условиям» has no independent base trigger. The condition tree itself is the trigger.
		ready = len(app.schedule.conditions) > 0 && condReady
	} else if len(app.schedule.conditions) > 0 {
		if app.schedule.triggerLogic == logicOR {
			ready = baseReady || condReady
		} else {
			ready = baseReady && condReady
		}
	}
	if ((app.schedule.mode == 5) || (baseReady && app.schedule.triggerLogic == logicAND)) && !condReady && len(app.schedule.conditions) > 0 {
		app.status = "Условия: " + detail
		if detail != app.schedule.lastWaitDetail || app.schedule.lastWaitLog.IsZero() || now.Sub(app.schedule.lastWaitLog) >= 30*time.Second {
			app.schedule.lastWaitDetail = detail
			app.schedule.lastWaitLog = now
			appendRunHistory("WAIT", detail, app.schedule.runID)
		}
	}
	if !baseReady && condReady && len(app.schedule.conditions) > 0 && app.schedule.triggerLogic == logicOR {
		app.status = "Сработало дополнительное условие"
	}
	if ready {
		if ok, reason := checkSafetyRules(); !ok {
			app.status = "Защита: " + reason
			if !app.schedule.safetyNotice {
				app.schedule.safetyNotice = true
				appendRunHistory("SAFETY", reason, app.schedule.runID)
				showNotification("PowerPilot — выполнение отложено", "Защитное правило: "+reason)
				pushAppNotification(notifWarning, "Задача отложена защитой", reason, notifTargetHistory)
			}
			invalidate(app.hwnd)
			return
		}
		executeActionAsync(app.schedule)
	}
	invalidate(app.hwnd)
}

func executeActionAsync(s Schedule) {
	if !app.schedule.active {
		return
	}
	app.schedule.active = false
	app.status = "Выполняю сценарий…"
	execVisualStart040()
	invalidate(app.hwnd)
	go func() {
		finalAction := s.action
		ok := false
		if len(s.graph.Nodes) > 0 {
			finalAction, ok = executeScenarioGraph(s)
		} else {
			ok = executeScenarioSteps(s)
		}
		if !ok {
			execVisualFinal040("error")
			app.mu.Lock()
			app.status = "Сценарий остановлен из-за ошибки"
			app.countdown = "00:00:00"
			app.progress = 0
			app.mu.Unlock()
			name := strings.TrimSpace(s.sourceTaskName)
			if name == "" {
				name = "Текущая задача"
			}
			pushAppNotification(notifError, "Ошибка выполнения задачи", name+" · сценарий остановлен", notifTargetHistory)
			invalidate(app.hwnd)
			return
		}
		appendRunHistory("EXECUTE", fmt.Sprintf("action=%d task=%s", finalAction, s.sourceTaskName), s.runID)
		showNotification("PowerPilot", "Выполняется: "+powerActionName(finalAction))
		taskName := strings.TrimSpace(s.sourceTaskName)
		if taskName == "" {
			taskName = powerActionName(finalAction)
		}
		pushAppNotification(notifSuccess, "Задача выполнена", taskName+" · "+powerActionName(finalAction), notifTargetHistory)
		time.Sleep(180 * time.Millisecond)
		switch finalAction {
		case 0:
			_ = exec.Command("shutdown.exe", "/s", "/t", "0").Start()
		case 1:
			_ = exec.Command("shutdown.exe", "/r", "/t", "0").Start()
		case 2:
			pSetSuspendState.Call(0, 0, 0)
		case 3:
			pSetSuspendState.Call(1, 0, 0)
		case 4:
			// The scenario intentionally ends without a Windows power operation.
		}
		execVisualFinal040("ok")
		app.mu.Lock()
		if finalAction == 4 {
			app.status = "Задача завершена"
		} else {
			app.status = "Действие отправлено Windows"
		}
		app.countdown = "00:00:00"
		app.progress = 0
		app.mu.Unlock()
		invalidate(app.hwnd)
	}()
}

func powerActionName(a int) string {
	names := []string{"Выключение", "Перезагрузка", "Сон", "Гибернация", "Завершить задачу"}
	if a >= 0 && a < len(names) {
		return names[a]
	}
	return "Действие"
}

func syncFields() {
	app.settings.DelayHours = parseInt(getText(app.edits[idDelayHours]), 0)
	app.settings.DelayMinutes = parseInt(getText(app.edits[idDelayMinutes]), 0)
	app.settings.DelaySeconds = parseInt(getText(app.edits[idDelaySeconds]), 0)
	app.settings.Exact = exactFromFields()
	app.settings.IdleMinutes = parseInt(getText(app.edits[idIdleMinutes]), 30)
	app.settings.WatchProcess = strings.TrimSpace(getText(app.edits[idWatchProcess]))
	app.settings.WarningSeconds = parseInt(getText(app.edits[idWarning]), 60)
	if v := strings.TrimSpace(getText(app.edits[idScheduleTime])); v != "" {
		app.settings.Recurrence.TimeHHMM = v
	}
	app.settings.SafetyIdleMinutes = parseInt(getText(app.edits[idSafetyIdle]), max(app.settings.SafetyIdleMinutes, 5))
	app.settings.WakeLeadMinutes = clampInt(parseInt(getText(app.edits[idWakeLead]), max(app.settings.WakeLeadMinutes, 1)), 0, 60)
	saveSettings()
	maintainWakeTimer(time.Now())
}

func listProcessInfos() []ProcessInfo {
	snap, _, _ := pCreateToolhelp32Snapshot.Call(TH32CS_SNAPPROCESS, 0)
	if snap == INVALID_HANDLE_VALUE || snap == 0 {
		return nil
	}
	defer pCloseHandle.Call(snap)
	pe := PROCESSENTRY32{Size: uint32(unsafe.Sizeof(PROCESSENTRY32{}))}
	r, _, _ := pProcess32FirstW.Call(snap, uintptr(unsafe.Pointer(&pe)))
	if r == 0 {
		return nil
	}
	byName := map[string]ProcessInfo{}
	for {
		n := syscall.UTF16ToString(pe.ExeFile[:])
		if n != "" && !strings.EqualFold(n, "PowerPilot.exe") {
			key := strings.ToLower(n)
			path, session0 := processImagePathAndSession(pe.ProcessID)
			isSys := isLikelySystemProcess(pe.ProcessID, n, path, session0)
			if old, ok := byName[key]; ok {
				old.System = old.System || isSys
				if old.ImagePath == "" && path != "" {
					old.ImagePath = path
				}
				byName[key] = old
			} else {
				byName[key] = ProcessInfo{Name: n, PID: pe.ProcessID, System: isSys, ImagePath: path}
			}
		}
		r, _, _ = pProcess32NextW.Call(snap, uintptr(unsafe.Pointer(&pe)))
		if r == 0 {
			break
		}
	}
	out := make([]ProcessInfo, 0, len(byName))
	for _, v := range byName {
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out
}

func processImagePathAndSession(pid uint32) (string, bool) {
	var session uint32
	session0 := false
	if ok, _, _ := pProcessIdToSessionId.Call(uintptr(pid), uintptr(unsafe.Pointer(&session))); ok != 0 {
		session0 = session == 0
	}
	h, _, _ := pOpenProcess.Call(PROCESS_QUERY_LIMITED_INFORMATION, 0, uintptr(pid))
	if h == 0 {
		return "", session0
	}
	defer pCloseHandle.Call(h)
	buf := make([]uint16, 1024)
	sz := uint32(len(buf))
	if ok, _, _ := pQueryFullProcessImageNameW.Call(h, 0, uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&sz))); ok != 0 && sz > 0 {
		return syscall.UTF16ToString(buf[:sz]), session0
	}
	return "", session0
}

func isLikelySystemProcess(pid uint32, name, imagePath string, session0 bool) bool {
	if pid <= 4 || session0 {
		return true
	}
	critical := map[string]bool{
		"system": true, "registry": true, "smss.exe": true, "csrss.exe": true, "wininit.exe": true,
		"services.exe": true, "lsass.exe": true, "svchost.exe": true, "winlogon.exe": true, "dwm.exe": true,
		"fontdrvhost.exe": true, "sihost.exe": true, "taskhostw.exe": true, "audiodg.exe": true,
	}
	if critical[strings.ToLower(strings.TrimSpace(name))] {
		return true
	}
	if imagePath != "" {
		windir := strings.ToLower(strings.TrimRight(os.Getenv("WINDIR"), `\/`))
		lp := strings.ToLower(imagePath)
		if windir != "" && strings.HasPrefix(lp, windir+`\`) {
			return true
		}
	}
	return false
}

func listProcesses() []string {
	infos := listProcessInfos()
	out := make([]string, 0, len(infos))
	for _, info := range infos {
		out = append(out, info.Name)
	}
	return out
}

func isSystemProcessName(name string) bool {
	key := strings.ToLower(strings.TrimSpace(name))
	if app.pickerSystem != nil {
		if v, ok := app.pickerSystem[key]; ok {
			return v
		}
	}
	for _, info := range listProcessInfos() {
		if strings.EqualFold(info.Name, name) {
			return info.System
		}
	}
	return false
}

func refreshProcessFilter() {
	if !app.settings.ShowSystemProcesses {
		app.processFilter = 2
	} else if app.processFilter < 0 || app.processFilter > 2 {
		app.processFilter = 0
	}
	app.pickerItems = app.pickerItems[:0]
	if app.pickerSystem == nil {
		app.pickerSystem = map[string]bool{}
	}
	for k := range app.pickerSystem {
		delete(app.pickerSystem, k)
	}
	for _, info := range app.pickerAll {
		app.pickerSystem[strings.ToLower(info.Name)] = info.System
		show := true
		switch app.processFilter {
		case 1:
			show = info.System && app.settings.ShowSystemProcesses
		case 2:
			show = !info.System
		default:
			show = !info.System || app.settings.ShowSystemProcesses
		}
		if show {
			app.pickerItems = append(app.pickerItems, info.Name)
		}
	}
	app.processScroll, app.processVisible = 0, 0
	app.processScrollPx, app.processScrollTarget = 0, 0
	if app.hwnd != 0 {
		layoutControls(app.hwnd)
	}
}

func processRunning(name string) bool {
	name = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(name)), ".exe") + ".exe"
	for _, n := range listProcesses() {
		if strings.EqualFold(n, name) {
			return true
		}
	}
	return false
}

func closeProcess(name string, wait time.Duration) bool {
	base := normalizeExeName(name)
	if isSystemProcessName(base) {
		if !app.settings.ShowSystemProcesses || !systemProcessConfirmed(base) {
			appendHistory("ERROR", "Системный процесс пропущен защитой: "+base)
			return false
		}
	}
	_ = exec.Command("taskkill.exe", "/IM", base).Run()
	deadline := time.Now().Add(wait)
	for time.Now().Before(deadline) {
		if !processRunning(base) {
			return true
		}
		time.Sleep(250 * time.Millisecond)
	}
	_ = exec.Command("taskkill.exe", "/F", "/IM", base).Run()
	time.Sleep(180 * time.Millisecond)
	return !processRunning(base)
}

func getIdleDuration() time.Duration {
	li := LASTINPUTINFO{CbSize: uint32(unsafe.Sizeof(LASTINPUTINFO{}))}
	r, _, _ := pGetLastInputInfo.Call(uintptr(unsafe.Pointer(&li)))
	if r == 0 {
		return 0
	}
	tick, _, _ := pGetTickCount64.Call()
	ms := uint32(tick) - li.DwTime
	return time.Duration(ms) * time.Millisecond
}

func addTrayIcon() {
	icon := app.appIcon
	if icon == 0 {
		icon, _, _ = pLoadIconW.Call(0, IDI_APPLICATION)
	}
	ni := NOTIFYICONDATA{CbSize: uint32(unsafe.Sizeof(NOTIFYICONDATA{})), HWnd: app.hwnd, UID: 1, UFlags: NIF_MESSAGE | NIF_ICON | NIF_TIP, UCallbackMessage: WM_TRAY, HIcon: icon}
	copy(ni.SzTip[:], syscall.StringToUTF16("PowerPilot"))
	pShellNotifyIconW.Call(NIM_ADD, uintptr(unsafe.Pointer(&ni)))
}
func removeTrayIcon() {
	if app.hwnd == 0 {
		return
	}
	ni := NOTIFYICONDATA{CbSize: uint32(unsafe.Sizeof(NOTIFYICONDATA{})), HWnd: app.hwnd, UID: 1}
	pShellNotifyIconW.Call(NIM_DELETE, uintptr(unsafe.Pointer(&ni)))
}
func showMain() {
	showMainWindowStateAnimated(SW_RESTORE)
	pSetFocus.Call(app.hwnd)
}

func animationWindowDuration() uintptr {
	if app.settings.AnimationMode == 1 {
		return 110
	}
	return 170
}

func windowLongIndex(v int32) uintptr { return uintptr(uint32(v)) }

func prepareWindowOpacity(hwnd uintptr, alpha byte) uintptr {
	old, _, _ := pGetWindowLongPtrW.Call(hwnd, windowLongIndex(GWL_EXSTYLE))
	if old&WS_EX_LAYERED == 0 {
		pSetWindowLongPtrW.Call(hwnd, windowLongIndex(GWL_EXSTYLE), old|WS_EX_LAYERED)
	}
	pSetLayeredWindowAttributes.Call(hwnd, 0, uintptr(alpha), LWA_ALPHA)
	return old
}

func restoreWindowOpacityStyle(hwnd, oldEx uintptr) {
	pSetLayeredWindowAttributes.Call(hwnd, 0, 255, LWA_ALPHA)
	if oldEx&WS_EX_LAYERED == 0 {
		pSetWindowLongPtrW.Call(hwnd, windowLongIndex(GWL_EXSTYLE), oldEx)
	}
}

func animateWindowOpacity(hwnd uintptr, from, to byte, duration time.Duration) {
	if hwnd == 0 {
		return
	}
	if duration <= 0 {
		pSetLayeredWindowAttributes.Call(hwnd, 0, uintptr(to), LWA_ALPHA)
		return
	}
	start := time.Now()
	for {
		t := float64(time.Since(start)) / float64(duration)
		if t > 1 {
			t = 1
		}
		// smootherstep: visually softer at both ends than the old abrupt AnimateWindow fade.
		e := t * t * t * (t*(t*6-15) + 10)
		v := float64(from) + (float64(to)-float64(from))*e
		if v < 0 {
			v = 0
		}
		if v > 255 {
			v = 255
		}
		pSetLayeredWindowAttributes.Call(hwnd, 0, uintptr(byte(v+0.5)), LWA_ALPHA)
		if t >= 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func minimizeMainWindowAnimated() {
	if app.hwnd == 0 {
		return
	}
	// Let DWM keep its native smooth minimize transition. Opacity animation here
	// caused the window to fade before and after the Windows animation.
	pDefWindowProcW.Call(app.hwnd, WM_SYSCOMMAND, SC_MINIMIZE, 0)
}

func showMainWindowStateAnimated(cmd uintptr) {
	if app.hwnd == 0 {
		return
	}
	var sc uintptr
	switch cmd {
	case SW_MAXIMIZE:
		sc = SC_MAXIMIZE
	case SW_RESTORE:
		sc = SC_RESTORE
	default:
		pShowWindow.Call(app.hwnd, cmd)
		return
	}
	if app.settings.AnimationMode == 2 {
		pDefWindowProcW.Call(app.hwnd, WM_SYSCOMMAND, sc, 0)
		applyRoundedWindowCorners(app.hwnd)
		return
	}

	iconic, _, _ := pIsIconic.Call(app.hwnd)
	visible, _, _ := pIsWindowVisible.Call(app.hwnd)
	if sc == SC_MAXIMIZE || (sc == SC_RESTORE && iconic == 0 && visible != 0) {
		// Keep Windows' native geometry transition for maximize / restore-from-maximized.
		pDefWindowProcW.Call(app.hwnd, WM_SYSCOMMAND, sc, 0)
		applyRoundedWindowCorners(app.hwnd)
		layoutControls(app.hwnd)
		invalidate(app.hwnd)
		return
	}

	if visible == 0 {
		pShowWindow.Call(app.hwnd, SW_RESTORE)
	} else {
		pDefWindowProcW.Call(app.hwnd, WM_SYSCOMMAND, sc, 0)
	}
	pUpdateWindow.Call(app.hwnd)
	applyRoundedWindowCorners(app.hwnd)
	layoutControls(app.hwnd)
	invalidate(app.hwnd)
}

func showTrayMenu() {
	createMenu := user32.NewProc("CreatePopupMenu")
	appendMenu := user32.NewProc("AppendMenuW")
	track := user32.NewProc("TrackPopupMenu")
	destroy := user32.NewProc("DestroyMenu")
	menu, _, _ := createMenu.Call()
	appendMenu.Call(menu, MF_STRING, 3001, uintptr(unsafe.Pointer(wstr("Открыть PowerPilot"))))
	appendMenu.Call(menu, MF_SEPARATOR, 0, 0)
	appendMenu.Call(menu, MF_STRING, 3010, uintptr(unsafe.Pointer(wstr("Сон через 30 минут"))))
	appendMenu.Call(menu, MF_STRING, 3011, uintptr(unsafe.Pointer(wstr("Выключить через 1 час"))))

	favMenu, _, _ := createMenu.Call()
	favIndices := []int{}
	for i, t := range app.settings.SavedTasks {
		if !t.Favorite {
			continue
		}
		if len(favIndices) >= 10 {
			break
		}
		cmd := uintptr(3100 + len(favIndices))
		label := t.Name
		if app.schedule.active && app.schedule.sourceTaskID == t.ID {
			label = "Остановить: " + label
		}
		appendMenu.Call(favMenu, MF_STRING, cmd, uintptr(unsafe.Pointer(wstr(truncateUTF16(label, 54)))))
		favIndices = append(favIndices, i)
	}
	if len(favIndices) > 0 {
		appendMenu.Call(menu, MF_POPUP|MF_STRING, favMenu, uintptr(unsafe.Pointer(wstr("Избранные задачи"))))
	}
	if app.schedule.active {
		appendMenu.Call(menu, MF_SEPARATOR, 0, 0)
		appendMenu.Call(menu, MF_STRING, 3002, uintptr(unsafe.Pointer(wstr("Отменить активную задачу"))))
	}
	appendMenu.Call(menu, MF_SEPARATOR, 0, 0)
	appendMenu.Call(menu, MF_STRING, 3003, uintptr(unsafe.Pointer(wstr("Выход"))))
	var pt POINT
	pGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	cmd, _, _ := track.Call(menu, TPM_RIGHTBUTTON|TPM_BOTTOMALIGN|0x0100, uintptr(pt.X), uintptr(pt.Y), 0, app.hwnd, 0)
	destroy.Call(menu)
	switch {
	case cmd == 3001:
		showMain()
	case cmd == 3002:
		cancelSchedule(true)
	case cmd == 3003:
		app.exiting = true
		pSendMessageW.Call(app.hwnd, WM_CLOSE, 0, 0)
	case cmd == 3010:
		startTrayQuickTimer(2, 30*time.Minute, "Сон через 30 минут")
	case cmd == 3011:
		startTrayQuickTimer(0, time.Hour, "Выключение через 1 час")
	case cmd >= 3100 && int(cmd-3100) < len(favIndices):
		startOrStopSavedTask(favIndices[int(cmd-3100)])
	}
}

func startTrayQuickTimer(action int, d time.Duration, name string) {
	if d <= 0 {
		return
	}
	if app.schedule.active {
		showNotification("PowerPilot", "Сначала остановите активную задачу перед запуском быстрого действия.")
		return
	}
	now := time.Now()
	app.schedule = Schedule{active: true, action: action, mode: 0, target: now.Add(d), started: now, total: d, warningSeconds: app.settings.WarningSeconds, sourceTaskName: name, runID: newRunID()}
	app.status = name
	app.progress = 0
	appendRunHistory("START", "tray="+name, app.schedule.runID)
	showNotification("PowerPilot", "Запущено: "+name)
	tick()
	invalidate(app.hwnd)
}

func applyGlobalHotkeys() {
	if app.hwnd == 0 {
		return
	}
	pUnregisterHotKey.Call(app.hwnd, 1)
	pUnregisterHotKey.Call(app.hwnd, 2)
	if !app.settings.GlobalHotkeys {
		return
	}
	mods := uintptr(MOD_CONTROL | MOD_ALT | MOD_NOREPEAT)
	pRegisterHotKey.Call(app.hwnd, 1, mods, uintptr('P'))
	pRegisterHotKey.Call(app.hwnd, 2, mods, uintptr('X'))
}

func settingsDir() string  { d, _ := os.UserConfigDir(); return filepath.Join(d, "PowerPilot") }
func settingsPath() string { return filepath.Join(settingsDir(), "settings.json") }
func historyPath() string  { return filepath.Join(settingsDir(), "history.log") }
func loadSettings() Settings {
	var s Settings
	b, err := os.ReadFile(settingsPath())
	if err == nil {
		_ = json.Unmarshal(b, &s)
		if !strings.Contains(string(b), "\"sounds\"") {
			s.Sounds = true
		}
		if !strings.Contains(string(b), "\"sound_volume\"") {
			s.SoundVolume = 65
		}
		if !strings.Contains(string(b), "\"notifications\"") {
			s.Notifications = true
		}
		if !strings.Contains(string(b), "\"temperature_auto_update\"") {
			s.TemperatureAutoUpdate = true
		}
	}
	return s
}
func saveSettings() {
	_ = os.MkdirAll(settingsDir(), 0755)
	b, _ := json.MarshalIndent(app.settings, "", "  ")
	_ = os.WriteFile(settingsPath(), b, 0644)
}
func appendHistory(kind, detail string) {
	techLog040(kind + " " + detail)
	_ = os.MkdirAll(settingsDir(), 0755)
	f, err := os.OpenFile(historyPath(), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err == nil {
		fmt.Fprintf(f, "%s\t%s\t%s\n", time.Now().Format("2006-01-02 15:04:05"), kind, detail)
		_ = f.Close()
	}
	// Update the History page on the UI thread immediately instead of requiring a tab re-entry.
	if app.hwnd != 0 {
		pPostMessageW.Call(app.hwnd, WM_HISTORY_CHANGED, 0, 0)
	}
}

func setAutoStart(enabled bool) {
	exe, _ := os.Executable()
	key := `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`
	if enabled {
		exec.Command("reg.exe", "add", key, "/v", "PowerPilot", "/t", "REG_SZ", "/d", `"`+exe+`"`, `/f`).Run()
	} else {
		exec.Command("reg.exe", "delete", key, "/v", "PowerPilot", "/f").Run()
	}
}

func splitExactParts(value string) [5]string {
	out := [5]string{"01", "01", strconv.Itoa(time.Now().Year()), "00", "00"}
	if t, err := parseExact(value); err == nil {
		out[0] = fmt.Sprintf("%02d", t.Day())
		out[1] = fmt.Sprintf("%02d", int(t.Month()))
		out[2] = fmt.Sprintf("%04d", t.Year())
		out[3] = fmt.Sprintf("%02d", t.Hour())
		out[4] = fmt.Sprintf("%02d", t.Minute())
	}
	return out
}

func exactFromFields() string {
	day := clampInt(parseInt(getText(app.edits[idExactDay]), 1), 1, 31)
	month := clampInt(parseInt(getText(app.edits[idExactMonth]), 1), 1, 12)
	year := clampInt(parseInt(getText(app.edits[idExactYear]), time.Now().Year()), 2000, 9999)
	hour := clampInt(parseInt(getText(app.edits[idExactHour]), 0), 0, 23)
	minute := clampInt(parseInt(getText(app.edits[idExactMinute]), 0), 0, 59)
	return fmt.Sprintf("%02d.%02d.%04d %02d:%02d", day, month, year, hour, minute)
}

func setExactFields(value string) {
	parts := splitExactParts(value)
	ids := []int{idExactDay, idExactMonth, idExactYear, idExactHour, idExactMinute}
	for i, id := range ids {
		if h := app.edits[id]; h != 0 {
			pSetWindowTextW.Call(h, uintptr(unsafe.Pointer(wstr(parts[i]))))
		}
	}
	if h := app.edits[idExact]; h != 0 {
		pSetWindowTextW.Call(h, uintptr(unsafe.Pointer(wstr(value))))
	}
}

func clampFloat(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func maxFloat(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func parseExact(s string) (time.Time, error) {
	loc := time.Local
	layouts := []string{"02.01.2006 15:04", "2.1.2006 15:04", "02.01.2006 15:04:05"}
	for _, l := range layouts {
		if t, e := time.ParseInLocation(l, s, loc); e == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("bad")
}
func formatDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	total := int(d.Seconds())
	h := total / 3600
	m := (total % 3600) / 60
	s := total % 60
	return fmt.Sprintf("%02d:%02d:%02d", h, m, s)
}
func (a *App) statusOrDefault() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.status != "" {
		return a.status
	}
	return "Нет активной задачи"
}
func (a *App) countdownOrDefault() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.countdown != "" {
		return a.countdown
	}
	return "00:00:00"
}

func move(h uintptr, x, y, w, hgt int) {
	if h != 0 {
		sc := uiScaleFactor040()
		px := int(float64(x+editAnimOffsetX)*sc + .5)
		py := int(float64(y+editAnimOffsetY)*sc + .5)
		pw := max(1, int(float64(w)*sc+.5))
		ph := max(1, int(float64(hgt)*sc+.5))
		repaint := uintptr(1)
		if suppressEditVisibilityDuringLayout {
			repaint = 0
		}
		pMoveWindow.Call(h, uintptr(px), uintptr(py), uintptr(pw), uintptr(ph), repaint)
	}
}
func getText(h uintptr) string {
	n, _, _ := pGetWindowTextLen.Call(h)
	buf := make([]uint16, int(n)+1)
	pGetWindowTextW.Call(h, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return syscall.UTF16ToString(buf)
}
func getCheck(h uintptr) bool { r, _, _ := pSendMessageW.Call(h, 0x00F0, 0, 0); return r == 1 }
func parseInt(s string, def int) int {
	v, e := strconv.Atoi(strings.TrimSpace(s))
	if e != nil || v < 0 {
		return def
	}
	return v
}
func fileExists(p string) bool { _, e := os.Stat(p); return e == nil }
func message(title, text string, flags uintptr) int {
	r, _, _ := pMessageBoxW.Call(app.hwnd, uintptr(unsafe.Pointer(wstr(text))), uintptr(unsafe.Pointer(wstr(title))), flags)
	return int(r)
}
func invalidate(hwnd uintptr) { pInvalidateRect.Call(hwnd, 0, 0) }
func solid(c uint32) uintptr  { b, _, _ := pCreateSolidBrush.Call(uintptr(c)); return b }
func fill(hdc uintptr, r RECT, c uint32) {
	if ui2d.active {
		d2dFillRect(r, c)
		return
	}
	b := solid(c)
	pFillRect.Call(hdc, uintptr(unsafe.Pointer(&r)), b)
	pDeleteObject.Call(b)
}
func roundFill(hdc uintptr, r RECT, c uint32, radius int32) {
	if r.Right <= r.Left || r.Bottom <= r.Top {
		return
	}
	if ui2d.active {
		if app.settings.SurfaceStyle == 4 {
			pc, bc := surfacePanelColor(), surfaceButtonColor()
			dp, db := colorDistance(c, pc), colorDistance(c, bc)
			// Large areas use a stable frosted companion material; Liquid Glass itself is
			// reserved for button/input-like interaction surfaces.
			if dp <= db && dp <= 90 {
				d2dDrawFrostedPanel(r, radius)
				if c != pc {
					d2dFillRoundedOpacity(r, c, radius, .08)
				}
				return
			}
			if db < dp && db <= 105 {
				d2dDrawLiquidGlass(r, radius, drawingInteractiveSurface)
				if c != bc {
					d2dFillRoundedOpacity(r, c, radius, .09)
				}
				return
			}
		}
		d2dFillRounded(r, c, radius)
		return
	}
	b := solid(c)
	old, _, _ := pSelectObject.Call(hdc, b)
	pen, _, _ := pCreatePen.Call(5, 1, uintptr(c))
	oldPen, _, _ := pSelectObject.Call(hdc, pen)
	pRoundRect.Call(hdc, uintptr(r.Left), uintptr(r.Top), uintptr(r.Right), uintptr(r.Bottom), uintptr(radius*2), uintptr(radius*2))
	pSelectObject.Call(hdc, old)
	pSelectObject.Call(hdc, oldPen)
	pDeleteObject.Call(b)
	pDeleteObject.Call(pen)
}
func createFont(size int, weight int) uintptr {
	key := size*1000 + weight
	if h := fontCache[key]; h != 0 {
		return h
	}
	h, _, _ := pCreateFontW.Call(uintptr(int64(-size)), 0, 0, 0, uintptr(weight), 0, 0, 0, 1, 0, 0, 5, 0, uintptr(unsafe.Pointer(wstr("Segoe UI"))))
	fontCache[key] = h
	return h
}
func drawText(hdc uintptr, text string, x, y, w, h, size, weight int, color uint32, flags uint32) {
	if ui2d.active {
		d2dDrawText(text, x, y, w, h, size, weight, color, flags)
		return
	}
	font := createFont(size, weight)
	old, _, _ := pSelectObject.Call(hdc, font)
	pSetBkMode.Call(hdc, TRANSPARENT)
	pSetTextColor.Call(hdc, uintptr(color))
	r := RECT{int32(x), int32(y), int32(x + w), int32(y + h)}
	u := syscall.StringToUTF16(text)
	pDrawTextW.Call(hdc, uintptr(unsafe.Pointer(&u[0])), ^uintptr(0), uintptr(unsafe.Pointer(&r)), uintptr(flags))
	pSelectObject.Call(hdc, old)
}
func normalizeExeName(name string) string {
	n := strings.TrimSpace(name)
	if len(n) >= 4 && strings.EqualFold(n[len(n)-4:], ".exe") {
		return n
	}
	return n + ".exe"
}
func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}
func colorDistance(a, b uint32) int {
	ar, ag, ab := int(a&0xff), int((a>>8)&0xff), int((a>>16)&0xff)
	br, bg, bb := int(b&0xff), int((b>>8)&0xff), int((b>>16)&0xff)
	dr, dg, db := ar-br, ag-bg, ab-bb
	if dr < 0 {
		dr = -dr
	}
	if dg < 0 {
		dg = -dg
	}
	if db < 0 {
		db = -db
	}
	return dr + dg + db
}
func blendColor(a, b uint32, t float64) uint32 {
	if t < 0 {
		t = 0
	}
	if t > 1 {
		t = 1
	}
	ar := float64(a & 0xff)
	ag := float64((a >> 8) & 0xff)
	ab := float64((a >> 16) & 0xff)
	br := float64(b & 0xff)
	bg := float64((b >> 8) & 0xff)
	bb := float64((b >> 16) & 0xff)
	return rgb(byte(ar+(br-ar)*t), byte(ag+(bg-ag)*t), byte(ab+(bb-ab)*t))
}
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func maxRectBottom(rects []RECT) int32 {
	var out int32
	for _, r := range rects {
		if r.Bottom > out {
			out = r.Bottom
		}
	}
	return out
}
