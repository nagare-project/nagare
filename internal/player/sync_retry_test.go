package player

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nagare-project/nagare/internal/animego"
	"github.com/nagare-project/nagare/internal/danmaku"
	"github.com/nagare-project/nagare/internal/library"
	"github.com/nagare-project/nagare/internal/mpv"
	"github.com/nagare-project/nagare/internal/store"
)

// retryClient 是补发测试用的账号替身：并发安全，可以让回写失败或卡住。
type retryClient struct {
	mu       sync.Mutex
	loggedIn bool
	markErr  error
	marked   []string // "anilistId/集号"，按调用顺序
	// entered 每次进入 MarkWatched 时收到一个信号（可为 nil）；release 非 nil 时 MarkWatched 等它关闭才返回
	entered chan struct{}
	release chan struct{}
}

func (c *retryClient) Match(context.Context, animego.MatchInput) (animego.MatchResult, error) {
	return animego.MatchResult{}, nil
}
func (c *retryClient) Comments(context.Context, int64) ([]danmaku.Comment, error) { return nil, nil }
func (c *retryClient) EnsureSubscription(context.Context, int) error              { return nil }
func (c *retryClient) LoggedIn() bool                                             { return c.loggedIn }
func (c *retryClient) MarkWatched(_ context.Context, id, ep int) error {
	if c.entered != nil {
		c.entered <- struct{}{}
	}
	if c.release != nil {
		<-c.release
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.markErr != nil {
		return c.markErr
	}
	c.marked = append(c.marked, fmt.Sprintf("%d/%d", id, ep))
	return nil
}

func (c *retryClient) markedSnapshot() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.marked...)
}

func retryHarness(t *testing.T, client AnimegoClient) (*Manager, *store.Store) {
	t.Helper()
	m, st, _ := newTestManager(t, client)
	m.retryPace = 0
	return m, st
}

// addPending 造一集「age 之前看完、还没记到账号」的进度，配一条匹配。
func addPending(t *testing.T, st *store.Store, fileID string, anilistID, episode int, age time.Duration) {
	t.Helper()
	require.NoError(t, st.SetBinding(fileID, store.Binding{AnilistID: anilistID, Episode: episode, Title: "某番"}))
	require.NoError(t, st.SetProgress(fileID, store.Progress{
		PositionSec: 1400, DurationSec: 1400, Completed: true, UpdatedAt: time.Now().Add(-age).UnixMilli(),
	}))
}

// stampNow 把进度的更新时间改成现在：收尾刚落过盘的进度就是这样（syncFixture 造的没有时间戳）。
func stampNow(t *testing.T, st *store.Store, fileID string) {
	t.Helper()
	p := progressOf(t, st, fileID)
	p.UpdatedAt = time.Now().UnixMilli()
	require.NoError(t, st.SetProgress(fileID, p))
}

func progressOf(t *testing.T, st *store.Store, fileID string) store.Progress {
	t.Helper()
	p, ok := st.Progress(fileID)
	require.True(t, ok)
	return p
}

// 上次没记上的集，启动时补发：写进账号、落盘「已回写」，不出任何横幅。
func TestRetryResendsPendingWatchedEpisodes(t *testing.T) {
	client := &retryClient{loggedIn: true}
	m, st := retryHarness(t, client)
	addPending(t, st, "a|1|1", 42, 3, time.Hour)
	addPending(t, st, "b|1|1", 42, 4, time.Minute)

	res := m.RetryPendingSyncs(context.Background())

	assert.Equal(t, SyncRetryResult{Synced: 2}, res)
	assert.Equal(t, []string{"42/3", "42/4"}, client.markedSnapshot())
	p := progressOf(t, st, "a|1|1")
	assert.True(t, p.Synced)
	assert.Equal(t, 3, p.SyncedEpisode)
	assert.True(t, progressOf(t, st, "b|1|1").Synced)
	assert.Nil(t, m.Status().Sync)
}

// 一轮最多补十集、最早看完的先补；剩下的留给下一轮。很久以前（一个月之外）的不碰。
func TestRetryIsCappedOldestFirstAndSkipsOldEntries(t *testing.T) {
	client := &retryClient{loggedIn: true}
	m, st := retryHarness(t, client)
	for i := 1; i <= 12; i++ {
		// 第 1 集最早看完
		addPending(t, st, fmt.Sprintf("ep%02d", i), 7, i, time.Duration(100-i)*time.Minute)
	}
	addPending(t, st, "ancient", 7, 99, 40*24*time.Hour)

	res := m.RetryPendingSyncs(context.Background())

	assert.Equal(t, 10, res.Synced)
	assert.Equal(t, 2, res.Remaining)
	want := make([]string, 0, 10)
	for i := 1; i <= 10; i++ {
		want = append(want, fmt.Sprintf("7/%d", i))
	}
	assert.Equal(t, want, client.markedSnapshot())
	assert.False(t, progressOf(t, st, "ancient").Synced)

	res = m.RetryPendingSyncs(context.Background())
	assert.Equal(t, SyncRetryResult{Synced: 2}, res)
	assert.NotContains(t, client.markedSnapshot(), "7/99", "一个月之外的「看完未同步」多半是登录之前看的，不补")
}

