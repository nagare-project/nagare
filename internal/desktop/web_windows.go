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
	"runtime/debug"
	"sync/atomic"
	"time"

	"golang.org/x/sys/windows"

	"github.com/nagare-project/nagare/internal/webview2/edge"
)

// webRetries 是浏览器进程意外退出后重建网页层的次数。
const webRetries = 1

// webCreateLimit 是等 WebView2 建好的上限（冷启动的运行时 + 首次建用户数据目录，慢机器上要好几秒）。
const webCreateLimit = 25 * time.Second

// devTools：NAGARE_DESKTOP_DEVTOOLS=1 时允许开发者工具（排查界面问题用）。
var devTools = os.Getenv("NAGARE_DESKTOP_DEVTOOLS") == "1"

// webBrowserPID 是当前网页层的浏览器进程（msedgewebview2.exe 主进程）。网页层的弹出窗口（右键菜单、下拉框）属于它，
// 测试遥控据此把它们也算作 nagare 的窗口。
var webBrowserPID atomic.Uint32

// liveWebLayers 让建过的网页层（连同交给 WebView2 的回调对象）一直可达，不被 GC 收走。只在界面线程上碰。
var liveWebLayers []*webLayer

// safeWeb 跑一个 WebView2 回调：里面 panic 了不带走整个进程，这个网页层按起不来处理（退回浏览器）。
func (a *app) safeWeb(w *webLayer, f func()) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("desktop: WebView2 回调出错：%v", r)
			if a.web == w {
				a.failWeb(fmt.Errorf("WebView2 回调出错：%v", r))
			}
		}
	}()
	f()
}

type webLayer struct {
	c      *edge.Chromium
	origin string
	ready  bool
	pid    uint32 // 浏览器进程（msedgewebview2.exe 主进程）
	// COM 那边存的是这几个对象的裸指针：一直持有，不能让 GC 收走
	navHandler  *edge.ICoreWebView2NavigationStartingEventHandler
	winHandler  *edge.ICoreWebView2NewWindowRequestedEventHandler
	menuHandler *edge.ICoreWebView2ContextMenuRequestedEventHandler
	events      *webEvents
	ctlClosed   bool // 控制器已经关过（close 可以调不止一次：超时退回之后控制器才迟迟建好时还要再关一次）
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
	base := o.CacheDir
	if base == "" {
		base = o.DataDir
	}
	if base == "" {
		a.failWeb(fmt.Errorf("没有目录可放 WebView2 的用户数据"))
		return
	}
	dir := filepath.Join(base, "webview2")
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
	c.ReadyCallback = func(err error) { a.safeWeb(w, func() { a.webReady(w, err) }) }
	c.ProcessFailedCallback = func(sender *edge.ICoreWebView2, args *edge.ICoreWebView2ProcessFailedEventArgs) {
		a.safeWeb(w, func() { w.events.processFailed(sender, args) })
	}
	c.ContainsFullScreenElementChangedCallback = func(sender *edge.ICoreWebView2, args *edge.ICoreWebView2ContainsFullScreenElementChangedEventArgs) {
		a.safeWeb(w, func() { w.events.fullscreenElement(sender, args) })
	}
	// WebView2 存的是回调对象的裸指针：网页层换掉之后它们也可能还被调到，永远留一份引用（一次运行最多建两三个）
	liveWebLayers = append(liveWebLayers, w)
	a.web = w
	if a.pending == "" {
		a.pending = o.URL
	}
	// 环境与控制器是异步建的：回调一直不来（运行时坏了、用户数据目录被别的进程占着）时窗口会永远是空的。
	// 到点还没好就按起不来处理，退回浏览器 —— 用户看得见发生了什么
	time.AfterFunc(webCreateLimit, func() {
		a.q.post(func() {
			if a.web == w && !w.ready && a.webFailed == "" {
				a.failWeb(fmt.Errorf("%s 内没建好（运行时没响应，或者用户数据目录 %s 被占着）", webCreateLimit, dir))
			}
		})
	})
	if err := c.Start(a.webHost); err != nil {
		a.failWeb(err)
	}
}

