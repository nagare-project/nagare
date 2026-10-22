//go:build darwin && cgo && libmpv

package desktop

// macOS 这一侧对 Windows 一起加进接口的几件事的回答（Windows 的实现见 desktop_windows.go）。

// Ready 报告这台机器上窗口能不能用：macOS 的 WKWebView 是系统自带的，libmpv 链接在二进制里，没有运行时可缺。
func Ready() error { return nil }

// InProcess 报告 mpv 是不是跑在 nagare 进程里：macOS 是进程内 libmpv（不要 mpv 可执行文件）。
func InProcess() bool { return true }

// State 是窗口此刻的状态。macOS 的窗口不会中途退回浏览器。
func State() Info { return Info{Active: true} }

// MainLoop 在 macOS 上没有：Cocoa 的事件循环由托盘（fyne systray 的 NSApplication）跑。
func MainLoop(<-chan struct{}) func() { return nil }

// BeforeHandoff 在 macOS 上什么都不做：已在运行的那一份自己 activateIgnoringOtherApps 就能到前面。
func BeforeHandoff() {}
