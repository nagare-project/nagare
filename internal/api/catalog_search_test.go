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
type fakeClock struct{ at time.Time }

func (c *fakeClock) now() time.Time { return c.at }

func searchService(f CatalogReader) (*DiscoverService, *fakeClock) {
	s := catalogService(f)
	clock := &fakeClock{at: time.Unix(1_791_000_000, 0)}
	s.cache.now = clock.now
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

	// 空白不同、大小写不同都是同一次搜索：命中缓存，不再出站
	_, err = s.Search(context.Background(), "葬送的 芙莉莲")
	require.NoError(t, err)
	assert.Equal(t, []string{"葬送的 芙莉莲"}, f.queries)
}

func TestCatalogSearchThrottlesUpstreamToOncePerSecond(t *testing.T) {
	f := &searchStub{}
	s, clock := searchService(f)

	_, err := s.Search(context.Background(), "芙莉莲")
	require.NoError(t, err)
	// 同一秒内换个词再搜：在本机挡住，不让 animego 回 429
	_, err = s.Search(context.Background(), "孤独摇滚")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "搜得太快了")

	clock.at = clock.at.Add(searchMinInterval)
	_, err = s.Search(context.Background(), "孤独摇滚")
	require.NoError(t, err)
	assert.Equal(t, []string{"芙莉莲", "孤独摇滚"}, f.queries)
}

func TestCatalogSearchRejectsEmptyAndOverlongKeywords(t *testing.T) {
	s, _ := searchService(&searchStub{})

	_, err := s.Search(context.Background(), "   ")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "请输入作品名")

	_, err = s.Search(context.Background(), strings.Repeat("番", maxSearchRunes+1))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "太长")
}

func TestCatalogSearchWithoutSearcherSaysUnsupported(t *testing.T) {
	// 只实现 CatalogReader 的源（比如旧的替身）不支持搜索：说清楚，而不是 panic
	s, _ := searchService(&catalogStub{})
	_, err := s.Search(context.Background(), "芙莉莲")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "不支持搜索")
}
