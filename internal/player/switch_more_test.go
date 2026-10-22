package player

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	errs "github.com/nagare-project/nagare/internal/errors"
	"github.com/nagare-project/nagare/internal/library"
	"github.com/nagare-project/nagare/internal/mpv"
	"github.com/nagare-project/nagare/internal/store"
)

// 下一集打不开：屏幕提示用分类错误自己的说法与恢复动作（不是分类错误就用通用说法），不 loadfile，
// 留在这一集；邻居重问一次（可能已经不在了）。
func TestSwitchOpenErrorShowsToastAndStays(t *testing.T) {
	tests := []struct {
		name          string
		err           error
		text, subText string
	}{
		{"分类错误", errs.New(errs.CategoryFS, "player.nav", "没有下一集了", "媒体库里已经找不到它"), "没有下一集了", "媒体库里已经找不到它"},
		{"其他错误", errors.New("boom"), "下一集没能打开", "还在播这一集"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := newNavHarness(t, nil, 2)
			h.nav.setOpenErr(tc.err)
			replyLoadfile(h.fake, switchEvents()...)
			_, pb := h.startPlaying(t, 0)
			before := h.nav.calls()

			uiMessage(t, h.fake, "next")
			eventually(t, func() bool { return count(h.fake, "script-message-to", "nagare", "toast", tc.text, tc.subText) == 1 }, "要提示切集失败")
			eventually(t, func() bool { return h.nav.calls() > before }, "失败后要重问邻居")
			assert.Zero(t, count(h.fake, "loadfile"))
			assert.Equal(t, h.eps[0].FileID, h.m.Status().FileID)
			assert.Same(t, pb, h.m.currentPlayback())
		})
	}
}

// 网络流有下一集、在四成处就到了尾：来源故障，关窗并记失败，不出倒计时。
func TestSwitchNetworkPrematureEOFClosesWithoutCountdown(t *testing.T) {
	h := newNavHarness(t, nil, 2)
	h.nav.wrap = func(it library.Item) MediaSource { return uiStreamSource{item: it} }
	res, pb := h.startPlaying(t, 0)
	eventually(t, func() bool { return count(h.fake, "set_property", "keep-open", "yes") == 1 }, "有下一集：要开 keep-open")

	reachEnd(t, h.fake, 576, 1440)
	awaitWindowEnd(t, pb)

	assert.Empty(t, upNextCards(t, h.fake), "提前断供不出倒计时")
	assert.Zero(t, count(h.fake, "loadfile"))
	assert.Equal(t, 1, count(h.fake, "quit"), "要由 nagare 关窗（keep-open 下 mpv 不会自己退）")
	failure := h.m.Status().PlaybackFailure
	require.NotNil(t, failure)
	assert.Equal(t, h.eps[0].FileID, failure.FileID)
	assert.Equal(t, res.PlaybackID, failure.PlaybackID)
	assert.Contains(t, failure.Reason, "没有记为看完")
	p, ok := h.st.Progress(h.eps[0].FileID)
	require.True(t, ok)
	assert.False(t, p.Completed)
	assert.Equal(t, int32(1), h.ends.Load())
}

// keep-open 开着、到尾时下一集已经不在了（重扫过）：照常关窗，这一集记为看完。
func TestSwitchEOFWithVanishedNextCloses(t *testing.T) {
	h := newNavHarness(t, nil, 2)
	_, pb := h.startPlaying(t, 0)
	eventually(t, func() bool { return count(h.fake, "set_property", "keep-open", "yes") == 1 }, "要开 keep-open")
	pb.episode().setNav(Neighbors{}) // 下一集刚被重扫掉，keep-open 还没来得及关

	reachEnd(t, h.fake, 1439, 1440)
	awaitWindowEnd(t, pb)
	assert.Empty(t, upNextCards(t, h.fake))
	p, ok := h.st.Progress(h.eps[0].FileID)
	require.True(t, ok)
	assert.True(t, p.Completed)
	assert.Nil(t, h.m.Status().PlaybackFailure)
}

