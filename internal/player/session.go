// 播放会话监督器：轨道编排（对白字幕主轨 + 弹幕副轨）、进度节流回写、
// 结束收敛。「用户手动关掉 mpv」这类失败模式在这里兜住 —— 状态收敛，
// 进度按最后已知位置写回，看完则回写 animego 账号。
package player

import (
	"context"
	"log"
	"os"
	"sync"
	"time"

	"github.com/nagare-project/nagare/internal/library"
	"github.com/nagare-project/nagare/internal/mpv"
	"github.com/nagare-project/nagare/internal/store"
)

// progressFlushInterval 是播放中进度落盘的节流间隔。
const progressFlushInterval = 5 * time.Second

// trackArrangeFallback：file-loaded 事件迟迟不来时兜底做一次轨道编排。
const trackArrangeFallback = 8 * time.Second

// danmakuTrackTitle 是弹幕轨的标题，用于在 track-list 里找回自己的轨。
const danmakuTrackTitle = "nagare-danmaku"

// markWatchedTimeout 是结束时回写 animego 的时限（不拖住退出流程）。
const markWatchedTimeout = 10 * time.Second

type session struct {
	// 不可变字段（创建后只读，无需加锁）：
	// src 是媒体来源（本地文件或磁力流），后台弹幕解析要用它算 16MB 哈希；
	// item 是 src.Item() 的快照 —— 会话内所有下游只认这一份。
	src      MediaSource
	item     library.Item
	player   *mpv.Player
	finished chan struct{} // watcher 完成最终回写后关闭
	// danmakuReady 在后台弹幕解析结束后关闭 —— 无论成功、失败还是无弹幕。
	// watcher 靠它知道 assPath/binding 已定型，可以编排字幕轨了。
	danmakuReady chan struct{}
	// assTarget 是本会话弹幕 ASS 的落点，每会话唯一 —— 快速切集时两个后台
	// 解析不会撞同一个文件。会话终结时删除。
	assTarget string

	// 可变字段（Play 后台 goroutine 与 watcher 并发访问，一律走 mu）：
	mu      sync.Mutex
	title   string
	binding store.Binding
	assPath string
	danmaku DanmakuInfo
}

func (s *session) danmakuInfo() DanmakuInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.danmaku
}

func (s *session) titleSnapshot() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.title
}

func (s *session) bindingSnapshot() store.Binding {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.binding
}

func (s *session) assPathSnapshot() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.assPath
}

// applyDanmaku 是后台解析的落点：一次性写入匹配结果、字幕路径与弹幕状态。
func (s *session) applyDanmaku(binding store.Binding, title, assPath string, dan DanmakuInfo) {
	s.mu.Lock()
	s.binding = binding
	if title != "" {
		s.title = title
	}
	s.assPath = assPath
	s.danmaku = dan
	s.mu.Unlock()
}

func (s *session) degradeDanmaku(reason string) {
	s.mu.Lock()
	s.danmaku = DanmakuInfo{State: "degraded", Reason: reason}
	s.mu.Unlock()
	log.Printf("player: 弹幕降级：%s", reason)
}

