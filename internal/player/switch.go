// 同一个 mpv 窗口里换集：准备（Navigator.Open，在 goroutine 里）→ 提交（LoadNext、换掉这一集）→
// 栅栏（丢掉上一个文件留在通道里的事件）→ 空闲。全部状态只在窗口的事件循环里读写。
package player

import (
	"context"
	"errors"
	"log"
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
	// 准备中：取消函数、结果通道（缓冲 1：窗口没了也不会把准备的 goroutine 卡住）、要切去的邻居。
	prepCancel context.CancelFunc
	prepared   chan prepResult
	target     *Neighbor
	// 准备得慢时出「正在准备」卡片的定时器。
	slow  *time.Timer
	slowC <-chan time.Time
	// cardShown：界面上挂着「下一集」卡片，换集结束或取消时要收起。
	cardShown bool
	// lastProbe 是上一次试探界面是否就绪的时刻（见 probeUI）。
	lastProbe time.Time
}

// prepResult 是后台准备下一集的结果。
type prepResult struct {
	dir    Direction
	opened Opened
	err    error
}

// requestSwitch 开始往 dir 方向换集（界面上的 ⏮ ⏭、倒计时到点、「立即播放」）。
func (m *Manager) requestSwitch(w *windowLoop, dir Direction) {
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
		if m.stopCountdown(w) {
			m.clearCard(w)
		}
		return
	}
	m.stopCountdown(w)

	ctx, cancel := context.WithCancel(w.pb.ctx)
	ch := make(chan prepResult, 1)
	w.nav.phase, w.nav.prepCancel, w.nav.prepared, w.nav.target = switchPreparing, cancel, ch, target
	w.nav.slow = time.NewTimer(preparingCardDelay)
	w.nav.slowC = w.nav.slow.C
	nav, src := m.opts.Navigator, sess.src
	// 准备放到 goroutine 里：本地文件的 os.Stat 在网络盘上可能卡住，磁力要等缓冲 ——
	// 事件循环不能停下来（界面消息、取消、窗口关闭都要照常处理）。
	go func() {
		opened, err := nav.Open(ctx, src, dir)
		ch <- prepResult{dir: dir, opened: opened, err: err}
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
// 迟到的结果落进一个已经没人读的缓冲通道，直接丢掉。
func (m *Manager) cancelPrep(w *windowLoop) {
	if w.nav.prepCancel != nil {
		w.nav.prepCancel()
	}
	w.nav.prepCancel, w.nav.prepared, w.nav.target = nil, nil, nil
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
		m.clearCard(w)
		text, sub := switchFailureText(res.err, openFailedMsg(res.dir))
		log.Printf("player: %s：%v", openFailedMsg(res.dir), res.err)
		m.uiToast(old, text, sub)
		// 邻居可能已经不在了（重扫过、文件被删）：重问一次，界面上的按钮跟着变
		m.refreshNav(w)
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
		m.clearCard(w)
		_, sub := switchFailureText(err, "")
		log.Printf("player: %s：%v", loadFailedMsg(res.dir), err)
		m.uiToast(old, loadFailedMsg(res.dir), sub)
		return
	}

	m.endEpisode(pb, old, st, w.loop.sawEOF && !premature)
	old.cancel()
	sess := m.newSession(pb, src, item, title)
	sess.setNav(m.neighbors(src))
	pb.setEpisode(sess)
	w.loop.stop()
	w.loop = newEpisodeLoop(sess)
	w.loop.fenced, w.loop.sidecar = true, res.opened.SubPath
	w.nav.phase = switchFenced
	m.clearCard(w)
	go m.resolveDanmaku(sess)
}

// liftFence 在新文件开始之后调用：栅栏落下，换集结束。界面状态等文件加载好再推（fileReady）。
func (m *Manager) liftFence(w *windowLoop) {
	if w.nav.phase == switchFenced {
		w.nav.phase = switchIdle
	}
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