// webReady 是 WebView2 创建的结果（界面线程，经消息循环回来）。
func (a *app) webReady(w *webLayer, err error) {
	if a.web != w {
		// 早被换掉了（浏览器进程崩了重建过），或者超过 webCreateLimit 已经退回了浏览器模式：这个迟到的控制器
		// 没人用了，关掉它（不然它的浏览器进程一直开着、占着用户数据目录）
		w.close()
		return
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
		// Ctrl+滚轮 / Ctrl+加减号的缩放：缩放比例会记在用户数据目录里，下次打开整个界面还是歪的
		"缩放": func() error { return s.PutIsZoomControlEnabled(false) },
		// 不替网页记密码、不记表单填过的内容：账号的密码不该落进 WebView2 的用户数据目录（nagare 自己只存登录会话），
		// 输入框的右键菜单里也就没有「导入密码」这种项
		"保存密码": func() error { return s.PutIsPasswordAutosaveEnabled(false) },
		"自动填充": func() error { return s.PutIsGeneralAutofillEnabled(false) },
		// 浏览器自己的右键菜单开着，由 ContextMenuRequested 筛：只在输入框与选中的文字上弹（见 keepContextMenu）
		"右键菜单": func() error { return s.PutAreDefaultContextMenusEnabled(true) },
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
	w.filterContextMenus(core)
	if pid, err := core.GetBrowserProcessID(); err == nil {
		w.pid = pid
		webBrowserPID.Store(pid)
	} else {
		log.Printf("desktop: 读不到 WebView2 浏览器进程的 PID（退出时不能等它）：%v", err)
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
		} else {
			log.Printf("desktop: 打不开 WebView2 的浏览器进程（不等它退出）：%v", err)
		}
	}
	w.close()
	if proc != 0 && !a.pumpWait(proc, limit) {
		log.Printf("desktop: WebView2 的浏览器进程 %s 内没有退出", limit)
	}
}

// filterContextMenus 挂上右键菜单的筛选。挂不上（运行时太老、没有 ICoreWebView2_11）就整个菜单都留着：
// 宁可多一个用不着的菜单，也不能让输入框里没有「粘贴」（用户要往里贴磁力链接、地址）。
func (w *webLayer) filterContextMenus(core *edge.ICoreWebView2) {
	c11 := core.GetICoreWebView2_11()
	if c11 == nil {
		log.Print("desktop: 这个 WebView2 运行时不支持筛右键菜单，浏览器的菜单全留着")
		return
	}
	defer c11.Release()
	w.menuHandler = edge.NewContextMenuRequestedHandler(w.events)
	if err := c11.AddContextMenuRequested(w.menuHandler); err != nil {
		log.Printf("desktop: 挂不上右键菜单的筛选（浏览器的菜单全留着）：%v", err)
	}
}

