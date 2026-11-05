package player

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nagare-project/nagare/internal/library"
	"github.com/nagare-project/nagare/internal/mpv/mpvtest"
)

// keep-open 已经在 mpv 里把这一集停在了最后一帧，可那条到尾的事件还在通道里、没轮到处理，nagare 这时
// 按「没有下一集了」把 keep-open 关掉：停在那里的 mpv 不会因此结束文件（0.41 实测），到尾的事件随后
// 才到，以前一律按「keep-open 关着、mpv 会自己退」不理 —— 窗口就卡在最后一帧上，也没有卡片。
// 假 mpv 照真 mpv 的样子：keep-open=no 之后不会自己结束一个已经停住的文件。

// setParkGrace 改「等 mpv 自己退出」的期限。要在建 harness 之前调：恢复排在 m.Stop 之后（t.Cleanup 后进先出）。
func setParkGrace(t *testing.T, d time.Duration) {
	t.Helper()
	prev := parkGracePeriod
	parkGracePeriod = d
	t.Cleanup(func() { parkGracePeriod = prev })
}

// 换到最后一集时，keep-open 还开着（上一集为倒计时开的），要等这一集的 file-loaded 处理到才关。
// 这一集很短（或者续播在片尾）、事件循环又慢了一步：mpv 已经播到尾停住，到尾的事件紧跟在 file-loaded 后面。
func TestLastEpisodeParkedBeforeKeepOpenOffCloses(t *testing.T) {
	setParkGrace(t, 50*time.Millisecond)
	h := newNavHarness(t, newNavClient(), 2)
	replyLoadfile(h.fake, append(switchEvents(),
		map[string]any{"event": "property-change", "name": "duration", "data": 1440.0},
		map[string]any{"event": "property-change", "name": "time-pos", "data": 1439.0},
		map[string]any{"event": "property-change", "name": "eof-reached", "data": true},
	)...)
	_, pb := h.startPlaying(t, 0)
	awaitKeepOpen(t, h.fake)

	uiMessage(t, h.fake, "next")
	awaitWindowEnd(t, pb, h.fake)

	assert.Equal(t, 1, count(h.fake, "set_property", "keep-open", "no"), "最后一集要把 keep-open 关回去")
	assert.Equal(t, 1, count(h.fake, "quit"), "已经停在最后一帧：要由 nagare 关窗")
	assert.Empty(t, parkedCards(t, h.fake), "最后一集不出倒计时或错误卡片")
	p, ok := h.st.Progress(h.eps[1].FileID)
	require.True(t, ok)
	assert.True(t, p.Completed, "最后一集播到了尾，要记为看完")
	assert.Nil(t, h.m.Status().PlaybackFailure)
	assert.Equal(t, int32(1), h.ends.Load())
}

// 播到片尾附近按 ⏭，下一集却已经被重扫掉了：准备失败 → 重问邻居没有下一集 → 关 keep-open。
// 准备的结果与到尾的事件同时可读时 select 随便挑一个：先挑中准备结果，到尾的事件就排在关 keep-open 之后。
func TestFailedSwitchDropsKeepOpenAfterUnseenParkCloses(t *testing.T) {
	setParkGrace(t, 50*time.Millisecond)
	h := newNavHarness(t, nil, 2)
	_, pb := dropKeepOpenMidEpisode(t, h)
	// mpv 在 keep-open 关掉之前就已经停在了最后一帧：到尾的事件这时才处理到
	reachEnd(t, h.fake, 1439, 1440)
	awaitWindowEnd(t, pb, h.fake)

	assert.Equal(t, 1, count(h.fake, "quit"), "已经停在最后一帧：要由 nagare 关窗")
	assert.Empty(t, parkedCards(t, h.fake), "没有下一集了：不出倒计时或错误卡片")
	p, ok := h.st.Progress(h.eps[0].FileID)
	require.True(t, ok)
	assert.True(t, p.Completed)
	assert.Nil(t, h.m.Status().PlaybackFailure)
}

