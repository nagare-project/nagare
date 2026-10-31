package mpv

import (
	"log"
	"os"
	"path/filepath"
)

// 进程内 libmpv 的起播选项：宿主（macOS 桌面窗口 internal/desktop，以及将来 iOS 的 internal/mobile）
// 共用这一份拼法 —— NewLaunchSpec 那份清单，加上画面经 render API 交给宿主画（vo=libmpv），再按宿主补几条。
// 这里不碰 cgo，只给数据（LibmpvSpec）；真正调 libmpv 的是宿主那一层。

// DesktopEmbeddedOptions 是 macOS 桌面窗口里的进程内 libmpv 在 LaunchSpec 之外要多带的几条。
//
//   - vo=libmpv：画面经 render API 交给窗口画（macOS 0.37 起不再认 --wid，原型实测 mpv 会另开窗口）。
//   - 不自动加载配置目录里的脚本，只点名加载 nagare 的界面，以及 thumbfast（thumbfast 为真时）。
//     thumbfast 要另起一个命令行 mpv 生成缩略图；它在 macOS 上找不到 mpv 时的兜底是用 `ps -o comm=`
//     找「自己这个进程」的可执行文件 —— 那就是 nagare 本身，会把 nagare 当 mpv 拉起来。所以只有确实
//     有一个命令行 mpv 交给它（thumbfastCLI）时才加载它，否则整个不加载。
//   - NAGARE_DESKTOP_MPV_LOG：开发时排查用的 mpv 日志（日志里会有媒体地址，只在显式设置时写）。
func DesktopEmbeddedOptions(configDir string, thumbfast bool) []Option {
	o := []Option{{Name: "vo", Kind: OptionString, Value: "libmpv"}}
	if configDir != "" {
		scripts := []string{filepath.Join(configDir, "scripts", "nagare")}
		if thumbfast {
			scripts = append(scripts, filepath.Join(configDir, filepath.FromSlash(ThumbfastScript)))
		}
		o = append(o,
			Option{Name: "load-scripts", Kind: OptionFlag, Value: "no"},
			Option{Name: "scripts", Kind: OptionList, Items: scripts},
		)
	}
	if path := os.Getenv("NAGARE_DESKTOP_MPV_LOG"); path != "" {
		o = append(o, Option{Name: "log-file", Kind: OptionString, Value: path})
	}
	return o
}

// DesktopLibmpvSpec 把一次起播翻成 macOS 桌面窗口里进程内 libmpv 的选项（命令行那份 + DesktopEmbeddedOptions）。
// hidpi 是窗口所在屏幕的缩放（backingScaleFactor），只用来算缩略图尺寸；self 返回 nagare 自己的可执行文件
// （解析过符号链接），用来确认交给 thumbfast 的命令行 mpv 不是 nagare 自己（见 thumbfastCLI）。
func DesktopLibmpvSpec(opts LaunchOptions, endpoint string, hidpi float64, self func() (string, error)) (LibmpvSpec, error) {
	thumb := ""
	if opts.ConfigDir != "" {
		var why string
		if thumb, why = thumbfastCLI(opts.ScriptOpts, self); thumb == "" {
			log.Printf("mpv: 桌面窗口的进度条不出缩略图：%s", why)
		}
	}
	opts.ScriptOpts = ThumbfastScriptOpts(opts.ScriptOpts, thumb != "", hidpi)
	spec := NewLaunchSpec(opts, endpoint)
	spec.Options = append(spec.Options, DesktopEmbeddedOptions(opts.ConfigDir, thumb != "")...)
	return spec.Libmpv()
}
