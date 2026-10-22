//go:build windows

package desktop

// 顶层窗口与两个子窗口（网页层 / 画面层）：建、拆、排版、两层切换、全屏、提到前面、关窗口。全部在界面线程上。

import (
	"fmt"
	"log"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	classTop      = "NagareDesktopWindow"
	classWebHost  = "NagareDesktopWebHost"
	classVideo    = "NagareDesktopVideoHost"
	classDispatch = "NagareDesktopDispatch"
	windowTitle   = "Nagare"

	// 窗口的最小尺寸（逻辑像素，按屏幕缩放换算）：再小 Lua 界面的窄窗口模式也排不下
	minWidth, minHeight = 640, 400
	// 第一次打开时的尺寸（逻辑像素），放不下就缩到工作区的九成
	defaultWidth, defaultHeight = 1280, 800
	// 网页的近黑底（与 macOS 窗口、网页 body 的底色一致），免得建网页层时白屏一闪
	bgR, bgG, bgB = 0x0B, 0x0B, 0x0B
)

var (
	topProcCb   = windows.NewCallback(topProc)
	hostProcCb  = windows.NewCallback(hostProc)
	videoProcCb = windows.NewCallback(videoProc)
)

// fullscreenState 是宿主的无边框全屏：进去时记下窗口样式与位置，出来时原样还回去。
type fullscreenState struct {
	on        bool
	style     uintptr
	placement windowPlacement
}

func registerClass(name string, proc uintptr, brush uintptr, icon windows.Handle) error {
	cursor, _, _ := pLoadCursorW.Call(0, idcArrow)
	wc := wndClassEx{
		cbSize:      uint32(unsafe.Sizeof(wndClassEx{})),
		lpfnWndProc: proc,
		hInstance:   moduleHandle(),
		hCursor:     windows.Handle(cursor),
		// 不带 CS_DBLCLKS：mpv 自己的窗口类也没有，双击由 mpv 的 input.c 用两次按下合成（IPC 转发那条退路同理）
		hbrBackground: windows.Handle(brush),
		lpszClassName: utf16Ptr(name),
		hIcon:         icon,
		hIconSm:       icon,
	}
	if r, _, err := pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); r == 0 && err != windows.ERROR_CLASS_ALREADY_EXISTS {
		return fmt.Errorf("注册窗口类 %s 失败：%w", name, err)
	}
	return nil
}

func createWindow(exStyle uintptr, class, title string, style uintptr, x, y, w, h, parent uintptr) (uintptr, error) {
	hwnd, _, err := pCreateWindowExW.Call(exStyle, uintptr(unsafe.Pointer(utf16Ptr(class))),
		uintptr(unsafe.Pointer(utf16Ptr(title))), style, x, y, w, h, parent, 0, uintptr(moduleHandle()), 0)
	if hwnd == 0 {
		return 0, fmt.Errorf("建窗口 %s 失败：%w", class, err)
	}
	return hwnd, nil
}

// appIcon 是 exe 里的图标（rsrc_windows_*.syso，go-winres 把它命名为 APP）；没有就用系统默认的。
func appIcon() windows.Handle {
	h, _, _ := pLoadImageW.Call(uintptr(moduleHandle()), uintptr(unsafe.Pointer(utf16Ptr("APP"))), imageIcon, 0, 0, lrDefaultSz|lrShared)
	return windows.Handle(h)
}

// createWindows 建消息窗口、顶层窗口（先藏着）与两个子窗口。网页层的 WebView2 到第一次显示时才建（ensureWeb）。
func (a *app) createWindows() error {
	black, _, _ := pGetStockObject.Call(4) // BLACK_BRUSH
	dark, _, _ := pCreateSolidBrush.Call(uintptr(bgR) | uintptr(bgG)<<8 | uintptr(bgB)<<16)
	icon := appIcon()
	for _, c := range []struct {
		name  string
		proc  uintptr
		brush uintptr
		icon  windows.Handle
	}{
		{classDispatch, dispatchProcCb, 0, 0},
		{classTop, topProcCb, dark, icon},
		{classWebHost, hostProcCb, dark, 0},
		{classVideo, videoProcCb, black, 0},
	} {
		if err := registerClass(c.name, c.proc, c.brush, c.icon); err != nil {
			return err
		}
	}
	var err error
	if a.dispatch, err = createWindow(0, classDispatch, "", 0, 0, 0, 0, 0, hwndMessage); err != nil {
		return err
	}
	if a.top, err = createWindow(0, classTop, windowTitle, wsOverlappedWindow|wsClipChildren,
		cwUseDefault, cwUseDefault, defaultWidth, defaultHeight, 0); err != nil {
		return err
	}
	if a.webHost, err = createWindow(0, classWebHost, "", wsChild|wsVisible|wsClipChildren|wsClipSiblings, 0, 0, 0, 0, a.top); err != nil {
		return err
	}
	if a.videoHost, err = createWindow(0, classVideo, "", wsChild|wsClipChildren|wsClipSiblings, 0, 0, 0, 0, a.top); err != nil {
		return err
	}
	a.restorePlacement()
	a.layout()
	return nil
}

