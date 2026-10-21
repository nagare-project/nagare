package player

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nagare-project/nagare/internal/animego"
	"github.com/nagare-project/nagare/internal/library"
	"github.com/nagare-project/nagare/internal/mpv"
	"github.com/nagare-project/nagare/internal/mpv/mpvtest"
	"github.com/nagare-project/nagare/internal/store"
)

// windowHarness 用假 mpv 驱动一个真正的 mpv.Player：Options.Launch 换成 mpv.Attach(假连接)，
// 事件循环、进度、收尾走的都是生产代码。
type windowHarness struct {
	m      *Manager
	st     *store.Store
	fake   *mpvtest.Fake
	item   library.Item
	ends   atomic.Int32
	launch mpv.LaunchOptions
}

func newWindowHarness(t *testing.T, client AnimegoClient) *windowHarness {
	t.Helper()
	m, st, dir := newTestManager(t, client)
	h := &windowHarness{m: m, st: st, fake: mpvtest.New(t), item: testItem(t, dir, 3)}
	// 弹幕轨挂上之后 track-list 里要找得到它，编排才算成功
	h.fake.SetProp("track-list", []map[string]any{{"id": 1, "type": "sub", "title": danmakuTrackTitle, "external": true}})
	m.opts.OnSessionEnd = func() { h.ends.Add(1) }
	m.opts.Launch = func(_ context.Context, o mpv.LaunchOptions) (*mpv.Player, error) {
		h.launch = o
		return mpv.Attach(h.fake.Dial(t), o.MediaPath)
	}
	t.Cleanup(m.Stop)
	return h
}

// play 起播并返回结果与窗口。
func (h *windowHarness) play(t *testing.T) (PlayResult, *playback) {
	t.Helper()
	res, err := h.m.Play(context.Background(), NewLocalSource(h.item), "")
	require.NoError(t, err)
	pb := h.m.currentPlayback()
	require.NotNil(t, pb)
	return res, pb
}

// awaitWindowEnd 等窗口收尾全部做完（包括后台回写）。
func awaitWindowEnd(t *testing.T, pb *playback) {
	t.Helper()
	select {
	case <-pb.finished:
	case <-time.After(5 * time.Second):
		t.Fatal("窗口没有在 5 秒内收尾")
	}
}

func matchedClient() *fakeClient {
	return &fakeClient{loggedIn: true, matchRes: animego.MatchResult{
		Matched: true, AnilistID: 42, TitleChinese: "测试番剧",
		EpisodeMap: map[int]animego.EpisodeRef{3: {DandanEpisodeID: 420003, Title: "第3话"}},
	}}
}

// 一集播到尾、mpv 自己退出：进度记为看完、账号回写恰好一次、OnSessionEnd 恰好一次，
// 弹幕文件删掉，这一集的 ctx 取消。没有 Navigator 时整个过程不碰 keep-open。
func TestWindowSingleEpisodeEOFFinalizesOnce(t *testing.T) {
	client := matchedClient()
	h := newWindowHarness(t, client)
	res, pb := h.play(t)
	require.NotEmpty(t, res.PlaybackID)
	assert.Equal(t, "loading", res.Danmaku.State)

	h.fake.Push(t, map[string]any{"event": "file-loaded"})
	require.Eventually(t, func() bool { return h.m.Status().Danmaku.State == "ok" }, 3*time.Second, 10*time.Millisecond)
	require.Eventually(t, func() bool { return len(h.fake.CommandsWith("set_property", "sid", 1)) == 1 },
		3*time.Second, 10*time.Millisecond, "弹幕解析完、文件加载好之后要编排一次字幕轨")
	sess := pb.episode()

	h.fake.PushProperty(t, "duration", 1440.0)
	h.fake.PushProperty(t, "time-pos", 1439.0)
	h.fake.PushProperty(t, "eof-reached", true)
	h.fake.Push(t, map[string]any{"event": "end-file", "reason": "eof"})
	h.fake.Quit(t)
	awaitWindowEnd(t, pb)

	p, ok := h.st.Progress(h.item.FileID)
	require.True(t, ok)
	assert.True(t, p.Completed)
	assert.True(t, p.Synced)
	assert.Equal(t, []int{3}, client.marked, "看完回写恰好一次")
	assert.Equal(t, 1, client.ensured)
	assert.Equal(t, int32(1), h.ends.Load(), "OnSessionEnd 恰好一次")
	assert.False(t, h.m.Status().Playing)
	assert.Nil(t, h.m.Status().PlaybackFailure)
	assert.NoFileExists(t, sess.assTarget, "这一集的弹幕文件要删掉")
	assert.Error(t, sess.ctx.Err(), "这一集收尾后它的 ctx 要取消")
	assert.Empty(t, h.fake.CommandsWith("set_property", "keep-open"), "没有 Navigator 时不碰 keep-open")
	assert.Len(t, h.fake.CommandsWith("set_property", "sid"), 1, "字幕轨只编排一次")
}

