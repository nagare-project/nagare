package mpv

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 验收：attach 后自动注册四个属性观察，命令形状与顺序固定；初始状态带媒体路径。
func TestAttach_ObservesProperties(t *testing.T) {
	f := newFakeMPV(t)
	p := attachTestPlayer(t, f, "/fake/a.mkv")

	// attach 返回即代表四条 observe_property 已应答完毕，记录是确定的
	require.Equal(t, [][]any{
		{"observe_property", float64(1), "time-pos"},
		{"observe_property", float64(2), "pause"},
		{"observe_property", float64(3), "duration"},
		{"observe_property", float64(4), "eof-reached"},
	}, f.commands())

	require.Equal(t, "/fake/a.mkv", p.State().Path)
}

// 验收：request_id 递增配对 —— 并发请求各自拿到自己的响应。
func TestCall_RequestResponsePairing(t *testing.T) {
	f := newFakeMPV(t)
	p := attachTestPlayer(t, f, "")

	const n = 10
	for i := 0; i < n; i++ {
		f.setProp(fmt.Sprintf("prop-%d", i), fmt.Sprintf("value-%d", i))
	}

	var wg sync.WaitGroup
	results := make([]string, n)
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs[i] = p.GetProperty(fmt.Sprintf("prop-%d", i), &results[i])
		}(i)
	}
	wg.Wait()

	for i := 0; i < n; i++ {
		require.NoError(t, errs[i])
		require.Equal(t, fmt.Sprintf("value-%d", i), results[i], "第 %d 个并发请求拿错了响应", i)
	}
}

// 验收：各命令方法发出的 IPC 命令形状正确。
func TestCommands_WireShape(t *testing.T) {
	f := newFakeMPV(t)
	p := attachTestPlayer(t, f, "")

	// 方法返回即代表 server 已记录该命令（serve 先记录后应答）
	require.NoError(t, p.SetPause(true))
	require.Equal(t, []any{"set_property", "pause", true}, f.lastCommand(t))

	require.NoError(t, p.SetPause(false))
	require.Equal(t, []any{"set_property", "pause", false}, f.lastCommand(t))

	require.NoError(t, p.Seek(90.5))
	require.Equal(t, []any{"seek", 90.5, "absolute"}, f.lastCommand(t))

	require.NoError(t, p.LoadSub("/tmp/danmaku.ass"))
	require.Equal(t, []any{"sub-add", "/tmp/danmaku.ass"}, f.lastCommand(t))

	require.NoError(t, p.ReloadSub())
	require.Equal(t, []any{"sub-reload"}, f.lastCommand(t))
}

// 验收：观察属性的推送流入 Events() 并更新 State() 快照。
func TestEvents_PropertyChangeFlow(t *testing.T) {
	f := newFakeMPV(t)
	p := attachTestPlayer(t, f, "/fake/a.mkv")

	f.pushEvent(t, map[string]any{"event": "property-change", "id": 1, "name": "time-pos", "data": 12.5})
	ev := waitEventKind(t, p, EventTimePos)
	require.Equal(t, 12.5, ev.TimePos)
	require.Equal(t, 12.5, p.State().TimePos) // emit 前已写状态，此处必然可见

	f.pushEvent(t, map[string]any{"event": "property-change", "id": 2, "name": "pause", "data": true})
	require.True(t, waitEventKind(t, p, EventPause).Paused)
	require.True(t, p.State().Paused)

	f.pushEvent(t, map[string]any{"event": "property-change", "id": 3, "name": "duration", "data": 3600.0})
	require.Eventually(t, func() bool { return p.State().Duration == 3600.0 },
		2*time.Second, 10*time.Millisecond, "duration 未更新进状态快照")

	// time-pos 推 null（mpv 停止时会发）：必须保留最后已知值，进度回写靠它
	f.pushEvent(t, map[string]any{"event": "property-change", "id": 1, "name": "time-pos", "data": nil})
	f.pushEvent(t, map[string]any{"event": "property-change", "id": 2, "name": "pause", "data": false}) // 顺序标记
	waitEventKind(t, p, EventPause)
	require.Equal(t, 12.5, p.State().TimePos, "null 推送不应清掉最后已知的播放位置")
}

// 验收：file-loaded 触发 FileLoaded 事件，并用 get_property 刷新一次 path。
func TestEvents_FileLoadedRefreshesPath(t *testing.T) {
	f := newFakeMPV(t)
	p := attachTestPlayer(t, f, "/fake/initial.mkv")

	f.setProp("path", "/served/real.mkv")
	f.pushEvent(t, map[string]any{"event": "file-loaded"})

	waitEventKind(t, p, EventFileLoaded)
	require.Eventually(t, func() bool { return p.State().Path == "/served/real.mkv" },
		2*time.Second, 10*time.Millisecond, "file-loaded 后应通过 get_property 刷新 path")
}

