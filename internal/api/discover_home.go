package api

import (
	"context"
	"fmt"
	"math"
	"sort"

	"github.com/nagare-project/nagare/internal/animego"
)

// 发现页的投影（GET /api/discover 与 GET /api/discover/gems）：上游行 → HomeMedia / RankedMedia / HomeAiring。
// 与 SummaryMedia 的投影共用标题、简介、图片登记的规则（catalog_projection.go）。

// homeSection 把一块的条目与读取错误装进 HomeSection；出错时条目为空数组（不是 null），界面只藏这一块。
func homeSection[T any](items []T, err error) HomeSection[T] {
	if items == nil {
		items = []T{}
	}
	if err != nil {
		return HomeSection[T]{Items: []T{}, Error: "暂时取不到该板块，请稍后重试：" + err.Error()}
	}
	return HomeSection[T]{Items: items}
}

func (s *DiscoverService) homeMedia(r animego.CatalogMedia) HomeMedia {
	return HomeMedia{
		AnilistID:    r.AnilistID,
		Title:        catalogTitle(r),
		TitleNative:  r.TitleNative,
		TitleRomaji:  r.TitleRomaji,
		TitleEnglish: r.TitleEnglish,
		Cover:        s.art.Register(r.CoverImageURL),
		Banner:       s.art.Register(r.BannerImageURL),
		Hue:          accentHue(r.PosterAccent),
		Score:        int(math.Round(r.AverageScore)),
		BangumiScore: r.BangumiScore,
		Episodes:     r.Episodes,
		Season:       r.Season,
		Year:         r.SeasonYear,
		Status:       r.Status,
		Format:       r.Format,
		Genres:       append([]string{}, r.Genres...),
		Description:  plainDescription(firstText(r.DescriptionCn, r.Description)),
	}
}

// homeRows 是按上游顺序留下来的行：没有 ID 或标题的丢掉，同一部作品只留第一次出现的那条。
// 先筛再投影：丢掉的行不占图片登记表的位置。
func homeRows(rows []animego.CatalogMedia) []animego.CatalogMedia {
	out := []animego.CatalogMedia{}
	seen := map[int]bool{}
	for _, r := range rows {
		if r.AnilistID < 1 || catalogTitle(r) == "" || seen[r.AnilistID] {
			continue
		}
		seen[r.AnilistID] = true
		out = append(out, r)
	}
	return out
}

func (s *DiscoverService) homeMedias(rows []animego.CatalogMedia) []HomeMedia {
	out := []HomeMedia{}
	for _, r := range homeRows(rows) {
		out = append(out, s.homeMedia(r))
	}
	return out
}

// rankedMedias 是热门榜：上游已按名次排好。缺名次（不该发生）时接在目前见过的最大名次后面，
// 名次总是从 1 起、不与上游给的重复。
func (s *DiscoverService) rankedMedias(rows []animego.CatalogMedia) []RankedMedia {
	out := []RankedMedia{}
	maxRank := 0
	for _, r := range homeRows(rows) {
		rank := r.Rank
		if rank < 1 {
			rank = maxRank + 1
		}
		maxRank = max(maxRank, rank)
		out = append(out, RankedMedia{HomeMedia: s.homeMedia(r), Rank: rank, Watchers: max(0, r.WatcherCount)})
	}
	return out
}

// homeAirings 是近七日的放送，按时间先后（同一时刻按作品 ID）；同一部作品同一集同一时刻只留一条。
// 不按上游的日期分组：上游按它自己服务器的日子分，「今天」由界面按本机的日子算。
func (s *DiscoverService) homeAirings(schedule animego.ScheduleData) []HomeAiring {
	out := []HomeAiring{}
	seen := map[string]bool{}
	for _, batch := range schedule.Groups {
		for _, r := range batch {
			title := catalogTitle(r.CatalogMedia)
			key := fmt.Sprintf("%d:%d:%d", r.AnilistID, r.Episode, r.AiringAt)
			if r.AnilistID < 1 || title == "" || r.Episode < 1 || r.AiringAt <= 0 || seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, HomeAiring{
				ScheduleID: r.ScheduleID, AnilistID: r.AnilistID, Episode: r.Episode, AiringAt: r.AiringAt,
				Title: title, Cover: s.art.Register(r.CoverImageURL), Hue: accentHue(r.PosterAccent),
			})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.AiringAt != b.AiringAt {
			return a.AiringAt < b.AiringAt
		}
		if a.AnilistID != b.AnilistID {
			return a.AnilistID < b.AnilistID
		}
		return a.Episode < b.Episode
	})
	return out
}

// MoreGems 是「换一批」：上游每次调用随机挑一批，所以不走缓存。连点只排队、不并发打上游
// （排队中的请求被取消就直接返回，不再出站）；失败照实返回，界面保留当前这一批。
// 上游一部都没给（或给的全不能用）时 items 是空数组，界面同样保留当前这一批。
func (s *DiscoverService) MoreGems(ctx context.Context) (GemsView, error) {
	select {
	case s.gemsSlot <- struct{}{}:
		defer func() { <-s.gemsSlot }()
	case <-ctx.Done():
		return GemsView{}, ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return GemsView{}, err
	}
	rows, err := s.source.Gems(ctx)
	if err != nil {
		return GemsView{}, err
	}
	return GemsView{Items: s.homeMedias(rows)}, nil
}
