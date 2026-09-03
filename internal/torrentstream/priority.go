// 分片优先级窗口。
//
// 顺序下载对「边下边播」是不够的：一拖进度条就会卡在没下到的位置，而 MKV 的
// seek 索引（Cues）又恰好在文件尾。这里维护一个跟着读位置滑动的四档窗口，
// 并在启动初期额外钉住头尾两段。
//
// 窗口常量与三处修正参照 seanime（GPL-3.0）的 torrentutil 实现。
package torrentstream

import (
	"sync"
	"time"

	"github.com/anacrolix/torrent"

	"github.com/nagare-project/nagare/internal/library"
)

const (
	// 四档窗口的宽度（分片数）。
	piecesForNow        = 5
	piecesForHighBefore = 2
	piecesForNext       = 30
	piecesForReadahead  = 30

	// updateStride：读位置每移动这么多字节才重算一次优先级。
	// 每次 Read 都重算会把 SetPriority 变成热路径上的锁竞争。
	updateStride = 512 * 1024

	// subtitleClusterBack：窗口起点先往回退这么多。
	// 修正 1 —— MKV 里字幕簇排在对应视频数据【之前】，不退这一段，
	// 一拖进度条字幕就会消失。
	subtitleClusterBack = 1 * 1024 * 1024

	// startupWindow：启动后多久内额外钉住头尾。
	startupWindow = 60 * time.Second

	// startupTailBytes：钉住的尾部长度。
	// 修正 2 的后半 —— 尾部是 MKV 的 Cues（seek 索引），顺序下载永远拿不到，
	// 没有它进度条就是死的。
	startupTailBytes = 4 * 1024 * 1024
)

// startupHeadBytes 是钉住的头部长度，直接取 library.Hash16MBytes。
// 修正 2 的前半 —— 头部既是 MKV 头，也是弹幕匹配哈希的取样区间；
// 取样长度只能有一个来源，两边各写死一个 16MB 必然漂移。
const startupHeadBytes = library.Hash16MBytes

// windowInput 是窗口计算的全部输入。
//
// 计算层与应用层分开：这样窗口逻辑是个纯函数，表驱动测试完全不必构造真实种子。
type windowInput struct {
	pieceLength int64
	fileOffset  int64 // 文件在种子里的起始偏移
	fileLength  int64
	firstPiece  int
	lastPiece   int     // 含
	positions   []int64 // 各 reader 的文件内读位置
	startup     bool    // 是否仍在启动 60 秒窗口内
}

// pieceAt 把文件内偏移换算成分片下标。
func (in windowInput) pieceAt(offset int64) int {
	return int((in.fileOffset + offset) / in.pieceLength)
}

// computeWindow 算出这一轮应当抬高优先级的分片。
//
// 多个 reader 取并集、同一分片取最高优先级：计算弹幕哈希的 reader 与 mpv 的
// reader 是并发存在的两个 reader，各管各的会互相把对方的分片降下去。
func computeWindow(in windowInput) map[int]torrent.PiecePriority {
	out := make(map[int]torrent.PiecePriority)
	if in.pieceLength <= 0 || in.fileLength <= 0 || in.lastPiece < in.firstPiece {
		return out
	}
	raise := func(idx int, prio torrent.PiecePriority) {
		if idx < in.firstPiece || idx > in.lastPiece {
			return
		}
		if cur, ok := out[idx]; !ok || prio > cur {
			out[idx] = prio
		}
	}
	raiseRange := func(begin, end int, prio torrent.PiecePriority) {
		for i := begin; i < end; i++ {
			raise(i, prio)
		}
	}

	for _, pos := range in.positions {
		i := in.pieceAt(clampOffset(pos-subtitleClusterBack, in.fileLength))
		raiseRange(i-piecesForHighBefore, i, torrent.PiecePriorityHigh)
		raiseRange(i, i+piecesForNow, torrent.PiecePriorityNow)
		next := i + piecesForNow
		raiseRange(next, next+piecesForNext, torrent.PiecePriorityNext)
		ahead := next + piecesForNext
		raiseRange(ahead, ahead+piecesForReadahead, torrent.PiecePriorityReadahead)
	}

	if in.startup {
		// 头部给 Now：起播缓冲、MKV 头、弹幕哈希取样都落在这一段，它就是
		// 当前真正被读的位置。
		headEnd := in.pieceAt(clampOffset(min64(startupHeadBytes, in.fileLength)-1, in.fileLength))
		raiseRange(in.firstPiece, headEnd+1, torrent.PiecePriorityNow)
		// 尾部给 Next（低于 Now）：Cues 要在 mpv 第一次 seek 前到位，
		// 但不能和起播缓冲抢带宽 —— 抢赢了就变成「进度条能拖，但迟迟不起播」。
		tailBegin := in.pieceAt(clampOffset(in.fileLength-startupTailBytes, in.fileLength))
		raiseRange(tailBegin, in.lastPiece+1, torrent.PiecePriorityNext)
	}
	return out
}

