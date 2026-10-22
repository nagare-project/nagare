//go:build windows

package desktop

// 画面层上的一次 mpv 会话：mpv.exe --wid=<画面层> 把自己的窗口挂进来。宿主这边要做的：
//
//   - 找到 mpv 的子窗口（窗口类 "mpv"、属于这个 mpv 进程），重新启用它（mpv 在 --wid 下把它建成 disabled），
//     给它焦点 —— 之后鼠标悬停、光标自动隐藏、按键、滚轮、双击、媒体键都由 mpv 自己处理，Lua 界面照旧；
//   - 另开一条 IPC 连接：告诉 Lua 嵌在宿主里（user-data/nagare/embedded），观察全屏 / 最大化 / 最小化 / 置顶 /
//     标题，落到顶层窗口上（winstate.go）；
//   - 看门狗：mpv 的窗口线程卡住超过几秒就结束 mpv 进程（跨进程父子窗口的输入队列是连着的，它卡住会拖住宿主的输入）；
//   - mpv 退出（q、返回、Esc、播完、崩溃、关窗）→ 换回网页。nagare 的收尾（进度落盘、OnSessionEnd）不经过这里，
//     它跟着 mpv 进程走（internal/player），与命令行下用户关掉 mpv 窗口是同一条路。
//
// user-data/nagare/hidpi 不发：--wid 下 mpv 自己读得到屏幕缩放（GetDpiForMonitor，display-hidpi-scale），
// Lua 优先用 mpv 的（embed.lua）。titlebar-inset 是 0：v1 保留系统的标题栏，左上角没有要让的东西。

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/nagare-project/nagare/internal/mpv"
)

const (
	// childSearchLimit 是等 mpv 建好子窗口的上限（mpv 在初始化 VO 时才建窗口，慢机器上要一两秒）。
	childSearchLimit = 30 * time.Second
	childSearchStep  = 50 * time.Millisecond
	// hungStrikes：IsHungAppWindow 连续这么多秒为真（它自己要 5 秒没处理消息才算）就结束 mpv。
	hungStrikes  = 3
	watchdogStep = time.Second
	ipcDialLimit = 15 * time.Second
	ipcReplyWait = 5 * time.Second
	ipcOutQueue  = 128
)

// 宿主观察的属性。开关落到窗口上；标题给任务栏与 Alt+Tab；shown / capture 只给测试遥控看。
var (
	windowFlags   = []string{"fullscreen", "window-maximized", "window-minimized", "ontop"}
	observedOther = []string{"media-title", "user-data/nagare/shown", "user-data/nagare/capture", "pause"}
)

type sessionPrep struct {
	ok   bool
	id   uint64
	host uintptr
}

// beginSession（界面线程）：换到画面层、开一个新会话号。网页层坏了就不在窗口里播（ok 为假，单独开 mpv 窗口）。
func (a *app) beginSession() sessionPrep {
	if a.top == 0 || a.quitting || a.webFailed != "" {
		return sessionPrep{}
	}
	a.ensureWeb()
	a.seq++
	a.sessionID = a.seq
	a.session = nil // 上一个会话（若还在退出）的消息从此对不上号，忽略
	a.showVideo()
	a.bringToFront()
	return sessionPrep{ok: true, id: a.seq, host: a.videoHost}
}

// attachSession（界面线程）：起播成功，记下这个会话。
func (a *app) attachSession(s *videoSession) {
	if a.sessionID == s.id {
		a.session = s
	}
}

// endSession（界面线程）：会话结束（或起播失败），换回网页。
func (a *app) endSession(id uint64) {
	if a.sessionID != id {
		return // 过期的：早被新的会话顶掉了
	}
	a.sessionID = 0
	a.session = nil
	if a.top != 0 {
		a.showWeb()
	}
}

