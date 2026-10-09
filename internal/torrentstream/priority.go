// 分片优先级窗口。
//
// 顺序下载对「边下边播」是不够的：一拖进度条就会卡在没下到的位置，而 MKV 的
// seek 索引（Cues）又恰好在文件尾。这里维护一个跟着读位置滑动的四档窗口，
// 并在启动阶段额外钉住起播门槛、弹幕哈希的头部与文件尾。
//
// 窗口的三处修正参照 seanime（GPL-3.0）的 torrentutil 实现；宽度按字节计。
//
// 改这里之前要知道 anacrolix v1.61.0 怎么用这些档位（requesting.go 的 lessByValue、
// internal/request-strategy/order.go 的 GetRequestablePieces）：
//
//   - 档位高的永远先请求。同档之内先比稀有度，再比一个【随机】的分片顺序
//     （torrent.go onSetInfo 里的 rand.Perm）—— 唯独 Readahead 档改比分片下标，
//     也就是文件顺序。想让一段数据「按顺序下」，只能把它放在 Readahead 档。
//   - 可请求的分片按档位从高到低累计，累计到 ClientConfig.MaxUnverifiedBytes 就截止；
//     预算之外的低档分片根本不会被请求（见 client.go 的 maxUnverifiedBytes）。
//   - 档位序是 None(0) < Normal < High < Readahead < Next < Now(5)：High 比 Readahead 低。
package torrentstream

import (
	"sort"
	"sync"
	"time"

	"github.com/anacrolix/torrent"

	"github.com/nagare-project/nagare/internal/library"
)

// 读位置前后四档窗口的宽度（字节），从窗口起点（读位置回退 subtitleClusterBack 之后）量起。
//
// 数值取 seanime 在 1MB 分片下的「2 / 5 / 30 / 30 片」，那才是它实际调过的字节量。
// 原先按片数计：16MB 分片的合集里光 Now 一档就是 80MB，比 anacrolix 默认的 64MB
// 未校验预算还大 —— Next / Readahead 永远轮不到，启动期的头部钉住与之平级混在一起。
const (
	highBeforeBytes = 2 << 20
	nowBytes        = 5 << 20
	nextBytes       = 30 << 20
	readaheadBytes  = 30 << 20

	// minBandPieces：每档至少比上一档多占这么多片。分片比档宽还大时（16MB 分片对 5MB
	// 的 Now），按字节算出的终点会落进上一档已占的分片里，这一档就整个消失了。
	minBandPieces = 1
)

