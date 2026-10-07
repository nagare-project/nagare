package api

import (
	"log"
	"math"
	"net/http"
	"net/url"
	"slices"
	"sort"
	"strconv"
	"time"

	errs "github.com/nagare-project/nagare/internal/errors"
	"github.com/nagare-project/nagare/internal/httpserver"
	"github.com/nagare-project/nagare/internal/library"
	"github.com/nagare-project/nagare/internal/store"
)

// 本地作品分组 ↔ 目录作品的手动关联（PUT /api/library/association）。
//
// 自动匹配（文件名 + 16MB hash 交给 animego）会认错，而认错的后果是每次看完都把进度
// 写到别的作品上。关联让用户说了算：按作品分组（clusterKey）认，存进 store；
// 播放时以它为准（下一步接入 player）。只用到 animego 的元数据读一条线（红线 2）。

const (
	// maxClusterKeyBytes 是请求里 clusterKey 的长度上限。正常的键是 8 位 hex；
	// 认不出标题的分组退化为目录相对路径，也不会超过路径长度上限。
	maxClusterKeyBytes = 4096
	// maxAssocTitleRunes / maxAssocCoverBytes 限制落盘的上游字段：每次写进度都会整份重写
	// state.json，上游给一个异常大的值不该让之后的每次写入都背着它。
	maxAssocTitleRunes = 200
	maxAssocCoverBytes = 2048
	// assocCoverPrefix 是封面端点下关联封面的子路径：/art/<能力段>/assoc/<clusterKey>。
	assocCoverPrefix = "assoc/"
)

// ViewAssociation 是作品分组的手动关联投影；nil 表示没认过，走自动匹配。
type ViewAssociation struct {
	Mode      string `json:"mode"` // manual | none
	AnilistID int    `json:"anilistId,omitempty"`
	Title     string `json:"title,omitempty"`
	SetAt     int64  `json:"setAt"`
	// SyncedElsewhere 是已经看完并回写到了【别的】作品的集。nagare 不撤销
	// （用户 2026-10-07 定），界面据此给出去那部作品改进度的入口。
	SyncedElsewhere []store.SyncedRecord `json:"syncedElsewhere,omitempty"`
}

func toViewAssociation(a store.Association) *ViewAssociation {
	return &ViewAssociation{Mode: a.Mode, AnilistID: a.AnilistID, Title: a.Title, SetAt: a.SetAt, SyncedElsewhere: a.SyncedElsewhere}
}

// assocCoverURL 拼出关联作品的封面地址；没有前缀或没有封面时返回空串。
// 与 artURL 同理：给客户端的是 clusterKey，真实图床地址留在服务端。
// 封面响应按 immutable 缓存，而同一个 clusterKey 可以改认成别的作品 —— 地址带上作品 ID 与
// 设定时间，改了关联就是新地址（处理器只看路径，不看查询参数）。
func assocCoverURL(prefix, clusterKey string, a store.Association) string {
	if prefix == "" || a.Mode != store.AssociationManual || a.CoverURL == "" {
		return ""
	}
	return prefix + "/" + assocCoverPrefix + url.PathEscape(clusterKey) +
		"?v=" + strconv.Itoa(a.AnilistID) + "-" + strconv.FormatInt(a.SetAt, 10)
}

// associatedCover 是认定过作品之后该显示的封面：认定的那部作品的封面优先，
// 它没有封面时才用自动匹配来的（ApplyAssociation 已经把指向别处的匹配作废了）；
// 标为「不是目录里的作品」就不显示任何作品封面。
func associatedCover(prefix, clusterKey string, a store.Association, matched string) string {
	if a.Mode != store.AssociationManual {
		return ""
	}
	if cover := assocCoverURL(prefix, clusterKey, a); cover != "" {
		return cover
	}
	return matched
}

type associationResult struct {
	Association *ViewAssociation `json:"association"`
}

// ClusterFiles 返回作品分组当前的全部 fileId（去重）；同一个 clusterKey 出现在多个库目录里
// （同一部番分在两块盘上）时合并。第二个返回值为 false 表示这次扫描里没有这个分组。
func (s *LibraryService) ClusterFiles(clusterKey string) ([]string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var ids []string
	seen := map[string]bool{}
	found := false
	for _, ce := range s.clusters {
		if ce.cluster.ClusterKey != clusterKey {
			continue
		}
		found = true
		ids = appendNew(ids, ce.cluster.Items, seen)
	}
	return ids, found
}

// withRescanLock 让 fn 与重扫串行。
func (s *LibraryService) withRescanLock(fn func()) {
	s.rescanMu.Lock()
	defer s.rescanMu.Unlock()
	fn()
}

