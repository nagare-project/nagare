// 窗口与集：一个 mpv 进程（窗口）是一个 playback，窗口里正在播的一集是一个 session。
//
// 窗口管 mpv 的生命周期：唯一的事件循环（watchWindow）、Stop 等的 finished、
// OnSessionEnd（磁力停做种删分片）都按窗口算，每个窗口恰好一次。
// 集管这一个文件的事：进度、弹幕、匹配、「看完」回写，每集收尾恰好一次（endEpisode）。
package player

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nagare-project/nagare/internal/library"
	"github.com/nagare-project/nagare/internal/mpv"
	"github.com/nagare-project/nagare/internal/random"
)

// PlaybackID 标识一个 mpv 窗口（一次 Play 起的那个进程）。同一个窗口里换集不换 ID，
// 调用方据此认出「是不是我起的那个窗口出的事」，而不必只靠 fileId。
type PlaybackID string

type playback struct {
	id     PlaybackID
	player *mpv.Player
	// finished 在窗口收尾全部做完后关闭（Stop 等的就是它）。
	finished chan struct{}
	// ctx 是窗口的存活期，窗口终结时取消。给窗口里的准备工作用（切集时准备下一集）；
	// 每一集自己的 ctx 刻意不从它派生，见 newSession。
	ctx    context.Context
	cancel context.CancelFunc
	// bg 是窗口里的后台工作（「看完」回写）。窗口收尾时有限期地等它们做完。
	bg sync.WaitGroup
	// uiReady：界面脚本发过 ready（它加载好了，收得到界面消息）。
	uiReady atomic.Bool

	mu sync.Mutex
	ep *session // 正在播的这一集
}

func newPlayback(pl *mpv.Player) *playback {
	ctx, cancel := context.WithCancel(context.Background())
	return &playback{
		id:       PlaybackID(random.Hex(8)),
		player:   pl,
		finished: make(chan struct{}),
		ctx:      ctx,
		cancel:   cancel,
	}
}

// episode 返回正在播的这一集。
func (pb *playback) episode() *session {
	pb.mu.Lock()
	defer pb.mu.Unlock()
	return pb.ep
}

func (pb *playback) setEpisode(sess *session) {
	pb.mu.Lock()
	pb.ep = sess
	pb.mu.Unlock()
}

// newSession 为窗口 pb 造一集。弹幕先标「正在匹配」，后台解析完再落定。
//
// 这一集的 ctx 不从窗口的 ctx 派生：关掉窗口时后台匹配要照样做完，把绑定与文件指纹落盘，
// 下次播放直接用（关窗之前就是这样）。只有在同一个窗口里换到别的集时，才取消被换走那一集
// 的 ctx（sess.cancel）——那时它的弹幕已经没人要了，磁力头部也不必再等。
func (m *Manager) newSession(pb *playback, src MediaSource, item library.Item, title string) *session {
	ctx, cancel := context.WithCancel(context.Background())
	return &session{
		src:          src,
		item:         item,
		player:       pb.player,
		danmakuReady: make(chan struct{}),
		assTarget:    filepath.Join(m.opts.RuntimeDir, "danmaku-"+random.Hex(8)+".ass"),
		ctx:          ctx,
		cancel:       cancel,
		title:        title,
		danmaku:      DanmakuInfo{State: "loading", Reason: "正在匹配弹幕…"},
	}
}

