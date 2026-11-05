package api

import (
	"context"
	"errors"
	"time"

	"github.com/nagare-project/nagare/internal/animego"
	"github.com/nagare-project/nagare/internal/artcache"
)

// 黄金环境里的目录 / 收藏替身：字段尽量填全，让每个可选字段都至少在一份黄金响应里出现
// （前端的严格解码会检查「TS 类型里的可选字段有没有被任何一份黄金文件覆盖」）。

const (
	goldenFrierenID = 154587
	goldenBocchiID  = 130003
	goldenUpcoming  = 180001
	goldenMovieID   = 180002
	goldenNextID    = 180003
)

func anilistCover(id string) string {
	return "https://s4.anilist.co/file/anilistcdn/media/anime/cover/large/" + id + ".jpg"
}

func anilistBanner(id string) string {
	return "https://s4.anilist.co/file/anilistcdn/media/anime/banner/" + id + ".jpg"
}

func frierenRow() animego.CatalogMedia {
	return animego.CatalogMedia{
		AnilistID: goldenFrierenID, TitleChinese: "葬送的芙莉莲", TitleRomaji: "Sousou no Frieren",
		TitleNative: "葬送のフリーレン", TitleEnglish: "Frieren: Beyond Journey's End",
		CoverImageURL: anilistCover("bx154587"), BannerImageURL: anilistBanner("154587"),
		Description: "<p>勇者一行打倒魔王之后的故事。</p>", Season: "FALL", SeasonYear: 2026,
		Episodes: ptr(28), AverageScore: 90.6, Genres: []string{"Adventure", "Drama", "Fantasy"},
		Status: "RELEASING", Format: "TV", Duration: 24, Source: "MANGA", StartDate: "2026-10-03",
		TrailerID: "qgQ9yXb7G7g", TrailerSite: "youtube", Studios: []string{"Madhouse"},
	}
}

func bocchiRow() animego.CatalogMedia {
	return animego.CatalogMedia{
		AnilistID: goldenBocchiID, TitleChinese: "孤独摇滚！", TitleRomaji: "Bocchi the Rock!",
		CoverImageURL: anilistCover("bx130003"), Season: "SUMMER", SeasonYear: 2026,
		Episodes: ptr(12), AverageScore: 88.2, Genres: []string{"Comedy", "Music"},
		Status: "FINISHED", Format: "TV",
	}
}

func upcomingRow(id int, title string) animego.CatalogMedia {
	return animego.CatalogMedia{AnilistID: id, TitleChinese: title, Status: "NOT_YET_RELEASED", Format: "TV", Genres: []string{}}
}

func movieRow() animego.CatalogMedia {
	return animego.CatalogMedia{
		AnilistID: goldenMovieID, TitleChinese: "剧场版 示例", Status: "FINISHED", Format: "MOVIE",
		Duration: 105, Genres: []string{"Drama"}, CoverImageURL: anilistCover("bx180002"),
	}
}

// frierenDetail 是作品详情：关系、推荐、角色、逐集标题都有。
func frierenDetail() animego.CatalogMedia {
	m := frierenRow()
	m.DescriptionCn = "勇者辛美尔一行打倒魔王之后，精灵魔法使芙莉莲踏上了新的旅程。"
	sequel := upcomingRow(goldenNextID, "葬送的芙莉莲 第二季")
	m.Relations = []animego.CatalogRelation{
		{CatalogMedia: sequel, RelationType: "SEQUEL"},
		{CatalogMedia: animego.CatalogMedia{AnilistID: 900001, TitleChinese: "原作漫画", Format: "MANGA"}, RelationType: "SOURCE"},
	}
	m.Recommendations = []animego.CatalogMedia{bocchiRow()}
	m.Characters = []animego.CatalogCharacter{{
		NameCn: "芙莉莲", NameJa: "フリーレン", ImageURL: "https://s4.anilist.co/file/anilistcdn/character/large/b176754.png",
		Role: "MAIN", VoiceActorJa: "種﨑敦美", VoiceActorImageURL: "https://s4.anilist.co/file/anilistcdn/staff/large/n106297.png",
	}}
	m.EpisodeTitles = []animego.EpisodeTitle{{Episode: 1, Name: "The Journey's End", NameCn: "旅途的终点"}, {Episode: 2, Name: "It Didn't Have to Be Magic..."}}
	return m
}

// goldenTones 是海报取色的替身：两部作品的封面算出了色相，其余（没下载完、灰度封面）是 null。
type goldenTones struct{}

func (goldenTones) PosterTones(source string) *artcache.PosterTones {
	hues := map[string]float64{anilistCover("bx154587"): 152.4, anilistCover("bx130003"): 341.8}
	hue, ok := hues[source]
	if !ok {
		return nil
	}
	tones := artcache.LadderFor(hue)
	return &tones
}

type goldenCatalog struct {
	failGems bool
	// failAll：上游整个连不上（离线启动）
	failAll bool
}

var errGoldenOffline = errors.New("dial tcp: network is unreachable")

func (g goldenCatalog) Trending(context.Context) ([]animego.CatalogMedia, error) {
	if g.failAll {
		return nil, errGoldenOffline
	}
	return []animego.CatalogMedia{{AnilistID: goldenFrierenID, TitleChinese: "葬送的芙莉莲"}, bocchiRow()}, nil
}

