package player

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nagare-project/nagare/internal/animego"
	"github.com/nagare-project/nagare/internal/library"
	"github.com/nagare-project/nagare/internal/store"
)

const frieren = 154587

// assocHarness 是带关联查询的 Manager；assoc 可以在测试中途改（模拟用户在媒体库里改关联）。
type assocHarness struct {
	m     *Manager
	st    *store.Store
	item  library.Item
	assoc *store.Association
}

func newAssocHarness(t *testing.T, client AnimegoClient, assoc *store.Association) *assocHarness {
	t.Helper()
	m, st, dir := newTestManager(t, client)
	h := &assocHarness{m: m, st: st, item: testItem(t, dir, 3), assoc: assoc}
	m.opts.Association = func(string) (store.Association, bool) {
		if h.assoc == nil {
			return store.Association{}, false
		}
		return *h.assoc, true
	}
	return h
}

func (h *assocHarness) ensure() (store.Binding, DanmakuInfo) {
	return h.m.ensureBinding(context.Background(), NewLocalSource(h.item), h.item)
}

// watchedThenSync 模拟这一集看完、会话收尾。
func (h *assocHarness) watchedThenSync(t *testing.T, b store.Binding) {
	t.Helper()
	require.NoError(t, h.st.SetProgress(h.item.FileID, store.Progress{Completed: true}))
	h.m.syncWatched(&session{item: h.item, finished: make(chan struct{}), binding: b})
}

func frierenAssoc() *store.Association {
	return &store.Association{Mode: store.AssociationManual, AnilistID: frieren, Title: "葬送的芙莉莲", CoverURL: "https://s4.anilist.co/c.jpg", Episodes: 28}
}

func matchFor(anilistID, episode int) animego.MatchResult {
	return animego.MatchResult{
		Matched:    true,
		AnilistID:  anilistID,
		EpisodeMap: map[int]animego.EpisodeRef{episode: {DandanEpisodeID: int64(anilistID)*1000 + int64(episode), Title: "第3话"}},
	}
}

// 没有关联、文件名里解析不出标题：照样带着空关键词去匹配（服务端靠指纹命中），与关联功能上线前一样。
func TestNoAssociationStillMatchesUntitledFiles(t *testing.T) {
	client := &fakeClient{matchRes: matchFor(999, 3)}
	h := newAssocHarness(t, client, nil)
	h.item.ParsedTitle = nil

	_, dan := h.ensure()

	assert.Equal(t, "ok", dan.State)
	require.Len(t, client.matchIn, 1)
	assert.Empty(t, client.matchIn[0].Keyword)
	assert.NotEmpty(t, client.matchIn[0].FileHash)
}

// 没有关联（不在媒体库里的磁力、在线候选，或从没认过）：缓存照用，行为不变。
func TestNoAssociationKeepsAutoMatching(t *testing.T) {
	client := &fakeClient{matchRes: matchFor(999, 3)}
	h := newAssocHarness(t, client, nil)
	require.NoError(t, h.st.SetBinding(h.item.FileID, store.Binding{AnilistID: 555, DandanEpisodeID: 1, Episode: 3}))

	b, dan := h.ensure()

	assert.Equal(t, "ok", dan.State)
	assert.Equal(t, 555, b.AnilistID)
	assert.Zero(t, client.matchCalls.Load())
}