// episodeLoop 是窗口事件循环里「这一集」的状态。窗口里换集时整个换掉。
//
// 弹幕轨编排要等两个条件都满足：mpv 已加载文件（能 sub-add）+ 后台弹幕已解析
// 完（assPath 定型）。任一先到都不动手，避免对空 assPath 或未就绪的播放器操作。
type episodeLoop struct {
	sess            *session
	fileLoaded      bool
	danmakuResolved bool
	arranged        bool
	// danmakuReady 是 sess.danmakuReady 的本地副本：收到一次后置 nil，让该 case 之后永久阻塞
	// （关闭的通道恒可读，不置 nil 会把 select 转成忙等）。
	danmakuReady <-chan struct{}
	// fallback：file-loaded 事件可能被丢（Events 通道满时丢旧），到点兜底认定播放器已就绪。
	fallback    *time.Timer
	lastFlush   time.Time
	sawEOF      bool   // 这一集播到了尾（keep-open 下跟着 EOF 的沿，拖回去就清掉）
	endedReason string // mpv 报的最近一次结束原因
	// fenced：刚在窗口里换到这一集，新文件还没开始（start-file / file-loaded 都还没到）。
	// 这期间通道里的位置、暂停、结束、到尾事件都是上一个文件的，一律丢掉；界面消息照常处理。
	fenced bool
	// premature：到尾时已判定为网络流提前断供（已经关窗），窗口收尾时照此记一次播放失败。
	premature bool
	// closing：到尾之后由我们关窗（提前断供或没有下一集了）。之后 eof-reached 落下是文件被卸载，
	// 不是用户拖回去了，不能把 sawEOF 清掉。
	closing bool
	// sidecar 是换过来的这一集要外挂的对白字幕，文件加载好时 sub-add 一次（开播那一集走 --sub-file）。
	sidecar string
}

func newEpisodeLoop(sess *session) *episodeLoop {
	return &episodeLoop{
		sess:         sess,
		danmakuReady: sess.danmakuReady,
		fallback:     time.NewTimer(trackArrangeFallback),
	}
}

func (l *episodeLoop) stop() { l.fallback.Stop() }

// note 记下这一集的结束状态（窗口终结时据此判断看完与否、是不是播放失败）。
//
// keep-open=no 时每个文件播完都有一个 Ended，跟着它走。开着 keep-open 时还要跟 EOF / EOFCleared
// 的沿：播到尾 mpv 停在最后一帧不结束文件，拖回去再播到尾不会再来 Ended（每个文件只发一次），
// 停在尾巴上换集也可能没有 Ended(stop)。
func (l *episodeLoop) note(ev mpv.Event, keepOpen bool) {
	switch ev.Kind {
	case mpv.EventEnded:
		l.endedReason = ev.Reason
		if ev.Reason == "eof" {
			l.sawEOF = true
		}
	case mpv.EventEOF:
		if keepOpen {
			l.sawEOF, l.endedReason = true, "eof"
		}
	case mpv.EventEOFCleared:
		if keepOpen && !l.closing {
			l.sawEOF, l.endedReason = false, ""
		}
	}
}

// passFence 报告 ev 该不该交给这一集处理，以及这条事件是不是让栅栏落下了。
// 栅栏在新文件的 start-file 或 file-loaded（先到的那个）落下。
func (l *episodeLoop) passFence(ev mpv.Event) (pass, lifted bool) {
	if !l.fenced {
		return true, false
	}
	switch ev.Kind {
	case mpv.EventFileStarted, mpv.EventFileLoaded:
		l.fenced = false
		return true, true
	case mpv.EventUIMessage:
		return true, false
	default:
		return false, false
	}
}

// windowLoop 是 watchWindow 那个 goroutine 独占的状态：正在播的这一集的 episodeLoop 与切集状态。
// 只在窗口事件循环里读写，不加锁。
type windowLoop struct {
	pb   *playback
	loop *episodeLoop
	nav  navState
}

// watchWindow 消费 mpv 事件直到窗口终结：编排轨道、节流写进度、处理界面消息、在窗口里换集、终态收敛。
func (m *Manager) watchWindow(pb *playback) {
	defer close(pb.finished)
	w := &windowLoop{pb: pb, loop: newEpisodeLoop(pb.episode())}
	defer func() { w.loop.stop() }()

	for {
		select {
		case ev := <-pb.player.Events():
			m.handleEvent(w, ev)
		case <-w.loop.danmakuReady:
			w.loop.danmakuResolved = true
			w.loop.danmakuReady = nil
			m.maybeArrange(w)
		case <-w.loop.fallback.C:
			m.fileReady(w)
		case <-w.nav.countdownC:
			m.countdownFired(w)
		case <-w.nav.slowC:
			m.preparingSlow(w)
		case res := <-w.nav.prepared:
			m.commitSwitch(w, res)
		case <-pb.player.Done():
			drainFinalEvents(w)
			m.endWindow(w)
			return
		}
	}
}

