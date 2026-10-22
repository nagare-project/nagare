//go:build windows

package desktop

// 网页层：WebView2（internal/webview2，vendored 的 wailsapp/go-webview2，出错不再结束进程）。
//
//   - 用户数据目录在 <配置目录>\webview2（微软建议自己指定；默认的 <exe>.WebView2 在安装目录里，自更新会换掉那里）。
//   - 导航规则与 macOS 一致（nav.go）：窗口里只开我们自己的源，外链交给默认浏览器，其余拦下；target=_blank 与
//     window.open 永远不开第二个窗口。
//   - 不加 JS 桥：关掉网页消息与宿主对象，页面照旧走 HTTP（A4 的鉴权原样适用）；不开开发者工具（除非
//     NAGARE_DESKTOP_DEVTOOLS=1）、状态栏与浏览器快捷键（Ctrl+P 打印、F5 刷新之类在应用里没有意义）。
//   - 起不来就退回浏览器模式（failWeb）；渲染进程崩了重新载入；浏览器进程退出了重建一次，再退出就退回浏览器。

import (
	"fmt"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/windows"

	"github.com/nagare-project/nagare/internal/webview2/edge"
)

// webRetries 是浏览器进程意外退出后重建网页层的次数。
const webRetries = 1

// devTools：NAGARE_DESKTOP_DEVTOOLS=1 时允许开发者工具（排查界面问题用）。
var devTools = os.Getenv("NAGARE_DESKTOP_DEVTOOLS") == "1"

type webLayer struct {
	c      *edge.Chromium
	origin string
	ready  bool
	pid    uint32 // 浏览器进程（msedgewebview2.exe 主进程）
	// COM 那边存的是这几个对象的裸指针：一直持有，不能让 GC 收走
	navHandler *edge.ICoreWebView2NavigationStartingEventHandler
	winHandler *edge.ICoreWebView2NewWindowRequestedEventHandler
	events     *webEvents
}

// ensureWeb 建网页层（只建一次；起不来过就不再试）。
func (a *app) ensureWeb() {
	if a.web != nil || a.webFailed != "" || a.top == 0 {
		return
	}
	o := a.options()
	if testHooks.failWeb {
		a.failWeb(fmt.Errorf("（测试）假装 WebView2 起不来"))
		return
	}
	dir := filepath.Join(o.DataDir, "webview2")
	if o.DataDir == "" {
		a.failWeb(fmt.Errorf("没有配置目录可放 WebView2 的用户数据"))
		return
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		a.failWeb(fmt.Errorf("建不了 WebView2 的用户数据目录：%w", err))
		return
	}
	// 第一帧之前的底色（微软文档的做法），与 SetBackgroundColour 同色，建网页层时不会白屏一闪
	_ = os.Setenv("WEBVIEW2_DEFAULT_BACKGROUND_COLOR", fmt.Sprintf("FF%02X%02X%02X", bgR, bgG, bgB))
	c := edge.NewChromium()
	c.DataPath = dir
	c.SetErrorCallback(func(err error) { log.Printf("desktop: WebView2：%v", err) })
	c.SetGlobalPermission(edge.CoreWebView2PermissionStateDeny) // 摄像头、麦克风、通知……界面一样都不要
	w := &webLayer{c: c, origin: o.Origin}
	w.events = &webEvents{a: a, w: w}
	c.ReadyCallback = func(err error) { a.webReady(w, err) }
	c.ProcessFailedCallback = w.events.processFailed
	c.ContainsFullScreenElementChangedCallback = w.events.fullscreenElement
	a.web = w
	if a.pending == "" {
		a.pending = o.URL
	}
	if err := c.Start(a.webHost); err != nil {
		a.failWeb(err)
	}
}

// webReady 是 WebView2 创建的结果（界面线程，经消息循环回来）。
func (a *app) webReady(w *webLayer, err error) {
	if a.web != w {
		return // 早被换掉了（浏览器进程崩了重建过）
	}
	if err != nil {
		a.failWeb(err)
		return
	}
	if err := w.configure(); err != nil {
		a.failWeb(err)
		return
	}
	w.ready = true
	log.Printf("desktop: 网页层就绪（WebView2 %s）", w.c.RuntimeVersion())
	w.c.SetBackgroundColour(bgR, bgG, bgB, 255)
	w.resize()
	w.setVisible(a.mode == modeWeb)
	if a.pending != "" {
		w.c.Navigate(a.pending)
		a.pending = ""
	}
	if a.mode == modeWeb {
		w.focus()
	}
}

