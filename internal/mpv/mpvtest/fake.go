// Package mpvtest 提供测试用的假 mpv：一个按 JSON IPC 协议应答的 unix socket server。
//
// 放在独立的包里（而不是 package mpv 的 _test 文件）是为了让 internal/player 这类上层包也能用它
// 驱动一个真正的 mpv.Player：mpv.Attach(fake.Dial(t), path)。它刻意不 import internal/mpv ——
// mpv 包自己的测试也用它，反向依赖就成了循环引用。
package mpvtest

import (
	"bufio"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"
)

// connTimeout 是等待客户端连上来的上限。
const connTimeout = 2 * time.Second

// Reply 是钩子对一条命令的应答。
type Reply struct {
	Data  any    // 应答的 data 字段
	Error string // mpv 的错误串；空 = success
	// Events 在应答写出之后按顺序推给客户端 —— 模拟 loadfile 之后 mpv 陆续发的
	// end-file / start-file / file-loaded。
	Events []map[string]any
	// Close 在应答（与 Events）之后断开连接，不发 shutdown，模拟 mpv 被强杀。
	Close bool
}

// Hook 决定一条命令怎么应答；返回 nil 走默认应答。在 server 的读循环里同步调用，不得阻塞。
type Hook func(cmd []any) *Reply

// Fake 是假 mpv。它记录收到的每条命令、按属性表应答 get_property、可主动推事件流、
// 可静默（不应答）、可模拟被强杀（直接断开，不发 shutdown 事件），还可以给特定命令挂钩子。
type Fake struct {
	t        testing.TB
	ln       net.Listener
	sockPath string

	connReady chan struct{}

	mu     sync.Mutex // 保护 conn / cmds / props / silent / hooks
	conn   net.Conn
	cmds   [][]any
	props  map[string]any
	silent bool
	hooks  map[string][]Hook

	writeMu sync.Mutex // 序列化应答与推送事件的写
}

