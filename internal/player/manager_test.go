package player

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nagare-project/nagare/internal/animego"
	"github.com/nagare-project/nagare/internal/danmaku"
	errs "github.com/nagare-project/nagare/internal/errors"
	"github.com/nagare-project/nagare/internal/library"
	"github.com/nagare-project/nagare/internal/mpv"
	"github.com/nagare-project/nagare/internal/store"
)

// fakeClient 是 AnimegoClient 的可编程替身。
type fakeClient struct {
	matchRes   animego.MatchResult
	matchErr   error
	matchCalls atomic.Int32
	loggedIn   bool
	marked     []int
}

func (f *fakeClient) Match(_ context.Context, _ animego.MatchInput) (animego.MatchResult, error) {
	f.matchCalls.Add(1)
	return f.matchRes, f.matchErr
}
func (f *fakeClient) Comments(context.Context, int64) ([]danmaku.Comment, error) { return nil, nil }
func (f *fakeClient) EnsureSubscription(context.Context, int) error              { return nil }
func (f *fakeClient) MarkWatched(_ context.Context, _ int, ep int) error {
	f.marked = append(f.marked, ep)
	return nil
}
func (f *fakeClient) LoggedIn() bool { return f.loggedIn }

func newTestManager(t *testing.T, client AnimegoClient) (*Manager, *store.Store, string) {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "state.json"))
	require.NoError(t, err)
	m := New(Options{Store: st, Client: client, RuntimeDir: dir})
	return m, st, dir
}

// testItem 造一个指向真实临时文件的条目（hash 计算要读它）。
func testItem(t *testing.T, dir string, episode int) library.Item {
	t.Helper()
	abs := filepath.Join(dir, "ep.mkv")
	require.NoError(t, os.WriteFile(abs, []byte("fake video content"), 0o644))
	title := "测试番剧"
	return library.Item{
		FileID:      "ep.mkv|18|1",
		FileName:    "ep.mkv",
		AbsPath:     abs,
		Size:        18,
		Episode:     &episode,
		ParsedTitle: &title,
		ParsedKind:  "main",
	}
}

// 匹配成功：绑定落库、hash 有缓存、弹幕链路 ok。
func TestEnsureBindingMatchSuccess(t *testing.T) {
	client := &fakeClient{matchRes: animego.MatchResult{
		Matched:      true,
		AnilistID:    9527,
		TitleChinese: "葬送的芙莉莲",
		EpisodeMap:   map[int]animego.EpisodeRef{7: {DandanEpisodeID: 184300007, Title: "第7话"}},
	}}
	m, st, dir := newTestManager(t, client)
	item := testItem(t, dir, 7)

	b, dan := m.ensureBinding(context.Background(), item)
	assert.Equal(t, "ok", dan.State)
	assert.Equal(t, int64(184300007), b.DandanEpisodeID)
	assert.Equal(t, 9527, b.AnilistID)
	assert.Equal(t, "葬送的芙莉莲", b.Title)

	// 绑定与 hash 都应已持久化；第二次不再打 Match。
	assert.NotEmpty(t, st.Hash(item.FileID))
	_, ok := st.Binding(item.FileID)
	assert.True(t, ok)
	_, dan2 := m.ensureBinding(context.Background(), item)
	assert.Equal(t, "ok", dan2.State)
	assert.Equal(t, int32(1), client.matchCalls.Load(), "有缓存绑定时不应重复匹配")
}

// 集号无法识别时不猜 1：跳过匹配，避免把剧场版/特典当第 1 集
// 污染用户的 animego 账号。
func TestEnsureBindingSkipsWhenEpisodeUnknown(t *testing.T) {
	client := &fakeClient{matchRes: animego.MatchResult{Matched: true}}
	m, _, dir := newTestManager(t, client)
	item := testItem(t, dir, 7)
	item.Episode = nil
	item.ParsedNumber = nil

	_, dan := m.ensureBinding(context.Background(), item)
	assert.Equal(t, "unmatched", dan.State)
	assert.Contains(t, dan.Reason, "无法识别集号")
	assert.Equal(t, int32(0), client.matchCalls.Load(), "集号未知时不该发起匹配")
}

