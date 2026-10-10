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
	// RemoteStallTimeout 控制在线媒体无进度的最长等待，非正值使用 45 秒。
	RemoteStallTimeout time.Duration
	// OnSessionEnd 在一次播放会话终结后调用（mpv 播完、用户关窗、或被 Stop）。
	//
	// 磁力播放靠它兑现「停止播放即停做种并删分片」：用户直接关掉 mpv 窗口时
	// 没有任何 API 请求发生，没有这个回调，种子会一直挂在那里下载和上传。
	// 播放层不认识磁力，装配层决定要不要收掉引擎。可为 nil。
	OnSessionEnd func()
	// Association 查某个文件所在作品分组的手动关联（媒体库按 clusterKey 存，见 association.go）。
	// 可为 nil：不在媒体库里的源（磁力、在线候选）本来就没有关联，一律走自动匹配。
	Association func(fileID string) (store.Association, bool)
	// GroupEpisodes 返回文件所在作品分组里全部正片的集号（判断跨季连续编号要看整组，见 episodes.go）。
	// 可为 nil，不在媒体库里的源返回空。
	GroupEpisodes func(fileID string) []int
	// EpisodeSpace 查目录作品的总集数与集号偏移，「看完」回写前把文件里的集号换成作品集号。
	// 可为 nil：不换算，按文件里的集号回写。
	EpisodeSpace func(ctx context.Context, anilistID int) (EpisodeSpace, error)
	// Shaders 返回每次起播要加载的 GLSL 着色器（Anime4K 超分；关掉时返回空）。每次起播、每次改设置时调。
	// 可为 nil：不加载任何着色器。
	Shaders func() []string
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
	FileID  string      `json:"fileId"`
	Title   string      `json:"title"`
	Danmaku DanmakuInfo `json:"danmaku"`
}

// PlaybackFailure 是最近一次媒体会话的异常终态。FileID 让调用方
// 只对自己启动的候选执行回退，不会把用户手动停止或正常播完误判为失败。
type PlaybackFailure struct {
	FileID string `json:"fileId"`
	Reason string `json:"reason"`
	At     int64  `json:"at"`
}

// Status 是当前播放状态快照。
type Status struct {
	Playing         bool             `json:"playing"`
	FileID          string           `json:"fileId,omitempty"`
	Title           string           `json:"title,omitempty"`
	Position        float64          `json:"position,omitempty"`
	Duration        float64          `json:"duration,omitempty"`
	Paused          bool             `json:"paused,omitempty"`
	Danmaku         *DanmakuInfo     `json:"danmaku,omitempty"`
	PlaybackFailure *PlaybackFailure `json:"playbackFailure,omitempty"`
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
	// lastPlaybackFailure 保留最近异常终止的媒体会话，下一次
	// Play 开始时清空。在线候选回退靠它跨请求观察 mpv 的异步失败。
	lastPlaybackFailure *PlaybackFailure
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
	m.setPlaybackFailure(nil)

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
		MPVPath:                mpvPath,
		MediaPath:              src.MPVPath(),
		SubPath:                subPath,
		Title:                  title,
		StartAt:                startAt,
		SocketDir:              m.opts.RuntimeDir,
		HTTPHeaders:            sourceHTTPHeaders(src),
		RedactMediaDiagnostics: sourceRedactsDiagnostics(src),
		NetworkStream:          isNetworkStream(src.MPVPath()),
		Shaders:                m.shaders(),
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
	go m.watchRemoteProgress(sess)
	go m.resolveDanmaku(sess)

	return PlayResult{FileID: item.FileID, Title: title, Danmaku: loading}, nil
}

// isNetworkStream 判断交给 mpv 的是不是一条 HTTP 流（磁力边下边播、在线候选），
// 而不是本地文件路径 —— 只有前者需要流式缓存参数。
func isNetworkStream(mpvPath string) bool {
	lower := strings.ToLower(mpvPath)
	return strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://")
}

type httpHeaderSource interface {
	HTTPHeaders() map[string]string
}

type redactedMediaSource interface {
	RedactMediaDiagnostics() bool
}

