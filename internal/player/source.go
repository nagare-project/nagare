// MediaSource 接缝：播放管线（懒哈希 → 匹配 → 弹幕 → 进度回写 → 看完同步）
// 原先只在三处直接引用本地路径，把这三处抽象掉之后，磁力边下边播就能复用
// 整条 M1 管线，而不必写第二条形状相同、行为会漂移的链路。
package player

import (
	"context"
	"os"

	errs "github.com/nagare-project/nagare/internal/errors"
	"github.com/nagare-project/nagare/internal/library"
)

// MediaSource 把「一个可播放的东西」抽象出来，让播放管线不关心它是
// 本地文件还是磁力流。
type MediaSource interface {
	// Item 返回媒体的元数据（FileID / FileName / Size / 集号 / 解析出的标题…）。
	// 磁力实现用同一条解析链从种子内的文件名派生，因此下游（进度、匹配、
	// 弹幕、看完同步）完全不必区分来源。
	Item() library.Item
	// Probe 在启动 mpv 前确认媒体可用（本地：os.Stat；磁力：种子已就绪）。
	Probe(ctx context.Context) error
	// MPVPath 返回交给 mpv 的路径或 URL。
	MPVPath() string
	// Hash16M 返回前 16MB 的 MD5，用于 dandanplay 匹配。
	Hash16M(ctx context.Context) (string, error)
}

// localFileSource 是本地文件实现。它不持有任何可变状态，构造后只读。
type localFileSource struct{ item library.Item }

// NewLocalSource 把一个库条目包成 MediaSource；行为与 M1 的直连路径逐字一致。
func NewLocalSource(item library.Item) MediaSource { return localFileSource{item: item} }

func (s localFileSource) Item() library.Item { return s.item }

func (s localFileSource) MPVPath() string { return s.item.AbsPath }

// Probe 确认文件仍在原位置。「文件被移走/改名」是本地库最常见的失效方式，
// 用户能做的恢复动作是重新扫描，因此归 CategoryFS（API 层据此映射 404）。
func (s localFileSource) Probe(context.Context) error {
	if _, err := os.Stat(s.item.AbsPath); err != nil {
		return errs.Wrap(errs.CategoryFS, "player.play",
			"文件不存在或已被移动："+s.item.FileName, "重新扫描媒体库后再试", err)
	}
	return nil
}

// Hash16M 忽略 ctx：本地读盘是同步的，顺序读 16MB 中间没有可取消的等待点。
// ctx 是为磁力实现留的 —— 那边要约束「等头部 16MB 从 swarm 到齐」这段
// 长度不确定的等待。
func (s localFileSource) Hash16M(context.Context) (string, error) {
	return library.Hash16M(s.item.AbsPath)
}
