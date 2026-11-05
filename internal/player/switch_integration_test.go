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

// realNavClipSeconds 是两集测试片的长度。以前是 2 秒，满载下偶发失败（压测 3/120）：界面脚本就绪、
// keep-open 打开要赶在第一集播完之前，最后一集的 keep-open 关回去要赶在它播完之前，而 mpv 偶尔会
// 5 秒左右不答复。现在片子放长，这两个前提按条件等，等的时候把那一集停住；剩下要赶的只有「换集之后、
// 停住第二集之前」那一次读属性，6 秒的片子留足了余量。
const realNavClipSeconds = 6

// realNavWait 是每一步等待的上限：条件一成立就往下走，只在出错时才等满。
const realNavWait = 30 * time.Second

// 端到端：真 mpv + nagare 自己的界面脚本，两集各 realNavClipSeconds 秒。第一集播到尾 → keep-open 停住 →
// 倒计时 → 同一个 mpv 进程里换到第二集 → 第二集是最后一集，keep-open 关回去，播完关窗。两集都记为看完，
// OnSessionEnd 恰好一次。没装 mpv 或 ffmpeg 就跳过。
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
		src := fmt.Sprintf("testsrc2=duration=%d:size=160x90:rate=10", realNavClipSeconds)
		out, err := exec.Command(ffmpeg, "-y", "-f", "lavfi", "-i", src,
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
	waitResolving(t, m)
	t.Cleanup(m.Stop)

	_, err = m.Play(context.Background(), NewLocalSource(items[0]), "")
	require.NoError(t, err)
	pb := m.currentPlayback()
	require.NotNil(t, pb)
	began := time.Now()
	stage := func(name string) { t.Logf("%s：开播后 %s", name, time.Since(began).Round(time.Millisecond)) }

	// 第一集先停住，等界面就绪、keep-open 打开再放。不等的话，界面脚本加载得慢（满载）时第一集先播完，
	// mpv 照 keep-open=no 直接退出 —— 那是测试片太短，不是要测的东西（正片二十几分钟）。
	setPauseMPV(t, pb, true)
	awaitMPVProp(t, pb, "keep-open", true, "有下一集、界面就绪：keep-open 要打开")
	stage("keep-open 打开")
	pid1 := mpvPID(t, pb)
	setPauseMPV(t, pb, false)

	require.Eventually(t, func() bool { return m.Status().FileID == items[1].FileID }, realNavWait, 20*time.Millisecond,
		"第一集播完没有在同一个窗口里换到第二集")
	stage("换到第二集")
	assert.Same(t, pb, m.currentPlayback(), "还是同一个窗口")
	// 停在第一集最后一帧时是暂停着的（keep-open 停住时 mpv 自己暂停），新文件开始时 nagare 放开它。
	// 放开了就马上停住第二集：它是最后一集，keep-open 要关回去，播完才会关窗 —— 等这一步时不让片子
	// 跟事件循环赛跑。关回去之前就播到尾的那种竞态是另一回事，由假 mpv 的确定性测试守着（见 #81）；
	// 这里要测的是真 mpv 上正常的一条路走通。
	awaitMPVProp(t, pb, "pause", false, "换到第二集要接着播")
	setPauseMPV(t, pb, true)
	awaitMPVProp(t, pb, "path", items[1].AbsPath, "mpv 里放的要是第二集")
	assert.Equal(t, pid1, mpvPID(t, pb), "还是同一个 mpv 进程")
	awaitMPVProp(t, pb, "keep-open", false, "最后一集：keep-open 要关回去")
	stage("keep-open 关回去")
	setPauseMPV(t, pb, false)

	select {
	case <-pb.finished:
		stage("窗口收尾")
	case <-time.After(realNavWait):
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

// awaitMPVProp 等 mpv 的属性 name 读出来等于 want。读不到（mpv 一时不答复、IPC 超时）就接着读。
func awaitMPVProp[T comparable](t *testing.T, pb *playback, name string, want T, msg string) {
	t.Helper()
	retryMPV(t, pb, fmt.Sprintf("%s（mpv 属性 %s）", msg, name), func() error {
		// 解进指针：没有值（null、空应答）时它留着 nil，不会拿零值冒充 want == false
		var got *T
		if err := pb.player.GetProperty(name, &got); err != nil {
			return err
		}
		if got == nil {
			return fmt.Errorf("没有值")
		}
		if *got != want {
			return fmt.Errorf("读到 %v，要 %v", *got, want)
		}
		return nil
	})
}

// setPauseMPV 暂停 / 继续，mpv 一时不答复就重试。
func setPauseMPV(t *testing.T, pb *playback, paused bool) {
	t.Helper()
	retryMPV(t, pb, fmt.Sprintf("mpv 设不上 pause=%v", paused), func() error { return pb.player.SetPause(paused) })
}

// mpvPID 读 mpv 进程号，mpv 一时不答复就重试。
func mpvPID(t *testing.T, pb *playback) int {
	t.Helper()
	var pid int
	retryMPV(t, pb, "读不到 mpv 的进程号", func() error {
		if err := pb.player.GetProperty("pid", &pid); err != nil {
			return err
		}
		if pid <= 0 {
			return fmt.Errorf("进程号是 %d", pid)
		}
		return nil
	})
	return pid
}

// retryMPV 反复做 op 直到它成功，最多 realNavWait。窗口已经收尾（mpv 退出了）就不再等，直接失败：
// 那时每次都会立刻出错，干等 30 秒只会把真正的原因（比如第一集播完 mpv 就退了）藏在超时后面。
func retryMPV(t *testing.T, pb *playback, msg string, op func() error) {
	t.Helper()
	var err error
	for deadline := time.Now().Add(realNavWait); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if err = op(); err == nil {
			return
		}
		select {
		case <-pb.finished:
			t.Fatalf("%s：mpv 已经退出了（%v）", msg, err)
		default:
		}
	}
	t.Fatalf("%s：%s 内没成（%v）", msg, realNavWait, err)
}