// 完全 miss → unmatched；episodeMap 缺该集号 → unmatched。
func TestEnsureBindingUnmatched(t *testing.T) {
	m, _, dir := newTestManager(t, &fakeClient{matchRes: animego.MatchResult{Matched: false}})
	item := testItem(t, dir, 7)
	_, dan := m.ensureBinding(context.Background(), item)
	assert.Equal(t, "unmatched", dan.State)

	m2, _, dir2 := newTestManager(t, &fakeClient{matchRes: animego.MatchResult{
		Matched:    true,
		EpisodeMap: map[int]animego.EpisodeRef{1: {DandanEpisodeID: 1}},
	}})
	item2 := testItem(t, dir2, 7)
	_, dan2 := m2.ensureBinding(context.Background(), item2)
	assert.Equal(t, "unmatched", dan2.State)
	assert.Contains(t, dan2.Reason, "第 7 集")
}

// 服务不可达 → unavailable，且理由是可读中文（本地播放不受影响的降级路径）。
func TestEnsureBindingUnavailable(t *testing.T) {
	client := &fakeClient{matchErr: &animego.Error{Kind: animego.ErrUnavailable, Op: "match"}}
	m, _, dir := newTestManager(t, client)
	item := testItem(t, dir, 7)
	_, dan := m.ensureBinding(context.Background(), item)
	assert.Equal(t, "unavailable", dan.State)
	assert.Contains(t, dan.Reason, "不可达")
}

// 无客户端（纯离线）→ none。
func TestEnsureBindingOffline(t *testing.T) {
	m, _, dir := newTestManager(t, nil)
	item := testItem(t, dir, 7)
	_, dan := m.ensureBinding(context.Background(), item)
	assert.Equal(t, "none", dan.State)
}

// CQ3 的核心承诺：animego 慢不得拖住本地播放。
// 匹配接口卡住时，Play 必须已经把 mpv 起起来并立即返回。
func TestPlayLaunchesBeforeAnimego(t *testing.T) {
	blocked := make(chan struct{})
	client := &slowClient{block: blocked}
	m, _, dir := newTestManager(t, client)
	item := testItem(t, dir, 7)

	launched := make(chan struct{})
	m.opts.Launch = func(context.Context, mpv.LaunchOptions) (*mpv.Player, error) {
		close(launched)
		return nil, errors.New("stub：本用例只验证启动时序")
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = m.Play(context.Background(), item, "")
	}()

	select {
	case <-launched:
	case <-time.After(3 * time.Second):
		t.Fatal("匹配接口阻塞时 mpv 仍未启动 —— 本地播放被 animego 拖住了")
	}
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Play 未能在匹配阻塞时立即返回")
	}
	close(blocked)
}

// slowClient 的 Match 会一直阻塞到 block 关闭。
type slowClient struct {
	fakeClient
	block chan struct{}
}

func (s *slowClient) Match(ctx context.Context, _ animego.MatchInput) (animego.MatchResult, error) {
	select {
	case <-s.block:
	case <-ctx.Done():
	}
	return animego.MatchResult{}, ctx.Err()
}

// Play 对不存在的文件给出文件系统类错误（明确错误，非黑屏）。
func TestPlayMissingFile(t *testing.T) {
	m, _, _ := newTestManager(t, nil)
	item := library.Item{FileID: "x", FileName: "x.mkv", AbsPath: "/不存在/x.mkv"}
	_, err := m.Play(context.Background(), item, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "不存在或已被移动")
}

