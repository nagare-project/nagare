//go:build windows && desktoptest

package desktop

// 测试构建（-tags desktoptest）才有的遥控：设了 NAGARE_DESKTOP_TEST_ADDR（只许回环地址）与
// NAGARE_DESKTOP_TEST_SECRET_FILE 就在那里开一个 HTTP 遥控，给 Windows 冒烟测试（CI）与用户手里的测试包
// （scripts/windows/desktoptest/）截窗口、合成真实输入、读状态、经 IPC 问 mpv。正式构建没有这个文件。
// 门与 macOS 那份相同（testctl_guard.go）：Host 必须正好是监听地址，每个请求带每次运行随机生成的密钥。
//
// 另有两个开关模拟退路（启动前设好环境变量）：
//   NAGARE_DESKTOP_TEST_NO_WEBVIEW2=1    假装没有 WebView2 运行时（启动时就退回浏览器模式）
//   NAGARE_DESKTOP_TEST_FAIL_WEBVIEW2=1  假装 WebView2 起不来（第一次打开窗口时退回）

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

func init() {
	testHooks.noRuntime = os.Getenv("NAGARE_DESKTOP_TEST_NO_WEBVIEW2") == "1"
	testHooks.failWeb = os.Getenv("NAGARE_DESKTOP_TEST_FAIL_WEBVIEW2") == "1"
	addr := os.Getenv("NAGARE_DESKTOP_TEST_ADDR")
	if addr == "" {
		return
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() {
		log.Printf("desktop: NAGARE_DESKTOP_TEST_ADDR 只能是回环地址，忽略：%v", err)
		return
	}
	secret, err := writeTestSecret(os.Getenv("NAGARE_DESKTOP_TEST_SECRET_FILE"))
	if err != nil {
		log.Printf("desktop: 测试遥控不开：%v", err)
		return
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/state", testState)
	mux.HandleFunc("/snap", testSnap)
	mux.HandleFunc("/input", testInput)
	mux.HandleFunc("/winop", testWinOp)
	mux.HandleFunc("/mpv", testMpv)
	mux.HandleFunc("/js", testJS)
	mux.HandleFunc("/show", func(w http.ResponseWriter, _ *http.Request) {
		Show()
		fmt.Fprintln(w, "ok")
	})
	go func() {
		log.Printf("desktop: 测试遥控在 %s", addr)
		if err := http.ListenAndServe(addr, guardTestControl(addr, secret, mux)); err != nil {
			log.Printf("desktop: 测试遥控停了：%v", err)
		}
	}()
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func fail(w http.ResponseWriter, err error) {
	http.Error(w, err.Error(), http.StatusInternalServerError)
}

func onUI(f func()) error { return ui.q.call(5*time.Second, f) }

// testState 是窗口此刻的全部状态（在界面线程上取）。
func testState(w http.ResponseWriter, _ *http.Request) {
	out := map[string]any{}
	err := onUI(func() {
		a := ui
		host := a.hostState()
		cr := clientRect(a.top)
		var title [256]uint16
		pGetWindowTextW.Call(a.top, uintptr(unsafePtr(&title[0])), uintptr(len(title)))
		focus, _, _ := pGetFocus.Call()
		fg, _, _ := pGetForegroundWindow.Call()
		out["mode"] = map[viewMode]string{modeWeb: "web", modeVideo: "video"}[a.mode]
		out["webReady"] = a.web != nil && a.web.ready
		out["webFailed"] = a.webFailed
		out["visible"] = boolCall(pIsWindowVisible, a.top)
		out["foreground"] = fg == a.top
		out["fullscreen"] = host.Fullscreen
		out["maximized"] = host.Maximized
		out["minimized"] = host.Minimized
		out["topmost"] = host.Topmost
		out["title"] = utf16ToString(title[:])
		out["client"] = []int32{cr.width(), cr.height()}
		out["window"] = windowRect(a.top)
		out["dpi"] = windowDPI(a.top)
		out["ipcInput"] = ipcInput
		out["sessionId"] = a.sessionID
		out["quitting"] = a.quitting
		if s := a.session; s != nil {
			child := s.child.Load()
			out["child"] = child != 0
			out["childEnabled"] = child != 0 && boolCall(pIsWindowEnabled, child)
			out["childHung"] = child != 0 && boolCall(pIsHungAppWindow, child)
			out["childRect"] = windowRect(child)
			out["focusIsChild"] = child != 0 && focus == child
			out["mpvPid"] = s.pid.Load()
			out["killedByWatchdog"] = s.killed.Load()
			out["ipcConnected"] = s.ipc.Load() != nil
			props := map[string]any{}
			for k, v := range s.snapshot() {
				var x any
				_ = json.Unmarshal(v, &x)
				props[k] = x
			}
			out["mpv"] = props
		}
	})
	if err != nil {
		fail(w, err)
		return
	}
	info := State()
	out["active"] = info.Active
	out["fallback"] = info.Fallback
	out["runtime"] = RuntimeVersion()
	writeJSON(w, out)
}

// testSnap 截窗口（从屏幕上截：mpv 的画面与 WebView2 都是别的进程画的，只有屏幕上那一份是合成好的）。
// full=1 截整个虚拟桌面。
func testSnap(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if path == "" {
		http.Error(w, "缺 path", http.StatusBadRequest)
		return
	}
	var box rect
	if r.URL.Query().Get("full") == "1" {
		box = virtualScreen()
	} else if err := onUI(func() { box = frameRect(ui.top) }); err != nil {
		fail(w, err)
		return
	}
	if err := captureScreen(box, path); err != nil {
		fail(w, err)
		return
	}
	fmt.Fprintf(w, "ok %dx%d\n", box.width(), box.height())
}

// testInput 往系统里注入真实的输入（SendInput / SetCursorPos），坐标是窗口客户区内的物理像素。
// 走的是真实的输入路径：落到哪个窗口由系统决定 —— 测的正是「重新启用的 mpv 子窗口收不收得到」。
func testInput(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	num := func(k string) int32 { v, _ := strconv.ParseInt(q.Get(k), 0, 32); return int32(v) }
	var err error
	switch kind := q.Get("kind"); kind {
	case "move", "click", "dblclick", "down", "up":
		var pt point
		if e := onUI(func() { pt = clientToScreen(ui.top, num("x"), num("y")) }); e != nil {
			fail(w, e)
			return
		}
		err = mouseAt(kind, pt, q.Get("button"))
	case "wheel":
		err = sendWheel(num("delta"))
	case "key":
		err = sendKey(uint16(num("vk")), q.Get("up") == "1")
	case "tap":
		err = tapKey(uint16(num("vk")), strings.Split(q.Get("mods"), ","))
	default:
		http.Error(w, "kind 不认识："+kind, http.StatusBadRequest)
		return
	}
	if err != nil {
		fail(w, err)
		return
	}
	fmt.Fprintln(w, "ok")
}

// testWinOp 是窗口上的系统操作：最小化 / 最大化 / 还原 / 关闭（WM_CLOSE，同标题栏的 ×）/ 抢前台。
func testWinOp(w http.ResponseWriter, r *http.Request) {
	op := r.URL.Query().Get("op")
	err := onUI(func() {
		a := ui
		switch op {
		case "minimize":
			pShowWindow.Call(a.top, swMinimize)
		case "maximize":
			pShowWindow.Call(a.top, swMaximize)
		case "restore":
			pShowWindow.Call(a.top, swRestore)
		case "close":
			postMessage(a.top, wmClose, 0, 0)
		case "front":
			forceForeground(a.top)
		}
	})
	if err != nil {
		fail(w, err)
		return
	}
	fmt.Fprintln(w, "ok")
}

// testMpv 经宿主自己的 IPC 连接发一条 mpv 命令（cmd 是 JSON 数组），返回 mpv 的回复。
func testMpv(w http.ResponseWriter, r *http.Request) {
	var cmd []any
	if err := json.Unmarshal([]byte(r.URL.Query().Get("cmd")), &cmd); err != nil || len(cmd) == 0 {
		http.Error(w, "cmd 要是非空的 JSON 数组", http.StatusBadRequest)
		return
	}
	var c *hostIPC
	if err := onUI(func() {
		if s := ui.session; s != nil {
			c = s.ipc.Load()
		}
	}); err != nil {
		fail(w, err)
		return
	}
	if c == nil {
		http.Error(w, "没有在播放（或 IPC 没连上）", http.StatusConflict)
		return
	}
	data, err := c.request(5*time.Second, cmd...)
	if err != nil {
		writeJSON(w, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, map[string]any{"error": "success", "data": data})
}

// testJS 在网页层执行一段脚本，返回结果（JSON 文本）。
func testJS(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	res := make(chan string, 1)
	var startErr error
	err := onUI(func() {
		if ui.web == nil || !ui.web.ready {
			startErr = fmt.Errorf("网页层还没就绪")
			return
		}
		startErr = ui.web.c.CoreWebView().RunScript(code, func(out string, err error) {
			if err != nil {
				out = "error: " + err.Error()
			}
			res <- out
		})
	})
	if err == nil {
		err = startErr
	}
	if err != nil {
		fail(w, err)
		return
	}
	select {
	case out := <-res:
		fmt.Fprintln(w, out)
	case <-time.After(5 * time.Second):
		fail(w, fmt.Errorf("脚本 5 秒内没有结果"))
	}
}