// 作品关联在补发前改了：标为「不是目录里的作品」的不写，认定成别的作品的也不按旧匹配写，
// 都不算待补发（横幅的计数与「重试」按钮不因它们出现）。
func TestRetryHonoursTheCurrentAssociation(t *testing.T) {
	client := &retryClient{loggedIn: true}
	m, st := retryHarness(t, client)
	addPending(t, st, "none|1|1", 42, 3, time.Hour)
	addPending(t, st, "moved|1|1", 42, 4, time.Hour)
	addPending(t, st, "same|1|1", 42, 5, time.Hour)
	assoc := map[string]store.Association{
		"none|1|1":  {Mode: store.AssociationNone},
		"moved|1|1": {Mode: store.AssociationManual, AnilistID: 99},
		"same|1|1":  {Mode: store.AssociationManual, AnilistID: 42},
	}
	m.opts.Association = func(id string) (store.Association, bool) { a, ok := assoc[id]; return a, ok }

	res := m.RetryPendingSyncs(context.Background())

	assert.Equal(t, SyncRetryResult{Synced: 1}, res)
	assert.Equal(t, []string{"42/5"}, client.markedSnapshot())
	assert.False(t, progressOf(t, st, "none|1|1").Synced)
	assert.False(t, progressOf(t, st, "moved|1|1").Synced)
}

// 媒体库里的文件：认定了作品的分组里，特典 / OVA 不按正片回写；集号与文件现在解析出的不一致
// （共享语料更新过）的缓存匹配不用 —— 播放路径对这两种都会重新判断，补发不能绕过去。
func TestRetrySkipsExtrasAndStaleEpisodeNumbers(t *testing.T) {
	client := &retryClient{loggedIn: true}
	m, st := retryHarness(t, client)
	addPending(t, st, "sp", 42, 1, time.Hour)
	addPending(t, st, "renumbered", 42, 3, time.Hour)
	addPending(t, st, "bonus", 42, 2, time.Hour)
	ep := func(n int) *int { return &n }
	items := map[string]library.Item{
		"sp":         {FileID: "sp", ParsedKind: "sp", Episode: ep(1)},
		"renumbered": {FileID: "renumbered", ParsedKind: "main", Episode: ep(4)},
		// bonus 不算附加内容：「Disc 2 - 03」这类 BD 正片也会被归进去
		"bonus": {FileID: "bonus", ParsedKind: "bonus", Episode: ep(2)},
	}
	m.opts.LibraryItem = func(id string) (library.Item, bool) { it, ok := items[id]; return it, ok }
	m.opts.Association = func(string) (store.Association, bool) {
		return store.Association{Mode: store.AssociationManual, AnilistID: 42}, true
	}

	res := m.RetryPendingSyncs(context.Background())

	assert.Equal(t, SyncRetryResult{Synced: 1}, res)
	assert.Equal(t, []string{"42/2"}, client.markedSnapshot())
}

// 跨季连续编号而偏移未知：「38」换算不出作品集号 —— 绝不当偏移 0 写成第 38 集。横幅说清楚一次，
// 之后的补发不再拿同一条匹配去试（否则每次启动弹同一条横幅），也不算在「还有几集」里。
func TestRetryNeverWritesWhenTheOffsetIsUnknown(t *testing.T) {
	client := &retryClient{loggedIn: true}
	m, st := retryHarness(t, client)
	addPending(t, st, "s2e38", 182255, 38, time.Hour)
	m.opts.GroupEpisodes = func(string) []int { return []int{38} }
	lookups := 0
	m.opts.EpisodeSpace = func(context.Context, int) (EpisodeSpace, error) {
		lookups++
		return EpisodeSpace{Total: 10}, nil // 偏移 Known=false：服务端说算不出来
	}

	res := m.RetryPendingSyncs(context.Background())

	assert.Empty(t, client.markedSnapshot())
	assert.Equal(t, SyncRetryResult{}, res)
	got := m.Status().Sync
	require.NotNil(t, got, "规则挡下要让用户知道（这一集得自己去标）")
	assert.Equal(t, 38, got.Episode)
	assert.Contains(t, got.Recovery, "不要按文件里的编号标")
	assert.Zero(t, got.Pending, "补发不会再试它，界面不该给「重试」")
	p := progressOf(t, st, "s2e38")
	assert.False(t, p.Synced)
	assert.True(t, p.SyncHold.Holds(182255, 38))

	m.setSyncFailure(nil)
	m.RetryPendingSyncs(context.Background())
	assert.Equal(t, 1, lookups, "挡下过的匹配不该再拿去查、再弹横幅")
	assert.Nil(t, m.Status().Sync)
}

