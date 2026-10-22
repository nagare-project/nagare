package player

import (
	"bytes"
	"context"
	"errors"
	"log"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nagare-project/nagare/internal/animego"
	"github.com/nagare-project/nagare/internal/library"
)

// 窗口里换集的几条完整流程：倒计时里按 ⏭、三集连切、准备 / 栅栏期间从网页上换着播、倒计时里关窗、
// 连着看时回写失败的留痕、界面脚本没加载时的日志。

// 倒计时里按 ⏭：只 loadfile 一次（倒计时不会再触发第二次换集，第三集在那里等着），回写恰好一次。
func TestSwitchNextDuringCountdown(t *testing.T) {
	client := newNavClient()
	h := newNavHarness(t, client, 3)
	upNextDelay = 300 * time.Millisecond // 在起播之前改：之后事件循环会读它
	replyLoadfile(h.fake, switchEvents()...)
	_, pb := h.startPlaying(t, 0)
	awaitKeepOpen(t, h.fake)
	h.awaitDanmakuOK(t, h.eps[0].FileID)
	reachEnd(t, h.fake, 1439, 1440)
	eventually(t, func() bool { return len(upNextCards(t, h.fake)) == 1 }, "到尾要出卡片")

	uiMessage(t, h.fake, "next")
	h.awaitEpisode(t, h.eps[1].FileID)
	eventually(t, func() bool { return count(h.fake, "set_property", "pause", false) == 1 }, "栅栏落下")
	// 越过原来的倒计时：它若没停掉，会从第二集再切到第三集
	time.Sleep(upNextDelay + 150*time.Millisecond)
	assert.Equal(t, 1, count(h.fake, "loadfile"))
	assert.Equal(t, h.eps[1].FileID, h.m.Status().FileID)
	eventually(t, func() bool { return len(client.marks()) == 1 }, "第一集要回写")
	assert.Equal(t, []int{1}, client.marks())
	assert.Zero(t, h.ends.Load())
	assert.Same(t, pb, h.m.currentPlayback())
	h.awaitDanmakuOK(t, h.eps[1].FileID) // 后台写的 ASS 不与 TempDir 的清理赛跑
}

// 三集连切：keep-open 只开一次、到最后一集关回去；每集的进度各写各的；OnSessionEnd 恰好一次。
func TestSwitchChainOfThree(t *testing.T) {
	h := newNavHarness(t, nil, 3)
	replyLoadfile(h.fake, switchEvents()...)
	_, pb := h.startPlaying(t, 0)
	awaitKeepOpen(t, h.fake)
	for i, pos := range []float64{100, 200} {
		h.fake.PushProperty(t, "duration", 1440.0)
		h.fake.PushProperty(t, "time-pos", pos)
		eventually(t, func() bool { return pb.player.State().TimePos == pos }, "位置没到")
		uiMessage(t, h.fake, "next")
		h.awaitEpisode(t, h.eps[i+1].FileID)
		eventually(t, func() bool { return count(h.fake, "set_property", "pause", false) == i+1 }, "栅栏落下")
	}
	eventually(t, func() bool { return count(h.fake, "set_property", "keep-open", "no") == 1 }, "最后一集要把 keep-open 关回去")
	assert.Equal(t, 1, count(h.fake, "set_property", "keep-open", "yes"), "中间那一集还有下一集：不重复开")
	assert.Equal(t, 2, count(h.fake, "loadfile"))
	for i, want := range []float64{100, 200} {
		p, ok := h.st.Progress(h.eps[i].FileID)
		require.True(t, ok)
		assert.Equal(t, want, p.PositionSec)
	}

	h.fake.Quit(t)
	awaitWindowEnd(t, pb)
	assert.Equal(t, int32(1), h.ends.Load())
	assert.Nil(t, h.m.Status().PlaybackFailure)
}

