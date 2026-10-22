// 一集的播放：轨道编排（对白字幕主轨 + 弹幕副轨）、进度回写、看完回写 animego 账号。
// 窗口级的事件循环与结束收敛在 playback.go —— 「用户手动关掉 mpv」这类失败模式在那里兜住。
package player

import (
	"context"
	"log"
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

// session 是一集：窗口（playback，见 playback.go）里每播一个文件就是一个 session。
// 进度、弹幕、匹配、「看完」回写都按集算；同一个窗口里换集时旧的一集收尾、新的一集开始。
type session struct {
	// 不可变字段（创建后只读，无需加锁）：
	// src 是媒体来源（本地文件或磁力流），后台弹幕解析要用它算 16MB 哈希；
	// item 是 src.Item() 的快照 —— 这一集里所有下游只认这一份。
	src  MediaSource
	item library.Item
	// player 是所在窗口的播放器（与 playback.player 同一个，只读副本）：轨道编排、推界面状态、
	// 截图都按「这一集」做，拿着它就不必回头找窗口。
	player *mpv.Player
	// danmakuReady 在后台弹幕解析结束后关闭 —— 无论成功、失败还是无弹幕。
	// 事件循环靠它知道 assPath/binding 已定型，可以编排字幕轨了。
	danmakuReady chan struct{}
	// assTarget 是这一集弹幕 ASS 的落点，每集唯一 —— 快速切集时两个后台
	// 解析不会撞同一个文件。这一集收尾时删除。
	assTarget string
	// ctx 跟着这一集，后台弹幕解析用它。关窗时不取消（解析照样做完、落盘，见 newSession）；
	// 只有在同一个窗口里换到别的集时，才由换集那边调 cancel 让被换走那一集的解析收手。
	ctx    context.Context
	cancel context.CancelFunc
	// endOnce：收尾（最后一次进度、删弹幕文件、回写账号）每集恰好一次。
	endOnce sync.Once
	// syncOnce：「看完」回写每集至多发一次。
	syncOnce sync.Once

	// 可变字段（后台弹幕解析与事件循环并发访问，一律走 mu）：
	mu      sync.Mutex
	title   string
	binding store.Binding
	assPath string
	danmaku DanmakuInfo
	// nav 是这一集的前后邻居（开播时问一次 Navigator；切集失败时重问）。界面状态从网页那边
	// （RefreshShaders）也会推，所以同样走 mu。
	nav Neighbors
}

func (s *session) navSnapshot() Neighbors {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.nav
}

func (s *session) setNav(n Neighbors) {
	s.mu.Lock()
	s.nav = n
	s.mu.Unlock()
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

func playbackFailureFor(src MediaSource, fileID, endedReason string, playerErr error, premature bool, at int64) *PlaybackFailure {
	if premature {
		reason := "媒体流在播完前就断了（下载停滞或连接中断），这一集没有记为看完"
		if _, online := src.(*remoteSource); online {
			// 在线候选的自动换源靠这条失败接着试下一个来源，措辞照旧
			reason = "在线媒体提前结束，已停止当前来源"
		}
		return &PlaybackFailure{FileID: fileID, Reason: reason, At: at}
	}
	if endedReason != "error" && playerErr == nil {
		return nil
	}
	return &PlaybackFailure{
		FileID: fileID,
		Reason: "媒体加载或播放失败",
		At:     at,
	}
}

// prematureEOF 判定网络流是否在远未播完时就到达 eof —— 服务器掐断连接、HLS 分片缺失、
// 磁力流断了供给（分享者走光、写盘失败停了下载、会话被收掉）时，mpv 不报错而是正常结束。
// 只对经 HTTP 交给 mpv 的流判定（在线候选与磁力边下边播）：本地文件时长估计偶有偏差，
// 误报会让用户看到一条并不存在的故障。
func prematureEOF(src MediaSource, st mpv.State, endedByEOF bool) bool {
	if !isNetworkStream(src.MPVPath()) || !endedByEOF || st.Duration <= 0 {
		return false
	}
	return st.TimePos < completionRatio*st.Duration
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
	// 用户在播放器里关过弹幕：这一集也先关着（界面上的「弹」按钮随时能再打开）
	if m.opts.Store.PlayerConfig().DanmakuHidden {
		visibility := "secondary-sub-visibility"
		if dialogueSubs == 0 {
			visibility = "sub-visibility"
		}
		if err := sess.player.SetProperty(visibility, false); err != nil {
			log.Printf("player: 按上次的设置关掉弹幕失败：%v", err)
		}
	}
}

// progressUpdate 计算一次进度落盘的内容与是否需要写（纯函数，可直接单测）：
//   - 位置小于 resumeMinSec 且未看完 → 不写。这同时挡住「续播后 mpv 秒退、
//     time-pos 还停在 0」把此前真实进度砸掉的场景（对齐网页端策略）；
//   - Completed 一旦为 true 就保持（重看不清除已看完标记）；
//   - Synced 跟随旧值（回写成功后由 syncWatched 单独置位）。
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
		// 写进账号的是哪一集跟着「已回写」一起保留（改关联时要列出来）
		SyncedEpisode: syncedEpisodeOf(prev, hadPrev),
	}, true
}

