package selfupdate

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestClassify 覆盖每一条渠道判定规则。判定只看可执行文件的真实路径。
func TestClassify(t *testing.T) {
	tests := []struct {
		name    string
		goos    string
		exe     string
		want    Channel
		target  string
		dirWant string
	}{
		{
			name:    "macOS app bundle",
			goos:    "darwin",
			exe:     "/Applications/Nagare.app/Contents/MacOS/nagare",
			want:    ChannelAppBundle,
			target:  "/Applications/Nagare.app",
			dirWant: "/Applications",
		},
		{
			name:    "macOS app bundle 装在用户目录",
			goos:    "darwin",
			exe:     "/Users/someone/Applications/Nagare.app/Contents/MacOS/nagare",
			want:    ChannelAppBundle,
			target:  "/Users/someone/Applications/Nagare.app",
			dirWant: "/Users/someone/Applications",
		},
		{
			name:    "macOS 裸二进制在下载目录",
			goos:    "darwin",
			exe:     "/Users/someone/Downloads/nagare",
			want:    ChannelDirect,
			target:  "/Users/someone/Downloads/nagare",
			dirWant: "/Users/someone/Downloads",
		},
		{
			name:   "只是名字像 .app，层级对不上",
			goos:   "darwin",
			exe:    "/tmp/foo.app/nagare",
			want:   ChannelDirect,
			target: "/tmp/foo.app/nagare",
		},
		{
			name:   "MacOS 上一层不叫 Contents",
			goos:   "darwin",
			exe:    "/tmp/foo.app/Wrong/MacOS/nagare",
			want:   ChannelDirect,
			target: "/tmp/foo.app/Wrong/MacOS/nagare",
		},
		{
			name:   "linux 上路径像 .app 也不算 bundle",
			goos:   "linux",
			exe:    "/home/u/Nagare.app/Contents/MacOS/nagare",
			want:   ChannelDirect,
			target: "/home/u/Nagare.app/Contents/MacOS/nagare",
		},
		{name: "deb/rpm 装到 /usr/bin", goos: "linux", exe: "/usr/bin/nagare", want: ChannelPackage},
		{name: "/usr/local/bin 也归 /usr/ 前缀", goos: "linux", exe: "/usr/local/bin/nagare", want: ChannelPackage},
		{name: "Homebrew（Apple Silicon）", goos: "darwin", exe: "/opt/homebrew/Cellar/nagare/0.1.0/bin/nagare", want: ChannelPackage},
		{name: "Linuxbrew", goos: "linux", exe: "/home/linuxbrew/.linuxbrew/bin/nagare", want: ChannelPackage},
		{name: "Nix", goos: "linux", exe: "/nix/store/abc-nagare-0.1.0/bin/nagare", want: ChannelPackage},
		{name: "Snap", goos: "linux", exe: "/snap/nagare/12/bin/nagare", want: ChannelPackage},
		{name: "Flatpak", goos: "linux", exe: "/var/lib/flatpak/app/io.github.nagare/x/bin/nagare", want: ChannelPackage},
		{
			name:   "Linux tar.gz 解到用户目录",
			goos:   "linux",
			exe:    "/home/u/apps/nagare/nagare",
			want:   ChannelDirect,
			target: "/home/u/apps/nagare/nagare",
		},
		{
			name:   "Windows 按用户安装",
			goos:   "windows",
			exe:    `C:\Users\u\AppData\Local\Programs\nagare\nagare.exe`,
			want:   ChannelDirect,
			target: `C:\Users\u\AppData\Local\Programs\nagare\nagare.exe`,
		},
		{
			name:   "Windows 上 /usr/ 前缀不生效",
			goos:   "windows",
			exe:    "/usr/bin/nagare.exe",
			want:   ChannelDirect,
			target: "/usr/bin/nagare.exe",
		},
		{
			name:   "没听说过的平台按可就地替换处理",
			goos:   "freebsd",
			exe:    "/opt/nagare/nagare",
			want:   ChannelDirect,
			target: "/opt/nagare/nagare",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := classify(tc.exe, tc.goos)
			require.Equal(t, tc.want, got.channel)
			require.Equal(t, tc.exe, got.exePath)
			if tc.target != "" {
				require.Equal(t, tc.target, got.target)
			}
			if tc.dirWant != "" {
				require.Equal(t, tc.dirWant, got.dir)
			}
			require.Equal(t, filepath.Dir(got.target), got.dir, "dir 必须是 target 的父目录")
		})
	}
}

// TestDetectResolvesSymlinks：Homebrew 的 bin/ 里是指向 Cellar 的链接，
// 不解开就会把 package 判成 direct，然后去替换一个链接文件。
func TestDetectResolvesSymlinks(t *testing.T) {
	u := newTestUpdater(t, nil, "pubkey-placeholder", "linux", "amd64", "/usr/local/bin/nagare")
	u.env.evalSymlinks = func(string) (string, error) { return "/home/u/apps/nagare", nil }
	inst, err := u.detect()
	require.NoError(t, err)
	require.Equal(t, ChannelDirect, inst.channel)
	require.Equal(t, "/home/u/apps/nagare", inst.exePath)

	// 解不开时退回未解析的路径，而不是整个失败。
	u.env.evalSymlinks = func(string) (string, error) { return "", errors.New("链接断了") }
	inst, err = u.detect()
	require.NoError(t, err)
	require.Equal(t, ChannelPackage, inst.channel)
}

// TestCapabilityUnknownWhenExecutableUnavailable：连自己在哪都不知道时，
// 界面上要如实说不支持，而不是让按钮亮着。
func TestCapabilityUnknownWhenExecutableUnavailable(t *testing.T) {
	u := newTestUpdater(t, nil, "pubkey-placeholder", "linux", "amd64", "")
	u.env.executable = func() (string, error) { return "", errors.New("取不到") }
	capability := u.Capability()
	require.False(t, capability.Supported)
	require.Equal(t, ChannelUnknown, capability.Channel)
	require.Contains(t, capability.Reason, "手动下载")
}

// TestCapabilityRejectsUnsupportedPlatform：没发布对应平台的包时提前说清楚。
func TestCapabilityRejectsUnsupportedPlatform(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "nagare")
	writeFile(t, exe, "x", 0o755)
	u := newTestUpdater(t, nil, "pubkey-placeholder", "linux", "riscv64", exe)
	capability := u.Capability()
	require.False(t, capability.Supported)
	require.Contains(t, capability.Reason, "riscv64")
}
