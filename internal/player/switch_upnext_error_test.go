package player

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	errs "github.com/nagare-project/nagare/internal/errors"
)

// 停在最后一帧等下一集时换集失败。真 mpv 0.41 上停在尾巴上把 keep-open 关掉并不会关窗：
// 只弹一条两秒的提示，窗口就卡在最后一帧上。还有下一集就挂错误卡片等重试 / 关窗，没有了就关窗。

// 倒计时到点、下一集打不开（下一集还在邻居里）：挂错误卡片，不弹一闪而过的提示，不 loadfile，窗口留着；
// 点重试马上换成「正在准备」，这次打开成功就换过去。
func TestUpNextOpenFailureAtEOFShowsErrorCard(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		reason string
	}{
		{"分类错误", errs.New(errs.CategoryFS, "player.nav", "下一集的文件不见了：x.mkv", "重新扫描媒体库后再试"), "下一集的文件不见了：x.mkv。重新扫描媒体库后再试"},
		{"媒体库说没有了、邻居却还在", errs.New(errs.CategoryFS, "player.nav", "没有下一集了", "媒体库里已经找不到它"), "没有下一集了。媒体库里已经找不到它"},
		{"其他错误", errors.New("boom"), "可以重试，或者关掉窗口回到网页上选这一集"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := newNavHarness(t, nil, 2)
			h.nav.setOpenErr(tc.err)
			replyLoadfile(h.fake, switchEvents()...)
			_, pb := h.startPlaying(t, 0)
			awaitKeepOpen(t, h.fake)
			reachEnd(t, h.fake, 1439, 1440)

			eventually(t, func() bool { return len(upNextCards(t, h.fake)) == 2 }, "倒计时之后要挂错误卡片")
			assert.Equal(t, upNextCard{Phase: "error", Label: "第 2 集", Title: "下一集没能打开", Reason: tc.reason}, upNextCards(t, h.fake)[1])
			assert.Zero(t, count(h.fake, "script-message-to", "nagare", "toast"), "停在最后一帧时不弹一闪而过的提示")
			assert.Zero(t, count(h.fake, "loadfile"))
			assert.Zero(t, count(h.fake, "quit"), "还有下一集：窗口留着")
			assertWindowOpen(t, pb)
			assert.True(t, h.m.Status().Playing)

			h.nav.setOpenErr(nil)
			uiMessage(t, h.fake, "upnext-retry")
			h.awaitEpisode(t, h.eps[1].FileID)
			assert.Equal(t, 1, count(h.fake, "loadfile"))
			cards := upNextCards(t, h.fake)
			require.Len(t, cards, 3)
			assert.Equal(t, upNextCard{Phase: "preparing", Label: "第 2 集"}, cards[2], "重试要马上换成「正在准备」")
			eventually(t, func() bool { return count(h.fake, "script-message-to", "nagare", "upnext-clear") == 1 }, "换过去之后收起卡片")
			assert.Same(t, pb, h.m.currentPlayback())
		})
	}
}

// 错误卡片上点「关闭窗口」：关窗，第一集记为看完、回写一次，不是播放失败，OnSessionEnd 恰好一次。
func TestUpNextErrorCardCloseEndsWindow(t *testing.T) {
	client := newNavClient()
	h := newNavHarness(t, client, 2)
	h.nav.setOpenErr(errors.New("boom"))
	_, pb := h.startPlaying(t, 0)
	awaitKeepOpen(t, h.fake)
	h.awaitDanmakuOK(t, h.eps[0].FileID)
	reachEnd(t, h.fake, 1439, 1440)
	eventually(t, func() bool { return len(upNextCards(t, h.fake)) == 2 }, "要挂错误卡片")

	uiMessage(t, h.fake, "upnext-close")
	awaitWindowEnd(t, pb)
	assert.Equal(t, 1, count(h.fake, "quit"))
	assert.Zero(t, count(h.fake, "loadfile"))
	p, ok := h.st.Progress(h.eps[0].FileID)
	require.True(t, ok)
	assert.True(t, p.Completed)
	assert.Equal(t, []int{1}, client.marks())
	assert.Nil(t, h.m.Status().PlaybackFailure)
	assert.False(t, h.m.Status().Playing)
	assert.Equal(t, int32(1), h.ends.Load())
}

