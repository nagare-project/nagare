package mpv

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// syncPoint 发一条请求并等应答：假 mpv 先写完之前推的事件才写应答，返回即代表读循环已处理完它们。
func syncPoint(t *testing.T, f *fakeMPV, p *Player) {
	t.Helper()
	f.setProp("mpv-version", "mpv 0.41.0")
	require.NoError(t, p.GetProperty("mpv-version", nil))
}

func kindsOf(events []Event) []EventKind {
	out := make([]EventKind, 0, len(events))
	for _, ev := range events {
		out = append(out, ev.Kind)
	}
	return out
}

// 同一个窗口里换文件：start-file 之后上一个文件的 Ended 去重标记、位置、时长都要清掉，
// 下一个文件播完要能再发一次 Ended。
func TestEvents_StartFileResetsEndedAndPosition(t *testing.T) {
	f := newFakeMPV(t)
	p := attachTestPlayer(t, f, "/a.mkv")

	f.PushProperty(t, "time-pos", 1420.0)
	f.PushProperty(t, "duration", 1440.0)
	f.pushEvent(t, map[string]any{"event": "end-file", "reason": "stop"})
	syncPoint(t, f, p)
	require.Equal(t, State{TimePos: 1420, Duration: 1440, Path: "/a.mkv"}, p.State())

	f.pushEvent(t, map[string]any{"event": "start-file", "playlist_entry_id": 2})
	syncPoint(t, f, p)
	st := p.State()
	assert.Zero(t, st.TimePos, "上一个文件的位置不能留给新文件")
	assert.Zero(t, st.Duration)

	f.pushEvent(t, map[string]any{"event": "file-loaded"})
	f.pushEvent(t, map[string]any{"event": "end-file", "reason": "eof"})
	syncPoint(t, f, p)

	var ended []string
	var kinds []EventKind
	for _, ev := range drainEvents(p) {
		if ev.Kind == EventTimePos {
			continue
		}
		kinds = append(kinds, ev.Kind)
		if ev.Kind == EventEnded {
			ended = append(ended, ev.Reason)
		}
	}
	assert.Equal(t, []EventKind{EventEnded, EventFileStarted, EventFileLoaded, EventEnded}, kinds)
	assert.Equal(t, []string{"stop", "eof"}, ended, "每个文件各发一次 Ended")
}

// keep-open 下播到尾、拖回去、再播到尾：每个上升沿都要有 EOF（不去重），落下要有 EOFCleared；
// Ended 仍然每个文件只发一次。null（文件卸载）按落下算，但没到过尾就不发。
func TestEvents_EOFEveryRisingEdge(t *testing.T) {
	f := newFakeMPV(t)
	p := attachTestPlayer(t, f, "")

	f.PushProperty(t, "eof-reached", nil) // 还没有文件：不算落下
	f.PushProperty(t, "eof-reached", false)
	f.PushProperty(t, "eof-reached", true)
	f.PushProperty(t, "eof-reached", true) // 重复推送不是新的上升沿
	f.PushProperty(t, "eof-reached", false)
	f.PushProperty(t, "eof-reached", true)
	f.PushProperty(t, "eof-reached", nil)
	syncPoint(t, f, p)

	assert.Equal(t, []EventKind{EventEOF, EventEnded, EventEOFCleared, EventEOF, EventEOFCleared}, kindsOf(drainEvents(p)))
}

// 消费方一慢，time-pos 会把通道塞满；按「丢最旧」腾位会把 FileStarted / Ended 挤掉。
// 位置事件只在通道不到半满时投递，生命周期事件永远有位置；State 仍是最新位置。
func TestEvents_TimePosFloodKeepsRoomForLifecycleEvents(t *testing.T) {
	f := newFakeMPV(t)
	p := attachTestPlayer(t, f, "")

	for i := 0; i < 300; i++ {
		f.PushProperty(t, "time-pos", float64(i))
	}
	f.pushEvent(t, map[string]any{"event": "start-file"})
	for i := 0; i < 300; i++ {
		f.PushProperty(t, "time-pos", float64(i))
	}
	f.pushEvent(t, map[string]any{"event": "end-file", "reason": "eof"})
	syncPoint(t, f, p)

	assert.Equal(t, 299.0, p.State().TimePos, "位置照样写进状态")
	events := drainEvents(p)
	timePos := 0
	var lifecycle []EventKind
	for _, ev := range events {
		if ev.Kind == EventTimePos {
			timePos++
			continue
		}
		lifecycle = append(lifecycle, ev.Kind)
	}
	assert.LessOrEqual(t, timePos, eventBufferSize/2)
	assert.Equal(t, []EventKind{EventFileStarted, EventEnded}, lifecycle, "生命周期事件一条都不能丢")
}

// start-file 之后的 file-loaded 照样异步刷新 path：换文件后 State().Path 是新文件（回归保护）。
func TestEvents_FileStartedThenLoadedRefreshesPath(t *testing.T) {
	f := newFakeMPV(t)
	p := attachTestPlayer(t, f, "/a.mkv")
	f.setProp("path", "/b.mkv")
	f.pushEvent(t, map[string]any{"event": "start-file"})
	f.pushEvent(t, map[string]any{"event": "file-loaded"})
	waitEventKind(t, p, EventFileLoaded)
	require.Eventually(t, func() bool { return p.State().Path == "/b.mkv" }, 2*time.Second, 10*time.Millisecond)
}

func TestEventKindString(t *testing.T) {
	assert.Equal(t, "FileStarted", EventFileStarted.String())
	assert.Equal(t, "EOF", EventEOF.String())
	assert.Equal(t, "EOFCleared", EventEOFCleared.String())
	assert.Equal(t, "Unknown", EventKind(0).String())
}