// 偏移这次没查到（网络）：同样不写，但这是一时的 —— 留着下次再试，补发时不出横幅。
func TestRetryKeepsEpisodesWhoseOffsetIsTemporarilyUnavailable(t *testing.T) {
	client := &retryClient{loggedIn: true}
	m, st := retryHarness(t, client)
	addPending(t, st, "s2e15", 9, 15, time.Hour)
	m.opts.GroupEpisodes = func(string) []int { return []int{13, 14, 15} }
	m.opts.EpisodeSpace = func(context.Context, int) (EpisodeSpace, error) {
		return EpisodeSpace{OffsetUnavailable: true}, nil
	}

	res := m.RetryPendingSyncs(context.Background())

	assert.Empty(t, client.markedSnapshot())
	assert.Equal(t, 1, res.Remaining)
	assert.Nil(t, m.Status().Sync)
	assert.Zero(t, progressOf(t, st, "s2e15").SyncHold)
}

// 断网：停下（后面的也会一样），全部留着下次再试；不出横幅 —— 开机自启时网络还没好是常事。
func TestRetryOfflineLeavesEverythingPendingWithoutABanner(t *testing.T) {
	client := &retryClient{loggedIn: true, markErr: &animego.Error{Kind: animego.ErrUnavailable, Op: "mark-watched"}}
	m, st := retryHarness(t, client)
	addPending(t, st, "a", 42, 3, time.Hour)
	addPending(t, st, "b", 42, 4, time.Minute)
	entered := 0
	m.opts.EpisodeSpace = func(context.Context, int) (EpisodeSpace, error) { entered++; return EpisodeSpace{Total: 12}, nil }

	res := m.RetryPendingSyncs(context.Background())

	assert.Equal(t, 0, res.Synced)
	assert.Equal(t, 2, res.Remaining)
	assert.Contains(t, res.Reason, "下次启动时会再试")
	assert.Equal(t, 1, entered, "第一集就连不上，不该接着打第二集")
	assert.Nil(t, m.Status().Sync)
	assert.False(t, progressOf(t, st, "a").Synced)
	assert.Zero(t, progressOf(t, st, "a").SyncHold, "连不上不是规则挡下")
}

// 没登录：什么都不发、不出横幅，留到登录之后。
func TestRetryWithoutLoginDoesNothing(t *testing.T) {
	client := &retryClient{loggedIn: false}
	m, st := retryHarness(t, client)
	addPending(t, st, "a", 42, 3, time.Hour)

	res := m.RetryPendingSyncs(context.Background())

	assert.Empty(t, client.markedSnapshot())
	assert.Equal(t, 1, res.Remaining)
	assert.Contains(t, res.Reason, "登录")
	assert.Nil(t, m.Status().Sync)
}

// 登录过期要让用户知道（他得去重新登录）；横幅说出还有几集没记上，界面据此给「重试」。
func TestRetryReportsExpiredLoginWithThePendingCount(t *testing.T) {
	client := &retryClient{loggedIn: true, markErr: &animego.Error{Kind: animego.ErrAuthExpired, Op: "mark-watched"}}
	m, st := retryHarness(t, client)
	addPending(t, st, "a", 42, 3, time.Hour)
	addPending(t, st, "b", 42, 4, time.Minute)

	res := m.RetryPendingSyncs(context.Background())

	assert.Equal(t, 2, res.Remaining)
	got := m.Status().Sync
	require.NotNil(t, got)
	assert.Equal(t, "a", got.FileID)
	assert.Contains(t, got.Recovery, "重新登录")
	assert.Equal(t, 2, got.Pending)
}

