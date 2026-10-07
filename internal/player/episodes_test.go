package player

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nagare-project/nagare/internal/mpv"
	"github.com/nagare-project/nagare/internal/store"
)

func seq(from, to int) []int {
	out := []int{}
	for n := from; n <= to; n++ {
		out = append(out, n)
	}
	return out
}

// 规则照 animego 的 lib/library/episodeOffset.ts；前几条用例取自真实媒体库。
func TestSiteEpisode(t *testing.T) {
	known := func(total, offset int) EpisodeSpace {
		return EpisodeSpace{Total: total, Offset: offset, OffsetKnown: true}
	}
	cases := []struct {
		name    string
		episode int
		group   []int
		space   EpisodeSpace
		want    int // 0 = 不写
		retry   bool
	}{
		// 葬送的芙莉莲第二季（10 集，前作 28 集），文件只有结局「38」
		{"跨季编号的结局：38 → 第 10 集", 38, []int{38}, known(10, 28), 10, false},
		// 相反的你和我第二季（13 集，前作 12 集），文件 13–25
		{"跨季编号整季：13 → 第 1 集", 13, seq(13, 25), known(13, 12), 1, false},
		{"跨季编号整季：25 → 第 13 集", 25, seq(13, 25), known(13, 12), 13, false},
		{"本来就按季编号：偏移已知也不动", 3, seq(1, 12), known(12, 12), 3, false},
		{"没有前作（偏移 0）：不动", 5, seq(1, 12), known(12, 0), 5, false},
		{"一切正常", 7, seq(1, 12), EpisodeSpace{Total: 12}, 7, false},
		// 没有第 1 集、偏移又用不上：分不清，宁可不记 —— 包括本身没超出的「13」（其实是第 1 集）
		{"偏移未知、从 13 编起：不记", 13, seq(13, 25), EpisodeSpace{Total: 13}, 0, false},
		{"偏移套不上的孤零零 38：不记", 38, []int{38}, known(10, 20), 0, false},
		{"偏移这次没查到：不记，但重看会再试", 13, seq(13, 25), EpisodeSpace{Total: 13, OffsetUnavailable: true}, 0, true},
		// 编号从 1 开始的：范围内照写，只拦超出的
		{"一个文件夹装了两季（认定为第一季）：第 5 集照写", 5, seq(1, 25), known(12, 0), 5, false},
		{"一个文件夹装了两季（认定为第一季）：第 20 集不写", 20, seq(1, 25), known(12, 0), 0, false},
		{"总集数登记少了一集：1–12 照写", 12, seq(1, 13), known(12, 0), 12, false},
		// 前作与本季放在一个文件夹里、认定为本季（13 集，前作 12 集）：偏移之后的逐集换算，之内的不记
		{"跨过偏移的一组：13 → 第 1 集", 13, seq(1, 25), known(13, 12), 1, false},
		{"跨过偏移的一组：25 → 第 13 集", 25, seq(1, 25), known(13, 12), 13, false},
		{"跨过偏移的一组：前作的第 5 集不记", 5, seq(1, 25), known(13, 12), 0, false},
		{"本季自己从 1 编、比前作长：不受跨偏移规则影响", 20, seq(1, 24), known(24, 12), 20, false},
		{"某个文件名被误读出 2019：其余照写", 3, append(seq(1, 12), 2019), EpisodeSpace{Total: 12}, 3, false},
		// 连载中没有总集数
		{"总集数未知、整组在偏移之后：按跨季编号换算", 27, seq(13, 27), EpisodeSpace{Offset: 12, OffsetKnown: true}, 15, false},
		{"总集数未知、组里有偏移之内的：不动", 5, seq(1, 15), EpisodeSpace{Offset: 12, OffsetKnown: true}, 5, false},
		{"总集数与偏移都未知：按文件里的集号", 27, seq(13, 27), EpisodeSpace{}, 27, false},
		{"总集数未知、偏移这次没查到、不从 1 编：先不写", 15, seq(13, 15), EpisodeSpace{OffsetUnavailable: true}, 0, true},
		{"总集数未知、偏移这次没查到、从 1 编：照写", 3, seq(1, 3), EpisodeSpace{OffsetUnavailable: true}, 3, false},
		{"不在媒体库里（磁力）：只有这一集", 38, nil, known(10, 28), 10, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			group := slices.Clone(tc.group)
			got := siteEpisode(tc.episode, group, tc.space)
			assert.Equal(t, tc.want, got.Episode)
			assert.Equal(t, tc.want == 0, got.Reason != "", got.Reason)
			assert.Equal(t, tc.retry, got.Retry)
			assert.Equal(t, tc.group, group, "不能改动调用方的集号列表")
		})
	}
}

