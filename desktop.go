package main

import (
	"log"

	"github.com/nagare-project/nagare/internal/api"
	"github.com/nagare-project/nagare/internal/desktop"
	"github.com/nagare-project/nagare/internal/httpserver"
	"github.com/nagare-project/nagare/internal/tray"
)

// 桌面窗口（internal/desktop）的接线：这次运行用不用它、播放交给谁、托盘与 Dock 的「打开」指向哪。
//
// 用的时候：首次启动、点 Dock、托盘的「打开 nagare」都开窗口而不是浏览器；托盘另有「在浏览器中打开」。
// 所有播放（窗口里的网页、外部浏览器里的网页都一样）都在窗口里放，并把窗口提到前面 ——
// 播放器的 Launch 换成 desktop.Launch，此外 internal/player 不知道窗口的存在。

// desktopEnabled 决定这次运行用不用桌面窗口：这个构建得带着它（macOS + cgo + libmpv 标签，或 Windows），还要有
// 托盘跑的事件循环（--no-tray 时没有）和内嵌界面（API-only 时窗口里没东西可看）。
// --no-window 退回以前的浏览器 + 独立 mpv 窗口。
func desktopEnabled(f flags, apiOnly bool) bool {
	on, _ := desktopDecision(f, apiOnly)
	return on
}

// desktopDecision 同 desktopEnabled，另外报告「本来要用、但这台机器上用不了」的原因（Windows 上没有 WebView2
// 运行时）：那时退回浏览器模式，原因写进日志与设置页。
func desktopDecision(f flags, apiOnly bool) (bool, string) {
	if !desktopUsable(desktop.Available(), f, apiOnly) {
		return false, ""
	}
	if err := desktop.Ready(); err != nil {
		log.Printf("桌面窗口用不了，退回浏览器模式：%v", err)
		return false, err.Error()
	}
	return true, ""
}

// desktopStatus 是设置页「播放窗口」一行的数据源：没用窗口时报启动时的退回原因，用着时报窗口此刻的状态
// （Windows 的网页层可能中途起不来、退回浏览器）。
func desktopStatus(window bool, fallback string) func() api.DesktopView {
	return func() api.DesktopView {
		if !window {
			return api.DesktopView{Fallback: fallback}
		}
		s := desktop.State()
		return api.DesktopView{Window: s.Active, Fallback: s.Fallback}
	}
}

// desktopUsable 是 desktopEnabled 的判断本身；available（这个构建带不带窗口）单独拿出来好测。
func desktopUsable(available bool, f flags, apiOnly bool) bool {
	return available && !f.noTray && !f.noWindow && !apiOnly
}

// withDesktop 把托盘的打开入口改指到窗口。showAtStart 为假（--no-browser）时启动后不弹窗口。
func withDesktop(opts tray.Options, url, about string, showAtStart bool) tray.Options {
	opts.Desktop = true
	opts.OnOpen = desktop.Show
	opts.OnOpenBrowser = func() { openBrowserLogged(url) }
	opts.OnAbout = func() { desktop.Open(about) }
	if showAtStart {
		opts.OnReady = desktop.Show
	}
	return opts
}

// windowShower 是第二次启动（POST /api/window/show，见 internal/httpserver/window.go）在这个实例上做的事：
// 用桌面窗口时把它提到前面（藏起来的、最小化的、还没建过的都算），否则为 nil —— 新进程收到「没有窗口」
// 就照旧开浏览器。desktop.Show 自己切到界面线程，不阻塞 HTTP 处理器。
// 窗口中途用不了时（Windows 的 WebView2 起不来、退回了浏览器模式）答「没有窗口」，由新进程开浏览器。
func windowShower(window bool) httpserver.ShowWindowFunc {
	if !window {
		return nil
	}
	return func() bool {
		if !desktop.State().Active {
			return false
		}
		desktop.Show()
		return true
	}
}
