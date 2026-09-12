package torrentstream

import (
	"testing"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testPieceLen  int64 = 1 << 20   // 1MB 一片，便于按「片号 == MB 数」心算
	testFileLen   int64 = 100 << 20 // 100MB
	testLastPiece       = 99
)

func baseInput(positions []int64, startup bool) windowInput {
	return windowInput{
		pieceLength: testPieceLen,
		fileOffset:  0,
		fileLength:  testFileLen,
		firstPiece:  0,
		lastPiece:   testLastPiece,
		positions:   positions,
		startup:     startup,
	}
}

func TestComputeWindowFourTiers(t *testing.T) {
	// 读位置 50MB：先回退 1MB 到 49MB，落在第 49 片。
	got := computeWindow(baseInput([]int64{50 << 20}, false))

	tests := []struct {
		name  string
		piece int
		want  torrent.PiecePriority
	}{
		{"回退容错窗口的第一片", 47, torrent.PiecePriorityHigh},
		{"回退容错窗口的最后一片", 48, torrent.PiecePriorityHigh},
		{"Now 窗口起点（position-1MB 生效）", 49, torrent.PiecePriorityNow},
		{"Now 窗口终点", 53, torrent.PiecePriorityNow},
		{"Next 窗口起点", 54, torrent.PiecePriorityNext},
		{"Next 窗口终点", 83, torrent.PiecePriorityNext},
		{"Readahead 窗口起点", 84, torrent.PiecePriorityReadahead},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, got[tc.piece])
		})
	}

	// 窗口之外不出现在结果里（由调用方降回 Normal）。
	_, ok := got[46]
	assert.False(t, ok, "回退窗口之前的分片不该被抬高")
	// 若没有 position-1MB 的回退，第 50 片才会是 Now 的起点、第 49 片会是 High。
	assert.NotEqual(t, torrent.PiecePriorityHigh, got[49])
}

func TestComputeWindowClampsAtFileStart(t *testing.T) {
	// 读位置 0：回退后仍是 0，不能出现负数片号。
	got := computeWindow(baseInput([]int64{0}, false))
	for idx := range got {
		require.GreaterOrEqual(t, idx, 0, "不该出现负数片号")
		require.LessOrEqual(t, idx, testLastPiece, "不该越过文件末片")
	}
	assert.Equal(t, torrent.PiecePriorityNow, got[0])
	assert.Equal(t, torrent.PiecePriorityNow, got[4])
	assert.Equal(t, torrent.PiecePriorityNext, got[5])
}

func TestComputeWindowClampsAtFileEnd(t *testing.T) {
	// 读位置贴着文件尾：Readahead 会算到文件之外，必须夹回末片。
	got := computeWindow(baseInput([]int64{testFileLen - 1}, false))
	for idx := range got {
		require.LessOrEqual(t, idx, testLastPiece)
	}
	assert.Equal(t, torrent.PiecePriorityNow, got[testLastPiece])
}

func TestComputeWindowStartupPinsHeadAndTail(t *testing.T) {
	// 读位置在文件中段，头尾两段靠启动期钉住，与滑动窗口无关。
	got := computeWindow(baseInput([]int64{50 << 20}, true))

	// 头 16MB = 第 0..15 片，取 Now（起播缓冲、MKV 头、弹幕哈希取样都在这里）。
	for idx := 0; idx <= 15; idx++ {
		assert.Equal(t, torrent.PiecePriorityNow, got[idx], "头部第 %d 片应被钉住", idx)
	}
	assert.NotEqual(t, torrent.PiecePriorityNow, got[16], "头部只钉 16MB")

	// 尾 4MB = 第 96..99 片，取 Next：Cues 要在第一次 seek 前到位，
	// 但不能和起播缓冲抢带宽。
	for idx := 96; idx <= testLastPiece; idx++ {
		assert.Equal(t, torrent.PiecePriorityNext, got[idx], "尾部第 %d 片应被钉住", idx)
	}
}

