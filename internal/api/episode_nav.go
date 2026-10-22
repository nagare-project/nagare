// 同一个 mpv 窗口里的上一集 / 下一集（装配层）：播放层只认 player.Navigator，邻居从哪来在这里定。
//
// 本地文件：媒体库里同一个作品分组（clusterKey，跨库目录合并）里的正片，按 episodenav.Pick 挑。
// 作品关联不影响导航 —— 关联管的是「这组文件是目录里哪部作品」，不改变组里有哪些集。
// 磁力：同一个种子里的另一个视频文件，下一步接上（现在没有邻居）。在线候选：没有。
package api

import (
	"context"
	"fmt"
	"os"

	"github.com/nagare-project/nagare/internal/episodenav"
	errs "github.com/nagare-project/nagare/internal/errors"
	"github.com/nagare-project/nagare/internal/library"
	"github.com/nagare-project/nagare/internal/player"
	"github.com/nagare-project/nagare/internal/torrentstream"
)

// EpisodeNeighbor 返回 fileID 所在作品分组里 dir 方向（-1 上一集、1 下一集）的邻居集及其配对的
// 外挂对白字幕（没有为空串）。不在媒体库里、不是认出集号的正片、没有邻居时 ok 为 false。
func (s *LibraryService) EpisodeNeighbor(fileID string, dir int) (library.Item, string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	cur, cands, ok := s.navCandidatesLocked(fileID)
	if !ok {
		return library.Item{}, "", false
	}
	i, ok := episodenav.Pick(cur, cands, dir)
	if !ok {
		return library.Item{}, "", false
	}
	return cands[i], s.subs[cands[i].FileID].AbsPath, true
}

// EpisodeNeighbors 返回 fileID 前后的邻居集（没有的一侧为 nil）。每一集开播时调，只读内存。
func (s *LibraryService) EpisodeNeighbors(fileID string) (prev, next *library.Item) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	cur, cands, ok := s.navCandidatesLocked(fileID)
	if !ok {
		return nil, nil
	}
	pick := func(dir int) *library.Item {
		if i, ok := episodenav.Pick(cur, cands, dir); ok {
			it := cands[i]
			return &it
		}
		return nil
	}
	return pick(-1), pick(1)
}

// navCandidatesLocked 取 fileID 的条目与它所在作品分组的全部文件（同一个 clusterKey 出现在多个
// 库目录里时合并）。调用方持 s.mu 读锁。
//
// 同一个 fileId 在两个库目录里各有一份副本时两份都留着（路径不同）：Pick 按 FileID 跳过当前这一集
// 的所有副本，邻居的副本之间按「与当前同目录」挑 —— 合并成一份反而会丢掉这个信息。
// 只去掉完全相同的条目（同一个文件在同一次扫描里出现两次）。
func (s *LibraryService) navCandidatesLocked(fileID string) (library.Item, []library.Item, bool) {
	cur, ok := s.items[fileID]
	if !ok {
		return library.Item{}, nil, false
	}
	key, ok := s.fileCluster[fileID]
	if !ok {
		return library.Item{}, nil, false
	}
	type copyKey struct{ id, abs string }
	seen := map[copyKey]bool{}
	var cands []library.Item
	for _, ce := range s.clusters {
		if ce.cluster.ClusterKey != key {
			continue
		}
		for _, it := range ce.cluster.Items {
			k := copyKey{it.FileID, it.AbsPath}
			if !seen[k] {
				seen[k] = true
				cands = append(cands, it)
			}
		}
	}
	return cur, cands, true
}

// EpisodeNavigator 是 player.Navigator 的装配实现。
type EpisodeNavigator struct {
	lib *LibraryService
}

// NewEpisodeNavigator 用媒体库找本地文件的邻居集。
func NewEpisodeNavigator(lib *LibraryService) *EpisodeNavigator {
	return &EpisodeNavigator{lib: lib}
}

// Neighbors 报告 src 前后有没有可切的集。
func (n *EpisodeNavigator) Neighbors(src player.MediaSource) player.Neighbors {
	if _, torrent := src.(*torrentstream.Source); torrent {
		// 同一个种子里的另一个视频文件：下一步接上，现在不出按钮
		return player.Neighbors{}
	}
	item, ok := player.LocalItem(src)
	if !ok {
		return player.Neighbors{}
	}
	prev, next := n.lib.EpisodeNeighbors(item.FileID)
	return player.Neighbors{Prev: navNeighbor(prev), Next: navNeighbor(next)}
}

// Open 把 dir 方向的邻居准备成可播放的源：重新在媒体库里找一次（播放期间可能重扫过），
// 再确认文件还在原位置。网络盘上 os.Stat 可能卡住，所以 ctx 取消时不等它。
func (n *EpisodeNavigator) Open(ctx context.Context, src player.MediaSource, dir player.Direction) (player.Opened, error) {
	if _, torrent := src.(*torrentstream.Source); torrent {
		return player.Opened{}, errs.New(errs.CategoryPlayback, "player.nav",
			"磁力里的"+dirLabel(dir)+"还不能在播放器里直接切", "回到网页上选这一集播放")
	}
	item, ok := player.LocalItem(src)
	if !ok {
		return player.Opened{}, errs.New(errs.CategoryInput, "player.nav",
			"这个来源不能切"+dirLabel(dir), "回到网页上选这一集播放")
	}
	target, subPath, ok := n.lib.EpisodeNeighbor(item.FileID, int(dir))
	if !ok {
		return player.Opened{}, errs.New(errs.CategoryFS, "player.nav",
			"没有"+dirLabel(dir)+"了", "媒体库里已经找不到它（可能刚重新扫描过）")
	}
	if err := statWithin(ctx, target.AbsPath); err != nil {
		if ctx.Err() != nil {
			return player.Opened{}, errs.Wrap(errs.CategoryPlayback, "player.nav", "切集已取消", "", err)
		}
		return player.Opened{}, errs.Wrap(errs.CategoryFS, "player.nav",
			dirLabel(dir)+"的文件不见了："+target.FileName, "重新扫描媒体库后再试", err)
	}
	return player.Opened{Source: player.NewLocalSource(target), SubPath: subPath}, nil
}

// statWithin 确认文件存在；ctx 先结束就不等了（卡住的 stat 留在后台自己结束）。
func statWithin(ctx context.Context, path string) error {
	done := make(chan error, 1)
	go func() {
		_, err := os.Stat(path)
		done <- err
	}()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// navNeighbor 把库条目翻成界面上的邻居（「第 5 集」）。
func navNeighbor(it *library.Item) *player.Neighbor {
	if it == nil || it.Episode == nil {
		return nil
	}
	return &player.Neighbor{Episode: *it.Episode, Label: fmt.Sprintf("第 %d 集", *it.Episode)}
}

func dirLabel(dir player.Direction) string {
	if dir == player.DirectionPrev {
		return "上一集"
	}
	return "下一集"
}
