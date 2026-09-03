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
	if file != nil {
		buffered = bufferedFraction(tor, file)
		progress = fileProgress(file)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.peers = stats.ActivePeers
	s.seeders = stats.ConnectedSeeders
	s.buffered, s.progress = buffered, progress
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

func (s *session) bufferedFraction() float64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buffered
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

// bufferedFraction 返回起播缓冲的完成比例（0..1）。
//
// 按【整分片】计而不是按可读字节：SetResponsive 让读可以在分片校验前返回，
// 但「够不够起播」这个判断必须保守，否则 mpv 起来后立刻就卡住。
func bufferedFraction(tor *torrent.Torrent, file *torrent.File) float64 {
	target := min64(bufferStartBytes, file.Length())
	if target <= 0 {
		return 1
	}
	done := completedBytesIn(tor, file, target)
	if done >= target {
		return 1
	}
	return float64(done) / float64(target)
}

// completedBytesIn 统计文件 [0, n) 区间内落在已完成分片里的字节数。
func completedBytesIn(tor *torrent.Torrent, file *torrent.File, n int64) int64 {
	info := tor.Info()
	if info == nil || info.PieceLength <= 0 || n <= 0 {
		return 0
	}
	pieceLen := info.PieceLength
	begin := file.Offset()
	end := begin + n
	total := tor.NumPieces()
	var done int64
	for idx := int(begin / pieceLen); idx <= int((end-1)/pieceLen); idx++ {
		if idx < 0 || idx >= total {
			break
		}
		if tor.PieceBytesMissing(idx) != 0 {
			continue
		}
		lo, hi := int64(idx)*pieceLen, int64(idx+1)*pieceLen
		if lo < begin {
			lo = begin
		}
		if hi > end {
			hi = end
		}
		if hi > lo {
			done += hi - lo
		}
	}
	return done
}

func fileProgress(file *torrent.File) float64 {
	if file.Length() <= 0 {
		return 0
	}
	return float64(file.BytesCompleted()) / float64(file.Length())
}
