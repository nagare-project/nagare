package mpv

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	errs "github.com/nagare-project/nagare/internal/errors"
	"github.com/nagare-project/nagare/internal/mpv/mpvtest"
)

// commandsSince 返回 attach 之后第 n 条起的命令（前面是四条 observe_property）。
func commandsSince(f *fakeMPV, n int) [][]any {
	cmds := f.commands()
	if len(cmds) < n {
		return nil
	}
	return cmds[n:]
}

// LoadNext 的完整命令序列：起播位置与外挂字幕 → 轨道选择回 auto → loadfile → 标题、弹幕轨与可见性。
// loadfile 必须正好三个元素：第四个位置在 mpv 0.38 前后含义不同（旧版是 options，新版插进了 index），
// 不带最稳 —— 每个文件的选项都靠前面的 set_property 设好。
func TestLoadNext_CommandSequence(t *testing.T) {
	f := newFakeMPV(t)
	p := attachTestPlayer(t, f, "/a.mkv")
	base := len(f.commands())

	require.NoError(t, p.LoadNext(NextFile{Path: "/v/ep02.mkv", Title: "葬送的芙莉莲 第2集", StartAt: 12}))

	assert.Equal(t, [][]any{
		{"set_property", "start", "+12.000"},
		{"set_property", "sub-files", []any{}},
		{"set_property", "sid", "auto"},
		{"set_property", "aid", "auto"},
		{"loadfile", "/v/ep02.mkv", "replace"},
		{"set_property", "force-media-title", "葬送的芙莉莲 第2集"},
		{"set_property", "title", "葬送的芙莉莲 第2集"},
		{"set_property", "secondary-sid", "no"},
		{"set_property", "sub-visibility", true},
		{"set_property", "secondary-sub-visibility", true},
	}, commandsSince(f, base))
}

// 从头播：start 设回 none（不能留着上一集的 --start=+N）；没有标题时只清掉强制的 media-title。
func TestLoadNext_FromStartWithoutTitle(t *testing.T) {
	f := newFakeMPV(t)
	p := attachTestPlayer(t, f, "/a.mkv")
	base := len(f.commands())

	require.NoError(t, p.LoadNext(NextFile{Path: "http://127.0.0.1:8823/stream/cap/t/abc/2"}))

	cmds := commandsSince(f, base)
	require.NotEmpty(t, cmds)
	assert.Equal(t, []any{"set_property", "start", "none"}, cmds[0])
	assert.Len(t, f.CommandsWith("set_property", "force-media-title", ""), 1)
	assert.Empty(t, f.CommandsWith("set_property", "title"), "没有标题时窗口标题不动")
	loads := f.CommandsWith("loadfile")
	require.Len(t, loads, 1)
	assert.Equal(t, []any{"loadfile", "http://127.0.0.1:8823/stream/cap/t/abc/2", "replace"}, loads[0])
}

// 起播位置或外挂字幕复位失败：不能发 loadfile —— 下一集会从上一集的续播点起播、挂着上一集的字幕。
// 原文件照常播，错误按播放类给出用户能做的动作。
func TestLoadNext_RequiredResetFailureSendsNoLoadfile(t *testing.T) {
	for _, prop := range []string{"start", "sub-files"} {
		t.Run(prop, func(t *testing.T) {
			f := newFakeMPV(t)
			f.FailOn("set_property", prop)
			p := attachTestPlayer(t, f, "/a.mkv")

			err := p.LoadNext(NextFile{Path: "/v/ep02.mkv", Title: "t", StartAt: 30})

			var ce *errs.E
			require.ErrorAs(t, err, &ce)
			assert.Equal(t, errs.CategoryPlayback, ce.Category)
			assert.Equal(t, "player.nav", ce.Op)
			assert.Equal(t, "没能切到下一集", ce.UserMsg)
			assert.Equal(t, "关掉播放器后从网页上重新播放", ce.Recovery)
			assert.Empty(t, f.CommandsWith("loadfile"), "必须复位的一项失败了就不能换文件")
		})
	}
}

// 其余复位失败（老 mpv 没有 secondary-sid 之类）不挡换集。
func TestLoadNext_BestEffortFailureStillLoads(t *testing.T) {
	f := newFakeMPV(t)
	f.FailOn("set_property", "aid")
	f.FailOn("set_property", "secondary-sid")
	f.FailOn("set_property", "force-media-title")
	p := attachTestPlayer(t, f, "/a.mkv")

	require.NoError(t, p.LoadNext(NextFile{Path: "/v/ep02.mkv", Title: "t"}))
	assert.Len(t, f.CommandsWith("loadfile", "/v/ep02.mkv", "replace"), 1)
	assert.Len(t, f.CommandsWith("set_property", "secondary-sub-visibility", true), 1, "一条失败不影响后面的复位")
}