// 错误卡片上按 Esc：收起卡片、停在最后一帧不关窗；卡片收起之后迟到的「重试」不理。
func TestUpNextErrorCardCancelStaysParked(t *testing.T) {
	h := newNavHarness(t, nil, 2)
	h.nav.setOpenErr(errors.New("boom"))
	_, pb := h.startPlaying(t, 0)
	awaitKeepOpen(t, h.fake)
	reachEnd(t, h.fake, 1439, 1440)
	eventually(t, func() bool { return len(upNextCards(t, h.fake)) == 2 }, "要挂错误卡片")
	opens := h.nav.openCount()

	uiMessage(t, h.fake, "upnext-cancel")
	eventually(t, func() bool { return count(h.fake, "script-message-to", "nagare", "upnext-clear") == 1 }, "要收起卡片")
	uiMessage(t, h.fake, "upnext-retry")
	awaitProcessed(t, h, 55)
	assert.Equal(t, opens, h.nav.openCount(), "卡片收起之后的重试不理")
	assert.Zero(t, count(h.fake, "quit"))
	assertWindowOpen(t, pb)
}

// 到尾时下一集已经被重扫掉了（会话手里的邻居还没更新）：倒计时到点打不开、重问也没有了 → 照最后一集
// 播完关窗，不挂错误卡片，第一集记为看完，不是播放失败。
func TestUpNextNextVanishedAtEOFClosesWindow(t *testing.T) {
	h := newNavHarness(t, nil, 2)
	_, pb := h.startPlaying(t, 0)
	awaitKeepOpen(t, h.fake)
	h.nav.setEps(h.eps[:1])

	reachEnd(t, h.fake, 1439, 1440)
	awaitWindowEnd(t, pb)
	assert.Equal(t, []upNextCard{{Phase: "countdown", Label: "第 2 集", Seconds: 1}}, upNextCards(t, h.fake), "不挂错误卡片")
	assert.Zero(t, count(h.fake, "loadfile"))
	assert.Equal(t, 1, count(h.fake, "quit"), "要由 nagare 关窗：停在尾巴上关 keep-open 不会关窗")
	p, ok := h.st.Progress(h.eps[0].FileID)
	require.True(t, ok)
	assert.True(t, p.Completed)
	assert.Nil(t, h.m.Status().PlaybackFailure)
	assert.Equal(t, int32(1), h.ends.Load())
}

// LoadNext 必须复位的一项设不上（set start 失败）、停在最后一帧：错误卡片说「没能切到下一集」，
// 这一集原样留着；点关闭窗口照常收尾。
func TestUpNextLoadNextFailureAtEOFShowsErrorCard(t *testing.T) {
	h := newNavHarness(t, nil, 2)
	h.fake.FailOn("set_property", "start")
	_, pb := h.startPlaying(t, 0)
	awaitKeepOpen(t, h.fake)
	reachEnd(t, h.fake, 1439, 1440)

	eventually(t, func() bool { return len(upNextCards(t, h.fake)) == 2 }, "要挂错误卡片")
	assert.Equal(t, upNextCard{Phase: "error", Label: "第 2 集", Title: "没能切到下一集", Reason: "关掉播放器后从网页上重新播放"}, upNextCards(t, h.fake)[1])
	assert.Zero(t, count(h.fake, "loadfile"))
	assert.Equal(t, h.eps[0].FileID, h.m.Status().FileID)

	uiMessage(t, h.fake, "upnext-close")
	awaitWindowEnd(t, pb)
	p, ok := h.st.Progress(h.eps[0].FileID)
	require.True(t, ok)
	assert.True(t, p.Completed)
	assert.Equal(t, int32(1), h.ends.Load())
}

