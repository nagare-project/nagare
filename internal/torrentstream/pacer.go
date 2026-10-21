// 每个 peer 的请求队列按它的实际速度封顶（照 libtorrent 的 request_queue_time）。
//
// anacrolix v1.61.0 给每个 peer 排一批请求，【整批】收完才排下一批（peerconn.go 的
// isLowOnRequests），批的大小只看对端声明的 reqq 与上一批的两倍（peerconn.go
// nominalMaxRequests），最多约 960 块、15MB —— 与这个 peer 有多快无关。少数几个做种方的
// 老番里，一个每秒 300KB 的 peer 手上挂着 15MB 要将近一分钟才收完；其中只要有一块正好是
// 播放位置的下一块，mpv 就干等，直到它发完，或者别的 peer 在重排时把它抢走 —— 而重排只在
// 抢的一方自己那批整批收完时才发生（requesting.go 的 applyRequestState）。
//
// 这里把每个 peer 的队列压到「它 requestQueueTime 之内送得完」的量：慢 peer 手上只挂几块，
// 快 peer 每隔几秒就整批收完、重排一次，重排时把慢 peer 手上更急的块抢过去。
//
// 只往下压、不往上抬：上限不超过对端在扩展握手里声明的 reqq，也不超过 anacrolix 自己的
// 上限。改 PeerMaxRequests 必须在 client 锁内 —— 写它的 ReceivedUsefulData 回调是 anacrolix
// 处理收到的块时同步调的，那时锁正持有着（peerconn.go 的 mainReadLoop 只在解码时放锁）；
// 也因此回调里不能调 Peer.Stats()（它要再拿一次读锁），速度只能自己按收到的字节算。
package torrentstream

import (
	"sync"
	"time"

	"github.com/anacrolix/torrent"
	pp "github.com/anacrolix/torrent/peer_protocol"
)

const (
	// requestQueueTime：每个 peer 手上最多挂着它这么久送得完的请求。与 libtorrent 的
	// request_queue_time 默认值相同；比任何正常的往返时延都大得多，所以不会因为排得少
	// 而拖慢快 peer 的吞吐。
	requestQueueTime = 3 * time.Second

	// minPacedRequests：再慢的 peer 也至少给这么多块，免得刚连上、还没测出速度的 peer 被
	// 卡成一次一块。
	minPacedRequests = 8

	// paceChunkBytes 是一个请求块的大小（anacrolix 默认的 chunk size）。
	paceChunkBytes = 16 << 10

	// paceSampleWindow：攒够这么久的字节才更新一次速度。
	paceSampleWindow = 500 * time.Millisecond

	// paceIdleGap：两块之间隔得比这久，才去分辨这段空档是「它慢」还是「它闲」（见 observe）。
	paceIdleGap = time.Second
)

// paceState 是一个 peer 的测速状态。
type paceState struct {
	ceiling  int       // 对端允许的上限；0 表示还不知道（用第一次见到时的 PeerMaxRequests）
	since    time.Time // 这一段采样的开始
	last     time.Time // 上一次收到有效块
	lastSent time.Time // 上一次给它发请求
	bytes    int64     // 这一段收到的字节
	rate     float64   // 字节/秒，各段之间对半平滑；0 表示还没测出来
}

// sent 记下 now 给这个 peer 发了一个请求。
func (s *paceState) sent(now time.Time) { s.lastSent = now }

// observe 记下 now 收到的 n 字节，返回此刻应当给这个 peer 的请求上限；
// 还没测出速度时 ok 为 false，调用方不该动它的上限。
//
// 两块之间的长空档有两种：它手上一直挂着请求、只是慢（这段要算进速度，否则每秒不到 16KB 的
// peer 永远测不出来、从快变慢的 peer 一直沿用旧上限 —— 偏偏正是最会卡住播放的那种）；
// 或者它手上的请求早就送完了、我们一时没有要它下的（窗口里的都下完了），这段不能算。
// 分辨办法是看空档里有没有给它发过新请求：没发过，收到的这块回应的就是空档之前发出的请求，
// 它一直在欠着；发过，就从重新发请求那一刻量起（队列可能空过，宁可把它估快一点）。
func (s *paceState) observe(now time.Time, n int) (limit int, ok bool) {
	switch {
	case s.since.IsZero():
		// 第一块：从给它发请求那一刻量起，而不是从这块到达算起 —— 否则这块的字节算进去了、
		// 等它的时间没算，第一段会测得偏快。
		s.since, s.bytes = s.startOf(now), 0
	case now.Sub(s.last) > paceIdleGap && s.lastSent.After(s.last):
		s.since, s.bytes = s.startOf(now), 0
	}
	s.last = now
	s.bytes += int64(n)
	if elapsed := now.Sub(s.since); elapsed >= paceSampleWindow {
		sample := float64(s.bytes) / elapsed.Seconds()
		if s.rate == 0 {
			s.rate = sample
		} else {
			s.rate = (s.rate + sample) / 2
		}
		s.since, s.bytes = now, 0
	}
	if s.rate == 0 {
		return 0, false
	}
	limit = int(s.rate * requestQueueTime.Seconds() / paceChunkBytes)
	limit = max(limit, minPacedRequests)
	if s.ceiling > 0 {
		limit = min(limit, s.ceiling)
	}
	return limit, true
}