func sourceHTTPHeaders(source MediaSource) map[string]string {
	withHeaders, ok := source.(httpHeaderSource)
	if !ok {
		return nil
	}
	return withHeaders.HTTPHeaders()
}

func sourceRedactsDiagnostics(source MediaSource) bool {
	redacted, ok := source.(redactedMediaSource)
	return ok && redacted.RedactMediaDiagnostics()
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
	if binding.Episode > 0 {
		m.warmEpisodeSpace(binding.AnilistID)
	}
}

// ensureBinding 取（或建立）文件与 dandanplay 剧集的绑定；一并返回弹幕链路状态。
// 任何失败都只影响 DanmakuInfo，不影响播放。媒体库里手动认定过作品的文件
// 按关联走（见 association.go）。
//
// item 是调用方持有的 src.Item() 快照，一并传进来是为了不在这里重复取。
func (m *Manager) ensureBinding(ctx context.Context, src MediaSource, item library.Item) (store.Binding, DanmakuInfo) {
	if a, ok := m.association(item.FileID); ok {
		if a.Mode != store.AssociationManual {
			return store.Binding{}, DanmakuInfo{State: "none", Reason: "已在媒体库标为不是目录里的作品：不匹配弹幕，也不回写进度"}
		}
		return m.ensureAssociatedBinding(ctx, src, item, a)
	}
	q := autoQuery(src, item)
	if b, ok := m.opts.Store.Binding(item.FileID); ok && b.DandanEpisodeID != 0 && q.accepts(b) {
		return b, DanmakuInfo{State: "ok"}
	}
	b, dan, missing := m.matchBinding(ctx, src, item, q)
	if missing {
		// 作品认出来了却没有这一集：跨季连续编号的文件（第二季的「15」）在分季条目里
		// 找不到，换成作品自己的集号再试一次
		if local, ok := m.seasonLocalEpisode(ctx, q.wantAnilist, episodeNumber(item)); ok {
			b, dan, _ = m.matchBindingAt(ctx, src, item, q, local)
		}
	}
	if dan.State == "ok" {
		m.saveBinding(item.FileID, b)
	}
	return b, dan
}

// episodeNumber 取文件的集号；认不出来返回 0。
//
// 集号无法识别时【不猜 1】：猜错会把剧场版/OVA/特典当"第1集"匹配，
// 播完还会拿 MarkWatched(anilistId, 1) 污染用户真实的 animego 账号。
func episodeNumber(item library.Item) int {
	if item.Episode != nil {
		return *item.Episode
	}
	if item.ParsedNumber != nil {
		return *item.ParsedNumber
	}
	return 0
}

func parsedTitle(item library.Item) string {
	if item.ParsedTitle != nil {
		return *item.ParsedTitle
	}
	return ""
}

// matchQuery 是一次匹配的作品身份约束：wantAnilist > 0 时结果必须是这部作品，
// keywords 按顺序试（空关键词也照发：服务端能靠文件指纹命中；列表为空则不发请求）。
type matchQuery struct {
	wantAnilist int
	keywords    []string
}

// accepts：缓存的匹配能不能沿用。没有作品身份时什么都沿用（与改动前一致）；带着身份来的
// 播放只认同一部作品 —— 之前没有身份时（从搜索页播、或旧版本）匹配错的作品会被缓存下来，
// 不拦的话此后每次都沿用：弹幕是别的番的，看完还记到别的番上。
func (q matchQuery) accepts(b store.Binding) bool {
	return q.wantAnilist <= 0 || b.AnilistID == q.wantAnilist
}

// autoQuery 是没有手动关联时的匹配条件。知道自己在目录里是哪部作品的源（在线候选、从作品页
// 播放的磁力）带来两样东西：校验用的 anilistId，和文件名标题失手时可以再试的目录标题；
// 本地文件和不带身份的源没有这些，只用文件名里的标题试一次（标题解析不出来也照发空关键词）。
func autoQuery(src MediaSource, item library.Item) matchQuery {
	title := parsedTitle(item)
	if hinted, ok := src.(MatchHinted); ok {
		if want, alts := hinted.MatchHints(); want > 0 || len(alts) > 0 {
			keywords := appendUniqueKeywords([]string{title}, alts)
			if len(keywords) == 0 {
				// 一个能用的标题都没有：照发一次空关键词，服务端还能靠文件指纹命中
				keywords = []string{""}
			}
			return matchQuery{wantAnilist: want, keywords: keywords}
		}
	}
	return matchQuery{keywords: []string{title}}
}

