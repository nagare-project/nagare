// Package desktop 是 nagare 的桌面窗口：一个窗口两层 —— 网页（照旧从 127.0.0.1 载入、同一套鉴权）和 mpv 的画面。
// 播放时 mpv 盖满窗口，控制条仍是 internal/mpv/ui 的 Lua 界面（嵌入模式，见 docs/player-ui.md）；mpv 的会话结束
// （q、返回、Esc、最后一集播完）就换回网页，窗口不关。
//
//   - macOS：WKWebView + 进程内 libmpv（render API），只在 darwin && cgo && libmpv 三者齐备时编进来。
//   - Windows：WebView2 + 外部 mpv.exe --wid=<子窗口>（纯 Go，CGO_ENABLED=0），默认构建就带着；机器上没有
//     WebView2 运行时、或者它起不来时退回浏览器模式（Ready / State）。
//   - 其余平台与标签组合是桩：Available 为假，nagare 的行为与以前一样（浏览器 + 独立的 mpv 窗口）。
//
// 平台层是自己写的薄薄一层（不用 webview / Wails 一类框架），也不加 JS 桥：网页本来就走 HTTP 调 Go，A4 的鉴权
// 原样适用。mpv 的控制仍走 JSON IPC，internal/player 只多知道一件事：Launch 是不是进程内的（InProcess）。
package desktop

import (
	"os"
	"path/filepath"

	"github.com/nagare-project/nagare/internal/mpv"
)

// Options 是窗口的输入。
type Options struct {
	// URL 是界面地址（含 token，与浏览器自动打开的是同一条）；第一次显示窗口时载入。
	URL string
	// Origin 是允许在窗口里打开的源（http://127.0.0.1:<port>）；别的地址（外链、target=_blank）
	// 交给系统默认浏览器。
	Origin string
	// DataDir 是 nagare 的配置目录。Windows：窗口的位置大小记在这里。macOS 不用。
	DataDir string
	// CacheDir 是本机的缓存目录（不随漫游配置同步）。Windows：WebView2 的用户数据目录放在 CacheDir\webview2
	// （几十到上百 MB 的浏览器缓存不该进 %AppData% 的漫游配置）；空串时退回 DataDir。macOS 不用。
	CacheDir string
	// OpenBrowser 在窗口用不了时（WebView2 起不来）代替窗口打开 url。Windows 用；macOS 不用。
	OpenBrowser func(url string)
	// Quit 让 nagare 退出并收尾（Windows 注销 / 关机时由窗口调，随后等进度回写完）。macOS 不用（走托盘的
	// NSTerminateLater）。
	Quit func()
}

// Info 是桌面窗口此刻的状态，给设置页看。
type Info struct {
	// Active：播放在 nagare 自己的窗口里。
	Active bool
	// Fallback 非空：窗口用不了、退回了浏览器模式，写的是原因（用户看得懂的中文）。
	Fallback string
}

// 进程内 libmpv 的起播选项在 internal/mpv（mpv.DesktopLibmpvSpec，与 iOS 的 mpv.IOSLibmpvSpec 共用一份拼法，
// 将来的 internal/mobile 不用引这个包）。下面几个是那边的薄包装：桌面窗口这一层只多提供 nagare 自己的路径
// 与屏幕缩放。embeddedOptions / thumbnailScriptOpts 只有 desktop_test.go 在用（挪动时那份测试原样保留，
// 作为 macOS 输出不变的回归证明）；以后改那份测试时可以直接改调 mpv 包里的函数，把这两个删掉。

// selfExecutable 返回 nagare 自己的可执行文件（解析过符号链接）。测试替换它。
var selfExecutable = func() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(exe)
}

// embeddedOptions 是进程内 libmpv 在 LaunchSpec 之外要多带的几条，见 mpv.DesktopEmbeddedOptions。
func embeddedOptions(configDir string, thumbfast bool) []mpv.Option {
	return mpv.DesktopEmbeddedOptions(configDir, thumbfast)
}

// thumbnailScriptOpts 按交给 thumbfast 的命令行 mpv 能不能用改写 script-opts，见 mpv.ThumbfastScriptOpts。
func thumbnailScriptOpts(scriptOpts []string, usable bool, hidpi float64) []string {
	return mpv.ThumbfastScriptOpts(scriptOpts, usable, hidpi)
}

// libmpvSpec 把一次起播翻成进程内 libmpv 的选项（命令行那份 + embeddedOptions），见 mpv.DesktopLibmpvSpec。
// hidpi 是窗口所在屏幕的缩放（backingScaleFactor），只用来算缩略图尺寸。
func libmpvSpec(opts mpv.LaunchOptions, endpoint string, hidpi float64) (mpv.LibmpvSpec, error) {
	return mpv.DesktopLibmpvSpec(opts, endpoint, hidpi, selfExecutable)
}