// flushProgress 把播放器当前位置写进 store（按 progressUpdate 的规则）。
func (m *Manager) flushProgress(sess *session, endedByEOF bool) {
	m.flushProgressAt(sess, sess.player.State(), endedByEOF)
}

// flushProgressAt 按调用方给的播放器快照写这一集的进度。收尾时快照由调用方取：
// 同一个窗口换集之后播放器的位置已经是下一集的了，不能再现读。
func (m *Manager) flushProgressAt(sess *session, st mpv.State, endedByEOF bool) {
	prev, hadPrev := m.opts.Store.Progress(sess.item.FileID)
	p, write := progressUpdate(prev, hadPrev, st, endedByEOF, time.Now().UnixMilli())
	if !write {
		return
	}
	if err := m.opts.Store.SetProgress(sess.item.FileID, p); err != nil {
		log.Printf("player: 写进度失败：%v", err)
	}
}

// syncWatched 把「这一集看完了」回写 animego 账号。
//
// 单独一个函数而不是塞在收尾（endEpisode）里：它是收尾几件事中唯一会
// 走网络、也是唯一会失败给用户看的一件，值得能被单独测（测它不需要 mpv）。
// 收尾经 kickSync 把它放到后台跑，不拖住窗口的事件循环。
func (m *Manager) syncWatched(sess *session) {
	p, ok := m.opts.Store.Progress(sess.item.FileID)
	if !ok || !p.Completed || p.Synced {
		return
	}
	b := sess.bindingSnapshot()
	if b.AnilistID <= 0 || b.Episode <= 0 || m.opts.Client == nil || !m.opts.Client.LoggedIn() {
		return
	}
	// 会话里的匹配是开播时的快照。收尾跑在停止播放之后，这期间用户可能已经在媒体库里
	// 改了作品关联（它会作废这条匹配）—— 那就不能再按旧匹配回写到旧作品上。
	if !m.bindingStillCurrent(sess.item.FileID, b) {
		log.Printf("player: 作品关联已改，不按旧匹配回写（anilistId=%d 第%d集）", b.AnilistID, b.Episode)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), markWatchedTimeout)
	defer cancel()
	// 文件里的集号 → 作品自己的集号（跨季连续编号的第二季「38」是第 10 集）
	place := m.placeEpisode(ctx, sess.item.FileID, b.AnilistID, b.Episode)
	if place.Episode <= 0 {
		recovery := "先确认它是作品的第几集，再在 animego 网站上手动标记（不要按文件里的编号标）"
		if place.Retry {
			recovery = "重看这一集看完时会再试"
		}
		m.reportSync(b, place.Reason, recovery)
		log.Printf("player: 集号无法换算成作品集号，不回写（anilistId=%d 文件里第%d集）", b.AnilistID, b.Episode)
		return
	}
	// 查集数可能等了一阵，再核对一次：这期间关联被改了就不写
	if !m.bindingStillCurrent(sess.item.FileID, b) {
		log.Printf("player: 作品关联已改，不按旧匹配回写（anilistId=%d 第%d集）", b.AnilistID, b.Episode)
		return
	}
	if err := m.markWatched(ctx, b.AnilistID, place.Episode); err != nil {
		// 只记日志是不够的：这条失败发生在 mpv 已经退出之后，用户面前
		// 什么都不会变 —— 他以为这一集记上了，下次打开网站才发现没有，
		// 而那时已经不知道是哪一集丢的。挂进 Status 让界面说出来。
		reason, recovery := classifySyncErr(err)
		m.reportSync(b, reason, recovery)
		// 措辞不再写「下次退出时重试」——那是过度承诺。重试只发生在
		// 重看【这一集】并再次看完时（上面那个 p.Synced 判断只在那一集的会话里跑）。
		log.Printf("player: 回写看完标记失败（重看这一集看完时会再试）：%v", err)
		return
	}
	// 成功就把上一次的失败留痕清掉，横幅跟着消失，不用用户手动关。
	m.setSyncFailure(nil)
	// 回写途中关联被改了：这一集已经写到了旧作品上，但不能标成「已回写」——
	// 否则重看这一集看完时也不会再按新关联写一次。
	if !m.bindingStillCurrent(sess.item.FileID, b) {
		log.Printf("player: 回写途中作品关联已改，这一集不标记为已回写")
		return
	}
	m.recordSynced(sess.item.FileID, place.Episode)
	if place.Episode != b.Episode {
		log.Printf("player: 已回写 animego：anilistId=%d 第%d集（文件里是第%d集）", b.AnilistID, place.Episode, b.Episode)
	} else {
		log.Printf("player: 已回写 animego：anilistId=%d 第%d集", b.AnilistID, place.Episode)
	}
}

