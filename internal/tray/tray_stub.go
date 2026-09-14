//go:build !((darwin && cgo) || windows || linux)

package tray

import "context"

// Run 是无托盘平台（CGO_ENABLED=0 的 macOS 交叉编译、BSD）的桩：
// 只阻塞到 ctx 取消。退出入口是信号或界面里的「退出」按钮（POST /api/shutdown）。
func Run(ctx context.Context, _ Options) {
	<-ctx.Done()
}

// Available 在桩实现里恒为假。
func Available() bool { return false }

// CurrentMode 见 Mode。
func CurrentMode() Mode { return ModeNone }