// close 关掉控制器。可以调不止一次：控制器还没建好时只记下「在关了」，之后迟到的控制器在 webReady 里再关一次。
func (w *webLayer) close() {
	w.c.ShuttingDown()
	w.ready = false
	if w.ctlClosed {
		return
	}
	if ctl := w.c.GetController(); ctl != nil {
		w.ctlClosed = true
		if err := ctl.Close(); err != nil {
			log.Printf("desktop: 关 WebView2 失败：%v", err)
		}
	}
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

// QueryInterface 一律答 E_NOINTERFACE：这两个对象只当回调用，WebView2 不该把它们当别的接口用
func (*webEvents) QueryInterface(_, _ uintptr) uintptr { return eNoInterface }
func (*webEvents) AddRef() uintptr                     { return 1 }
func (*webEvents) Release() uintptr                    { return 1 }

// NavigationStarting：顶层导航，只放行我们自己的源。
func (e *webEvents) NavigationStarting(_ *edge.ICoreWebView2, args *edge.ICoreWebView2NavigationStartingEventArgs) uintptr {
	defer func() {
		if r := recover(); r != nil {
			logPanic("网页导航的回调", r)
			// 导航是安全边界：判断到一半出错就拦下这次导航（fail closed）；取消本身再出错也不往外抛
			func() {
				defer recoverCallback("取消导航")
				cancelNav(args)
			}()
		}
	}()
	uri, err := args.GetUri()
	if err != nil {
		cancelNav(args)
		return 0
	}
	switch decideNav(uri, false, false, e.w.origin) {
	case actAllow:
	case actExternal:
		cancelNav(args)
		// 页面脚本自己跳到外链（不是用户点的）不替它开浏览器：恶意 / 出错的页面不能借窗口一直弹浏览器
		if user, uerr := args.GetIsUserInitiated(); uerr == nil && user {
			openExternal(uri)
		} else {
			logBlocked(uri)
		}
	default:
		cancelNav(args)
		logBlocked(uri)
	}
	return 0
}

// recoverCallback 接住 WebView2 回调里的 panic（defer 直接调它）：回调是从系统的 COM 调用里进来的，panic 穿过去
// 就是整个进程退出。
func recoverCallback(where string) {
	if r := recover(); r != nil {
		logPanic(where, r)
	}
}

func logPanic(where string, r any) {
	log.Printf("desktop: %s出错（已跳过）：%v\n%s", where, r, debug.Stack())
}

// ContextMenuRequested：右键。输入框与选中的文字上留浏览器的菜单（复制、粘贴、全选），别处不弹。
// 读不到点的是什么时照默认菜单弹：宁可多一个菜单，不能让输入框没有粘贴。
func (e *webEvents) ContextMenuRequested(_ *edge.ICoreWebView2, args *edge.ICoreWebView2ContextMenuRequestedEventArgs) uintptr {
	defer recoverCallback("右键菜单的回调")
	t, err := args.GetContextMenuTarget()
	if err != nil || t == nil {
		log.Printf("desktop: 读不到右键点到的东西（照默认菜单）：%v", err)
		return 0
	}
	editable, eerr := t.IsEditable()
	selection, serr := t.HasSelection()
	t.Release()
	if !keepContextMenu(editable || eerr != nil, selection || serr != nil, devTools) {
		if err := args.PutHandled(true); err != nil {
			log.Printf("desktop: 压不住右键菜单（浏览器的菜单照样弹）：%v", err)
		}
		return 0
	}
	if !devTools {
		pruneMenuItems(args)
	}
	return 0
}

// pruneMenuItems 从留下来的菜单里去掉应用里不该有的项（见 dropMenuItem）。按 Name 认，认不出的照留。
func pruneMenuItems(args *edge.ICoreWebView2ContextMenuRequestedEventArgs) {
	items, err := args.GetMenuItems()
	if err != nil || items == nil {
		return
	}
	defer items.Release()
	count, err := items.Count()
	if err != nil {
		return
	}
	var kept, dropped []string
	for i := int(count) - 1; i >= 0; i-- { // 倒着删，前面的下标不变
		it, err := items.ItemAt(uint32(i))
		if err != nil || it == nil {
			continue
		}
		name, _ := it.Name()
		it.Release()
		if !dropMenuItem(name) {
			kept = append([]string{name}, kept...)
			continue
		}
		if err := items.RemoveAt(uint32(i)); err != nil {
			log.Printf("desktop: 删不掉右键菜单的「%s」：%v", name, err)
			kept = append([]string{name}, kept...)
			continue
		}
		dropped = append([]string{name}, dropped...)
	}
	if testHooks.logMenus {
		log.Printf("desktop: 右键菜单：留 %v，去掉 %v", kept, dropped)
	}
}

// cancelNav 取消一次导航；取消不了是安全边界上的事，要记下来。
func cancelNav(args *edge.ICoreWebView2NavigationStartingEventArgs) {
	if err := args.PutCancel(true); err != nil {
		log.Printf("desktop: 取消导航失败（这次导航可能照样进行了）：%v", err)
	}
}

// NewWindowRequested：target=_blank / window.open。永远不开第二个窗口：自己的源就在这个窗口里开。
func (e *webEvents) NewWindowRequested(_ *edge.ICoreWebView2, args *edge.ICoreWebView2NewWindowRequestedEventArgs) uintptr {
	defer recoverCallback("新窗口的回调")
	uri, err := args.GetUri()
	if herr := args.PutHandled(true); herr != nil {
		log.Printf("desktop: 拦不住新窗口（WebView2 可能自己开了一个）：%v", herr)
	}
	if err != nil {
		return 0
	}
	// 不是用户点出来的弹窗（脚本自己 window.open、内嵌的预告片框架）一律不开
	if user, uerr := args.GetIsUserInitiated(); uerr == nil && !user {
		logBlocked(uri)
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