// New 启动假 mpv。socket 放在独立短路径临时目录下，
// 避开 darwin 上 sun_path 104 字节的长度上限（t.TempDir 路径可能过长）。
func New(t testing.TB) *Fake {
	t.Helper()
	dir, err := os.MkdirTemp("", "nagfake")
	if err != nil {
		t.Fatalf("mpvtest: 建临时目录失败: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	sockPath := filepath.Join(dir, "m.sock")
	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("mpvtest: 监听 %s 失败: %v", sockPath, err)
	}

	f := &Fake{
		t:         t,
		ln:        ln,
		sockPath:  sockPath,
		connReady: make(chan struct{}),
		props:     make(map[string]any),
		hooks:     make(map[string][]Hook),
	}
	go f.acceptLoop()
	t.Cleanup(f.stop)
	return f
}

func (f *Fake) acceptLoop() {
	conn, err := f.ln.Accept()
	if err != nil {
		return // listener 已关闭（测试结束）
	}
	f.mu.Lock()
	f.conn = conn
	f.mu.Unlock()
	close(f.connReady)
	f.serve(conn)
}

// serve 逐行读命令：记录 → 钩子或默认规则生成应答 → 写回。
// 默认规则下收到 quit 时模拟真 mpv：应答 success、广播 shutdown 事件、关闭连接。
func (f *Fake) serve(conn net.Conn) {
	sc := bufio.NewScanner(conn)
	for sc.Scan() {
		var req struct {
			Command   []any           `json:"command"`
			RequestID json.RawMessage `json:"request_id"`
		}
		if json.Unmarshal(sc.Bytes(), &req) != nil || len(req.Command) == 0 {
			continue
		}
		name, _ := req.Command[0].(string)

		f.mu.Lock()
		f.cmds = append(f.cmds, req.Command)
		silent := f.silent
		hooks := append([]Hook(nil), f.hooks[name]...)
		var data any
		if name == "get_property" && len(req.Command) >= 2 {
			if key, ok := req.Command[1].(string); ok {
				data = f.props[key]
			}
		}
		f.mu.Unlock()

		if silent {
			continue
		}
		reply := runHooks(hooks, req.Command)
		if reply != nil {
			if !f.answer(conn, req.RequestID, *reply) {
				return
			}
			continue
		}
		f.writeJSON(map[string]any{"request_id": req.RequestID, "error": "success", "data": data})
		if name == "quit" {
			f.writeJSON(map[string]any{"event": "shutdown"})
			_ = conn.Close()
			return
		}
	}
}

// runHooks 依次问钩子，第一个给出应答的说了算。
func runHooks(hooks []Hook, cmd []any) *Reply {
	for _, h := range hooks {
		if r := h(cmd); r != nil {
			return r
		}
	}
	return nil
}

// answer 写出钩子给的应答与随后的事件；返回 false 表示连接已按钩子要求断开。
func (f *Fake) answer(conn net.Conn, requestID json.RawMessage, r Reply) bool {
	status := r.Error
	if status == "" {
		status = "success"
	}
	f.writeJSON(map[string]any{"request_id": requestID, "error": status, "data": r.Data})
	for _, ev := range r.Events {
		f.writeJSON(ev)
	}
	if r.Close {
		_ = conn.Close()
		return false
	}
	return true
}

func (f *Fake) writeJSON(v any) {
	b, err := json.Marshal(v)
	if err != nil {
		f.t.Errorf("mpvtest: 序列化失败: %v", err)
		return
	}
	f.mu.Lock()
	conn := f.conn
	f.mu.Unlock()
	if conn == nil {
		return
	}
	f.writeMu.Lock()
	defer f.writeMu.Unlock()
	_, _ = conn.Write(append(b, '\n'))
}

// SockPath 是 socket 的路径。
func (f *Fake) SockPath() string { return f.sockPath }

// Dial 返回客户端侧连接，交给 mpv.Attach。
func (f *Fake) Dial(t testing.TB) net.Conn {
	t.Helper()
	conn, err := net.Dial("unix", f.sockPath)
	if err != nil {
		t.Fatalf("mpvtest: 连接假 mpv 失败: %v", err)
	}
	return conn
}

// Push 主动向客户端推一条事件行（等 server 侧连接就绪）。
func (f *Fake) Push(t testing.TB, ev map[string]any) {
	t.Helper()
	select {
	case <-f.connReady:
	case <-time.After(connTimeout):
		t.Fatal("mpvtest: 等待连接建立超时")
	}
	f.writeJSON(ev)
}

// PushProperty 推一条 property-change（观察 id 不重要，客户端按属性名认）。
func (f *Fake) PushProperty(t testing.TB, name string, data any) {
	t.Helper()
	f.Push(t, map[string]any{"event": "property-change", "name": name, "data": data})
}

// CloseConn 模拟 mpv 被强杀：直接断开，不发 shutdown 事件。
func (f *Fake) CloseConn() {
	f.mu.Lock()
	conn := f.conn
	f.mu.Unlock()
	if conn != nil {
		_ = conn.Close()
	}
}

// Quit 模拟用户在窗口里关掉 mpv：广播 shutdown 再断开（与真 mpv 正常退出一致）。
func (f *Fake) Quit(t testing.TB) {
	t.Helper()
	f.Push(t, map[string]any{"event": "shutdown"})
	f.CloseConn()
}

func (f *Fake) stop() {
	_ = f.ln.Close()
	f.CloseConn()
}

// SetProp 设定 get_property 对某个属性的应答。
func (f *Fake) SetProp(name string, value any) {
	f.mu.Lock()
	f.props[name] = value
	f.mu.Unlock()
}

// SetSilent 让假 mpv 收命令不应答（模拟卡死）。
func (f *Fake) SetSilent(v bool) {
	f.mu.Lock()
	f.silent = v
	f.mu.Unlock()
}

// OnCommand 给命令名为 name 的命令挂一个钩子（可挂多个，按挂上的顺序问）。
func (f *Fake) OnCommand(name string, h Hook) {
	f.mu.Lock()
	f.hooks[name] = append(f.hooks[name], h)
	f.mu.Unlock()
}

// FailOn 让以 prefix 开头的命令（比如 "set_property", "start"）一律以 mpv 错误应答。
func (f *Fake) FailOn(prefix ...any) {
	if len(prefix) == 0 {
		return
	}
	name, _ := prefix[0].(string)
	f.OnCommand(name, func(cmd []any) *Reply {
		if !HasPrefix(cmd, prefix...) {
			return nil
		}
		return &Reply{Error: "error running command"}
	})
}

// Commands 返回已收到命令的快照副本。
func (f *Fake) Commands() [][]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([][]any, len(f.cmds))
	copy(out, f.cmds)
	return out
}

// CommandsWith 返回以 prefix 开头的命令（按收到的顺序）。
func (f *Fake) CommandsWith(prefix ...any) [][]any {
	var out [][]any
	for _, cmd := range f.Commands() {
		if HasPrefix(cmd, prefix...) {
			out = append(out, cmd)
		}
	}
	return out
}

// LastCommand 返回最近一条命令（要求至少收到过一条）。
func (f *Fake) LastCommand(t testing.TB) []any {
	t.Helper()
	cmds := f.Commands()
	if len(cmds) == 0 {
		t.Fatal("mpvtest: 尚未收到任何命令")
	}
	return cmds[len(cmds)-1]
}

// HasPrefix 报告 cmd 是否以 prefix 开头。JSON 解出来的数字是 float64，比较前把 prefix 里的 int 换成 float64。
func HasPrefix(cmd []any, prefix ...any) bool {
	if len(cmd) < len(prefix) {
		return false
	}
	for i, want := range prefix {
		if n, ok := want.(int); ok {
			want = float64(n)
		}
		// DeepEqual 而不是 !=：参数里可能有数组（sub-files []），两边都是切片时 != 会 panic
		if !reflect.DeepEqual(cmd[i], want) {
			return false
		}
	}
	return true
}
