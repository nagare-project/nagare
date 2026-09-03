// Package player 是播放编排层：把 库条目 → 懒哈希 → dandanplay 匹配 →
// 弹幕 ASS → mpv 启动 → 进度回写 串成一条链，并保证任何一环失败都
// 不阻断「本地文件照样能播」这条底线（决议 CQ3 的降级姿态）。
package player

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/nagare-project/nagare/internal/animego"
	"github.com/nagare-project/nagare/internal/danmaku"
	errs "github.com/nagare-project/nagare/internal/errors"
	"github.com/nagare-project/nagare/internal/library"
	"github.com/nagare-project/nagare/internal/mpv"
	"github.com/nagare-project/nagare/internal/random"
	"github.com/nagare-project/nagare/internal/store"
)

// SweepRuntimeDir 清掉上次运行残留的弹幕 ASS 与 mpv socket 私有目录
// （崩溃/SIGKILL 时来不及清理的）。启动时调用一次；不存在或删不掉都无所谓。
func SweepRuntimeDir(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		name := e.Name()
		switch {
		case e.IsDir() && strings.HasPrefix(name, "nagare-mpv-"):
			_ = os.RemoveAll(filepath.Join(dir, name))
		case !e.IsDir() && strings.HasPrefix(name, "danmaku-") && strings.HasSuffix(name, ".ass"):
			_ = os.Remove(filepath.Join(dir, name))
		case !e.IsDir() && strings.HasPrefix(name, "mpv-") && strings.HasSuffix(name, ".sock"):
			_ = os.Remove(filepath.Join(dir, name))
		}
	}
}

// AnimegoClient 是 Manager 需要的 animego 能力子集（接口收窄便于测试替身）。
type AnimegoClient interface {
	Match(ctx context.Context, in animego.MatchInput) (animego.MatchResult, error)
	Comments(ctx context.Context, episodeID int64) ([]danmaku.Comment, error)
	EnsureSubscription(ctx context.Context, anilistID int) error
	MarkWatched(ctx context.Context, anilistID, episode int) error
	LoggedIn() bool
}

// Launcher 抽象 mpv.Launch，测试注入假实现。
type Launcher func(ctx context.Context, opts mpv.LaunchOptions) (*mpv.Player, error)

// Options 是 Manager 的全部依赖。
type Options struct {
	Store      *store.Store
	Client     AnimegoClient // 可为 nil：纯离线（弹幕/匹配一律标不可用）
	MPV        *mpv.Runtime  // 共享探测状态；可为 nil（测试注入假 Launch 时不需要真实 mpv）
	RuntimeDir string        // 弹幕 ASS、IPC socket 等运行时文件目录
	Launch     Launcher      // 为 nil 时用 mpv.Launch
	// OnSessionEnd 在一次播放会话终结后调用（mpv 播完、用户关窗、或被 Stop）。
	//
	// 磁力播放靠它兑现「停止播放即停做种并删分片」：用户直接关掉 mpv 窗口时
	// 没有任何 API 请求发生，没有这个回调，种子会一直挂在那里下载和上传。
	// 播放层不认识磁力，装配层决定要不要收掉引擎。可为 nil。
	OnSessionEnd func()
}

// DanmakuInfo 是弹幕链路的结果状态 —— 失败必须可见（CQ3：不静默）。
type DanmakuInfo struct {
	State  string `json:"state"` // ok / unmatched / unavailable / degraded / none
	Count  int    `json:"count,omitempty"`
	Reason string `json:"reason,omitempty"` // 中文，用户可读
}

