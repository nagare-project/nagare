package player

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/nagare-project/nagare/internal/library"
	"github.com/nagare-project/nagare/internal/store"
)

// 媒体库里的手动作品关联在播放时的作用（用户 2026-10-07 定：按作品分组认）：
//   - 认定为作品 X：缓存的匹配只有指向 X 才沿用；重新匹配时先用 X 的标题、结果必须对上 X；
//     弹幕匹配不上也照样按 X 回写进度 —— 关联是用户认定的事实，弹幕只是附带的。
//     但 animego 已经说了「X 没有这一集」、或集号超出 X 的总集数时不回写（多半是绝对集号）；
//     特典 / OVA / NCOP 这类附加内容在分组里，却不是 X 的第 N 集：不匹配弹幕、不回写。
//   - 标为 none：不匹配、不拉弹幕、不回写。
//   - 关联随时可能被改，而匹配在后台跑：落盘前、回写前后都再核对一次。

// extraKinds 是明确不是正片的文件类型（library.ParseEpisodeKind 的取值）。bonus 不在里面：
// 「Disc 2 - 03」这类 BD 正片也会被归成 bonus；unknown 没有集号，本来就不会回写。
var extraKinds = map[string]bool{
	"commentary": true, "ncop": true, "nced": true, "menu": true, "trailer": true,
	"interview": true, "wp": true, "cm": true, "movie": true, "sp": true, "ova": true, "pv": true,
}

// association 查文件所在作品分组的手动关联；没有装配查询函数时一律当没有关联。
func (m *Manager) association(fileID string) (store.Association, bool) {
	if m.opts.Association == nil {
		return store.Association{}, false
	}
	return m.opts.Association(fileID)
}

// agreesWithAssociation：这条匹配与文件当前的关联不冲突（没有关联时一律不冲突）。
func (m *Manager) agreesWithAssociation(fileID string, b store.Binding) bool {
	a, ok := m.association(fileID)
	if !ok {
		return true
	}
	return a.Mode == store.AssociationManual && b.AnilistID == a.AnilistID
}

// saveBinding 落盘匹配结果。匹配跑的这段时间里关联可能被改了：对不上就不落盘
// （读的时候也会再核对，这里只是不留一条注定被忽略的记录）。
func (m *Manager) saveBinding(fileID string, b store.Binding) {
	if !m.agreesWithAssociation(fileID, b) {
		log.Printf("player: 匹配期间作品关联已改，丢弃这次匹配结果")
		return
	}
	if err := m.opts.Store.SetBinding(fileID, b); err != nil {
		log.Printf("player: 保存匹配结果失败：%v", err)
	}
}

// ensureAssociatedBinding 是认定为作品 a 之后的匹配：弹幕尽量匹配，进度尽量记到 a。
func (m *Manager) ensureAssociatedBinding(ctx context.Context, src MediaSource, item library.Item, a store.Association) (store.Binding, DanmakuInfo) {
	if extraKinds[item.ParsedKind] {
		// 拿正片第 N 集的弹幕配特典，或把 OVA 记成第 N 集，都是错的
		return store.Binding{}, DanmakuInfo{State: "unmatched", Reason: "这是特典、OVA 一类的附加内容：不按正片匹配弹幕，也不回写进度"}
	}
	episode := episodeNumber(item)
	if b, ok := m.fullBinding(item.FileID, a); ok {
		switch {
		case b.Episode == episode:
			return b, DanmakuInfo{State: "ok"}
		case b.Episode == 0 && episode > 0:
			// 旧数据里集号没记上：补上，弹幕那一集本来就是按这个集号匹配的
			b.Episode = episode
			m.saveBinding(item.FileID, b)
			return b, DanmakuInfo{State: "ok"}
		}
		// 集号变了（共享语料更新）：弹幕那一集也跟着不对了，重新匹配
	}

	// 文件名里的标题正是认错的来源，先用认定作品的标题
	q := matchQuery{wantAnilist: a.AnilistID, keywords: appendUniqueKeywords([]string{a.Title}, []string{parsedTitle(item)})}
	b, dan, missing := m.matchBinding(ctx, src, item, q)
	if dan.State == "ok" {
		m.saveBinding(item.FileID, b)
		return b, dan
	}
	// 同一集重播时，另一次后台匹配可能刚拿到完整结果：别用兜底把它盖掉
	if full, ok := m.fullBinding(item.FileID, a); ok {
		return full, DanmakuInfo{State: "ok"}
	}
	if ok, why := progressOnlyAllowed(a, episode, missing); !ok {
		if why != "" {
			dan.Reason += "；" + why
		}
		return store.Binding{}, dan
	}
	return m.progressOnlyBinding(item, a, episode), m.withSyncNote(dan, a)
}

// fullBinding：store 里已有一条指向 a、带弹幕的匹配。
func (m *Manager) fullBinding(fileID string, a store.Association) (store.Binding, bool) {
	b, ok := m.opts.Store.Binding(fileID)
	return b, ok && b.DandanEpisodeID != 0 && b.AnilistID == a.AnilistID
}

// progressOnlyAllowed：弹幕没匹配上时，能不能只凭关联回写这一集；不能时附一句给用户看的原因
// （集号认不出来的原因已经写在弹幕状态里，不再重复）。
func progressOnlyAllowed(a store.Association, episode int, missing bool) (bool, string) {
	switch {
	case episode <= 0:
		return false, "" // 不猜第 1 集
	case missing:
		return false, "看完不回写进度" // 「匹配结果里没有第 N 集」已经写在弹幕状态里
	case a.Episodes > 0 && episode > a.Episodes:
		return false, fmt.Sprintf("「%s」只有 %d 集，第 %d 集看完不回写进度", a.Title, a.Episodes, episode)
	}
	return true, ""
}

// progressOnlyBinding 落一条只有作品与集号的匹配（没有弹幕）：回写前要核对 store 里的匹配，
// 所以它也要落盘。与上一次的兜底只差匹配时间时沿用旧的，免得每播一次都重写状态文件、换一次封面地址。
func (m *Manager) progressOnlyBinding(item library.Item, a store.Association, episode int) store.Binding {
	b := store.Binding{AnilistID: a.AnilistID, Episode: episode, Title: a.Title, CoverURL: a.CoverURL}
	if prev, ok := m.opts.Store.Binding(item.FileID); ok {
		prevAt := prev.MatchedAt
		prev.MatchedAt = 0
		if prev == b {
			b.MatchedAt = prevAt
			return b
		}
	}
	b.MatchedAt = time.Now().UnixMilli()
	m.saveBinding(item.FileID, b)
	return b
}

// withSyncNote 在弹幕状态里说一句「看完仍会记到 X」—— 只在确实会回写时说（登录了才会回写）。
func (m *Manager) withSyncNote(dan DanmakuInfo, a store.Association) DanmakuInfo {
	if m.opts.Client != nil && m.opts.Client.LoggedIn() && a.Title != "" {
		dan.Reason += "；看完仍会记到「" + a.Title + "」"
	}
	return dan
}
