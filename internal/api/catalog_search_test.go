package api

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nagare-project/nagare/internal/animego"
)

// searchStub 在 catalogStub 之上加关键词搜索，并记下每次出站的关键词。
type searchStub struct {
	catalogStub
	queries []string
}

func (f *searchStub) Search(_ context.Context, q string) ([]animego.CatalogMedia, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.queries = append(f.queries, q)
	return []animego.CatalogMedia{{AnilistID: 154587, TitleChinese: "葬送的芙莉莲", Format: "TV", Status: "FINISHED"}}, nil
}

// fakeClock 让节流可控：每次调用 advance 前都停在同一刻。
type fakeClock struct {
	at    time.Time
	slept []time.Duration
}

func (c *fakeClock) now() time.Time { return c.at }

func searchService(f CatalogReader) (*DiscoverService, *fakeClock) {
	s := catalogService(f)
	clock := &fakeClock{at: time.Unix(1_791_000_000, 0)}
	s.cache.now = clock.now
	// 不真睡：记下要等多久，并把假时钟拨过去
	s.sleep = func(_ context.Context, d time.Duration) error {
		clock.slept = append(clock.slept, d)
		clock.at = clock.at.Add(d)
		return nil
	}
	return s, clock
}

func TestCatalogSearchProjectsRowsAndCachesByNormalizedKeyword(t *testing.T) {
	f := &searchStub{}
	s, _ := searchService(f)

	first, err := s.Search(context.Background(), "  葬送的  芙莉莲 ")
	require.NoError(t, err)
	assert.Equal(t, "葬送的 芙莉莲", first.Query)
	require.Len(t, first.Items, 1)
	assert.Equal(t, 154587, first.Items[0].AnilistID)

	// 空白不同是同一次搜索：命中缓存，不再出站
	_, err = s.Search(context.Background(), "葬送的 芙莉莲")
	require.NoError(t, err)
	// 拉丁字母大小写不同也是同一次（animego 按 ILIKE 匹配，结果本就相同）
	_, err = s.Search(context.Background(), "Frieren")
	require.NoError(t, err)
	_, err = s.Search(context.Background(), "FRIEREN")
	require.NoError(t, err)
	assert.Equal(t, []string{"葬送的 芙莉莲", "Frieren"}, f.queries)
}

func TestCatalogSearchSpacesUpstreamCallsInsteadOfRejecting(t *testing.T) {
	f := &searchStub{}
	s, clock := searchService(f)

	_, err := s.Search(context.Background(), "芙莉莲")
	require.NoError(t, err)
	// 同一秒内换个词再搜：等到满一秒再发，而不是让用户再点一次
	_, err = s.Search(context.Background(), "孤独摇滚")
	require.NoError(t, err)

	assert.Equal(t, []string{"芙莉莲", "孤独摇滚"}, f.queries)
	assert.Equal(t, []time.Duration{0, searchMinInterval}, clock.slept)
}

func TestCatalogSearchRefusesWhenTheQueueIsTooLong(t *testing.T) {
	s, _ := searchService(&searchStub{})
	// 已经预约到几秒之后（同时堆了好几个不同关键词）：直接说搜得太快了
	s.lastSearch = s.cache.now().Add(maxSearchWait)

	_, err := s.Search(context.Background(), "芙莉莲")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "搜得太快了")
}

func TestCatalogSearchRejectsEmptyAndOverlongKeywords(t *testing.T) {
	s, _ := searchService(&searchStub{})

	_, err := s.Search(context.Background(), "   ")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "请输入作品名")

	_, err = s.Search(context.Background(), strings.Repeat("番", maxSearchRunes+1))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "太长")

	// 按字计而不是按字节：64 个汉字（192 字节）照样能搜
	_, err = s.Search(context.Background(), strings.Repeat("番", maxSearchRunes))
	require.NoError(t, err)
}

func TestCatalogSearchWithoutSearcherSaysUnsupported(t *testing.T) {
	// 只实现 CatalogReader 的源（比如旧的替身）不支持搜索：说清楚，而不是 panic
	s, _ := searchService(&catalogStub{})
	_, err := s.Search(context.Background(), "芙莉莲")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "不支持搜索")
}
