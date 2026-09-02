//go:build unix

package mpv

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// 验收：真 mpv 全链路 —— Detect → --idle=yes 启动（无需媒体文件）→ IPC 连接
// → get_property mpv-version → Close 收敛 → socket 文件清理。
// 机器上没装 mpv 就跳过；-short 跳过（会短暂弹出一个 mpv 窗口）。
func TestIntegration_RealMPV(t *testing.T) {
	if testing.Short() {
		t.Skip("-short 模式跳过真 mpv 集成测试")
	}
	mpvPath, err := exec.LookPath("mpv")
	if err != nil {
		t.Skip("未安装 mpv，跳过集成测试")
	}

	info, err := Detect(mpvPath)
	require.NoError(t, err)
	require.Equal(t, mpvPath, info.Path)
	require.NotEmpty(t, info.Version)

	// 独立短路径目录：避开 darwin sun_path 104 字节上限
	dir, err := os.MkdirTemp("", "nagit")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	p, err := Launch(ctx, LaunchOptions{
		MPVPath:   info.Path,
		SocketDir: dir,
		Title:     "nagare 集成测试",
		// MediaPath 留空 → --idle=yes
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = p.Close() })

	// IPC 命令通路：真 mpv 应答 get_property
	var version string
	require.NoError(t, p.GetProperty("mpv-version", &version))
	require.True(t, strings.HasPrefix(version, "mpv"), "mpv-version 属性应形如 \"mpv 0.41.0\"，实际 %q", version)

	// 命令写通路：idle 下设置暂停属性应成功
	require.NoError(t, p.SetPause(true))

	// 关闭并收敛：主动退出终态无错误
	require.NoError(t, p.Close())
	requireDoneWithin(t, p, 5*time.Second)
	require.NoError(t, p.Err())

	// 退出后 socket 文件已清理（mpv 自删或 terminate 兜底删除）
	require.Eventually(t, func() bool {
		entries, err := os.ReadDir(dir)
		return err == nil && len(entries) == 0
	}, 3*time.Second, 50*time.Millisecond, "退出后 SocketDir 下不应残留 socket 文件")
}

// 验收：Launch 预检 —— 媒体文件不存在时在启动进程之前就报错（不依赖真 mpv）。
func TestLaunch_MissingMediaRejectedEarly(t *testing.T) {
	_, err := Launch(context.Background(), LaunchOptions{
		MPVPath:   "/fake/mpv",
		MediaPath: "/nonexistent/nagare/void.mkv",
	})
	require.ErrorContains(t, err, "找不到要播放的文件")

	_, err = Launch(context.Background(), LaunchOptions{})
	require.ErrorContains(t, err, "缺少 mpv 路径")
}

// 验收：用户自己关掉 mpv（窗口关闭 / 按 q）不是异常 —— mpv 关 socket 时
// shutdown 事件常常送不到，IPC 先于进程退出断开；终态必须按 exit 0 判正常。
// 这里用 IPC 发 quit 模拟用户退出，而不是走 Close()。
func TestIntegration_UserQuitIsNotAnError(t *testing.T) {
	if testing.Short() {
		t.Skip("-short 模式跳过真 mpv 集成测试")
	}
	mpvPath, err := exec.LookPath("mpv")
	if err != nil {
		t.Skip("未安装 mpv，跳过集成测试")
	}
	info, err := Detect(mpvPath)
	require.NoError(t, err)

	dir, err := os.MkdirTemp("", "nagit")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	p, err := Launch(ctx, LaunchOptions{MPVPath: info.Path, SocketDir: dir})
	require.NoError(t, err)
	t.Cleanup(func() { _ = p.Close() })

	// 等价于用户在窗口里按 q：quit 的响应经常赶不上进程退出，错误不算数。
	_, _ = p.Command("quit")

	requireDoneWithin(t, p, 5*time.Second)
	require.NoError(t, p.Err(), "用户主动退出 mpv 必须判为正常结束，而不是「IPC 意外断开」")
}
