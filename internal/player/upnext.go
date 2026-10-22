// 播完接下一集：keep-open 的开关、到尾时的「下一集」卡片与倒计时、界面上的切集消息。
//
// keep-open 的启动参数一直是 no（没有下一集、没有界面时播完即关窗，与以前一样）。只有界面脚本
// 已经加载好（能画卡片）而且确实有下一集时，才在运行时打开它：mpv 播到尾停在最后一帧，
// 留出倒计时的时间。最后一集再关回去。
package player

import (
	"log"
	"math"
	"time"
)

// upNextDelay 是「下一集」卡片的倒计时：到点自动切过去。测试里改短。
var upNextDelay = 5 * time.Second

// uiProbeInterval：界面脚本加载好时发的 ready 可能在 IPC 连上之前就发完了。有下一集、还没认定
// 界面就绪时，播放中隔这么久试推一次状态，推得过去就认定就绪（见 pushWindowState）。
const uiProbeInterval = time.Second

// upNextCard 是推给界面的「下一集」卡片（script-message-to nagare upnext <json>）。
type upNextCard struct {
	// Phase：countdown（倒计时，到点自动播）/ preparing（正在准备，磁力缓冲之类）。
	Phase   string `json:"phase"`
	Label   string `json:"label"`
	Seconds int    `json:"seconds,omitempty"`
}

// countdownSeconds 是卡片上显示的秒数（向上取整，至少 1）。
func countdownSeconds() int {
	return max(1, int(math.Ceil(upNextDelay.Seconds())))
}

// syncKeepOpen 按「界面已就绪且有下一集」开关 keep-open。界面 ready、每个文件加载好、邻居变了时调。
// 设不上只记日志：退化成播完即关窗，⏭ 照常可用。
func (m *Manager) syncKeepOpen(w *windowLoop) {
	want := w.pb.uiReady.Load() && w.loop.sess.navSnapshot().Next != nil
	if want == w.nav.keepOpen {
		return
	}
	if err := w.pb.player.SetKeepOpen(want); err != nil {
		log.Printf("player: 设置 keep-open=%v 失败（播完不会停下来等下一集，⏭ 照常可用）：%v", want, err)
		return
	}
	w.nav.keepOpen = want
}

// probeUI 在播放中补认界面就绪（ready 丢了的时候）；没有下一集就不必认，keep-open 不会开。
func (m *Manager) probeUI(w *windowLoop) {
	if m.opts.Navigator == nil || w.pb.uiReady.Load() || w.loop.sess.navSnapshot().Next == nil {
		return
	}
	if time.Since(w.nav.lastProbe) < uiProbeInterval {
		return
	}
	w.nav.lastProbe = time.Now()
	m.pushWindowState(w)
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
		loop.premature, loop.closing = true, true
		closeAsync(pb)
		return
	}
	// Completed 是粘的：之后拖回去再关窗也还是看完；回写每集至多一次（syncOnce）。
	// 弹幕匹配还没回来就先不回写（没有匹配不知道写到哪部作品），留给这一集收尾时再试。
	m.flushProgressAt(sess, st, true)
	if loop.danmakuResolved {
		m.kickSync(pb, sess)
	}
	if w.nav.phase != switchIdle {
		return
	}
	next := sess.navSnapshot().Next
	if next == nil {
		loop.closing = true
		closeAsync(pb)
		return
	}
	m.startCountdown(w, next)
}

// leftEOF 处理 eof-reached 落下（用户从尾巴上拖回去了）：倒计时悄悄取消。
func (m *Manager) leftEOF(w *windowLoop) {
	if m.stopCountdown(w) {
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
	m.requestSwitch(w, DirectionNext)
}

// cancelUpNext：用户在卡片上点了取消。停在最后一帧（暂停着），不关窗。
func (m *Manager) cancelUpNext(w *windowLoop) {
	m.stopCountdown(w)
	if w.nav.phase == switchPreparing {
		m.cancelPrep(w)
	}
	m.clearCard(w)
}

// handleNavMessage 处理界面上的切集消息；不是切集消息时返回 false。
func (m *Manager) handleNavMessage(w *windowLoop, name string) bool {
	switch name {
	case "prev":
		m.requestSwitch(w, DirectionPrev)
	case "next", "upnext-now":
		m.requestSwitch(w, DirectionNext)
	case "upnext-cancel":
		m.cancelUpNext(w)
	default:
		return false
	}
	return true
}

// showCard 让界面显示「下一集」卡片。推不过去（界面脚本没响应）只记日志：倒计时照样走。
func (m *Manager) showCard(w *windowLoop, card upNextCard) {
	if err := w.pb.player.SendUIUpNext(card); err != nil {
		log.Printf("player: 推送「下一集」卡片失败：%v", err)
	}
	w.nav.cardShown = true
}

// clearCard 收起「下一集」卡片（没挂着就什么都不发）。
func (m *Manager) clearCard(w *windowLoop) {
	if !w.nav.cardShown {
		return
	}
	w.nav.cardShown = false
	if err := w.pb.player.ClearUIUpNext(); err != nil {
		log.Printf("player: 收起「下一集」卡片失败：%v", err)
	}
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