// assertNewWindowWorks：网页上换着播之后，新窗口照常播、照常写进度、照常收尾（第二次 OnSessionEnd）。
func assertNewWindowWorks(t *testing.T, h *navHarness, res PlayResult, it library.Item) {
	t.Helper()
	pb := h.m.currentPlayback()
	require.NotNil(t, pb)
	assert.Equal(t, res.PlaybackID, pb.id)
	f := h.fakeAt(t, 1)
	f.Push(t, map[string]any{"event": "file-loaded"})
	f.PushProperty(t, "duration", 1440.0)
	f.PushProperty(t, "time-pos", 200.0)
	eventually(t, func() bool {
		st := h.m.Status()
		return st.Playing && st.FileID == it.FileID && st.Position == 200
	}, "新窗口要照常播")
	f.Quit(t)
	awaitWindowEnd(t, pb)
	p, ok := h.st.Progress(it.FileID)
	require.True(t, ok)
	assert.Equal(t, 200.0, p.PositionSec)
	assert.Equal(t, int32(2), h.ends.Load())
}

// 正在准备下一集时网页上点了播放：旧窗口收尾恰好一次、准备被取消、不 loadfile；新窗口照常。
func TestPlayFromWebWhilePreparing(t *testing.T) {
	h := newNavHarness(t, nil, 2)
	h.nav.setGate(make(chan struct{}))
	_, pb := h.startPlaying(t, 0)
	uiMessage(t, h.fake, "next")
	eventually(t, func() bool { return h.nav.openCount() == 1 }, "要开始准备")

	res, err := h.m.Play(context.Background(), NewLocalSource(h.eps[1]), "")
	require.NoError(t, err)
	assertWindowFinished(t, pb)
	eventually(t, func() bool { return errors.Is(h.nav.ctxErr(), context.Canceled) }, "旧窗口的准备要取消")
	assert.Zero(t, count(h.fake, "loadfile"))
	assert.Equal(t, int32(1), h.ends.Load())
	assertNewWindowWorks(t, h, res, h.eps[1])
}

// loadfile 已经发出、新文件还没开始（栅栏里）时网页上点了播放：第二集不写进度，第一集照常写，
// 不记播放失败，旧窗口收尾恰好一次；新窗口照常。
func TestPlayFromWebWhileFenced(t *testing.T) {
	h := newNavHarness(t, nil, 2)
	replyLoadfile(h.fake, map[string]any{"event": "end-file", "reason": "stop"}) // 新文件迟迟不开始
	_, pb := h.startPlaying(t, 0)
	h.fake.PushProperty(t, "duration", 1440.0)
	h.fake.PushProperty(t, "time-pos", 300.0)
	eventually(t, func() bool { return pb.player.State().TimePos == 300 }, "位置没到")
	uiMessage(t, h.fake, "next")
	h.awaitEpisode(t, h.eps[1].FileID)
	assert.Zero(t, h.m.Status().Position, "栅栏里不报上一集的位置")

	res, err := h.m.Play(context.Background(), NewLocalSource(h.eps[0]), "")
	require.NoError(t, err)
	assertWindowFinished(t, pb)
	assert.Equal(t, int32(1), h.ends.Load())
	_, ok := h.st.Progress(h.eps[1].FileID)
	assert.False(t, ok, "第二集没开始，不写进度")
	p, ok := h.st.Progress(h.eps[0].FileID)
	require.True(t, ok)
	assert.Equal(t, 300.0, p.PositionSec)
	assert.Nil(t, h.m.Status().PlaybackFailure)
	assertNewWindowWorks(t, h, res, h.eps[0])
}

// 倒计时里用户关掉窗口：不 loadfile，第一集记为看完、回写恰好一次，OnSessionEnd 恰好一次。
func TestSwitchWindowClosedDuringCountdown(t *testing.T) {
	client := newNavClient()
	h := newNavHarness(t, client, 2)
	upNextDelay = 2 * time.Second // 在起播之前改：之后事件循环会读它
	_, pb := h.startPlaying(t, 0)
	awaitKeepOpen(t, h.fake)
	h.awaitDanmakuOK(t, h.eps[0].FileID)
	reachEnd(t, h.fake, 1439, 1440)
	eventually(t, func() bool { return len(upNextCards(t, h.fake)) == 1 }, "到尾要出卡片")

	h.fake.PushProperty(t, "eof-reached", nil) // 关窗时 mpv 把 eof-reached 置空
	h.fake.Quit(t)
	awaitWindowEnd(t, pb)
	assert.Zero(t, count(h.fake, "loadfile"))
	p, ok := h.st.Progress(h.eps[0].FileID)
	require.True(t, ok)
	assert.True(t, p.Completed)
	assert.Equal(t, []int{1}, client.marks())
	assert.Nil(t, h.m.Status().PlaybackFailure)
	assert.Equal(t, int32(1), h.ends.Load())
}