// markWatched 先确保订阅存在，再标记这一集看完（episode 是作品集号）。
func (m *Manager) markWatched(ctx context.Context, anilistID, episode int) error {
	if err := m.opts.Client.EnsureSubscription(ctx, anilistID); err != nil {
		return err
	}
	return m.opts.Client.MarkWatched(ctx, anilistID, episode)
}

// reportSync 把「这一集没记到账号」挂进 Status，媒体库页据此出横幅。
// Episode 是文件里的集号 —— 用户在界面上看到的是它。
func (m *Manager) reportSync(b store.Binding, reason, recovery string) {
	m.setSyncFailure(&SyncInfo{
		State:    "failed",
		Title:    b.Title,
		Episode:  b.Episode,
		Reason:   reason,
		Recovery: recovery,
		At:       time.Now().UnixMilli(),
	})
}

// recordSynced 标记这一集已回写，并记下写进账号的作品集号。重读一次再置位：
// 网络请求期间进度可能被别处改过，不能用开头那份整个盖回去。
func (m *Manager) recordSynced(fileID string, episode int) {
	p, ok := m.opts.Store.Progress(fileID)
	if !ok {
		return
	}
	p.Synced, p.SyncedEpisode = true, episode
	if err := m.opts.Store.SetProgress(fileID, p); err != nil {
		log.Printf("player: 记录同步状态失败：%v", err)
	}
}

// syncedEpisodeOf：进度更新时，「写进账号的是哪一集」跟着「已回写」一起保留。
func syncedEpisodeOf(prev store.Progress, hadPrev bool) int {
	if hadPrev && prev.Synced {
		return prev.SyncedEpisode
	}
	return 0
}

// bindingStillCurrent：store 里这个文件的匹配仍是会话开播时那一条（同一部作品、同一集），
// 并且与文件现在的作品关联不冲突。改关联会作废不一致的匹配；后台匹配迟到时
// 又可能把它写回来，所以关联本身也要再看一眼。
func (m *Manager) bindingStillCurrent(fileID string, b store.Binding) bool {
	cur, ok := m.opts.Store.Binding(fileID)
	return ok && cur.AnilistID == b.AnilistID && cur.Episode == b.Episode && m.agreesWithAssociation(fileID, b)
}
