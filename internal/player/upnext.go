// 播完接下一集：keep-open 的开关、到尾时的「下一集」卡片与倒计时、界面上的切集消息。
//
// keep-open 的启动参数一直是 no（没有下一集、没有界面时播完即关窗，与以前一样）。只有界面脚本
// 已经加载好（能画卡片）而且确实有下一集时，才在运行时打开它：mpv 播到尾停在最后一帧，
// 留出倒计时的时间。最后一集再关回去。
//
// 停在最后一帧之后，窗口要么换到下一集，要么由 nagare 关掉 —— 停在那里时再把 keep-open 关掉，
// mpv（0.41 实测）并不会因此结束文件、关窗，窗口就卡在最后一帧上。所以下一集没能打开时，
// 还有下一集就挂一张不会自己消失的错误卡片（重试 / 关闭窗口），没有了就照「最后一集播完」关窗。
package player

import (
	"log"
	"math"
	"time"
)

// upNextDelay 是「下一集」卡片的倒计时：到点自动切过去。测试里改短。
var upNextDelay = 5 * time.Second

// uiProbeInterval：界面脚本加载好时发的 ready 可能在 IPC 连上之前就发完了。有下一集、还没认定
// 界面就绪时，播放中隔这么久试推一次状态，推得过去就认定就绪（见 pushWindowState）。测试里改短。
var uiProbeInterval = time.Second

// upNextCard 是推给界面的「下一集」卡片（script-message-to nagare upnext <json>）。
type upNextCard struct {
	// Phase：countdown（倒计时，到点自动播）/ preparing（正在准备，磁力缓冲之类）/
	// error（下一集没能打开：不会自己消失，等用户点重试或关闭窗口）。
	Phase   string `json:"phase"`
	Label   string `json:"label"`
	Seconds int    `json:"seconds,omitempty"`
	// Title / Reason 只在 error 时有：哪一步没成（「下一集没能打开」），以及为什么、能做什么。
	Title  string `json:"title,omitempty"`
	Reason string `json:"reason,omitempty"`
}

// cardPhaseError 是错误卡片的 phase。
const cardPhaseError = "error"

// countdownSeconds 是卡片上显示的秒数（向上取整，至少 1）。
func countdownSeconds() int {
	return max(1, int(math.Ceil(upNextDelay.Seconds())))
}

// parkedAtEOF：keep-open 开着，这一集播到了尾、停在最后一帧（还没由我们关窗，也不是刚换过来的一集）。
func (w *windowLoop) parkedAtEOF() bool {
	return w.nav.keepOpen && w.loop.sawEOF && !w.loop.closing && !w.loop.fenced
}

// syncKeepOpen 按「界面已就绪且有下一集」开关 keep-open。界面 ready、每个文件加载好、邻居变了时调。
// 设不上只记日志：退化成播完即关窗，⏭ 照常可用。
//
// 已经停在最后一帧时下一集没了：关掉 keep-open 不会让 mpv 关窗，由我们关（与最后一集播完一样）。
func (m *Manager) syncKeepOpen(w *windowLoop) {
	want := w.pb.uiReady.Load() && w.loop.sess.navSnapshot().Next != nil
	if want == w.nav.keepOpen {
		return
	}
	if !want && w.parkedAtEOF() {
		log.Printf("player: 停在最后一帧时下一集没了，关窗")
		m.closeWindow(w)
		return
	}
	if err := w.pb.player.SetKeepOpen(want); err != nil {
		log.Printf("player: 设置 keep-open=%v 失败（播完不会停下来等下一集，⏭ 照常可用）：%v", want, err)
		return
	}
	w.nav.keepOpen = want
}

