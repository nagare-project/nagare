package main

import (
	"github.com/nagare-project/nagare/internal/desktop"
	"github.com/nagare-project/nagare/internal/tray"
)

// 桌面窗口（internal/desktop）的接线：这次运行用不用它、播放交给谁、托盘与 Dock 的「打开」指向哪。
//
// 用的时候：首次启动、点 Dock、托盘的「打开 nagare」都开窗口而不是浏览器；托盘另有「在浏览器中打开」。
// 所有播放（窗口里的网页、外部浏览器里的网页都一样）都在窗口里放，并把窗口提到前面 ——
// 播放器的 Launch 换成 desktop.Launch，此外 internal/player 不知道窗口的存在。

// desktopEnabled 决定这次运行用不用桌面窗口：这个构建得带着它（macOS + cgo + libmpv 标签），还要有
// Cocoa 的事件循环（由托盘跑；--no-tray 时没有）和内嵌界面（API-only 时窗口里没东西可看）。
// --no-window 退回以前的浏览器 + 独立 mpv 窗口。
func desktopEnabled(f flags, apiOnly bool) bool {
	return desktopUsable(desktop.Available(), f, apiOnly)
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