// 认定为 X：指向别处的缓存不再沿用；先用 X 的标题匹配，结果对上 X 就落盘；之后缓存照用。
func TestAssociatedRematchesWithTheAssociatedTitleFirst(t *testing.T) {
	client := &fakeClient{matchByKw: map[string]animego.MatchResult{
		"测试番剧":   matchFor(999, 3),
		"葬送的芙莉莲": matchFor(frieren, 3),
	}}
	h := newAssocHarness(t, client, frierenAssoc())
	require.NoError(t, h.st.SetBinding(h.item.FileID, store.Binding{AnilistID: 999, DandanEpisodeID: 1, Episode: 3}))

	b, dan := h.ensure()

	assert.Equal(t, "ok", dan.State)
	assert.Equal(t, frieren, b.AnilistID)
	require.Len(t, client.matchIn, 1, "文件名里的标题正是认错的来源，先用认定作品的标题")
	assert.Equal(t, "葬送的芙莉莲", client.matchIn[0].Keyword)
	stored, _ := h.st.Binding(h.item.FileID)
	assert.Equal(t, frieren, stored.AnilistID)

	_, dan = h.ensure()
	assert.Equal(t, "ok", dan.State)
	assert.Len(t, client.matchIn, 1, "已经匹配到 X 的缓存照用")
}

// 认定为 X 但弹幕匹配不上（或 animego 暂不可达）：没有弹幕，进度照样记到 X。
func TestAssociatedFallsBackToProgressOnly(t *testing.T) {
	for name, client := range map[string]*fakeClient{
		"未匹配":   {loggedIn: true},
		"服务不可达": {loggedIn: true, matchErr: &animego.Error{Kind: animego.ErrUnavailable, Op: "match"}},
	} {
		t.Run(name, func(t *testing.T) {
			h := newAssocHarness(t, client, frierenAssoc())

			b, dan := h.ensure()

			assert.NotEqual(t, "ok", dan.State)
			assert.Contains(t, dan.Reason, "看完仍会记到「葬送的芙莉莲」")
			assert.Equal(t, store.Binding{AnilistID: frieren, Episode: 3, Title: "葬送的芙莉莲", CoverURL: "https://s4.anilist.co/c.jpg", MatchedAt: b.MatchedAt}, b)
			stored, ok := h.st.Binding(h.item.FileID)
			require.True(t, ok, "回写前要核对 store 里的匹配，所以兜底的这条也要落盘")
			assert.Equal(t, b, stored)

			h.watchedThenSync(t, b)
			assert.Equal(t, []int{3}, client.marked)
		})
	}
}

// 兜底没变就不重写：每播一次都换匹配时间，会重写状态文件、换封面地址。
func TestProgressOnlyBindingIsReusedWhenUnchanged(t *testing.T) {
	h := newAssocHarness(t, &fakeClient{loggedIn: true}, frierenAssoc())
	first, _ := h.ensure()
	second, _ := h.ensure()
	assert.Equal(t, first.MatchedAt, second.MatchedAt)
}

// 兜底之后再播，弹幕能匹配上了：升级成完整的匹配。
func TestProgressOnlyBindingUpgradesWhenDanmakuMatches(t *testing.T) {
	client := &fakeClient{loggedIn: true}
	h := newAssocHarness(t, client, frierenAssoc())
	h.ensure()

	client.matchRes = matchFor(frieren, 3)
	b, dan := h.ensure()

	assert.Equal(t, "ok", dan.State)
	assert.NotZero(t, b.DandanEpisodeID)
	stored, _ := h.st.Binding(h.item.FileID)
	assert.Equal(t, b, stored)
}

// animego 认出了 X 却说没有这一集，或集号超出 X 的总集数（绝对集号的文件配上分季条目）：
// 不能只凭关联把第 27 集写进账号。
func TestAssociatedDoesNotWriteEpisodesTheWorkDoesNotHave(t *testing.T) {
	t.Run("匹配结果里没有这一集", func(t *testing.T) {
		client := &fakeClient{loggedIn: true, matchRes: matchFor(frieren, 12)}
		h := newAssocHarness(t, client, frierenAssoc())
		ep := 27
		h.item.Episode = &ep

		b, dan := h.ensure()

		assert.Equal(t, store.Binding{}, b)
		assert.Equal(t, "匹配结果里没有第 27 集；看完不回写进度", dan.Reason)
		_, ok := h.st.Binding(h.item.FileID)
		assert.False(t, ok)
	})
	t.Run("超出总集数", func(t *testing.T) {
		h := newAssocHarness(t, &fakeClient{loggedIn: true}, frierenAssoc())
		ep := 29
		h.item.Episode = &ep

		b, dan := h.ensure()

		assert.Equal(t, store.Binding{}, b)
		assert.Contains(t, dan.Reason, "只有 28 集")
	})
	t.Run("总集数未知（还在播）照样回写", func(t *testing.T) {
		assoc := frierenAssoc()
		assoc.Episodes = 0
		h := newAssocHarness(t, &fakeClient{loggedIn: true}, assoc)
		ep := 29
		h.item.Episode = &ep

		b, _ := h.ensure()

		assert.Equal(t, 29, b.Episode)
	})
}

