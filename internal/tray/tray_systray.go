//go:build (darwin && cgo) || windows

package tray

import (
	"context"

	"fyne.io/systray"
)

// Run 在菜单栏 / 托盘放一个图标并阻塞，直到 ctx 取消或用户点「退出 nagare」。
// 用户点退出时先调 OnQuit 再返回；ctx 取消（信号 / API 退出）时直接收起图标返回。
func Run(ctx context.Context, opts Options) {
	onReady := func() {
		systray.SetTooltip("nagare v" + opts.Version)
		applyIcon()
		openItem := systray.AddMenuItem("打开界面", "在浏览器中打开 nagare")
		systray.AddSeparator()
		quitItem := systray.AddMenuItem("退出 nagare", "停止播放、回写进度并退出")
		go dispatch(ctx, opts, openItem.ClickedCh, quitItem.ClickedCh, systray.Quit)
	}
	systray.Run(onReady, nil)
}

// dispatch 分发菜单事件；退出与 ctx 取消都收敛到 quit（真机实现是 systray.Quit，
// 幂等），之后 Run 里的事件循环结束、Run 返回。quit 作为参数注入以便单测。
func dispatch(ctx context.Context, opts Options, openCh, quitCh <-chan struct{}, quit func()) {
	for {
		select {
		case <-openCh:
			if opts.OnOpen != nil {
				opts.OnOpen()
			}
		case <-quitCh:
			if opts.OnQuit != nil {
				opts.OnQuit()
			}
			quit()
			return
		case <-ctx.Done():
			quit()
			return
		}
	}
}
