package api

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/png"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nagare-project/nagare/internal/animego"
	"github.com/nagare-project/nagare/internal/artcache"
	"github.com/nagare-project/nagare/internal/store"
)

// fakeTones 按封面原始地址返回固定色阶，并记下被问过哪些地址。
type fakeTones struct {
	mu    sync.Mutex
	by    map[string]artcache.PosterTones
	asked []string
}

func (f *fakeTones) PosterTones(source string) *artcache.PosterTones {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.asked = append(f.asked, source)
	t, ok := f.by[source]
	if !ok {
		return nil
	}
	return &t
}

func tonesFixture(hue float64) artcache.PosterTones { return artcache.LadderFor(hue) }

const coverA = "https://s4.anilist.co/a.jpg"

// 目录投影：有封面的条目带上色阶，取不到就是 null；没接取色来源时也是 null，不报错。
func TestCatalogSummaryCarriesPosterTones(t *testing.T) {
	s := catalogService(&catalogStub{})
	now := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	s.cache.now = func() time.Time { return now }

	view, err := s.View(context.Background())
	require.NoError(t, err)
	require.NotEmpty(t, view.Sections[2].Items)
	assert.Nil(t, view.Sections[2].Items[0].PosterTones, "还没接取色来源")

	src := &fakeTones{by: map[string]artcache.PosterTones{coverA: tonesFixture(264)}}
	s.tones.set(src)
	view, err = s.View(context.Background())
	require.NoError(t, err)
	got := view.Sections[2].Items[0].PosterTones
	require.NotNil(t, got)
	assert.Equal(t, tonesFixture(264), *got)
	assert.Contains(t, src.asked, coverA, "按封面的原始地址查，不是 /art 键")

	// 没登记的图床：Cover 为空，也就不查颜色
	m := s.summary(animego.CatalogMedia{AnilistID: 9, TitleChinese: "外部图床", CoverImageURL: "https://tracker.evil/p.png"})
	assert.Empty(t, m.Cover)
	assert.Nil(t, m.PosterTones)
	assert.NotContains(t, src.asked, "https://tracker.evil/p.png")
}

type coverScheduleStub struct{ catalogStub }

func (f *coverScheduleStub) Schedule(context.Context) (animego.ScheduleData, error) {
	item := animego.ScheduleItem{CatalogMedia: animego.CatalogMedia{AnilistID: 1, TitleNative: "作品", CoverImageURL: coverA}, Episode: 3, AiringAt: 1788656400}
	return animego.ScheduleData{Groups: map[string][]animego.ScheduleItem{"d": {item}}}, nil
}

func TestScheduleCarriesPosterTones(t *testing.T) {
	s := catalogService(&coverScheduleStub{})
	s.tones.set(&fakeTones{by: map[string]artcache.PosterTones{coverA: tonesFixture(30)}})
	view, err := s.ScheduleView(context.Background(), nil)
	require.NoError(t, err)
	require.Len(t, view.Airings, 1)
	require.NotNil(t, view.Airings[0].PosterTones)
	assert.Equal(t, 30.0, view.Airings[0].PosterTones.Hue)
}

// 接口形状：字段名固定为 posterTones，取不到时是显式的 null（客户端按可空解码）。
func TestPosterTonesJSONShape(t *testing.T) {
	raw, err := json.Marshal(SummaryMedia{AnilistID: 1, Title: "x"})
	require.NoError(t, err)
	assert.Contains(t, string(raw), `"posterTones":null`)

	tones := tonesFixture(120)
	raw, err = json.Marshal(AiringView{AnilistID: 1, PosterTones: &tones})
	require.NoError(t, err)
	var decoded struct {
		PosterTones map[string]any `json:"posterTones"`
	}
	require.NoError(t, json.Unmarshal(raw, &decoded))
	assert.Len(t, decoded.PosterTones, 6)
	for _, k := range []string{"tone", "quiet", "mid", "low", "onTone"} {
		assert.Regexp(t, `^#[0-9a-f]{6}$`, decoded.PosterTones[k], k)
	}
	assert.Equal(t, 120.0, decoded.PosterTones["hue"])
}

// 媒体库：分组封面与继续观看都带色阶；查的是那张封面自己的原始地址（认定的作品优先）。
func TestLibraryViewCarriesPosterTones(t *testing.T) {
	env := newAssocEnv(t, &detailStub{})
	ids := env.fileIDs(t)
	matched := "https://s4.anilist.co/matched.jpg"
	require.NoError(t, env.store.SetBinding(ids[0], store.Binding{AnilistID: 154587, Episode: 1, Title: "t", CoverURL: matched, MatchedAt: 1}))
	require.NoError(t, env.store.SetProgress(ids[0], store.Progress{PositionSec: 300, DurationSec: 1400, UpdatedAt: 1}))

	c := env.cluster(t)
	assert.NotEmpty(t, c.Cover)
	assert.Nil(t, c.PosterTones, "还没接取色来源")

	src := &fakeTones{by: map[string]artcache.PosterTones{matched: tonesFixture(200), frierenCover: tonesFixture(10)}}
	env.lib.SetPosterTones(src)
	view := env.lib.View()
	require.NotNil(t, view.Clusters[0].PosterTones)
	assert.Equal(t, 200.0, view.Clusters[0].PosterTones.Hue)
	require.Len(t, view.ContinueWatching, 1)
	require.NotNil(t, view.ContinueWatching[0].PosterTones)
	assert.Equal(t, 200.0, view.ContinueWatching[0].PosterTones.Hue)

	// 认定为某部作品：封面换成那部作品的，颜色也跟着换
	decodeAssoc(t, env.put(t, assocBody(t, c.ClusterKey, "manual", 154587)))
	view = env.lib.View()
	assert.Equal(t, 10.0, view.Clusters[0].PosterTones.Hue)
	assert.Equal(t, 10.0, view.ContinueWatching[0].PosterTones.Hue)

	// 标为「不是目录里的作品」：没有封面，也就没有颜色
	decodeAssoc(t, env.put(t, assocBody(t, c.ClusterKey, "none", 0)))
	view = env.lib.View()
	assert.Empty(t, view.Clusters[0].Cover)
	assert.Nil(t, view.Clusters[0].PosterTones)
}

// 接真实的封面缓存走一遍：封面已缓存、还没取色 → 先是 null，后台算完之后带上颜色。
func TestCatalogPosterTonesFromRealCache(t *testing.T) {
	cache, err := artcache.New(t.TempDir())
	require.NoError(t, err)
	img := image.NewNRGBA(image.Rect(0, 0, 24, 32))
	for i := range img.Pix {
		if i%4 == 2 || i%4 == 3 {
			img.Pix[i] = 255 // 纯蓝、不透明
		}
	}
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))
	require.NoError(t, os.WriteFile(cache.Path(coverA), buf.Bytes(), 0o600))

	s := catalogService(&catalogStub{})
	s.tones.set(cache)
	m := s.summary(animego.CatalogMedia{AnilistID: 1, TitleChinese: "作品", CoverImageURL: coverA})
	assert.Nil(t, m.PosterTones, "第一次只排队，不等解码")
	require.Eventually(t, func() bool {
		return s.summary(animego.CatalogMedia{AnilistID: 1, TitleChinese: "作品", CoverImageURL: coverA}).PosterTones != nil
	}, 5*time.Second, 10*time.Millisecond)
	got := s.summary(animego.CatalogMedia{AnilistID: 1, TitleChinese: "作品", CoverImageURL: coverA}).PosterTones
	assert.InDelta(t, 264.1, got.Hue, 0.2)
}
