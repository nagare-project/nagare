package player

import "context"

// 同一个 mpv 窗口里的上一集 / 下一集：播放层只认这个接缝，邻居从哪来（媒体库同一分组、
// 同一个种子里的另一个视频文件）由装配层决定。Options.Navigator 为 nil 时窗口里没有切集，
// 一切照旧：播完关窗、不设 keep-open、不推导航状态。

// Direction 是切集的方向。
type Direction int

const (
	// DirectionPrev 是上一集。
	DirectionPrev Direction = -1
	// DirectionNext 是下一集。
	DirectionNext Direction = 1
)

// Neighbor 是一个可以切过去的邻居集。
type Neighbor struct {
	// Episode 是它的集号（可以有空缺：第 4 集后面直接是第 6 集）。
	Episode int
	// Label 是界面上的说法，比如「第 5 集」。
	Label string
}

// Neighbors 是当前这一集的前后邻居；没有的一侧为 nil。
type Neighbors struct {
	Prev, Next *Neighbor
}

// Opened 是切过去之前准备好的下一集：可以交给 mpv 的媒体源，以及配对的外挂对白字幕（可空）。
type Opened struct {
	Source  MediaSource
	SubPath string
}

// Navigator 为一个正在播的媒体源找邻居集并把它准备好。
type Navigator interface {
	// Neighbors 报告 src 前后有没有可切的集。要快：每一集开播时都会调。
	Neighbors(src MediaSource) Neighbors
	// Open 把 dir 方向的邻居准备成可播放的源（磁力要先把那个文件缓冲到能起播）。
	// ctx 取消时要尽快返回。邻居已经不在了（重新扫描、种子没了）返回错误。
	Open(ctx context.Context, src MediaSource, dir Direction) (Opened, error)
}
