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
		systray.SetTooltip("nagare v" + opts.Version + " · " + opts.Address)
		applyIcon()
		items := buildMenu(opts)
		terminateCh, replyTerminate := platformReady(opts)
		go dispatch(ctx, opts, items, terminateCh, systray.Quit, replyTerminate)
		if opts.Notice != nil {
			// 通知不是关键路径：弹不出来只记日志，不影响托盘本身。
			if err := showNotice(*opts.Notice); err != nil {
				log.Printf("tray: 首次启动提示弹不出来：%v", err)
			}
		}
	}
	systray.Run(onReady, nil)
}

// menuChans 是菜单里会产生事件的那几项；不产生事件的（版本、地址）只是显示。
type menuChans struct {
	open, copyAddr, about, quit <-chan struct{}
}

// buildMenu 按固定顺序搭托盘菜单（三平台同一份）：
//
//	nagare v0.3.0            ← 禁用行，只看
//	http://127.0.0.1:8591/   ← 禁用行，只看
//	──────────
//	打开界面
//	复制地址
//	──────────
//	关于 nagare
//	──────────
//	退出 nagare
func buildMenu(opts Options) menuChans {
	systray.AddMenuItem("nagare v"+opts.Version, "当前版本").Disable()
	systray.AddMenuItem(opts.Address, "界面地址（不含 token）").Disable()
	systray.AddSeparator()
	open := systray.AddMenuItem("打开界面", "在浏览器中打开 nagare")
	copyAddr := systray.AddMenuItem("复制地址", "把界面地址复制到剪贴板（不含 token；新浏览器首次请用「打开界面」）")
	systray.AddSeparator()
	about := systray.AddMenuItem("关于 nagare", "版本、数据目录、日志位置")
	systray.AddSeparator()
	quit := systray.AddMenuItem("退出 nagare", "停止播放、回写进度并退出")
	return menuChans{open: open.ClickedCh, copyAddr: copyAddr.ClickedCh, about: about.ClickedCh, quit: quit.ClickedCh}
}

// dispatch 分发菜单事件。三条退出路径：
//   - quit（托盘菜单「退出 nagare」）：OnQuit → quit（收起图标，Run 返回，主进程收尾后退出）
//   - terminateCh（macOS ⌘Q / Dock / 注销 / 关机）：OnQuit → 等 Done → replyTerminate（系统结束进程）
//   - ctx 取消（信号 / API 退出）：主进程已在收尾，只收图标
//
// quit / replyTerminate 作为参数注入以便单测（真机实现是 systray.Quit 与 Cocoa 的 reply）。
func dispatch(ctx context.Context, opts Options, m menuChans, terminateCh <-chan struct{},
	quit, replyTerminate func()) {
	for {
		select {
		case <-m.open:
			if opts.OnOpen != nil {
				opts.OnOpen()
			}
		case <-m.copyAddr:
			if err := copyToClipboard(opts.Address); err != nil {
				log.Printf("tray: 复制地址失败：%v", err)
			}
		case <-m.about:
			if opts.OnAbout != nil {
				opts.OnAbout()
			}
		case <-m.quit:
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