// startOf 是一段新采样的起点：最近一次发请求的时刻，没有记录（或时钟不单调）时退回 now。
func (s *paceState) startOf(now time.Time) time.Time {
	if s.lastSent.IsZero() || s.lastSent.After(now) {
		return now
	}
	return s.lastSent
}

// requestPacer 按 peer 记测速状态，挂在 client 的回调上。
type requestPacer struct {
	mu    sync.Mutex
	peers map[*torrent.PeerConn]*paceState
	now   func() time.Time
}

// installRequestPacer 把请求队列封顶挂到 client 配置上。生产与集成测试的 client 都要调，
// 否则测试量到的就不是用户实际跑的调度。
//
// ReadExtendedHandshake 与 PeerConnClosed 在 anacrolix 里是单个函数而不是列表：已有的
// 要接着调，不能覆盖 —— 覆盖掉前者丢的是上限，覆盖掉后者 map 就只增不减。
func installRequestPacer(tc *torrent.ClientConfig) *requestPacer {
	p := &requestPacer{peers: make(map[*torrent.PeerConn]*paceState), now: time.Now}
	cb := &tc.Callbacks
	if prev := cb.ReadExtendedHandshake; prev != nil {
		cb.ReadExtendedHandshake = func(pc *torrent.PeerConn, msg *pp.ExtendedHandshakeMessage) {
			prev(pc, msg)
			p.onExtendedHandshake(pc, msg)
		}
	} else {
		cb.ReadExtendedHandshake = p.onExtendedHandshake
	}
	if prev := cb.PeerConnClosed; prev != nil {
		cb.PeerConnClosed = func(pc *torrent.PeerConn) {
			prev(pc)
			p.forget(pc)
		}
	} else {
		cb.PeerConnClosed = p.forget
	}
	cb.ReceivedUsefulData = append(cb.ReceivedUsefulData, p.onUsefulData)
	cb.SentRequest = append(cb.SentRequest, p.onSentRequest)
	return p
}

// state 取出（或建立）一个 peer 的状态。调用方必须持有 p.mu。
func (p *requestPacer) state(pc *torrent.PeerConn) *paceState {
	s := p.peers[pc]
	if s == nil {
		s = &paceState{}
		p.peers[pc] = s
	}
	return s
}

// onExtendedHandshake 记下对端声明的 reqq。anacrolix 在这个回调【之后】才把 reqq 写进
// PeerMaxRequests，所以这里拿到的是对端的原话，不会是我们自己压过的值。
func (p *requestPacer) onExtendedHandshake(pc *torrent.PeerConn, msg *pp.ExtendedHandshakeMessage) {
	if msg == nil || msg.Reqq <= 0 {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.state(pc).ceiling = msg.Reqq
}

// onSentRequest 只记时间，不动上限：它是在 anacrolix 发请求的循环里同步调的，那个循环每发
// 一个请求前才按上限检查一次，循环中途改上限没有意义。
func (p *requestPacer) onSentRequest(ev torrent.PeerRequestEvent) {
	pc, ok := ev.Peer.TryAsPeerConn()
	if !ok {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.state(pc).sent(p.now())
}

func (p *requestPacer) onUsefulData(ev torrent.ReceivedUsefulDataEvent) {
	pc, ok := ev.Peer.TryAsPeerConn()
	if !ok || ev.Message == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	s := p.state(pc)
	if s.ceiling == 0 {
		// 没发过扩展握手（或 reqq 为 0）的对端：此刻的值还是 anacrolix 的默认上限。
		s.ceiling = pc.PeerMaxRequests
	}
	if limit, ok := s.observe(p.now(), len(ev.Message.Piece)); ok {
		pc.PeerMaxRequests = limit
	}
}

// tracked 返回正在测速的 peer 数，以及其中已经测出速度的有几个。
func (p *requestPacer) tracked() (peers, measured int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, s := range p.peers {
		if s.rate > 0 {
			measured++
		}
	}
	return len(p.peers), measured
}

func (p *requestPacer) forget(pc *torrent.PeerConn) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.peers, pc)
}