// 集号认不出来：不猜第 1 集，什么都不落。
func TestAssociatedUnknownEpisodeDoesNotFallBack(t *testing.T) {
	h := newAssocHarness(t, &fakeClient{loggedIn: true}, frierenAssoc())
	h.item.Episode, h.item.ParsedNumber = nil, nil

	b, dan := h.ensure()

	assert.Equal(t, store.Binding{}, b)
	assert.Equal(t, "无法识别集号，弹幕匹配跳过", dan.Reason)
}

// 没配 animego 或没登录：兜底照落（登录后重看就能回写），但不承诺「看完会记到」。
func TestSyncNoteOnlyWhenItWillActuallySync(t *testing.T) {
	for name, client := range map[string]AnimegoClient{"未配置": nil, "未登录": &fakeClient{}} {
		t.Run(name, func(t *testing.T) {
			h := newAssocHarness(t, client, frierenAssoc())
			b, dan := h.ensure()
			assert.Equal(t, frieren, b.AnilistID)
			assert.NotContains(t, dan.Reason, "看完仍会记到")
		})
	}
}

// 特典 / OVA 一类在作品分组里，但不是 X 的第 N 集：不拿正片的弹幕配它，也不回写。
func TestAssociatedExtrasSkipMatchingAndProgress(t *testing.T) {
	client := &fakeClient{matchRes: matchFor(frieren, 3), loggedIn: true}
	h := newAssocHarness(t, client, frierenAssoc())
	h.item.ParsedKind = "ova"
	require.NoError(t, h.st.SetBinding(h.item.FileID, store.Binding{AnilistID: frieren, DandanEpisodeID: 7, Episode: 3}))

	b, dan := h.ensure()

	assert.Equal(t, store.Binding{}, b)
	assert.Contains(t, dan.Reason, "附加内容")
	assert.Zero(t, client.matchCalls.Load())
}

// BD 的「Disc 2 - 03」会被归成 bonus，但它就是正片：照常匹配、照常回写。
func TestAssociatedBonusKindStillWritesProgress(t *testing.T) {
	client := &fakeClient{matchRes: matchFor(frieren, 3), loggedIn: true}
	h := newAssocHarness(t, client, frierenAssoc())
	h.item.ParsedKind = "bonus"

	b, dan := h.ensure()
	require.Equal(t, "ok", dan.State)
	h.watchedThenSync(t, b)

	assert.Equal(t, []int{3}, client.marked)
}

// 旧数据里缓存的集号没记上：按文件现在的解析补上，不必重新匹配。
func TestAssociatedCachedBindingFillsMissingEpisode(t *testing.T) {
	client := &fakeClient{loggedIn: true}
	h := newAssocHarness(t, client, frierenAssoc())
	require.NoError(t, h.st.SetBinding(h.item.FileID, store.Binding{AnilistID: frieren, DandanEpisodeID: 7, Episode: 0}))

	b, dan := h.ensure()

	assert.Equal(t, "ok", dan.State)
	assert.Equal(t, 3, b.Episode)
	assert.Equal(t, int64(7), b.DandanEpisodeID)
	stored, _ := h.st.Binding(h.item.FileID)
	assert.Equal(t, 3, stored.Episode)
	assert.Zero(t, client.matchCalls.Load())
}