// configure 关掉用不着的能力、挂上导航规则。
func (w *webLayer) configure() error {
	core := w.c.CoreWebView()
	if core == nil {
		return fmt.Errorf("WebView2 没有给出网页对象")
	}
	s, err := w.c.GetSettings()
	if err != nil {
		return fmt.Errorf("读不到 WebView2 的设置：%w", err)
	}
	for name, put := range map[string]func() error{
		"状态栏":    func() error { return s.PutIsStatusBarEnabled(false) },
		"开发者工具":  func() error { return s.PutAreDevToolsEnabled(devTools) },
		"宿主对象":   func() error { return s.PutAreHostObjectsAllowed(false) },
		"网页消息":   func() error { return s.PutIsWebMessageEnabled(false) },
		"浏览器快捷键": func() error { return s.PutAreBrowserAcceleratorKeysEnabled(devTools) },
	} {
		if err := put(); err != nil {
			// 老版本的运行时可能没有某一项：记下来，不当成起不来
			log.Printf("desktop: WebView2 的「%s」设不上：%v", name, err)
		}
	}
	// User-Agent 末尾追加 NagareDesktop：网页据此说「关闭窗口」而不是「关闭标签页」（docs/player-ui.md「宿主与网页」）。
	// 保留 WebView2 默认的那段（发现页的 YouTube 预告片会看它）；只管措辞，不参与鉴权。
	if ua, err := s.UserAgentString(); err != nil || ua == "" {
		log.Printf("desktop: 读不到 WebView2 的 User-Agent（网页会按浏览器标签页措辞）：%v", err)
	} else if err := s.PutUserAgent(withDesktopMarker(ua)); err != nil {
		log.Printf("desktop: 设不上 User-Agent（网页会按浏览器标签页措辞）：%v", err)
	}
	w.navHandler = edge.NewNavigationStartingHandler(w.events)
	if err := core.AddNavigationStarting(w.navHandler); err != nil {
		return fmt.Errorf("挂不上导航规则：%w", err)
	}
	w.winHandler = edge.NewNewWindowRequestedHandler(w.events)
	if err := core.AddNewWindowRequested(w.winHandler); err != nil {
		return fmt.Errorf("挂不上新窗口规则：%w", err)
	}
	if pid, err := core.GetBrowserProcessID(); err == nil {
		w.pid = pid
	}
	return nil
}

// failWeb：网页层起不来 / 坏了，退回浏览器模式。正在播的那一集照常在窗口里播完，之后的播放单独开 mpv 窗口。
func (a *app) failWeb(err error) {
	reason := "WebView2 起不来：" + err.Error()
	log.Printf("desktop: %s。界面改在浏览器里打开，mpv 单独开窗口", reason)
	if a.web != nil {
		a.web.close()
		a.web = nil
	}
	a.webFailed = reason
	a.setInfo(Info{Fallback: reason})
	if a.mode == modeWeb && !a.quitting {
		pShowWindow.Call(a.top, swHide)
		a.fallback(a.pending, reason)
	}
}

// navigate 在网页层载入 url；还没建好就等建好再载入。
func (a *app) navigate(url string) {
	if a.web != nil && a.web.ready {
		a.web.c.Navigate(url)
		return
	}
	a.pending = url
}

// shutdownWeb 关掉 WebView2 并等它的浏览器进程退出（最多 limit，期间照常处理消息）：自更新之后马上起来的
// 新版本要用同一个用户数据目录，而一个目录同时只能有一个会话（微软 user-data-folder 一文）。
func (a *app) shutdownWeb(limit time.Duration) {
	w := a.web
	a.web = nil
	if w == nil {
		return
	}
	var proc windows.Handle
	if w.pid != 0 {
		if h, err := windows.OpenProcess(windows.SYNCHRONIZE, false, w.pid); err == nil {
			proc = h
			defer windows.CloseHandle(h)
		}
	}
	w.close()
	if proc != 0 && !a.pumpWait(proc, limit) {
		log.Printf("desktop: WebView2 的浏览器进程 %s 内没有退出", limit)
	}
}

func (w *webLayer) close() {
	w.c.ShuttingDown()
	if ctl := w.c.GetController(); ctl != nil {
		if err := ctl.Close(); err != nil {
			log.Printf("desktop: 关 WebView2 失败：%v", err)
		}
	}
	w.ready = false
}

