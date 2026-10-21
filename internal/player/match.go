// 匹配：文件 → animego/dandanplay 剧集绑定（懒哈希、关键词顺序、作品身份校验、跨季集号重试）。
// 结果只影响弹幕与「看完」回写，任何失败都不阻断播放（CQ3）。
package player

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/nagare-project/nagare/internal/animego"
	"github.com/nagare-project/nagare/internal/library"
	"github.com/nagare-project/nagare/internal/store"
)

// ensureBinding 取（或建立）文件与 dandanplay 剧集的绑定；一并返回弹幕链路状态。
// 任何失败都只影响 DanmakuInfo，不影响播放。媒体库里手动认定过作品的文件
// 按关联走（见 association.go）。
//
// item 是调用方持有的 src.Item() 快照，一并传进来是为了不在这里重复取。
func (m *Manager) ensureBinding(ctx context.Context, src MediaSource, item library.Item) (store.Binding, DanmakuInfo) {
	if a, ok := m.association(item.FileID); ok {
		if a.Mode != store.AssociationManual {
			return store.Binding{}, DanmakuInfo{State: "none", Reason: "已在媒体库标为不是目录里的作品：不匹配弹幕，也不回写进度"}
		}
		return m.ensureAssociatedBinding(ctx, src, item, a)
	}
	q := autoQuery(src, item)
	if b, ok := m.opts.Store.Binding(item.FileID); ok && b.DandanEpisodeID != 0 && q.accepts(b) {
		return b, DanmakuInfo{State: "ok"}
	}
	b, dan, missing := m.matchBinding(ctx, src, item, q)
	if missing {
		// 作品认出来了却没有这一集：跨季连续编号的文件（第二季的「15」）在分季条目里
		// 找不到，换成作品自己的集号再试一次
		if local, ok := m.seasonLocalEpisode(ctx, q.wantAnilist, episodeNumber(item)); ok {
			b, dan, _ = m.matchBindingAt(ctx, src, item, q, local)
		}
	}
	if dan.State == "ok" {
		m.saveBinding(item.FileID, b)
	}
	return b, dan
}

// episodeNumber 取文件的集号；认不出来返回 0。
//
// 集号无法识别时【不猜 1】：猜错会把剧场版/OVA/特典当"第1集"匹配，
// 播完还会拿 MarkWatched(anilistId, 1) 污染用户真实的 animego 账号。
func episodeNumber(item library.Item) int {
	if item.Episode != nil {
		return *item.Episode
	}
	if item.ParsedNumber != nil {
		return *item.ParsedNumber
	}
	return 0
}

func parsedTitle(item library.Item) string {
	if item.ParsedTitle != nil {
		return *item.ParsedTitle
	}
	return ""
}

// matchQuery 是一次匹配的作品身份约束：wantAnilist > 0 时结果必须是这部作品，
// keywords 按顺序试（空关键词也照发：服务端能靠文件指纹命中；列表为空则不发请求）。
type matchQuery struct {
	wantAnilist int
	keywords    []string
}

// accepts：缓存的匹配能不能沿用。没有作品身份时什么都沿用（与改动前一致）；带着身份来的
// 播放只认同一部作品 —— 之前没有身份时（从搜索页播、或旧版本）匹配错的作品会被缓存下来，
// 不拦的话此后每次都沿用：弹幕是别的番的，看完还记到别的番上。
func (q matchQuery) accepts(b store.Binding) bool {
	return q.wantAnilist <= 0 || b.AnilistID == q.wantAnilist
}

// autoQuery 是没有手动关联时的匹配条件。知道自己在目录里是哪部作品的源（在线候选、从作品页
// 播放的磁力）带来两样东西：校验用的 anilistId，和文件名标题失手时可以再试的目录标题；
// 本地文件和不带身份的源没有这些，只用文件名里的标题试一次（标题解析不出来也照发空关键词）。
func autoQuery(src MediaSource, item library.Item) matchQuery {
	title := parsedTitle(item)
	if hinted, ok := src.(MatchHinted); ok {
		if want, alts := hinted.MatchHints(); want > 0 || len(alts) > 0 {
			keywords := appendUniqueKeywords([]string{title}, alts)
			if len(keywords) == 0 {
				// 一个能用的标题都没有：照发一次空关键词，服务端还能靠文件指纹命中
				keywords = []string{""}
			}
			return matchQuery{wantAnilist: want, keywords: keywords}
		}
	}
	return matchQuery{keywords: []string{title}}
}

// matchBinding 向 animego 匹配这个文件，不落盘。第三个返回值：作品认出来了，
// 但匹配结果里没有这一集（绝对集号配上分季条目时常见）。
func (m *Manager) matchBinding(ctx context.Context, src MediaSource, item library.Item, q matchQuery) (store.Binding, DanmakuInfo, bool) {
	return m.matchBindingAt(ctx, src, item, q, episodeNumber(item))
}