// watch 消费 mpv 事件直到会话终结：编排轨道、节流写进度、终态收敛。
//
// 弹幕轨编排要等两个条件都满足：mpv 已加载文件（能 sub-add）+ 后台弹幕已解析
// 完（assPath 定型）。任一先到都不动手，避免对空 assPath 或未就绪的播放器操作。
func (m *Manager) watch(sess *session) {
	defer close(sess.finished)

	var arrangeOnce sync.Once
	arrange := func() { arrangeOnce.Do(func() { m.arrangeTracks(sess) }) }
	fileLoaded, danmakuResolved := false, false
	maybeArrange := func() {
		if fileLoaded && danmakuResolved {
			arrange()
		}
	}
	// 本地副本：收到一次后置 nil，让该 case 之后永久阻塞（关闭的通道恒可读，
	// 不置 nil 会把 select 转成忙等）。
	danmakuReady := sess.danmakuReady
	fallback := time.NewTimer(trackArrangeFallback)
	defer fallback.Stop()

	var lastFlush time.Time
	endedByEOF := false

	for {
		select {
		case ev := <-sess.player.Events():
			switch ev.Kind {
			case mpv.EventFileLoaded:
				fileLoaded = true
				maybeArrange()
			case mpv.EventTimePos:
				if time.Since(lastFlush) >= progressFlushInterval {
					m.flushProgress(sess, false)
					lastFlush = time.Now()
				}
			case mpv.EventEnded:
				if ev.Reason == "eof" {
					endedByEOF = true
				}
			}
		case <-danmakuReady:
			danmakuResolved = true
			danmakuReady = nil
			maybeArrange()
		case <-fallback.C:
			// file-loaded 事件可能被丢（Events 通道满时丢旧），兜底认定播放器已就绪。
			fileLoaded = true
			maybeArrange()
		case <-sess.player.Done():
			m.finalize(sess, endedByEOF)
			// mpv 自然退出（播完/用户关窗）时收敛 current，Status 不再误报在播。
			m.mu.Lock()
			if m.current == sess {
				m.current = nil
			}
			m.mu.Unlock()
			// 放在函数体里而不是 defer 里：sess.finished 由顶部的 defer 关闭，
			// 而 Stop 等的正是那个信号 —— 回调必须先跑完，换会话时新旧才不会重叠
			// （磁力场景下重叠意味着新种子刚建好就被旧会话的收尾停掉）。
			if m.opts.OnSessionEnd != nil {
				m.opts.OnSessionEnd()
			}
			return
		}
	}
}

// mpvTrack 是 track-list 属性里我们关心的字段。
type mpvTrack struct {
	ID       int    `json:"id"`
	Type     string `json:"type"`
	Title    string `json:"title"`
	External bool   `json:"external"`
}

// arrangeTracks 完成双字幕轨编排：
//
//	有对白字幕（内嵌或外挂）→ 对白保持主轨，弹幕挂副轨，
//	  并设 secondary-sub-ass-override=no 保住 \move 样式（mpv 0.36+）；
//	没有对白字幕 → 弹幕直接当主轨（完整样式）。
//
// 任何一步失败都只降级弹幕，绝不影响播放本身。
func (m *Manager) arrangeTracks(sess *session) {
	assPath := sess.assPathSnapshot()
	if assPath == "" {
		return
	}
	// 第三参 "auto"：只挂载不选中，选择权留给下面的编排。
	if _, err := sess.player.Command("sub-add", assPath, "auto", danmakuTrackTitle); err != nil {
		sess.degradeDanmaku("挂载弹幕轨失败：" + err.Error())
		return
	}

	var tracks []mpvTrack
	if err := sess.player.GetProperty("track-list", &tracks); err != nil {
		sess.degradeDanmaku("读取轨道列表失败：" + err.Error())
		return
	}
	danmakuID, dialogueSubs := 0, 0
	for _, t := range tracks {
		if t.Type != "sub" {
			continue
		}
		if t.External && t.Title == danmakuTrackTitle {
			danmakuID = t.ID
			continue
		}
		dialogueSubs++
	}
	if danmakuID == 0 {
		sess.degradeDanmaku("弹幕轨挂载后未在轨道列表中出现")
		return
	}

	if dialogueSubs > 0 {
		// 先开样式直通再选副轨，避免闪一帧被剥样式的纯文本。
		if err := sess.player.SetProperty("secondary-sub-ass-override", "no"); err != nil {
			// 老 mpv 的副轨会剥掉全部 ASS 样式，弹幕会糊成居中纯文本 —— 宁可停用。
			sess.degradeDanmaku("当前 mpv 不支持带样式的第二字幕轨（需 0.36+，建议升级 mpv），弹幕已停用")
			return
		}
		if err := sess.player.SetProperty("secondary-sid", danmakuID); err != nil {
			sess.degradeDanmaku("选中弹幕副轨失败：" + err.Error())
			return
		}
	} else {
		if err := sess.player.SetProperty("sid", danmakuID); err != nil {
			sess.degradeDanmaku("选中弹幕轨失败：" + err.Error())
			return
		}
	}
}