func (w *webLayer) resize() {
	if w.ready {
		w.c.Resize()
	}
}

func (w *webLayer) moved() {
	if w.ready {
		_ = w.c.NotifyParentWindowPositionChanged()
	}
}

func (w *webLayer) focus() {
	if w.ready {
		w.c.Focus()
	}
}

// setVisible 藏起来的 WebView2 会降低渲染频率，mpv 解码时正好不和它抢。
func (w *webLayer) setVisible(v bool) {
	if !w.ready {
		return
	}
	var err error
	if v {
		err = w.c.Show()
	} else {
		err = w.c.Hide()
	}
	if err != nil {
		log.Printf("desktop: WebView2 显示 / 隐藏失败：%v", err)
	}
}

// ───────────────────────── 事件 ─────────────────────────

// webEvents 实现 NavigationStarting / NewWindowRequested 两个回调对象（IUnknown 的引用计数由我们自己持有对象代替）。
type webEvents struct {
	a *app
	w *webLayer
}

func (*webEvents) QueryInterface(_, _ uintptr) uintptr { return 0 }
func (*webEvents) AddRef() uintptr                     { return 1 }
func (*webEvents) Release() uintptr                    { return 1 }

// NavigationStarting：顶层导航，只放行我们自己的源。
func (e *webEvents) NavigationStarting(_ *edge.ICoreWebView2, args *edge.ICoreWebView2NavigationStartingEventArgs) uintptr {
	uri, err := args.GetUri()
	if err != nil {
		_ = args.PutCancel(true)
		return 0
	}
	switch decideNav(uri, false, false, e.w.origin) {
	case actAllow:
	case actExternal:
		_ = args.PutCancel(true)
		openExternal(uri)
	default:
		_ = args.PutCancel(true)
		logBlocked(uri)
	}
	return 0
}

// NewWindowRequested：target=_blank / window.open。永远不开第二个窗口：自己的源就在这个窗口里开。
func (e *webEvents) NewWindowRequested(_ *edge.ICoreWebView2, args *edge.ICoreWebView2NewWindowRequestedEventArgs) uintptr {
	uri, err := args.GetUri()
	_ = args.PutHandled(true)
	if err != nil {
		return 0
	}
	switch decideNav(uri, true, false, e.w.origin) {
	case actLoadHere:
		e.a.q.post(func() { e.a.navigate(uri) }) // 回调返回之后再导航
	case actExternal:
		openExternal(uri)
	default:
		logBlocked(uri)
	}
	return 0
}

func (e *webEvents) processFailed(_ *edge.ICoreWebView2, args *edge.ICoreWebView2ProcessFailedEventArgs) {
	kind, err := args.GetProcessFailedKind()
	if err != nil {
		return
	}
	a := e.a
	switch kind {
	case edge.COREWEBVIEW2_PROCESS_FAILED_KIND_BROWSER_PROCESS_EXITED:
		if a.web != e.w {
			return
		}
		a.webTries++
		if a.webTries > webRetries {
			a.failWeb(fmt.Errorf("浏览器进程反复退出"))
			return
		}
		log.Print("desktop: WebView2 的浏览器进程退出了，重建网页层")
		e.w.close()
		a.web = nil
		a.q.post(a.ensureWeb)
	case edge.COREWEBVIEW2_PROCESS_FAILED_KIND_RENDER_PROCESS_EXITED,
		edge.COREWEBVIEW2_PROCESS_FAILED_KIND_RENDER_PROCESS_UNRESPONSIVE:
		log.Print("desktop: 网页的渲染进程退出或没响应，重新载入")
		if core := e.w.c.CoreWebView(); core != nil {
			_ = core.Reload()
		}
	}
}

// fullscreenElement：网页里有元素要全屏（发现页预告片的全屏按钮），窗口跟着全屏。
func (e *webEvents) fullscreenElement(sender *edge.ICoreWebView2, _ *edge.ICoreWebView2ContainsFullScreenElementChangedEventArgs) {
	on, err := sender.HasFullScreenElement()
	if err != nil || e.a.mode != modeWeb {
		return
	}
	e.a.setFullscreen(on)
}

// logBlocked 只记协议：地址里可能有东西。
func logBlocked(uri string) {
	scheme := "(无协议)"
	if u, err := url.Parse(uri); err == nil && u.Scheme != "" {
		scheme = u.Scheme
	}
	log.Printf("desktop: 网页层拦下了一个 %s: 导航", scheme)
}
