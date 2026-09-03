// Source：磁力流的 player.MediaSource 实现。
//
// 本文件【不】import internal/player —— player 在上游，反向 import 会成环。
// 结构性满足那四个方法即可，调用方那边有编译期断言兜底。
package torrentstream

import (
	"context"
	"strconv"
	"strings"

	errs "github.com/nagare-project/nagare/internal/errors"
	"github.com/nagare-project/nagare/internal/library"
)

// Source 是一次磁力播放的媒体来源。构造后只读。
type Source struct {
	sess     *session
	item     library.Item
	infohash string
	index    int
}

// newSource 由会话在缓冲完成后构造。
func (s *session) newSource() *Source {
	s.mu.Lock()
	defer s.mu.Unlock()
	return &Source{sess: s, item: s.item, infohash: s.infohash, index: s.index}
}

// Item 返回走同一条解析链派生的库条目：下游（弹幕匹配、进度回写、看完同步）
// 因此完全不必区分媒体来自本地文件还是磁力。
func (s *Source) Item() library.Item { return s.item }

// MPVPath 返回交给 mpv 的流地址。每次现算：能力段每进程轮换，
// 缓存下来的 URL 会在轮换后失效。
func (s *Source) MPVPath() string {
	base := strings.TrimSuffix(s.sess.engine.streamBase(), "/")
	return base + "/t/" + s.infohash + "/" + strconv.Itoa(s.index)
}

// Probe 确认这条流仍然可服务。会话被停掉（用户取消、切了另一条磁力）之后
// 再启动 mpv 只会得到 404，不如在这里就说清楚。
func (s *Source) Probe(context.Context) error {
	if s.sess.target(s.infohash, s.index) == nil {
		return errs.New(errs.CategoryTorrent, "torrentstream.probe",
			"这条磁力的播放会话已经结束", "重新点一次播放")
	}
	return nil
}

// Hash16M 读前 16MB 算 dandanplay 匹配用的 MD5。
//
// 复用 library.Hash16MFrom：取样长度与算法只能有一份，否则同一个文件在本地库
// 和磁力两条路上会算出两个哈希。启动期本来就钉住了头部这一段，正常几秒内到齐；
// 迟迟不到时返回错误，由播放管线降级成「弹幕未匹配」，播放本身不受影响。
func (s *Source) Hash16M(ctx context.Context) (string, error) {
	target := s.sess.target(s.infohash, s.index)
	if target == nil {
		return "", errs.New(errs.CategoryTorrent, "torrentstream.hash",
			"播放会话已结束，无法计算弹幕匹配哈希", "弹幕会退回按文件名匹配")
	}
	reader, err := target.open(ctx, 0)
	if err != nil {
		return "", errs.Wrap(errs.CategoryTorrent, "torrentstream.hash",
			"无法读取头部数据", "弹幕会退回按文件名匹配", err)
	}
	defer reader.Close()

	sum, err := library.Hash16MFrom(reader)
	if err != nil {
		return "", errs.Wrap(errs.CategoryTorrent, "torrentstream.hash",
			"头部数据迟迟没到，无法计算弹幕匹配哈希", "弹幕会退回按文件名匹配，播放不受影响", err)
	}
	return sum, nil
}