// 看到一半用户关掉窗口：记下最后位置、不算看完、不回写账号、不是播放失败。
func TestWindowClosedMidEpisodeKeepsPosition(t *testing.T) {
	client := matchedClient()
	h := newWindowHarness(t, client)
	_, pb := h.play(t)

	h.fake.Push(t, map[string]any{"event": "file-loaded"})
	h.fake.PushProperty(t, "duration", 1440.0)
	h.fake.PushProperty(t, "time-pos", 300.0)
	h.fake.Quit(t)
	awaitWindowEnd(t, pb)

	p, ok := h.st.Progress(h.item.FileID)
	require.True(t, ok)
	assert.Equal(t, 300.0, p.PositionSec)
	assert.False(t, p.Completed)
	assert.Empty(t, client.marked)
	assert.Equal(t, int32(1), h.ends.Load())
	assert.Nil(t, h.m.Status().PlaybackFailure, "用户自己关窗不是播放失败")
}

// 网页上点停止：Stop 返回时进度已经落盘、OnSessionEnd 已经跑过。
func TestWindowStopFromAPI(t *testing.T) {
	h := newWindowHarness(t, nil)
	_, pb := h.play(t)

	h.fake.Push(t, map[string]any{"event": "file-loaded"})
	h.fake.PushProperty(t, "duration", 1440.0)
	h.fake.PushProperty(t, "time-pos", 300.0)
	h.m.Stop()

	select {
	case <-pb.finished:
	default:
		t.Fatal("Stop 返回时窗口还没收尾")
	}
	assert.Len(t, h.fake.CommandsWith("quit"), 1)
	p, ok := h.st.Progress(h.item.FileID)
	require.True(t, ok)
	assert.Equal(t, 300.0, p.PositionSec)
	assert.False(t, p.Completed)
	assert.Equal(t, int32(1), h.ends.Load())
	assert.False(t, h.m.Status().Playing)
	assert.Nil(t, h.m.Status().PlaybackFailure)
}

// 媒体打不开（end-file error）：失败带着 fileId 和这个窗口的 playbackId，调用方据此只处理自己起的窗口。
func TestWindowFailureCarriesPlaybackID(t *testing.T) {
	h := newWindowHarness(t, nil)
	res, pb := h.play(t)

	h.fake.Push(t, map[string]any{"event": "end-file", "reason": "error"})
	h.fake.Quit(t)
	awaitWindowEnd(t, pb)

	failure := h.m.Status().PlaybackFailure
	require.NotNil(t, failure)
	assert.Equal(t, h.item.FileID, failure.FileID)
	assert.Equal(t, res.PlaybackID, failure.PlaybackID)
	assert.Equal(t, int32(1), h.ends.Load())
}

// 界面消息照常处理：ready 推一次状态并记下界面已就绪；音量、画质增强存进设置并套到窗口上。
func TestWindowHandlesUIMessages(t *testing.T) {
	h := newWindowHarness(t, nil)
	h.m.opts.Shaders = func() []string {
		if h.st.PlayerConfig().Anime4K == mpv.Anime4KHQ {
			return []string{"/rt/anime4k/hq.glsl"}
		}
		return nil
	}
	_, pb := h.play(t)
	uiMessage := func(args ...string) {
		h.fake.Push(t, map[string]any{"event": "client-message", "args": append([]string{"nagare-event"}, args...)})
	}

	stateMessages := func() int { return len(h.fake.CommandsWith("script-message-to", "nagare", "state")) }
	before := stateMessages()
	uiMessage("ready")
	require.Eventually(t, func() bool { return pb.uiReady.Load() && stateMessages() > before }, 3*time.Second, 10*time.Millisecond)

	uiMessage("volume", "70")
	require.Eventually(t, func() bool {
		v := h.st.PlayerConfig().Volume
		return v != nil && *v == 70
	}, 3*time.Second, 10*time.Millisecond)

	uiMessage("anime4k", "hq")
	require.Eventually(t, func() bool {
		return len(h.fake.CommandsWith("set_property", "glsl-shaders", []any{"/rt/anime4k/hq.glsl"})) == 1
	}, 3*time.Second, 10*time.Millisecond, "换了档位要套到正在播放的窗口上")
	assert.Equal(t, mpv.Anime4KHQ, h.st.PlayerConfig().Anime4K)
}

// 没有 Navigator 时起播参数照旧：本地文件、标题、续播位置都按原来的规则给。
func TestWindowLaunchOptionsUnchangedWithoutNavigator(t *testing.T) {
	h := newWindowHarness(t, nil)
	require.NoError(t, h.st.SetProgress(h.item.FileID, store.Progress{PositionSec: 120, DurationSec: 1440}))
	h.play(t)

	assert.Equal(t, mpv.LaunchOptions{
		MediaPath: h.item.AbsPath,
		Title:     "测试番剧",
		StartAt:   120,
		SocketDir: h.m.opts.RuntimeDir,
	}, h.launch)
}
