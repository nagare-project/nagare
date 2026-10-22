package player

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nagare-project/nagare/internal/mpv/mpvtest"
)

// 播到尾自动接下一集：界面 ready 后开 keep-open；到尾出倒计时卡片，到点在同一个窗口里 loadfile；
// 第一集记为看完、回写恰好一次，OnSessionEnd 一次都不调。第二集是最后一集：keep-open 关回去，
// 播完照常关窗，OnSessionEnd 恰好一次。
func TestSwitchAutoNextAtEOF(t *testing.T) {
	client := newNavClient()
	h := newNavHarness(t, client, 2)
	replyLoadfile(h.fake, switchEvents()...)
	ep1, ep2 := h.eps[0], h.eps[1]
	res, pb := h.startPlaying(t, 0)

	eventually(t, func() bool { return count(h.fake, "set_property", "keep-open", "yes") == 1 }, "有下一集、界面就绪：要开 keep-open")
	h.awaitDanmakuOK(t, ep1.FileID)
	reachEnd(t, h.fake, 1439, 1440)

	eventually(t, func() bool { return count(h.fake, "loadfile") == 1 }, "倒计时到点要切到下一集")
	assert.Equal(t, []upNextCard{{Phase: "countdown", Label: "第 2 集", Seconds: 1}}, upNextCards(t, h.fake))
	assert.Equal(t, []any{"loadfile", ep2.AbsPath, "replace"}, h.fake.CommandsWith("loadfile")[0])
	h.awaitEpisode(t, ep2.FileID)
	assert.Same(t, pb, h.m.currentPlayback(), "还是同一个窗口")
	assert.Equal(t, res.PlaybackID, pb.id)

	eventually(t, func() bool {
		p, ok := h.st.Progress(ep1.FileID)
		return ok && p.Completed && p.Synced
	}, "第一集要记为看完并回写")
	assert.Equal(t, []int{1}, client.marks(), "第一集回写恰好一次")
	assert.Equal(t, int32(0), h.ends.Load(), "窗口里换集不调 OnSessionEnd")
	eventually(t, func() bool { return count(h.fake, "set_property", "keep-open", "no") == 1 }, "最后一集要把 keep-open 关回去")
	assert.Equal(t, 1, count(h.fake, "script-message-to", "nagare", "upnext-clear"), "切过去之后收起卡片")
	assert.Equal(t, 1, count(h.fake, "set_property", "pause", false), "停在最后一帧的暂停要放开")

	h.awaitDanmakuOK(t, ep2.FileID)
	pushEOF(t, h.fake, 1439, 1440)
	awaitWindowEnd(t, pb)

	p, ok := h.st.Progress(ep2.FileID)
	require.True(t, ok)
	assert.True(t, p.Completed)
	assert.Equal(t, []int{1, 2}, client.marks())
	assert.Equal(t, int32(1), h.ends.Load(), "OnSessionEnd 每个窗口恰好一次")
	assert.False(t, h.m.Status().Playing)
	assert.Nil(t, h.m.Status().PlaybackFailure)
	assert.Equal(t, 1, count(h.fake, "set_property", "keep-open", "yes"))
}

// 卡片上点取消：不切、收起卡片、窗口留着；拖回去（eof-reached 落下）再播到尾，卡片再出一次。
func TestSwitchUpNextCancel(t *testing.T) {
	h := newNavHarness(t, nil, 2)
	// 倒计时要长到来得及点取消（在起播之前改：之后事件循环会读它）
	upNextDelay = 2 * time.Second
	replyLoadfile(h.fake, switchEvents()...)
	_, pb := h.startPlaying(t, 0)
	eventually(t, func() bool { return count(h.fake, "set_property", "keep-open", "yes") == 1 }, "要开 keep-open")

	reachEnd(t, h.fake, 1439, 1440)
	eventually(t, func() bool { return len(upNextCards(t, h.fake)) == 1 }, "到尾要出卡片")
	uiMessage(t, h.fake, "upnext-cancel")
	eventually(t, func() bool { return count(h.fake, "script-message-to", "nagare", "upnext-clear") == 1 }, "取消要收起卡片")
	time.Sleep(200 * time.Millisecond)
	assert.Zero(t, count(h.fake, "loadfile"), "取消之后不切")
	assert.True(t, h.m.Status().Playing, "窗口留着")
	select {
	case <-pb.finished:
		t.Fatal("取消之后窗口不该关")
	default:
	}

	h.fake.PushProperty(t, "eof-reached", false)
	h.fake.PushProperty(t, "time-pos", 1200.0)
	h.fake.PushProperty(t, "eof-reached", true)
	eventually(t, func() bool { return len(upNextCards(t, h.fake)) == 2 }, "拖回去再播到尾，卡片再出一次")
}

// 拖回去（eof-reached 落下）时倒计时悄悄取消。
func TestSwitchSeekBackCancelsCountdown(t *testing.T) {
	h := newNavHarness(t, nil, 2)
	upNextDelay = 300 * time.Millisecond // 在起播之前改：之后事件循环会读它
	replyLoadfile(h.fake, switchEvents()...)
	h.startPlaying(t, 0)
	eventually(t, func() bool { return count(h.fake, "set_property", "keep-open", "yes") == 1 }, "要开 keep-open")

	reachEnd(t, h.fake, 1439, 1440)
	eventually(t, func() bool { return len(upNextCards(t, h.fake)) == 1 }, "到尾要出卡片")
	h.fake.PushProperty(t, "eof-reached", false)
	eventually(t, func() bool { return count(h.fake, "script-message-to", "nagare", "upnext-clear") == 1 }, "拖回去要收起卡片")
	time.Sleep(400 * time.Millisecond)
	assert.Zero(t, count(h.fake, "loadfile"))
}

