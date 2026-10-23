//go:build !(darwin && cgo && libmpv) && !windows

package desktop

import (
	"context"
	"errors"

	"github.com/nagare-project/nagare/internal/mpv"
)

// 没有桌面窗口的构建（Linux、CGO_ENABLED=0 或没带 libmpv 标签的 macOS）：
// 一切照旧，浏览器 + 独立的 mpv 窗口。

// Available 报告这个构建里有没有桌面窗口。
func Available() bool { return false }

// Ready 报告这台机器上窗口能不能用（桩里永远不能）。
func Ready() error { return errors.New("这个构建没有桌面窗口") }

// InProcess 报告 mpv 是不是跑在 nagare 进程里（桩里没有窗口，无从谈起）。
func InProcess() bool { return false }

// Init 在桩里什么都不做。
func Init(Options) {}

// Show 在桩里什么都不做。
func Show() {}

// Open 在桩里什么都不做。
func Open(string) {}

// State 在桩里永远是「没有窗口」。
func State() Info { return Info{} }

// MainLoop 在桩里没有：托盘照旧自己跑消息循环。
func MainLoop(<-chan struct{}) func() { return nil }

// BeforeHandoff 在桩里什么都不做（见 Windows 版）。
func BeforeHandoff() {}

// Launch 在桩里不可用：调用方只在 Available 为真时才把它交给播放器。
func Launch(context.Context, mpv.LaunchOptions) (*mpv.Player, error) {
	return nil, errors.New("这个构建没有桌面窗口")
}
