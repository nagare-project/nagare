//go:build windows

package desktop

// 桌面窗口用到的 Win32 接口：全部经 golang.org/x/sys/windows 的延迟加载，CGO_ENABLED=0 照样编得出来。

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32   = windows.NewLazySystemDLL("user32.dll")
	gdi32    = windows.NewLazySystemDLL("gdi32.dll")
	shell32  = windows.NewLazySystemDLL("shell32.dll")
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")
	dwmapi   = windows.NewLazySystemDLL("dwmapi.dll")

	pDwmSetWindowAttribute = dwmapi.NewProc("DwmSetWindowAttribute")

	pRegisterClassExW         = user32.NewProc("RegisterClassExW")
	pCreateWindowExW          = user32.NewProc("CreateWindowExW")
	pDefWindowProcW           = user32.NewProc("DefWindowProcW")
	pDestroyWindow            = user32.NewProc("DestroyWindow")
	pShowWindow               = user32.NewProc("ShowWindow")
	pIsWindowVisible          = user32.NewProc("IsWindowVisible")
	pIsWindow                 = user32.NewProc("IsWindow")
	pIsWindowEnabled          = user32.NewProc("IsWindowEnabled")
	pIsIconic                 = user32.NewProc("IsIconic")
	pIsZoomed                 = user32.NewProc("IsZoomed")
	pIsHungAppWindow          = user32.NewProc("IsHungAppWindow")
	pGetMessageW              = user32.NewProc("GetMessageW")
	pPeekMessageW             = user32.NewProc("PeekMessageW")
	pTranslateMessage         = user32.NewProc("TranslateMessage")
	pDispatchMessageW         = user32.NewProc("DispatchMessageW")
	pPostMessageW             = user32.NewProc("PostMessageW")
	pPostQuitMessage          = user32.NewProc("PostQuitMessage")
	pMsgWaitForMultipleObj    = user32.NewProc("MsgWaitForMultipleObjects")
	pGetClientRect            = user32.NewProc("GetClientRect")
	pGetWindowRect            = user32.NewProc("GetWindowRect")
	pSetWindowPos             = user32.NewProc("SetWindowPos")
	pMoveWindow               = user32.NewProc("MoveWindow")
	pSetFocus                 = user32.NewProc("SetFocus")
	pGetFocus                 = user32.NewProc("GetFocus")
	pFindWindowExW            = user32.NewProc("FindWindowExW")
	pEnableWindow             = user32.NewProc("EnableWindow")
	pLoadCursorW              = user32.NewProc("LoadCursorW")
	pSetCursor                = user32.NewProc("SetCursor")
	pLoadImageW               = user32.NewProc("LoadImageW")
	pGetWindowLongPtrW        = user32.NewProc("GetWindowLongPtrW")
	pSetWindowLongPtrW        = user32.NewProc("SetWindowLongPtrW")
	pGetWindowPlacement       = user32.NewProc("GetWindowPlacement")
	pSetWindowPlacement       = user32.NewProc("SetWindowPlacement")
	pMonitorFromWindow        = user32.NewProc("MonitorFromWindow")
	pGetMonitorInfoW          = user32.NewProc("GetMonitorInfoW")
	pSetWindowTextW           = user32.NewProc("SetWindowTextW")
	pGetForegroundWindow      = user32.NewProc("GetForegroundWindow")
	pSetForegroundWindow      = user32.NewProc("SetForegroundWindow")
	pAllowSetForegroundWindow = user32.NewProc("AllowSetForegroundWindow")
	pBringWindowToTop         = user32.NewProc("BringWindowToTop")
	pFlashWindowEx            = user32.NewProc("FlashWindowEx")
	pGetWindowThreadProcessId = user32.NewProc("GetWindowThreadProcessId")
	pGetDpiForWindow          = user32.NewProc("GetDpiForWindow")
	pTrackMouseEvent          = user32.NewProc("TrackMouseEvent")
	pGetKeyState              = user32.NewProc("GetKeyState")
	pSetCapture               = user32.NewProc("SetCapture")
	pReleaseCapture           = user32.NewProc("ReleaseCapture")
	pGetStockObject           = gdi32.NewProc("GetStockObject")
	pCreateSolidBrush         = gdi32.NewProc("CreateSolidBrush")
	pShellExecuteW            = shell32.NewProc("ShellExecuteW")
	pGetCurrentThreadId       = kernel32.NewProc("GetCurrentThreadId")
	pInSendMessage            = user32.NewProc("InSendMessage")
)