func (g goldenCatalog) Seasonal(_ context.Context, season string, year int) ([]animego.CatalogMedia, error) {
	if g.failAll {
		return nil, errGoldenOffline
	}
	switch {
	case season == "FALL" && year == 2026:
		return []animego.CatalogMedia{frierenRow(), upcomingRow(goldenUpcoming, "秋季待播作品"), movieRow()}, nil
	case season == "SUMMER" && year == 2026:
		return []animego.CatalogMedia{bocchiRow()}, nil
	case season == "WINTER" && year == 2027:
		return []animego.CatalogMedia{upcomingRow(goldenNextID, "葬送的芙莉莲 第二季")}, nil
	}
	return []animego.CatalogMedia{}, nil
}

func (g goldenCatalog) Gems(context.Context) ([]animego.CatalogMedia, error) {
	if g.failGems || g.failAll {
		return nil, errors.New("upstream 502")
	}
	return []animego.CatalogMedia{bocchiRow()}, nil
}

func (g goldenCatalog) YearlyTop(context.Context, int) ([]animego.CatalogMedia, error) {
	if g.failAll {
		return nil, errGoldenOffline
	}
	return []animego.CatalogMedia{frierenRow()}, nil
}

func (g goldenCatalog) Schedule(context.Context) (animego.ScheduleData, error) {
	if g.failAll {
		return animego.ScheduleData{}, errGoldenOffline
	}
	at := func(d time.Duration) int64 { return goldenNow.Add(d).Unix() }
	return animego.ScheduleData{Today: "2026-10-15", Groups: map[string][]animego.ScheduleItem{
		"2026-10-15": {
			{CatalogMedia: frierenRow(), ScheduleID: 1, Episode: 7, AiringAt: at(-24 * time.Hour)},
			{CatalogMedia: frierenRow(), ScheduleID: 2, Episode: 8, AiringAt: at(6 * 24 * time.Hour)},
			{CatalogMedia: bocchiRow(), ScheduleID: 3, Episode: 3, AiringAt: at(-3 * time.Hour)},
		},
	}}, nil
}

func (g goldenCatalog) Detail(_ context.Context, id int) (animego.CatalogMedia, error) {
	if g.failAll {
		return animego.CatalogMedia{}, errGoldenOffline
	}
	if id == goldenFrierenID {
		return frierenDetail(), nil
	}
	m := bocchiRow()
	m.AnilistID = id
	return m, nil
}

func (goldenCatalog) Search(context.Context, string) ([]animego.CatalogMedia, error) {
	return []animego.CatalogMedia{frierenRow(), upcomingRow(goldenNextID, "葬送的芙莉莲 第二季")}, nil
}

// EpisodeOffset：第二季之前有 28 集（跨季连续编号），其余作品算不出来。
func (goldenCatalog) EpisodeOffset(_ context.Context, id int) (animego.EpisodeOffset, error) {
	if id == goldenNextID {
		return animego.EpisodeOffset{Known: true, Offset: 28}, nil
	}
	return animego.EpisodeOffset{}, nil
}

// goldenLists 是已登录账号的收藏。
type goldenLists struct {
	epoch uint64
}

func (l *goldenLists) LoggedIn() bool            { return l.epoch > 0 }
func (l *goldenLists) SessionGeneration() uint64 { return l.epoch }

func goldenListEntries() []animego.ListEntry {
	return []animego.ListEntry{
		{
			AnilistID: goldenFrierenID, Status: "watching", CurrentEpisode: 7, Score: ptr(9),
			TitleChinese: "葬送的芙莉莲", TitleRomaji: "Sousou no Frieren", TitleNative: "葬送のフリーレン",
			CoverImageURL: anilistCover("bx154587"), BannerImageURL: anilistBanner("154587"), Episodes: ptr(28),
			Season: "FALL", SeasonYear: 2026, AnimeStatus: "RELEASING", LastWatchedAt: "2026-10-14T20:30:00Z",
		},
		{
			AnilistID: goldenBocchiID, Status: "completed", CurrentEpisode: 12,
			TitleChinese: "孤独摇滚！", CoverImageURL: anilistCover("bx130003"), Episodes: ptr(12),
			Season: "SUMMER", SeasonYear: 2026, AnimeStatus: "FINISHED",
		},
	}
}

func (l *goldenLists) ListEntries(context.Context) ([]animego.ListEntry, error) {
	return goldenListEntries(), nil
}

func (l *goldenLists) GetListEntry(_ context.Context, id int) (*animego.ListEntry, error) {
	for _, e := range goldenListEntries() {
		if e.AnilistID == id {
			return &e, nil
		}
	}
	return nil, nil
}

func (l *goldenLists) SaveListEntry(context.Context, int, string, int, *int) error { return nil }
func (l *goldenLists) DeleteListEntry(context.Context, int) error                  { return nil }

// goldenCoverCache 是封面缓存的替身：占用固定；清空后剩一张正在显示的封面没删。
func goldenCoverCache() *fakeCoverCache {
	return &fakeCoverCache{
		usage:   artcache.Usage{Bytes: 52428800, Count: 312, MaxBytes: artcache.MaxBytes},
		after:   artcache.Usage{Bytes: 183296, Count: 1, MaxBytes: artcache.MaxBytes},
		skipped: 1,
	}
}
