// Package tray 是菜单栏 / 系统托盘图标：双击启动的 nagare 没有终端窗口，
// 这是用户唯一能看见「它在跑」并主动退出的地方。
//
// 平台策略（决议 D1 / M4 零证书方案）：
//   - macOS（需要 cgo）与 Windows：fyne.io/systray，菜单「打开界面」「退出 nagare」
//   - Linux：走 deb/rpm + .desktop 启动器，不做托盘，界面里有退出按钮
//   - CI 的 CGO_ENABLED=0 交叉编译同样落到无托盘的桩实现（tray_stub.go）
//
// 约束：Run 必须在主 goroutine 调用（macOS 的 Cocoa 事件循环要求主线程；
// systray 包在 init 里 LockOSThread 就是为此），HTTP 服务等长活工作放到其他 goroutine。
package tray

// Options 是托盘的全部输入。
type Options struct {
	// URL 是界面地址（含 token）。只交给 OnOpen 那条路用，托盘本身不显示、不记日志。
	URL string
	// Version 显示在图标的悬停提示里。
	Version string
	// OnOpen 在用户点「打开界面」时调用（主进程负责开浏览器）。
	OnOpen func()
	// OnQuit 在用户点「退出 nagare」时调用，早于 Run 返回；主进程在这里取消根 ctx。
	OnQuit func()
}