// SyncInfo 是「看完标记回写 animego 账号」的结果。
//
// 存在的理由与 DanmakuInfo 一样（CQ3：失败不静默），但它比弹幕更需要被看见：
// 弹幕没了用户当场就知道，而进度没同步上去【毫无迹象】—— 用户以为记上了，
// 下次打开网站发现还停在上一集，也不知道是哪一集丢的。
//
// 只在【失败】时存在。成功不报：每看完一集弹一次「已同步」是噪音，
// 而噪音会让人学会忽略这块区域，真出事时也就跟着被忽略了。
type SyncInfo struct {
	State string `json:"state"` // failed
	// Title / Episode 说清是哪一集没上去 —— 只说「同步失败」用户无从下手。
	Title    string `json:"title,omitempty"`
	Episode  int    `json:"episode,omitempty"`
	Reason   string `json:"reason,omitempty"`   // 中文，用户可读
	Recovery string `json:"recovery,omitempty"` // 用户能做的一步动作
	At       int64  `json:"at,omitempty"`       // 毫秒时间戳
}

// PlayResult 是发起播放的即时结果。
type PlayResult struct {
	Title   string      `json:"title"`
	Danmaku DanmakuInfo `json:"danmaku"`
}

// Status 是当前播放状态快照。
type Status struct {
	Playing  bool         `json:"playing"`
	FileID   string       `json:"fileId,omitempty"`
	Title    string       `json:"title,omitempty"`
	Position float64      `json:"position,omitempty"`
	Duration float64      `json:"duration,omitempty"`
	Paused   bool         `json:"paused,omitempty"`
	Danmaku  *DanmakuInfo `json:"danmaku,omitempty"`
	// Sync 是上一次回写账号失败的留痕，成功或从未失败时为 nil。
	// 它【不随会话结束消失】：失败发生在 mpv 已经退出之后，
	// 挂在会话上等于永远没人看得见。
	Sync *SyncInfo `json:"sync,omitempty"`
}

// Manager 串联一次播放会话的全生命周期；同一时刻至多一个会话。
//
// 两把锁的分工：startMu 串行化 Play/Stop 这类「换会话」操作（期间要等旧会话
// 回写完进度，耗时）；mu 只保护 current 指针本身（Status/watch 的快读快写）。
// 绝不在持 mu 时做任何可能阻塞的事。
type Manager struct {
	opts Options

	startMu sync.Mutex
	mu      sync.Mutex
	current *session
	// lastSync 是上一次回写账号失败的留痕（nil = 没有待处理的失败）。
	// 归 Manager 而不是 session：失败发生在会话已经结束之后。
	lastSync *SyncInfo
}

// New 构造 Manager。
func New(opts Options) *Manager {
	if opts.Launch == nil {
		opts.Launch = mpv.Launch
	}
	return &Manager{opts: opts}
}

// completionRatio：观看进度超过时长的九成视为看完（与网页端习惯一致）。
const completionRatio = 0.9

// resumeMinSec：小于 5 秒的进度不值得续播（对齐网页端 MIN_POSITION_SEC）。
const resumeMinSec = 5.0

// Play 停掉现有会话并播放 src 指向的媒体（本地文件或磁力流，见 source.go）。
// subPath 是配对的外挂对白字幕（可空，mpv 会选中它做主轨）。
// 匹配/弹幕失败不阻断播放，结果写进 DanmakuInfo。
func (m *Manager) Play(ctx context.Context, src MediaSource, subPath string) (PlayResult, error) {
	m.startMu.Lock()
	defer m.startMu.Unlock()
	m.takeAndStopCurrent()

	if err := src.Probe(ctx); err != nil {
		return PlayResult{}, err
	}

	// 元数据在这里取一次快照存进会话：整条链路（标题、进度、匹配、看完同步）
	// 都只认这一份，避免同一会话里前后两次 Item() 拿到不一致的值。
	item := src.Item()

	// 先起 mpv，再在后台匹配弹幕 —— animego 慢/挂都不能拖住本地播放（CQ3）。
	// 窗口标题先用文件名派生的本地标题，匹配到官方标题后由后台升级。
	title := pickTitle(store.Binding{}, item)

	startAt := 0.0
	if p, ok := m.opts.Store.Progress(item.FileID); ok {
		resumable := p.PositionSec > resumeMinSec &&
			(p.DurationSec <= 0 || p.PositionSec < completionRatio*p.DurationSec)
		if resumable {
			startAt = p.PositionSec
		}
	}

	mpvPath, err := m.mpvPath()
	if err != nil {
		return PlayResult{}, err
	}
	pl, err := m.opts.Launch(ctx, mpv.LaunchOptions{
		MPVPath:   mpvPath,
		MediaPath: src.MPVPath(),
		SubPath:   subPath,
		Title:     title,
		StartAt:   startAt,
		SocketDir: m.opts.RuntimeDir,
	})
	if err != nil {
		return PlayResult{}, errs.Wrap(errs.CategoryPlayback, "player.launch",
			"启动 mpv 失败", "确认 mpv 已安装且可运行（mpv --version）", err)
	}

	loading := DanmakuInfo{State: "loading", Reason: "正在匹配弹幕…"}
	sess := &session{
		src:          src,
		item:         item,
		player:       pl,
		finished:     make(chan struct{}),
		danmakuReady: make(chan struct{}),
		assTarget:    filepath.Join(m.opts.RuntimeDir, "danmaku-"+random.Hex(8)+".ass"),
		title:        title,
		danmaku:      loading,
	}
	m.mu.Lock()
	m.current = sess
	m.mu.Unlock()
	go m.watch(sess)
	go m.resolveDanmaku(sess)

	return PlayResult{Title: title, Danmaku: loading}, nil
}