// progressUpdate 的规则表。
func TestProgressUpdate(t *testing.T) {
	cases := []struct {
		name       string
		prev       store.Progress
		hadPrev    bool
		st         mpv.State
		eof        bool
		wantWrite  bool
		wantDone   bool
		wantSynced bool
	}{
		{name: "开播不足5秒不写", st: mpv.State{TimePos: 3, Duration: 1400}, wantWrite: false},
		{name: "正常进度写入", st: mpv.State{TimePos: 100, Duration: 1400}, wantWrite: true},
		{name: "过九成算看完", st: mpv.State{TimePos: 1300, Duration: 1400}, wantWrite: true, wantDone: true},
		{name: "eof即看完", st: mpv.State{TimePos: 60, Duration: 0}, eof: true, wantWrite: true, wantDone: true},
		{
			name: "秒退不砸掉旧进度",
			prev: store.Progress{PositionSec: 300, DurationSec: 1400}, hadPrev: true,
			st: mpv.State{TimePos: 1, Duration: 0}, wantWrite: false,
		},
		{
			name: "看完标记保持",
			prev: store.Progress{PositionSec: 1400, DurationSec: 1400, Completed: true, Synced: true}, hadPrev: true,
			st: mpv.State{TimePos: 30, Duration: 1400}, wantWrite: true, wantDone: true, wantSynced: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, write := progressUpdate(tc.prev, tc.hadPrev, tc.st, tc.eof, 123)
			assert.Equal(t, tc.wantWrite, write)
			if write {
				assert.Equal(t, tc.wantDone, p.Completed)
				assert.Equal(t, tc.wantSynced, p.Synced)
				assert.Equal(t, tc.st.TimePos, p.PositionSec)
			}
		})
	}
}

func TestPickTitle(t *testing.T) {
	ep := 7
	title := "标题"
	item := library.Item{FileName: "f.mkv", ParsedTitle: &title, Episode: &ep}
	assert.Equal(t, "芙莉莲 第7集", pickTitle(store.Binding{Title: "芙莉莲", Episode: 7}, item))
	assert.Equal(t, "标题", pickTitle(store.Binding{}, item))
	assert.Equal(t, "f.mkv", pickTitle(store.Binding{}, library.Item{FileName: "f.mkv"}))
}

// mpv 缺失：Play 直接返回带安装指引的播放类错误，不会去调 Launch。
func TestPlayWithoutMPVGivesGuidance(t *testing.T) {
	m, _, dir := newTestManager(t, nil)
	m.opts.MPV = mpv.NewRuntimeWith(func(string) (mpv.Info, error) {
		return mpv.Info{}, errors.New("未找到 mpv")
	}, "")
	m.opts.Launch = func(context.Context, mpv.LaunchOptions) (*mpv.Player, error) {
		t.Fatal("mpv 缺失时不应启动")
		return nil, nil
	}

	_, err := m.Play(context.Background(), testItem(t, dir, 1), "")
	var ce *errs.E
	require.ErrorAs(t, err, &ce)
	assert.Equal(t, errs.CategoryPlayback, ce.Category)
	assert.Contains(t, ce.UserFacing(), "重新检测")
	assert.False(t, m.Status().Playing)
}

// 共享状态里的路径就是喂给 Launch 的路径；Redetect 后立刻生效（不必重启）。
func TestPlayUsesRuntimeMPVPath(t *testing.T) {
	m, _, dir := newTestManager(t, nil)
	path := "/first/mpv"
	m.opts.MPV = mpv.NewRuntimeWith(func(string) (mpv.Info, error) {
		return mpv.Info{Path: path, Version: "0.41.0"}, nil
	}, "")
	var got string
	m.opts.Launch = func(_ context.Context, o mpv.LaunchOptions) (*mpv.Player, error) {
		got = o.MPVPath
		return nil, errors.New("到此为止")
	}

	_, err := m.Play(context.Background(), testItem(t, dir, 1), "")
	require.Error(t, err)
	assert.Equal(t, "/first/mpv", got)

	path = "/second/mpv"
	_, err = m.opts.MPV.Redetect("")
	require.NoError(t, err)
	_, _ = m.Play(context.Background(), testItem(t, dir, 1), "")
	assert.Equal(t, "/second/mpv", got)
}