// probeUI 在播放中补认界面就绪（ready 丢了的时候）；没有下一集就不必认，keep-open 不会开。
// 界面脚本根本没加载（配置目录没准备好、用的是 mpv 自带的控制条）时每次都推不过去：
// 每个窗口只记一次日志，不每秒刷一条。
func (m *Manager) probeUI(w *windowLoop) {
	if m.opts.Navigator == nil || w.pb.uiReady.Load() || w.loop.sess.navSnapshot().Next == nil {
		return
	}
	if time.Since(w.nav.lastProbe) < uiProbeInterval {
		return
	}
	w.nav.lastProbe = time.Now()
	if err := m.sendUIState(w.loop.sess); err != nil {
		if !w.nav.probeWarned {
			w.nav.probeWarned = true
			log.Printf("player: 播放器界面没有响应，播完不会停下来等下一集（这个窗口不再重复记录）：%v", err)
		}
		return
	}
	w.pb.uiReady.Store(true)
	m.syncKeepOpen(w)
}

// reachedEOF 处理播到尾（eof-reached 的上升沿）。keep-open 关着时 mpv 自己结束文件、关窗，照旧。
//
// 开着 keep-open 时 mpv 停在最后一帧，由这里决定接下来：
//   - 网络流在远没播完时就到了尾（下载停滞、连接断了）：这是来源故障，关窗并记一次播放失败；
//   - 否则这一集就是看完了：立刻把「看完」落盘并回写（不等换集或关窗 —— 用户也可能就停在这里），
//     有下一集就出倒计时卡片，下一集不见了就照常关窗。
func (m *Manager) reachedEOF(w *windowLoop) {
	if !w.nav.keepOpen {
		return
	}
	pb, loop := w.pb, w.loop
	sess := loop.sess
	st := pb.player.State()
	if prematureEOF(sess.src, st, true) {
		loop.premature = true
		m.closeWindow(w)
		return
	}
	// Completed 是粘的：之后拖回去再关窗也还是看完；回写每集至多一次（syncOnce）。
	// 弹幕匹配还没回来就先不回写（没有匹配不知道写到哪部作品），留给这一集收尾时再试。
	m.flushProgressAt(sess, st, true)
	if loop.danmakuResolved {
		m.kickSync(pb, sess)
	}
	if w.nav.phase != switchIdle {
		// 用户在最后一刻按了 ⏭ / ⏮，正在准备：不另起倒计时。准备失败时见 switchFailed。
		return
	}
	next := sess.navSnapshot().Next
	if next == nil {
		m.closeWindow(w)
		return
	}
	m.startCountdown(w, next)
}

// leftEOF 处理 eof-reached 落下（用户从尾巴上拖回去了）：「下一集」那一套悄悄收掉 —— 倒计时、
// 由倒计时或卡片上的按钮发起的准备、错误卡片。用户自己按 ⏭ 发起的准备不动（他就是要换集）。
func (m *Manager) leftEOF(w *windowLoop) {
	if w.loop.closing {
		// 我们关的窗：eof-reached 落下是文件被卸载，不是用户拖回去了
		return
	}
	dismiss := m.stopCountdown(w)
	if w.nav.phase == switchPreparing && w.nav.auto {
		m.cancelPrep(w)
		dismiss = true
	}
	if dismiss || w.nav.errorCard {
		m.clearCard(w)
	}
}

// startCountdown 出「下一集」卡片并开始倒计时（倒计时由这里计，界面只负责显示）。
func (m *Manager) startCountdown(w *windowLoop, next *Neighbor) {
	m.stopCountdown(w)
	m.showCard(w, upNextCard{Phase: "countdown", Label: next.Label, Seconds: countdownSeconds()})
	w.nav.countdown = time.NewTimer(upNextDelay)
	w.nav.countdownC = w.nav.countdown.C
}

// stopCountdown 停掉倒计时；返回它是不是正在倒计时。
func (m *Manager) stopCountdown(w *windowLoop) bool {
	if w.nav.countdown == nil {
		return false
	}
	w.nav.countdown.Stop()
	w.nav.countdown, w.nav.countdownC = nil, nil
	return true
}