// danmakuResolveTimeout 是后台弹幕解析的总时限（含懒哈希 + 匹配 + 拉取 + 写盘）。
//
// 之所以宽到 120 秒：本地文件算首 16MB 哈希是瞬时的，磁力却要等头部 16MB
// 从 swarm 到齐，30 秒经常不够 —— 一超时磁力播放就永远匹配不到弹幕。
// 这段是纯后台工作，不阻塞播放也不占用请求，放宽只影响一个后台 goroutine
// 的存活时长。
const danmakuResolveTimeout = 120 * time.Second

// resolveDanmaku 在后台完成 懒哈希 → 匹配 → 弹幕 ASS，把结果落回 session，
// 最后关闭 danmakuReady 通知 watcher 可以编排字幕轨了。用独立的后台 ctx：
// Play 的请求 ctx 会随响应返回而取消，不能用它跑这段延后工作。
func (m *Manager) resolveDanmaku(sess *session) {
	defer close(sess.danmakuReady)
	ctx, cancel := context.WithTimeout(context.Background(), danmakuResolveTimeout)
	defer cancel()

	binding, dan := m.ensureBinding(ctx, sess.src, sess.item)
	assPath := ""
	if dan.State == "ok" {
		if count, err := m.writeDanmakuASS(ctx, sess.assTarget, binding.DandanEpisodeID); err != nil {
			dan = DanmakuInfo{State: "unavailable", Reason: err.Error()}
		} else {
			assPath, dan.Count = sess.assTarget, count
		}
	}
	sess.applyDanmaku(binding, pickTitle(binding, sess.item), assPath, dan)
}