const (
	wsOverlappedWindow = 0x00CF0000
	wsPopup            = 0x80000000
	wsChild            = 0x40000000
	wsVisible          = 0x10000000
	wsClipChildren     = 0x02000000
	wsClipSiblings     = 0x04000000
	cwUseDefault       = 0x80000000

	swHide            = 0
	swShowNormal      = 1
	swShowMinimized   = 2
	swMaximize        = 3
	swShowNoActivate  = 4
	swShow            = 5
	swMinimize        = 6
	swRestore         = 9
	swShowDefault     = 10
	wmDestroy         = 0x0002
	wmMove            = 0x0003
	wmSize            = 0x0005
	wmActivate        = 0x0006
	wmSetFocus        = 0x0007
	wmClose           = 0x0010
	wmQueryEndSession = 0x0011
	wmQuit            = 0x0012
	wmEndSession      = 0x0016
	wmSetCursor       = 0x0020
	wmGetMinMaxInfo   = 0x0024
	wmKeyDown         = 0x0100
	wmKeyUp           = 0x0101
	wmChar            = 0x0102
	wmSysKeyDown      = 0x0104
	wmSysKeyUp        = 0x0105
	wmSysCommand      = 0x0112
	wmTimer           = 0x0113
	wmMouseMove       = 0x0200
	wmLButtonDown     = 0x0201
	wmLButtonUp       = 0x0202
	wmLButtonDblClk   = 0x0203
	wmRButtonDown     = 0x0204
	wmRButtonUp       = 0x0205
	wmMButtonDown     = 0x0207
	wmMButtonUp       = 0x0208
	wmMouseWheel      = 0x020A
	wmXButtonDown     = 0x020B
	wmXButtonUp       = 0x020C
	wmMouseHWheel     = 0x020E
	wmMouseLeave      = 0x02A3
	wmDpiChanged      = 0x02E0
	wmAppCommand      = 0x0319
	wmApp             = 0x8000

	sizeRestored  = 0
	sizeMinimized = 1
	sizeMaximized = 2

	scKeyMenu  = 0xF100
	scMinimize = 0xF020
	scMaximize = 0xF030
	scRestore  = 0xF120

	htClient = 1

	swpNoSize         = 0x0001
	swpNoMove         = 0x0002
	swpNoZOrder       = 0x0004
	swpNoActivate     = 0x0010
	swpFrameChanged   = 0x0020
	swpShowWindow     = 0x0040
	swpNoOwnerZOrder  = 0x0200
	swpAsyncWindowPos = 0x4000

	monitorDefaultToNearest = 2

	pmRemove   = 0x0001
	qsAllInput = 0x04FF

	waitObject0  = 0x00000000
	waitTimeout  = 0x00000102
	infiniteWait = 0xFFFFFFFF

	idcArrow     = 32512
	imageIcon    = 1
	lrDefaultSz  = 0x0040
	lrShared     = 0x8000
	asfwAny      = ^uintptr(0) // ASFW_ANY = (DWORD)-1
	flashwAll    = 0x00000003
	flashwTimerN = 0x0000000C // FLASHW_TIMERNOFG

	tmeLeave = 0x00000002
)

