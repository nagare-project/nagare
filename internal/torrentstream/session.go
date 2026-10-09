// 一次播放会话：加入磁力 → 等元数据 → 选文件 → 等起播缓冲 → 停止即删。
//
// 会话的生命周期 ctx 派生自引擎的根 ctx，【不是】Prepare 传进来的那个：
// 后者是 HTTP 请求的 ctx，响应一返回就被取消，种子和 reader 挂上去会让 mpv
// 在刚起来的瞬间断流。Prepare 的 ctx 只约束「等元数据」「等缓冲」两段等待。
package torrentstream

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"

	errs "github.com/nagare-project/nagare/internal/errors"
	"github.com/nagare-project/nagare/internal/library"
)

// minSampleSeconds 是速率差分的最小采样间隔，避免除以一个接近零的时间差
// 得出荒谬的瞬时速度。
const minSampleSeconds = 0.2

// awaitingTTL 是「停在选集弹窗」的最长保留时间。
//
// 弹出选集时种子是留着的：用户马上就要带 fileIndex 重发，重新等一轮元数据是白等。
// 但用户完全可能直接关掉标签页而不点取消 —— 那条磁力就会一直挂在 DHT 里
// announce 到进程退出（虽然一个分片都不下）。给它一个上限，超时自己收掉。
//
// 做成变量是为了让测试能把它调短，生产路径不改这个值。
var awaitingTTL = 10 * time.Minute

// PrepareRequest 是一次播放准备请求。
type PrepareRequest struct {
	Magnet      string
	TorrentURL  string // Plugin API v1 的 .torrent 地址；与 Magnet 二选一
	Title       string // 搜索结果标题，用于派生集号提示；可空
	EpisodeHint int    // <=0 表示未指定
	// AltEpisodeHint 是同一集的另一种编号（跨季连续编号：第二季第 3 集在合集里叫 15）；
	// <=0 表示没有。只在 EpisodeHint 也给了时才有意义，见 select.go。
	AltEpisodeHint int
	FileIndex      int // <0 表示用户还没手选
	// SuggestedFileIndex 是来源插件指明的文件（nil 表示没有）。只是建议：不在候选里就当没给。
	SuggestedFileIndex *int
}

// FileChoice 是交给用户手选的一个候选文件。
type FileChoice struct {
	Index   int    `json:"index"`
	Name    string `json:"name"`
	Path    string `json:"path"`
	Size    int64  `json:"sizeBytes"`
	Episode *int   `json:"episode"`
}

// PrepareResult 是准备结果：要么需要用户选集，要么给出可播放的 Source。
type PrepareResult struct {
	NeedSelection bool
	Files         []FileChoice
	Source        *Source // NeedSelection == false 时非 nil
}

// session 是当前这一次播放。字段由 mu 保护，可被 Status / handler 并发读。
type session struct {
	engine   *Engine
	client   *torrent.Client
	locator  string
	magnet   string
	metaInfo *metainfo.MetaInfo

	ctx    context.Context
	cancel context.CancelFunc

	mu       sync.Mutex
	tor      *torrent.Torrent
	file     *torrent.File
	pm       *priorityManager
	item     library.Item
	index    int
	infohash string
	name     string
	phase    string
	awaiting bool // 停在「等用户选集」，此时种子保留不 drop
	// awaitTimer 是 awaiting 状态的保留时限；选完集或会话终结时停掉。
	awaitTimer *time.Timer
	// writeErr 是分片写盘失败（磁盘满 / 无权限）；只留第一条。
	writeErr error
	closed   bool

	sampleAt  time.Time
	lastRead  int64
	lastWrite int64
	downRate  int64
	upRate    int64
	peers     int
	seeders   int
	// buffered 是给界面看的起播缓冲进度（计入未完成分片里已到手的块）；
	// gateOpen 才是真正放行 mpv 的判定（整片到齐），两者分开见 gateProgress。
	buffered float64
	gateOpen bool
	progress float64
}