// ensureBinding 取（或建立）文件与 dandanplay 剧集的绑定；一并返回弹幕链路状态。
// 任何失败都只影响 DanmakuInfo，不影响播放。
//
// item 是调用方持有的 src.Item() 快照，一并传进来是为了不在这里重复取。
func (m *Manager) ensureBinding(ctx context.Context, src MediaSource, item library.Item) (store.Binding, DanmakuInfo) {
	if b, ok := m.opts.Store.Binding(item.FileID); ok && b.DandanEpisodeID != 0 {
		return b, DanmakuInfo{State: "ok"}
	}
	if m.opts.Client == nil {
		return store.Binding{}, DanmakuInfo{State: "none", Reason: "未配置 animego 服务"}
	}

	hash := m.opts.Store.Hash(item.FileID)
	if hash == "" {
		h, err := src.Hash16M(ctx)
		if err != nil {
			return store.Binding{}, DanmakuInfo{State: "unavailable", Reason: "计算文件指纹失败，弹幕匹配跳过"}
		}
		hash = h
		if err := m.opts.Store.SetHash(item.FileID, hash); err != nil {
			// 缓存写失败不致命：下次再算一遍。
			log.Printf("player: 缓存 hash 失败：%v", err)
		}
	}

	// 集号无法识别时【不猜 1】：猜错会把剧场版/OVA/特典当"第1集"匹配，
	// 播完还会拿 MarkWatched(anilistId, 1) 污染用户真实的 animego 账号。
	episode := 0
	if item.Episode != nil {
		episode = *item.Episode
	} else if item.ParsedNumber != nil {
		episode = *item.ParsedNumber
	}
	if episode <= 0 {
		return store.Binding{}, DanmakuInfo{State: "unmatched", Reason: "无法识别集号，弹幕匹配跳过"}
	}
	keyword := ""
	if item.ParsedTitle != nil {
		keyword = *item.ParsedTitle
	}

	res, err := m.opts.Client.Match(ctx, animego.MatchInput{
		FileName: item.FileName,
		FileHash: hash,
		FileSize: item.Size,
		Episode:  episode,
		Keyword:  keyword,
	})
	if err != nil {
		return store.Binding{}, DanmakuInfo{State: "unavailable", Reason: classifyAnimegoErr(err)}
	}
	if !res.Matched {
		return store.Binding{}, DanmakuInfo{State: "unmatched", Reason: "未匹配到对应剧集，弹幕不可用"}
	}
	ref, ok := res.EpisodeMap[episode]
	if !ok {
		return store.Binding{}, DanmakuInfo{State: "unmatched", Reason: fmt.Sprintf("匹配结果里没有第 %d 集", episode)}
	}

	b := store.Binding{
		AnilistID:       res.AnilistID,
		DandanEpisodeID: ref.DandanEpisodeID,
		Episode:         episode,
		Title:           firstNonEmpty(res.TitleChinese, res.TitleNative, keyword),
		EpisodeTitle:    ref.Title,
		CoverURL:        res.CoverImageURL,
		MatchedAt:       time.Now().UnixMilli(),
	}
	if err := m.opts.Store.SetBinding(item.FileID, b); err != nil {
		log.Printf("player: 保存匹配结果失败：%v", err)
	}
	return b, DanmakuInfo{State: "ok"}
}

// writeDanmakuASS 拉取弹幕并写到 path（每会话唯一，避免并发解析互相覆盖），
// 返回弹幕条数。
func (m *Manager) writeDanmakuASS(ctx context.Context, path string, episodeID int64) (int, error) {
	comments, err := m.opts.Client.Comments(ctx, episodeID)
	if err != nil {
		return 0, fmt.Errorf("拉取弹幕失败：%s", classifyAnimegoErr(err))
	}
	stats, err := danmaku.WriteFile(path, comments, danmaku.Options{})
	if err != nil {
		return 0, fmt.Errorf("生成弹幕字幕失败：%w", err)
	}
	return stats.Converted, nil
}

// mpvPath 从共享探测状态取 mpv 路径；未找到时给出带安装指引的播放类错误
// —— 用户装好后到设置页点「重新检测」即可，不必重启。Runtime 为 nil 时返回空路径，
// 交给 Launch 自行报错（测试注入假实现时走这里）。
func (m *Manager) mpvPath() (string, error) {
	if m.opts.MPV == nil {
		return "", nil
	}
	info, err := m.opts.MPV.Get()
	if err != nil {
		return "", errs.Wrap(errs.CategoryPlayback, "player.mpv",
			"mpv 不可用，无法播放", "按设置页的指引安装 mpv 后点「重新检测」", err)
	}
	return info.Path, nil
}

// Stop 停止当前会话（若有）并等待进度落盘。
func (m *Manager) Stop() {
	m.startMu.Lock()
	defer m.startMu.Unlock()
	m.takeAndStopCurrent()
}

