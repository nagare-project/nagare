package player

import (
	"context"
	"sync"
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
// 事件循环、进度、收尾走的都是生产代码。每次 Launch 用一个新的假 mpv（一个假 mpv 只接一条连接）。
type windowHarness struct {
	m    *Manager
	st   *store.Store
	fake *mpvtest.Fake // 第一次 Launch 用的那个
	item library.Item
	ends atomic.Int32
	// sessionEnd 在 OnSessionEnd 里调（可为 nil）。
	sessionEnd func()

	mu       sync.Mutex
	fakes    []*mpvtest.Fake
	launches int
	launch   mpv.LaunchOptions
	order    []string // 「launch」与「end」按发生的先后
}

func newWindowHarness(t *testing.T, client AnimegoClient) *windowHarness {
	t.Helper()
	m, st, dir := newTestManager(t, client)
	h := &windowHarness{m: m, st: st, item: testItem(t, dir, 3)}
	h.fake = h.newFake(t)
	m.opts.OnSessionEnd = func() {
		if h.sessionEnd != nil {
			h.sessionEnd()
		}
		h.ends.Add(1)
		h.record("end")
	}
	m.opts.Launch = func(_ context.Context, o mpv.LaunchOptions) (*mpv.Player, error) {
		h.mu.Lock()
		h.launch = o
		i := h.launches
		h.launches++
		var f *mpvtest.Fake
		if i < len(h.fakes) {
			f = h.fakes[i]
		}
		h.mu.Unlock()
		if f == nil {
			f = h.newFake(t)
		}
		h.record("launch")
		return mpv.Attach(f.Dial(t), o.MediaPath)
	}
	t.Cleanup(m.Stop)
	return h
}

func (h *windowHarness) newFake(t *testing.T) *mpvtest.Fake {
	f := mpvtest.New(t)
	// 弹幕轨挂上之后 track-list 里要找得到它，编排才算成功
	f.SetProp("track-list", []map[string]any{{"id": 1, "type": "sub", "title": danmakuTrackTitle, "external": true}})
	h.mu.Lock()
	h.fakes = append(h.fakes, f)
	h.mu.Unlock()
	return f
}

func (h *windowHarness) record(event string) {
	h.mu.Lock()
	h.order = append(h.order, event)
	h.mu.Unlock()
}

func (h *windowHarness) events() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.order...)
}

// play 起播本地文件，返回结果与窗口。
func (h *windowHarness) play(t *testing.T) (PlayResult, *playback) {
	t.Helper()
	return h.playSource(t, NewLocalSource(h.item))
}

func (h *windowHarness) playSource(t *testing.T, src MediaSource) (PlayResult, *playback) {
	t.Helper()
	res, err := h.m.Play(context.Background(), src, "")
	require.NoError(t, err)
	pb := h.m.currentPlayback()
	require.NotNil(t, pb)
	return res, pb
}

