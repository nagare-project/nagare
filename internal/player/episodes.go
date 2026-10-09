package player

import (
	"context"
	"fmt"
	"log"
	"slices"
)

// 两套集号：
//
//	本地  文件名里的集号。字幕组常常跨季连续编号：前作 28 集时，第二季结局叫 38。
//	作品  目录作品自己的 1..总集数，animego 账号记的是这个。
//
// 「看完」回写之前要换成作品集号。换算规则照 animego 网站的 lib/library/episodeOffset.ts
// （网站自己的媒体库踩过同一个坑：结局 38 推给只有 10 集的第二季）。
// 不换算的后果比网站更糟：nagare 用的逐集标记接口不检查总集数（animego#214），
// 38 会被照单全收，账号进度直接变成 38。

// EpisodeSpace 是一部目录作品的集号空间。
type EpisodeSpace struct {
	// Total 是总集数，0 表示未知（连载中常见）。
	Total int
	// Offset 是之前（沿前作关系）一共有多少集 TV 正片；只有 OffsetKnown 时才有意义。
	// 「算不出来」与「前面没有作品」（Offset=0）是两回事，绝不能混用。
	Offset      int
	OffsetKnown bool
	// OffsetUnavailable：偏移这次没查到（网络等）。与 animego 明说「算不出来」不同，重试可能就有了。
	OffsetUnavailable bool
}

// placement 是这一集该怎么回写：Episode > 0 就写这个作品集号；否则 Reason 说明为什么不写，
// Retry 表示是一时查不到造成的（重看这一集看完时会再试）。
type placement struct {
	Episode int
	Reason  string
	Retry   bool
}

// offsetApplies：这个偏移能不能套到这组集号上 —— 每一集减去偏移后都得落在 1..总集数 里。
// 全有或全无：只换算套得上的那几集，会让同一组文件出现两套编号。
func offsetApplies(numbers []int, total, offset int) bool {
	if offset <= 0 || total <= 0 || len(numbers) == 0 {
		return false
	}
	for _, n := range numbers {
		if site := n - offset; site < 1 || site > total {
			return false
		}
	}
	return true
}

// withEpisode 返回 group ∪ {episode}，不改动 group。
func withEpisode(group []int, episode int) []int {
	if slices.Contains(group, episode) {
		return group
	}
	return append(slices.Clone(group), episode)
}

// siteEpisode 把文件里的集号 episode 换成作品集号。group 是同一作品分组里全部正片的集号
// （必须拿整组判断：单独一个 38，偏移 28 套得上，37 也套得上）。
func siteEpisode(episode int, group []int, space EpisodeSpace) placement {
	numbers := withEpisode(group, episode)
	if space.OffsetKnown && offsetApplies(numbers, space.Total, space.Offset) {
		return placement{Episode: episode - space.Offset}
	}
	highest, startsAtOne := slices.Max(numbers), slices.Contains(numbers, 1)
	if space.Total <= 0 {
		return placeWithoutTotal(episode, numbers, space, startsAtOne)
	}
	if highest <= space.Total {
		return placement{Episode: episode}
	}
	// 组里有集号超出总集数。偏移已知、这组跨过了偏移（前作与本季放在一个文件夹里）：
	// 偏移之后的逐集换算，偏移之内的是前作，不记到这部作品上。
	if space.OffsetKnown && space.Offset > 0 && slices.Min(numbers) <= space.Offset {
		if site := episode - space.Offset; site >= 1 && site <= space.Total {
			return placement{Episode: site}
		}
		return placement{Reason: fmt.Sprintf("文件里的第 %d 集不在这部作品的 %d 集范围里（看起来是前作或后作的集）", episode, space.Total)}
	}
	// 编号从 1 开始的（总集数登记少了、某个文件名被误读出一个大集号）范围内的这一集照写，
	// 只拦超出的；没有第 1 集的（第二季从 13 编起、只有一个结局 38）分不清哪一集对哪一集 ——
	// 宁可不记也不记错：第二季的「13」其实是第 1 集。
	if episode <= space.Total && startsAtOne {
		return placement{Episode: episode}
	}
	return placement{
		Reason: fmt.Sprintf("这组文件编到第 %d 集，而这部作品只有 %d 集（跨季连续编号），算不出文件里的第 %d 集是作品的第几集",
			highest, space.Total, episode),
		Retry: space.OffsetUnavailable,
	}
}

