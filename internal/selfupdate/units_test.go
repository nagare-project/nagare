package selfupdate

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLookupChecksum(t *testing.T) {
	const want = "a2b0c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f90"
	tests := []struct {
		name     string
		manifest string
		asset    string
		wantSum  string
		wantErr  string
	}{
		{
			name:     "coreutils 的两个空格",
			manifest: want + "  nagare-0.2.0_Linux_x86_64.tar.gz\n",
			asset:    "nagare-0.2.0_Linux_x86_64.tar.gz",
			wantSum:  want,
		},
		{
			name:     "二进制模式的星号前缀",
			manifest: want + " *nagare-0.2.0_Windows_x86_64.zip\n",
			asset:    "nagare-0.2.0_Windows_x86_64.zip",
			wantSum:  want,
		},
		{
			name:     "大写十六进制归一化成小写",
			manifest: strings.ToUpper(want) + "  a.tar.gz\n",
			asset:    "a.tar.gz",
			wantSum:  want,
		},
		{
			name:     "多行里挑出对的那条",
			manifest: "0000000000000000000000000000000000000000000000000000000000000000  other.zip\n" + want + "  a.tar.gz\n",
			asset:    "a.tar.gz",
			wantSum:  want,
		},
		{
			name:     "CRLF 行尾",
			manifest: want + "  a.tar.gz\r\n",
			asset:    "a.tar.gz",
			wantSum:  want,
		},
		{
			name:     "清单里没有这个文件",
			manifest: want + "  other.zip\n",
			asset:    "a.tar.gz",
			wantErr:  "没有适用于当前平台",
		},
		{
			name:     "同名两条哈希互相矛盾",
			manifest: want + "  a.tar.gz\n0000000000000000000000000000000000000000000000000000000000000000  a.tar.gz\n",
			asset:    "a.tar.gz",
			wantErr:  "互相矛盾",
		},
		{
			name:     "哈希与文件名之间没有空白",
			manifest: want + "a.tar.gz\n",
			asset:    "a.tar.gz",
			wantErr:  "没有适用于当前平台",
		},
		{
			name:     "哈希不是十六进制",
			manifest: strings.Repeat("z", 64) + "  a.tar.gz\n",
			asset:    "a.tar.gz",
			wantErr:  "没有适用于当前平台",
		},
		{
			name:     "空清单",
			manifest: "",
			asset:    "a.tar.gz",
			wantErr:  "没有适用于当前平台",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := lookupChecksum([]byte(tc.manifest), tc.asset)
			if tc.wantErr != "" {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.wantSum, got)
		})
	}
}