// Prepare 准备一次播放。同一时刻只有一个会话，开新的之前先停掉旧的。
func (e *Engine) Prepare(ctx context.Context, req PrepareRequest) (PrepareResult, error) {
	req.Magnet = strings.TrimSpace(req.Magnet)
	req.TorrentURL = strings.TrimSpace(req.TorrentURL)
	if (req.Magnet == "") == (req.TorrentURL == "") {
		return PrepareResult{}, errs.New(errs.CategoryInput, "torrentstream.prepare",
			"需要提供一条磁力链接或种子文件地址", "换一条资源后重试")
	}
	locator := req.Magnet
	if req.Magnet != "" {
		if !strings.HasPrefix(strings.ToLower(req.Magnet), "magnet:") {
			return PrepareResult{}, errs.New(errs.CategoryInput, "torrentstream.prepare",
				"这不是一条磁力链接", "复制完整的 magnet: 链接后重试")
		}
	} else {
		if err := validateTorrentURL(req.TorrentURL); err != nil {
			return PrepareResult{}, errs.Wrap(errs.CategoryInput, "torrentstream.torrent-url",
				"种子文件地址无效", "换一条资源后重试", err)
		}
		locator = "torrent-url:" + req.TorrentURL
	}

	// 抢占：上一次 Prepare 可能还卡在等元数据里，先打断它，否则这一次要在
	// prepMu 上白等一个超时。
	e.interrupt(locator)

	e.prepMu.Lock()
	defer e.prepMu.Unlock()

	// 选集回传时复用已经下载并解析好的 .torrent，不再次访问可能已经失效的地址。
	if sess := e.reusableSession(locator); sess != nil {
		sess.resume()
		res, err := sess.run(ctx, req)
		if err != nil {
			e.Stop()
			return PrepareResult{}, err
		}
		return res, nil
	}

	var metadata *metainfo.MetaInfo
	if req.TorrentURL != "" {
		fetch := e.fetchMetaInfo
		if fetch == nil {
			fetch = fetchTorrentMetaInfo
		}
		var err error
		metadata, err = fetch(ctx, req.TorrentURL)
		if err != nil {
			return PrepareResult{}, err
		}
		info, err := metadata.UnmarshalInfo()
		if err != nil {
			return PrepareResult{}, errs.Wrap(errs.CategoryInput, "torrentstream.metainfo",
				"种子文件内容无效", "换一条资源后重试", err)
		}
		if isPrivate(&info) {
			return PrepareResult{}, errPrivateTorrent()
		}
		// magnet 只作为当前会话的内部标识；实际加入的是已经验证的 metainfo，
		// 不会为了拿元数据再等待 DHT。
		req.Magnet = metadata.Magnet(nil, &info).String()
	}

	sess, err := e.beginSession(locator, req.Magnet, metadata)
	if err != nil {
		return PrepareResult{}, err
	}
	res, err := sess.run(ctx, req)
	if err != nil {
		// 也包括 ctx 被取消：用户取消后不能留一个种子在后台继续下载。
		e.Stop()
		return PrepareResult{}, err
	}
	return res, nil
}

// interrupt 打断当前会话，除非它正停在选集弹窗上等同一条磁力
// —— 那种情况重新等一次元数据是白等。
func (e *Engine) interrupt(locator string) {
	e.mu.Lock()
	sess := e.sess
	e.mu.Unlock()
	if sess != nil && sess.reusableFor(locator) {
		return
	}
	// 只收掉刚才观察到的那一个：这段跑在 prepMu 之外，无条件 Stop 会误杀
	// 另一次 Prepare 刚刚建好的会话。
	e.stopSession(sess)
}