// spaceHarness：带集号空间查询的回写收尾场景。
func spaceHarness(t *testing.T, binding store.Binding, group []int, space EpisodeSpace, spaceErr error) (*Manager, *fakeClient, *session) {
	t.Helper()
	client := &fakeClient{loggedIn: true}
	m, st, dir := newTestManager(t, client)
	item := testItem(t, dir, binding.Episode)
	require.NoError(t, st.SetBinding(item.FileID, binding))
	require.NoError(t, st.SetProgress(item.FileID, store.Progress{Completed: true}))
	m.opts.GroupEpisodes = func(string) []int { return group }
	m.opts.EpisodeSpace = func(_ context.Context, id int) (EpisodeSpace, error) {
		assert.Equal(t, binding.AnilistID, id)
		return space, spaceErr
	}
	return m, client, &session{item: item, finished: make(chan struct{}), binding: binding}
}

// 看完回写写的是作品集号，不是文件里的集号。
func TestSyncWritesTheWorksOwnEpisodeNumber(t *testing.T) {
	m, client, sess := spaceHarness(t, store.Binding{AnilistID: 182255, Episode: 38, Title: "葬送的芙莉莲 第二季"}, []int{38}, EpisodeSpace{Total: 10, Offset: 28, OffsetKnown: true}, nil)

	m.syncWatched(sess)

	assert.Equal(t, []int{10}, client.marked)
	p, _ := m.opts.Store.Progress(sess.item.FileID)
	assert.True(t, p.Synced)
	assert.Nil(t, m.Status().Sync)
}

// 换算不了：不回写（连订阅都不建），横幅说清是哪一集、为什么、别按文件里的编号去标；这一集留着下次再试。
func TestSyncRefusesEpisodesItCannotPlace(t *testing.T) {
	m, client, sess := spaceHarness(t, store.Binding{AnilistID: 210031, Episode: 13, Title: "相反的你和我 第二季"}, seq(13, 25), EpisodeSpace{Total: 13}, nil)

	m.syncWatched(sess)

	assert.Empty(t, client.marked, "第二季的「13」其实是第 1 集，按 13 写会把整季标成看完")
	assert.Zero(t, client.ensured)
	got := m.Status().Sync
	require.NotNil(t, got)
	assert.Equal(t, 13, got.Episode)
	assert.Contains(t, got.Reason, "这组文件编到第 25 集，而这部作品只有 13 集")
	assert.Contains(t, got.Recovery, "不要按文件里的编号标")
	p, _ := m.opts.Store.Progress(sess.item.FileID)
	assert.False(t, p.Synced)
}

// 查不到集数：编号从 1 开始的照写（绝大多数文件夹是这样）；没有第 1 集的先不写，说重看会再试。
func TestSyncWhenSpaceLookupFails(t *testing.T) {
	m, client, sess := spaceHarness(t, store.Binding{AnilistID: 9, Episode: 4, Title: "某番"}, seq(1, 12), EpisodeSpace{}, errors.New("unreachable"))
	m.syncWatched(sess)
	assert.Equal(t, []int{4}, client.marked)

	m, client, sess = spaceHarness(t, store.Binding{AnilistID: 9, Episode: 38, Title: "某番"}, []int{38}, EpisodeSpace{}, errors.New("unreachable"))
	m.syncWatched(sess)
	assert.Empty(t, client.marked)
	require.NotNil(t, m.Status().Sync)
	assert.Equal(t, "重看这一集看完时会再试", m.Status().Sync.Recovery)
}

// 拦下之后：查得到偏移了再看完一次，就按换算后的集号写进去，横幅自己消失。
func TestRefusedEpisodeSyncsOnceTheOffsetIsKnown(t *testing.T) {
	m, client, sess := spaceHarness(t, store.Binding{AnilistID: 210031, Episode: 13, Title: "相反的你和我 第二季"}, seq(13, 25), EpisodeSpace{Total: 13, OffsetUnavailable: true}, nil)
	m.syncWatched(sess)
	require.NotNil(t, m.Status().Sync)

	m.opts.EpisodeSpace = func(context.Context, int) (EpisodeSpace, error) {
		return EpisodeSpace{Total: 13, Offset: 12, OffsetKnown: true}, nil
	}
	m.syncWatched(sess)

	assert.Equal(t, []int{1}, client.marked)
	assert.Nil(t, m.Status().Sync)
}