// appendNew 追加还没见过的 fileId。fileId 是「文件名|大小|修改时间」，与路径无关 ——
// 两个库目录里一模一样的副本是同一个 id。
func appendNew(ids []string, items []library.Item, seen map[string]bool) []string {
	for _, it := range items {
		if !seen[it.FileID] {
			seen[it.FileID] = true
			ids = append(ids, it.FileID)
		}
	}
	return ids
}

// syncAssociations 让已存的关联跟上这次扫描（见 reconcileAssociations）。
// 失败只记日志：关联没跟上的代价是这一轮显示成「没认过」，不该拖垮整次扫描。
func (s *LibraryService) syncAssociations(clusters []clusterEntry) {
	current := map[string][]string{}
	seen := map[string]map[string]bool{}
	for _, ce := range clusters {
		key := ce.cluster.ClusterKey
		if seen[key] == nil {
			seen[key] = map[string]bool{}
		}
		current[key] = appendNew(current[key], ce.cluster.Items, seen[key])
	}
	err := s.st.UpdateAssociations(func(all map[string]store.Association) bool {
		return reconcileAssociations(all, current)
	})
	if err != nil {
		log.Printf("library: 更新作品关联失败：%v", err)
	}
}

// reconcileAssociations 让关联跟上这次扫描出的作品分组（current：clusterKey → 全部 fileId，已去重）：
//   - 键还在：成员刷新成分组当前的文件；
//   - 键不见了（解析语料更新、目录改名都会让 clusterKey 变）：在还没有关联的分组里找一个
//     与旧成员【两边都过半】重合的分组，把关联迁过去。只看旧成员这一边不够 ——
//     第一季、第二季被合进同一个分组时，第一季的关联会盖住第二季的集。
//     多个配对同时成立时，重合多的先配；
//   - 都找不到就原样留着 —— 多半是外接盘没插、这个目录这次扫描失败，插上就对上了。
//
// 就地修改 all，返回是否有改动。
func reconcileAssociations(all map[string]store.Association, current map[string][]string) bool {
	changed := false
	taken := map[string]bool{}
	var orphans []string
	for key, a := range all {
		files, ok := current[key]
		if !ok {
			orphans = append(orphans, key)
			continue
		}
		taken[key] = true
		if members := capMembers(files); !slices.Equal(members, a.Members) || a.MemberCount != len(files) {
			a.Members, a.MemberCount = members, len(files)
			all[key] = a
			changed = true
		}
	}

	moved := map[string]bool{}
	for _, m := range migrationCandidates(all, orphans, current, taken) {
		if moved[m.orphan] || taken[m.target] {
			continue
		}
		a := all[m.orphan]
		a.Members, a.MemberCount = capMembers(current[m.target]), len(current[m.target])
		all[m.target] = a
		delete(all, m.orphan)
		moved[m.orphan], taken[m.target] = true, true
		changed = true
	}
	return changed
}

type migration struct {
	orphan, target string
	overlap        int
}

// migrationCandidates 列出所有「两边都过半」的（旧键 → 新分组）配对，重合多的在前，
// 重合一样时按键排序（结果可复现）。
//
// 旧成员只记了前 64 个，新分组那一边分两步判断：抽样的 64 个里过半，且新分组的集数
// 不超过旧分组总集数的两倍 —— 否则上百集的长篇遇上两季合簇，抽样看着全对，实际一半是别的季。
func migrationCandidates(all map[string]store.Association, orphans []string, current map[string][]string, taken map[string]bool) []migration {
	var out []migration
	for _, old := range orphans {
		a := all[old]
		if len(a.Members) == 0 {
			continue
		}
		members := make(map[string]bool, len(a.Members))
		for _, id := range a.Members {
			members[id] = true
		}
		total := max(a.MemberCount, len(a.Members))
		for key, files := range current {
			if taken[key] || len(files) >= total*2 {
				continue
			}
			n := 0
			for _, id := range files {
				if members[id] {
					n++
				}
			}
			if n*2 > len(members) && n*2 > min(len(files), store.MaxAssociationMembers) {
				out = append(out, migration{orphan: old, target: key, overlap: n})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].overlap != out[j].overlap {
			return out[i].overlap > out[j].overlap
		}
		if out[i].orphan != out[j].orphan {
			return out[i].orphan < out[j].orphan
		}
		return out[i].target < out[j].target
	})
	return out
}

func capMembers(files []string) []string {
	if len(files) > store.MaxAssociationMembers {
		files = files[:store.MaxAssociationMembers]
	}
	return append([]string(nil), files...)
}