// beginSession 复用或新建会话。
func (e *Engine) beginSession(locator, magnet string, metadata *metainfo.MetaInfo) (*session, error) {
	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		return nil, errEngineClosed()
	}
	if sess := e.sess; sess != nil && sess.reusableFor(locator) {
		e.mu.Unlock()
		sess.resume()
		return sess, nil
	}
	e.mu.Unlock()

	e.Stop()
	client, err := e.activeClient()
	if err != nil {
		return nil, err
	}

	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return nil, errEngineClosed()
	}
	ctx, cancel := context.WithCancel(e.rootCtx)
	sess := &session{
		engine: e, client: client, locator: locator, magnet: magnet, metaInfo: metadata,
		ctx: ctx, cancel: cancel, index: -1, phase: PhaseMetadata,
	}
	e.sess = sess
	return sess, nil
}

func (e *Engine) reusableSession(locator string) *session {
	e.mu.Lock()
	sess := e.sess
	e.mu.Unlock()
	if sess != nil && sess.reusableFor(locator) {
		return sess
	}
	return nil
}

// Stop 停止当前播放：取消会话、丢掉种子、删掉这次的分片。幂等，可并发调用。
func (e *Engine) Stop() {
	e.mu.Lock()
	sess := e.sess
	e.sess = nil
	e.mu.Unlock()
	e.finishSession(sess)
}

// stopSession 只收掉【指定的那个】会话：它仍是当前会话才动手，否则什么都不做。
//
// 与 Stop 的区别是关键的。抢占（interrupt）与保留时限到点（expireAwaiting）都是
// 「我观察到某个会话该收了」，而它们跑在 prepMu 之外 —— 期间完全可能已经换成了
// 另一条磁力。无条件收「当前那个」就会把后来者误杀。
func (e *Engine) stopSession(target *session) {
	if target == nil {
		return
	}
	e.mu.Lock()
	if e.sess != target {
		e.mu.Unlock()
		return
	}
	e.sess = nil
	e.mu.Unlock()
	e.finishSession(target)
}

// finishSession 收尾一个已经从 e.sess 上摘下来的会话。
func (e *Engine) finishSession(sess *session) {
	if sess == nil {
		return
	}
	sess.close()
	// 停播即删（决议 M3-4）。一次只播一个种子，整目录删掉就是删掉这次的分片。
	_ = e.purgeCache()
}

// run 跑完一次准备。
//
// 每个阶段的耗时都落日志：「缓冲时间长」这种反馈没有数字就只能猜。日志里只有
// 耗时、peer 数与速率，没有种子名或 infohash（那是用户在看什么，不进日志）。
func (s *session) run(ctx context.Context, req PrepareRequest) (PrepareResult, error) {
	started := time.Now()
	tor, err := s.ensureTorrent(ctx)
	if err != nil {
		return PrepareResult{}, err
	}
	s.refresh()
	metaPeers, metaSeeders := s.snapshotPeers()
	log.Printf("torrent: 元数据就绪，耗时 %s（peers=%d seeders=%d）",
		time.Since(started).Round(100*time.Millisecond), metaPeers, metaSeeders)

	s.setPhase(PhaseSelecting)
	sel, err := selectFile(entriesOf(tor), req)
	if err != nil {
		return PrepareResult{}, err
	}
	if sel.Need {
		s.markAwaiting()
		return PrepareResult{NeedSelection: true, Files: sel.Files}, nil
	}

	if err := s.startFile(tor, sel); err != nil {
		return PrepareResult{}, err
	}
	s.setPhase(PhaseBuffering)
	bufferStarted := time.Now()
	readBefore := usefulBytesRead(tor)
	if err := s.waitBuffered(ctx); err != nil {
		return PrepareResult{}, err
	}
	s.openGate()
	s.setPhase(PhaseReady)
	s.logBuffered(tor, time.Since(bufferStarted), usefulBytesRead(tor)-readBefore)
	return PrepareResult{Source: s.newSource()}, nil
}

