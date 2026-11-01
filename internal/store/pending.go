package store

import (
	"cmp"
	"slices"
)

// PendingSync 是一集看完了、还没记到账号的进度。
type PendingSync struct {
	FileID   string
	Progress Progress
}

// PendingSyncs 返回「看完了、还没记到账号」的全部进度，最早更新的在前（同一时刻按 fileID，顺序稳定）。
// since > 0 时只要 UpdatedAt 不早于它（毫秒时间戳）的。
//
// 这里只看进度本身；匹配、作品关联、被挡下的回写由调用方（internal/player 的补发）判断。
func (s *Store) PendingSyncs(since int64) []PendingSync {
	s.mu.Lock()
	var out []PendingSync
	for id, p := range s.data.Progress {
		if !p.Completed || p.Synced || (since > 0 && p.UpdatedAt < since) {
			continue
		}
		out = append(out, PendingSync{FileID: id, Progress: p})
	}
	s.mu.Unlock()
	slices.SortFunc(out, func(a, b PendingSync) int {
		if c := cmp.Compare(a.Progress.UpdatedAt, b.Progress.UpdatedAt); c != 0 {
			return c
		}
		return cmp.Compare(a.FileID, b.FileID)
	})
	return out
}