// progressUpdate 计算一次进度落盘的内容与是否需要写（纯函数，可直接单测）：
//   - 位置小于 resumeMinSec 且未看完 → 不写。这同时挡住「续播后 mpv 秒退、
//     time-pos 还停在 0」把此前真实进度砸掉的场景（对齐网页端策略）；
//   - Completed 一旦为 true 就保持（重看不清除已看完标记）；
//   - Synced 跟随旧值（回写成功后由 finalize 单独置位）。
func progressUpdate(prev store.Progress, hadPrev bool, st mpv.State, endedByEOF bool, nowMs int64) (store.Progress, bool) {
	completed := endedByEOF || (st.Duration > 0 && st.TimePos >= completionRatio*st.Duration)
	if hadPrev && prev.Completed {
		completed = true
	}
	if st.TimePos < resumeMinSec && !completed {
		return store.Progress{}, false
	}
	return store.Progress{
		PositionSec: st.TimePos,
		DurationSec: st.Duration,
		UpdatedAt:   nowMs,
		Completed:   completed,
		Synced:      hadPrev && prev.Synced,
	}, true
}

// flushProgress 把当前位置写进 store（按 progressUpdate 的规则）。
func (m *Manager) flushProgress(sess *session, endedByEOF bool) {
	st := sess.player.State()
	prev, hadPrev := m.opts.Store.Progress(sess.item.FileID)
	p, write := progressUpdate(prev, hadPrev, st, endedByEOF, time.Now().UnixMilli())
	if !write {
		return
	}
	if err := m.opts.Store.SetProgress(sess.item.FileID, p); err != nil {
		log.Printf("player: 写进度失败：%v", err)
	}
}

// finalize 是会话终态：最后一次进度回写 + 看完标记回写 animego 账号。
func (m *Manager) finalize(sess *session, endedByEOF bool) {
	m.flushProgress(sess, endedByEOF)

	if err := sess.player.Err(); err != nil {
		log.Printf("player: 会话异常结束：%v", err)
	}

	// 清掉本会话的弹幕 ASS。极少数情况下后台解析在此之后才落盘，残留由
	// 启动时的 SweepRuntimeDir 兜底清掉。
	if sess.assTarget != "" {
		_ = os.Remove(sess.assTarget)
	}

	p, ok := m.opts.Store.Progress(sess.item.FileID)
	if !ok || !p.Completed || p.Synced {
		return
	}
	b := sess.bindingSnapshot()
	if b.AnilistID <= 0 || b.Episode <= 0 || m.opts.Client == nil || !m.opts.Client.LoggedIn() {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), markWatchedTimeout)
	defer cancel()
	err := m.opts.Client.EnsureSubscription(ctx, b.AnilistID)
	if err == nil {
		err = m.opts.Client.MarkWatched(ctx, b.AnilistID, b.Episode)
	}
	// 无论成败都持久化会话：401 自动刷新可能已轮换 refresh cookie。
	if m.opts.PersistSession != nil {
		m.opts.PersistSession()
	}
	if err != nil {
		log.Printf("player: 回写看完标记失败（下次退出时重试）：%v", err)
		return
	}
	p.Synced = true
	if err := m.opts.Store.SetProgress(sess.item.FileID, p); err != nil {
		log.Printf("player: 记录同步状态失败：%v", err)
	}
	log.Printf("player: 已回写 animego：anilistId=%d 第%d集", b.AnilistID, b.Episode)
}