// 看到一半按 ⏭、LoadNext 设不上：弹屏幕提示、留在这一集，还能再试；关窗时这一集照常收尾。
func TestSwitchLoadNextFailureMidEpisodeStays(t *testing.T) {
	h := newNavHarness(t, nil, 2)
	h.fake.FailOn("set_property", "start")
	_, pb := h.startPlaying(t, 0)
	h.fake.PushProperty(t, "duration", 1440.0)
	h.fake.PushProperty(t, "time-pos", 300.0)
	eventually(t, func() bool { return pb.player.State().TimePos == 300 }, "位置没到")

	toast := func() int {
		return count(h.fake, "script-message-to", "nagare", "toast", "没能切到下一集", "关掉播放器后从网页上重新播放")
	}
	uiMessage(t, h.fake, "next")
	eventually(t, func() bool { return toast() == 1 }, "要提示没切过去")
	assert.Zero(t, count(h.fake, "loadfile"))
	assert.Equal(t, h.eps[0].FileID, h.m.Status().FileID)
	eventually(t, func() bool { return h.nav.discardCount() == 1 }, "准备好的下一集没换上去：要告诉来源（磁力换回原来那一集）")
	uiMessage(t, h.fake, "next")
	eventually(t, func() bool { return toast() == 2 }, "失败之后还能再试")
	assert.Empty(t, upNextCards(t, h.fake), "还在播这一集：不挂错误卡片")

	h.fake.Quit(t)
	awaitWindowEnd(t, pb)
	p, ok := h.st.Progress(h.eps[0].FileID)
	require.True(t, ok)
	assert.Equal(t, 300.0, p.PositionSec)
	assert.False(t, p.Completed)
	assert.Equal(t, int32(1), h.ends.Load())
}

// 倒计时到点开始准备之后用户拖回去（eof-reached 落下）：这次准备是「下一集」那一套发起的，一并取消，
// 卡片收起，不 loadfile；之后照常能切。
func TestUpNextSeekBackCancelsAutoPreparation(t *testing.T) {
	h := newNavHarness(t, nil, 2)
	h.nav.setGate(make(chan struct{}))
	replyLoadfile(h.fake, switchEvents()...)
	_, pb := h.startPlaying(t, 0)
	awaitKeepOpen(t, h.fake)
	reachEnd(t, h.fake, 1439, 1440)
	eventually(t, func() bool { return h.nav.openCount() == 1 }, "倒计时到点要开始准备")

	h.fake.PushProperty(t, "eof-reached", false)
	eventually(t, func() bool { return errors.Is(h.nav.ctxErr(), context.Canceled) }, "拖回去要取消自动发起的准备")
	eventually(t, func() bool { return count(h.fake, "script-message-to", "nagare", "upnext-clear") == 1 }, "要收起卡片")
	assert.Zero(t, count(h.fake, "loadfile"))
	assertWindowOpen(t, pb)

	h.nav.setGate(nil)
	uiMessage(t, h.fake, "next")
	h.awaitEpisode(t, h.eps[1].FileID)
}

// 用户在最后一刻按了 ⏭（正在准备）时播到尾：不另起倒计时，第一集立刻记为看完；之后拖回去也不取消
// 这次准备（是用户自己要换的）；准备好了照常换过去，回写恰好一次。
func TestSwitchUserPreparationSurvivesEOFAndSeekBack(t *testing.T) {
	client := newNavClient()
	h := newNavHarness(t, client, 2)
	gate := make(chan struct{})
	h.nav.setGate(gate)
	replyLoadfile(h.fake, switchEvents()...)
	h.startPlaying(t, 0)
	awaitKeepOpen(t, h.fake)
	h.awaitDanmakuOK(t, h.eps[0].FileID)
	uiMessage(t, h.fake, "next")
	eventually(t, func() bool { return h.nav.openCount() == 1 }, "要开始准备")

	reachEnd(t, h.fake, 1439, 1440)
	eventually(t, func() bool {
		p, ok := h.st.Progress(h.eps[0].FileID)
		return ok && p.Completed
	}, "到尾要立刻记为看完")
	for _, c := range upNextCards(t, h.fake) {
		assert.NotEqual(t, "countdown", c.Phase, "已经在准备：不另起倒计时")
	}
	h.fake.PushProperty(t, "eof-reached", false)
	awaitProcessed(t, h, 60)
	assert.NoError(t, h.nav.ctxErr(), "用户按的 ⏭ 不因拖回去取消")
	assert.Zero(t, count(h.fake, "loadfile"))

	close(gate)
	h.awaitEpisode(t, h.eps[1].FileID)
	assert.Equal(t, 1, count(h.fake, "loadfile"))
	eventually(t, func() bool { return len(client.marks()) == 1 }, "第一集要回写")
	assert.Equal(t, []int{1}, client.marks())
	// 等第二集的弹幕落盘再结束：否则后台写的 ASS 会和 TempDir 的清理赛跑
	h.awaitDanmakuOK(t, h.eps[1].FileID)
}
