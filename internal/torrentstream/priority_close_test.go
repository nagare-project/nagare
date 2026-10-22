package torrentstream

import (
	"sort"
	"testing"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 合集里换到另一集时停用旧窗口：它动过的分片全部降回 None（分片自己的优先级不随 file.Cancel 清零），
// 之后无论 reader 怎么上报、注销、门槛怎么放行，都不再写任何一个分片。
func TestPriorityManagerCloseStopsAllWrites(t *testing.T) {
	clock := &fakeClock{at: time.Now()}
	pm, rec := newTestManager(clock)
	pm.openGate()
	id := pm.addReader(0)
	clock.advance(startupWindow + time.Second)
	pm.report(id, 40<<20) // 让一批分片离开窗口、降成 Normal：它们也算动过
	touched := map[int]bool{}
	for _, c := range rec.calls {
		touched[c.idx] = true
	}
	require.NotEmpty(t, touched)

	before := len(rec.calls)
	pm.close()
	resets := rec.calls[before:]
	require.Len(t, resets, len(touched), "动过的分片每片恰好降一次")
	for i, c := range resets {
		assert.Equal(t, torrent.PiecePriorityNone, c.prio)
		assert.True(t, touched[c.idx], "没动过的分片不碰：片 %d", c.idx)
		if i > 0 {
			assert.Less(t, resets[i-1].idx, c.idx, "按分片顺序降")
		}
	}
	for idx := range touched {
		assert.Equal(t, torrent.PiecePriorityNone, rec.state[idx])
	}

	closed := len(rec.calls)
	pm.report(id, 80<<20)
	other := pm.addReader(10 << 20)
	pm.removeReader(other)
	pm.openGate()
	pm.close()
	assert.Len(t, rec.calls, closed, "停用之后一个分片都不再写")
}

// 换集没成、回到这一集：窗口按 reader 停用期间上报的最新位置重画；停用期间注销的 reader 不留窗口。
func TestPriorityManagerReopenRedrawsAtLatestPosition(t *testing.T) {
	clock := &fakeClock{at: time.Now()}
	pm, rec := newTestManager(clock)
	pm.openGate()
	keep := pm.addReader(0)
	gone := pm.addReader(80 << 20)
	clock.advance(startupWindow + time.Second)
	pm.report(keep, 0)
	pm.close()

	pm.report(keep, 10<<20)
	pm.removeReader(gone)
	written := len(rec.calls)
	pm.reopen()

	require.Greater(t, len(rec.calls), written, "重新启用要把窗口写回去")
	assert.Equal(t, torrent.PiecePriorityNow, rec.state[9], "按停用期间的最新位置画（10MB 回退 1MB）")
	assert.Equal(t, torrent.PiecePriorityNone, rec.state[0], "旧位置的窗口不回来")
	assert.Equal(t, torrent.PiecePriorityNone, rec.state[85], "停用期间注销的 reader 不留窗口")

	// 重新启用之后照常工作：再停用一次，这一轮写过的分片照样全部降回去
	again := len(rec.calls)
	pm.close()
	var idxs []int
	for _, c := range rec.calls[again:] {
		assert.Equal(t, torrent.PiecePriorityNone, c.prio)
		idxs = append(idxs, c.idx)
	}
	assert.True(t, sort.IntsAreSorted(idxs))
	assert.Contains(t, idxs, 9)
	pm.reopen()
	pm.reopen() // 重复调用无副作用
	assert.Equal(t, torrent.PiecePriorityNow, rec.state[9])
}

// readerCount 在锁内数管理器上登记着的 reader。
func readerCount(pm *priorityManager) int {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	return len(pm.readers)
}

// 停用之后才登记的 reader（换集准备期间拖进度条，mpv 带着旧地址重连）：照常记账、一个分片都不写；
// 换回这一集时窗口按它的最新位置画。
func TestPriorityManagerTracksReadersAddedAfterClose(t *testing.T) {
	clock := &fakeClock{at: time.Now()}
	pm, rec := newTestManager(clock)
	pm.openGate()
	clock.advance(startupWindow + time.Second)
	pm.close()
	closed := len(rec.calls)

	late := pm.addReader(10 << 20)
	pm.report(late, 10<<20+updateStride) // 跨过 stride
	pm.report(late, 30<<20)
	head := pm.addReaderUpTo(0, 16<<20)
	pm.report(head, 4<<20)
	assert.Len(t, rec.calls, closed, "停用期间登记、上报都不写分片")
	pm.mu.Lock()
	require.Contains(t, pm.readers, late)
	assert.Equal(t, int64(30<<20), pm.readers[late].pos, "读位置照常记账")
	pm.mu.Unlock()

	pm.reopen()
	assert.Equal(t, torrent.PiecePriorityNow, rec.state[29], "按停用后才登记的 reader 的最新位置画（30MB 回退 1MB）")
	assert.Equal(t, torrent.PiecePriorityNext, rec.state[40])
	assert.Equal(t, torrent.PiecePriorityNow, rec.state[3], "有上界的 reader 也照常画")
	assert.NotContains(t, rec.state, 20, "登记时的旧位置（10MB 之后的 Next 档）不留窗口")
}

// 停用之后才登记、却一直没换回来的 reader（换集成功了；之后被换走那一集又被下一次换集替掉、或会话收掉）：
// 注销时干净放掉，从头到尾不写任何分片。
func TestPriorityManagerReleasesReadersOfClosedManager(t *testing.T) {
	clock := &fakeClock{at: time.Now()}
	pm, rec := newTestManager(clock)
	pm.openGate()
	pm.close()
	closed := len(rec.calls)

	ids := []int{pm.addReader(5 << 20), pm.addReader(50 << 20), pm.addReaderUpTo(0, 16<<20)}
	for _, id := range ids {
		pm.report(id, 60<<20)
	}
	clock.advance(startupWindow + time.Second) // 启动期在停用期间结束：也不能借机重算
	pm.report(ids[0], 70<<20)
	pm.openGate()
	pm.close()
	for _, id := range ids {
		pm.removeReader(id)
	}
	pm.removeReader(ids[0]) // 重复注销无副作用

	assert.Zero(t, readerCount(pm), "注销之后不留 reader")
	assert.Len(t, rec.calls, closed, "停用之后一个分片都不写")
}