func TestComputeWindowNoStartupPinsAfterWindow(t *testing.T) {
	got := computeWindow(baseInput([]int64{50 << 20}, false))
	for idx := 0; idx <= 15; idx++ {
		_, ok := got[idx]
		assert.False(t, ok, "启动期过后不该再钉住头部第 %d 片", idx)
	}
	// 尾部此时只可能来自 Readahead（84..99），优先级比钉住时低。
	assert.Equal(t, torrent.PiecePriorityReadahead, got[99])
}

func TestComputeWindowMultipleReadersTakeUnionMax(t *testing.T) {
	// 计算弹幕哈希的 reader 停在 0，mpv 的 reader 在 50MB。
	got := computeWindow(baseInput([]int64{0, 50 << 20}, false))

	// 各自的 Now 窗口都在。
	assert.Equal(t, torrent.PiecePriorityNow, got[0], "reader A 的 Now 窗口")
	assert.Equal(t, torrent.PiecePriorityNow, got[49], "reader B 的 Now 窗口")

	// 第 49 片同时落在 A 的 Readahead（35..64）与 B 的 Now 里，取高的那个。
	fromAOnly := computeWindow(baseInput([]int64{0}, false))
	require.Equal(t, torrent.PiecePriorityReadahead, fromAOnly[49])
	assert.Equal(t, torrent.PiecePriorityNow, got[49], "同一分片取最高优先级")

	// 第 10 片只有 A 覆盖（Next），B 不覆盖，结果应保留 A 的。
	assert.Equal(t, torrent.PiecePriorityNext, got[10])
}

func TestComputeWindowRejectsDegenerateInput(t *testing.T) {
	assert.Empty(t, computeWindow(windowInput{pieceLength: 0, fileLength: 10, lastPiece: 3}))
	assert.Empty(t, computeWindow(windowInput{pieceLength: 1 << 20, fileLength: 0, lastPiece: 3}))
	assert.Empty(t, computeWindow(windowInput{pieceLength: 1 << 20, fileLength: 10, firstPiece: 4, lastPiece: 3}))
}

// fakeClock 让启动期的 60 秒边界可测。
type fakeClock struct{ at time.Time }

func (c *fakeClock) now() time.Time          { return c.at }
func (c *fakeClock) advance(d time.Duration) { c.at = c.at.Add(d) }

// recorder 记录被写下去的优先级。
type recorder struct {
	state map[int]torrent.PiecePriority
	calls int
}

func newRecorder() *recorder {
	return &recorder{state: map[int]torrent.PiecePriority{}}
}

func (r *recorder) set(idx int, prio torrent.PiecePriority) {
	r.state[idx] = prio
	r.calls++
}

func newTestManager(clock *fakeClock) (*priorityManager, *recorder) {
	rec := newRecorder()
	pm := newPriorityManager(windowInput{
		pieceLength: testPieceLen,
		fileLength:  testFileLen,
		firstPiece:  0,
		lastPiece:   testLastPiece,
	}, rec.set, clock.now)
	return pm, rec
}

func TestPriorityManagerStrideGate(t *testing.T) {
	// 用 64KB 的小分片：这样「有没有重算」在结果里立刻看得见 ——
	// 1MB 大分片下几百 KB 的位移根本不跨片，重算与否无从分辨。
	const smallPiece int64 = 64 << 10
	clock := &fakeClock{at: time.Now()}
	rec := newRecorder()
	pm := newPriorityManager(windowInput{
		pieceLength: smallPiece,
		fileLength:  testFileLen,
		firstPiece:  0,
		lastPiece:   int(testFileLen/smallPiece) - 1,
	}, rec.set, clock.now)
	clock.advance(startupWindow + time.Second) // 跳出启动期，排除头尾钉住的干扰

	const start int64 = 4 << 20
	id := pm.addReader(start)
	pm.report(id, start) // 触发一次非启动期的重算
	// 回退 1MB 后落在第 48 片，Now 窗口是 [48, 53)。
	require.Equal(t, torrent.PiecePriorityNow, rec.state[48])
	require.Equal(t, torrent.PiecePriorityNext, rec.state[53])

	// 位移不足 512KB：不重算，窗口原地不动。
	pm.report(id, start+100<<10)
	assert.Equal(t, torrent.PiecePriorityNext, rec.state[53], "位移不足一个 stride 不该重算")

	// 跨过 512KB：重算，窗口跟着挪。
	pm.report(id, start+600<<10)
	assert.Equal(t, torrent.PiecePriorityNow, rec.state[57], "跨过 stride 应重算并挪动窗口")
	assert.Equal(t, torrent.PiecePriorityNormal, rec.state[48], "旧窗口的分片降回 Normal")
}