// usefulBytesRead 是这个种子迄今收到的有效数据字节（不含重复与作废的块）。
// t.Stats() 会拿 client 锁，不能在持有 s.mu 时调用。
func usefulBytesRead(tor *torrent.Torrent) int64 {
	stats := tor.Stats()
	return stats.BytesReadUsefulData.Int64()
}

// logBuffered 记一笔起播缓冲的耗时。
//
// 除了门槛大小，还记这段时间实际收到多少字节、分片多大：门槛按整片判定，16MB 分片下
// 「8MB 门槛」实际要下满第一片；收到的远多于门槛，说明带宽被门槛之外的分片分走了。
// 这两个数正是判断「起播慢是种子慢，还是优先级排错了」的依据。
func (s *session) logBuffered(tor *torrent.Torrent, elapsed time.Duration, received int64) {
	gate := int64(bufferStartBytes)
	s.mu.Lock()
	if s.file != nil { // 会话可能刚被并发 Stop 掉
		gate = min(gate, s.file.Length())
	}
	s.mu.Unlock()
	pieceLen := int64(0)
	if info := tor.Info(); info != nil {
		pieceLen = info.PieceLength
	}
	peers, seeders := s.snapshotPeers()
	log.Printf("torrent: 起播缓冲 %s 到齐，耗时 %s（期间收到 %s，平均 %s/s；分片 %s；peers=%d seeders=%d），交给 mpv",
		formatBytes(gate), elapsed.Round(100*time.Millisecond),
		formatBytes(received), formatBytes(int64(float64(received)/max(elapsed.Seconds(), 0.1))),
		formatBytes(pieceLen), peers, seeders)
}

// snapshotPeers 取最近一次 refresh 记下的 peer / seeder 数。
func (s *session) snapshotPeers() (int, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.peers, s.seeders
}

// formatBytes 给日志用的粗粒度体积（MB 一位小数；日志不是界面，不做本地化）。
func formatBytes(n int64) string {
	const mb = 1024 * 1024
	if n < mb {
		return fmt.Sprintf("%dKB", n/1024)
	}
	return fmt.Sprintf("%.1fMB", float64(n)/mb)
}

// ensureTorrent 加入磁力并等到元数据；已有元数据（选集重发）时直接复用。
func (s *session) ensureTorrent(ctx context.Context) (*torrent.Torrent, error) {
	if tor := s.currentTorrent(); tor != nil && tor.Info() != nil {
		return tor, nil
	}
	s.setPhase(PhaseMetadata)
	if s.metaInfo != nil {
		info, err := s.metaInfo.UnmarshalInfo()
		if err != nil {
			return nil, errs.Wrap(errs.CategoryInput, "torrentstream.metainfo",
				"种子文件内容无效", "换一条资源后重试", err)
		}
		if isPrivate(&info) {
			return nil, errPrivateTorrent()
		}
		tor, err := s.client.AddTorrent(s.metaInfo)
		if err != nil {
			return nil, errs.Wrap(errs.CategoryInput, "torrentstream.metainfo",
				"种子文件无法载入", "换一条资源后重试", err)
		}
		s.adopt(tor)
		applyTrackers(tor, &info, s.engine.trackers())
		s.noteName(tor.Name())
		return tor, nil
	}
	tor, err := s.client.AddMagnet(s.magnet)
	if err != nil {
		return nil, errs.Wrap(errs.CategoryInput, "torrentstream.magnet",
			"磁力链接无法解析", "复制完整的 magnet: 链接后重试", err)
	}
	s.adopt(tor)
	// 用户配的 tracker 在【拿 info 之前】就挂上。实测（2026-09-12，Anime Garden 的无 tracker
	// 磁力）：纯 DHT 找元数据 50–125 秒甚至找不到，带公共 tracker 2.6 秒 —— 这一段正是
	// 「点了播放却迟迟不起播」的全部。
	// 私有种子的顾虑在磁力这条路上不成立：private 标记只在 info 里，而磁力本来就要先靠
	// DHT 公开找 peer 才拿得到 info，多报几个公共 tracker 并没有多暴露什么；passkey 只存在
	// 于私有站自己的 announce 地址里，我们从不碰它。拿到 info 发现是私有种子照旧中止。
	applyTrackers(tor, nil, s.engine.trackers())

	timeout := time.NewTimer(metadataTimeout)
	defer timeout.Stop()
	tick := time.NewTicker(statusPollInterval)
	defer tick.Stop()
	for {
		select {
		case <-tor.GotInfo():
			info := tor.Info()
			// 私有种子到此为止，不往下播（见 errPrivateTorrent 的理由）。
			if isPrivate(info) {
				return nil, errPrivateTorrent()
			}
			s.noteName(tor.Name())
			return tor, nil
		case <-tick.C:
			s.refresh() // 等待期间也要有实时 peer 数与速度可显示
		case <-ctx.Done():
			return nil, errRequestCanceled(ctx.Err())
		case <-s.ctx.Done():
			return nil, errSessionStopped()
		case <-timeout.C:
			return nil, errs.New(errs.CategoryTorrent, "torrentstream.metadata",
				"找不到可用的分享者", "换一条资源试试，或稍后重试")
		}
	}
}