// putAssociation 设定 / 清除一个作品分组的关联。
//
//	{"clusterKey": "...", "mode": "manual", "anilistId": 154587}  认定为某部目录作品
//	{"clusterKey": "...", "mode": "none"}                          不是目录里的作品
//	{"clusterKey": "...", "mode": "auto"}                          回到自动匹配
func (h *Handler) putAssociation(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ClusterKey string `json:"clusterKey"`
		Mode       string `json:"mode"`
		AnilistID  int    `json:"anilistId"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	if req.ClusterKey == "" || len(req.ClusterKey) > maxClusterKeyBytes {
		writeErr(w, errs.New(errs.CategoryInput, "library.association", "缺少作品分组", ""))
		return
	}
	switch req.Mode {
	case store.AssociationManual:
		if req.AnilistID < 1 || req.AnilistID > math.MaxInt32 {
			writeErr(w, errs.New(errs.CategoryInput, "library.association", "无效的作品 ID", ""))
			return
		}
	case store.AssociationNone, "auto":
	default:
		writeErr(w, errs.New(errs.CategoryInput, "library.association", "未知的关联方式", "可选 manual / none / auto"))
		return
	}

	// 先看一眼分组还在不在：不为一个已经消失的分组去请求上游
	if _, ok := h.deps.Lib.ClusterFiles(req.ClusterKey); !ok && req.Mode != "auto" {
		writeVanished(w)
		return
	}
	var a *store.Association
	switch req.Mode {
	case store.AssociationManual:
		if !h.requireCatalog(w) {
			return
		}
		var ok bool
		if a, ok = h.manualAssociation(w, r, req.AnilistID); !ok {
			return
		}
	case store.AssociationNone:
		a = &store.Association{Mode: store.AssociationNone}
	}
	// 从读分组成员到落盘与重扫串行：中间插进一次重扫，关联就会写在一个刚被换掉的键上。
	// 锁在请求上游之后才拿，网络慢不会卡住重扫。
	h.deps.Lib.withRescanLock(func() { h.commitAssociation(w, req.ClusterKey, a) })
}

// commitAssociation 在重扫锁内落盘关联（a 为 nil 表示回到自动匹配）。
func (h *Handler) commitAssociation(w http.ResponseWriter, clusterKey string, a *store.Association) {
	files, inLibrary := h.deps.Lib.ClusterFiles(clusterKey)
	_, stored := h.deps.Store.Association(clusterKey)
	switch {
	case !inLibrary && a == nil && stored:
		// 分组不在了（盘没插、目录删了）也要能把它的关联删掉，否则这条记录永远清不掉
		h.applyAssociation(w, clusterKey, nil, nil)
		return
	case !inLibrary:
		writeVanished(w)
		return
	case a == nil && !stored:
		// 从没认过的分组本来就在自动匹配：什么都不动，不为一次重复点击清掉它的匹配缓存
		httpserver.WriteJSON(w, http.StatusOK, associationResult{})
		return
	case h.playingAny(files):
		// 播放中的那一集在结束时会按开播时的匹配回写进度；这时改关联，那次回写会绕过这里的记录
		httpserver.WriteError(w, http.StatusConflict, "正在播放这部作品，停止播放后再改")
		return
	}
	if a != nil {
		a.Members, a.MemberCount = capMembers(files), len(files)
		a.SetAt = time.Now().UnixMilli()
	}
	h.applyAssociation(w, clusterKey, a, files)
}

func writeVanished(w http.ResponseWriter) {
	httpserver.WriteError(w, http.StatusConflict, "媒体库里已经没有这个作品分组了（可能刚重新扫描过），刷新页面后再试")
}

// manualAssociation 从目录读出作品的标题与封面。不收客户端给的值：封面地址要经 /art 转发，
// 收了就等于让页面指定 nagare 去请求什么地址。失败时已写好响应。
func (h *Handler) manualAssociation(w http.ResponseWriter, r *http.Request, anilistID int) (*store.Association, bool) {
	raw, err := h.catalog.detailRaw(r.Context(), anilistID)
	if err != nil {
		writeErr(w, err)
		return nil, false
	}
	cover := raw.CoverImageURL
	if len(cover) > maxAssocCoverBytes || !h.remoteArt.Allowed(cover) {
		cover = ""
	}
	return &store.Association{
		Mode:      store.AssociationManual,
		AnilistID: anilistID,
		Title:     truncateRunes(catalogTitle(raw), maxAssocTitleRunes),
		CoverURL:  cover,
	}, true
}

func (h *Handler) applyAssociation(w http.ResponseWriter, clusterKey string, a *store.Association, files []string) {
	stored, err := h.deps.Store.ApplyAssociation(clusterKey, a, files)
	if err != nil {
		writeErr(w, errs.Wrap(errs.CategoryStorage, "library.association", "保存作品关联失败", "", err))
		return
	}
	res := associationResult{}
	if a != nil {
		res.Association = toViewAssociation(stored)
	}
	httpserver.WriteJSON(w, http.StatusOK, res)
}

// playingAny：播放器此刻是否在播分组里的某个文件。
func (h *Handler) playingAny(files []string) bool {
	if h.deps.Player == nil {
		return false
	}
	st := h.deps.Player.Status()
	return st.Playing && slices.Contains(files, st.FileID)
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