var (
	gwlStyle      = ^uintptr(15) // GWL_STYLE = -16
	hwndTopmost   = ^uintptr(0)  // HWND_TOPMOST = (HWND)-1
	hwndNoTopmost = ^uintptr(1)  // HWND_NOTOPMOST = (HWND)-2
	hwndMessage   = ^uintptr(2)  // HWND_MESSAGE = (HWND)-3
)

type wndClassEx struct {
	cbSize        uint32
	style         uint32
	lpfnWndProc   uintptr
	cbClsExtra    int32
	cbWndExtra    int32
	hInstance     windows.Handle
	hIcon         windows.Handle
	hCursor       windows.Handle
	hbrBackground windows.Handle
	lpszMenuName  *uint16
	lpszClassName *uint16
	hIconSm       windows.Handle
}

type point struct{ X, Y int32 }

type rect struct{ Left, Top, Right, Bottom int32 }

func (r rect) width() int32  { return r.Right - r.Left }
func (r rect) height() int32 { return r.Bottom - r.Top }

type msg struct {
	Hwnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      point
	private uint32
}

type windowPlacement struct {
	Length           uint32
	Flags            uint32
	ShowCmd          uint32
	PtMinPosition    point
	PtMaxPosition    point
	RcNormalPosition rect
	RcDevice         rect
}

type monitorInfo struct {
	CbSize    uint32
	RcMonitor rect
	RcWork    rect
	DwFlags   uint32
}

type minMaxInfo struct {
	PtReserved     point
	PtMaxSize      point
	PtMaxPosition  point
	PtMinTrackSize point
	PtMaxTrackSize point
}

type flashInfo struct {
	CbSize    uint32
	Hwnd      uintptr
	DwFlags   uint32
	UCount    uint32
	DwTimeout uint32
}

type trackMouseEvent struct {
	CbSize      uint32
	DwFlags     uint32
	HwndTrack   uintptr
	DwHoverTime uint32
}

func utf16Ptr(s string) *uint16 {
	p, err := windows.UTF16PtrFromString(s)
	if err != nil {
		// 只用于本文件里写死的类名、标题这类常量；出错就是代码写错了
		p, _ = windows.UTF16PtrFromString("?")
	}
	return p
}

func moduleHandle() windows.Handle {
	var h windows.Handle
	_ = windows.GetModuleHandleEx(0, nil, &h)
	return h
}

func clientRect(hwnd uintptr) rect {
	var r rect
	pGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&r)))
	return r
}

func windowRect(hwnd uintptr) rect {
	var r rect
	pGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&r)))
	return r
}

func boolCall(p *windows.LazyProc, args ...uintptr) bool {
	r, _, _ := p.Call(args...)
	return r != 0
}

func postMessage(hwnd uintptr, m uint32, w, l uintptr) bool {
	return boolCall(pPostMessageW, hwnd, uintptr(m), w, l)
}

func defWindowProc(hwnd uintptr, m uint32, w, l uintptr) uintptr {
	r, _, _ := pDefWindowProcW.Call(hwnd, uintptr(m), w, l)
	return r
}

// windowPID 是窗口所属进程的 PID。
func windowPID(hwnd uintptr) uint32 {
	var pid uint32
	pGetWindowThreadProcessId.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	return pid
}

func setWindowText(hwnd uintptr, s string) {
	p, err := windows.UTF16PtrFromString(s)
	if err != nil {
		return
	}
	pSetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(p)))
}

// openExternal 用系统默认程序打开 url（只给 http / https / mailto 用，见 decideNav）。
func openExternal(url string) {
	p, err := windows.UTF16PtrFromString(url)
	if err != nil {
		return
	}
	pShellExecuteW.Call(0, uintptr(unsafe.Pointer(utf16Ptr("open"))), uintptr(unsafe.Pointer(p)), 0, 0, swShowNormal)
}

func loword(v uintptr) int32 { return int32(int16(v & 0xFFFF)) }
func hiword(v uintptr) int32 { return int32(int16((v >> 16) & 0xFFFF)) }