// startFile 选定文件、只下这一个文件、建立优先级窗口。
func (s *session) startFile(tor *torrent.Torrent, sel selection) error {
	if s.ctx.Err() != nil {
		return errSessionStopped()
	}
	files := tor.Files()
	if sel.Index < 0 || sel.Index >= len(files) {
		return errs.New(errs.CategoryInternal, "torrentstream.select",
			"选中的文件下标越界", "重新打开选集列表再选一次")
	}
	info := tor.Info()
	if info == nil || info.PieceLength <= 0 {
		return errs.New(errs.CategoryTorrent, "torrentstream.select",
			"种子信息不完整", "换一条资源试试")
	}
	file := files[sel.Index]

	item := sel.Item
	// FileID 稳定且跨会话可续播：同一条磁力的同一个文件永远是同一个 id。
	item.FileID = "t:" + tor.InfoHash().HexString() + "/" + strconv.Itoa(sel.Index)

	s.mu.Lock()
	prev := s.file
	s.file, s.index, s.item = file, sel.Index, item
	s.infohash = tor.InfoHash().HexString()
	// awaiting 与它的计时器永远同进同退，别让二者分家。
	s.awaiting = false
	s.stopAwaitTimerLocked()
	s.mu.Unlock()

	if prev != nil && prev != file {
		prev.Cancel()
	}
	// 接住分片写盘失败。anacrolix 的默认处理是记一条 CRITICAL 日志再永久停掉
	// 这个种子的下载，而那条日志正好被我们丢弃了（见 client.go 里丢 Slogger 的理由）——
	// 不接住它，用户看到的只有「进度不动了」，与「分享者太慢」完全无法区分。
	tor.SetOnWriteChunkError(func(err error) {
		s.noteWriteError(err)
		// 复刻默认处理剩下的那一半：别对着写不进去的磁盘反复重试。
		tor.DisallowDataDownload()
	})

	// 缓冲期只有门槛可请求，文件其余部分先保持 None，等门槛放行再打开（见 openGate）。
	pm := newPriorityManager(windowInput{
		pieceLength: info.PieceLength,
		fileOffset:  file.Offset(),
		fileLength:  file.Length(),
		firstPiece:  file.BeginPieceIndex(),
		lastPiece:   file.EndPieceIndex() - 1,
	}, func(idx int, prio torrent.PiecePriority) {
		tor.Piece(idx).SetPriority(prio)
	}, nil)

	s.mu.Lock()
	s.pm = pm
	s.mu.Unlock()
	return nil
}

