package store

import (
	"errors"
	"slices"
)

// 本地作品分组 ↔ 目录作品的对应关系（用户手动认定）。
//
// 键是作品分组的 clusterKey（用户 2026-10-07 定：按作品分组认，不按文件夹）。
// clusterKey 由共享解析语料算出（CQ2），语料更新或目录改名都可能让它变；
// 所以每条关联同时记下当时的成员文件，媒体库重扫时按成员重合把关联迁到新键上
// （迁移策略在 internal/api，这里只管原子地存取）。
//
// 它与 Bindings 是两回事：Bindings 是「某个文件自动匹配到了什么」的缓存，
// 磁力 / 在线 / 本地都往里写；关联是用户的决定，优先级高于自动匹配。

// 关联的两种形态。没有关联 = 走自动匹配（不存记录）。
const (
	// AssociationManual：用户指定了这部作品对应哪个目录作品。
	AssociationManual = "manual"
	// AssociationNone：用户说这不是目录里的作品（自己录的视频、找不到条目的冷门番）
	// —— 不匹配、不拉弹幕、不回写进度。
	AssociationNone = "none"
)

// MaxAssociationMembers 是一条关联最多记几个成员文件。只用来在 clusterKey 变化后
// 找回关联，几十个足够判断重合；上千集的长篇不必全记。
const MaxAssociationMembers = 64

// Association 是一个作品分组的手动关联。
type Association struct {
	Mode      string `json:"mode"`
	AnilistID int    `json:"anilistId,omitempty"`
	Title     string `json:"title,omitempty"`
	// CoverURL 是目录作品的封面原始地址，只在服务端用（经 /art 能力 URL 转发），不发给客户端。
	CoverURL string `json:"coverUrl,omitempty"`
	// Members 是设定（或最近一次重扫）时分组里的文件 fileId，最多 MaxAssociationMembers 个；
	// MemberCount 是那时分组的文件总数（不受上限约束），迁键时用来判断新分组是不是大了一圈。
	Members     []string `json:"members,omitempty"`
	MemberCount int      `json:"memberCount,omitempty"`
	SetAt       int64    `json:"setAt"`
	// SyncedElsewhere 是改关联时发现的、已经看完并回写到了【别的】作品的集。
	// nagare 不去撤销（用户 2026-10-07 定），记在这里让界面一直能给出修正入口，
	// 而不是只在那一次响应里出现一下；改成认定为那部作品时自动去掉。
	SyncedElsewhere []SyncedRecord `json:"syncedElsewhere,omitempty"`
}

// SyncedRecord 是回写到某部作品上的几集。
type SyncedRecord struct {
	AnilistID int    `json:"anilistId"`
	Title     string `json:"title,omitempty"`
	Episodes  []int  `json:"episodes"`
}

func (a Association) clone() Association {
	a.Members = append([]string(nil), a.Members...)
	a.SyncedElsewhere = cloneSynced(a.SyncedElsewhere)
	return a
}

func cloneSynced(in []SyncedRecord) []SyncedRecord {
	if in == nil {
		return nil
	}
	out := make([]SyncedRecord, len(in))
	for i, r := range in {
		r.Episodes = append([]int(nil), r.Episodes...)
		out[i] = r
	}
	return out
}

// Association 读取一个作品分组的手动关联。
func (s *Store) Association(clusterKey string) (Association, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.data.Associations[clusterKey]
	if !ok {
		return Association{}, false
	}
	return a.clone(), true
}