// stateNav 取出最近一次界面状态里的 nav。
func stateNav(t *testing.T, h *windowHarness) map[string]any {
	t.Helper()
	nav, ok := lastUIState(t, h.fake)["nav"].(map[string]any)
	require.True(t, ok, "界面状态里每次都要带 nav")
	return nav
}

// 没有 Navigator：nav 恒为两个 false、从不碰 keep-open、起播参数照旧；收到 next 什么都不做。
func TestSwitchNilNavigatorUnchanged(t *testing.T) {
	h := newWindowHarness(t, nil)
	res, pb := h.play(t)
	h.fake.Push(t, map[string]any{"event": "file-loaded"})
	uiMessage(t, h.fake, "ready")
	uiMessage(t, h.fake, "next")
	uiMessage(t, h.fake, "upnext-cancel")
	eventually(t, func() bool { return count(h.fake, "script-message-to", "nagare", "state") >= 2 }, "要推状态")

	assert.Equal(t, map[string]any{"prev": false, "next": false}, stateNav(t, h))
	assert.Zero(t, count(h.fake, "set_property", "keep-open"))
	assert.Equal(t, mpv.LaunchOptions{MediaPath: h.item.AbsPath, Title: "测试番剧", SocketDir: h.m.opts.RuntimeDir}, h.launch)
	pushEOF(t, h.fake, 1439, 1440)
	awaitWindowEnd(t, pb)
	assert.Zero(t, count(h.fake, "loadfile"))
	assert.Zero(t, count(h.fake, "script-message-to", "nagare", "upnext"))
	assert.Zero(t, count(h.fake, "script-message-to", "nagare", "toast"))
	assert.Equal(t, int32(1), h.ends.Load())
	assert.NotEmpty(t, res.PlaybackID)
}

// 在线来源：有 Navigator 也不问邻居、不开 keep-open。
func TestSwitchRemoteSourceHasNoNav(t *testing.T) {
	h := newNavHarness(t, nil, 2)
	src := NewRemoteSource(RemoteSourceOptions{SourceID: "web", CandidateID: "c1", URL: "https://example.test/ep1.m3u8", Title: "测试番剧", Episode: 1})
	h.playSource(t, src)
	h.fake.Push(t, map[string]any{"event": "file-loaded"})
	uiMessage(t, h.fake, "ready")
	eventually(t, func() bool { return count(h.fake, "script-message-to", "nagare", "state") >= 2 }, "要推状态")

	assert.Equal(t, map[string]any{"prev": false, "next": false}, stateNav(t, h.windowHarness))
	assert.Zero(t, count(h.fake, "set_property", "keep-open"))
	assert.Zero(t, h.nav.calls(), "在线来源不问 Navigator")
}

// 本地文件有邻居：界面状态带着按钮与集号说法。
func TestSwitchUIStateCarriesNav(t *testing.T) {
	h := newNavHarness(t, nil, 3)
	h.startPlaying(t, 1)
	eventually(t, func() bool { return count(h.fake, "script-message-to", "nagare", "state") >= 2 }, "要推状态")
	assert.Equal(t, map[string]any{"prev": true, "next": true, "prevLabel": "第 1 集", "nextLabel": "第 3 集"}, stateNav(t, h.windowHarness))
}

// 换集的栅栏期间（新文件还没开始）界面消息照常处理：音量、画质增强存进设置并套到窗口上。
func TestSwitchUIMessagesHandledInsideFence(t *testing.T) {
	h := newNavHarness(t, nil, 2)
	h.m.opts.Shaders = func() []string {
		if h.st.PlayerConfig().Anime4K == mpv.Anime4KHQ {
			return []string{"/rt/anime4k/hq.glsl"}
		}
		return nil
	}
	replyLoadfile(h.fake, map[string]any{"event": "end-file", "reason": "stop"}) // 新文件迟迟不开始
	h.startPlaying(t, 0)
	uiMessage(t, h.fake, "next")
	h.awaitEpisode(t, h.eps[1].FileID)

	uiMessage(t, h.fake, "volume", "70")
	uiMessage(t, h.fake, "anime4k", "hq")
	eventually(t, func() bool {
		v := h.st.PlayerConfig().Volume
		return v != nil && *v == 70
	}, "栅栏里的音量消息要处理")
	eventually(t, func() bool {
		return count(h.fake, "set_property", "glsl-shaders", []any{"/rt/anime4k/hq.glsl"}) == 1
	}, "栅栏里的画质增强消息要处理")
	assert.Zero(t, count(h.fake, "set_property", "pause", false), "新文件还没开始：栅栏还在")

	h.fake.Push(t, map[string]any{"event": "start-file"})
	eventually(t, func() bool { return count(h.fake, "set_property", "pause", false) == 1 }, "start-file 让栅栏落下")
}