// 缓存的集号与文件现在的解析不一致（共享语料更新过）：弹幕那一集也不对了，重新匹配，
// 不能拿第 27 集的弹幕配第 3 集的进度。
func TestAssociatedCachedBindingRematchesWhenEpisodeChanged(t *testing.T) {
	client := &fakeClient{loggedIn: true, matchRes: matchFor(frieren, 3)}
	h := newAssocHarness(t, client, frierenAssoc())
	require.NoError(t, h.st.SetBinding(h.item.FileID, store.Binding{AnilistID: frieren, DandanEpisodeID: 27027, Episode: 27}))

	b, dan := h.ensure()

	assert.Equal(t, "ok", dan.State)
	assert.Equal(t, 3, b.Episode)
	assert.Equal(t, int64(frieren)*1000+3, b.DandanEpisodeID)
	assert.Equal(t, int32(1), client.matchCalls.Load())
}

// 同一集重播：慢的那次匹配落空时，另一次已经拿到了完整匹配 —— 不拿兜底把它盖掉。
func TestProgressOnlyDoesNotOverwriteAFullBinding(t *testing.T) {
	client := &matchHookClient{fakeClient: fakeClient{loggedIn: true}}
	h := newAssocHarness(t, client, frierenAssoc())
	full := store.Binding{AnilistID: frieren, DandanEpisodeID: 9, Episode: 3}
	client.onMatch = func() { require.NoError(t, h.st.SetBinding(h.item.FileID, full)) }

	b, dan := h.ensure()

	assert.Equal(t, "ok", dan.State)
	assert.Equal(t, full, b)
	stored, _ := h.st.Binding(h.item.FileID)
	assert.Equal(t, full, stored)
}

// 标为「不是目录里的作品」：不匹配、不拉弹幕、不回写，缓存里的旧匹配也不用。
func TestAssociationNoneSkipsMatching(t *testing.T) {
	client := &fakeClient{matchRes: matchFor(999, 3), loggedIn: true}
	h := newAssocHarness(t, client, &store.Association{Mode: store.AssociationNone})
	require.NoError(t, h.st.SetBinding(h.item.FileID, store.Binding{AnilistID: 999, DandanEpisodeID: 1, Episode: 3}))

	b, dan := h.ensure()

	assert.Equal(t, store.Binding{}, b)
	assert.Equal(t, "none", dan.State)
	assert.Contains(t, dan.Reason, "不是目录里的作品")
	assert.Zero(t, client.matchCalls.Load())
}

// 后台匹配还在跑时用户改了关联：迟到的结果对不上新关联就不落盘，回写也不按它来。
func TestLateMatchAfterAssociationChangeIsDiscarded(t *testing.T) {
	for name, start := range map[string]*store.Association{"原本没有关联": nil, "原本认定为另一部": {Mode: store.AssociationManual, AnilistID: 999, Title: "别的番"}} {
		t.Run(name, func(t *testing.T) {
			client := &matchHookClient{fakeClient: fakeClient{matchRes: matchFor(999, 3), loggedIn: true}}
			h := newAssocHarness(t, client, start)
			client.onMatch = func() { h.assoc = frierenAssoc() }

			b, _ := h.ensure()

			_, ok := h.st.Binding(h.item.FileID)
			assert.False(t, ok, "匹配期间关联改成了别的作品，这条结果不该落盘")
			// 就算它被别的路径写回了 store，回写时也要按关联再核对一次
			require.NoError(t, h.st.SetBinding(h.item.FileID, b))
			h.watchedThenSync(t, b)
			assert.Empty(t, client.marked)
		})
	}
}

// matchHookClient 在 Match 返回之前跑一段（模拟匹配期间发生的事）。
type matchHookClient struct {
	fakeClient
	onMatch func()
}

func (c *matchHookClient) Match(ctx context.Context, in animego.MatchInput) (animego.MatchResult, error) {
	res, err := c.fakeClient.Match(ctx, in)
	if c.onMatch != nil {
		c.onMatch()
	}
	return res, err
}