// ApplyAssociation 原子地改一个作品分组的关联，并作废分组内与新关联不一致的自动匹配。
// 返回落盘后的关联（a 为 nil 时返回零值）。
//
// a 为 nil 表示删除关联、回到自动匹配。fileIDs 是分组【当前全部】文件（不受成员上限约束）。
// 作废规则：手动关联到 X 时只作废指向别处的匹配（已经匹配到 X 的弹幕与集号照用）；
// 标为 none 或回到自动匹配时全部作废。被作废的那几集清掉 Synced：
// 它回写到的是旧作品，重看这一集看完时应当按新关联再回写一次。
// 被作废且已经回写过的集并进 SyncedElsewhere。落盘失败时内存状态整体回滚。
func (s *Store) ApplyAssociation(clusterKey string, a *Association, fileIDs []string) (Association, error) {
	if clusterKey == "" {
		return Association{}, errors.New("clusterKey 为空")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	target := 0
	if a != nil && a.Mode == AssociationManual {
		target = a.AnilistID
	}
	prev, hadPrev := s.data.Associations[clusterKey]
	oldBindings := map[string]Binding{}
	oldProgress := map[string]Progress{}
	var synced []SyncedRecord
	for _, id := range fileIDs {
		b, ok := s.data.Bindings[id]
		if !ok || (target > 0 && b.AnilistID == target) {
			continue
		}
		oldBindings[id] = b
		delete(s.data.Bindings, id)
		p, ok := s.data.Progress[id]
		if !ok || !p.Synced {
			continue
		}
		if p.Completed && b.AnilistID > 0 && b.Episode > 0 {
			synced = mergeSynced(synced, b.AnilistID, b.Title, b.Episode)
		}
		oldProgress[id] = p
		p.Synced = false
		s.data.Progress[id] = p
	}

	var stored Association
	if a == nil {
		delete(s.data.Associations, clusterKey)
	} else {
		stored = a.clone()
		if len(stored.Members) > MaxAssociationMembers {
			stored.Members = stored.Members[:MaxAssociationMembers]
		}
		var carried []SyncedRecord
		if hadPrev {
			carried = cloneSynced(prev.SyncedElsewhere)
		}
		for _, r := range synced {
			for _, ep := range r.Episodes {
				carried = mergeSynced(carried, r.AnilistID, r.Title, ep)
			}
		}
		// 认定为那部作品了，之前回写到它上面的就不再是「写错了」
		stored.SyncedElsewhere = slices.DeleteFunc(carried, func(r SyncedRecord) bool { return r.AnilistID == target })
		if len(stored.SyncedElsewhere) == 0 {
			stored.SyncedElsewhere = nil
		}
		s.data.Associations[clusterKey] = stored
	}

	if err := s.save(); err != nil {
		for id, b := range oldBindings {
			s.data.Bindings[id] = b
		}
		for id, p := range oldProgress {
			s.data.Progress[id] = p
		}
		if hadPrev {
			s.data.Associations[clusterKey] = prev
		} else {
			delete(s.data.Associations, clusterKey)
		}
		return Association{}, err
	}
	return stored.clone(), nil
}

// mergeSynced 把一集并进按作品归拢的记录：作品按首次出现排，集号升序去重。
func mergeSynced(records []SyncedRecord, anilistID int, title string, episode int) []SyncedRecord {
	i := slices.IndexFunc(records, func(r SyncedRecord) bool { return r.AnilistID == anilistID })
	if i < 0 {
		return append(records, SyncedRecord{AnilistID: anilistID, Title: title, Episodes: []int{episode}})
	}
	r := &records[i]
	if r.Title == "" {
		r.Title = title
	}
	if j, found := slices.BinarySearch(r.Episodes, episode); !found {
		r.Episodes = slices.Insert(r.Episodes, j, episode)
	}
	return records
}

// UpdateAssociations 在锁内读改写全部关联（重扫后迁键、刷新成员用）。
// mutate 拿到的是副本，返回 false 表示没有改动 —— 不落盘。
func (s *Store) UpdateAssociations(mutate func(all map[string]Association) bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	work := make(map[string]Association, len(s.data.Associations))
	for k, a := range s.data.Associations {
		work[k] = a.clone()
	}
	if !mutate(work) {
		return nil
	}
	prev := s.data.Associations
	s.data.Associations = work
	if err := s.save(); err != nil {
		s.data.Associations = prev
		return err
	}
	return nil
}
