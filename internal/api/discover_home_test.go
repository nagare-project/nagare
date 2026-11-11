package api

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nagare-project/nagare/internal/animego"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// homeStub 给发现页每一块各一份有特点的数据：封面主色、名次、追番人数、重复的放送。
type homeStub struct {
	catalogStub
	mu       sync.Mutex
	gems     int
	gemsFail bool
}

func (f *homeStub) Trending(context.Context) ([]animego.CatalogMedia, error) {
	return []animego.CatalogMedia{
		{AnilistID: 11, TitleChinese: "第一", PosterAccent: "#31add6", Rank: 1, WatcherCount: 142, AverageScore: 81, BangumiScore: 7.2},
		{AnilistID: 12, TitleNative: "二番目", PosterAccent: "#8B5CF6", WatcherCount: -3}, // 占位色、缺名次、坏人数
		{AnilistID: 11, TitleChinese: "重复"},
		{AnilistID: 13}, // 没有标题
	}, nil
}

func (f *homeStub) Seasonal(_ context.Context, season string, year int) ([]animego.CatalogMedia, error) {
	eps := 12
	return []animego.CatalogMedia{
		{AnilistID: 21, TitleChinese: "本季一", PosterAccent: "#d88852", CoverImageURL: "https://s4.anilist.co/c21.jpg", BannerImageURL: "https://s4.anilist.co/b21.jpg",
			AverageScore: 87.4, Episodes: &eps, Season: season, SeasonYear: year, Status: "RELEASING", Format: "TV", Genres: []string{"Action"}, DescriptionCn: "<b>简介</b>"},
		{AnilistID: 22, TitleChinese: "本季二", PosterAccent: "#808080"},
		{AnilistID: 22, TitleChinese: "本季二（重复）"},
	}, nil
}

func (f *homeStub) Gems(context.Context) ([]animego.CatalogMedia, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.gems++
	if f.gemsFail {
		return nil, errors.New("upstream 502")
	}
	return []animego.CatalogMedia{{AnilistID: 30 + f.gems, TitleChinese: "佳作", BangumiScore: 7.85, Format: "TV"}}, nil
}

func (f *homeStub) YearlyTop(_ context.Context, year int) ([]animego.CatalogMedia, error) {
	return []animego.CatalogMedia{{AnilistID: 40, TitleChinese: "年度第一", SeasonYear: year, Season: "SPRING", AverageScore: 90}}, nil
}

func (f *homeStub) Schedule(context.Context) (animego.ScheduleData, error) {
	at := int64(1794276000)
	row := func(id, ep int, airingAt int64) animego.ScheduleItem {
		return animego.ScheduleItem{CatalogMedia: animego.CatalogMedia{AnilistID: id, TitleChinese: "放送", PosterAccent: "#bd9946"}, ScheduleID: id*100 + ep, Episode: ep, AiringAt: airingAt}
	}
	return animego.ScheduleData{Today: "2026-11-10", Groups: map[string][]animego.ScheduleItem{
		"2026-11-11": {row(2, 5, at+86400)},
		"2026-11-10": {row(3, 1, at), row(1, 9, at), row(1, 9, at), row(4, 0, at), row(5, 2, 0)},
	}}, nil
}

