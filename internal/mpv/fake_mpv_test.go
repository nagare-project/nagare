package mpv

import (
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/nagare-project/nagare/internal/mpv/mpvtest"
)

// fakeMPV 是 mpvtest.Fake 的薄包装：保留本包测试原来的小写方法名。
// 假 mpv 本体搬到了 mpvtest，上层包（internal/player）的测试也要用它驱动真正的 Player。
type fakeMPV struct{ *mpvtest.Fake }

func newFakeMPV(t *testing.T) *fakeMPV {
	t.Helper()
	return &fakeMPV{Fake: mpvtest.New(t)}
}

func (f *fakeMPV) dial(t *testing.T) net.Conn { t.Helper(); return f.Dial(t) }

func (f *fakeMPV) pushEvent(t *testing.T, ev map[string]any) { t.Helper(); f.Push(t, ev) }

func (f *fakeMPV) closeConn() { f.CloseConn() }

func (f *fakeMPV) setProp(name string, value any) { f.SetProp(name, value) }

func (f *fakeMPV) setSilent(v bool) { f.SetSilent(v) }

func (f *fakeMPV) commands() [][]any { return f.Commands() }

func (f *fakeMPV) lastCommand(t *testing.T) []any { t.Helper(); return f.LastCommand(t) }

// attachTestPlayer 建立客户端连接并 attach 一个无进程 Player。
func attachTestPlayer(t *testing.T, f *fakeMPV, initialPath string) *Player {
	t.Helper()
	p, err := Attach(f.dial(t), initialPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = p.Close() })
	return p
}

// waitEventKind 从 Events() 读取直到出现指定类别的事件（跳过其余），超时即失败。
func waitEventKind(t *testing.T, p *Player, kind EventKind) Event {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		select {
		case ev := <-p.Events():
			if ev.Kind == kind {
				return ev
			}
		case <-deadline:
			t.Fatalf("等待 %v 事件超时", kind)
			return Event{}
		}
	}
}

// requireDoneWithin 断言 Done() 在限时内关闭。
func requireDoneWithin(t *testing.T, p *Player, d time.Duration) {
	t.Helper()
	select {
	case <-p.Done():
	case <-time.After(d):
		t.Fatal("Done() 未在限时内关闭")
	}
}