// openGate 在起播门槛放行后把这一集的其余部分放开下载。
//
// 缓冲期间之所以只有门槛可请求：anacrolix 给每个 peer 一批请求，【整批】收完才排下一批
// （requesting.go 只在 isLowOnRequests 时更新），新的一批按档位从高到低填满。门槛的块
// 派完之后，空出的名额会被任何可请求的低档块填上 —— 于是持有门槛最后几块的那一批里
// 混着一堆低档请求：按随机顺序出货的对端（anacrolix 系的做种方就是这样，peerconn.go
// 从 map 里取请求）要把整批发完门槛才凑齐；按顺序出货的对端也受害 —— 快的 peer 得先
// 收完自己那批低档块，才会重排请求、去抢慢 peer 手上卡住的门槛块。按
// startup_integration_test.go 的设置（4 个做种方各限速 1MB/s）实测：改动前 3.7–4.5 秒，
// 缓冲期门槛 + 尾部可请求 2.7 秒，只有门槛 1.8 秒 —— 正好是 8MB 除以总速率。
// 尾部虽是放行后第一次 seek / 续播要用的，也放到放行时才钉。
//
// 顺序同样要紧：先钉尾部与门槛之后的头部、再把整个文件设为 Normal。file.Download()
// 会立刻唤醒所有手上没有请求的 peer 去排请求（torrent.go updatePeerRequestsForPiece），
// 那一刻要是它们还没钉上，这批请求就按稀有度散落在整个文件里。
func (s *session) openGate() {
	s.mu.Lock()
	pm, file := s.pm, s.file
	s.mu.Unlock()
	if pm == nil || file == nil {
		return // 会话刚被并发 Stop 掉
	}
	pm.openGate()
	// 只下要播的这一集：其余文件保持 PiecePriorityNone。
	file.Download()
}

// waitBuffered 等起播缓冲到齐。
func (s *session) waitBuffered(ctx context.Context) error {
	timeout := time.NewTimer(bufferTimeout)
	defer timeout.Stop()
	tick := time.NewTicker(gatePollInterval)
	defer tick.Stop()
	// 只轮询、不订阅 SubscribePieceStateChanges：那个订阅必须持续消费，
	// 一旦这里因超时或取消提前返回而没排干，就会顶住 client 的发布路径。
	// 门槛按 gatePollInterval 查（门槛一齐就放行，swarm 不空等）；完整的状态刷新
	// （速度、peer 数、整集进度）仍按 statusPollInterval，界面按秒轮询，不必更勤。
	lastFull := time.Time{}
	for {
		if now := time.Now(); now.Sub(lastFull) >= statusPollInterval {
			s.refresh()
			lastFull = now
		} else {
			s.refreshGate()
		}
		if err := s.storageFailure(); err != nil {
			return err
		}
		if s.gateReady() {
			return nil
		}
		select {
		case <-tick.C:
		case <-ctx.Done():
			return errRequestCanceled(ctx.Err())
		case <-s.ctx.Done():
			return errSessionStopped()
		case <-timeout.C:
			return errs.New(errs.CategoryTorrent, "torrentstream.buffer",
				"缓冲超时，分享者太少", "稍后重试，或换一条分享者更多的资源")
		}
	}
}

// close 取消会话并丢掉种子。幂等。
func (s *session) close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	s.stopAwaitTimerLocked()
	tor := s.tor
	s.tor, s.file, s.pm = nil, nil, nil
	s.phase = PhaseIdle
	s.mu.Unlock()

	s.cancel()
	if tor != nil {
		tor.Drop()
	}
}

// stopAwaitTimerLocked 停掉保留时限计时器。调用方必须已持 s.mu。
func (s *session) stopAwaitTimerLocked() {
	if s.awaitTimer != nil {
		s.awaitTimer.Stop()
		s.awaitTimer = nil
	}
}

