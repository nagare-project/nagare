//go:build !(darwin && cgo && libmpv)

package desktop

import (
	"context"
	"errors"

	"github.com/nagare-project/nagare/internal/mpv"
)

// 没有桌面窗口的构建（Linux、Windows、CGO_ENABLED=0、没带 libmpv 标签的 macOS）：
// 一切照旧，浏览器 + 独立的 mpv 窗口。

// Available 报告这个构建里有没有桌面窗口。
func Available() bool { return false }

// Init 在桩里什么都不做。
func Init(Options) {}

// Show 在桩里什么都不做。
func Show() {}

// Open 在桩里什么都不做。
func Open(string) {}

// Launch 在桩里不可用：调用方只在 Available 为真时才把它交给播放器。
func Launch(context.Context, mpv.LaunchOptions) (*mpv.Player, error) {
	return nil, errors.New("这个构建没有桌面窗口")
}