// 同样晚到的停住，换成网络流在四成处就到了尾：照样关窗，记的是来源故障，不是看完。
func TestUnseenParkOfNetworkStreamIsAPlaybackFailure(t *testing.T) {
	setParkGrace(t, 50*time.Millisecond)
	h := newNavHarness(t, nil, 2)
	h.nav.wrap = func(it library.Item) MediaSource { return uiStreamSource{item: it} }
	res, pb := dropKeepOpenMidEpisode(t, h)
	reachEnd(t, h.fake, 576, 1440)
	awaitWindowEnd(t, pb, h.fake)

	assert.Equal(t, 1, count(h.fake, "quit"), "已经停住：要由 nagare 关窗")
	failure := h.m.Status().PlaybackFailure
	require.NotNil(t, failure)
	assert.Equal(t, res.PlaybackID, failure.PlaybackID)
	p, ok := h.st.Progress(h.eps[0].FileID)
	require.True(t, ok)
	assert.False(t, p.Completed, "提前断供不算看完")
}

// 对照：keep-open 关掉时还远没到尾，之后 mpv 播到尾自己结束文件、退出（keep-open=no 的正常收场）。
// nagare 等它自己退，不去关它；这一集照常记为看完。期限放得很长：窗口在等待上限之内收尾，就只能是 mpv 自己退的。
func TestKeepOpenDroppedEarlyThenNaturalEOF(t *testing.T) {
	setParkGrace(t, time.Minute)
	h := newNavHarness(t, nil, 2)
	_, pb := dropKeepOpenMidEpisode(t, h)

	pushEOF(t, h.fake, 1439, 1440)
	awaitWindowEnd(t, pb, h.fake)

	assert.Zero(t, count(h.fake, "quit"), "mpv 自己会退：不由 nagare 关")
	p, ok := h.st.Progress(h.eps[0].FileID)
	require.True(t, ok)
	assert.True(t, p.Completed)
	assert.Nil(t, h.m.Status().PlaybackFailure)
	assert.Equal(t, int32(1), h.ends.Load())
}

// 等 mpv 自己退的期间拖回去了（eof-reached 落下）：不再等，窗口留着；之后再播到尾 mpv 自己退出。
func TestSeekBackDuringParkGraceKeepsTheWindow(t *testing.T) {
	setParkGrace(t, 300*time.Millisecond)
	h := newNavHarness(t, nil, 2)
	_, pb := dropKeepOpenMidEpisode(t, h)
	h.fake.PushProperty(t, "eof-reached", true)
	h.fake.PushProperty(t, "eof-reached", false)
	h.fake.PushProperty(t, "time-pos", 1200.0)
	awaitProcessed(t, h, 61)

	time.Sleep(2 * parkGracePeriod)
	assertWindowOpen(t, pb)
	assert.Zero(t, count(h.fake, "quit"), "拖回去了：不关窗")

	pushEOF(t, h.fake, 1439, 1440)
	awaitWindowEnd(t, pb, h.fake)
}

// dropKeepOpenMidEpisode 起播第一集（有下一集，keep-open 开着），然后让下一集被重扫掉、按 ⏭：
// 准备失败、重问邻居没有下一集，keep-open 关掉 —— 这时这一集还没到尾（至少 nagare 还没看到）。
func dropKeepOpenMidEpisode(t *testing.T, h *navHarness) (PlayResult, *playback) {
	t.Helper()
	res, pb := h.startPlaying(t, 0)
	awaitKeepOpen(t, h.fake)
	h.nav.setEps(h.eps[:1]) // 下一集被重扫掉了：Open 失败、重问也没有
	uiMessage(t, h.fake, "next")
	eventually(t, func() bool { return count(h.fake, "set_property", "keep-open", "no") == 1 }, "下一集没了：要把 keep-open 关掉")
	assertWindowOpen(t, pb)
	return res, pb
}

// parkedCards 是停在最后一帧时才会出的卡片（倒计时、错误）。「正在准备」不算：准备慢于
// preparingCardDelay（测试里 30ms）时它就会出，满载时说不准。
func parkedCards(t *testing.T, f *mpvtest.Fake) []upNextCard {
	t.Helper()
	var out []upNextCard
	for _, c := range upNextCards(t, f) {
		if c.Phase != "preparing" {
			out = append(out, c)
		}
	}
	return out
}