// TestAssetName 钉死产物命名。这些名字必须与 .goreleaser.yaml 和
// scripts/macos/build-app.sh 逐字一致 —— 对不上的后果是「有新版本但更新不了」，
// 而且只有真发一次版才会暴露。
func TestAssetName(t *testing.T) {
	tests := []struct {
		channel      Channel
		goos, goarch string
		want         string
		wantErr      bool
	}{
		{ChannelAppBundle, "darwin", "arm64", "nagare-0.2.0_MacOS_universal.app.zip", false},
		{ChannelAppBundle, "darwin", "amd64", "nagare-0.2.0_MacOS_universal.app.zip", false},
		{ChannelDirect, "darwin", "arm64", "nagare-0.2.0_MacOS_universal.tar.gz", false},
		{ChannelDirect, "darwin", "amd64", "nagare-0.2.0_MacOS_universal.tar.gz", false},
		{ChannelDirect, "linux", "amd64", "nagare-0.2.0_Linux_x86_64.tar.gz", false},
		{ChannelDirect, "linux", "arm64", "nagare-0.2.0_Linux_arm64.tar.gz", false},
		{ChannelDirect, "windows", "amd64", "nagare-0.2.0_Windows_x86_64.zip", false},
		{ChannelDirect, "linux", "riscv64", "", true},
		{ChannelDirect, "windows", "arm64", "", true},
		{ChannelDirect, "freebsd", "amd64", "", true},
	}
	for _, tc := range tests {
		t.Run(string(tc.channel)+"/"+tc.goos+"/"+tc.goarch, func(t *testing.T) {
			got, err := assetName(tc.channel, tc.goos, tc.goarch, "0.2.0")
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestResolveBaseURL(t *testing.T) {
	const repo = "nagare-project/nagare"
	t.Run("默认是仓库的 releases/download", func(t *testing.T) {
		t.Setenv(EnvBaseURL, "")
		got, err := resolveBaseURL("", repo)
		require.NoError(t, err)
		require.Equal(t, "https://github.com/nagare-project/nagare/releases/download", got)
	})
	t.Run("环境变量覆盖", func(t *testing.T) {
		t.Setenv(EnvBaseURL, "https://mirror.example/nagare/")
		got, err := resolveBaseURL("", repo)
		require.NoError(t, err)
		require.Equal(t, "https://mirror.example/nagare", got, "结尾的斜杠要去掉")
	})
	t.Run("显式参数优先于环境变量", func(t *testing.T) {
		t.Setenv(EnvBaseURL, "https://mirror.example/nagare")
		got, err := resolveBaseURL("https://explicit.example/dl", repo)
		require.NoError(t, err)
		require.Equal(t, "https://explicit.example/dl", got)
	})
	t.Run("拒绝明文 http", func(t *testing.T) {
		t.Setenv(EnvBaseURL, "http://mirror.example/nagare")
		_, err := resolveBaseURL("", repo)
		require.Error(t, err)
		require.Contains(t, err.Error(), "https")
	})
	t.Run("拒绝 file 协议", func(t *testing.T) {
		_, err := resolveBaseURL("file:///tmp/evil", repo)
		require.Error(t, err)
	})
	t.Run("拒绝没有主机名的地址", func(t *testing.T) {
		_, err := resolveBaseURL("https:///nowhere", repo)
		require.Error(t, err)
	})
	t.Run("New 在环境变量不合法时就报错", func(t *testing.T) {
		t.Setenv(EnvBaseURL, "http://insecure.example")
		_, err := New(Options{CurrentVersion: "0.1.0"})
		require.Error(t, err)
	})
}

func TestAssetURL(t *testing.T) {
	u, err := New(Options{CurrentVersion: "0.1.0", BaseURL: "https://dl.example/n"})
	require.NoError(t, err)
	require.Equal(t, "https://dl.example/n/v0.2.0/checksums.txt", u.assetURL("0.2.0", checksumsName))
}

func TestNormalizeVersion(t *testing.T) {
	tests := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{"0.2.0", "0.2.0", false},
		{"v0.2.0", "0.2.0", false},
		{" 1.0.0 ", "1.0.0", false},
		{"1.0.0-rc.1", "1.0.0-rc.1", false},
		{"../../etc", "", true},
		{"0.2", "", true},
		{"0.2.0/../evil", "", true},
		{"0.2.0%2F..", "", true},
		{"", "", true},
		{"latest", "", true},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			got, err := normalizeVersion(tc.in)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestNewRejectsBadRepo(t *testing.T) {
	_, err := New(Options{CurrentVersion: "0.1.0", Repo: "not-a-repo"})
	require.Error(t, err)
	_, err = New(Options{CurrentVersion: "0.1.0", Repo: "a/b/c"})
	require.Error(t, err)
}

// TestGuardDowngrade：本地开发构建（-dev）没法比较版本，不拦。
func TestGuardDowngrade(t *testing.T) {
	tests := []struct {
		current, next string
		wantErr       bool
	}{
		{"0.1.0", "0.2.0", false},
		{"0.1.0", "0.1.0", false},
		{"0.2.0", "0.1.0", true},
		{"0.1.0-dev", "0.1.0", false},
		{"", "0.1.0", false},
		{"not-semver", "0.1.0", false},
	}
	for _, tc := range tests {
		t.Run(tc.current+"→"+tc.next, func(t *testing.T) {
			u := &Updater{current: tc.current}
			err := u.guardDowngrade(tc.next)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestCheckTrustedComment(t *testing.T) {
	s := newTestSigner(t)
	msg := []byte("checksums")

	t.Run("版本对上", func(t *testing.T) {
		require.NoError(t, checkTrustedComment(s.sign(t, msg, "nagare 0.2.0 checksums", false), "0.2.0"))
	})
	t.Run("版本对不上", func(t *testing.T) {
		err := checkTrustedComment(s.sign(t, msg, "nagare 0.1.5 checksums", false), "0.2.0")
		require.Error(t, err)
		require.Contains(t, err.Error(), "不是要安装的版本")
	})
	t.Run("注释里没有版本号就不拦", func(t *testing.T) {
		require.NoError(t, checkTrustedComment(s.sign(t, msg, "timestamp:1700000000", false), "0.2.0"))
	})
	t.Run("签名解析不了也不拦（Verify 已经过了）", func(t *testing.T) {
		require.NoError(t, checkTrustedComment([]byte("garbage"), "0.2.0"))
	})
}

func TestTrunc(t *testing.T) {
	require.Equal(t, "abc", trunc("abc", 10))
	require.Equal(t, "abcde…", trunc("abcdefghij", 5))
	require.Equal(t, "a b", trunc("a\nb", 10), "换行要压掉，日志一条就是一行")
}
