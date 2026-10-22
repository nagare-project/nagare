// 同一个 mpv 窗口里换集：准备（Navigator.Open，在 goroutine 里）→ 提交（LoadNext、换掉这一集）→
// 栅栏（丢掉上一个文件留在通道里的事件）→ 空闲。全部状态只在窗口的事件循环里读写。
package player

import (
	"context"
	"errors"
	"log"
	"strings"
	"sync"
	"time"

	errs "github.com/nagare-project/nagare/internal/errors"
	"github.com/nagare-project/nagare/internal/mpv"
	"github.com/nagare-project/nagare/internal/store"
)

// preparingCardDelay：准备下一集超过这么久还没好（磁力要缓冲、网络盘 stat 卡住），界面上出
// 「正在准备」卡片。本地文件通常远在这之前就好了，不闪一下卡片。测试里可改短。
var preparingCardDelay = 400 * time.Millisecond

// switchPhase 是窗口里换集的阶段：idle → preparing → committing → fenced → idle。
type switchPhase int

const (
	switchIdle switchPhase = iota
	// switchPreparing：Navigator.Open 在后台跑，结果从 navState.prepared 回来。
	switchPreparing
	// switchCommitting：在事件循环里同步发 LoadNext、换掉这一集（瞬时，只在 commitSwitch 里出现）。
	switchCommitting
	// switchFenced：loadfile 已发出，新文件还没开始；见 episodeLoop.fenced。
	switchFenced
)

// navState 是窗口事件循环里与切集有关的状态（windowLoop.nav），只在那个 goroutine 里读写。
type navState struct {
	phase switchPhase
	// keepOpen：运行时开着 keep-open=yes（见 syncKeepOpen）。从没开过的窗口从不发 keep-open。
	keepOpen bool
	// 「下一集」倒计时；countdownC 为 nil 表示没在倒计时（select 里 nil 通道永远阻塞）。
	countdown  *time.Timer
	countdownC <-chan time.Time
	// 准备中：取消函数、结果通道（缓冲 1：窗口没了也不会把准备的 goroutine 卡住）与它的交接处、要切去的邻居。
	prepCancel context.CancelFunc
	prepared   chan prepResult
	prepSlot   *prepSlot
	target     *Neighbor
	// 准备得慢时出「正在准备」卡片的定时器。
	slow  *time.Timer
	slowC <-chan time.Time
	// auto：正在准备的这次换集是「下一集」那一套发起的（倒计时到点、卡片上的立即播放 / 重试），
	// 不是用户自己按的 ⏭。用户从尾巴上拖回去时只收掉前者（见 leftEOF）。
	auto bool
	// cardShown：界面上挂着「下一集」卡片，换集结束或取消时要收起。errorCard：挂着的是错误卡片
	// （下一集没能打开，等重试或关窗）。两者都只由 showCard / clearCard 改。
	cardShown bool
	errorCard bool
	// lastProbe 是上一次试探界面是否就绪的时刻；probeWarned：试探失败已经记过一次日志（见 probeUI）。
	lastProbe   time.Time
	probeWarned bool
}

// prepResult 是后台准备下一集的结果。
type prepResult struct {
	dir    Direction
	opened Opened
	err    error
}

// prepSlot 是准备结果的交接处。准备的 goroutine 交结果、事件循环放弃这次准备，两者在同一把锁下：
// 结果要么被事件循环取走，要么由放弃的一方拿去 Discard，不会落进一个没人读的通道 —— 磁力那边
// 已经换到了下一集的文件，没人告诉它窗口没跟上，它就一直在下那一集。
type prepSlot struct {
	mu        sync.Mutex
	abandoned bool
	ch        chan prepResult // 缓冲 1，每个交接处只交一次
}

// deliver 交出结果；这次准备已经被放弃时返回 false（结果归调用方处理）。
func (p *prepSlot) deliver(res prepResult) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.abandoned {
		return false
	}
	p.ch <- res
	return true
}

// abandon 放弃这次准备；结果已经交了、事件循环还没取走时把它交给调用方。
func (p *prepSlot) abandon() (prepResult, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.abandoned = true
	select {
	case res := <-p.ch:
		return res, true
	default:
		return prepResult{}, false
	}
}

// discardOpened 告诉来源：准备好的这一集没换上去。在后台调（磁力要等引擎的锁）。
func discardOpened(res prepResult) {
	if res.err == nil && res.opened.Discard != nil {
		go res.opened.Discard()
	}
}