// 起播缓冲期间还没有任何 reader（mpv 未起、弹幕哈希未读），头尾钉住必须在
// 管理器建好那一刻就生效；否则头 8MB 只能按默认顺序碰运气，实测要等整个文件下到四成。
func TestPriorityManagerPinsHeadAndTailBeforeAnyReader(t *testing.T) {
	clock := &fakeClock{at: time.Now()}
	pm, rec := newTestManager(clock)
	_ = pm
	require.Equal(t, torrent.PiecePriorityNow, rec.state[0], "没有 reader 时头部也该已钉住")
	require.Equal(t, torrent.PiecePriorityNext, rec.state[testLastPiece], "没有 reader 时尾部也该已钉住")
}

func TestPriorityManagerDropsPinsAfterStartupWindow(t *testing.T) {
	clock := &fakeClock{at: time.Now()}
	pm, rec := newTestManager(clock)

	// 读位置放在中段，头部只可能来自启动期钉住。
	id := pm.addReader(50 << 20)
	require.Equal(t, torrent.PiecePriorityNow, rec.state[0], "启动期头部应被钉住")
	require.Equal(t, torrent.PiecePriorityNext, rec.state[99], "启动期尾部应被钉住")

	// 60 秒之后：即使读位置没动，也应当因为启动期结束而重算。
	clock.advance(startupWindow + time.Second)
	pm.report(id, 50<<20)

	assert.Equal(t, torrent.PiecePriorityNormal, rec.state[0], "启动期结束后头部降回 Normal")
	assert.Equal(t, torrent.PiecePriorityReadahead, rec.state[99], "尾部退回普通 Readahead")
}

func TestPriorityManagerDowngradesPiecesLeavingWindow(t *testing.T) {
	clock := &fakeClock{at: time.Now().Add(-2 * startupWindow)}
	pm, rec := newTestManager(clock)
	// createdAt 取自注入的时钟，构造时就已经过了启动期。
	clock.advance(2 * startupWindow)

	id := pm.addReader(0)
	pm.report(id, 0) // 触发一次非启动期的重算
	require.Equal(t, torrent.PiecePriorityNow, rec.state[0])

	// 跳到文件另一端：原窗口里的分片必须降回 Normal，而不是留在 Now。
	pm.report(id, 80<<20)
	assert.Equal(t, torrent.PiecePriorityNormal, rec.state[0], "离开窗口的分片降回 Normal")
	assert.Equal(t, torrent.PiecePriorityNow, rec.state[79], "新位置进入 Now 窗口")
}

func TestPriorityManagerRemoveReaderReleasesWindow(t *testing.T) {
	clock := &fakeClock{at: time.Now().Add(-2 * startupWindow)}
	pm, rec := newTestManager(clock)
	clock.advance(2 * startupWindow)

	keep := pm.addReader(0)
	temp := pm.addReader(80 << 20)
	pm.report(keep, 0)
	require.Equal(t, torrent.PiecePriorityNow, rec.state[79])

	pm.removeReader(temp)
	assert.Equal(t, torrent.PiecePriorityNormal, rec.state[79], "reader 注销后让出它那段窗口")
	assert.Equal(t, torrent.PiecePriorityNow, rec.state[0], "另一个 reader 的窗口不受影响")
}