// 收尾时回写失败留下的横幅：补发补上之后自己消失。
func TestRetrySuccessClearsTheBannerLeftByPlayback(t *testing.T) {
	client := &retryClient{loggedIn: true, markErr: &animego.Error{Kind: animego.ErrUnavailable, Op: "mark-watched"}}
	m, sess, _ := syncFixture(t, client)
	stampNow(t, m.opts.Store, sess.item.FileID)
	m.syncWatched(sess)
	got := m.Status().Sync
	require.NotNil(t, got)
	assert.Contains(t, got.Recovery, "重试")
	assert.Equal(t, 1, got.Pending, "这一集会再试：横幅要给「重试」")

	client.mu.Lock()
	client.markErr = nil
	client.mu.Unlock()
	res := m.RetryPendingSyncs(context.Background())

	assert.Equal(t, SyncRetryResult{Synced: 1}, res)
	assert.True(t, progressOf(t, m.opts.Store, sess.item.FileID).Synced)
	assert.Nil(t, m.Status().Sync, "已经补上了，横幅还挂着就是在误导用户")
}

// 补发正在写某一集时，收尾不会再并发写同一集：等它写完，看到已回写就不再发。
func TestPlaybackSyncWaitsForAnInFlightRetryOfTheSameEpisode(t *testing.T) {
	client := &retryClient{loggedIn: true, entered: make(chan struct{}, 4), release: make(chan struct{})}
	m, sess, _ := syncFixture(t, client)
	stampNow(t, m.opts.Store, sess.item.FileID)

	retried := make(chan SyncRetryResult, 1)
	go func() { retried <- m.RetryPendingSyncs(context.Background()) }()
	<-client.entered // 补发已经在写了

	synced := make(chan struct{})
	go func() { m.syncWatched(sess); close(synced) }()
	select {
	case <-client.entered:
		t.Fatal("收尾在补发写到一半时又发了一次")
	case <-synced:
		t.Fatal("收尾没有等补发写完就走了")
	case <-time.After(100 * time.Millisecond):
	}
	close(client.release)
	<-synced

	assert.Equal(t, SyncRetryResult{Synced: 1}, <-retried)
	assert.Equal(t, []string{"42/3"}, client.markedSnapshot())
}

// 反过来：收尾正在写的那一集，补发跳过它（交给收尾，不并发写）；两轮补发也不会同时跑。
func TestRetrySkipsAnEpisodeThePlaybackIsSyncing(t *testing.T) {
	client := &retryClient{loggedIn: true, entered: make(chan struct{}, 4), release: make(chan struct{})}
	m, sess, _ := syncFixture(t, client)
	stampNow(t, m.opts.Store, sess.item.FileID)

	synced := make(chan struct{})
	go func() { m.syncWatched(sess); close(synced) }()
	<-client.entered

	res := m.RetryPendingSyncs(context.Background())
	assert.Equal(t, 0, res.Synced)
	close(client.release)
	<-synced

	assert.Equal(t, []string{"42/3"}, client.markedSnapshot())
	assert.True(t, progressOf(t, m.opts.Store, sess.item.FileID).Synced)
}

func TestConcurrentRetriesSendEachEpisodeOnce(t *testing.T) {
	client := &retryClient{loggedIn: true}
	m, st := retryHarness(t, client)
	for i := 1; i <= 3; i++ {
		addPending(t, st, fmt.Sprintf("ep%d", i), 5, i, time.Duration(10-i)*time.Minute)
	}

	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() { defer wg.Done(); m.RetryPendingSyncs(context.Background()) }()
	}
	wg.Wait()

	assert.ElementsMatch(t, []string{"5/1", "5/2", "5/3"}, client.markedSnapshot())
}

// 退出（或网页不等了）时补发随 ctx 停下，没补的留着；排队等前一轮的调用方也不会卡住。
func TestRetryStopsWithTheContext(t *testing.T) {
	client := &retryClient{loggedIn: true}
	m, st := retryHarness(t, client)
	addPending(t, st, "a", 42, 3, time.Hour)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	m.retrySem <- struct{}{} // 另一轮正在跑
	res := m.RetryPendingSyncs(ctx)
	<-m.retrySem

	assert.Empty(t, client.markedSnapshot())
	assert.Equal(t, 1, res.Remaining)
}

// 进度落盘（每 5 秒一次）不能把「挡下过」抹掉：keep-open 停在最后一帧时收尾回写之后还会再落盘。
func TestProgressUpdateKeepsTheSyncHold(t *testing.T) {
	hold := store.SyncHold{AnilistID: 9, Episode: 38}
	p, write := progressUpdate(store.Progress{Completed: true, SyncHold: hold}, true, mpv.State{TimePos: 1400, Duration: 1400}, true, 1)
	require.True(t, write)
	assert.Equal(t, hold, p.SyncHold)

	p, _ = progressUpdate(store.Progress{Completed: true, Synced: true, SyncHold: hold}, true, mpv.State{TimePos: 1400, Duration: 1400}, true, 1)
	assert.Zero(t, p.SyncHold, "已回写的不会再有挡下")
}
