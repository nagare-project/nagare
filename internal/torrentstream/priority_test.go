package torrentstream

import (
	"fmt"
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

// baseInput 造一份 1MB 分片、100MB 文件的输入；positions 是各个读到文件尾的 reader。
func baseInput(positions []int64, startup bool) windowInput {
	readers := make([]readerSpan, 0, len(positions))
	for _, pos := range positions {
		readers = append(readers, readerSpan{pos: pos})
	}
	return windowInput{
		pieceLength: testPieceLen,
		fileOffset:  0,
		fileLength:  testFileLen,
		firstPiece:  0,
		lastPiece:   testLastPiece,
		readers:     readers,
		startup:     startup,
	}
}

// fileInput 造一份指定分片大小、文件偏移与长度的输入（文件之外的分片不归它管）。
func fileInput(pieceLen, fileOffset, fileLen int64) windowInput {
	return windowInput{
		pieceLength: pieceLen,
		fileOffset:  fileOffset,
		fileLength:  fileLen,
		firstPiece:  int(fileOffset / pieceLen),
		lastPiece:   int((fileOffset + fileLen - 1) / pieceLen),
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

// 启动期钉住的分片，档位各有理由：
//   - 起播门槛是【唯一】的 Now：Now 档内 anacrolix 按随机分片顺序请求，门槛又要等其中
//     每一片都到齐 —— 和别的分片平级，门槛就得等那些分片一起下完；
//   - 缓冲期（门槛放行之前）【只有】门槛可请求：别的分片哪怕档位低，也会被填进与门槛
//     最后几块同一批的请求里，拖慢门槛；
//   - 放行之后，弹幕哈希头部余下的部分低于门槛，而且必须是 Readahead：那是 anacrolix
//     唯一按文件顺序请求的档，正是 mpv 起播后紧接着要读的那段；
//   - 放行之后，尾部（Cues）是 Next：高于头部余下部分，低于门槛。
func TestComputeWindowStartupPins(t *testing.T) {
	tests := []struct {
		name       string
		pieceLen   int64
		fileOffset int64
		fileLen    int64
	}{
		{"256KB 分片", 256 << 10, 0, 1 << 30},
		{"1MB 分片", 1 << 20, 0, 1 << 30},
		{"1MB 分片、文件从分片中间开始（合集）", 1 << 20, 3<<20 + 123, 1 << 30},
		{"4MB 分片", 4 << 20, 0, 1400 << 20},
		{"8MB 分片", 8 << 20, 0, 1400 << 20},
		{"16MB 分片", 16 << 20, 0, 1400 << 20},
		{"16MB 分片、文件从分片中间开始（合集）", 16 << 20, 7 << 20, 1400<<20 + 5},
		{"比门槛还小的文件", 1 << 20, 0, 5<<20 + 7},
	}
	for _, tc := range tests {
		in := fileInput(tc.pieceLen, tc.fileOffset, tc.fileLen)
		gateLast := in.pieceAt(min(bufferStartBytes, tc.fileLen) - 1)
		headLast := in.pieceAt(min(startupHeadBytes, tc.fileLen) - 1)
		tailFirst := in.pieceAt(max(tc.fileLen-startupTailBytes, 0))

		t.Run(tc.name+"/缓冲中", func(t *testing.T) {
			in.startup, in.buffering = true, true
			got := computeWindow(in)
			for idx := in.firstPiece; idx <= in.lastPiece; idx++ {
				prio, raised := got[idx]
				if idx <= gateLast {
					assert.Equal(t, torrent.PiecePriorityNow, prio, "门槛内第 %d 片必须是 Now", idx)
					continue
				}
				assert.False(t, raised, "缓冲中只有门槛可请求，第 %d 片不该被抬高（实际 %v）", idx, prio)
			}
		})

		t.Run(tc.name+"/放行后", func(t *testing.T) {
			in.startup, in.buffering = true, false
			got := computeWindow(in)
			for idx := in.firstPiece; idx <= in.lastPiece; idx++ {
				prio, raised := got[idx]
				switch {
				case idx <= gateLast:
					assert.Equal(t, torrent.PiecePriorityNow, prio, "门槛内第 %d 片必须是 Now", idx)
				case idx >= tailFirst:
					assert.Equal(t, torrent.PiecePriorityNext, prio, "尾部第 %d 片必须是 Next", idx)
				case idx <= headLast:
					assert.Equal(t, torrent.PiecePriorityReadahead, prio,
						"头部余下的第 %d 片要低于门槛、且按文件顺序（Readahead）", idx)
				default:
					assert.False(t, raised, "第 %d 片不在任何钉住的区间里，不该被抬高（实际 %v）", idx, prio)
				}
			}
			assert.Greater(t, torrent.PiecePriorityNext, torrent.PiecePriorityReadahead,
				"尾部必须排在头部余下部分之前")
		})
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

// 窗口按字节量，分片再大每档也至少占一片：原先按片数计，16MB 分片下 Now 一档就是
// 80MB，比 anacrolix 的未校验预算还大，Next / Readahead 永远轮不到。
func TestComputeWindowBandsScaleWithPieceSize(t *testing.T) {
	const pos int64 = 700 << 20 // 窗口起点 699MB
	tests := []struct {
		pieceLen  int64
		wantNow   int // 片数
		wantNext  int
		wantAhead int
	}{
		{256 << 10, 20, 120, 120},
		{1 << 20, 5, 30, 30},
		{8 << 20, 1, 4, 4},
		{16 << 20, 1, 2, 2},
	}
	for _, tc := range tests {
		t.Run(fmt.Sprintf("%dKB 分片", tc.pieceLen>>10), func(t *testing.T) {
			got := computeWindow(windowInput{
				pieceLength: tc.pieceLen, fileLength: 4 << 30,
				firstPiece: 0, lastPiece: int((4<<30)/tc.pieceLen) - 1,
				readers: []readerSpan{{pos: pos}},
			})
			counts := map[torrent.PiecePriority]int{}
			for _, prio := range got {
				counts[prio]++
			}
			assert.Equal(t, tc.wantNow, counts[torrent.PiecePriorityNow], "Now")
			assert.Equal(t, tc.wantNext, counts[torrent.PiecePriorityNext], "Next")
			assert.Equal(t, tc.wantAhead, counts[torrent.PiecePriorityReadahead], "Readahead")
			// 读位置所在的分片永远在 Now 里。
			assert.Equal(t, torrent.PiecePriorityNow, got[int(pos/tc.pieceLen)])
		})
	}
}

// 弹幕哈希只读前 16MB：它的窗口与预读都不该越过 16MB，否则续播时会拿用不上的字节
// 和 mpv 读位置上的窗口平级抢带宽。
func TestComputeWindowBoundedReaderStopsAtItsEnd(t *testing.T) {
	for _, pieceLen := range []int64{256 << 10, 1 << 20, 16 << 20} {
		t.Run(fmt.Sprintf("%dKB 分片", pieceLen>>10), func(t *testing.T) {
			in := fileInput(pieceLen, 0, 1400<<20)
			headLast := in.pieceAt(startupHeadBytes - 1)
			for _, pos := range []int64{0, 6 << 20, startupHeadBytes - 1} {
				in.readers = []readerSpan{{pos: pos, end: startupHeadBytes}}
				got := computeWindow(in)
				require.NotEmpty(t, got)
				for idx := range got {
					assert.LessOrEqual(t, idx, headLast, "读位置 %d：第 %d 片在 16MB 之外", pos, idx)
				}
			}
		})
	}
}

// 抬高的分片总量必须装得进 anacrolix 的未校验预算：预算按档位从高到低累计，装不下的
// 低档分片根本不会被请求 —— 窗口画得再好，超出预算的那部分等于没画。
func TestComputeWindowStaysWithinUnverifiedBudget(t *testing.T) {
	const fileLen int64 = 1400 << 20 // 一集 BDRip 的量级
	hash := func(pos int64) readerSpan { return readerSpan{pos: pos, end: startupHeadBytes} }
	type scenario struct {
		name               string
		readers            []readerSpan
		startup, buffering bool
	}
	scenarios := []scenario{
		{"起播缓冲中（还没有 reader）", nil, true, true},
		{"刚起播：mpv 与弹幕哈希都从头读", []readerSpan{{pos: 0}, hash(0)}, true, false},
		{"正常播放", []readerSpan{{pos: 700 << 20}}, false, false},
	}
	// 续播：mpv 在中段、弹幕哈希在头部。读位置逐 MB 扫过一个 16MB 周期，
	// 覆盖窗口起点相对分片边界的每一种对齐（最坏的对齐会多占一片）。
	for k := int64(0); k < 16; k++ {
		scenarios = append(scenarios, scenario{
			fmt.Sprintf("续播（读位置 +%dMB）", k), []readerSpan{{pos: 700<<20 + k<<20}, hash(3 << 20)}, true, false,
		})
	}
	for _, pieceLen := range []int64{256 << 10, 1 << 20, 4 << 20, 8 << 20, 16 << 20} {
		for _, sc := range scenarios {
			t.Run(fmt.Sprintf("%dKB 分片/%s", pieceLen>>10, sc.name), func(t *testing.T) {
				in := fileInput(pieceLen, 0, fileLen)
				in.readers, in.startup, in.buffering = sc.readers, sc.startup, sc.buffering
				var raised int64
				for idx := range computeWindow(in) {
					raised += min(pieceLen, fileLen-int64(idx)*pieceLen)
				}
				assert.LessOrEqual(t, raised, int64(maxUnverifiedBytes),
					"抬高了 %dMB，超出未校验预算的部分不会被请求", raised>>20)
			})
		}
	}
}

func TestComputeWindowRejectsDegenerateInput(t *testing.T) {
	assert.Empty(t, computeWindow(windowInput{pieceLength: 0, fileLength: 10, lastPiece: 3}))
	assert.Empty(t, computeWindow(windowInput{pieceLength: 1 << 20, fileLength: 0, lastPiece: 3}))
	assert.Empty(t, computeWindow(windowInput{pieceLength: 1 << 20, fileLength: 10, firstPiece: 4, lastPiece: 3}))
}

// fakeClock 让启动期的边界可测。
type fakeClock struct{ at time.Time }

func (c *fakeClock) now() time.Time          { return c.at }
func (c *fakeClock) advance(d time.Duration) { c.at = c.at.Add(d) }

// setCall 是一次 SetPriority。
type setCall struct {
	idx  int
	prio torrent.PiecePriority
}

// recorder 记录被写下去的优先级与写入顺序。
type recorder struct {
	state map[int]torrent.PiecePriority
	calls []setCall
}

func newRecorder() *recorder {
	return &recorder{state: map[int]torrent.PiecePriority{}}
}

func (r *recorder) set(idx int, prio torrent.PiecePriority) {
	r.state[idx] = prio
	r.calls = append(r.calls, setCall{idx, prio})
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

	const start int64 = 4 << 20
	pm.openGate()
	id := pm.addReader(start)
	clock.advance(startupWindow + time.Second) // 跳出启动期，排除钉住的干扰
	pm.report(id, start)                       // 启动期结束触发一次重算
	// 回退 1MB 后起点 3MB = 第 48 片：Now 覆盖 [3MB, 8MB) = 第 48..127 片，
	// Next 从第 128 片起；回退容错 [1MB, 3MB) = 第 16..47 片。
	require.Equal(t, torrent.PiecePriorityNow, rec.state[48])
	require.Equal(t, torrent.PiecePriorityNow, rec.state[127])
	require.Equal(t, torrent.PiecePriorityNext, rec.state[128])
	require.Equal(t, torrent.PiecePriorityHigh, rec.state[16])

	// 位移不足 512KB：不重算，窗口原地不动。
	pm.report(id, start+100<<10)
	assert.Equal(t, torrent.PiecePriorityNext, rec.state[128], "位移不足一个 stride 不该重算")

	// 跨过 512KB：重算。起点挪到 3MB+600KB（第 57 片），Now 延伸到第 137 片，
	// 回退容错变成第 25..56 片。
	pm.report(id, start+600<<10)
	assert.Equal(t, torrent.PiecePriorityNow, rec.state[128], "跨过 stride 应重算并挪动窗口")
	assert.Equal(t, torrent.PiecePriorityNormal, rec.state[16], "旧窗口的分片降回 Normal")
}

// 起播缓冲期间还没有任何 reader（mpv 未起、弹幕哈希未读），门槛必须在管理器建好那一刻
// 就钉住；否则头 8MB 只能按默认顺序碰运气，实测要等整个文件下到四成。
// 缓冲期【只有】门槛可请求，尾部与头部余下部分在放行时才钉。
func TestPriorityManagerPinsOnlyGateWhileBuffering(t *testing.T) {
	clock := &fakeClock{at: time.Now()}
	pm, rec := newTestManager(clock)
	for idx := 0; idx <= 7; idx++ {
		require.Equal(t, torrent.PiecePriorityNow, rec.state[idx], "没有 reader 时门槛第 %d 片也该已钉住", idx)
	}
	require.Len(t, rec.state, 8, "缓冲中除了门槛什么都不钉")

	pm.openGate()
	assert.Equal(t, torrent.PiecePriorityReadahead, rec.state[8], "放行后门槛之后的头部按文件顺序钉住")
	assert.Equal(t, torrent.PiecePriorityReadahead, rec.state[15])
	assert.Equal(t, torrent.PiecePriorityNext, rec.state[testLastPiece], "放行后尾部钉住")
	assert.Equal(t, torrent.PiecePriorityNow, rec.state[0], "门槛仍是 Now")
}

// 每一次 SetPriority 都会立刻唤醒空闲的 peer 排请求：先写到的档位会被先请求。
// 所以必须高档先写、同档按文件顺序 —— map 的随机遍历顺序会让第一批请求落到尾部去。
func TestPriorityManagerWritesHigherPrioritiesFirst(t *testing.T) {
	clock := &fakeClock{at: time.Now()}
	pm, rec := newTestManager(clock)
	require.NotEmpty(t, rec.calls)
	assert.Equal(t, setCall{0, torrent.PiecePriorityNow}, rec.calls[0], "门槛的第一片最先写")
	assertWriteOrder(t, rec.calls)

	rec.calls = nil
	pm.openGate()
	require.NotEmpty(t, rec.calls)
	assert.Equal(t, torrent.PiecePriorityNext, rec.calls[0].prio, "放行时尾部先于头部余下部分写")
	assertWriteOrder(t, rec.calls)
}

// assertWriteOrder 断言写入顺序是高档在前、同档按文件顺序。
func assertWriteOrder(t *testing.T, calls []setCall) {
	t.Helper()
	for i := 1; i < len(calls); i++ {
		prev, cur := calls[i-1], calls[i]
		if prev.prio == cur.prio {
			assert.Less(t, prev.idx, cur.idx, "同档按文件顺序写")
			continue
		}
		assert.Greater(t, prev.prio, cur.prio, "第 %d 次写入的档位不该比前一次高", i)
	}
}

func TestPriorityManagerDropsPinsAfterStartupWindow(t *testing.T) {
	clock := &fakeClock{at: time.Now()}
	pm, rec := newTestManager(clock)

	// 读位置放在中段，头部只可能来自启动期钉住。
	pm.openGate()
	id := pm.addReader(50 << 20)
	require.Equal(t, torrent.PiecePriorityNow, rec.state[0], "启动期门槛应被钉住")
	require.Equal(t, torrent.PiecePriorityReadahead, rec.state[8], "启动期头部余下部分应被钉住")
	require.Equal(t, torrent.PiecePriorityNext, rec.state[99], "启动期尾部应被钉住")

	// 放行 60 秒之后：即使读位置没动，也应当因为启动期结束而重算。
	clock.advance(startupWindow + time.Second)
	pm.report(id, 50<<20)

	assert.Equal(t, torrent.PiecePriorityNormal, rec.state[0], "启动期结束后门槛降回 Normal")
	assert.Equal(t, torrent.PiecePriorityNormal, rec.state[8], "启动期结束后头部降回 Normal")
	assert.Equal(t, torrent.PiecePriorityReadahead, rec.state[99], "尾部退回普通 Readahead")
}

// 慢种子的起播缓冲可以等上两分钟。启动期从门槛放行（mpv 随即启动）算起：
// 从管理器建好算起的话，mpv 一起来钉住就已过期，尾部的 Cues 还没下完就被降回 Normal。
func TestPriorityManagerStartupCountsFromGateOpen(t *testing.T) {
	clock := &fakeClock{at: time.Now()}
	pm, rec := newTestManager(clock)

	clock.advance(10 * time.Minute) // 缓冲等得再久，门槛也一直钉着
	require.Equal(t, torrent.PiecePriorityNow, rec.state[0])
	pm.openGate()
	id := pm.addReader(50 << 20)
	require.Equal(t, torrent.PiecePriorityNext, rec.state[99], "mpv 刚开始读时尾部钉着")

	clock.advance(startupWindow - time.Second)
	pm.report(id, 50<<20)
	assert.Equal(t, torrent.PiecePriorityNext, rec.state[99], "放行不到 60 秒，尾部仍钉着")

	clock.advance(2 * time.Second)
	pm.report(id, 50<<20)
	assert.Equal(t, torrent.PiecePriorityReadahead, rec.state[99], "满 60 秒后钉住解除")
}

func TestPriorityManagerDowngradesPiecesLeavingWindow(t *testing.T) {
	clock := &fakeClock{at: time.Now()}
	pm, rec := newTestManager(clock)

	pm.openGate()
	id := pm.addReader(0)
	clock.advance(startupWindow + time.Second) // 跳出启动期，排除钉住的干扰
	pm.report(id, 0)
	require.Equal(t, torrent.PiecePriorityNow, rec.state[0])

	// 跳到文件另一端：原窗口里的分片必须降回 Normal，而不是留在 Now。
	pm.report(id, 80<<20)
	assert.Equal(t, torrent.PiecePriorityNormal, rec.state[0], "离开窗口的分片降回 Normal")
	assert.Equal(t, torrent.PiecePriorityNow, rec.state[79], "新位置进入 Now 窗口")
}

func TestPriorityManagerRemoveReaderReleasesWindow(t *testing.T) {
	clock := &fakeClock{at: time.Now()}
	pm, rec := newTestManager(clock)

	pm.openGate()
	keep := pm.addReader(0)
	temp := pm.addReader(80 << 20)
	clock.advance(startupWindow + time.Second) // 跳出启动期，排除钉住的干扰
	pm.report(keep, 0)
	require.Equal(t, torrent.PiecePriorityNow, rec.state[79])

	pm.removeReader(temp)
	assert.Equal(t, torrent.PiecePriorityNormal, rec.state[79], "reader 注销后让出它那段窗口")
	assert.Equal(t, torrent.PiecePriorityNow, rec.state[0], "另一个 reader 的窗口不受影响")
}
