package mpv

import (
	"bufio"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// fakeMPV 是测试用的假 mpv：一个按 JSON IPC 协议应答的 unix socket server。
// 它记录收到的每条命令、按 props 表应答 get_property、可主动推事件流、
// 可静默（不应答）、可模拟被强杀（直接断开，不发 shutdown 事件）。
type fakeMPV struct {
	t        *testing.T
	ln       net.Listener
	sockPath string

	connReady chan struct{}

	mu     sync.Mutex // 保护 conn / cmds / props / silent
	conn   net.Conn
	cmds   [][]any
	props  map[string]any
	silent bool

	writeMu sync.Mutex // 序列化应答与推送事件的写
}

// newFakeMPV 启动假 mpv。socket 放在独立短路径临时目录下，
// 避开 darwin 上 sun_path 104 字节的长度上限（t.TempDir 路径可能过长）。
func newFakeMPV(t *testing.T) *fakeMPV {
	t.Helper()
	dir, err := os.MkdirTemp("", "nagfake")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	sockPath := filepath.Join(dir, "m.sock")
	ln, err := net.Listen("unix", sockPath)
	require.NoError(t, err)

	f := &fakeMPV{
		t:         t,
		ln:        ln,
		sockPath:  sockPath,
		connReady: make(chan struct{}),
		props:     make(map[string]any),
	}
	go f.acceptLoop()
	t.Cleanup(f.stop)
	return f
}

func (f *fakeMPV) acceptLoop() {
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

// serve 逐行读命令：记录 → 按命令类型生成 data → 应答。
// 收到 quit 时模拟真 mpv：应答 success、广播 shutdown 事件、关闭连接。
func (f *fakeMPV) serve(conn net.Conn) {
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
		f.writeJSON(map[string]any{"request_id": req.RequestID, "error": "success", "data": data})
		if name == "quit" {
			f.writeJSON(map[string]any{"event": "shutdown"})
			_ = conn.Close()
			return
		}
	}
}

func (f *fakeMPV) writeJSON(v any) {
	b, err := json.Marshal(v)
	if err != nil {
		f.t.Errorf("fakeMPV: 序列化失败: %v", err)
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

// dial 返回客户端侧连接，供 attach 使用。
func (f *fakeMPV) dial(t *testing.T) net.Conn {
	t.Helper()
	conn, err := net.Dial("unix", f.sockPath)
	require.NoError(t, err)
	return conn
}

// pushEvent 主动向客户端推一条事件行（等 server 侧连接就绪）。
func (f *fakeMPV) pushEvent(t *testing.T, ev map[string]any) {
	t.Helper()
	select {
	case <-f.connReady:
	case <-time.After(2 * time.Second):
		t.Fatal("fakeMPV: 等待连接建立超时")
	}
	f.writeJSON(ev)
}

// closeConn 模拟 mpv 被强杀：直接断开，不发 shutdown 事件。
func (f *fakeMPV) closeConn() {
	f.mu.Lock()
	conn := f.conn
	f.mu.Unlock()
	if conn != nil {
		_ = conn.Close()
	}
}

func (f *fakeMPV) stop() {
	_ = f.ln.Close()
	f.closeConn()
}

func (f *fakeMPV) setProp(name string, value any) {
	f.mu.Lock()
	f.props[name] = value
	f.mu.Unlock()
}

func (f *fakeMPV) setSilent(v bool) {
	f.mu.Lock()
	f.silent = v
	f.mu.Unlock()
}

// commands 返回已收到命令的快照副本。
func (f *fakeMPV) commands() [][]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([][]any, len(f.cmds))
	copy(out, f.cmds)
	return out
}

// lastCommand 返回最近一条命令（要求至少收到过一条）。
func (f *fakeMPV) lastCommand(t *testing.T) []any {
	t.Helper()
	cmds := f.commands()
	require.NotEmpty(t, cmds, "fakeMPV 尚未收到任何命令")
	return cmds[len(cmds)-1]
}

// attachTestPlayer 建立客户端连接并 attach 一个无进程 Player。
func attachTestPlayer(t *testing.T, f *fakeMPV, initialPath string) *Player {
	t.Helper()
	p, err := attach(f.dial(t), initialPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = p.Close() })
	return p
}

// waitEventKind 从 Events() 读取直到出现指定类别的事件（跳过其余），超时即失败。
func waitEventKind(t *testing.T, p *Player, kind EventKind) Event {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		select {
		case ev := <-p.Events():
			if ev.Kind == kind {
				return ev
			}
		case <-deadline:
			t.Fatalf("等待 %v 事件超时", kind)
			return Event{}
		}
	}
}

// requireDoneWithin 断言 Done() 在限时内关闭。
func requireDoneWithin(t *testing.T, p *Player, d time.Duration) {
	t.Helper()
	select {
	case <-p.Done():
	case <-time.After(d):
		t.Fatal("Done() 未在限时内关闭")
	}
}
