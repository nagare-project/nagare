//go:build windows

package tray

import (
	_ "embed"

	"fyne.io/systray"
)

// iconWin 是含 16/32/48 三档的 ICO（带浅色圆角底，在深浅任务栏下均清晰可见）。
// 由 gen-icons.sh 从 packaging/icon/nagare-256.png 缩放生成。
//
//go:embed icon_win.ico
var iconWin []byte

// applyIcon 设置托盘图标；Windows 的 systray 只接受 ICO 字节。
func applyIcon() { systray.SetIcon(iconWin) }
