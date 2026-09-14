//go:build (darwin && cgo) || windows || linux

package tray

import (
	"context"
	"log"
	"time"

	"fyne.io/systray"
)

// Run 在菜单栏 / 托盘放一个图标并阻塞，直到 ctx 取消或用户点「退出 nagare」。
// 用户点退出时先调 OnQuit 再返回；ctx 取消（信号 / API 退出）时直接收起图标返回。
//
// macOS .app 里还有第三条退出路径：⌘Q / Dock 退出 / 注销 / 关机。系统那时在等我们
// 答复，所以那条路不走「收起图标让 Run 返回」，而是等收尾完成后答复系统「可以终止」，
// 由系统结束进程（见 dispatch 的 terminateCh 分支与 dock_darwin.m）。
func Run(ctx context.Context, opts Options) {
	if !Available() {
		// Linux 没有会话总线时 systray 会在退出路径上对空连接解引用；
		// 与其赌，不如明确退回无托盘，行为与 --no-tray 一致。
		log.Print("tray: 当前环境没有托盘可用，退出请用界面里的按钮或信号")
		<-ctx.Done()
		return
	}
	onReady := func() {
		systray.SetTooltip("nagare v" + opts.Version)
		applyIcon()
		openItem := systray.AddMenuItem("打开界面", "在浏览器中打开 nagare")
		systray.AddSeparator()
		quitItem := systray.AddMenuItem("退出 nagare", "停止播放、回写进度并退出")
		terminateCh, replyTerminate := platformReady(opts)
		go dispatch(ctx, opts, openItem.ClickedCh, quitItem.ClickedCh, terminateCh, systray.Quit, replyTerminate)
		if opts.Notice != nil {
			// 通知不是关键路径：弹不出来只记日志，不影响托盘本身。
			if err := showNotice(*opts.Notice); err != nil {
				log.Printf("tray: 首次启动提示弹不出来：%v", err)
			}
		}
	}
	systray.Run(onReady, nil)
}

// dispatch 分发菜单事件。三条退出路径：
//   - quitCh（托盘菜单「退出 nagare」）：OnQuit → quit（收起图标，Run 返回，主进程收尾后退出）
//   - terminateCh（macOS ⌘Q / Dock / 注销 / 关机）：OnQuit → 等 Done → replyTerminate（系统结束进程）
//   - ctx 取消（信号 / API 退出）：主进程已在收尾，只收图标
//
// quit / replyTerminate 作为参数注入以便单测（真机实现是 systray.Quit 与 Cocoa 的 reply）。
func dispatch(ctx context.Context, opts Options, openCh, quitCh, terminateCh <-chan struct{},
	quit, replyTerminate func()) {
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
		case <-terminateCh:
			if opts.OnQuit != nil {
				opts.OnQuit()
			}
			waitTeardown(opts.Done)
			replyTerminate()
			return
		case <-ctx.Done():
			quit()
			return
		}
	}
}

// waitTeardown 等主进程收尾完成，最多 terminateGrace；超时也放行并记日志。
func waitTeardown(done <-chan struct{}) {
	if done == nil {
		return
	}
	select {
	case <-done:
	case <-time.After(terminateGrace):
		log.Printf("tray: 收尾超过 %s 仍未完成，按系统要求先退出", terminateGrace)
	}
}