// 换过来的这一集有外挂对白字幕：文件加载好时先 sub-add 它并选中，再挂弹幕轨（编排要数对白字幕）。
func TestSwitchSidecarSubtitleBeforeDanmaku(t *testing.T) {
	h := newNavHarness(t, newNavClient(), 2)
	h.nav.subs[h.eps[1].FileID] = "/subs/ep2.ass"
	replyLoadfile(h.fake, switchEvents()...)
	_, pb := h.startPlaying(t, 0)
	h.awaitDanmakuOK(t, h.eps[0].FileID)

	uiMessage(t, h.fake, "next")
	h.awaitDanmakuOK(t, h.eps[1].FileID)
	ass := pb.episode().assTarget
	eventually(t, func() bool { return count(h.fake, "sub-add", ass) == 1 }, "第二集的弹幕轨要挂上")

	sidecar := indexOfCommand(h.fake, "sub-add", "/subs/ep2.ass", "select")
	require.GreaterOrEqual(t, sidecar, 0, "要外挂第二集的对白字幕")
	assert.Less(t, sidecar, indexOfCommand(h.fake, "sub-add", ass), "对白字幕要在弹幕轨之前挂")
	assert.Greater(t, sidecar, indexOfCommand(h.fake, "loadfile"))
	assert.Equal(t, 1, count(h.fake, "sub-add", "/subs/ep2.ass"))
}

// ⏮：换到上一集，续播位置按那一集的进度来。
func TestSwitchPrev(t *testing.T) {
	h := newNavHarness(t, nil, 3)
	require.NoError(t, h.st.SetProgress(h.eps[0].FileID, store.Progress{PositionSec: 120, DurationSec: 1440}))
	replyLoadfile(h.fake, switchEvents()...)
	h.startPlaying(t, 1)

	uiMessage(t, h.fake, "prev")
	h.awaitEpisode(t, h.eps[0].FileID)
	assert.Equal(t, []any{"loadfile", h.eps[0].AbsPath, "replace"}, h.fake.CommandsWith("loadfile")[0])
	assert.Equal(t, 1, count(h.fake, "set_property", "start", "+120.000"), "上一集看到一半：从那里接着播")
	eventually(t, func() bool {
		nav, _ := lastUIState(t, h.fake)["nav"].(map[string]any)
		return nav["prev"] == false && nav["next"] == true
	}, "换过去之后按新的一集推按钮")
}

// 准备得慢：出「正在准备」卡片；这期间再按 ⏭ 提示「正在切换…」；准备好了照常切过去并收起卡片。
func TestSwitchSlowPrepareShowsCardAndRejectsRepeats(t *testing.T) {
	h := newNavHarness(t, nil, 2)
	gate := make(chan struct{})
	h.nav.setGate(gate)
	replyLoadfile(h.fake, switchEvents()...)
	h.startPlaying(t, 0)

	uiMessage(t, h.fake, "next")
	eventually(t, func() bool { return len(upNextCards(t, h.fake)) == 1 }, "准备得慢要出卡片")
	assert.Equal(t, upNextCard{Phase: "preparing", Label: "第 2 集"}, upNextCards(t, h.fake)[0])
	uiMessage(t, h.fake, "next")
	eventually(t, func() bool { return count(h.fake, "script-message-to", "nagare", "toast", "正在切换…") == 1 }, "切换中再按要提示")

	close(gate)
	h.awaitEpisode(t, h.eps[1].FileID)
	assert.Equal(t, 1, count(h.fake, "loadfile"))
	eventually(t, func() bool { return count(h.fake, "script-message-to", "nagare", "upnext-clear") == 1 }, "切过去之后收起卡片")
}

