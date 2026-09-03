package mpv

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 验收：磁力边下边播交给 mpv 的是本机流端点 URL，Launch 不得对它做本地存在性
// 预检 —— os.Stat 对 URL 必然失败，会在起 mpv 之前就把磁力播放挡死。
func TestLaunchSkipsStatForHTTPMediaPath(t *testing.T) {
	const streamURL = "http://127.0.0.1:8823/stream/deadbeefcafe/t/0123456789abcdef/2"

	_, err := Launch(context.Background(), LaunchOptions{
		MPVPath:   "/nonexistent/nagare/mpv",
		MediaPath: streamURL,
	})
	// 必然失败（mpv 路径是假的），但绝不能是「找不到要播放的文件」——
	// 那说明 URL 走进了本地文件预检。
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "找不到要播放的文件")
}

// 与上一条成对：分支的另一侧不能被顺手放宽 —— 本地路径的存在性预检
// 必须原样保留（「文件被移走」的第一道拦截）。
func TestLaunchStillChecksLocalMediaPath(t *testing.T) {
	_, err := Launch(context.Background(), LaunchOptions{
		MPVPath:   "/nonexistent/nagare/mpv",
		MediaPath: "/nonexistent/nagare/void.mkv",
	})
	require.ErrorContains(t, err, "找不到要播放的文件")
}

func TestIsRemoteURL(t *testing.T) {
	for _, p := range []string{
		"http://127.0.0.1:8823/stream/a/t/b/0",
		"https://example.test/x.mkv",
		"HTTP://127.0.0.1/x", // scheme 大小写不敏感
	} {
		assert.True(t, isRemoteURL(p), "%q 应判为远端地址", p)
	}
	for _, p := range []string{
		"/Volumes/媒体/番剧/ep01.mkv",
		"file:///Volumes/x.mkv", // 非 http(s)：仍按本地路径预检
		"",
	} {
		assert.False(t, isRemoteURL(p), "%q 不应判为远端地址", p)
	}
}

// URL 同样走 "--" 分隔，参数组装与本地路径没有分叉。
func TestBuildArgsPassesURLAfterSeparator(t *testing.T) {
	const streamURL = "http://127.0.0.1:8823/stream/cap/t/hash/0"
	args := buildArgs(LaunchOptions{MediaPath: streamURL}, "/tmp/nagare.sock")
	require.GreaterOrEqual(t, len(args), 2)
	assert.Equal(t, []string{"--", streamURL}, args[len(args)-2:])
}
