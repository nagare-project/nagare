//go:build windows

package desktop

// 消息循环：托盘（systray.Register 建的隐藏窗口）、我们的窗口、WebView2 的回调都在主线程上，由这里的循环处理。
// 别的 goroutine 经 uiQueue 排活，投递是往「只收消息的窗口」上 PostMessage 一条 wmAppRun。

import (
	"log"
	"runtime"
	"runtime/debug"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	wmAppRun = wmApp + 1 // uiQueue 有活
)

var (
	dispatchProcCb        = windows.NewCallback(dispatchProc)
	user32SetThreadDpiCtx = user32.NewProc("SetThreadDpiAwarenessContext")
)

// dpiPerMonitorV2 是 DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2（(HANDLE)-4），与 mpv.exe 的 manifest 一致。
var dpiPerMonitorV2 = ^uintptr(3)

// run 是托盘那边要跑的消息循环（MainLoop）。
func (a *app) run(done <-chan struct{}) {
	runtime.LockOSThread() // 主 goroutine 本来就锁在主线程上（systray 的 init），这里写明
	a.done = done
	a.threadID = currentThreadID()
	// nagare.exe 带着 PerMonitorV2 的 manifest（rsrc_windows_*.syso）；线程上再设一次，没有 manifest 的构建
	// （go run、测试二进制）建出来的窗口也是 PerMonitorV2 —— mpv.exe 是 PerMonitorV2，跨进程的父子窗口 DPI
	// 模式不一致时系统会强制重置子窗口所在进程的 DPI 设置（SetParent 文档的表）。
	user32SetThreadDpiCtx.Call(dpiPerMonitorV2)
	if err := a.createWindows(); err != nil {
		log.Printf("desktop: 建不了窗口，界面改在浏览器里打开：%v", err)
		a.q.close()
		a.setInfo(Info{Fallback: "nagare 的窗口建不起来：" + err.Error()})
		a.fallback("", err.Error())
		pumpUntilQuit() // 托盘的消息照样要处理
		a.pumpUntilDone(quitPumpLimit)
		return
	}
	a.startQueue()
	a.running.Store(true)
	a.quitWhenDone()

	pumpUntilQuit()
	a.running.Store(false)

	// 托盘退出了（或 ctx 取消）：主进程正在收尾。窗口先藏起来，消息照常处理到收尾完成
	a.beginQuit()
	a.pumpUntilDone(quitPumpLimit)
	a.shutdownWeb(browserExitLimit)
	a.savePlacement()
	a.q.close() // 先关队列再拆窗口：之后的投递不会落到已经销毁的消息窗口上
	a.killStraggler()
	a.destroyWindows()
}

// quitWhenDone：主进程收尾完成（done 关闭）时也给这个线程投一个 WM_QUIT。平时 WM_QUIT 来自托盘（systray.Quit），
// 但托盘起不来时（registerSystray 失败只记日志）没有人投，循环就永远不停、进程退不出去、自更新也重启不了。
func (a *app) quitWhenDone() {
	if a.done == nil {
		return
	}
	tid := a.threadID
	go func() {
		<-a.done
		if r, _, err := pPostThreadMessageW.Call(uintptr(tid), wmQuit, 0, 0); r == 0 {
			log.Printf("desktop: 收尾完成后投不出 WM_QUIT：%v", err)
		}
	}()
}

// killStraggler：收尾超时、mpv 还挂在画面层上（卡死了）时先结束它 —— 销毁顶层窗口要连带销毁它的子窗口，
// 别的进程的子窗口卡着时那一步也会卡住。
func (a *app) killStraggler() {
	s := a.session
	if s == nil {
		return
	}
	if p := s.player; p != nil {
		select {
		case <-p.Done():
			return // mpv 已经退出：它的进程号可能已经给了别的进程，不能再按这个号去结束
		default:
		}
	}
	pid := s.pid.Load() // IPC 问来的 mpv 进程号；没问到才看窗口属于谁
	if child := s.child.Load(); pid == 0 && child != 0 {
		pid = windowPID(child)
	}
	log.Print("desktop: 退出时 mpv 还挂在窗口上，先结束它")
	if err := killProcess(pid); err != nil {
		log.Printf("desktop: 结束 mpv 失败：%v", err)
	}
}

