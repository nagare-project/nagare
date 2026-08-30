package player

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nagare-player/nagare/internal/library"
	"github.com/nagare-player/nagare/internal/mpv"
	"github.com/nagare-player/nagare/internal/store"
)

// 端到端集成（T1 精神）：真 ffmpeg 造 1 秒视频 → 真 mpv 播完自动退出 →
// 断言进度已写、按 eof 标记看完。本机没有 mpv/ffmpeg 时跳过（CI 会跳过）。
func TestPlayRealMPVEndToEnd(t *testing.T) {
	mpvInfo, err := mpv.Detect("")
	if err != nil {
		t.Skipf("本机无可用 mpv：%v", err)
	}
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("本机无 ffmpeg，跳过")
	}

	dir := t.TempDir()
	media := filepath.Join(dir, "[test] nagare - 01 [1080p].mkv")
	// 1 秒无声测试卡（体积小、任何 mpv 都能解）。
	gen := exec.Command(ffmpeg, "-y", "-f", "lavfi", "-i", "testsrc2=duration=1:size=320x180:rate=10",
		"-c:v", "libx264", "-preset", "ultrafast", media)
	out, err := gen.CombinedOutput()
	require.NoError(t, err, "ffmpeg 生成测试视频失败：%s", out)

	st, err := store.Open(filepath.Join(dir, "state.json"))
	require.NoError(t, err)
	m := New(Options{Store: st, Client: nil, MPV: mpvInfo, RuntimeDir: dir})

	items := library.BuildItems([]library.SourceFile{{
		RelPath: filepath.Base(media), AbsPath: media, Size: 1, MTimeMs: 1,
	}})
	require.Len(t, items, 1)

	res, err := m.Play(context.Background(), items[0], "")
	require.NoError(t, err)
	// Play 先起 mpv 再后台解析弹幕，所以立刻返回的是 loading；
	// 离线（Client=nil）时后台会很快收敛成 none。
	assert.Equal(t, "loading", res.Danmaku.State, "Play 应立即返回，弹幕后台解析")
	require.Eventually(t, func() bool {
		st := m.Status()
		return st.Danmaku != nil && st.Danmaku.State == "none"
	}, 5*time.Second, 50*time.Millisecond, "离线模式弹幕最终应标 none")

	// keep-open=no：播完 mpv 自动退出；等状态收敛。
	deadline := time.After(20 * time.Second)
	for {
		if !m.Status().Playing {
			break
		}
		select {
		case <-deadline:
			m.Stop()
			t.Fatal("mpv 未在期限内自然退出")
		case <-time.After(200 * time.Millisecond):
		}
	}
	// Status().Playing 为 false 只说明 mpv 已退出；等 watcher 完成最终回写。
	m.Stop()

	p, ok := st.Progress(items[0].FileID)
	require.True(t, ok, "播完应写入进度")
	assert.True(t, p.Completed, "eof 结束应标记看完")
}
