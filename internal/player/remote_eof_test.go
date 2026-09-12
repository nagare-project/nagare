package player

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/nagare-project/nagare/internal/mpv"
	"github.com/nagare-project/nagare/internal/store"
	"github.com/stretchr/testify/require"
)

// 在线流被截断：服务器只给出前一小段就正常结束连接，mpv 报的是 eof 而不是 error。
// 这必须记成来源故障（进自动换源），并且不能把这一集标成看完。
func TestRemoteTruncatedStreamIsFailureNotCompletion(t *testing.T) {
	info, err := mpv.Detect("")
	if err != nil {
		t.Skipf("本机无可用 mpv：%v", err)
	}
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("本机无 ffmpeg，跳过")
	}
	dir := t.TempDir()
	full := filepath.Join(dir, "full.mp4")
	// faststart 把 moov 放前面：截断后时长仍可知，与真实在线 mp4 一致。
	out, err := exec.Command(ffmpeg, "-y", "-f", "lavfi", "-i", "testsrc2=duration=20:size=320x180:rate=10",
		"-c:v", "libx264", "-preset", "ultrafast", "-movflags", "+faststart", full).CombinedOutput()
	require.NoError(t, err, "ffmpeg 生成测试视频失败：%s", out)
	data, err := os.ReadFile(full)
	require.NoError(t, err)
	truncated := data[:len(data)*2/5]

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "ep.mp4", time.Time{}, bytes.NewReader(truncated))
	}))
	defer server.Close()

	st, err := store.Open(filepath.Join(dir, "state.json"))
	require.NoError(t, err)
	m := New(Options{Store: st, RuntimeDir: dir, MPV: mpv.NewRuntimeWith(func(string) (mpv.Info, error) { return info, nil }, "")})
	defer m.Stop()
	result, err := m.Play(context.Background(), NewRemoteSource(RemoteSourceOptions{
		SourceID: "fixture", CandidateID: "truncated", Title: "截断测试", Episode: 1, URL: server.URL + "/ep.mp4",
	}), "")
	require.NoError(t, err)

	require.Eventually(t, func() bool { return !m.Status().Playing }, 30*time.Second, 100*time.Millisecond)
	m.Stop()
	status := m.Status()
	require.NotNil(t, status.PlaybackFailure, "提前 eof 要记为播放失败")
	require.Equal(t, result.FileID, status.PlaybackFailure.FileID)
	require.Contains(t, status.PlaybackFailure.Reason, "提前结束")
	if p, ok := st.Progress(result.FileID); ok {
		require.False(t, p.Completed, "只播了一小段不能算看完")
	}
}