// matchBindingAt 按指定集号向 animego 要弹幕：通常就是文件里的集号，跨季连续编号时是换算后的
// 作品集号。换算后的集号只用于这次请求与查弹幕那一集；绑定里记的仍是文件里的集号 ——
// 回写前 placeEpisode 统一把它换成作品集号，绑定里已经换过一次就会被再换一次。
func (m *Manager) matchBindingAt(ctx context.Context, src MediaSource, item library.Item, q matchQuery, episode int) (store.Binding, DanmakuInfo, bool) {
	if m.opts.Client == nil {
		return store.Binding{}, DanmakuInfo{State: "none", Reason: "未配置 animego 服务"}, false
	}
	hash, ok := m.fileHash(ctx, src, item)
	if !ok {
		return store.Binding{}, DanmakuInfo{State: "unavailable", Reason: "计算文件指纹失败，弹幕匹配跳过"}, false
	}
	if episode <= 0 {
		return store.Binding{}, DanmakuInfo{State: "unmatched", Reason: "无法识别集号，弹幕匹配跳过"}, false
	}

	res, stray, err := m.firstMatch(ctx, item, hash, episode, q)
	if err != nil {
		return store.Binding{}, DanmakuInfo{State: "unavailable", Reason: classifyAnimegoErr(err)}, false
	}
	if !res.Matched {
		if stray > 0 {
			return store.Binding{}, DanmakuInfo{State: "unmatched", Reason: fmt.Sprintf("关键词只匹配到另一部作品（anilistId %d），弹幕已跳过", stray)}, false
		}
		return store.Binding{}, DanmakuInfo{State: "unmatched", Reason: "未匹配到对应剧集，弹幕不可用"}, false
	}
	ref, ok := res.EpisodeMap[episode]
	if !ok {
		return store.Binding{}, DanmakuInfo{State: "unmatched", Reason: fmt.Sprintf("匹配结果里没有第 %d 集", episode)}, true
	}

	anilistID := res.AnilistID
	if anilistID == 0 {
		// phase1 命中可能不带 anilistId；作品身份已知时用它，进度才能回写到正确的作品。
		anilistID = q.wantAnilist
	}
	return store.Binding{
		AnilistID:       anilistID,
		DandanEpisodeID: ref.DandanEpisodeID,
		Episode:         episodeNumber(item),
		Title:           firstNonEmpty(res.TitleChinese, res.TitleNative, parsedTitle(item)),
		EpisodeTitle:    ref.Title,
		CoverURL:        res.CoverImageURL,
		MatchedAt:       time.Now().UnixMilli(),
	}, DanmakuInfo{State: "ok"}, false
}

// fileHash 取（懒算并缓存）文件首 16MB 指纹；在线媒体没有指纹时返回空串。
// 第二个返回值为 false 表示算的时候出错了。
func (m *Manager) fileHash(ctx context.Context, src MediaSource, item library.Item) (string, bool) {
	if hash := m.opts.Store.Hash(item.FileID); hash != "" {
		return hash, true
	}
	hash, err := src.Hash16M(ctx)
	switch {
	case errors.Is(err, ErrNoFingerprint):
		// 在线媒体没有指纹：hash 留空，服务端只能靠文件名/关键词（见 MatchInput.FileHash）。
		return "", true
	case err != nil:
		return "", false
	}
	if err := m.opts.Store.SetHash(item.FileID, hash); err != nil {
		// 缓存写失败不致命：下次再算一遍。
		log.Printf("player: 缓存 hash 失败：%v", err)
	}
	return hash, true
}

// firstMatch 按顺序试关键词，返回第一个命中（且作品身份对得上）的结果；都没命中时
// res.Matched 为 false，stray 是被身份校验挡掉的那部作品。
func (m *Manager) firstMatch(ctx context.Context, item library.Item, hash string, episode int, q matchQuery) (res animego.MatchResult, stray int, err error) {
	for _, kw := range q.keywords {
		r, err := m.opts.Client.Match(ctx, animego.MatchInput{
			FileName: item.FileName,
			FileHash: hash,
			FileSize: item.Size,
			Episode:  episode,
			Keyword:  kw,
		})
		if err != nil {
			return animego.MatchResult{}, 0, err
		}
		if !r.Matched {
			continue
		}
		// 关键词匹配对同名不同代/同系列不同季会命中别的作品：弹幕会错，播完还会把
		// 错的作品标成已看。目录身份已知时必须对上，对不上就换下一个关键词。
		if q.wantAnilist > 0 && r.AnilistID > 0 && r.AnilistID != q.wantAnilist {
			stray = r.AnilistID
			continue
		}
		return r, 0, nil
	}
	return animego.MatchResult{}, stray, nil
}