func (a *app) destroyWindows() {
	for _, h := range []*uintptr{&a.top, &a.dispatch} { // 子窗口随顶层窗口一起销毁
		if *h != 0 {
			pDestroyWindow.Call(*h)
			*h = 0
		}
	}
	a.webHost, a.videoHost = 0, 0
}

// layout 让两个子窗口都铺满客户区（同一时刻只露一个），mpv 的子窗口也跟着改：它自己靠跨进程的 WinEvent 钩子
// 跟随父窗口（mpv 的注释说「不如窗口钩子平滑」），宿主直接改一次省掉那点延迟（异步，不等 mpv 的线程）。
func (a *app) layout() {
	if a.top == 0 {
		return
	}
	r := clientRect(a.top)
	w, h := uintptr(r.width()), uintptr(r.height())
	pMoveWindow.Call(a.webHost, 0, 0, w, h, 1)
	pMoveWindow.Call(a.videoHost, 0, 0, w, h, 1)
	if a.web != nil {
		a.web.resize()
	}
	if s := a.session; s != nil {
		if child := s.child.Load(); child != 0 {
			pSetWindowPos.Call(child, 0, 0, 0, w, h, swpNoZOrder|swpNoActivate|swpNoOwnerZOrder|swpAsyncWindowPos)
		}
	}
}

// show 是 Show / Open 在界面线程上的部分。
func (a *app) show(url string) {
	if a.quitting || a.top == 0 {
		return
	}
	if a.webFailed != "" && a.mode == modeWeb {
		a.fallback(url, a.webFailed)
		return
	}
	a.ensureWeb()
	if url != "" {
		a.navigate(url)
	}
	a.bringToFront()
}

// bringToFront 显示窗口并尽量放到前台。后台进程（浏览器里点了播放、托盘菜单）不一定拿得到前台：拿不到时
// 先把它摆到最上层（不抢焦点），再让任务栏按钮闪一下。
func (a *app) bringToFront() {
	if !a.shownOnce {
		a.shownOnce = true
		cmd := uintptr(swShow)
		if a.startMaximized {
			cmd = swMaximize
		}
		pShowWindow.Call(a.top, cmd)
	} else if boolCall(pIsIconic, a.top) {
		pShowWindow.Call(a.top, swRestore)
	} else if !boolCall(pIsWindowVisible, a.top) {
		pShowWindow.Call(a.top, swShow)
	}
	if boolCall(pSetForegroundWindow, a.top) {
		return
	}
	flags := uintptr(swpNoMove | swpNoSize | swpShowWindow)
	pSetWindowPos.Call(a.top, hwndTopmost, 0, 0, 0, 0, flags)
	if !a.topmost {
		pSetWindowPos.Call(a.top, hwndNoTopmost, 0, 0, 0, 0, flags)
	}
	fi := flashInfo{CbSize: uint32(unsafe.Sizeof(flashInfo{})), Hwnd: a.top, DwFlags: flashwAll | flashwTimerN}
	pFlashWindowEx.Call(uintptr(unsafe.Pointer(&fi)))
}

// ───────────────────────── 两层切换 ─────────────────────────

func (a *app) showVideo() {
	a.mode = modeVideo
	if a.web != nil {
		a.web.setVisible(false)
	}
	pShowWindow.Call(a.webHost, swHide)
	pShowWindow.Call(a.videoHost, swShow)
	a.layout()
	a.focusVideo()
}

func (a *app) showWeb() {
	if a.fs.on {
		a.setFullscreen(false)
	}
	a.setTopmost(false) // 「窗口置顶」只管播放的时候
	setWindowText(a.top, windowTitle)
	a.mode = modeWeb
	pShowWindow.Call(a.videoHost, swHide)
	if a.webFailed != "" {
		// 网页层在播放的时候坏了：窗口里没东西可看，藏起来；窗口本来露着（不是用户关的）才把界面开到浏览器里
		visible := boolCall(pIsWindowVisible, a.top)
		pShowWindow.Call(a.top, swHide)
		if visible && !a.quitting {
			a.fallback("", a.webFailed)
		}
		return
	}
	pShowWindow.Call(a.webHost, swShow)
	if a.web != nil {
		a.web.setVisible(true)
		a.web.focus()
	}
}