// pushEOF 推一次「播到尾、mpv 自己退出」：时长、末尾位置、eof-reached、end-file(eof)、shutdown。
func pushEOF(t *testing.T, f *mpvtest.Fake, pos, duration float64) {
	t.Helper()
	f.PushProperty(t, "duration", duration)
	f.PushProperty(t, "time-pos", pos)
	f.PushProperty(t, "eof-reached", true)
	f.Push(t, map[string]any{"event": "end-file", "reason": "eof"})
	f.Quit(t)
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

// matchedResult 是 testItem 第 3 集的匹配结果。
func matchedResult() animego.MatchResult {
	return animego.MatchResult{
		Matched: true, AnilistID: 42, TitleChinese: "测试番剧",
		EpisodeMap: map[int]animego.EpisodeRef{3: {DandanEpisodeID: 420003, Title: "第3话"}},
	}
}

func matchedClient() *fakeClient {
	return &fakeClient{loggedIn: true, matchRes: matchedResult()}
}

// 一集播到尾、mpv 自己退出：进度记为看完、账号回写恰好一次、OnSessionEnd 恰好一次，
// 弹幕文件删掉。没有 Navigator 时整个过程不碰 keep-open。
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

	pushEOF(t, h.fake, 1439, 1440)
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
	assert.NoError(t, sess.ctx.Err(), "关窗不取消这一集的 ctx：后台匹配要照样做完、落盘")
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

// slowSyncFailClient 的回写先拖一阵再失败（animego 慢、最后超时那种）。
type slowSyncFailClient struct {
	fakeClient
	delay time.Duration
}

func (c *slowSyncFailClient) MarkWatched(context.Context, int, int) error {
	time.Sleep(c.delay)
	return &animego.Error{Kind: animego.ErrUnavailable, Op: "markWatched"}
}

// 回写失败要在「不在播了」的同时就看得见：网页一读到 playing=false 就停止轮询，媒体库的
// 失败横幅读的正是那最后一次的 sync。所以窗口要等回写做完，才对外宣布不在播。
func TestWindowSyncFailureVisibleWhenPlayingTurnsFalse(t *testing.T) {
	client := &slowSyncFailClient{fakeClient: fakeClient{loggedIn: true, matchRes: matchedResult()}, delay: 400 * time.Millisecond}
	h := newWindowHarness(t, client)
	_, pb := h.play(t)
	h.fake.Push(t, map[string]any{"event": "file-loaded"})
	require.Eventually(t, func() bool { return h.m.Status().Danmaku.State == "ok" }, 3*time.Second, 10*time.Millisecond)

	pushEOF(t, h.fake, 1439, 1440)
	deadline := time.Now().Add(5 * time.Second)
	for {
		st := h.m.Status()
		if !st.Playing {
			require.NotNil(t, st.Sync, "读到不在播的那一刻，回写失败必须已经在 Status 里")
			assert.Contains(t, st.Sync.Reason, "暂不可达")
			break
		}
		require.True(t, time.Now().Before(deadline), "窗口一直没有收尾")
		time.Sleep(time.Millisecond)
	}
	awaitWindowEnd(t, pb)
}

// 磁力流断了供给：mpv 报完 eof 就断开。Done 与那条 Ended 同时可读时也不能丢掉它 ——
// 丢了就记不成播放失败，在线来源也就不会自动换源。（-count=40 跑过：select 的随机挑选）
func TestWindowNetworkStreamPrematureEOFIsFailure(t *testing.T) {
	h := newWindowHarness(t, nil)
	src := torrentLike(3, "Sousou no Frieren", 0, nil)
	res, pb := h.playSource(t, src)

	pushEOF(t, h.fake, 300, 1440)
	awaitWindowEnd(t, pb)

	failure := h.m.Status().PlaybackFailure
	require.NotNil(t, failure, "播到两成就 eof 是来源故障")
	assert.Equal(t, src.item.FileID, failure.FileID)
	assert.Equal(t, res.PlaybackID, failure.PlaybackID)
	assert.Contains(t, failure.Reason, "没有记为看完")
	p, ok := h.st.Progress(src.item.FileID)
	require.True(t, ok)
	assert.False(t, p.Completed)
}

// mpv 被强杀（连接直接断、没有 shutdown）：最后位置照写，记一次带 playbackId 的播放失败。
func TestWindowKilledMPVRecordsFailureWithPlaybackID(t *testing.T) {
	h := newWindowHarness(t, nil)
	res, pb := h.play(t)

	h.fake.PushProperty(t, "duration", 1440.0)
	h.fake.PushProperty(t, "time-pos", 300.0)
	h.fake.CloseConn()
	awaitWindowEnd(t, pb)

	failure := h.m.Status().PlaybackFailure
	require.NotNil(t, failure)
	assert.Equal(t, h.item.FileID, failure.FileID)
	assert.Equal(t, res.PlaybackID, failure.PlaybackID)
	p, ok := h.st.Progress(h.item.FileID)
	require.True(t, ok)
	assert.Equal(t, 300.0, p.PositionSec)
	assert.Equal(t, int32(1), h.ends.Load())
}

// gatedClient 的匹配卡到 gate 关闭才返回。
type gatedClient struct {
	fakeClient
	gate chan struct{}
}

func (c *gatedClient) Match(ctx context.Context, in animego.MatchInput) (animego.MatchResult, error) {
	select {
	case <-c.gate:
	case <-ctx.Done():
		return animego.MatchResult{}, ctx.Err()
	}
	return c.fakeClient.Match(ctx, in)
}

// 匹配还在路上就关了窗口：匹配照样做完，绑定与文件指纹落盘，下次播放直接用（关窗之前就是这样）。
func TestWindowClosedMidMatchStillPersistsBinding(t *testing.T) {
	client := &gatedClient{fakeClient: fakeClient{loggedIn: true, matchRes: matchedResult()}, gate: make(chan struct{})}
	h := newWindowHarness(t, client)
	_, pb := h.play(t)
	sess := pb.episode()

	h.fake.Quit(t)
	awaitWindowEnd(t, pb)
	close(client.gate)
	select {
	case <-sess.danmakuReady:
	case <-time.After(3 * time.Second):
		t.Fatal("关窗之后后台匹配没有做完")
	}

	b, ok := h.st.Binding(h.item.FileID)
	require.True(t, ok, "关窗不该让匹配结果丢掉")
	assert.Equal(t, 42, b.AnilistID)
	assert.NotEmpty(t, h.st.Hash(h.item.FileID))
}

// 换着播：旧窗口的 OnSessionEnd（磁力停做种删分片）必须在新窗口 Launch 之前跑完，
// 否则新种子刚建好就会被旧会话的收尾停掉。
func TestPlayReplacesPlayEndsOldWindowBeforeLaunch(t *testing.T) {
	h := newWindowHarness(t, nil)
	h.sessionEnd = func() { time.Sleep(100 * time.Millisecond) }
	_, first := h.play(t)
	_, second := h.play(t)

	select {
	case <-first.finished:
	default:
		t.Fatal("新窗口起来的时候旧窗口还没收尾")
	}
	assert.Equal(t, []string{"launch", "end", "launch"}, h.events())
	assert.NotEqual(t, first.id, second.id)
	assert.Len(t, h.fake.CommandsWith("quit"), 1, "旧窗口要被关掉")
}