// drainFinalEvents 在窗口终结后把通道里剩下的事件读完，只取结束状态。
//
// Done 与 Events 同时可读时 select 随机挑一个：mpv 先报 eof 再断开，若先挑中 Done，
// 那条 Ended(eof) 就丢了 —— 网络流提前断掉（下载停滞）因此记不成播放失败，在线来源的
// 自动换源也就不会触发。事件是在 Done 关闭之前投递的，这时一定都在通道里。
// 其余事件要对着活的播放器处理（推界面状态、编排字幕轨），窗口已经没了，不再处理。
func drainFinalEvents(w *windowLoop) {
	for {
		select {
		case ev := <-w.pb.player.Events():
			if pass, _ := w.loop.passFence(ev); pass {
				w.loop.note(ev, w.nav.keepOpen)
			}
		default:
			return
		}
	}
}

// handleEvent 处理一条 mpv 事件。在窗口的事件循环里同步调用（可以发 IPC 命令）。
func (m *Manager) handleEvent(w *windowLoop, ev mpv.Event) {
	loop := w.loop
	pass, lifted := loop.passFence(ev)
	if lifted {
		m.liftFence(w)
	}
	if !pass {
		return
	}
	loop.note(ev, w.nav.keepOpen)
	switch ev.Kind {
	case mpv.EventFileLoaded:
		m.fileReady(w)
	case mpv.EventUIMessage:
		m.handleWindowUIMessage(w, ev.Args)
	case mpv.EventTimePos:
		if time.Since(loop.lastFlush) >= progressFlushInterval {
			m.flushProgress(loop.sess, false)
			loop.lastFlush = time.Now()
		}
		m.probeUI(w)
	case mpv.EventEOF:
		m.reachedEOF(w)
	case mpv.EventEOFCleared:
		m.leftEOF(w)
	}
}

// fileReady 是「播放器里这一集的文件加载好了」：file-loaded 到了，或兜底定时器到点（事件被丢了，
// 或者第一个文件在 IPC 连上之前就加载完了）。换过来的这一集先挂外挂对白字幕，
// 再编排弹幕轨 —— 编排要数对白字幕有几条。
func (m *Manager) fileReady(w *windowLoop) {
	loop := w.loop
	// 已经加载好了就不必再兜底（否则 8 秒后再推一遍状态）
	loop.fallback.Stop()
	if loop.fenced {
		// file-loaded 与 start-file 都被丢了（通道满时丢旧）：到点就当新文件已经开始
		loop.fenced = false
		m.liftFence(w)
	}
	loop.fileLoaded = true
	m.addSidecar(loop)
	m.pushWindowState(w)
	m.maybeArrange(w)
}

// handleWindowUIMessage 处理界面消息：切集相关的在这里（要动窗口状态），其余交给 handleUIMessage。
func (m *Manager) handleWindowUIMessage(w *windowLoop, args []string) {
	if len(args) == 0 || m.handleNavMessage(w, args[0]) {
		return
	}
	ready := args[0] == "ready"
	if ready {
		w.pb.uiReady.Store(true)
	}
	m.handleUIMessage(w.loop.sess, args)
	if ready {
		m.syncKeepOpen(w)
	}
}

// maybeArrange 在文件加载好、弹幕也解析完之后编排一次字幕轨。
func (m *Manager) maybeArrange(w *windowLoop) {
	loop := w.loop
	if loop.arranged || !loop.fileLoaded || !loop.danmakuResolved {
		return
	}
	loop.arranged = true
	m.arrangeTracks(loop.sess)
	// 弹幕匹配完标题也可能升级成了作品名：编排完再推一次
	m.pushWindowState(w)
}

// pushWindowState 把这一集的状态推给界面，并据此重新决定 keep-open。
//
// 推得过去就说明界面脚本已经加载好了：mpv 对不存在的脚本 script-message-to 会报错。界面加载好时
// 发的 ready 可能在 IPC 连上之前就发完了（第一个文件的 file-loaded 也一样），丢了它不能让这个窗口
// 永远不接下一集。
func (m *Manager) pushWindowState(w *windowLoop) {
	if m.pushUIState(w.loop.sess) {
		w.pb.uiReady.Store(true)
	}
	m.syncKeepOpen(w)
}

