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
	"log"
	"os"
	"path/filepath"
	"strings"

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
//   - 不自动加载配置目录里的脚本，只点名加载 nagare 的界面，以及 thumbfast（thumbfast 为真时）。
//     thumbfast 要另起一个命令行 mpv 生成缩略图；它在 macOS 上找不到 mpv 时的兜底是用 `ps -o comm=`
//     找「自己这个进程」的可执行文件 —— 那就是 nagare 本身，会把 nagare 当 mpv 拉起来。所以只有确实
//     有一个命令行 mpv 交给它（thumbnailMPV）时才加载它，否则整个不加载。
//   - NAGARE_DESKTOP_MPV_LOG：开发时排查用的 mpv 日志（日志里会有媒体地址，只在显式设置时写）。
func embeddedOptions(configDir string, thumbfast bool) []mpv.Option {
	o := []mpv.Option{{Name: "vo", Kind: mpv.OptionString, Value: "libmpv"}}
	if configDir != "" {
		scripts := []string{filepath.Join(configDir, "scripts", "nagare")}
		if thumbfast {
			scripts = append(scripts, filepath.Join(configDir, filepath.FromSlash(mpv.ThumbfastScript)))
		}
		o = append(o,
			mpv.Option{Name: "load-scripts", Kind: mpv.OptionFlag, Value: "no"},
			mpv.Option{Name: "scripts", Kind: mpv.OptionList, Items: scripts},
		)
	}
	if path := os.Getenv("NAGARE_DESKTOP_MPV_LOG"); path != "" {
		o = append(o, mpv.Option{Name: "log-file", Kind: mpv.OptionString, Value: path})
	}
	return o
}

// selfExecutable 返回 nagare 自己的可执行文件（解析过符号链接）。测试替换它。
var selfExecutable = func() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(exe)
}

// thumbnailMPV 从起播参数里取出交给 thumbfast 的命令行 mpv（player 在探测到命令行 mpv 时给
// thumbfast-mpv_path），确认它能用：绝对路径、是个普通文件、而且不是 nagare 自己。不能用就返回空串
// 和原因 —— 嵌入时就不加载 thumbfast。
func thumbnailMPV(scriptOpts []string) (string, string) {
	path := mpv.ThumbfastMPVPath(scriptOpts)
	if path == "" {
		return "", "没有命令行 mpv"
	}
	if !filepath.IsAbs(path) {
		return "", "命令行 mpv 的路径不是绝对路径"
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", "命令行 mpv 不存在"
	}
	st, err := os.Stat(resolved)
	if err != nil || !st.Mode().IsRegular() {
		return "", "命令行 mpv 不是普通文件"
	}
	self, err := selfExecutable()
	if err != nil {
		return "", "取不到 nagare 自己的路径，没法确认 thumbfast 不会把 nagare 当 mpv 拉起来"
	}
	if selfSt, err := os.Stat(self); err != nil || resolved == self || os.SameFile(st, selfSt) {
		return "", "交给 thumbfast 的 mpv 就是 nagare 自己"
	}
	return path, ""
}

// thumbnailScriptOpts 按 thumbnailMPV 的结论改写 script-opts（返回新切片，不动传进来的）：
// 能用就追加按屏幕缩放放大的缩略图尺寸（render API 下 thumbfast 读不到 display-hidpi-scale）；
// 不能用就去掉 thumbfast-mpv_path（thumbfast 不加载，这一项没人读）。
func thumbnailScriptOpts(scriptOpts []string, usable bool, hidpi float64) []string {
	out := make([]string, 0, len(scriptOpts)+2)
	for _, kv := range scriptOpts {
		if !usable && strings.HasPrefix(kv, "thumbfast-mpv_path=") {
			continue
		}
		out = append(out, kv)
	}
	if usable {
		out = append(out, mpv.ThumbfastScaledSize(hidpi)...)
	}
	return out
}

// libmpvSpec 把一次起播翻成进程内 libmpv 的选项（命令行那份 + embeddedOptions）。
// hidpi 是窗口所在屏幕的缩放（backingScaleFactor），只用来算缩略图尺寸。
func libmpvSpec(opts mpv.LaunchOptions, endpoint string, hidpi float64) (mpv.LibmpvSpec, error) {
	thumb := ""
	if opts.ConfigDir != "" {
		var why string
		if thumb, why = thumbnailMPV(opts.ScriptOpts); thumb == "" {
			log.Printf("desktop: 进度条不出缩略图：%s", why)
		}
	}
	opts.ScriptOpts = thumbnailScriptOpts(opts.ScriptOpts, thumb != "", hidpi)
	spec := mpv.NewLaunchSpec(opts, endpoint)
	spec.Options = append(spec.Options, embeddedOptions(opts.ConfigDir, thumb != "")...)
	return spec.Libmpv()
}
