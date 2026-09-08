package api

import (
	"context"
	"errors"
	"github.com/nagare-project/nagare/internal/animego"
	"github.com/stretchr/testify/require"
	"sync"
	"testing"
	"time"
)

type catalogStub struct {
	mu           sync.Mutex
	calls        map[string]int
	seasonalKeys []string
	failTrending bool
}

func (f *catalogStub) batch(key string) ([]animego.CatalogMedia, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.calls == nil {
		f.calls = map[string]int{}
	}
	f.calls[key]++
	if key == "trending" && f.failTrending {
		return nil, errors.New("upstream 502")
	}
	return []animego.CatalogMedia{{AnilistID: 1, TitleChinese: "作品", Format: "TV", Status: "RELEASING", CoverImageURL: "https://s4.anilist.co/a.jpg"}}, nil
}
func (f *catalogStub) Trending(context.Context) ([]animego.CatalogMedia, error) {
	return f.batch("trending")
}
func (f *catalogStub) Seasonal(_ context.Context, season string, year int) ([]animego.CatalogMedia, error) {
	return f.batch(season + time.Date(year, 1, 1, 0, 0, 0, 0, time.UTC).Format("2006"))
}
func (f *catalogStub) Gems(context.Context) ([]animego.CatalogMedia, error) { return f.batch("gems") }
func (f *catalogStub) YearlyTop(context.Context, int) ([]animego.CatalogMedia, error) {
	return f.batch("yearly")
}
func (f *catalogStub) Detail(context.Context, int) (animego.CatalogMedia, error) {
	return animego.CatalogMedia{AnilistID: 1, TitleNative: "作品"}, nil
}
func (f *catalogStub) Schedule(context.Context) (animego.ScheduleData, error) {
	_, err := f.batch("schedule")
	item := animego.ScheduleItem{CatalogMedia: animego.CatalogMedia{AnilistID: 1, TitleNative: "作品"}, Episode: 3, AiringAt: 1788656400}
	return animego.ScheduleData{Groups: map[string][]animego.ScheduleItem{"server-date": {item, item}}}, err
}
func catalogService(f CatalogReader) *DiscoverService {
	a := NewRemoteArt("https://animego.example", 8)
	a.SetPrefix("/art/test")
	return NewDiscoverService(f, a)
}
func TestDiscoverPartialFailureCacheAndYearBoundary(t *testing.T) {
	f := &catalogStub{failTrending: true}
	s := catalogService(f)
	now := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	s.cache.now = func() time.Time { return now }
	for range 2 {
		view, err := s.View(context.Background())
		require.NoError(t, err)
		require.NotEmpty(t, view.Sections[0].Error)
		require.Empty(t, view.Sections[0].Items)
		require.Len(t, view.Sections[2].Items, 1)
	}
	require.Equal(t, 2, f.calls["trending"])
	require.Equal(t, 1, f.calls["WINTER2027"])
	require.Equal(t, 1, f.calls["FALL2026"])
	require.Equal(t, 1, f.calls["SPRING2027"])
	now = now.Add(11 * time.Minute)
	_, err := s.View(context.Background())
	require.NoError(t, err)
	require.Equal(t, 2, f.calls["WINTER2027"])
	require.Equal(t, 1, f.calls["schedule"])
}
func TestSchedulePreservesUnixDeduplicatesAndSharesCache(t *testing.T) {
	f := &catalogStub{}
	s := catalogService(f)
	_, err := s.View(context.Background())
	require.NoError(t, err)
	v, err := s.ScheduleView(context.Background(), func(id int) bool { return id == 1 })
	require.NoError(t, err)
	require.Len(t, v.Airings, 1)
	require.Equal(t, int64(1788656400), v.Airings[0].AiringAt)
	require.True(t, v.Airings[0].InLibrary)
	require.Equal(t, 1, f.calls["schedule"])
}
func TestProjectionSanitizesAndFallsBack(t *testing.T) {
	s := catalogService(&catalogStub{})
	rows := []animego.CatalogMedia{
		{AnilistID: 1, TitleChinese: " 中文 ", TitleRomaji: "R", Description: `<script>bad()</script><b>A &amp; B</b>`, TrailerID: "abcdefghijk", TrailerSite: "youtube"},
		{AnilistID: 2, TitleRomaji: "R", TitleNative: "日"}, {AnilistID: 3, TitleNative: "日"}, {AnilistID: 4},
	}
	v := s.summaries(rows)
	require.Len(t, v, 3)
	require.Equal(t, "中文", v[0].Title)
	require.Equal(t, "A & B", v[0].Description)
	require.Equal(t, "abcdefghijk", v[0].TrailerID)
	require.Equal(t, "R", v[1].Title)
	require.Equal(t, "日", v[2].Title)
}
func TestReadCacheIndependentKeysAndCanceledWaiter(t *testing.T) {
	cache := newReadCache()
	started, release := make(chan struct{}), make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := cachedRead(ctx, cache, "slow", time.Minute, func(context.Context) (int, error) { close(started); <-release; return 7, nil })
		done <- err
	}()
	<-started
	fast, err := cachedRead(context.Background(), cache, "fast", time.Minute, func(context.Context) (int, error) { return 9, nil })
	require.NoError(t, err)
	require.Equal(t, 9, fast)
	cancel()
	require.ErrorIs(t, <-done, context.Canceled)
	close(release)
	v, err := cachedRead(context.Background(), cache, "slow", time.Minute, func(context.Context) (int, error) { t.Error("duplicate load"); return 0, nil })
	require.NoError(t, err)
	require.Equal(t, 7, v)
}