// countdownFired：倒计时到点，切到下一集。
func (m *Manager) countdownFired(w *windowLoop) {
	w.nav.countdown, w.nav.countdownC = nil, nil
	m.requestSwitch(w, DirectionNext, true)
}

// cancelUpNext：用户在卡片上点了取消（倒计时、正在准备、错误卡片都一样）。停在最后一帧（暂停着），不关窗。
func (m *Manager) cancelUpNext(w *windowLoop) {
	m.stopCountdown(w)
	if w.nav.phase == switchPreparing {
		m.cancelPrep(w)
	}
	m.clearCard(w)
}

// retryUpNext：错误卡片上点了重试。马上换成「正在准备」（卡片本来就挂着，不等 preparingCardDelay）。
// 卡片已经不是错误卡片了（收起了、换成别的了）就不理：迟到的点击不能凭空发起换集。
func (m *Manager) retryUpNext(w *windowLoop) {
	if !w.nav.errorCard {
		return
	}
	m.requestSwitch(w, DirectionNext, true)
	if w.nav.phase != switchPreparing {
		return
	}
	if w.nav.slow != nil {
		w.nav.slow.Stop()
	}
	m.preparingSlow(w)
}

// navMessages 是界面上与切集有关的消息（见 docs/player-ui.md）。
var navMessages = map[string]bool{
	"prev": true, "next": true, "upnext-now": true, "upnext-cancel": true, "upnext-retry": true, "upnext-close": true,
}

// handleNavMessage 处理界面上的切集消息；不是切集消息时返回 false。没有 Navigator 时一概不理
// （窗口里本来就没有切集，界面不会出卡片）。
func (m *Manager) handleNavMessage(w *windowLoop, name string) bool {
	if !navMessages[name] {
		return false
	}
	if m.opts.Navigator == nil {
		return true
	}
	switch name {
	case "prev":
		m.requestSwitch(w, DirectionPrev, false)
	case "next":
		m.requestSwitch(w, DirectionNext, false)
	case "upnext-now":
		m.requestSwitch(w, DirectionNext, true)
	case "upnext-retry":
		m.retryUpNext(w)
	case "upnext-close":
		m.closeWindow(w)
	case "upnext-cancel":
		m.cancelUpNext(w)
	}
	return true
}

// showCard 让界面显示「下一集」卡片。推不过去（界面脚本没响应）只记日志：倒计时照样走。
func (m *Manager) showCard(w *windowLoop, card upNextCard) {
	if err := w.pb.player.SendUIUpNext(card); err != nil {
		log.Printf("player: 推送「下一集」卡片失败：%v", err)
	}
	w.nav.cardShown = true
	w.nav.errorCard = card.Phase == cardPhaseError
}

// clearCard 收起「下一集」卡片（没挂着就什么都不发）。
func (m *Manager) clearCard(w *windowLoop) {
	if !w.nav.cardShown {
		return
	}
	w.nav.cardShown, w.nav.errorCard = false, false
	if err := w.pb.player.ClearUIUpNext(); err != nil {
		log.Printf("player: 收起「下一集」卡片失败：%v", err)
	}
}

// closeWindow 由 nagare 关掉窗口：停在最后一帧之后（网络流提前断供、没有下一集了）或用户在错误卡片上
// 点了关闭。在途的换集一并放弃（关窗途中不能再 loadfile）；收尾照常走 Done → endWindow。
func (m *Manager) closeWindow(w *windowLoop) {
	if w.loop.closing {
		return
	}
	w.loop.closing = true
	m.abandonSwitch(w)
	closeAsync(w.pb)
}

// closeAsync 关掉窗口但不在事件循环里等它退出（Close 最长要等几秒）：事件循环接着跑，
// 由 Done 走正常的窗口收尾。
func closeAsync(pb *playback) {
	go func() {
		if err := pb.player.Close(); err != nil {
			log.Printf("player: 关闭 mpv 失败：%v", err)
		}
	}()
}