// 验收：end-file 转成 Ended 事件并带原因；eof-reached 与 end-file 只产生一次 Ended。
func TestEvents_EndedOnce(t *testing.T) {
	f := newFakeMPV(t)
	p := attachTestPlayer(t, f, "")

	f.pushEvent(t, map[string]any{"event": "property-change", "id": 4, "name": "eof-reached", "data": true})
	f.pushEvent(t, map[string]any{"event": "end-file", "reason": "eof"})
	f.pushEvent(t, map[string]any{"event": "property-change", "id": 2, "name": "pause", "data": true}) // 顺序标记

	require.Equal(t, "eof", waitEventKind(t, p, EventEnded).Reason)
	// 读到标记事件为止，中途不允许出现第二个 Ended
	deadline := time.After(3 * time.Second)
	for {
		select {
		case ev := <-p.Events():
			require.NotEqual(t, EventEnded, ev.Kind, "Ended 事件重复")
			if ev.Kind == EventPause {
				return
			}
		case <-deadline:
			t.Fatal("等待顺序标记事件超时")
		}
	}
}

// 验收（计划点名的失败模式）：server 断开（模拟 mpv 被强杀）后 Done() 关闭、
// Err() 非 nil、State() 保留最后已知值、Events() 收到 Died。
func TestDisconnect_ConvergesAndKeepsLastState(t *testing.T) {
	f := newFakeMPV(t)
	p := attachTestPlayer(t, f, "/fake/a.mkv")

	f.pushEvent(t, map[string]any{"event": "property-change", "id": 1, "name": "time-pos", "data": 99.5})
	f.pushEvent(t, map[string]any{"event": "property-change", "id": 3, "name": "duration", "data": 1440.0})
	require.Eventually(t, func() bool { return p.State().TimePos == 99.5 && p.State().Duration == 1440.0 },
		2*time.Second, 10*time.Millisecond)

	f.closeConn()

	requireDoneWithin(t, p, 3*time.Second)
	require.Error(t, p.Err(), "非正常断开应有终态错误")
	waitEventKind(t, p, EventDied)

	got := p.State()
	require.Equal(t, 99.5, got.TimePos, "进程死后必须保留最后已知进度")
	require.Equal(t, 1440.0, got.Duration)
	require.Equal(t, "/fake/a.mkv", got.Path)
}

// 验收：断开时挂起的命令立即失败返回，不干等 5 秒超时。
func TestDisconnect_UnblocksPendingCall(t *testing.T) {
	f := newFakeMPV(t)
	p := attachTestPlayer(t, f, "")

	f.setSilent(true)
	errCh := make(chan error, 1)
	go func() { errCh <- p.GetProperty("time-pos", nil) }()

	// 等命令抵达 server（静默不应答），再断开
	require.Eventually(t, func() bool {
		for _, cmd := range f.commands() {
			if len(cmd) > 0 && cmd[0] == "get_property" {
				return true
			}
		}
		return false
	}, 2*time.Second, 10*time.Millisecond)
	f.closeConn()

	select {
	case err := <-errCh:
		require.Error(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("断开后挂起的命令未及时返回（不应等满命令超时）")
	}
}

// 验收：Close 幂等且可并发；正常关闭后 Err() 为 nil。
func TestClose_IdempotentAndGraceful(t *testing.T) {
	f := newFakeMPV(t)
	p := attachTestPlayer(t, f, "")

	require.NoError(t, p.Close())
	requireDoneWithin(t, p, time.Second)
	require.NoError(t, p.Err(), "主动 Close 引发的退出应视为正常")
	require.Equal(t, []any{"quit"}, f.lastCommand(t))
	waitEventKind(t, p, EventDied)

	// 重复与并发调用都应立即返回且无错误
	start := time.Now()
	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			assert.NoError(t, p.Close())
		}()
	}
	wg.Wait()
	require.Less(t, time.Since(start), time.Second, "重复 Close 不应再走等待流程")
}

// 验收：不消费 Events() 时读循环不被卡住 —— 丢旧事件策略生效，命令通路仍可用。
func TestEvents_DropOldestNeverBlocks(t *testing.T) {
	f := newFakeMPV(t)
	p := attachTestPlayer(t, f, "")

	const pushes = eventBufferSize * 3
	for i := 0; i < pushes; i++ {
		f.pushEvent(t, map[string]any{"event": "property-change", "id": 1, "name": "time-pos", "data": float64(i)})
	}

	// 事件全部写入 socket 之后才发起请求：应答排在事件之后，
	// 请求能返回就证明读循环把积压事件全部处理完且没有阻塞
	f.setProp("mpv-version", "mpv 0.41.0")
	var version string
	require.NoError(t, p.GetProperty("mpv-version", &version))
	require.Equal(t, "mpv 0.41.0", version)

	require.Equal(t, float64(pushes-1), p.State().TimePos, "状态应反映最新一条推送")
	require.LessOrEqual(t, len(p.Events()), eventBufferSize)

	// 丢的是旧事件：缓冲里最后一条应是最新推送
	var last Event
	for {
		select {
		case ev := <-p.Events():
			last = ev
			continue
		default:
		}
		break
	}
	require.Equal(t, EventTimePos, last.Kind)
	require.Equal(t, float64(pushes-1), last.TimePos)
}
