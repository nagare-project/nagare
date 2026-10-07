package api

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/nagare-project/nagare/internal/animego"
	"github.com/nagare-project/nagare/internal/player"
)

// 「看完」回写前把文件里的集号换成作品集号要用的数据：作品总集数 + 集号偏移
// （规则与来由见 internal/player/episodes.go）。只走 animego 的元数据读（红线 2）。

// EpisodeSpaceSource 是查集号空间要用的两个目录读接口。
type EpisodeSpaceSource interface {
	Detail(context.Context, int) (animego.CatalogMedia, error)
	EpisodeOffset(context.Context, int) (animego.EpisodeOffset, error)
}

var _ EpisodeSpaceSource = (*animego.Client)(nil)

// episodeSpaceTTL：总集数与前作关系很少变（连载中的作品补上总集数，晚几小时知道无妨）。
// 要比一集的时长长得多：开播时预取的结果得撑到看完回写那一刻，否则预取白做。
const episodeSpaceTTL = 6 * time.Hour

// EpisodeSpaces 查作品的集号空间，按作品缓存（同键请求合并、失败不缓存）。
type EpisodeSpaces struct {
	source EpisodeSpaceSource
	cache  *readCache
}

// NewEpisodeSpaces 构造查询服务；source 为 nil 时 Lookup 一律报错（调用方按文件里的集号回写）。
func NewEpisodeSpaces(source EpisodeSpaceSource) *EpisodeSpaces {
	return &EpisodeSpaces{source: source, cache: newReadCache()}
}

// Lookup 返回作品的集号空间。总集数取不到算失败（换算无从谈起）；偏移取不到只算「这次没查到」——
// 不能当 0 用，而有总集数仍然足以挡住超出范围的集号。两样分开缓存：偏移一时取不到不能
// 连同总集数一起缓存，否则那几个小时里跨季编号的作品都会被白白拦下。
func (s *EpisodeSpaces) Lookup(ctx context.Context, anilistID int) (player.EpisodeSpace, error) {
	if s.source == nil {
		return player.EpisodeSpace{}, errors.New("作品目录不可用")
	}
	total, err := cachedRead(ctx, s.cache, fmt.Sprintf("total:%d", anilistID), episodeSpaceTTL, func(ctx context.Context) (int, error) {
		detail, err := s.source.Detail(ctx, anilistID)
		if err != nil {
			return 0, err
		}
		if detail.Episodes == nil || *detail.Episodes < 0 {
			return 0, nil
		}
		return *detail.Episodes, nil
	})
	if err != nil {
		return player.EpisodeSpace{}, err
	}
	space := player.EpisodeSpace{Total: total}
	offset, err := cachedRead(ctx, s.cache, fmt.Sprintf("offset:%d", anilistID), episodeSpaceTTL, func(ctx context.Context) (animego.EpisodeOffset, error) {
		return s.source.EpisodeOffset(ctx, anilistID)
	})
	switch {
	case err != nil:
		log.Printf("api: 查集号偏移失败（anilistId=%d），这次按未知处理：%v", anilistID, err)
		space.OffsetUnavailable = true
	case offset.Known && offset.Offset >= 0:
		space.Offset, space.OffsetKnown = offset.Offset, true
	}
	return space, nil
}