// seedSession（界面线程）：连上 IPC 之后先把宿主的状态写给 mpv、再开始观察 —— 观察报回来的初值与宿主一致，
// 不会反过来改窗口（比如换集时窗口正全屏）。
func (a *app) seedSession(s *videoSession) {
	c := s.ipc.Load()
	if a.sessionID != s.id || c == nil {
		return
	}
	s.sync = newWindowSync()
	c.send("set_property", "user-data/nagare/embedded", "yes")
	c.send("set_property", "user-data/nagare/titlebar-inset", 0)
	s.write(s.sync.fromHost(a.hostState()))
	id := 1
	for _, name := range append(append([]string{}, windowFlags...), observedOther...) {
		c.send("observe_property", id, name)
		id++
	}
}

// attachChild（界面线程）：mpv 的子窗口找到了。
func (a *app) attachChild(s *videoSession) {
	if a.sessionID != s.id {
		return
	}
	a.layout()
	if a.mode == modeVideo {
		a.focusVideo()
	}
}

// onProperty（界面线程）：mpv 报来一次属性变化。
func (a *app) onProperty(s *videoSession, name string, data json.RawMessage) {
	s.remember(name, data)
	if a.sessionID != s.id || s.sync == nil {
		return
	}
	if name == "media-title" {
		var t string
		if json.Unmarshal(data, &t) == nil && t != "" {
			setWindowText(a.top, t) // 任务栏与 Alt+Tab 显示这一集
		}
		return
	}
	var v bool
	if json.Unmarshal(data, &v) != nil {
		return
	}
	acts, writes := s.sync.fromMpv(name, v, a.hostState())
	s.write(writes)
	a.apply(acts)
}

// ───────────────────────── 会话 ─────────────────────────

type videoSession struct {
	id     uint64
	player *mpv.Player
	host   uintptr

	ipc     atomic.Pointer[hostIPC] // run 里连上之后写一次
	ipcOnce chan struct{}           // 连上（或连不上）之后关闭
	pid     atomic.Uint32
	child   atomic.Uintptr
	enabled atomic.Bool
	killed  atomic.Bool

	sync *windowSync // 界面线程

	mu    sync.Mutex
	props map[string]json.RawMessage // 最近一次的属性值（给测试遥控看）
}

func newVideoSession(id uint64, p *mpv.Player, host uintptr) *videoSession {
	return &videoSession{id: id, player: p, host: host, ipcOnce: make(chan struct{}), props: map[string]json.RawMessage{}}
}

// run：连 IPC、找子窗口、看门狗，等 mpv 退出后通知界面换回网页。
func (s *videoSession) run() {
	ctx, cancel := context.WithTimeout(context.Background(), ipcDialLimit)
	conn, err := mpv.DialEndpoint(ctx, s.player.Endpoint())
	cancel()
	if err != nil {
		log.Printf("desktop: 连不上 mpv 的 IPC（全屏、置顶跟不上窗口，播放不受影响）：%v", err)
	} else {
		c := newHostIPC(conn, func(name string, data json.RawMessage) {
			ui.q.post(func() { ui.onProperty(s, name, data) })
		})
		s.ipc.Store(c)
		if raw, err := c.request(ipcReplyWait, "get_property", "pid"); err == nil {
			var pid uint32
			if json.Unmarshal(raw, &pid) == nil {
				s.pid.Store(pid)
			}
		}
		ui.q.post(func() { ui.seedSession(s) })
	}
	close(s.ipcOnce)
	go s.findChild()
	go s.watchdog()
	<-s.player.Done()
	if c := s.ipc.Load(); c != nil {
		c.close()
	}
	ui.q.post(func() { ui.endSession(s.id) })
}

// write 把要写回 mpv 的开关发过去（IPC 没连上就算了）。
func (s *videoSession) write(ws []mpvWrite) {
	c := s.ipc.Load()
	if c == nil {
		return
	}
	for _, w := range ws {
		c.send("set_property", w.Name, w.Value)
	}
}

// quit 结束这次播放（关窗口时）。IPC 没连上就走 internal/player 那条连接（会等回复，放到后台）。
func (s *videoSession) quit() {
	if c := s.ipc.Load(); c != nil && c.send("quit") {
		return
	}
	go func() {
		if _, err := s.player.Command("quit"); err != nil {
			log.Printf("desktop: 让 mpv 退出失败：%v", err)
		}
	}()
}