// clampOffset 把偏移夹进 [0, length-1]（length 为 0 时返回 0）。
func clampOffset(off, length int64) int64 {
	if off < 0 {
		return 0
	}
	if length > 0 && off > length-1 {
		return length - 1
	}
	return off
}

func min64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}

// prioritySetter 是应用层：真的去改一个分片的优先级。
// 抽成函数字段是为了让管理器也能脱离真实种子测试。
type prioritySetter func(idx int, prio torrent.PiecePriority)

// readerSlot 记一个 reader 的读位置，以及上一次触发重算时的位置。
type readerSlot struct {
	pos       int64
	lastApply int64
	primed    bool
}

// priorityManager 按 torrent + file 共享一套窗口，多个 reader 取并集。
type priorityManager struct {
	mu   sync.Mutex
	base windowInput // 静态部分（positions / startup 每次重算时填）
	set  prioritySetter
	now  func() time.Time

	createdAt   time.Time
	readers     map[int]*readerSlot
	nextID      int
	applied     map[int]torrent.PiecePriority
	lastStartup bool
}

// newPriorityManager 建立管理器。now 可注入，便于测试启动期的 60 秒边界。
func newPriorityManager(base windowInput, set prioritySetter, now func() time.Time) *priorityManager {
	if now == nil {
		now = time.Now
	}
	pm := &priorityManager{
		base:    base,
		set:     set,
		now:     now,
		readers: make(map[int]*readerSlot),
		applied: make(map[int]torrent.PiecePriority),
	}
	pm.createdAt = now()
	pm.lastStartup = true
	return pm
}

// addReader 登记一个 reader 及其初始读位置，返回句柄。
func (pm *priorityManager) addReader(pos int64) int {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	pm.nextID++
	id := pm.nextID
	pm.readers[id] = &readerSlot{pos: pos}
	pm.apply()
	return id
}

// removeReader 注销 reader 并立刻重算：它贡献的那段窗口应当马上让出来。
func (pm *priorityManager) removeReader(id int) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	if _, ok := pm.readers[id]; !ok {
		return
	}
	delete(pm.readers, id)
	pm.apply()
}

// report 上报某个 reader 的最新读位置；位移不足 updateStride 时不重算。
func (pm *priorityManager) report(id int, pos int64) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	slot, ok := pm.readers[id]
	if !ok {
		return
	}
	slot.pos = pos
	if !pm.shouldRecompute(slot) {
		return
	}
	pm.apply()
}

// shouldRecompute 判断是否值得重算：首次、跨过 stride、或启动期刚结束。
func (pm *priorityManager) shouldRecompute(slot *readerSlot) bool {
	if !slot.primed {
		return true
	}
	if pm.inStartup() != pm.lastStartup {
		return true
	}
	delta := slot.pos - slot.lastApply
	if delta < 0 {
		delta = -delta
	}
	return delta >= updateStride
}

func (pm *priorityManager) inStartup() bool {
	return pm.now().Sub(pm.createdAt) < startupWindow
}

// apply 重算窗口并把差量写下去。调用方必须持有 pm.mu。
func (pm *priorityManager) apply() {
	in := pm.base
	in.startup = pm.inStartup()
	in.positions = make([]int64, 0, len(pm.readers))
	for _, slot := range pm.readers {
		in.positions = append(in.positions, slot.pos)
	}
	want := computeWindow(in)

	for idx, prio := range want {
		if cur, ok := pm.applied[idx]; !ok || cur != prio {
			pm.set(idx, prio)
		}
	}
	// 离开窗口的分片降回 Normal（不是 None）：它仍属于用户选中的这个文件，
	// 只是不再紧急。降成 None 会把已经排好的请求整段作废。
	for idx := range pm.applied {
		if _, ok := want[idx]; !ok {
			pm.set(idx, torrent.PiecePriorityNormal)
		}
	}

	pm.applied = want
	pm.lastStartup = in.startup
	for _, slot := range pm.readers {
		slot.lastApply = slot.pos
		slot.primed = true
	}
}