// failEpisodeClient 回写某一集时 animego 不可达，其余照常。
type failEpisodeClient struct {
	*navClient
	failEp int
}

func (c *failEpisodeClient) MarkWatched(ctx context.Context, anilistID, ep int) error {
	if ep == c.failEp {
		return &animego.Error{Kind: animego.ErrUnavailable, Op: "markWatched"}
	}
	return c.navClient.MarkWatched(ctx, anilistID, ep)
}

// 同一个窗口里连着看：第一集回写失败、第二集成功 —— 横幅说的是第一集没记上，那件事仍然成立，
// 不能被第二集的成功清掉。
func TestSwitchSyncFailureOfPreviousEpisodeSurvivesNextSuccess(t *testing.T) {
	client := &failEpisodeClient{navClient: newNavClient(), failEp: 1}
	h := newNavHarness(t, client, 2)
	replyLoadfile(h.fake, switchEvents()...)
	_, pb := h.startPlaying(t, 0)
	awaitKeepOpen(t, h.fake)
	h.awaitDanmakuOK(t, h.eps[0].FileID)
	reachEnd(t, h.fake, 1439, 1440)
	eventually(t, func() bool { return h.m.Status().Sync != nil }, "第一集回写失败要挂出来")

	h.awaitEpisode(t, h.eps[1].FileID)
	h.awaitDanmakuOK(t, h.eps[1].FileID)
	pushEOF(t, h.fake, 1439, 1440)
	awaitWindowEnd(t, pb)

	assert.Equal(t, []int{2}, client.marks(), "第二集回写成功")
	got := h.m.Status().Sync
	require.NotNil(t, got, "第二集的成功不能清掉第一集的失败")
	assert.Equal(t, h.eps[0].FileID, got.FileID)
	assert.Equal(t, 1, got.Episode)
	assert.Contains(t, got.Reason, "暂不可达")
}

// syncBuffer 是可以并发写读的日志落点（后台 goroutine 也在写日志）。
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// 界面脚本没加载（配置目录没准备好，mpv 用自带的控制条）：试探照常隔一阵推一次，但日志每个窗口只记一次。
func TestProbeUIFailureLoggedOncePerWindow(t *testing.T) {
	var logs syncBuffer
	log.SetOutput(&logs)
	prevProbe := uiProbeInterval
	uiProbeInterval = 5 * time.Millisecond
	// 恢复要排在 m.Stop 之后（Cleanup 后进先出）：事件循环那时已经退出
	t.Cleanup(func() {
		log.SetOutput(os.Stderr)
		uiProbeInterval = prevProbe
	})
	h := newNavHarness(t, nil, 2)
	h.fake.FailOn("script-message-to", "nagare", "state")
	_, pb := h.playSource(t, h.nav.source(h.eps[0]))
	h.fake.Push(t, map[string]any{"event": "file-loaded"})

	attempts := func() int { return count(h.fake, "script-message-to", "nagare", "state") }
	deadline := time.Now().Add(3 * time.Second)
	for pos := 10.0; attempts() < 8; pos++ {
		require.True(t, time.Now().Before(deadline), "试探没有照常进行")
		h.fake.PushProperty(t, "time-pos", pos)
		time.Sleep(10 * time.Millisecond)
	}
	assert.Equal(t, 1, strings.Count(logs.String(), "播放器界面没有响应"), "试探失败每个窗口只记一次")
	assert.Zero(t, count(h.fake, "set_property", "keep-open"), "界面没就绪：不开 keep-open")
	h.fake.Quit(t)
	awaitWindowEnd(t, pb)
}