// 看到一半按 ⏭：第一集记 300 秒、不算看完；上一个文件的 end-file(stop) 不会把第二集当成播完了。
func TestSwitchMidEpisodeNext(t *testing.T) {
	client := newNavClient()
	h := newNavHarness(t, client, 2)
	replyLoadfile(h.fake, switchEvents()...)
	ep1, ep2 := h.eps[0], h.eps[1]
	_, pb := h.startPlaying(t, 0)
	h.fake.PushProperty(t, "duration", 1440.0)
	h.fake.PushProperty(t, "time-pos", 300.0)
	eventually(t, func() bool { return pb.player.State().TimePos == 300 }, "位置没到")

	uiMessage(t, h.fake, "next")
	h.awaitEpisode(t, ep2.FileID)
	p, ok := h.st.Progress(ep1.FileID)
	require.True(t, ok)
	assert.Equal(t, 300.0, p.PositionSec)
	assert.False(t, p.Completed)

	h.fake.PushProperty(t, "duration", 1440.0)
	h.fake.PushProperty(t, "time-pos", 100.0)
	eventually(t, func() bool { return pb.player.State().TimePos == 100 }, "第二集的位置没到")
	assert.True(t, h.m.Status().Playing, "上一个文件的 end-file(stop) 不能结束窗口")
	h.fake.Quit(t)
	awaitWindowEnd(t, pb)

	p, ok = h.st.Progress(ep2.FileID)
	require.True(t, ok)
	assert.Equal(t, 100.0, p.PositionSec)
	assert.False(t, p.Completed)
	assert.Nil(t, h.m.Status().PlaybackFailure, "旧文件的 stop 不是第二集的播放失败")
	assert.Empty(t, client.marks(), "两集都没看完")
	assert.Equal(t, int32(1), h.ends.Load())
}

// 栅栏期间（新文件还没开始）推来的旧位置不能落到第二集的进度上。
func TestSwitchStaleTimePosInsideFence(t *testing.T) {
	h := newNavHarness(t, nil, 2)
	// 只推旧文件的结束和一条旧位置，新文件的 start-file 晚点再来
	replyLoadfile(h.fake,
		map[string]any{"event": "end-file", "reason": "stop"},
		map[string]any{"event": "property-change", "name": "time-pos", "data": 1430.0},
	)
	ep2 := h.eps[1]
	_, pb := h.startPlaying(t, 0)
	h.fake.PushProperty(t, "duration", 1440.0)
	h.fake.PushProperty(t, "time-pos", 600.0)
	eventually(t, func() bool { return pb.player.State().TimePos == 600 }, "位置没到")

	uiMessage(t, h.fake, "next")
	h.awaitEpisode(t, ep2.FileID)
	eventually(t, func() bool { return pb.player.State().TimePos == 1430 }, "旧位置还没到")
	time.Sleep(50 * time.Millisecond) // 让事件循环处理完它
	_, ok := h.st.Progress(ep2.FileID)
	assert.False(t, ok, "栅栏里的旧位置不能写成第二集的进度")
	st := h.m.Status()
	assert.Equal(t, ep2.FileID, st.FileID)
	assert.Zero(t, st.Position, "栅栏里 Status 不能把上一个文件的位置安到第二集头上")
	assert.Zero(t, st.Duration)

	h.fake.Push(t, map[string]any{"event": "start-file"})
	h.fake.Push(t, map[string]any{"event": "file-loaded"})
	eventually(t, func() bool { return count(h.fake, "set_property", "pause", false) == 1 }, "栅栏落下")
	h.fake.Quit(t)
	awaitWindowEnd(t, pb)
	_, ok = h.st.Progress(ep2.FileID)
	assert.False(t, ok, "第二集一秒没播，不该有进度")
}

// 刚发出 loadfile、新文件还没开始 mpv 就没了：第二集不写进度，第一集照常写；
// 失败记在第二集上、带着这个窗口的 playbackId；OnSessionEnd 恰好一次。
func TestSwitchConnectionDiesWhileFenced(t *testing.T) {
	h := newNavHarness(t, nil, 2)
	h.fake.OnCommand("loadfile", func([]any) *mpvtest.Reply { return &mpvtest.Reply{Close: true} })
	ep1, ep2 := h.eps[0], h.eps[1]
	res, pb := h.startPlaying(t, 0)
	h.fake.PushProperty(t, "duration", 1440.0)
	h.fake.PushProperty(t, "time-pos", 300.0)
	eventually(t, func() bool { return pb.player.State().TimePos == 300 }, "位置没到")

	uiMessage(t, h.fake, "next")
	awaitWindowEnd(t, pb)

	_, ok := h.st.Progress(ep2.FileID)
	assert.False(t, ok, "第二集没开始，不能写进度（更不能写成第一集的 300 秒）")
	p, ok := h.st.Progress(ep1.FileID)
	require.True(t, ok)
	assert.Equal(t, 300.0, p.PositionSec)
	assert.False(t, p.Completed)
	failure := h.m.Status().PlaybackFailure
	require.NotNil(t, failure)
	assert.Equal(t, ep2.FileID, failure.FileID)
	assert.Equal(t, res.PlaybackID, failure.PlaybackID)
	assert.Equal(t, int32(1), h.ends.Load())
}