// takeAndStopCurrent 摘下当前会话并停掉它；调用方必须持 startMu（不得持 mu）。
func (m *Manager) takeAndStopCurrent() {
	m.mu.Lock()
	sess := m.current
	m.current = nil
	m.mu.Unlock()
	if sess == nil {
		return
	}
	if err := sess.player.Close(); err != nil {
		// mpv 未能在限期内退出（可能需手动结束进程）——这是唯一会被吞掉的
		// 关闭错误，显式记一笔，别让卡死的 mpv 毫无痕迹。
		log.Printf("player: 关闭 mpv 失败：%v", err)
	}
	select {
	case <-sess.finished:
	case <-time.After(5 * time.Second):
		// watcher 卡死属程序缺陷，不该拖死调用方；进度可能少一拍。
	}
}

// SetPause 暂停/继续当前会话。
func (m *Manager) SetPause(v bool) error {
	m.mu.Lock()
	sess := m.current
	m.mu.Unlock()
	if sess == nil {
		return errs.New(errs.CategoryInput, "player.pause", "当前没有正在播放的内容", "")
	}
	return sess.player.SetPause(v)
}

// Seek 定位当前会话。
func (m *Manager) Seek(seconds float64) error {
	m.mu.Lock()
	sess := m.current
	m.mu.Unlock()
	if sess == nil {
		return errs.New(errs.CategoryInput, "player.seek", "当前没有正在播放的内容", "")
	}
	return sess.player.Seek(seconds)
}

// Status 返回当前播放状态快照。
func (m *Manager) Status() Status {
	m.mu.Lock()
	sess, syncFailure := m.current, m.lastSync
	m.mu.Unlock()
	if sess == nil {
		return Status{Playing: false, Sync: syncFailure}
	}
	st := sess.player.State()
	dan := sess.danmakuInfo()
	return Status{
		Playing:  true,
		FileID:   sess.item.FileID,
		Title:    sess.titleSnapshot(),
		Position: st.TimePos,
		Duration: st.Duration,
		Paused:   st.Paused,
		Danmaku:  &dan,
		Sync:     syncFailure,
	}
}

// setSyncFailure 记下一次回写失败；nil 表示清掉（成功了，或不再适用）。
func (m *Manager) setSyncFailure(info *SyncInfo) {
	m.mu.Lock()
	m.lastSync = info
	m.mu.Unlock()
}

func pickTitle(b store.Binding, item library.Item) string {
	if b.Title != "" {
		if b.Episode > 0 {
			return fmt.Sprintf("%s 第%d集", b.Title, b.Episode)
		}
		return b.Title
	}
	if item.ParsedTitle != nil && *item.ParsedTitle != "" {
		return *item.ParsedTitle
	}
	return item.FileName
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}

// classifyAnimegoErr 把 animego 客户端错误翻成用户可读的中文（CQ3 分类）。
// classifySyncErr 把回写失败翻成「用户看到什么、能做什么」。
// 与 classifyAnimegoErr 分开：弹幕失败的落款是「本地播放不受影响」，
// 而这里丢的是账号进度，恢复动作完全不同。
func classifySyncErr(err error) (reason, recovery string) {
	var ae *animego.Error
	if errors.As(err, &ae) {
		switch ae.Kind {
		case animego.ErrUnavailable:
			return "animego 服务暂不可达", "确认网络后重看这一集的结尾，会自动再试一次"
		case animego.ErrRateLimited:
			return "请求过于频繁被限流", "过几分钟后重看这一集的结尾，会自动再试一次"
		case animego.ErrAuthExpired:
			return "登录已过期", "到设置里重新登录，然后重看这一集的结尾"
		}
	}
	return "回写失败：" + err.Error(), "可以到 animego 网站上手动标记这一集"
}

func classifyAnimegoErr(err error) string {
	var ae *animego.Error
	if errors.As(err, &ae) {
		switch ae.Kind {
		case animego.ErrUnavailable:
			return "animego 服务暂不可达，本地播放不受影响，稍后重试即可"
		case animego.ErrRateLimited:
			return "请求过于频繁，稍后重试"
		case animego.ErrAuthExpired:
			return "登录已过期，请到设置里重新登录"
		}
	}
	return "弹幕服务出错：" + err.Error()
}
