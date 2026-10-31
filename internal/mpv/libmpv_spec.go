package mpv

import (
	"log"
	"os"
	"path/filepath"
	"slices"
)

// 进程内 libmpv 的起播选项：宿主（macOS 桌面窗口 internal/desktop，以及将来 iOS 的 internal/mobile）
// 共用这一份拼法 —— NewLaunchSpec 那份清单，加上画面经 render API 交给宿主画（vo=libmpv），再按宿主补几条。
// 这里不碰 cgo，只给数据（LibmpvSpec）；真正调 libmpv 的是宿主那一层。
//
//	            桌面窗口（DesktopLibmpvSpec）           iOS（IOSLibmpvSpec）
//	控制条       nagare 的 Lua 界面（点名加载）           SwiftUI 画：不加载任何脚本，mpv 自带的 osc 也关掉
//	缩略图       thumbfast（有命令行 mpv 时才加载）        不加载（iOS 起不了子进程，缩略图由 Swift 另做）
//	预读上限     网络流 256MiB，本地文件用 mpv 默认         一律 64MiB 向前 / 16MiB 向后（R12 内存预算）
//	Anime4K      照 LaunchOptions.Shaders（设置默认关）    同左：宿主不自己加着色器
//
// 其余每一项（IPC、配置目录、音量、标题、字幕、续播、请求头……）两边都出自同一个 NewLaunchSpec，
// 以后加的选项自动同时到两边（TestIOSAndDesktopShareLaunchSpec 守着）。

// iOS 的预读上限（R12）。iPhone 上 Go 堆、mpv 的解码缓冲与预读缓存、缩略图用的第二个 libmpv 同在一个进程，
// 超了前台内存系统直接结束 App；桌面的 256MiB（streamCacheOptions）与 mpv 对本地文件的默认值
// （150MiB 向前 / 50MiB 向后）都是按电脑定的。数值是估计值，在最老的那台设备上用 Instruments 量过再调。
const (
	iosDemuxerMaxBytes     = "64MiB"
	iosDemuxerMaxBackBytes = "16MiB"
)

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

// iosEmbeddedOptions 是 iOS 上的进程内 libmpv 在 LaunchSpec 之外要多带（或换掉）的几条，每次返回新切片。
//
//   - vo=libmpv：画面经 render API 交给 Swift 在自己视图上建的 OpenGL ES 上下文画（R5）。
//   - load-scripts=no：配置目录里的脚本一个都不自动加载，有没有配置目录、读不读配置都一样 —— nagare 的 Lua
//     界面与 thumbfast 都在那里；控制条由 SwiftUI 画（D4），thumbfast 还要起子进程，而 iOS 起不了。
//   - osc=no：mpv 自带的控制条也关掉。libmpvProfile 默认把它打开，那是给桌面「界面没加载起来」兜底的；
//     iOS 上它只会叠在 SwiftUI 控制条下面。
//   - demuxer-max-bytes / demuxer-max-back-bytes：预读上限（iosDemuxerMaxBytes），本地文件与网络流一样。
func iosEmbeddedOptions() []Option {
	return []Option{
		stringOption("vo", "libmpv"),
		flagOption("load-scripts", false),
		flagOption("osc", false),
		stringOption("demuxer-max-bytes", iosDemuxerMaxBytes),
		stringOption("demuxer-max-back-bytes", iosDemuxerMaxBackBytes),
	}
}

// IOSLibmpvSpec 把一次起播翻成 iOS 上进程内 libmpv 的选项（R5：Go 建并持有 mpv 句柄，Swift 接画面与交互）。
// 与 DesktopLibmpvSpec 出自同一份 NewLaunchSpec，差别见 iosEmbeddedOptions：网络流那份 256MiB 就地换成
// iOS 的预算，清单里同名的只留一条；script-opts 整个不传（不加载任何脚本就没人读，thumbfast-mpv_path 一类
// 在 iOS 上也指不到能起的程序）。
//
// 着色器照 LaunchOptions.Shaders 传：Anime4K 是用户设置，默认关（store.PlayerConfig 的零值），宿主不自己加，
// 用户在 iOS 上打开画质增强才有（R12）。
func IOSLibmpvSpec(opts LaunchOptions, endpoint string) (LibmpvSpec, error) {
	opts.ScriptOpts = nil
	spec := NewLaunchSpec(opts, endpoint)
	spec.Options = withOptions(spec.Options, iosEmbeddedOptions())
	return spec.Libmpv()
}

// withOptions 把 extra 合进 base，返回新切片（不动 base）：同名的就地换掉，其余按顺序接在后面。
func withOptions(base, extra []Option) []Option {
	out := slices.Clone(base)
	for _, o := range extra {
		if i := slices.IndexFunc(out, func(b Option) bool { return b.Name == o.Name }); i >= 0 {
			out[i] = o
			continue
		}
		out = append(out, o)
	}
	return out
}