// loadfile 被拒：换集失败，原来那一集原样留着 —— 弹幕副轨、可见性、标题一个都不能动。
// 错误里不能出现流端点地址：能力段等同凭证，不能经错误进日志与界面。
func TestLoadNext_LoadfileRejectedLeavesOldFileAndRedactsURL(t *testing.T) {
	const streamURL = "http://127.0.0.1:8823/stream/0123456789abcdef0123456789abcdef/t/abc/2"
	f := newFakeMPV(t)
	f.OnCommand("loadfile", func([]any) *mpvtest.Reply { return &mpvtest.Reply{Error: "invalid parameter"} })
	p := attachTestPlayer(t, f, "/a.mkv")

	err := p.LoadNext(NextFile{Path: streamURL, Title: "第2集"})
	var ce *errs.E
	require.ErrorAs(t, err, &ce)
	assert.Equal(t, "没能切到下一集", ce.UserMsg)
	assert.NotContains(t, err.Error(), streamURL)
	assert.NotContains(t, err.Error(), "0123456789abcdef0123456789abcdef", "能力段不能出现在错误里")
	assert.Contains(t, err.Error(), "invalid parameter", "mpv 自己的错误文字要留着，排查靠它")
	for _, name := range []string{"secondary-sid", "sub-visibility", "secondary-sub-visibility", "force-media-title", "title"} {
		assert.Empty(t, f.CommandsWith("set_property", name), "loadfile 被拒时不能动上一集的 %s", name)
	}
}

// 卡住的 mpv：一条命令没应答就收手，不发 loadfile，也不为后面每条命令各等一次超时。
func TestLoadNext_HungMPVStopsAfterFirstTimeout(t *testing.T) {
	saved := commandTimeout
	commandTimeout = 150 * time.Millisecond
	t.Cleanup(func() { commandTimeout = saved })

	f := newFakeMPV(t)
	p := attachTestPlayer(t, f, "/a.mkv")
	f.OnCommand("set_property", func(cmd []any) *mpvtest.Reply {
		if mpvtest.HasPrefix(cmd, "set_property", "sid") {
			return &mpvtest.Reply{Hang: true}
		}
		return nil
	})

	began := time.Now()
	err := p.LoadNext(NextFile{Path: "/v/ep02.mkv"})
	require.Error(t, err)
	assert.Less(t, time.Since(began), 3*commandTimeout, "只该等一次超时")
	assert.Empty(t, f.CommandsWith("set_property", "aid"), "没应答之后不再发后面的复位")
	assert.Empty(t, f.CommandsWith("loadfile"))
}

// mpv 已经退出：一条命令都不再发。
func TestLoadNext_DeadPlayerSendsNothing(t *testing.T) {
	f := newFakeMPV(t)
	p := attachTestPlayer(t, f, "/a.mkv")
	f.closeConn()
	requireDoneWithin(t, p, 3*time.Second)
	base := len(f.commands())

	err := p.LoadNext(NextFile{Path: "/v/ep02.mkv"})
	var ce *errs.E
	require.ErrorAs(t, err, &ce)
	assert.Equal(t, "player.nav", ce.Op)
	assert.Empty(t, commandsSince(f, base))
}

func TestUpNextAndKeepOpen_WireShape(t *testing.T) {
	f := newFakeMPV(t)
	p := attachTestPlayer(t, f, "")

	require.NoError(t, p.SendUIUpNext(map[string]any{"phase": "countdown", "label": "第 5 集", "seconds": 5}))
	cmd := f.lastCommand(t)
	require.Len(t, cmd, 4)
	assert.Equal(t, []any{"script-message-to", "nagare", "upnext"}, cmd[:3])
	assert.JSONEq(t, `{"phase":"countdown","label":"第 5 集","seconds":5}`, cmd[3].(string))

	require.NoError(t, p.ClearUIUpNext())
	assert.Equal(t, []any{"script-message-to", "nagare", "upnext-clear"}, f.lastCommand(t))

	require.NoError(t, p.SetKeepOpen(true))
	assert.Equal(t, []any{"set_property", "keep-open", "yes"}, f.lastCommand(t))
	require.NoError(t, p.SetKeepOpen(false))
	assert.Equal(t, []any{"set_property", "keep-open", "no"}, f.lastCommand(t))
}

// 钩子随应答推事件：loadfile 之后 mpv 依次报上一个文件结束、新文件开始、新文件加载好。
func TestFakeHookPushesEventsAfterReply(t *testing.T) {
	f := newFakeMPV(t)
	f.OnCommand("loadfile", func([]any) *mpvtest.Reply {
		return &mpvtest.Reply{Events: []map[string]any{
			{"event": "end-file", "reason": "stop"},
			{"event": "start-file"},
			{"event": "file-loaded"},
		}}
	})
	p := attachTestPlayer(t, f, "/a.mkv")

	require.NoError(t, p.LoadNext(NextFile{Path: "/b.mkv"}))
	assert.Equal(t, "stop", waitEventKind(t, p, EventEnded).Reason)
	waitEventKind(t, p, EventFileStarted)
	waitEventKind(t, p, EventFileLoaded)
}
