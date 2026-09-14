// Package tray 是 nagare 在后台运行时的「总控」：双击启动的 nagare 没有终端窗口，
// 用户关掉浏览器标签页之后，这是唯一能看见「它还在跑」并主动退出的地方。
//
// 平台策略（决议 D1 / M4 零证书方案；2026-09-14 用户定「三层都做、mac 走 Dock 应用」）：
//   - macOS（需要 cgo）：fyne.io/systray 的菜单栏图标；装成 .app 时再加 Dock 图标 +
//     应用菜单（⌘O 打开界面 / ⌘Q 退出）+ Dock 右键菜单，⌘Q / 注销 / 关机走优雅退出
//   - Windows：fyne.io/systray 托盘图标 + 首次启动一条气泡通知（新托盘图标默认被折进「^」）
//   - Linux：fyne.io/systray 的 StatusNotifierItem（纯 DBus，无 cgo）+ 首次启动一条桌面通知；
//     没有会话总线（headless / systemd 服务）时退回无托盘
//   - CI 的 CGO_ENABLED=0 交叉编译 macOS 同样落到无托盘的桩实现（tray_stub.go）
//
// 约束：Run 必须在主 goroutine 调用（macOS 的 Cocoa 事件循环要求主线程；
// systray 包在 init 里 LockOSThread 就是为此），HTTP 服务等长活工作放到其他 goroutine。
package tray

import "time"

// Mode 描述本次运行里用户能从哪里看见 / 退出 nagare；设置页据此写提示文案。
type Mode string

const (
	// ModeNone 无任何图标：退出靠界面里的按钮、信号或 Ctrl+C。
	ModeNone Mode = "none"
	// ModeMenuBar 只有 macOS 右上角菜单栏图标（裸二进制、Homebrew 装的那种）。
	ModeMenuBar Mode = "menubar"
	// ModeDock macOS Dock 图标 + 应用菜单 + 菜单栏图标（.app 安装包）。
	ModeDock Mode = "dock"
	// ModeTray Windows / Linux 系统托盘图标。
	ModeTray Mode = "tray"
)

// Notice 是托盘就绪后弹一次的系统通知（首次启动告诉用户「关浏览器不等于退出」）。
type Notice struct {
	Title string
	Body  string
}

// Options 是托盘的全部输入。
type Options struct {
	// URL 是界面地址（含 token）。只交给 OnOpen 那条路用，托盘本身不显示、不记日志。
	URL string
	// Address 是不带 token 的界面地址（http://127.0.0.1:<port>/）：菜单里原样显示，
	// 「复制地址」复制的也是它 —— 剪贴板会被各种剪贴板管理器留档，token 不进去。
	// 浏览器首次访问过「打开界面」之后 token 已存在其本地存储里，裸地址照样能开。
	Address string
	// Version 显示在菜单第一行与图标的悬停提示里。
	Version string
	// OnOpen 在用户点「打开界面」时调用（主进程负责开浏览器）。
	OnOpen func()
	// OnAbout 在用户点「关于 nagare」时调用（主进程开到设置页的「关于」分区）。
	// macOS 的应用菜单另有系统标准的「关于」面板，不经这条路。
	OnAbout func()
	// OnQuit 在用户点「退出 nagare」时调用，早于 Run 返回；主进程在这里取消根 ctx。
	OnQuit func()
	// Done 在主进程收尾（停播放、回写进度、关磁力引擎）完成后关闭。
	// macOS 的 ⌘Q / 注销 / 关机路径要等它：系统在等我们答复「可以终止了」，
	// 答早了进度就丢在半路。nil 表示没有可等的，立即答复。
	Done <-chan struct{}
	// Notice 非 nil 时在托盘就绪后弹一次系统通知；主进程只在首次启动时传。
	Notice *Notice
}

// terminateGrace 是 macOS 终止路径上等收尾的上限：用户已经明确要退出，
// 收尾卡死时不能让「退出」这件事本身也卡死（注销 / 关机会被我们挡住）。
var terminateGrace = 15 * time.Second // 变量而非常量：单测把它调短