// endWindow 是窗口终结（mpv 播完退出、用户关窗、被 Stop）之后的收尾，顺序有讲究：
//
//  1. 这一集收尾：最后一次进度落盘、删弹幕文件，「看完」回写放到后台；
//  2. OnSessionEnd（磁力停做种删分片）—— 不必等账号回写，但必须在 finished 关闭之前：
//     Stop 等的正是 finished，回调先跑完，换会话时新旧才不会重叠（磁力场景下重叠意味着
//     新种子刚建好就被旧会话的收尾停掉）。按窗口算，每个窗口恰好一次：窗口里换集不调它；
//  3. 有限期地等后台回写；
//  4. 同一把锁里发布 PlaybackFailure 并摘下 current。回写失败必须在「不在播了」的同时就
//     看得见：网页一读到 playing=false 就停止轮询，横幅读的就是那最后一次的 sync；
//  5. finished 由 watchWindow 的 defer 关闭。
func (m *Manager) endWindow(w *windowLoop) {
	pb, loop := w.pb, w.loop
	sess := loop.sess
	m.abandonSwitch(w)
	st := pb.player.State()
	// 在线流被服务器掐断、磁力流断了供给时 mpv 同样报 eof：这种 eof 不能算看完
	// （否则会把账号标成已看），而是一次来源故障。
	premature := loop.premature || prematureEOF(sess.src, st, loop.sawEOF)
	endedByEOF := loop.sawEOF && !premature
	if loop.fenced {
		// 换集刚发出、新文件还没开始窗口就没了：播放器里的位置还是上一个文件的，
		// 不能落到这一集的进度上
		st, premature, endedByEOF = mpv.State{}, false, false
	}
	m.endEpisode(pb, sess, st, endedByEOF)
	pb.cancel()

	playerErr := pb.player.Err()
	if playerErr != nil {
		log.Printf("player: 会话异常结束：%v", playerErr)
	}
	failure := playbackFailureFor(sess.src, sess.item.FileID, loop.endedReason, playerErr, premature, time.Now().UnixMilli())
	if failure != nil {
		failure.PlaybackID = pb.id
	}

	if m.opts.OnSessionEnd != nil {
		m.opts.OnSessionEnd()
	}
	if !waitTimeout(&pb.bg, markWatchedTimeout) {
		log.Printf("player: 回写账号在 %s 内没做完，不再等它", markWatchedTimeout)
	}

	m.mu.Lock()
	if failure != nil {
		m.lastPlaybackFailure = failure
	}
	if m.current == pb {
		m.current = nil
	}
	m.mu.Unlock()
}

// endEpisode 是一集的收尾，每集恰好一次：按调用方取的播放器快照 st 写最后一次进度，
// 删掉这一集的弹幕 ASS，再把「看完」回写放到后台。
//
// 不取消这一集的 ctx：关窗之后后台匹配照样做完、落盘（见 newSession）。窗口里换集时，
// 换集那边在这之后再调 sess.cancel()。
func (m *Manager) endEpisode(pb *playback, sess *session, st mpv.State, endedByEOF bool) {
	sess.endOnce.Do(func() {
		m.flushProgressAt(sess, st, endedByEOF)
		// 后台解析可能在此之后才落盘（关窗时它照样做完），残留由启动时的 SweepRuntimeDir 兜底清掉。
		if sess.assTarget != "" {
			_ = os.Remove(sess.assTarget)
		}
		m.kickSync(pb, sess)
	})
}

// kickSync 把「看完」回写放到后台（要走网络，最长 markWatchedTimeout），每集至多一次。
// 只在窗口的事件循环里调用：pb.bg 的 Add 与窗口收尾时的 Wait 在同一个 goroutine 里，先后有序。
func (m *Manager) kickSync(pb *playback, sess *session) {
	sess.syncOnce.Do(func() {
		pb.bg.Add(1)
		go func() {
			defer pb.bg.Done()
			m.syncWatched(sess)
		}()
	})
}

// waitTimeout 等 wg 归零，最多等 d；返回是否等到了。
func waitTimeout(wg *sync.WaitGroup, d time.Duration) bool {
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return true
	case <-time.After(d):
		return false
	}
}
