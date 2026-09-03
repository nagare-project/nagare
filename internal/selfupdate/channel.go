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
// managedSegments 是路径里出现即判定「归包管理器管」的片段。
//
// 与 packagePrefixes 的区别是它们不在固定前缀上：Homebrew 的 Caskroom 与 Scoop
// 的应用目录都在用户家目录下，位置随安装而变，只能靠中间那一段认。
var managedSegments = []string{
	"/caskroom/",   // Homebrew cask：/opt/homebrew/Caskroom/... 或 /usr/local/Caskroom/...
	"/scoop/apps/", // Scoop：~/scoop/apps/nagare/current/...（含 SCOOP_GLOBAL 的自定义根）
}

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

	// 包管理器装的一律不自更新，这一条要排在 app bundle 判定【之前】：
	// Homebrew cask 装的就是 /Applications/Nagare.app，只看「是不是 .app」会把它
	// 判成可自更新，整包替换之后 brew 的版本记录立刻过时，下次 brew upgrade 会拿
	// 它自己记的旧版本盖回去。Scoop 同理（~/scoop/apps/... 下就地替换会让 Scoop
	// 的版本记账错位，还会在应用目录里留下 .old 残留）。
	//
	// 这两条不是理论推演：接 Homebrew 与 Scoop 渠道时两边各自独立撞上了。
	if managedBy(exePath, goos) {
		return install{channel: ChannelPackage, exePath: exePath, target: exePath, dir: filepath.Dir(exePath)}
	}

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
		// 包管理器判定已经在上面统一做过了（managedBy）。
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

// managedBy 判断这个可执行文件是不是由包管理器安放的。
//
// 路径统一成小写正斜杠再比：Windows 的 Scoop 路径是反斜杠，而盘符与用户名的
// 大小写不可预期（`C:\Users\Foo\scoop\apps\...`）。macOS 的文件系统默认
// 也不区分大小写。只按原样比对会漏判，而漏判的后果是自更新覆盖掉包管理器的文件。
func managedBy(exePath, goos string) bool {
	// 固定前缀只在类 Unix 上有意义：/usr/ 这类路径在 Windows 上不代表任何东西
	// （既有测试盯着这一点）。而下面的片段判定是跨平台的 —— /scoop/apps/ 与
	// /caskroom/ 足够独特，不会在别的平台上误命中。
	if goos != "windows" && hasPackagePrefix(exePath) {
		return true
	}
	// 手工换反斜杠，不用 filepath.ToSlash —— 它按【宿主机】的分隔符工作，
	// 而 classify 是按传进来的 goos 跨平台判定的：在 macOS 上判一条 Windows 路径时
	// ToSlash 什么都不做，Scoop 的路径就漏判了。（测试正是这么发现的。）
	norm := strings.ToLower(strings.ReplaceAll(exePath, `\`, "/"))
	for _, seg := range managedSegments {
		if strings.Contains(norm, seg) {
			return true
		}
	}
	return false
}
