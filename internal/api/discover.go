package api

import (
	"context"
	"fmt"
	"github.com/nagare-project/nagare/internal/animego"
	"golang.org/x/sync/errgroup"
	"sort"
	"sync"
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
	tones  posterToneSlot // 海报取色（poster_tones.go），启动时注入
	// 目录搜索的全局节流（见 catalog_search.go）；sleep 可在测试里换掉
	searchMu   sync.Mutex
	lastSearch time.Time
	// 「换一批」（MoreGems）连点只排队，不并发打上游
	gemsMu sync.Mutex
	sleep  func(context.Context, time.Duration) error
}

// sleepCtx 等 d 或等到 ctx 结束（浏览器关掉了页面就不必再等着出站）。
func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func NewDiscoverService(source CatalogReader, art *RemoteArt) *DiscoverService {
	return &DiscoverService{source: source, art: art, cache: newReadCache(), sleep: sleepCtx}
}

// useDisk 给目录缓存接上磁盘层（见 catalog_disk.go）；只在构造之后、开始服务之前调用。
func (s *DiscoverService) useDisk(d *catalogDisk) {
	s.cache.disk = d
	d.now = func() time.Time { return s.cache.now() } // 与内存层共用一只钟（测试会换掉它）
}

func (s *DiscoverService) schedule(ctx context.Context) (animego.ScheduleData, cacheMeta, error) {
	return cachedReadMeta(ctx, s.cache, "schedule", 30*time.Minute, s.source.Schedule)
}

// detailRaw 取作品详情的上游原始数据（与 Detail 共用 detail:<id> 缓存）。
// 媒体库关联要存封面的原始地址，投影之后就只剩 /art 键了。
func (s *DiscoverService) detailRaw(ctx context.Context, id int) (animego.CatalogMedia, error) {
	raw, _, err := s.detailRawMeta(ctx, id)
	return raw, err
}

func (s *DiscoverService) detailRawMeta(ctx context.Context, id int) (animego.CatalogMedia, cacheMeta, error) {
	return cachedReadMeta(ctx, s.cache, fmt.Sprintf("detail:%d", id), 10*time.Minute, func(ctx context.Context) (animego.CatalogMedia, error) {
		raw, err := s.source.Detail(ctx, id)
		if err != nil {
			return raw, err
		}
		if raw.AnilistID != id || catalogTitle(raw) == "" {
			return raw, &animego.Error{Kind: animego.ErrDecode, Op: "detail", Err: fmt.Errorf("作品详情 ID 不一致或缺少标题")}
		}
		return raw, nil
	})
}

func (s *DiscoverService) Detail(ctx context.Context, id int) (SummaryMedia, error) {
	raw, meta, err := s.detailRawMeta(ctx, id)
	if err != nil {
		return SummaryMedia{}, err
	}
	m := s.summary(raw)
	m.Freshness = freshnessOf(meta)
	// 详情复用已有放送快照；不因补倒计时再增加一个阻塞上游请求。
	if value, ok := s.cache.peek("schedule"); ok {
		m.NextAiring = nextAirings(value.(animego.ScheduleData), s.cache.now().Unix())[id]
		m.RecentAiring = recentAirings(value.(animego.ScheduleData), s.cache.now().Unix())[id]
	}
	return m, nil
}

// View 是发现页：本季全部、热门、完结佳作、年度榜、近七日放送，五块各自读、各自缓存、各自降级。
func (s *DiscoverService) View(ctx context.Context) (DiscoverView, error) {
	now := s.cache.now()
	quarter := (int(now.Month()) - 1) / 3
	season, year := seasonNames[quarter], now.Year()
	// 本季与季度页（GET /api/seasonal）共用 seasonal:<季>:<年> 一个键：翻到当季时不重复请求。
	keys := []string{
		fmt.Sprintf("seasonal:%s:%d", season, year),
		fmt.Sprintf("trending:%d:%d", year, quarter),
		fmt.Sprintf("gems:%d:%d", year, quarter),
		fmt.Sprintf("yearly:%d:%d", year, quarter),
	}
	loaders := []func(context.Context) ([]animego.CatalogMedia, error){
		func(ctx context.Context) ([]animego.CatalogMedia, error) { return s.source.Seasonal(ctx, season, year) },
		s.source.Trending,
		s.source.Gems,
		func(ctx context.Context) ([]animego.CatalogMedia, error) { return s.source.YearlyTop(ctx, year) },
	}
	rows := make([][]animego.CatalogMedia, len(keys))
	failures := make([]error, len(keys))
	metas := make([]cacheMeta, len(keys)+1) // 最后一格是放送
	var schedule animego.ScheduleData
	var scheduleErr error
	var group errgroup.Group
	group.SetLimit(4)
	for i := range keys {
		group.Go(func() error {
			rows[i], metas[i], failures[i] = cachedReadMeta(ctx, s.cache, keys[i], 10*time.Minute, loaders[i])
			return nil
		})
	}
	group.Go(func() error { schedule, metas[len(keys)], scheduleErr = s.schedule(ctx); return nil })
	if err := group.Wait(); err != nil {
		return DiscoverView{}, err
	}
	if err := ctx.Err(); err != nil {
		return DiscoverView{}, err
	}
	fetchedAt := now
	for _, m := range metas {
		if !m.fetchedAt.IsZero() && m.fetchedAt.Before(fetchedAt) {
			fetchedAt = m.fetchedAt
		}
	}
	view := DiscoverView{
		Season:      season,
		Year:        year,
		Seasonal:    homeSection(s.homeMedias(rows[0]), failures[0]),
		SeasonTotal: len(rows[0]),
		Trending:    homeSection(s.rankedMedias(rows[1]), failures[1]),
		Gems:        homeSection(s.homeMedias(rows[2]), failures[2]),
		Yearly:      homeSection(s.homeMedias(rows[3]), failures[3]),
		Schedule:    homeSection(s.homeAirings(schedule), scheduleErr),
		FetchedAt:   fetchedAt.Unix(),
		Freshness:   freshnessOf(metas...),
	}
	return view, nil
}

// ScheduleView 不把未建立作品绑定的条目说成缺集；只报告本机是否有可信作品关联。
func (s *DiscoverService) ScheduleView(ctx context.Context, inLibrary func(int) bool) (ScheduleView, error) {
	raw, meta, err := s.schedule(ctx)
	if err != nil {
		return ScheduleView{}, err
	}
	out := ScheduleView{Airings: []AiringView{}, FetchedAt: meta.fetchedAt.Unix(), Freshness: freshnessOf(meta)}
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
			out.Airings = append(out.Airings, AiringView{m.AnilistID, r.Episode, r.AiringAt, m.Title, m.Cover, m.Format, found, m.PosterTones})
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

// 所有目录入口共享最近已播记录，避免季番卡片或详情在暂无下一次排期时丢失选集。
func recentAirings(schedule animego.ScheduleData, now int64) map[int]*Airing {
	out := map[int]*Airing{}
	for _, batch := range schedule.Groups {
		for _, a := range batch {
			if a.AnilistID < 1 || a.Episode < 1 || a.AiringAt <= 0 || a.AiringAt > now {
				continue
			}
			if old := out[a.AnilistID]; old == nil || a.AiringAt > old.At {
				out[a.AnilistID] = &Airing{Episode: a.Episode, At: a.AiringAt}
			}
		}
	}
	return out
}
