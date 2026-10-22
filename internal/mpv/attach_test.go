package mpv

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// 进程内 libmpv（AttachEndpoint）：mpv 的 IPC 从不发 shutdown，会话结束就是连接断开。报过 end-file
// 之后的干净断开（返回、q、关窗、最后一集播完）是正常结束；还没报过就断开仍然算异常 ——
// 否则每次返回媒体库都会在网页上留一条「播放失败」。
func TestInProcessDisconnectAfterEndFileIsNormal(t *testing.T) {
	for _, tc := range []struct {
		name    string
		endFile bool
		wantErr bool
	}{
		{"报过 end-file 再断开", true, false},
		{"没报 end-file 就断开", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeMPV(t)
			p, err := attachInProcess(f.dial(t), "", "/fake/a.mkv")
			require.NoError(t, err)
			t.Cleanup(func() { _ = p.Close() })
			if tc.endFile {
				f.pushEvent(t, map[string]any{"event": "end-file", "reason": "quit"})
				waitEventKind(t, p, EventEnded)
			}
			f.closeConn()
			requireDoneWithin(t, p, 3*time.Second)
			if tc.wantErr {
				require.Error(t, p.Err())
			} else {
				require.NoError(t, p.Err())
			}
		})
	}
}

// 命令行 mpv 与测试用的 Attach 不受影响：干净断开、没有进程可看时照旧算异常。
func TestAttachDisconnectAfterEndFileStillAnError(t *testing.T) {
	f := newFakeMPV(t)
	p := attachTestPlayer(t, f, "/fake/a.mkv")
	f.pushEvent(t, map[string]any{"event": "end-file", "reason": "quit"})
	waitEventKind(t, p, EventEnded)
	f.closeConn()
	requireDoneWithin(t, p, 3*time.Second)
	require.Error(t, p.Err())
}
