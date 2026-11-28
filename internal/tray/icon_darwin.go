//go:build darwin && cgo

package tray

import (
	_ "embed"

	"fyne.io/systray"
)

// iconMac 是 32×32 的模板图（纯黑 + alpha）：菜单栏按明暗主题自动着色。
// 由 gen-icons.sh 按 tomoru 断臂光缝图形生成。
//
//go:embed icon_mac.png
var iconMac []byte

// applyIcon 设置模板图；第二个参数是非模板回退，macOS 上用不到，传同一份即可。
func applyIcon() { systray.SetTemplateIcon(iconMac, iconMac) }
