// Package desktop 是 nagare 的桌面窗口：一个窗口两层 —— 网页（WKWebView，照旧从 127.0.0.1 载入、
// 同一套鉴权）和 mpv 的画面（进程内 libmpv + render API）。播放时 mpv 盖满窗口，控制条仍是
// internal/mpv/ui 的 Lua 界面（嵌入模式，见 docs/player-ui.md）；mpv 的会话结束（q、返回、Esc、
// 最后一集播完）就换回网页，窗口不关。
//
// 现在只有 macOS，而且只在 darwin && cgo && libmpv 三者齐备时编进来（libmpv 是新的构建标签）。
// 其余任何平台与标签组合都是桩：Available 为假，nagare 的行为与以前一样（浏览器 + 独立的 mpv 窗口）。
//
// 平台层是自己写的薄薄一层 ObjC（不用 webview / Wails 一类框架），也不加 JS 桥：网页本来就走 HTTP
// 调 Go，A4 的鉴权原样适用。mpv 的控制仍走 JSON IPC（libmpv 开的 input-ipc-server 与外部 mpv
// 是同一个协议），internal/player 只多知道一件事：Launch 是进程内的（EmbeddedMPV）。
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
}

// embeddedOptions 是进程内 libmpv 在 LaunchSpec 之外要多带的几条。
//
//   - vo=libmpv：画面经 render API 交给窗口画（macOS 0.37 起不再认 --wid，原型实测 mpv 会另开窗口）。
//   - 不加载 thumbfast：它要另起一个命令行 mpv 生成缩略图，而嵌入时根本没有那个可执行文件；它在
//     macOS 上的兜底是用 `ps -o comm=` 找「自己这个进程」的可执行文件 —— 那就是 nagare 本身，
//     会把 nagare 当 mpv 拉起来。所以关掉配置目录里脚本的自动加载，只点名加载 nagare 的界面。
//     TODO(desktop): 我们自己构建 libmpv 时（分支 ci/libmpv-macos）用 -Dcplayer=true 顺带出一个
//     命令行 mpv 放进 .app，再把 thumbfast 加回来、把 thumbfast-mpv_path 指过去。缩略图尺寸也要那时
//     一起处理：thumbfast 按 display-hidpi-scale 放大缩略图，render API 下读不到它 —— 经 script-opts
//     传按宿主缩放算好的 thumbfast-max_width / thumbfast-max_height，不改 thumbfast.lua。
//   - NAGARE_DESKTOP_MPV_LOG：开发时排查用的 mpv 日志（日志里会有媒体地址，只在显式设置时写）。
func embeddedOptions(configDir string) []mpv.Option {
	o := []mpv.Option{{Name: "vo", Kind: mpv.OptionString, Value: "libmpv"}}
	if configDir != "" {
		o = append(o,
			mpv.Option{Name: "load-scripts", Kind: mpv.OptionFlag, Value: "no"},
			mpv.Option{Name: "scripts", Kind: mpv.OptionList, Items: []string{filepath.Join(configDir, "scripts", "nagare")}},
		)
	}
	if path := os.Getenv("NAGARE_DESKTOP_MPV_LOG"); path != "" {
		o = append(o, mpv.Option{Name: "log-file", Kind: mpv.OptionString, Value: path})
	}
	return o
}

// libmpvSpec 把一次起播翻成进程内 libmpv 的选项（命令行那份 + embeddedOptions）。
func libmpvSpec(opts mpv.LaunchOptions, endpoint string) (mpv.LibmpvSpec, error) {
	spec := mpv.NewLaunchSpec(opts, endpoint)
	spec.Options = append(spec.Options, embeddedOptions(opts.ConfigDir)...)
	return spec.Libmpv()
}