// requestSwitch 开始往 dir 方向换集。auto 为 true 是「下一集」那一套发起的（倒计时到点、卡片上的
// 立即播放 / 重试），false 是用户按的 ⏮ ⏭。
func (m *Manager) requestSwitch(w *windowLoop, dir Direction, auto bool) {
	if m.opts.Navigator == nil {
		return
	}
	sess := w.loop.sess
	if w.nav.phase != switchIdle {
		m.uiToast(sess, "正在切换…", "")
		return
	}
	target := sess.navSnapshot().toward(dir)
	if target == nil {
		// 界面在没有邻居时自己会提示；这里只是不做事（比如邻居刚被重扫掉、界面还没收到新状态）
		log.Printf("player: 没有%s可切，忽略", dirWord(dir))
		if dir == DirectionNext && w.parkedAtEOF() {
			// 停在最后一帧等下一集，下一集却已经没了：照最后一集播完关窗，不留一个卡住的窗口
			m.closeWindow(w)
			return
		}
		if m.stopCountdown(w) {
			m.clearCard(w)
		}
		return
	}
	m.stopCountdown(w)

	ctx, cancel := context.WithCancel(w.pb.ctx)
	ch := make(chan prepResult, 1)
	slot := &prepSlot{ch: ch}
	w.nav.phase, w.nav.prepCancel, w.nav.prepared, w.nav.target = switchPreparing, cancel, ch, target
	w.nav.prepSlot = slot
	w.nav.auto = auto
	w.nav.slow = time.NewTimer(preparingCardDelay)
	w.nav.slowC = w.nav.slow.C
	nav, src := m.opts.Navigator, sess.src
	// 准备放到 goroutine 里：本地文件的 os.Stat 在网络盘上可能卡住，磁力要等缓冲 ——
	// 事件循环不能停下来（界面消息、取消、窗口关闭都要照常处理）。
	go func() {
		opened, err := nav.Open(ctx, src, dir)
		res := prepResult{dir: dir, opened: opened, err: err}
		if !slot.deliver(res) {
			discardOpened(res) // 准备好的时候这次换集已经被放弃了
		}
	}()
}

// preparingSlow：准备超过 preparingCardDelay 还没好，出「正在准备」卡片。
func (m *Manager) preparingSlow(w *windowLoop) {
	w.nav.slow, w.nav.slowC = nil, nil
	if w.nav.phase != switchPreparing || w.nav.target == nil {
		return
	}
	m.showCard(w, upNextCard{Phase: "preparing", Label: w.nav.target.Label})
}

// cancelPrep 放弃正在准备的换集（用户在卡片上点了取消、窗口没了、准备的结果已经取回）。
// 已经交出、还没取走的结果，或者之后才准备好的结果，都交给 Discard（见 prepSlot）。
func (m *Manager) cancelPrep(w *windowLoop) {
	if w.nav.prepCancel != nil {
		w.nav.prepCancel()
	}
	if w.nav.prepSlot != nil {
		if res, ok := w.nav.prepSlot.abandon(); ok {
			discardOpened(res)
		}
	}
	w.nav.prepCancel, w.nav.prepared, w.nav.prepSlot, w.nav.target, w.nav.auto = nil, nil, nil, nil, false
	if w.nav.slow != nil {
		w.nav.slow.Stop()
	}
	w.nav.slow, w.nav.slowC = nil, nil
	if w.nav.phase == switchPreparing {
		w.nav.phase = switchIdle
	}
}

// abandonSwitch 在窗口终结时停掉一切在途的换集（不再对播放器发命令：窗口已经没了）。
func (m *Manager) abandonSwitch(w *windowLoop) {
	m.cancelPrep(w)
	m.stopCountdown(w)
}

// commitSwitch 拿到准备好的邻居集，在同一个窗口里换过去。
//
// 顺序：取播放器快照 → LoadNext（失败就留在这一集，什么都不动）→ 这一集收尾（按快照写进度、
// 后台回写看完）→ 取消这一集的 ctx（它的弹幕解析没人要了）→ 换上新的一集、立起栅栏 → 后台解析弹幕。
// 收尾放在 LoadNext 成功之后：换不过去时这一集还得原样播下去，最后关窗时照常收尾。
func (m *Manager) commitSwitch(w *windowLoop, res prepResult) {
	m.cancelPrep(w)
	pb, old := w.pb, w.loop.sess
	if res.err != nil {
		title := openFailedMsg(res.dir)
		log.Printf("player: %s：%v", title, res.err)
		text, sub := switchFailureText(res.err, title)
		m.switchFailed(w, failedSwitch{dir: res.dir, text: text, sub: sub, title: title, reason: cardReason(res.err, title)})
		return
	}

	w.nav.phase = switchCommitting
	src := res.opened.Source
	item := src.Item()
	title := pickTitle(store.Binding{}, item)
	startAt := m.resumeAt(item.FileID)
	st := pb.player.State()
	premature := w.loop.premature || prematureEOF(old.src, st, w.loop.sawEOF)
	if err := pb.player.LoadNext(mpv.NextFile{Path: src.MPVPath(), Title: title, StartAt: startAt}); err != nil {
		w.nav.phase = switchIdle
		discardOpened(res) // 留在这一集：磁力要把种子换回来
		failed := loadFailedMsg(res.dir)
		log.Printf("player: %s：%v", failed, err)
		_, sub := switchFailureText(err, "")
		m.switchFailed(w, failedSwitch{dir: res.dir, text: failed, sub: sub, title: failed, reason: cardReason(err, failed)})
		return
	}

	m.endEpisode(pb, old, st, w.loop.sawEOF && !premature)
	old.cancel()
	sess := m.newSession(pb, src, item, title)
	sess.setNav(m.neighbors(src))
	// 先立栅栏再换集：Status 一读到新的一集，位置就已经按「还没开始」报
	pb.fenced.Store(true)
	pb.setEpisode(sess)
	w.loop.stop()
	w.loop = newEpisodeLoop(sess)
	w.loop.fenced, w.loop.sidecar = true, res.opened.SubPath
	w.nav.phase = switchFenced
	m.clearCard(w)
	m.startResolve(sess)
}