// matchBinding 向 animego 匹配这个文件，不落盘。第三个返回值：作品认出来了，
// 但匹配结果里没有这一集（绝对集号配上分季条目时常见）。
func (m *Manager) matchBinding(ctx context.Context, src MediaSource, item library.Item, q matchQuery) (store.Binding, DanmakuInfo, bool) {
	return m.matchBindingAt(ctx, src, item, q, episodeNumber(item))
}

// matchBindingAt 按指定集号向 animego 要弹幕：通常就是文件里的集号，跨季连续编号时是换算后的
// 作品集号。换算后的集号只用于这次请求与查弹幕那一集；绑定里记的仍是文件里的集号 ——
// 回写前 placeEpisode 统一把它换成作品集号，绑定里已经换过一次就会被再换一次。
func (m *Manager) matchBindingAt(ctx context.Context, src MediaSource, item library.Item, q matchQuery, episode int) (store.Binding, DanmakuInfo, bool) {
	if m.opts.Client == nil {
		return store.Binding{}, DanmakuInfo{State: "none", Reason: "未配置 animego 服务"}, false
	}
	hash, ok := m.fileHash(ctx, src, item)
	if !ok {
		return store.Binding{}, DanmakuInfo{State: "unavailable", Reason: "计算文件指纹失败，弹幕匹配跳过"}, false
	}
	if episode <= 0 {
		return store.Binding{}, DanmakuInfo{State: "unmatched", Reason: "无法识别集号，弹幕匹配跳过"}, false
	}

	res, stray, err := m.firstMatch(ctx, item, hash, episode, q)
	if err != nil {
		return store.Binding{}, DanmakuInfo{State: "unavailable", Reason: classifyAnimegoErr(err)}, false
	}
	if !res.Matched {
		if stray > 0 {
			return store.Binding{}, DanmakuInfo{State: "unmatched", Reason: fmt.Sprintf("关键词只匹配到另一部作品（anilistId %d），弹幕已跳过", stray)}, false
		}
		return store.Binding{}, DanmakuInfo{State: "unmatched", Reason: "未匹配到对应剧集，弹幕不可用"}, false
	}
	ref, ok := res.EpisodeMap[episode]
	if !ok {
		return store.Binding{}, DanmakuInfo{State: "unmatched", Reason: fmt.Sprintf("匹配结果里没有第 %d 集", episode)}, true
	}

	anilistID := res.AnilistID
	if anilistID == 0 {
		// phase1 命中可能不带 anilistId；作品身份已知时用它，进度才能回写到正确的作品。
		anilistID = q.wantAnilist
	}
	return store.Binding{
		AnilistID:       anilistID,
		DandanEpisodeID: ref.DandanEpisodeID,
		Episode:         episodeNumber(item),
		Title:           firstNonEmpty(res.TitleChinese, res.TitleNative, parsedTitle(item)),
		EpisodeTitle:    ref.Title,
		CoverURL:        res.CoverImageURL,
		MatchedAt:       time.Now().UnixMilli(),
	}, DanmakuInfo{State: "ok"}, false
}

// fileHash 取（懒算并缓存）文件首 16MB 指纹；在线媒体没有指纹时返回空串。
// 第二个返回值为 false 表示算的时候出错了。
func (m *Manager) fileHash(ctx context.Context, src MediaSource, item library.Item) (string, bool) {
	if hash := m.opts.Store.Hash(item.FileID); hash != "" {
		return hash, true
	}
	hash, err := src.Hash16M(ctx)
	switch {
	case errors.Is(err, ErrNoFingerprint):
		// 在线媒体没有指纹：hash 留空，服务端只能靠文件名/关键词（见 MatchInput.FileHash）。
		return "", true
	case err != nil:
		return "", false
	}
	if err := m.opts.Store.SetHash(item.FileID, hash); err != nil {
		// 缓存写失败不致命：下次再算一遍。
		log.Printf("player: 缓存 hash 失败：%v", err)
	}
	return hash, true
}