func TestDiscoverViewProjectsHomeSections(t *testing.T) {
	s := catalogService(&homeStub{})
	s.cache.now = func() time.Time { return time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC) }
	view, err := s.View(context.Background())
	require.NoError(t, err)
	require.Equal(t, "FALL", view.Season)
	require.Equal(t, 2026, view.Year)

	// 本季：按上游顺序、去重；色相来自封面主色，灰色没有色相；总数是上游给的行数
	require.Len(t, view.Seasonal.Items, 2)
	first := view.Seasonal.Items[0]
	require.Equal(t, "本季一", first.Title)
	require.NotNil(t, first.Hue)
	require.InDelta(t, 53.5, *first.Hue, 1e-9)
	require.Equal(t, 87, first.Score)
	require.Equal(t, "FALL", first.Season)
	require.Equal(t, 2026, first.Year)
	require.Equal(t, "简介", first.Description)
	require.Regexp(t, `^/art/test/remote/`, first.Cover)
	require.Regexp(t, `^/art/test/remote/`, first.Banner)
	require.Nil(t, view.Seasonal.Items[1].Hue)
	require.Equal(t, 2, view.SeasonTotal, "与能显示的条数一致（重复的不算）")

	// 热门：名次缺了按位置补，人数不会是负数，占位色没有色相
	require.Len(t, view.Trending.Items, 2)
	require.Equal(t, 1, view.Trending.Items[0].Rank)
	require.Equal(t, 142, view.Trending.Items[0].Watchers)
	require.InDelta(t, 7.2, view.Trending.Items[0].BangumiScore, 1e-9)
	require.Equal(t, 2, view.Trending.Items[1].Rank)
	require.Zero(t, view.Trending.Items[1].Watchers)
	require.Nil(t, view.Trending.Items[1].Hue)
	require.Equal(t, "二番目", view.Trending.Items[1].Title)

	// 完结佳作、年度榜各在自己的位置上（不互换）
	require.Len(t, view.Gems.Items, 1)
	require.Equal(t, "佳作", view.Gems.Items[0].Title)
	require.InDelta(t, 7.85, view.Gems.Items[0].BangumiScore, 1e-9)
	require.Len(t, view.Yearly.Items, 1)
	require.Equal(t, "年度第一", view.Yearly.Items[0].Title)
	require.Equal(t, 2026, view.Yearly.Items[0].Year)

	// 放送：不分上游的日期组，按时间先后；同一集同一时刻只留一条，集号或时间无效的丢掉
	got := []int{}
	for _, a := range view.Schedule.Items {
		got = append(got, a.ScheduleID)
		require.NotNil(t, a.Hue)
	}
	require.Equal(t, []int{109, 301, 205}, got)
}

// 换一批：每次都向上游要新的一批，不走缓存；失败照实报错。
func TestMoreGemsBypassesCache(t *testing.T) {
	f := &homeStub{}
	s := catalogService(f)
	a, err := s.MoreGems(context.Background())
	require.NoError(t, err)
	b, err := s.MoreGems(context.Background())
	require.NoError(t, err)
	require.NotEqual(t, a.Items[0].AnilistID, b.Items[0].AnilistID)
	require.InDelta(t, 7.85, b.Items[0].BangumiScore, 1e-9)
	require.Equal(t, 2, f.gems)

	f.gemsFail = true
	_, err = s.MoreGems(context.Background())
	require.Error(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = s.MoreGems(ctx)
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, 3, f.gems, "已取消的请求不再打上游")
}

// 名次缺了接在见过的最大名次后面，不与上游给的重复
func TestRankedMediasFillsMissingRanksAfterTheLargest(t *testing.T) {
	s := catalogService(&homeStub{})
	got := s.rankedMedias([]animego.CatalogMedia{
		{AnilistID: 1, TitleChinese: "一", Rank: 2},
		{AnilistID: 2, TitleChinese: "二"},
		{AnilistID: 3, TitleChinese: "三"},
	})
	ranks := []int{}
	for _, m := range got {
		ranks = append(ranks, m.Rank)
	}
	require.Equal(t, []int{2, 3, 4}, ranks)
}

// slowGems 记下同一时刻有几个「换一批」在出站。
type slowGems struct {
	homeStub
	inFlight, peak, calls atomic.Int32
	release               chan struct{}
}

func (f *slowGems) Gems(ctx context.Context) ([]animego.CatalogMedia, error) {
	f.calls.Add(1)
	n := f.inFlight.Add(1)
	defer f.inFlight.Add(-1)
	for {
		p := f.peak.Load()
		if n <= p || f.peak.CompareAndSwap(p, n) {
			break
		}
	}
	select {
	case <-f.release:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return []animego.CatalogMedia{{AnilistID: 50, TitleChinese: "佳作"}}, nil
}

// 连点「换一批」：同一时刻只有一个在出站；排队中被取消的直接返回，不再出站。
func TestMoreGemsSerialisesAndQueuedCallsCanBeCancelled(t *testing.T) {
	f := &slowGems{release: make(chan struct{})}
	s := catalogService(f)

	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.MoreGems(context.Background())
			assert.NoError(t, err) // 不在测试 goroutine 里，不能用 require（FailNow）
		}()
	}
	require.Eventually(t, func() bool { return f.inFlight.Load() == 1 }, time.Second, time.Millisecond)

	// 第一个还占着：排队的这个被取消，原样返回，不打上游
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, err := s.MoreGems(ctx); done <- err }()
	cancel()
	require.ErrorIs(t, <-done, context.Canceled)

	close(f.release)
	wg.Wait()
	require.Equal(t, int32(1), f.peak.Load(), "同一时刻只有一个在出站")
	require.Equal(t, int32(4), f.calls.Load(), "取消的那个没有出站")
}
