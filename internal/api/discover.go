package api

import (
	"context"
	"fmt"
	"github.com/nagare-project/nagare/internal/animego"
	"golang.org/x/sync/errgroup"
	"sort"
	"strings"
	"time"
)

// CatalogReader 是公开元数据的窄接口；handler 测试不需要真实网络客户端。
type CatalogReader interface {
	Trending(context.Context) ([]animego.CatalogMedia, error)
	Seasonal(context.Context, string, int) ([]animego.CatalogMedia, error)
	Gems(context.Context) ([]animego.CatalogMedia, error)
	YearlyTop(context.Context, int) ([]animego.CatalogMedia, error)
	Schedule(context.Context) (animego.ScheduleData, error)
	Detail(context.Context, int) (animego.CatalogMedia, error)
}
type DiscoverService struct {
	source CatalogReader
	art    *RemoteArt
	cache  *readCache
}

func NewDiscoverService(source CatalogReader, art *RemoteArt) *DiscoverService {
	return &DiscoverService{source: source, art: art, cache: newReadCache()}
}
func (s *DiscoverService) schedule(ctx context.Context) (animego.ScheduleData, error) {
	return cachedRead(ctx, s.cache, "schedule", 30*time.Minute, s.source.Schedule)
}
func (s *DiscoverService) Detail(ctx context.Context, id int) (SummaryMedia, error) {

	raw, err := cachedRead(ctx, s.cache, fmt.Sprintf("detail:%d", id), 10*time.Minute, func(ctx context.Context) (animego.CatalogMedia, error) {
		raw, err := s.source.Detail(ctx, id)
		if err != nil {
			return raw, err
		}
		if raw.AnilistID != id || firstText(raw.TitleChinese, raw.TitleRomaji, raw.TitleNative, raw.TitleEnglish, raw.Title) == "" {
			return raw, &animego.Error{Kind: animego.ErrDecode, Op: "detail", Err: fmt.Errorf("作品详情 ID 不一致或缺少标题")}
		}
		return raw, nil
	})
	if err != nil {
		return SummaryMedia{}, err
	}
	m := s.summary(raw)
	// 详情复用已有放送快照；不因补倒计时再增加一个阻塞上游请求。
	if value, ok := s.cache.get("schedule"); ok {
		m.NextAiring = nextAirings(value.(animego.ScheduleData), s.cache.now().Unix())[id]
	}
	return m, nil
}

