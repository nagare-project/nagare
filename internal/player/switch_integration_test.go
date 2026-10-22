package player

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nagare-project/nagare/internal/library"
	"github.com/nagare-project/nagare/internal/mpv"
	"github.com/nagare-project/nagare/internal/store"
)

// 端到端：真 mpv + nagare 自己的界面脚本，两集各 2 秒。第一集播到尾 → keep-open 停住 → 倒计时 →
// 同一个 mpv 进程里换到第二集 → 第二集是最后一集，播完关窗。两集都记为看完，OnSessionEnd 恰好一次。
// 没装 mpv 或 ffmpeg 就跳过。
func TestNextEpisodeSameWindowRealMPV(t *testing.T) {
	if testing.Short() {
		t.Skip("-short 模式跳过真 mpv 集成测试")
	}
	mpvInfo, err := mpv.Detect("")
	if err != nil {
		t.Skipf("本机无可用 mpv：%v", err)
	}
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("本机无 ffmpeg，跳过")
	}
	shortNavDelays(t)
	upNextDelay = 300 * time.Millisecond

	// 短路径：mpv 的 IPC socket 放在这下面，darwin 上 sun_path 只有 104 字节
	dir, err := os.MkdirTemp("", "nagnav")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	lib := filepath.Join(dir, "lib")
	require.NoError(t, os.MkdirAll(lib, 0o755))
	var files []library.SourceFile
	for i := 1; i <= 2; i++ {
		name := fmt.Sprintf("[Test] Show - %02d [1080p].mkv", i)
		media := filepath.Join(lib, name)
		out, err := exec.Command(ffmpeg, "-y", "-f", "lavfi", "-i", "testsrc2=duration=2:size=160x90:rate=10",
			"-c:v", "libx264", "-preset", "ultrafast", media).CombinedOutput()
		require.NoError(t, err, "ffmpeg 生成测试视频失败：%s", out)
		files = append(files, library.SourceFile{RelPath: name, AbsPath: media, Size: int64(i), MTimeMs: 1})
	}
	items := library.BuildItems(files)
	require.Len(t, items, 2)
	require.Equal(t, 1, *items[0].Episode)

	st, err := store.Open(filepath.Join(dir, "state.json"))
	require.NoError(t, err)
	var ends atomic.Int32
	m := New(Options{
		Store:        st,
		MPV:          mpv.NewRuntimeWith(func(string) (mpv.Info, error) { return mpvInfo, nil }, ""),
		RuntimeDir:   dir,
		UIConfigDir:  func() (string, error) { return mpv.PrepareUIConfig(dir) },
		Navigator:    &fakeNav{eps: items, subs: map[string]string{}},
		OnSessionEnd: func() { ends.Add(1) },
	})
	t.Cleanup(m.Stop)

	_, err = m.Play(context.Background(), NewLocalSource(items[0]), "")
	require.NoError(t, err)
	pb := m.currentPlayback()
	require.NotNil(t, pb)
	var pid1 int
	require.NoError(t, pb.player.GetProperty("pid", &pid1))

	require.Eventually(t, func() bool { return m.Status().FileID == items[1].FileID }, 20*time.Second, 20*time.Millisecond,
		"第一集播完没有在同一个窗口里换到第二集（界面脚本没就绪、keep-open 没开？）")
	assert.Same(t, pb, m.currentPlayback(), "还是同一个窗口")
	var pid2 int
	require.NoError(t, pb.player.GetProperty("pid", &pid2))
	assert.Equal(t, pid1, pid2, "还是同一个 mpv 进程")
	var path string
	require.Eventually(t, func() bool {
		return pb.player.GetProperty("path", &path) == nil && path == items[1].AbsPath
	}, 10*time.Second, 20*time.Millisecond, "mpv 里放的要是第二集")

	select {
	case <-pb.finished:
	case <-time.After(20 * time.Second):
		t.Fatal("第二集是最后一集：播完要关窗")
	}
	for _, it := range items {
		p, ok := st.Progress(it.FileID)
		require.True(t, ok, "%s 要有进度", it.FileName)
		assert.True(t, p.Completed, "%s 要记为看完", it.FileName)
	}
	assert.Equal(t, int32(1), ends.Load(), "OnSessionEnd 每个窗口恰好一次")
	assert.False(t, m.Status().Playing)
	assert.Nil(t, m.Status().PlaybackFailure)
}