// 写进账号的作品集号要记下来：改关联时列「写到别的作品上的集」用的是它，不是文件里的 38。
func TestSyncRecordsTheEpisodeWrittenToTheAccount(t *testing.T) {
	m, _, sess := spaceHarness(t, store.Binding{AnilistID: 182255, Episode: 38, Title: "葬送的芙莉莲 第二季"}, []int{38}, EpisodeSpace{Total: 10, Offset: 28, OffsetKnown: true}, nil)
	m.syncWatched(sess)

	p, _ := m.opts.Store.Progress(sess.item.FileID)
	assert.Equal(t, 10, p.SyncedEpisode)

	got, err := m.opts.Store.ApplyAssociation("k", &store.Association{Mode: store.AssociationManual, AnilistID: 999}, []string{sess.item.FileID})
	require.NoError(t, err)
	assert.Equal(t, []store.SyncedRecord{{AnilistID: 182255, Title: "葬送的芙莉莲 第二季", Episodes: []int{10}}}, got.SyncedElsewhere)
}

// 进度更新时，「写进账号的是哪一集」跟着「已回写」一起保留；没回写过的不带。
func TestProgressUpdateKeepsSyncedEpisode(t *testing.T) {
	prev := store.Progress{Completed: true, Synced: true, SyncedEpisode: 10}
	got, write := progressUpdate(prev, true, mpv.State{TimePos: 600, Duration: 1400}, false, 1)
	require.True(t, write)
	assert.True(t, got.Synced)
	assert.Equal(t, 10, got.SyncedEpisode)

	got, _ = progressUpdate(store.Progress{SyncedEpisode: 10}, true, mpv.State{TimePos: 600, Duration: 1400}, false, 1)
	assert.Zero(t, got.SyncedEpisode)
}

// 查集数可能等了一阵：这期间改了关联（作废了这条匹配）就不写，也不标记已回写。
func TestSyncRechecksAssociationAfterTheLookup(t *testing.T) {
	m, client, sess := spaceHarness(t, store.Binding{AnilistID: 7, Episode: 3, Title: "某番"}, seq(1, 12), EpisodeSpace{Total: 12}, nil)
	m.opts.EpisodeSpace = func(context.Context, int) (EpisodeSpace, error) {
		_, err := m.opts.Store.ApplyAssociation("k", &store.Association{Mode: store.AssociationNone}, []string{sess.item.FileID})
		require.NoError(t, err)
		return EpisodeSpace{Total: 12}, nil
	}

	m.syncWatched(sess)

	assert.Empty(t, client.marked)
	assert.Zero(t, client.ensured)
}

// 没装集号空间查询：按文件里的集号（不去查同组集号）。
func TestPlaceEpisodeWithoutSpaceLookup(t *testing.T) {
	m, _, _ := newTestManager(t, &fakeClient{})
	m.opts.GroupEpisodes = func(string) []int { t.Fatal("没有集号空间时不该查同组集号"); return nil }
	assert.Equal(t, placement{Episode: 38}, m.placeEpisode(context.Background(), "f", 1, 38))
}

// 预取：只在登录了、匹配到作品时做；查的是这部作品。
func TestWarmEpisodeSpace(t *testing.T) {
	calls := make(chan int, 4)
	lookup := func(_ context.Context, id int) (EpisodeSpace, error) { calls <- id; return EpisodeSpace{}, nil }

	loggedOut, _, _ := newTestManager(t, &fakeClient{})
	loggedOut.opts.EpisodeSpace = lookup
	loggedOut.warmEpisodeSpace(7)

	m, _, _ := newTestManager(t, &fakeClient{loggedIn: true})
	m.warmEpisodeSpace(7) // 没装查询：什么都不做
	m.opts.EpisodeSpace = lookup
	m.warmEpisodeSpace(0)
	m.warmEpisodeSpace(182255)

	select {
	case id := <-calls:
		assert.Equal(t, 182255, id)
	case <-time.After(2 * time.Second):
		t.Fatal("登录了却没有预取")
	}
	select {
	case id := <-calls:
		t.Fatalf("多查了一次：%d", id)
	case <-time.After(100 * time.Millisecond):
	}
}