// expireAwaiting 在选集弹窗被晾了太久之后收掉这条磁力。
func (s *session) expireAwaiting() {
	s.mu.Lock()
	stale := !s.closed && s.awaiting
	s.mu.Unlock()
	if !stale {
		return
	}
	// stopSession 在锁内比对身份：期间可能早已换成另一条磁力，那时什么都不做。
	log.Printf("torrent: 选集列表 %v 无人选择，已释放该种子", awaitingTTL)
	s.engine.stopSession(s)
}

// reusableFor 判断这个会话能否直接承接同一资源的重发（选集弹窗选完那一次）。
func (s *session) reusableFor(locator string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return !s.closed && s.awaiting && s.locator == locator &&
		s.tor != nil && s.tor.Info() != nil
}

// resume 让复用的会话从「等选集」回到工作状态。
func (s *session) resume() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.awaiting = false
	s.phase = PhaseSelecting
	s.stopAwaitTimerLocked()
}

func (s *session) markAwaiting() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.awaiting = true
	s.phase = PhaseSelecting
	s.stopAwaitTimerLocked()
	s.awaitTimer = time.AfterFunc(awaitingTTL, s.expireAwaiting)
}

func (s *session) setPhase(phase string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.closed {
		s.phase = phase
	}
}

func (s *session) noteName(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.name = name
}

func (s *session) adopt(tor *torrent.Torrent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tor = tor
	s.infohash = tor.InfoHash().HexString()
}

func (s *session) currentTorrent() *torrent.Torrent {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.tor
}

// entriesOf 把种子里的文件转成选集逻辑要的形状。
func entriesOf(tor *torrent.Torrent) []fileEntry {
	files := tor.Files()
	out := make([]fileEntry, 0, len(files))
	for i, f := range files {
		out = append(out, fileEntry{Index: i, Path: f.Path(), Size: f.Length()})
	}
	return out
}

// noteWriteError 记下分片写盘失败，只留第一条（后面的都是同一个原因的回声）。
func (s *session) noteWriteError(err error) {
	s.mu.Lock()
	first := s.writeErr == nil
	if first {
		s.writeErr = err
	}
	s.mu.Unlock()
	if first {
		log.Printf("torrent: 分片写盘失败，已停止这条种子的下载：%v", err)
	}
}

// storageFailure 把已记下的写盘失败翻成分类错误；没失败返回 nil。
func (s *session) storageFailure() error {
	s.mu.Lock()
	err := s.writeErr
	s.mu.Unlock()
	if err == nil {
		return nil
	}
	return errs.Wrap(errs.CategoryStorage, "torrentstream.write",
		"磁盘写入失败，已停止下载", "确认磁盘还有空间、缓存目录可写，然后重试", err)
}

// errPrivateTorrent 是私有站（PT）种子。
//
// anacrolix v1.61.0 没有「只对某个种子关掉 DHT/PEX」的能力（全仓对 info.Private
// 的引用只剩结构体定义），而 private 标记只存在于 info 字典里 —— 磁力必须先靠
// DHT 找到 peer 才拿得到 info。也就是说等我们知道它是私有种子时，infohash 已经
// 进过公共 DHT 了，这一步无法避免。能做的是立刻停手并说清楚，而不是继续播下去
// 让用户的私有站账号一直暴露在被封的风险里。
func errPrivateTorrent() error {
	return errs.New(errs.CategoryInput, "torrentstream.private",
		"这是私有站（PT）的种子，nagare 不支持边下边播",
		"当前 BT 引擎无法只对单个种子关闭 DHT/PEX，继续下载可能导致私有站账号被封；请改用该站推荐的客户端")
}

// errRequestCanceled 是调用方（HTTP 请求）中途断开。
func errRequestCanceled(cause error) error {
	return errs.Wrap(errs.CategoryTorrent, "torrentstream.prepare",
		"播放准备已取消", "重新点一次播放", cause)
}

// errSessionStopped 是会话被 Stop/Close 打断。
func errSessionStopped() error {
	return errs.New(errs.CategoryTorrent, "torrentstream.prepare",
		"播放已停止", "重新点一次播放")
}
