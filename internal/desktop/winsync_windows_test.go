//go:build windows

package desktop

import (
	"bufio"
	"encoding/json"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeMpv 是宿主那条 IPC 连接的另一头（net.Pipe）：记下宿主写过来的开关，get_property 照记下的值答，其余命令答成功。
// 不主动发通知 —— mpv 那边自己的改动（Lua 的 cycle fullscreen、Esc 的 set fullscreen no）由测试先 set、再经
// onProperty 报给宿主。
type fakeMpv struct {
	mu     sync.Mutex
	props  map[string]bool
	writes []mpvWrite
}

func (f *fakeMpv) set(name string, v bool) {
	f.mu.Lock()
	f.props[name] = v
	f.mu.Unlock()
}

func (f *fakeMpv) get(name string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.props[name]
}

// takeWrites 是宿主上次取过之后写过来的开关（取完清空）。
func (f *fakeMpv) takeWrites() []mpvWrite {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := f.writes
	f.writes = nil
	return out
}

func (f *fakeMpv) serve(conn net.Conn) {
	sc := bufio.NewScanner(conn)
	for sc.Scan() {
		var m struct {
			Command   []json.RawMessage `json:"command"`
			RequestID int64             `json:"request_id"`
		}
		if json.Unmarshal(sc.Bytes(), &m) != nil || len(m.Command) == 0 {
			continue
		}
		var cmd, name string
		_ = json.Unmarshal(m.Command[0], &cmd)
		if len(m.Command) > 1 {
			_ = json.Unmarshal(m.Command[1], &name)
		}
		reply := map[string]any{"request_id": m.RequestID, "error": "success"}
		f.mu.Lock()
		switch cmd {
		case "set_property":
			var v bool // 只记开关（embedded 之类的字符串、数字不算）
			if len(m.Command) > 2 && json.Unmarshal(m.Command[2], &v) == nil {
				f.props[name] = v
				f.writes = append(f.writes, mpvWrite{name, v})
			}
		case "get_property":
			reply["data"] = f.props[name]
		}
		f.mu.Unlock()
		b, _ := json.Marshal(reply)
		if _, err := conn.Write(append(b, '\n')); err != nil {
			return
		}
	}
}

// fakeSession 起一个接着 fakeMpv 的播放会话（不起 mpv、没有子窗口）：换到画面层、连上 IPC、写初值、收到观察的初值。
func fakeSession(t *testing.T, l *testLoop) (*videoSession, *fakeMpv) {
	t.Helper()
	var prep sessionPrep
	l.onUI(t, func() { prep = l.a.beginSession() })
	require.True(t, prep.ok)
	server, client := net.Pipe()
	f := &fakeMpv{props: map[string]bool{}}
	go f.serve(server)
	s := newVideoSession(prep.id, nil, prep.host)
	c := newHostIPC(client, func(string, json.RawMessage) {})
	s.ipc.Store(c)
	t.Cleanup(func() {
		c.close()
		_ = server.Close()
	})
	l.onUI(t, func() {
		l.a.attachSession(s)
		l.a.seedSession(s)
		for _, name := range windowFlags { // observe_property 报回来的初值
			l.a.onProperty(s, name, json.RawMessage(strconv.FormatBool(f.get(name))))
		}
	})
	flushIPC(t, s)
	return s, f
}

// flushIPC 等宿主那条连接上排着的命令都到了 fakeMpv：同一条连接按顺序，问一个值、回复回来就说明之前的都到了。
func flushIPC(t *testing.T, s *videoSession) {
	t.Helper()
	_, err := s.ipc.Load().request(5*time.Second, "get_property", "pid")
	require.NoError(t, err)
}

// recordTrace 把窗口同步的记录收下来（失败时报出来）。要在 startLoop 之前调：界面线程起来之后再改 testHooks 是数据竞争。
func recordTrace(t *testing.T) func() string {
	t.Helper()
	var mu sync.Mutex
	var lines []string
	old := testHooks.trace
	testHooks.trace = func(line string) {
		mu.Lock()
		lines = append(lines, line)
		mu.Unlock()
	}
	t.Cleanup(func() { testHooks.trace = old }) // 在 startLoop 的收尾（停界面线程）之后才跑
	return func() string {
		mu.Lock()
		defer mu.Unlock()
		return strings.Join(lines, "\n")
	}
}

// mpv 请求的进出全屏（Enter、双击、Esc），宿主照做之后两边本来就一致：进出多少次都不用往回写 fullscreen。
//
// CI 冒烟偶发失败的根因：第二次进全屏起，宿主去掉标题栏（SetWindowLongPtr）时系统当场发一个半路上的 WM_SIZE
// （客户区先变成整个窗口那么大，再铺满显示器）。那一刻宿主看自己「没全屏」，就把 fullscreen=假 写给 mpv、到位后又
// 写真；mpv 的通知取在两下之间时，晚到的假被当成 mpv 要退出全屏 —— 窗口自己弹出来、再进去，来回打乒乓，最后停在
// 哪边看运气（停在全屏里时，之后的 Esc 只退出全屏、回不了媒体库）。
func TestFullscreenFromMpvWritesNothingBack(t *testing.T) {
	trace := recordTrace(t)
	l := startLoop(t, true)
	a := l.a
	s, f := fakeSession(t, l)
	f.takeWrites() // 起播时写的初值

	hostFullscreen := func() bool {
		var on bool
		l.onUI(t, func() { on = a.hostState().Fullscreen })
		return on
	}
	mpvSays := func(name string, v bool) {
		f.set(name, v)
		l.onUI(t, func() { a.onProperty(s, name, json.RawMessage(strconv.FormatBool(v))) })
	}
	for i := 1; i <= 3; i++ {
		mpvSays("fullscreen", true)
		require.True(t, hostFullscreen(), "第 %d 次进全屏", i)
		mpvSays("fullscreen", false)
		require.False(t, hostFullscreen(), "第 %d 次退出全屏", i)
	}
	flushIPC(t, s)
	assert.Empty(t, f.takeWrites(), "宿主不该往回写；同步记录：\n%s", trace())

	// 窗口最大化着时进出全屏：mpv 那边的 window-maximized 跟着变（全屏时不算最大化），fullscreen 照样一个都不写
	l.onUI(t, func() { pShowWindow.Call(a.top, swMaximize) })
	flushIPC(t, s)
	assert.Equal(t, []mpvWrite{{"window-maximized", true}}, f.takeWrites(), "用户最大化：写回真")
	for i := 1; i <= 2; i++ {
		mpvSays("fullscreen", true)
		require.True(t, hostFullscreen(), "最大化着第 %d 次进全屏", i)
		mpvSays("fullscreen", false)
		require.False(t, hostFullscreen(), "最大化着第 %d 次退出全屏", i)
	}
	flushIPC(t, s)
	assert.Equal(t, []mpvWrite{{"window-maximized", false}, {"window-maximized", true}, {"window-maximized", false}, {"window-maximized", true}},
		f.takeWrites(), "只有进出全屏时 window-maximized 的变化；同步记录：\n%s", trace())
}
