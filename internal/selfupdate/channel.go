package selfupdate

// 本文件回答一个问题：nagare 是【怎么装进来的】—— 这决定了能不能就地替换它自己。
// 判定只看可执行文件的真实路径，不读任何配置：配置会被复制、会过时，路径不会。

import (
	"path/filepath"
	"strings"
)

// Channel 是安装渠道。
type Channel string

const (
	// ChannelAppBundle：macOS，跑在 Xxx.app/Contents/MacOS/ 里，整包替换 .app。
	ChannelAppBundle Channel = "app-bundle"
	// ChannelDirect：Windows 安装包/便携版、Linux tar.gz，可就地替换二进制。
	ChannelDirect Channel = "direct"
	// ChannelPackage：deb/rpm/brew/nix/snap/flatpak，包管理器拥有这个文件，只提示不自更新。
	ChannelPackage Channel = "package"
	// ChannelUnknown：判不出来，按不支持处理。
	ChannelUnknown Channel = "unknown"
)

// packagePrefixes 是「这个文件归包管理器管」的路径前缀。
// 我们自己的 deb/rpm 装进 /usr/bin（见 .goreleaser.yaml 的 nfpms.bindir）。
// 在这些位置就地替换会与包管理器的文件清单打架：升级或卸载时它会按自己的记录
// 覆盖/删除，用户的自更新成果被静默抹掉，或者留下包管理器不认识的孤儿文件。
var packagePrefixes = []string{
	"/usr/",
	"/opt/homebrew/",
	"/home/linuxbrew/",
	"/nix/store/",
	"/snap/",
	"/var/lib/flatpak/",
}

// install 是一次安装的物理形态。
type install struct {
	channel Channel
	// exePath 是当前可执行文件的真实路径（已解符号链接）。重启时用它。
	exePath string
	// target 是将被替换的东西：普通二进制是它自己，app bundle 是那个 .app 目录。
	target string
	// dir 是替换发生的目录，也是暂存目录的落点：必须与 target 同卷、且可写。
	dir string
}

// detect 判定当前安装的形态。
func (u *Updater) detect() (install, error) {
	exe, err := u.env.executable()
	if err != nil {
		return install{}, err
	}
	// 解符号链接：Homebrew 的 bin/ 里是指向 Cellar 的链接，不解开就会把
	// package 渠道误判成 direct，然后去替换一个链接文件。
	if resolved, err := u.env.evalSymlinks(exe); err == nil {
		exe = resolved
	}
	return classify(exe, u.env.goos), nil
}

// classify 是纯函数形式的渠道判定，便于表驱动测试。
func classify(exePath, goos string) install {
	direct := install{channel: ChannelDirect, exePath: exePath, target: exePath, dir: filepath.Dir(exePath)}

	// .app 是 macOS 独有的形态。别的平台上就算路径长得像（有人把归档解到一个
	// 自己叫 foo.app 的目录里），也不能按 app bundle 处理 —— 那样会去下载
	// 一个 macOS 的更新包。
	if goos == "darwin" {
		if app := appBundleOf(exePath); app != "" {
			return install{channel: ChannelAppBundle, exePath: exePath, target: app, dir: filepath.Dir(app)}
		}
	}
	switch goos {
	case "windows":
		// Windows 没有系统包管理器意义上的固定前缀（NSIS 装到 %LOCALAPPDATA%，
		// 便携版在任意目录），一律按可就地替换处理，能不能写由 probeWritable 兜底。
		return direct
	case "darwin", "linux":
		if hasPackagePrefix(exePath) {
			return install{channel: ChannelPackage, exePath: exePath, target: exePath, dir: filepath.Dir(exePath)}
		}
		return direct
	default:
		return direct
	}
}

// appBundleOf 从 .../Xxx.app/Contents/MacOS/nagare 反推出 .../Xxx.app；不是这个形状返回空串。
// 逐层比目录名而不是查 ".app/Contents/MacOS/" 子串：后者会被路径里恰好含这段文字的
// 普通目录骗到（比如把 tar.gz 解到一个自己叫 foo.app 的目录里）。
func appBundleOf(exePath string) string {
	macOSDir := filepath.Dir(exePath)
	if filepath.Base(macOSDir) != "MacOS" {
		return ""
	}
	contents := filepath.Dir(macOSDir)
	if filepath.Base(contents) != "Contents" {
		return ""
	}
	app := filepath.Dir(contents)
	if !strings.HasSuffix(filepath.Base(app), ".app") {
		return ""
	}
	return app
}

func hasPackagePrefix(exePath string) bool {
	for _, p := range packagePrefixes {
		if strings.HasPrefix(exePath, p) {
			return true
		}
	}
	return false
}