// 准备期间关掉窗口：准备被取消，不 loadfile；第一集照常收尾，OnSessionEnd 恰好一次。
func TestSwitchWindowClosedWhilePreparing(t *testing.T) {
	h := newNavHarness(t, nil, 2)
	h.nav.setGate(make(chan struct{}))
	_, pb := h.startPlaying(t, 0)
	h.fake.PushProperty(t, "duration", 1440.0)
	h.fake.PushProperty(t, "time-pos", 300.0)
	eventually(t, func() bool { return pb.player.State().TimePos == 300 }, "位置没到")

	uiMessage(t, h.fake, "next")
	eventually(t, func() bool { return h.nav.openCount() == 1 }, "要开始准备")
	h.fake.Quit(t)
	awaitWindowEnd(t, pb)

	eventually(t, func() bool { return errors.Is(h.nav.ctxErr(), context.Canceled) }, "窗口没了要取消准备")
	assert.Zero(t, count(h.fake, "loadfile"))
	p, ok := h.st.Progress(h.eps[0].FileID)
	require.True(t, ok)
	assert.Equal(t, 300.0, p.PositionSec)
	assert.Equal(t, int32(1), h.ends.Load())
}

// 卡片在准备中点取消：放弃这次切换，准备的结果来了也不切。
func TestSwitchCancelWhilePreparing(t *testing.T) {
	h := newNavHarness(t, nil, 2)
	gate := make(chan struct{})
	h.nav.setGate(gate)
	replyLoadfile(h.fake, switchEvents()...)
	h.startPlaying(t, 0)

	uiMessage(t, h.fake, "next")
	eventually(t, func() bool { return len(upNextCards(t, h.fake)) == 1 }, "准备得慢要出卡片")
	uiMessage(t, h.fake, "upnext-cancel")
	eventually(t, func() bool { return errors.Is(h.nav.ctxErr(), context.Canceled) }, "取消要传到准备里")
	// 先取消准备、再收卡片：准备那边看到取消的时候，收卡片的消息可能还没发出去
	eventually(t, func() bool { return count(h.fake, "script-message-to", "nagare", "upnext-clear") == 1 }, "取消要收起卡片")
	assert.Zero(t, count(h.fake, "loadfile"))
	assert.Equal(t, h.eps[0].FileID, h.m.Status().FileID)

	// 取消之后还能再切
	h.nav.setGate(nil)
	uiMessage(t, h.fake, "next")
	h.awaitEpisode(t, h.eps[1].FileID)
	assert.Zero(t, h.nav.discardCount(), "换上去了的不 Discard")
}

// 取消之后来源才准备好（磁力缓冲刚好到齐）：不切，准备好的来源交回去 Discard，恰好一次。
func TestSwitchPreparedAfterCancelIsDiscarded(t *testing.T) {
	h := newNavHarness(t, nil, 2)
	gate := make(chan struct{})
	h.nav.setGate(gate)
	h.nav.mu.Lock()
	h.nav.ignoreCancel = true
	h.nav.mu.Unlock()
	replyLoadfile(h.fake, switchEvents()...)
	_, pb := h.startPlaying(t, 0)

	uiMessage(t, h.fake, "next")
	eventually(t, func() bool { return len(upNextCards(t, h.fake)) == 1 }, "准备得慢要出卡片")
	uiMessage(t, h.fake, "upnext-cancel")
	eventually(t, func() bool { return count(h.fake, "script-message-to", "nagare", "upnext-clear") == 1 }, "取消要收起卡片")
	close(gate)

	eventually(t, func() bool { return h.nav.discardCount() == 1 }, "取消之后才准备好的来源要 Discard")
	awaitProcessed(t, h, 61)
	assert.Zero(t, count(h.fake, "loadfile"))
	assert.Equal(t, h.eps[0].FileID, h.m.Status().FileID)
	assert.Same(t, pb, h.m.currentPlayback())
	assert.Equal(t, 1, h.nav.discardCount())
}
