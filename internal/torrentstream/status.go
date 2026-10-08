// 状态快照与实时采样：界面按它显示分阶段状态条、peer 数、速度与缓冲进度
// （决议 M3-8）。磁力无法像本地文件那样立即起播，不确定的等待必须有可观测的
// 进展，否则用户无法区分「在下载」与「卡死」。
package torrentstream

import (
	"time"

	"github.com/anacrolix/torrent"
)

// Status 是引擎的实时状态快照，界面按它画状态条。
type Status struct {
	Active     bool    `json:"active"`
	Phase      string  `json:"phase"`
	Name       string  `json:"name,omitempty"`
	FileName   string  `json:"fileName,omitempty"`
	Infohash   string  `json:"infohash,omitempty"`
	Peers      int     `json:"peers"`
	Seeders    int     `json:"seeders"`
	DownRate   int64   `json:"downRate"`
	UpRate     int64   `json:"upRate"`
	Buffered   float64 `json:"buffered"`
	Progress   float64 `json:"progress"`
	CacheBytes int64   `json:"cacheBytes"`
	Seeding    bool    `json:"seeding"`
	// Error 是本次会话已经发生、但不体现在 Prepare 返回值里的失败（目前只有
	// 播放开始【之后】的分片写盘失败）。空串表示一切正常。
	// 不放进 error 通道是因为这类失败发生时没有任何请求在等着接它 ——
	// 界面靠轮询状态才看得见，否则用户只会看到「进度不动了」。
	Error string `json:"error,omitempty"`
}

// Status 返回实时状态快照。
func (e *Engine) Status() Status {
	e.mu.Lock()
	sess := e.sess
	seeding := e.cfg.Seeding
	e.mu.Unlock()

	st := Status{Phase: PhaseIdle, Seeding: seeding, CacheBytes: e.CacheBytes()}
	if sess == nil {
		return st
	}
	sess.refresh()
	sess.fill(&st)
	return st
}

// refresh 采样 swarm 状态与缓冲进度。等待循环与 Status 共用。
func (s *session) refresh() {
	s.mu.Lock()
	tor, file := s.tor, s.file
	s.mu.Unlock()
	if tor == nil {
		return
	}

	// t.Stats() 会拿 client 锁，不能在持有 s.mu 时调用。
	stats := tor.Stats()
	now := time.Now()
	read := stats.BytesReadData.Int64()
	written := stats.BytesWrittenData.Int64()
	var buffered, progress float64
	var open bool
	if file != nil {
		buffered, open = gateProgress(tor, file)
		progress = fileProgress(file)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.peers = stats.ActivePeers
	s.seeders = stats.ConnectedSeeders
	s.buffered, s.gateOpen, s.progress = buffered, open, progress
	if s.sampleAt.IsZero() {
		s.sampleAt, s.lastRead, s.lastWrite = now, read, written
		return
	}
	if dt := now.Sub(s.sampleAt).Seconds(); dt >= minSampleSeconds {
		s.downRate = int64(float64(read-s.lastRead) / dt)
		s.upRate = int64(float64(written-s.lastWrite) / dt)
		s.sampleAt, s.lastRead, s.lastWrite = now, read, written
	}
}

// refreshGate 只复查起播门槛（缓冲期的快速轮询用）：不取种子统计，不算整集进度。
func (s *session) refreshGate() {
	s.mu.Lock()
	tor, file := s.tor, s.file
	s.mu.Unlock()
	if tor == nil || file == nil {
		return
	}
	buffered, open := gateProgress(tor, file)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.buffered, s.gateOpen = buffered, open
}

// gateReady 报告最近一次 refresh 时起播门槛是否已经放行。
func (s *session) gateReady() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.gateOpen
}

// fill 把会话状态填进快照。
func (s *session) fill(st *Status) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st.Active = !s.closed
	st.Phase = s.phase
	st.Name = s.name
	st.FileName = s.item.FileName
	st.Infohash = s.infohash
	st.Peers, st.Seeders = s.peers, s.seeders
	st.DownRate, st.UpRate = s.downRate, s.upRate
	st.Buffered, st.Progress = s.buffered, s.progress
	if s.writeErr != nil {
		// 播放已经开始之后才发生的写盘失败没有请求在等着接它，只能从状态里透出来。
		st.Error = "磁盘写入失败，已停止下载。确认磁盘还有空间、缓存目录可写，然后重新播放"
	}
}

// pendingGateCap 是门槛还没放行时界面进度的上限：按块估出来的进度可能先到 100%
// （块都到了、分片还在校验或还差别处的块），显示 100% 却不起播会被当成卡死。
const pendingGateCap = 0.99

// gateProgress 返回起播门槛（文件头 bufferStartBytes）的进度。
//
// 两个返回值刻意分开：
//   - open 按【整分片】判定：门槛覆盖的每一片都没有缺块才放行。SetResponsive 让读
//     可以在分片校验前返回，但「够不够起播」必须保守，否则 mpv 起来后立刻就卡住。
//   - fraction 给界面看，把未完成分片里已经到手的块也按比例算进去。只数整片的话，
//     16MB 分片的合集要下满第一片进度条才会动一下，等待的几十秒里一直显示 0%，
//     看起来和卡死一样。anacrolix 只公开每片缺多少字节、不公开缺的是哪几块，
//     所以按比例摊到门槛内的那一段上 —— 这是估计，只用于显示。
func gateProgress(tor *torrent.Torrent, file *torrent.File) (fraction float64, open bool) {
	target := min(bufferStartBytes, file.Length())
	info := tor.Info()
	if target <= 0 {
		return 1, true
	}
	if info == nil || info.PieceLength <= 0 {
		return 0, false
	}
	pieceLen := info.PieceLength
	begin := file.Offset()
	end := begin + target
	total := tor.NumPieces()
	var whole, partial int64
	for idx := int(begin / pieceLen); idx <= int((end-1)/pieceLen) && idx < total; idx++ {
		lo, hi := max(int64(idx)*pieceLen, begin), min(int64(idx+1)*pieceLen, end)
		missing := tor.PieceBytesMissing(idx)
		if missing == 0 {
			whole += hi - lo
			continue
		}
		if size := info.Piece(idx).Length(); size > 0 && missing < size {
			partial += (hi - lo) * (size - missing) / size
		}
	}
	if whole >= target {
		return 1, true
	}
	return min(float64(whole+partial)/float64(target), pendingGateCap), false
}

func fileProgress(file *torrent.File) float64 {
	if file.Length() <= 0 {
		return 0
	}
	return float64(file.BytesCompleted()) / float64(file.Length())
}