// focusVideo 把键盘焦点给 mpv 的子窗口。跨进程的父子窗口输入队列是连着的（Raymond Chen 2013-04-12），
// 所以 SetFocus 别的进程的窗口在这里是成立的。mpv 卡住时不碰它（SetFocus 要等它处理消息）。
func (a *app) focusVideo() {
	if s := a.session; s != nil && !ipcInput {
		if child := s.child.Load(); child != 0 && !boolCall(pIsHungAppWindow, child) {
			pSetFocus.Call(child)
			return
		}
	}
	pSetFocus.Call(a.videoHost)
}

// ───────────────────────── 全屏、置顶 ─────────────────────────

// setFullscreen 是宿主的无边框全屏（--wid 下 mpv 自己的全屏不起作用）：去掉标题栏与边框、铺满所在的显示器。
func (a *app) setFullscreen(on bool) {
	if on == a.fs.on || a.top == 0 {
		return
	}
	if on {
		a.fs.placement = windowPlacement{Length: uint32(unsafe.Sizeof(windowPlacement{}))}
		pGetWindowPlacement.Call(a.top, uintptr(unsafe.Pointer(&a.fs.placement)))
		a.fs.style, _, _ = pGetWindowLongPtrW.Call(a.top, gwlStyle)
		pSetWindowLongPtrW.Call(a.top, gwlStyle, a.fs.style&^wsOverlappedWindow|wsPopup)
		mon, _, _ := pMonitorFromWindow.Call(a.top, monitorDefaultToNearest)
		mi := monitorInfo{CbSize: uint32(unsafe.Sizeof(monitorInfo{}))}
		pGetMonitorInfoW.Call(mon, uintptr(unsafe.Pointer(&mi)))
		r := mi.RcMonitor
		a.fs.on = true
		pSetWindowPos.Call(a.top, 0, uintptr(r.Left), uintptr(r.Top), uintptr(r.width()), uintptr(r.height()),
			swpNoZOrder|swpNoOwnerZOrder|swpFrameChanged|swpShowWindow)
	} else {
		a.fs.on = false
		pSetWindowLongPtrW.Call(a.top, gwlStyle, a.fs.style)
		pSetWindowPlacement.Call(a.top, uintptr(unsafe.Pointer(&a.fs.placement)))
		pSetWindowPos.Call(a.top, 0, 0, 0, 0, 0, swpNoMove|swpNoSize|swpNoZOrder|swpNoOwnerZOrder|swpFrameChanged)
	}
	a.hostChanged()
}

func (a *app) setTopmost(on bool) {
	if on == a.topmost || a.top == 0 {
		return
	}
	a.topmost = on
	after := hwndNoTopmost
	if on {
		after = hwndTopmost
	}
	pSetWindowPos.Call(a.top, after, 0, 0, 0, 0, swpNoMove|swpNoSize|swpNoActivate)
	a.hostChanged()
}

// hostState 是顶层窗口此刻的状态（给 winstate 的规则用）。
func (a *app) hostState() hostWindow {
	return hostWindow{
		Fullscreen: a.fs.on,
		Maximized:  !a.fs.on && boolCall(pIsZoomed, a.top),
		Minimized:  boolCall(pIsIconic, a.top),
		Topmost:    a.topmost,
	}
}

// hostChanged：顶层窗口的状态变了（用户点了标题栏、Win+↑、宿主执行了 mpv 的请求），写回 mpv。
func (a *app) hostChanged() {
	if s := a.session; s != nil && s.sync != nil {
		s.write(s.sync.fromHost(a.hostState()))
	}
}

// apply 执行 winstate 给出的动作。
func (a *app) apply(acts []winAction) {
	for _, act := range acts {
		switch act {
		case winEnterFullscreen:
			if a.mode == modeVideo {
				a.setFullscreen(true)
			}
		case winExitFullscreen:
			a.setFullscreen(false)
		case winMaximize:
			pShowWindow.Call(a.top, swMaximize)
		case winMinimize:
			pShowWindow.Call(a.top, swMinimize)
		case winTopmostOn:
			a.setTopmost(true)
		case winTopmostOff:
			a.setTopmost(false)
		}
	}
}

// ───────────────────────── 关窗口、退出 ─────────────────────────

// closeToBackground 是关窗口（标题栏的 ×、Alt+F4）：播放中就停播（与命令行下关掉 mpv 窗口一样发 quit：
// 进度落盘、OnSessionEnd 都跟着 mpv 的退出走），然后藏起窗口 —— nagare 照旧在后台，托盘再打开。
func (a *app) closeToBackground() {
	if s := a.session; s != nil {
		log.Print("desktop: 关窗口：停止播放")
		s.quit()
	}
	if a.fs.on {
		a.setFullscreen(false)
	}
	a.savePlacement()
	pShowWindow.Call(a.top, swHide)
}

