// 磁力来源在同一个 mpv 窗口里的上一集 / 下一集：同一个种子里的另一个视频文件
// （torrentstream.Source.Siblings / OpenSibling，挑法与本地文件同为 episodenav.Pick）；单集种子没有邻居。
// 其余来源（本地文件、在线候选）原样交给内层导航（桌面版里是 api.EpisodeNavigator）。

package sources

import (
	"context"

	"github.com/nagare-project/nagare/internal/api"
	"github.com/nagare-project/nagare/internal/player"
	"github.com/nagare-project/nagare/internal/torrentstream"
)

// TorrentNavigator 是套在本地导航外面的 player.Navigator：先认磁力来源，其余交给 next。
type TorrentNavigator struct {
	next player.Navigator
}

// NewTorrentNavigator 包一层磁力导航。next 不能为 nil（桌面版传 api.NewEpisodeNavigator(lib)）。
func NewTorrentNavigator(next player.Navigator) *TorrentNavigator {
	return &TorrentNavigator{next: next}
}

// Neighbors 报告 src 前后有没有可切的集。
func (n *TorrentNavigator) Neighbors(src player.MediaSource) player.Neighbors {
	if t, ok := src.(*torrentstream.Source); ok {
		prev, next := t.Siblings()
		return player.Neighbors{Prev: choiceNeighbor(prev), Next: choiceNeighbor(next)}
	}
	return n.next.Neighbors(src)
}

// Open 把 dir 方向的邻居准备成可播放的源。
func (n *TorrentNavigator) Open(ctx context.Context, src player.MediaSource, dir player.Direction) (player.Opened, error) {
	if t, ok := src.(*torrentstream.Source); ok {
		// 缓冲到能起播才返回（慢了窗口上出「正在准备」卡片）；失败的错误已分类，原样交给屏幕提示 / 错误卡片
		next, err := t.OpenSibling(ctx, int(dir))
		if err != nil {
			return player.Opened{}, err
		}
		// 窗口最终没换上这一集（取消了、mpv 没切过去）时，种子换回原来那一集
		return player.Opened{Source: next, Discard: next.Discard}, nil
	}
	return n.next.Open(ctx, src, dir)
}

// choiceNeighbor 把种子里的候选文件翻成界面上的邻居（Siblings 只挑认出集号的正片，Episode 不会为空）。
func choiceNeighbor(c *torrentstream.FileChoice) *player.Neighbor {
	if c == nil || c.Episode == nil {
		return nil
	}
	return api.NeighborForEpisode(*c.Episode)
}
