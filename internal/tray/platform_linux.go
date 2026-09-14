//go:build linux

package tray

import (
	_ "embed"
	"fmt"
	"os"

	"fyne.io/systray"
	"github.com/godbus/dbus/v5"
)

// Linux 平台钩子：托盘走 StatusNotifierItem（KDE / 装了 AppIndicator 扩展的 GNOME /
// XFCE 等能显示；原版 GNOME 看不到），首次启动再经 org.freedesktop.Notifications
// 发一条桌面通知兜底 —— 托盘看不见的桌面至少还有通知中心。两者都是纯 DBus，无 cgo。

// iconLinux 是 64×64 的彩色 PNG（面板明暗主题都能看见；模板黑图在深色面板上是隐形的）。
// 由 packaging/icon/nagare-256.png 缩放而来。
//
//go:embed icon_linux.png
var iconLinux []byte

func applyIcon() { systray.SetIcon(iconLinux) }

func platformReady(Options) (<-chan struct{}, func()) { return nil, func() {} }

// Available 只在有会话总线时为真：headless / systemd 服务里没有它，
// systray 连不上总线会带着空连接一路走到退出时解引用。
func Available() bool { return os.Getenv("DBUS_SESSION_BUS_ADDRESS") != "" }

// CurrentMode 见 Mode。
func CurrentMode() Mode {
	if Available() {
		return ModeTray
	}
	return ModeNone
}

// showNotice 经 org.freedesktop.Notifications 发一条桌面通知（规范 1.2 的 Notify 签名）。
func showNotice(n Notice) error {
	conn, err := dbus.SessionBus()
	if err != nil {
		return fmt.Errorf("连接会话总线：%w", err)
	}
	obj := conn.Object("org.freedesktop.Notifications", "/org/freedesktop/Notifications")
	call := obj.Call("org.freedesktop.Notifications.Notify", 0,
		"nagare",                  // app_name
		uint32(0),                 // replaces_id
		"",                        // app_icon（不指定：图标资源没有装进系统主题目录）
		n.Title,                   // summary
		n.Body,                    // body
		[]string{},                // actions
		map[string]dbus.Variant{}, // hints
		int32(-1),                 // expire_timeout：交给通知服务的默认值
	)
	if call.Err != nil {
		return fmt.Errorf("Notifications.Notify：%w", call.Err)
	}
	return nil
}