// beginQuit：nagare 要退出了。窗口藏起来（播放由主进程的收尾停掉），之后的 Show / 播放都不再理会。
func (a *app) beginQuit() {
	a.quitting = true
	if a.fs.on {
		a.setFullscreen(false)
	}
	a.savePlacement()
	if a.top != 0 {
		pShowWindow.Call(a.top, swHide)
	}
}

// onEndSession 处理注销 / 关机：让主进程退出并收尾，最多等 endSessionLimit（之后进程随时会被结束）。
func (a *app) onEndSession() {
	if a.endingSession {
		return
	}
	a.endingSession = true
	log.Print("desktop: Windows 正在注销或关机：停止播放并回写进度")
	if quit := a.options().Quit; quit != nil {
		quit()
	}
	a.pumpUntilDone(endSessionLimit)
}

// ───────────────────────── 窗口过程 ─────────────────────────

func topProc(hwnd uintptr, m uint32, w, l uintptr) uintptr {
	a := ui
	switch m {
	case wmSize:
		a.layout()
		if w != sizeMinimized {
			a.hostChanged()
		}
		return 0
	case wmMove:
		if a.web != nil {
			a.web.moved()
		}
		return 0
	case wmGetMinMaxInfo:
		dpi := windowDPI(hwnd)
		mmi := *(**minMaxInfo)(unsafe.Pointer(&l)) // lParam 是系统给的指针（这样写 vet 才认）
		mmi.PtMinTrackSize = point{int32(minWidth * dpi / 96), int32(minHeight * dpi / 96)}
		return 0
	case wmDpiChanged:
		r := *(**rect)(unsafe.Pointer(&l)) // 系统建议的新位置大小
		pSetWindowPos.Call(hwnd, 0, uintptr(r.Left), uintptr(r.Top), uintptr(r.width()), uintptr(r.height()),
			swpNoZOrder|swpNoActivate)
		return 0
	case wmActivate:
		if loword(w) != 0 && a.mode == modeVideo {
			a.focusVideo()
		}
	case wmSetFocus:
		if a.mode == modeVideo {
			a.focusVideo()
		} else if a.web != nil {
			a.web.focus()
		}
		return 0
	case wmSysCommand:
		if r, handled := a.sysCommand(hwnd, w, l); handled {
			return r
		}
	case wmClose:
		a.closeToBackground()
		return 0
	case wmQueryEndSession:
		return 1
	case wmEndSession:
		if w != 0 {
			a.onEndSession()
		}
		return 0
	}
	return defWindowProc(hwnd, m, w, l)
}

// sysCommand 处理系统命令里会进模态循环的那几个（SC_KEYMENU：单按 Alt、Alt+空格）。
//
// mpv 的子窗口收到 Alt 之后由它的 DefWindowProc 把 WM_SYSCOMMAND 跨线程「发」（SendMessage）给我们的顶层窗口；我们
// 的 DefWindowProc 在处理它的过程中进入菜单的模态循环，循环不结束，mpv 的窗口线程就一直卡在那次 SendMessage 里 ——
// 画面冻住、看门狗把 mpv 当卡死结束掉（CI 的冒烟测试实测：单按一下 Alt，8 秒后 mpv 被杀）。所以：
//   - 别的线程发来的系统命令先放回自己的消息队列（PostMessage），马上返回，mpv 的线程不等我们；
//   - 播放时单按 Alt（lParam 为 0）直接吞掉：没有菜单栏，「菜单模式」只会吃掉接下来的一下按键（空格、t、Enter）。
//     Alt+空格（lParam 为空格）照常弹出系统菜单（移动、大小、关闭……）。
func (a *app) sysCommand(hwnd uintptr, w, l uintptr) (uintptr, bool) {
	if w&0xFFF0 != scKeyMenu {
		return 0, false
	}
	if l == 0 && a.mode == modeVideo {
		return 0, true
	}
	if boolCall(pInSendMessage) {
		postMessage(hwnd, wmSysCommand, w, l)
		return 0, true
	}
	return 0, false
}

func hostProc(hwnd uintptr, m uint32, w, l uintptr) uintptr {
	return defWindowProc(hwnd, m, w, l)
}

// videoProc 是 mpv 子窗口的父窗口。正常情况下 mpv 的子窗口被重新启用、自己收输入，这里只是黑底；
// IPC 转发那条退路下输入落在这里，由 forwardInput 转给 mpv。
func videoProc(hwnd uintptr, m uint32, w, l uintptr) uintptr {
	if ipcInput {
		if r, handled := ui.forwardInput(hwnd, m, w, l); handled {
			return r
		}
	}
	return defWindowProc(hwnd, m, w, l)
}

func windowDPI(hwnd uintptr) uintptr {
	if err := pGetDpiForWindow.Find(); err != nil {
		return 96
	}
	r, _, _ := pGetDpiForWindow.Call(hwnd)
	if r == 0 {
		return 96
	}
	return r
}