const (
	// updateStride：读位置每移动这么多字节才重算一次优先级。
	// 每次 Read 都重算会把 SetPriority 变成热路径上的锁竞争。
	updateStride = 512 * 1024

	// subtitleClusterBack：窗口起点先往回退这么多。
	// 修正 1 —— MKV 里字幕簇排在对应视频数据【之前】，不退这一段，
	// 一拖进度条字幕就会消失。
	subtitleClusterBack = 1 * 1024 * 1024

	// startupWindow：起播门槛放行（mpv 随即启动）之后，启动期钉住还要保留多久。
	//
	// 从放行算起而不是从管理器建好算起：慢种子的起播缓冲可以等到 bufferTimeout（两分钟）
	// —— 从建好算起的话，mpv 一起来钉住就已经过期，尾部的 Cues 还没下完就被降回 Normal，
	// 第一次拖进度条只能现等。
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

// band 是读位置之后的一档窗口。
type band struct {
	prio  torrent.PiecePriority
	bytes int64
}

// forwardBands 从窗口起点往后依次排开。
var forwardBands = [...]band{
	{torrent.PiecePriorityNow, nowBytes},
	{torrent.PiecePriorityNext, nextBytes},
	{torrent.PiecePriorityReadahead, readaheadBytes},
}

// readerSpan 是一个 reader 的读位置，以及它最多读到哪里。
//
// 弹幕哈希的 reader 只读前 16MB：照常给它画一整个 65MB 的窗口，多出来那段会在
// 续播时与 mpv 读位置上的 Next 档平级抢带宽，而那些字节它根本不会读。
type readerSpan struct {
	pos int64 // 文件内读位置
	end int64 // 不含；<= 0 表示读到文件尾
}

// windowInput 是窗口计算的全部输入。
//
// 计算层与应用层分开：这样窗口逻辑是个纯函数，表驱动测试完全不必构造真实种子。
type windowInput struct {
	pieceLength int64
	fileOffset  int64 // 文件在种子里的起始偏移
	fileLength  int64
	firstPiece  int
	lastPiece   int // 含
	readers     []readerSpan
	startup     bool // 是否仍在启动期（起播缓冲中，或放行后 startupWindow 之内）
	buffering   bool // 起播门槛还没放行（此时 startup 必为真）
}

// pieceAt 把文件内偏移换算成分片下标。
func (in windowInput) pieceAt(offset int64) int {
	return int((in.fileOffset + offset) / in.pieceLength)
}

// windowBuilder 把各段字节区间换算成分片，同一分片取最高档。
type windowBuilder struct {
	in  windowInput
	out map[int]torrent.PiecePriority
}

func (w *windowBuilder) raise(idx int, prio torrent.PiecePriority) {
	if idx < w.in.firstPiece || idx > w.in.lastPiece {
		return
	}
	if cur, ok := w.out[idx]; !ok || prio > cur {
		w.out[idx] = prio
	}
}

// raisePieces 抬高 [begin, end) 这些分片。
func (w *windowBuilder) raisePieces(begin, end int, prio torrent.PiecePriority) {
	for i := begin; i < end; i++ {
		w.raise(i, prio)
	}
}

// raiseBytes 抬高覆盖文件内 [lo, hi) 的全部分片。
func (w *windowBuilder) raiseBytes(lo, hi int64, prio torrent.PiecePriority) {
	if hi <= lo {
		return
	}
	w.raisePieces(w.in.pieceAt(lo), w.in.pieceAt(hi-1)+1, prio)
}

// addReader 画一个 reader 的四档窗口。
func (w *windowBuilder) addReader(r readerSpan) {
	length := w.in.fileLength
	limit := length
	if r.end > 0 && r.end < limit {
		limit = r.end
	}
	start := clampOffset(r.pos-subtitleClusterBack, length)
	if start >= limit {
		return // 它要读的那段已经读完
	}
	first := w.in.pieceAt(start)
	// 回退容错：起点之前 2MB，不含起点所在的分片。
	w.raisePieces(w.in.pieceAt(clampOffset(start-highBeforeBytes, length)), first, torrent.PiecePriorityHigh)

	stop := w.in.pieceAt(limit-1) + 1 // 这个 reader 用得到的最后一片之后
	cursor, off := first, start
	for _, b := range forwardBands {
		off += b.bytes
		end := max(w.in.pieceAt(clampOffset(off-1, limit))+1, cursor+minBandPieces)
		end = min(end, stop)
		w.raisePieces(cursor, end, b.prio)
		cursor = end
	}
}

// addStartupPins 钉住启动期的三段。
//
//   - 起播门槛 [0, bufferStartBytes) 给 Now，而且【只有】它是 Now：Now 档内是随机分片
//     顺序，门槛又要等其中每一片都到齐 —— 原先整个 16MB 头部都是 Now，门槛实际要等
//     头部下到九成五以上，起播慢了将近一倍。
//   - 尾部给 Next：Cues 要在第一次拖进度条（或续播的 --start）之前到位；但不能和门槛
//     平级，否则就成了「进度条能拖，迟迟不起播」。
//   - 弹幕哈希头部余下的 [门槛, 16MB) 给 Readahead：低于门槛与尾部，而且 Readahead
//     是 anacrolix 唯一按文件顺序请求的档 —— 这正是 mpv 起播后紧接着要读的那段。
//
// 缓冲期（门槛还没放行）只钉门槛：别的分片哪怕档位更低，只要可请求，就会被填进与门槛
// 最后几块同一批的请求里，拖慢门槛（详见 session.openGate）。尾部与头部余下部分在放行
// 那一刻才钉上 —— 那时它们仍然排在随后放开的整个文件（Normal）之前。
func (w *windowBuilder) addStartupPins() {
	length := w.in.fileLength
	gateEnd := min(bufferStartBytes, length)
	w.raiseBytes(0, gateEnd, torrent.PiecePriorityNow)
	if w.in.buffering {
		return
	}
	w.raiseBytes(max(length-startupTailBytes, 0), length, torrent.PiecePriorityNext)
	w.raiseBytes(gateEnd, min(startupHeadBytes, length), torrent.PiecePriorityReadahead)
}

// computeWindow 算出这一轮应当抬高优先级的分片。
//
// 多个 reader 取并集、同一分片取最高优先级：计算弹幕哈希的 reader 与 mpv 的
// reader 是并发存在的两个 reader，各管各的会互相把对方的分片降下去。
func computeWindow(in windowInput) map[int]torrent.PiecePriority {
	w := windowBuilder{in: in, out: make(map[int]torrent.PiecePriority)}
	if in.pieceLength <= 0 || in.fileLength <= 0 || in.lastPiece < in.firstPiece {
		return w.out
	}
	for _, r := range in.readers {
		w.addReader(r)
	}
	if in.startup {
		w.addStartupPins()
	}
	return w.out
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

// prioritySetter 是应用层：真的去改一个分片的优先级。
// 抽成函数字段是为了让管理器也能脱离真实种子测试。
type prioritySetter func(idx int, prio torrent.PiecePriority)

// readerSlot 记一个 reader 的读位置、读取上界，以及上一次触发重算时的位置。
type readerSlot struct {
	pos       int64
	end       int64
	lastApply int64
	primed    bool
}

// priorityManager 按 torrent + file 共享一套窗口，多个 reader 取并集。
type priorityManager struct {
	mu   sync.Mutex
	base windowInput // 静态部分（readers / startup 每次重算时填）
	set  prioritySetter
	now  func() time.Time

	// gateOpenedAt 是起播门槛放行的时刻；零值表示还在缓冲。
	gateOpenedAt time.Time
	readers      map[int]*readerSlot
	nextID       int
	applied      map[int]torrent.PiecePriority
	lastStartup  bool
}

// newPriorityManager 建立管理器。now 可注入，便于测试启动期的边界。
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
	pm.lastStartup = true
	// 建好就把缓冲期的钉住写下去，不等第一个 reader。
	// 起播缓冲那一段正是没有任何 reader 的时候（mpv 还没起、弹幕哈希还没读）；
	// 不先应用，头 8MB 就只能靠运气按默认顺序凑齐 —— 实测要等整个文件下到四成。
	pm.apply()
	return pm
}

// openGate 在起播门槛放行时调用：钉上门槛之后的头部，并从此刻开始计启动期。
// 调用方随后才把整个文件设为 Normal（见 session.openGate）。重复调用无副作用。
func (pm *priorityManager) openGate() {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	if !pm.gateOpenedAt.IsZero() {
		return
	}
	pm.gateOpenedAt = pm.now()
	pm.apply()
}

// addReader 登记一个读到文件尾的 reader（mpv 的流请求），返回句柄。
func (pm *priorityManager) addReader(pos int64) int {
	return pm.addReaderUpTo(pos, 0)
}

// addReaderUpTo 登记一个最多读到 end（不含）的 reader；end <= 0 表示读到文件尾。
func (pm *priorityManager) addReaderUpTo(pos, end int64) int {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	pm.nextID++
	id := pm.nextID
	pm.readers[id] = &readerSlot{pos: pos, end: end}
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

// inStartup：还在起播缓冲，或门槛放行还不到 startupWindow。
func (pm *priorityManager) inStartup() bool {
	return pm.gateOpenedAt.IsZero() || pm.now().Sub(pm.gateOpenedAt) < startupWindow
}

// apply 重算窗口并把差量写下去。调用方必须持有 pm.mu。
func (pm *priorityManager) apply() {
	in := pm.base
	in.startup = pm.inStartup()
	in.buffering = pm.gateOpenedAt.IsZero()
	in.readers = make([]readerSpan, 0, len(pm.readers))
	for _, slot := range pm.readers {
		in.readers = append(in.readers, readerSpan{pos: slot.pos, end: slot.end})
	}
	want := computeWindow(in)

	for _, idx := range changedByPriority(want, pm.applied) {
		pm.set(idx, want[idx])
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

// changedByPriority 列出 want 里与 applied 不同的分片：高档在前，同档按文件顺序。
//
// 写入顺序是有后果的：分片从 None 变成可请求的那一次 SetPriority 会立刻唤醒手上没有请求的
// peer 去排新请求（torrent.go updatePeerRequestsForPiece；只在 None → 非 None 时），而 map
// 的遍历顺序是随机的 —— 先写到尾部那几片，连上的 peer 第一批请求就可能全落在尾部，
// 门槛要等那一批排空才轮得到。
func changedByPriority(want, applied map[int]torrent.PiecePriority) []int {
	idxs := make([]int, 0, len(want))
	for idx, prio := range want {
		if cur, ok := applied[idx]; !ok || cur != prio {
			idxs = append(idxs, idx)
		}
	}
	sort.Slice(idxs, func(i, j int) bool {
		if pi, pj := want[idxs[i]], want[idxs[j]]; pi != pj {
			return pi > pj
		}
		return idxs[i] < idxs[j]
	})
	return idxs
}