// firstMatch 按顺序试关键词，返回第一个命中（且作品身份对得上）的结果；都没命中时
// res.Matched 为 false，stray 是被身份校验挡掉的那部作品。
func (m *Manager) firstMatch(ctx context.Context, item library.Item, hash string, episode int, q matchQuery) (res animego.MatchResult, stray int, err error) {
	for _, kw := range q.keywords {
		r, err := m.opts.Client.Match(ctx, animego.MatchInput{
			FileName: item.FileName,
			FileHash: hash,
			FileSize: item.Size,
			Episode:  episode,
			Keyword:  kw,
		})
		if err != nil {
			return animego.MatchResult{}, 0, err
		}
		if !r.Matched {
			continue
		}
		// 关键词匹配对同名不同代/同系列不同季会命中别的作品：弹幕会错，播完还会把
		// 错的作品标成已看。目录身份已知时必须对上，对不上就换下一个关键词。
		if q.wantAnilist > 0 && r.AnilistID > 0 && r.AnilistID != q.wantAnilist {
			stray = r.AnilistID
			continue
		}
		return r, 0, nil
	}
	return animego.MatchResult{}, stray, nil
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

func (m *Manager) shaders() []string {
	if m.opts.Shaders == nil {
		return nil
	}
	return m.opts.Shaders()
}

// RefreshShaders 把当前设置的着色器套到正在播放的窗口上（改了 Anime4K 设置时调），不用重开 mpv。
// 返回是否真的套上了：没有在播就是 false（下次起播自然按新设置来）。
func (m *Manager) RefreshShaders() (bool, error) {
	m.mu.Lock()
	sess := m.current
	m.mu.Unlock()
	if sess == nil {
		return false, nil
	}
	if err := sess.player.SetShaders(m.shaders()); err != nil {
		return false, errs.Wrap(errs.CategoryPlayback, "player.shaders",
			"没能把画质增强套到正在播放的窗口上", "下次开始播放时会按新设置加载", err)
	}
	return true, nil
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
	sess, syncFailure, playbackFailure := m.current, m.lastSync, m.lastPlaybackFailure
	m.mu.Unlock()
	if sess == nil {
		return Status{Playing: false, Sync: syncFailure, PlaybackFailure: playbackFailure}
	}
	st := sess.player.State()
	dan := sess.danmakuInfo()
	return Status{
		Playing:         true,
		FileID:          sess.item.FileID,
		Title:           sess.titleSnapshot(),
		Position:        st.TimePos,
		Duration:        st.Duration,
		Paused:          st.Paused,
		Danmaku:         &dan,
		Sync:            syncFailure,
		PlaybackFailure: playbackFailure,
	}
}

func (m *Manager) setPlaybackFailure(info *PlaybackFailure) {
	m.mu.Lock()
	m.lastPlaybackFailure = info
	m.mu.Unlock()
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

// MatchHinted 由知道自己在目录里是哪部作品的媒体源实现（在线候选、从作品页播放的磁力）。
// anilistID 用于校验关键词匹配没有命中别的作品；alt 是主标题失手后可再试的别名。
// 两样都只在本机用来核对 animego 的匹配结果，不会把媒体地址或磁力带给 animego。
type MatchHinted interface {
	MatchHints() (anilistID int, alt []string)
}

// appendUniqueKeywords 追加非空且未出现过的关键词，保持原有顺序。
func appendUniqueKeywords(base []string, extra []string) []string {
	seen := make(map[string]bool, len(base)+len(extra))
	out := make([]string, 0, len(base)+len(extra))
	for _, kw := range append(append([]string{}, base...), extra...) {
		kw = strings.TrimSpace(kw)
		if kw == "" || seen[kw] {
			continue
		}
		seen[kw] = true
		out = append(out, kw)
	}
	return out
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