func (s *videoSession) remember(name string, data json.RawMessage) {
	s.mu.Lock()
	s.props[name] = data
	s.mu.Unlock()
}

func (s *videoSession) snapshot() map[string]json.RawMessage {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]json.RawMessage, len(s.props))
	for k, v := range s.props {
		out[k] = v
	}
	return out
}

// findChild 等 mpv 建好子窗口：窗口类 "mpv"（w32_common.c 的 MPV_WINDOW_CLASS_NAME）、属于这个 mpv 进程
// （换集换片时画面层下面可能还挂着上一个正在退出的 mpv 的窗口）。找到就重新启用它 —— 在这个 goroutine 上做：
// EnableWindow 要等 mpv 的窗口线程处理 WM_ENABLE，不能让界面线程等。
func (s *videoSession) findChild() {
	<-s.ipcOnce
	class := utf16Ptr("mpv")
	deadline := time.Now().Add(childSearchLimit)
	for time.Now().Before(deadline) {
		select {
		case <-s.player.Done():
			return
		default:
		}
		if h := s.scanChildren(class); h != 0 {
			s.child.Store(h)
			if !ipcInput {
				pEnableWindow.Call(h, 1)
			}
			s.enabled.Store(boolCall(pIsWindowEnabled, h))
			ui.q.post(func() { ui.attachChild(s) })
			return
		}
		time.Sleep(childSearchStep)
	}
	log.Printf("desktop: %s 内没等到 mpv 的窗口（画面可能不在 nagare 的窗口里）", childSearchLimit)
}

func (s *videoSession) scanChildren(class *uint16) uintptr {
	pid := s.pid.Load()
	var after uintptr
	for {
		h, _, _ := pFindWindowExW.Call(s.host, after, uintptr(unsafe.Pointer(class)), 0)
		if h == 0 {
			return 0
		}
		if pid == 0 || windowPID(h) == pid {
			return h
		}
		after = h
	}
}

// watchdog：mpv 的窗口线程卡住（IsHungAppWindow 连续 hungStrikes 秒为真）就结束 mpv 进程。之后 internal/player
// 照常看到 mpv 退出（异常），最后的进度已经在它手上；画面层换回网页。
func (s *videoSession) watchdog() {
	t := time.NewTicker(watchdogStep)
	defer t.Stop()
	strikes := 0
	for {
		select {
		case <-s.player.Done():
			return
		case <-t.C:
		}
		child := s.child.Load()
		if child == 0 {
			continue
		}
		if !boolCall(pIsHungAppWindow, child) {
			strikes = 0
			continue
		}
		strikes++
		if strikes < hungStrikes {
			continue
		}
		log.Printf("desktop: mpv 的窗口超过 %d 秒没响应，结束 mpv 进程", 5+hungStrikes)
		if err := killProcess(windowPID(child)); err != nil {
			log.Printf("desktop: 结束 mpv 失败：%v", err)
		} else {
			s.killed.Store(true)
		}
		return
	}
}

func killProcess(pid uint32) error {
	if pid == 0 {
		return errors.New("不知道 mpv 的进程号")
	}
	h, err := windows.OpenProcess(windows.PROCESS_TERMINATE, false, pid)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(h)
	return windows.TerminateProcess(h, 1)
}

// ───────────────────────── 宿主自己的 IPC 连接 ─────────────────────────

// hostIPC 是宿主自己的一条 mpv IPC 连接（mpv 的命名管道允许多个客户端；internal/player 那条不动）。写走一个
// 有界队列、由后台 goroutine 发：界面线程只往队列里放，mpv 卡住时不会把界面线程拖住（满了就丢并记日志）。
type hostIPC struct {
	conn    net.Conn
	out     chan []byte
	onEvent func(name string, data json.RawMessage)

	mu      sync.Mutex
	nextID  int64
	pending map[int64]ipcPending
	closed  bool
	done    chan struct{}
}