func (s *DiscoverService) View(ctx context.Context) (DiscoverView, error) {
	now := s.cache.now()
	quarter := (int(now.Month()) - 1) / 3
	seasons := []string{"WINTER", "SPRING", "SUMMER", "FALL"}
	year := now.Year()
	pastYear, nextYear := year, year
	if quarter == 0 {
		pastYear--
	}
	if quarter == 3 {
		nextYear++
	}
	keys := []string{"trending", "thisSeason", "pastSeason", "nextSeason", "gems", "yearly"}
	loaders := []func(context.Context) ([]animego.CatalogMedia, error){s.source.Trending,
		func(ctx context.Context) ([]animego.CatalogMedia, error) {
			return s.source.Seasonal(ctx, seasons[quarter], year)
		},
		func(ctx context.Context) ([]animego.CatalogMedia, error) {
			return s.source.Seasonal(ctx, seasons[(quarter+3)%4], pastYear)
		},
		func(ctx context.Context) ([]animego.CatalogMedia, error) {
			return s.source.Seasonal(ctx, seasons[(quarter+1)%4], nextYear)
		}, s.source.Gems,
		func(ctx context.Context) ([]animego.CatalogMedia, error) { return s.source.YearlyTop(ctx, year) }}
	rows := make([][]animego.CatalogMedia, len(keys))
	failures := make([]error, len(keys))
	var schedule animego.ScheduleData
	var scheduleErr error
	var group errgroup.Group
	group.SetLimit(4)
	for i := range keys {
		group.Go(func() error {
			key := fmt.Sprintf("%s:%d:%d", keys[i], year, quarter)
			rows[i], failures[i] = cachedRead(ctx, s.cache, key, 10*time.Minute, loaders[i])
			return nil
		})
	}
	group.Go(func() error { schedule, scheduleErr = s.schedule(ctx); return nil })
	if err := group.Wait(); err != nil {
		return DiscoverView{}, err
	}
	if err := ctx.Err(); err != nil {
		return DiscoverView{}, err
	}
	// 在已取到的季度资料中补全热门项，不为每张卡片扇出详情请求。
	index := map[int]animego.CatalogMedia{}
	for _, batch := range rows[1:] {
		for _, r := range batch {
			if _, exists := index[r.AnilistID]; !exists {
				index[r.AnilistID] = r
			}
		}
	}
	rows[0] = append([]animego.CatalogMedia{}, rows[0]...)
	for i, r := range rows[0] {
		if full, ok := index[r.AnilistID]; ok {
			rows[0][i] = full
		}
	}
	next := nextAirings(schedule, now.Unix())
	fetchedAt := now
	for _, key := range keys {
		at := s.cache.fetchedAt(fmt.Sprintf("%s:%d:%d", key, year, quarter))
		if !at.IsZero() && at.Before(fetchedAt) {
			fetchedAt = at
		}
	}
	if at := s.cache.fetchedAt("schedule"); !at.IsZero() && at.Before(fetchedAt) {
		fetchedAt = at
	}
	section := func(key, title string, items []animego.CatalogMedia, err error) SectionView {
		v := SectionView{Key: key, Title: title, Items: s.summaries(items)}
		for i := range v.Items {
			v.Items[i].NextAiring = next[v.Items[i].AnilistID]
		}
		if err != nil {
			v.Error = "暂时取不到该板块，请稍后重试：" + err.Error()
		}
		return v
	}
	recent := []SummaryMedia{}
	airings := []animego.ScheduleItem{}
	for _, items := range schedule.Groups {
		airings = append(airings, items...)
	}
	sort.Slice(airings, func(i, j int) bool { return airings[i].AiringAt > airings[j].AiringAt })
	seen := map[int]bool{}
	for _, a := range airings {
		if a.AiringAt > now.Unix() || a.Episode < 1 || seen[a.AnilistID] {
			continue
		}
		m := s.summary(a.CatalogMedia)
		if m.Title == "" || m.AnilistID < 1 {
			continue
		}
		m.RecentAiring = &Airing{a.Episode, a.AiringAt}
		recent = append(recent, m)
		seen[a.AnilistID] = true
		if len(recent) == 20 {
			break
		}
	}
	recentSection := SectionView{Key: "recent", Title: "最近已播出", Items: recent}
	if scheduleErr != nil {
		recentSection.Error = "暂时取不到放送信息，请稍后重试：" + scheduleErr.Error()
	}
	upcoming, movies := []animego.CatalogMedia{}, []animego.CatalogMedia{}
	for i, batch := range rows[1:] {
		for _, r := range batch {
			if (i == 0 || i == 2) && r.Status == "NOT_YET_RELEASED" {
				upcoming = append(upcoming, r)
			}
			if r.Format == "MOVIE" {
				movies = append(movies, r)
			}
		}
	}
	derivedError := func(indexes ...int) error {
		messages := []string{}
		for _, i := range indexes {
			if failures[i] != nil {
				messages = append(messages, keys[i]+" 暂不可用")
			}
		}
		if len(messages) > 0 {
			return fmt.Errorf("部分数据未加载：%s", strings.Join(messages, "、"))
		}
		return nil
	}
	return DiscoverView{Sections: []SectionView{
		section("trending", "animego 上在看最多", rows[0], failures[0]), recentSection,
		section("thisSeason", "本季新番", rows[1], failures[1]), section("pastSeason", "上季作品", rows[2], failures[2]),
		section("upcoming", "本季及下季待播", upcoming, derivedError(1, 3)), section("movies", "近期精选剧场版", movies, derivedError(1, 2, 3, 4, 5)),
	}, FetchedAt: fetchedAt.Unix()}, nil
}

// ScheduleView 不把未建立作品绑定的条目说成缺集；只报告本机是否有可信作品关联。
func (s *DiscoverService) ScheduleView(ctx context.Context, inLibrary func(int) bool) (ScheduleView, error) {
	raw, err := s.schedule(ctx)
	if err != nil {
		return ScheduleView{}, err
	}
	out := ScheduleView{Airings: []AiringView{}, FetchedAt: s.cache.fetchedAt("schedule").Unix()}
	seen := map[string]bool{}
	for _, batch := range raw.Groups {
		for _, r := range batch {
			m := s.summary(r.CatalogMedia)
			key := fmt.Sprintf("%d:%d:%d", m.AnilistID, r.Episode, r.AiringAt)
			if m.AnilistID < 1 || m.Title == "" || r.Episode < 1 || r.AiringAt <= 0 || seen[key] {
				continue
			}
			seen[key] = true
			found := false
			if inLibrary != nil {
				found = inLibrary(m.AnilistID)
			}
			out.Airings = append(out.Airings, AiringView{m.AnilistID, r.Episode, r.AiringAt, m.Title, m.Cover, m.Format, found})
		}
	}
	sort.Slice(out.Airings, func(i, j int) bool {
		a, b := out.Airings[i], out.Airings[j]
		if a.AiringAt == b.AiringAt {
			return a.AnilistID < b.AnilistID
		}
		return a.AiringAt < b.AiringAt
	})
	return out, nil
}

func nextAirings(schedule animego.ScheduleData, now int64) map[int]*Airing {
	out := map[int]*Airing{}
	for _, batch := range schedule.Groups {
		for _, a := range batch {
			if a.AnilistID < 1 || a.Episode < 1 || a.AiringAt <= now {
				continue
			}
			if old := out[a.AnilistID]; old == nil || a.AiringAt < old.At {
				out[a.AnilistID] = &Airing{Episode: a.Episode, At: a.AiringAt}
			}
		}
	}
	return out
}