// startQueue 让 uiQueue 往这个线程上的消息窗口投递（建好 dispatch 之后、在这个线程上调）。
func (a *app) startQueue() {
	self := currentThreadID()
	a.q.mu.Lock()
	a.q.onUI = func() bool { return currentThreadID() == self }
	a.q.mu.Unlock()
	d := a.dispatch // 按值捕获：投递可能在任何 goroutine 上，而 a.dispatch 在拆窗口时清零
	a.q.start(func() bool { return postMessage(d, wmAppRun, 0, 0) })
}

// pumpUntilQuit 处理消息直到 WM_QUIT（嵌套的 pumpWait 取到 WM_QUIT 会补投，这里照样收得到）。
func pumpUntilQuit() {
	var m msg
	for {
		r, _, err := pGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		switch int32(r) {
		case -1:
			log.Printf("desktop: 消息循环出错：%v", err)
			return
		case 0:
			return
		}
		pTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		pDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
}

// pumpUntilDone 处理消息直到 a.done 关闭，最多 limit。
func (a *app) pumpUntilDone(limit time.Duration) {
	if a.done == nil {
		return
	}
	ev, err := windows.CreateEvent(nil, 1, 0, nil)
	if err != nil {
		log.Printf("desktop: 建不了等待用的事件，不等收尾：%v", err)
		return
	}
	stop := make(chan struct{})
	helper := make(chan struct{})
	// 关句柄之前等 helper 退出：否则它可能在句柄关掉（值被系统复用）之后才 SetEvent
	defer func() {
		close(stop)
		<-helper
		windows.CloseHandle(ev)
	}()
	go func() {
		defer close(helper)
		select {
		case <-a.done:
			if err := windows.SetEvent(ev); err != nil {
				log.Printf("desktop: 收尾完成的事件设不上（只能等到超时）：%v", err)
			}
		case <-stop:
		}
	}()
	if !a.pumpWait(ev, limit) {
		log.Printf("desktop: 收尾超过 %s 仍未完成，先拆窗口", limit)
	}
}

// pumpWait 一边处理消息一边等 h 有信号，最多 limit；等到了返回真。可以嵌套（窗口过程里也能调）：
// 取到 WM_QUIT 时记下来，返回前补投一次，外层的循环照样停得下来。
func (a *app) pumpWait(h windows.Handle, limit time.Duration) bool {
	deadline := time.Now().Add(limit)
	sawQuit := false
	defer func() {
		if sawQuit {
			pPostQuitMessage.Call(0)
		}
	}()
	for {
		left := time.Until(deadline)
		if left <= 0 {
			return false
		}
		r, _, _ := pMsgWaitForMultipleObj.Call(1, uintptr(unsafe.Pointer(&h)), 0, uintptr(left/time.Millisecond)+1, qsAllInput)
		switch uint32(r) {
		case waitObject0:
			return true
		case waitTimeout:
			return false
		case waitObject0 + 1:
			if drainMessages() {
				sawQuit = true
			}
		default:
			return false // WAIT_FAILED：句柄坏了，不能空转
		}
	}
}

// drainMessages 把队列里现有的消息处理完；取到 WM_QUIT 返回真（不分发它）。
func drainMessages() bool {
	var m msg
	quit := false
	for boolCall(pPeekMessageW, uintptr(unsafe.Pointer(&m)), 0, 0, 0, pmRemove) {
		if m.Message == wmQuit {
			quit = true
			continue
		}
		pTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		pDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
	return quit
}

func dispatchProc(hwnd uintptr, m uint32, w, l uintptr) (r uintptr) {
	defer recoverProc(&r, "dispatchProc")
	if m == wmAppRun {
		ui.q.drain()
		return 0
	}
	return defWindowProc(hwnd, m, w, l)
}

// recoverProc 接住窗口过程里的 panic（defer 直接调它）：窗口过程是系统从 DispatchMessage 里回调进来的，panic 穿过
// 系统的回调就是整个进程退出 —— 同一个进程里还跑着 HTTP 服务、磁力引擎与进度回写。这一条消息当作处理过了。
func recoverProc(r *uintptr, where string) {
	if p := recover(); p != nil {
		log.Printf("desktop: %s 处理一条消息时出错（已跳过）：%v\n%s", where, p, debug.Stack())
		*r = 0
	}
}

func currentThreadID() uint32 {
	r, _, _ := pGetCurrentThreadId.Call()
	return uint32(r)
}