// liftFence 在新文件开始之后调用：栅栏落下，换集结束。界面状态等文件加载好再推（fileReady）。
func (m *Manager) liftFence(w *windowLoop) {
	if w.nav.phase == switchFenced {
		w.nav.phase = switchIdle
	}
	w.pb.fenced.Store(false)
	// keep-open 停在上一集最后一帧时是暂停着的，pause 是全局属性：不放开的话新的一集也停着
	if err := w.pb.player.SetPause(false); err != nil {
		log.Printf("player: 换集后继续播放失败：%v", err)
	}
}

// addSidecar 给换过来的这一集挂上配对的外挂对白字幕并选中它（开播那一集走 --sub-file，不经过这里）。
// 挂不上不影响播放：对白退回内嵌字幕，说一声。
func (m *Manager) addSidecar(loop *episodeLoop) {
	path := loop.sidecar
	if path == "" {
		return
	}
	loop.sidecar = ""
	if _, err := loop.sess.player.Command("sub-add", path, "select"); err != nil {
		log.Printf("player: 这一集的外挂字幕没挂上：%v", err)
		m.uiToast(loop.sess, "这一集的外挂字幕没能加载", "照常播放，可以在字幕菜单里换一条")
	}
}

// refreshNav 重问这一集的邻居，推给界面，并据此重新决定 keep-open。
func (m *Manager) refreshNav(w *windowLoop) {
	sess := w.loop.sess
	sess.setNav(m.neighbors(sess.src))
	m.pushWindowState(w)
}

// failedSwitch 是一次没换成的切集：还在播这一集时弹的屏幕提示（text / sub），停在最后一帧时
// 挂的错误卡片（title / reason）。
type failedSwitch struct {
	dir           Direction
	text, sub     string
	title, reason string
}

// switchFailed 处理换集失败（下一集打不开、LoadNext 没发出去），这一集原样留着。
//
// 还在播这一集：弹一条屏幕提示就够了，用户接着看。停在最后一帧等下一集（keep-open 开着）时，
// 两秒的提示一过窗口就卡在最后一帧上 —— 所以重问一次邻居：没有下一集了就照最后一集播完关窗
// （refreshNav → syncKeepOpen 那一步就关了），还有就挂错误卡片等用户点重试或关闭窗口。
// 上一集没换成不算：那是用户自己按的，停在哪里都由他接着操作。
func (m *Manager) switchFailed(w *windowLoop, f failedSwitch) {
	parked := f.dir == DirectionNext && w.parkedAtEOF()
	if !parked {
		m.clearCard(w)
		m.uiToast(w.loop.sess, f.text, f.sub)
	}
	// 邻居可能已经不在了（重扫过、文件被删）：重问一次，界面上的按钮跟着变
	m.refreshNav(w)
	if !parked || w.loop.closing {
		return
	}
	next := w.loop.sess.navSnapshot().Next
	if next == nil {
		m.closeWindow(w)
		return
	}
	m.showCard(w, upNextCard{Phase: cardPhaseError, Label: next.Label, Title: f.title, Reason: f.reason})
}

// cardReason 是错误卡片上的说明：分类错误的说法（与卡片标题重复时略过）加恢复动作；
// 其余错误没有可说的细节，给一句通用的。
func cardReason(err error, title string) string {
	var ce *errs.E
	if errors.As(err, &ce) {
		var parts []string
		if ce.UserMsg != "" && ce.UserMsg != title {
			parts = append(parts, ce.UserMsg)
		}
		if ce.Recovery != "" {
			parts = append(parts, ce.Recovery)
		}
		if len(parts) > 0 {
			return strings.Join(parts, "。")
		}
	}
	return "可以重试，或者关掉窗口回到网页上选这一集"
}

// switchFailureText 是切集失败的屏幕提示：分类错误用它自己的说法与恢复动作，其余用 fallback。
func switchFailureText(err error, fallback string) (text, sub string) {
	text, sub = fallback, "还在播这一集"
	var ce *errs.E
	if errors.As(err, &ce) {
		if ce.UserMsg != "" {
			text = ce.UserMsg
		}
		if ce.Recovery != "" {
			sub = ce.Recovery
		}
	}
	return text, sub
}

func dirWord(dir Direction) string {
	if dir == DirectionPrev {
		return "上一集"
	}
	return "下一集"
}

func openFailedMsg(dir Direction) string { return dirWord(dir) + "没能打开" }

func loadFailedMsg(dir Direction) string { return "没能切到" + dirWord(dir) }
