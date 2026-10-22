package selfupdate

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// envReleaseAppZip 指向一个真实的 nagare-<版本>_MacOS_universal.app.zip（scripts/macos/build-app.sh 的产物）。
// 发布流水线在 goreleaser 之后设它跑这一个测试（release.yml「自更新解包往返」）；平时不设，测试跳过。
const envReleaseAppZip = "NAGARE_RELEASE_APPZIP"

// 用客户端自更新【同一套】解包代码解开发布出去的 .app.zip，确认解出来的 Nagare.app 装上就能用：
// 签名有效、自建 libmpv 的库都是真实文件且都在、命令行 mpv 有可执行位，而且在这台机器的架构上
// 主程序与 mpv 都能把 Contents/Frameworks 里的库全部加载起来（CI 的 runner 是 arm64，
// 我们手上没有 Apple Silicon 的 Mac，arm64 切片只在这里跑得到）。
//
// build-app.sh 已经用 ditto / unzip 验过往返；unzip 只是与 Go archive/zip「同一档能力」，
// 这里跑的是真东西。
func TestReleaseAppZip(t *testing.T) {
	zipPath := os.Getenv(envReleaseAppZip)
	if zipPath == "" {
		t.Skip("没有设 " + envReleaseAppZip + "（只在发布流水线里跑）")
	}
	if runtime.GOOS != "darwin" {
		t.Skip("Nagare.app 只能在 macOS 上验")
	}

	dst := t.TempDir()
	require.NoError(t, extractArchive(zipPath, dst, filepath.Base(zipPath)), "自更新的解包代码解不开发布的 .app.zip")
	app, err := findAppBundle(dst)
	require.NoError(t, err)
	require.NoError(t, verifyBundle(app))

	out, err := exec.Command("codesign", "--verify", "--deep", "--strict", "--verbose=2", app).CombinedOutput()
	require.NoError(t, err, "解包后签名校验不过 —— 自更新装上去会打不开：\n%s", out)

	contents := filepath.Join(app, "Contents")
	libs, err := filepath.Glob(filepath.Join(contents, "Frameworks", "*.dylib"))
	require.NoError(t, err)
	require.NotEmpty(t, libs, "Contents/Frameworks 里没有库")
	assert.FileExists(t, filepath.Join(contents, "Frameworks", "libmpv.2.dylib"))
	for _, lib := range libs {
		st, err := os.Lstat(lib)
		require.NoError(t, err)
		assert.True(t, st.Mode().IsRegular(), "%s 不是普通文件", filepath.Base(lib))
	}
	for _, exe := range []string{"nagare", "mpv"} {
		st, err := os.Stat(filepath.Join(contents, "MacOS", exe))
		require.NoError(t, err)
		assert.NotZero(t, st.Mode().Perm()&0o111, "Contents/MacOS/%s 解出来没有可执行位", exe)
	}

	// 在这台机器的架构上真的起一次：dyld 加载不了哪个库，进程当场就退出、stderr 里写着是哪一个
	mpvOut, err := exec.Command(filepath.Join(contents, "MacOS", "mpv"), "--no-config", "--version").CombinedOutput()
	require.NoError(t, err, "命令行 mpv 起不来（%s）：\n%s", runtime.GOARCH, mpvOut)
	assert.True(t, strings.HasPrefix(string(mpvOut), "mpv "), "mpv --version 的输出不对：%s", mpvOut)
	t.Logf("mpv（%s）：%s", runtime.GOARCH, firstLine(string(mpvOut)))

	nagareOut, err := exec.Command(filepath.Join(contents, "MacOS", "nagare"), "-version").CombinedOutput()
	require.NoError(t, err, "nagare 起不来（%s，桌面窗口版链接了 libmpv）：\n%s", runtime.GOARCH, nagareOut)
	t.Logf("nagare（%s）：%s", runtime.GOARCH, firstLine(string(nagareOut)))

	// 用包里的命令行 mpv 真解一段：H.264 + AAC + ASS（解码、字幕、音频都走一遍），再像 thumbfast 那样
	// 缩放出一张图 —— 只能证明这台机器的架构上 Frameworks 里那套库能干活，不经过 render API（那一段在
	// libmpv-macos.yml 的 verify.sh 里测）。
	media := filepath.Join("..", "..", "scripts", "macos", "libmpv", "tests", "media", "h264-aac-ass.mkv")
	require.FileExists(t, media)
	mpvBin := filepath.Join(contents, "MacOS", "mpv")
	out, err = exec.Command(mpvBin, "--no-config", "--vo=null", "--ao=null", "--frames=24", "--msg-level=all=error", media).CombinedOutput()
	require.NoError(t, err, "包里的 mpv 解不了测试片段（%s）：\n%s", runtime.GOARCH, out)
	imgDir := t.TempDir()
	out, err = exec.Command(mpvBin, "--no-config", "--ao=null", "--frames=1", "--vf=scale=w=384:h=216",
		"--vo=image", "--vo-image-format=jpg", "--vo-image-outdir="+imgDir, "--msg-level=all=error", media).CombinedOutput()
	require.NoError(t, err, "包里的 mpv 出不了缩略图（%s）：\n%s", runtime.GOARCH, out)
	shots, _ := filepath.Glob(filepath.Join(imgDir, "*.jpg"))
	assert.Len(t, shots, 1, "应当恰好出一张 jpg")
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return line
}
