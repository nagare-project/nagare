package player

import (
	"context"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nagare-project/nagare/internal/library"
)

// navStream 是磁力合集里一集的替身：每集一个本机 HTTP 流地址（与 torrentstream.Source 同形），带文件指纹。
type navStream struct{ item library.Item }

func (s navStream) Item() library.Item { return s.item }
func (s navStream) MPVPath() string {
	return "http://127.0.0.1:8590/stream/cap/t/0123456789abcdef0123456789abcdef01234567/" + strconv.Itoa(*s.item.Episode)
}
func (s navStream) Probe(context.Context) error { return nil }
func (s navStream) Hash16M(context.Context) (string, error) {
	return "0123456789abcdef0123456789abcdef", nil
}

// 磁力合集里有下一集、在四成处就到了尾：先过「网络流提前断供」那一关 —— 关窗、记播放失败、
// 不出倒计时，也不去准备下一集。
func TestSwitchTorrentPackPrematureEOFCloses(t *testing.T) {
	h := newNavHarness(t, nil, 2)
	h.nav.wrap = func(it library.Item) MediaSource { return navStream{item: it} }
	res, pb := h.startPlaying(t, 0)
	awaitKeepOpen(t, h.fake)

	reachEnd(t, h.fake, 576, 1440)
	awaitWindowEnd(t, pb)

	assert.Empty(t, upNextCards(t, h.fake), "提前断供不出倒计时")
	assert.Zero(t, h.nav.openCount(), "不去准备下一集")
	assert.Zero(t, count(h.fake, "loadfile"))
	failure := h.m.Status().PlaybackFailure
	require.NotNil(t, failure)
	assert.Equal(t, h.eps[0].FileID, failure.FileID)
	assert.Equal(t, res.PlaybackID, failure.PlaybackID)
	p, ok := h.st.Progress(h.eps[0].FileID)
	require.True(t, ok)
	assert.False(t, p.Completed)
	assert.Equal(t, int32(1), h.ends.Load())
}

// 磁力合集播到真正的尾：同一个窗口里换到下一集的流地址，上一集记为看完、回写一次，不调 OnSessionEnd。
func TestSwitchTorrentPackAdvancesAtRealEnd(t *testing.T) {
	client := newNavClient()
	h := newNavHarness(t, client, 2)
	h.nav.wrap = func(it library.Item) MediaSource { return navStream{item: it} }
	replyLoadfile(h.fake, switchEvents()...)
	ep1, ep2 := h.eps[0], h.eps[1]
	res, pb := h.startPlaying(t, 0)
	awaitKeepOpen(t, h.fake)
	h.awaitDanmakuOK(t, ep1.FileID)

	reachEnd(t, h.fake, 1439, 1440)
	eventually(t, func() bool { return count(h.fake, "loadfile") == 1 }, "倒计时到点要切到下一集")
	assert.Equal(t, []any{"loadfile", navStream{item: ep2}.MPVPath(), "replace"}, h.fake.CommandsWith("loadfile")[0])
	h.awaitEpisode(t, ep2.FileID)
	assert.Same(t, pb, h.m.currentPlayback(), "还是同一个窗口")
	assert.Equal(t, res.PlaybackID, pb.id)
	assert.Nil(t, h.m.Status().PlaybackFailure)
	eventually(t, func() bool {
		p, ok := h.st.Progress(ep1.FileID)
		return ok && p.Completed && p.Synced
	}, "上一集要记为看完并回写")
	assert.Equal(t, []int{1}, client.marks())
	assert.Zero(t, h.ends.Load(), "窗口里换集不调 OnSessionEnd")

	h.awaitDanmakuOK(t, ep2.FileID)
	h.fake.Quit(t)
	awaitWindowEnd(t, pb)
	assert.Equal(t, int32(1), h.ends.Load())
}