// placeWithoutTotal：连载中还没有总集数时的规则。
func placeWithoutTotal(episode int, numbers []int, space EpisodeSpace, startsAtOne bool) placement {
	// 偏移已知、整组都排在偏移之后：按跨季连续编号换算。自己从 13 起编的分季文件也会被这样
	// 减掉 —— 那是记少；不换算的风险是记多（原来的毛病），记少更好补。
	if space.OffsetKnown && space.Offset > 0 && slices.Min(numbers) > space.Offset {
		return placement{Episode: episode - space.Offset}
	}
	// 偏移这次没查到，而编号又不是从 1 开始：可能正是跨季编号，等查得到再写
	if space.OffsetUnavailable && !startsAtOne {
		return placement{Reason: fmt.Sprintf("暂时查不到这部作品的集号偏移，确认不了文件里的第 %d 集是作品的第几集", episode), Retry: true}
	}
	return placement{Episode: episode}
}

// placeEpisode 查出作品的集号空间并换算。查不到总集数（animego 暂不可达）时：组里有第 1 集，
// 编号多半就是作品集号，照写；否则先不写，重看这一集看完时再试。
func (m *Manager) placeEpisode(ctx context.Context, fileID string, anilistID, episode int) placement {
	if m.opts.EpisodeSpace == nil {
		return placement{Episode: episode}
	}
	var group []int
	if m.opts.GroupEpisodes != nil {
		group = m.opts.GroupEpisodes(fileID)
	}
	space, err := m.opts.EpisodeSpace(ctx, anilistID)
	if err == nil {
		return siteEpisode(episode, group, space)
	}
	log.Printf("player: 查作品集数失败：%v", err)
	if slices.Contains(withEpisode(group, episode), 1) {
		return placement{Episode: episode}
	}
	return placement{Reason: fmt.Sprintf("暂时查不到这部作品的集数，确认不了文件里的第 %d 集是作品的第几集", episode), Retry: true}
}

// seasonLocalEpisode 把跨季连续编号的集号换成作品自己的集号（前作 12 集时，第二季的「15」
// 是第 3 集），给弹幕匹配用：animego 的分季条目里没有第 15 集，按 3 才找得到这一集的弹幕。
// 只在知道是哪部作品时换算；偏移未知（animego 算不出、这次没查到）时不换 —— 拿一个没人
// 确认过的起点去换算集号，正是偏移接口要防的错。
func (m *Manager) seasonLocalEpisode(ctx context.Context, anilistID, episode int) (int, bool) {
	if anilistID <= 0 || episode <= 0 || m.opts.EpisodeSpace == nil {
		return 0, false
	}
	space, err := m.opts.EpisodeSpace(ctx, anilistID)
	if err != nil || !space.OffsetKnown || space.Offset <= 0 {
		return 0, false
	}
	local := episode - space.Offset
	if local < 1 || (space.Total > 0 && local > space.Total) {
		return 0, false
	}
	return local, true
}

// warmEpisodeSpace 在开播、匹配出作品之后预先查好集号空间：回写跑在收尾里、有总超时，
// 别让它在那时才去等两个上游请求。
func (m *Manager) warmEpisodeSpace(anilistID int) {
	// 没登录就不会回写，不必为它去打上游
	if m.opts.EpisodeSpace == nil || anilistID <= 0 || m.opts.Client == nil || !m.opts.Client.LoggedIn() {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), markWatchedTimeout)
		defer cancel()
		if _, err := m.opts.EpisodeSpace(ctx, anilistID); err != nil {
			log.Printf("player: 预取作品集数失败（回写时会再查）：%v", err)
		}
	}()
}