type ipcPending struct {
	name  string
	reply chan ipcReply // nil：不等回复，出错只记日志
}

type ipcReply struct {
	data json.RawMessage
	err  error
}

func newHostIPC(conn net.Conn, onEvent func(string, json.RawMessage)) *hostIPC {
	c := &hostIPC{conn: conn, out: make(chan []byte, ipcOutQueue), onEvent: onEvent,
		pending: map[int64]ipcPending{}, done: make(chan struct{})}
	go c.writeLoop()
	go c.readLoop()
	return c
}

// send 发一条命令、不等回复（失败只记日志，不记参数：里面可能有地址）。队列满或已关返回假。
func (c *hostIPC) send(cmd ...any) bool {
	_, ok := c.enqueue(nil, cmd)
	return ok
}

// request 发一条命令并等回复，最多 timeout。
func (c *hostIPC) request(timeout time.Duration, cmd ...any) (json.RawMessage, error) {
	reply := make(chan ipcReply, 1)
	id, ok := c.enqueue(reply, cmd)
	if !ok {
		return nil, errors.New("IPC 连接已经断开或太忙")
	}
	select {
	case r := <-reply:
		return r.data, r.err
	case <-time.After(timeout):
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, fmt.Errorf("mpv 在 %s 内没有回复", timeout)
	case <-c.done:
		return nil, errors.New("IPC 连接断开了")
	}
}

func (c *hostIPC) enqueue(reply chan ipcReply, cmd []any) (int64, bool) {
	name := fmt.Sprint(cmd[0])
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return 0, false
	}
	c.nextID++
	id := c.nextID
	b, err := json.Marshal(map[string]any{"command": cmd, "request_id": id})
	if err != nil {
		c.mu.Unlock()
		log.Printf("desktop: 发给 mpv 的 %s 编不成 JSON：%v", name, err)
		return 0, false
	}
	c.pending[id] = ipcPending{name: name, reply: reply}
	select {
	case c.out <- append(b, '\n'):
		c.mu.Unlock()
		return id, true
	default:
		delete(c.pending, id)
		c.mu.Unlock()
		log.Printf("desktop: 发给 mpv 的命令排满了，丢掉 %s", name)
		return 0, false
	}
}

func (c *hostIPC) writeLoop() {
	for {
		select {
		case b := <-c.out:
			if _, err := c.conn.Write(b); err != nil {
				c.close()
				return
			}
		case <-c.done:
			return
		}
	}
}

func (c *hostIPC) readLoop() {
	defer c.close()
	sc := bufio.NewScanner(c.conn)
	sc.Buffer(make([]byte, 64*1024), 4<<20)
	for sc.Scan() {
		var m struct {
			Event     string          `json:"event"`
			Name      string          `json:"name"`
			Data      json.RawMessage `json:"data"`
			RequestID *int64          `json:"request_id"`
			Error     string          `json:"error"`
		}
		if json.Unmarshal(sc.Bytes(), &m) != nil {
			continue
		}
		switch {
		case m.Event == "property-change":
			c.onEvent(m.Name, m.Data)
		case m.Event == "" && m.RequestID != nil:
			c.deliver(*m.RequestID, m.Data, m.Error)
		}
	}
}

func (c *hostIPC) deliver(id int64, data json.RawMessage, status string) {
	c.mu.Lock()
	p, ok := c.pending[id]
	delete(c.pending, id)
	c.mu.Unlock()
	if !ok {
		return
	}
	var err error
	if status != "success" {
		err = fmt.Errorf("mpv 没接受 %s：%s", p.name, status)
	}
	if p.reply != nil {
		p.reply <- ipcReply{data: data, err: err}
		return
	}
	if err != nil {
		log.Printf("desktop: %v", err)
	}
}

func (c *hostIPC) close() {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.closed = true
	close(c.done)
	c.mu.Unlock()
	_ = c.conn.Close()
}
