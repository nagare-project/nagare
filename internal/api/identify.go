package api

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"slices"
	"sync"
	"time"

	"github.com/nagare-project/nagare/internal/animego"
	"github.com/nagare-project/nagare/internal/library"
	"github.com/nagare-project/nagare/internal/store"
)

// 媒体库作品自动识别 —— 移植自 animego「我的库」导入时的匹配（importPipeline.js 的
// processCluster：取簇代表文件的首 16MB 指纹，交给 /api/dandanplay/match，拿回 siteAnime）。
//
// 扫描只解析文件名，分不出「这一组文件是哪部番」：同一部番的四个字幕组版本是四张海报，
// 而且没播过的海报一律无图。这里在扫描之后、在后台给每个还没认出作品的分组挑一个代表集，
// 算首 16MB 指纹去匹配，结果就是那个文件的匹配缓存（store.Binding）—— 与播放前的匹配
// 完全同一份请求、同一份落盘，只是提前了。海报墙据此显示封面与作品名，并按作品归组。
//
// 与决议 P1（扫描懒算 hash）的关系：P1 挡的是「导入一千集要读 16GB」，即逐文件全量算指纹。
// 这里每个分组一周之内最多读两个文件的首 16MB，量级是「分组数 × 16MB」，且在后台按
// animego 的限速一个个来；逐文件的指纹仍然延到首次播放前。2026-10-08 用用户真实媒体库实测
// （37 个分组）：只发文件名关键词认出 21 个、其中 2 个认错季；带指纹 37 个全部认对。
//
// 只走 animego 元数据读这一条线（红线 2）：发出去的是文件名、大小、指纹，不涉及任何磁力。

// identifyMatcher 是自动识别要用到的 animego 能力（测试注入假实现）。
type identifyMatcher interface {
	Match(ctx context.Context, in animego.MatchInput) (animego.MatchResult, error)
}

const (
	// identifyPace 是两次匹配请求之间的间隔：animego 全站按 IP 限速 1 req/s（突发 60），
	// 识别是后台的事，不该把播放时那一次匹配挤进限速。
	identifyPace = 1100 * time.Millisecond
	// identifyRetry：问过的文件多久之后可以再问。上游的番剧库会更新，但不会天天变。
	identifyRetry = 7 * 24 * time.Hour
	// identifyQuiet：最近这么久内还在被写入的文件先不读。BT 客户端乱序写入，下载中的文件
	// 首 16MB 可能还是空洞，算出来的指纹是错的（animego 网页端的 QUIET_PERIOD 同理）。
	identifyQuiet = 2 * time.Minute
	// identifyPerCluster：一个分组在 identifyRetry 之内最多问几个文件。代表集没认出来时
	// 换一集再试一次就够了，再多就是在为一个上游没有的冷门番反复读盘。
	identifyPerCluster = 2
	// identifyMatchTimeout：单次匹配的时限（服务端自己 20 秒超时）。
	identifyMatchTimeout = 30 * time.Second
	// animego 整体不可用 / 限速时整轮推迟多久再来。
	identifyBackoffUnavailable = 5 * time.Minute
	identifyBackoffRateLimited = 2 * time.Minute
	// identifyDeferFile：单个文件出了暂时性问题（读不了、这一次的响应解析不了）多久之后再试。
	// 只推迟这一个文件，排在它后面的分组照常识别。
	identifyDeferFile = time.Hour
	// identifyMaxFileFailures：连续这么多个文件都出暂时性问题，多半不是文件的事而是上游整体
	// 出了问题，这时才整轮推迟。
	identifyMaxFileFailures = 3
	// 上游字段落盘前的长度上限：每次写进度都会整份重写 state.json（与手动关联同一组上限）。
	maxIdentifyTitleRunes = maxAssocTitleRunes
	maxIdentifyCoverBytes = maxAssocCoverBytes
)

// IdentifyStatus 是后台识别的进度，随 GET /api/library 下发（字段 identify）。
type IdentifyStatus struct {
	// Running：正在识别，或者重扫之后已经排上队了。
	Running bool `json:"running"`
	// Done / Total：这一轮已处理 / 要处理的分组数（排队时都是 0）。
	Done  int `json:"done"`
	Total int `json:"total"`
	// Error 是上一轮因为 animego 不可用而暂停时给用户看的原因；到点会自动重试，跑完就清掉。
	Error string `json:"error,omitempty"`
}

// identifyJob 是一个要识别的作品分组。
type identifyJob struct {
	clusterKey string
	candidates []library.Item
}

// Identifier 在后台识别媒体库里还不知道是哪部作品的分组。
type Identifier struct {
	lib    *LibraryService
	st     *store.Store
	client identifyMatcher
	hash   func(path string) (string, error)
	stat   func(path string) (fs.FileInfo, error)
	now    func() time.Time
	pace   time.Duration
	kick   chan struct{}

	// deferred：fileID → 到这个时间之前先不试（单个文件的暂时性问题）。只在 Run 的
	// goroutine 里读写；不落盘 —— 重启之后再试一次无妨。
	deferred map[string]time.Time

	mu        sync.Mutex
	status    IdentifyStatus
	notBefore time.Time // 上游不可用时的推迟期限；kick 不提前打断它
}

// NewIdentifier 构造识别器；调用方负责起 Run。client 为 nil 时 Run 什么都不做。
func NewIdentifier(lib *LibraryService, st *store.Store, client identifyMatcher) *Identifier {
	return &Identifier{
		lib:      lib,
		st:       st,
		client:   client,
		hash:     library.Hash16M,
		stat:     os.Stat,
		now:      time.Now,
		pace:     identifyPace,
		kick:     make(chan struct{}, 1),
		deferred: map[string]time.Time{},
	}
}

// Kick 请求跑一轮（不阻塞；已经有一轮在排队时合并成一次）。重扫完成后调用。
// 不在推迟期内时，进度立刻显示为「排上队了」：调用方（重扫的请求）一返回，界面重读媒体库
// 就能看到识别正在进行，而不是赶在后台那一轮真正开始之前读到「没在识别」就不再刷新。
func (id *Identifier) Kick() {
	if id.client == nil {
		return
	}
	id.mu.Lock()
	if !id.now().Before(id.notBefore) && !id.status.Running {
		id.status = IdentifyStatus{Running: true}
	}
	id.mu.Unlock()
	select {
	case id.kick <- struct{}{}:
	default:
	}
}

// Status 返回进度快照。
func (id *Identifier) Status() IdentifyStatus {
	id.mu.Lock()
	defer id.mu.Unlock()
	return id.status
}

// Run 循环等待 Kick 或到期的重试，直到 ctx 取消。
func (id *Identifier) Run(ctx context.Context) {
	if id.client == nil {
		return
	}
	var retry <-chan time.Time
	for {
		select {
		case <-ctx.Done():
			return
		case <-id.kick:
			// 上游正在限速 / 不可用：重扫的 kick 不提前打断推迟，等到点再跑
			if left := id.backoffLeft(); left > 0 {
				if retry == nil {
					retry = time.After(left)
				}
				continue
			}
		case <-retry:
		}
		retry = nil
		if wait := id.pass(ctx); wait > 0 {
			retry = time.After(wait)
		}
	}
}

func (id *Identifier) backoffLeft() time.Duration {
	id.mu.Lock()
	defer id.mu.Unlock()
	return id.notBefore.Sub(id.now())
}

// pass 跑一轮识别，返回多久之后应当再跑一轮（0 = 不用，等下一次 Kick）。
func (id *Identifier) pass(ctx context.Context) time.Duration {
	now := id.now()
	// 过了重试期的记录已经不起作用；按时间清，不按文件在不在（拔掉的盘插回来不用从头问）
	if err := id.st.PruneIdentifyAttempts(now.Add(-identifyRetry).UnixMilli()); err != nil {
		log.Printf("library: 清理识别记录失败：%v", err)
	}
	for fileID, until := range id.deferred {
		if !now.Before(until) {
			delete(id.deferred, fileID)
		}
	}
	jobs, wait := id.lib.identifyJobs(id.st.Snapshot(), id.deferred, now)
	if len(jobs) == 0 {
		id.finish(IdentifyStatus{}, time.Time{})
		return wait
	}

	id.setStatus(IdentifyStatus{Running: true, Total: len(jobs)})
	tally := passTally{}
	for i, job := range jobs {
		res := id.identifyCluster(ctx, job, &tally)
		switch {
		case ctx.Err() != nil:
			id.finish(IdentifyStatus{}, time.Time{})
			return 0
		case res.kind == fileBackoff:
			log.Printf("library: 作品识别暂停，%s 后重试：%s（%v）", res.backoff, res.reason, withoutURL(res.err))
			id.finish(IdentifyStatus{Done: i, Total: len(jobs), Error: res.reason}, id.now().Add(res.backoff))
			return res.backoff
		}
		id.setStatus(IdentifyStatus{Running: true, Done: i + 1, Total: len(jobs)})
	}
	log.Printf("library: 作品识别完成：%d 个分组，认出 %d 个，问过没认出 %d 个文件，暂缓 %d 个文件",
		len(jobs), tally.identified, tally.missed, tally.deferred)
	id.finish(IdentifyStatus{}, time.Time{})
	return wait
}

func (id *Identifier) setStatus(s IdentifyStatus) {
	id.mu.Lock()
	defer id.mu.Unlock()
	id.status = s
}

// finish 结束一轮：写下最终进度，以及（暂停时）推迟期限。
func (id *Identifier) finish(s IdentifyStatus, notBefore time.Time) {
	id.mu.Lock()
	defer id.mu.Unlock()
	id.status, id.notBefore = s, notBefore
}

// fileKind 是试一个文件的结果。
type fileKind int

const (
	fileSkipped    fileKind = iota // 没发请求：文件变了、读不了、还在写入
	fileMiss                       // 问过了：上游没有、请求被拒、认出了番剧却没有作品 ID
	fileIdentified                 // 认出了作品（落盘，或已有指向同一部作品的匹配）
	fileSettled                    // 问过了，答案没落盘（已有别的作品的匹配、用户认定了作品）：分组不再识别
	fileDeferred                   // 这一次的响应有问题：这个文件过一阵再试
	fileBackoff                    // 上游整体不可用：整轮推迟
)

type fileResult struct {
	kind      fileKind
	requested bool // 发过匹配请求（要按限速等一等）
	backoff   time.Duration
	reason    string // 给用户看的暂停原因（fileBackoff）
	err       error  // 写日志用的底层错误
}

// passTally 是一轮里的计数。failures：连续出暂时性问题的文件数（隔着一个正常答复就清零）。
type passTally struct {
	identified, missed, deferred, failures int
}

// identifyCluster 依次试分组的候选文件，认出（或定下来）一个就停。
func (id *Identifier) identifyCluster(ctx context.Context, job identifyJob, tally *passTally) fileResult {
	for _, it := range job.candidates {
		if ctx.Err() != nil {
			return fileResult{kind: fileSkipped}
		}
		res := id.identifyFile(ctx, job.clusterKey, it)
		if res.requested {
			id.sleep(ctx)
		}
		switch res.kind {
		case fileBackoff:
			return res
		case fileDeferred:
			tally.deferred++
			if tally.failures++; tally.failures >= identifyMaxFileFailures {
				return fileResult{kind: fileBackoff, backoff: identifyBackoffUnavailable, err: res.err,
					reason: "animego 的匹配接口连续返回了无法识别的内容，作品识别稍后自动重试；若持续出现，请检查 nagare 是否需要更新"}
			}
		case fileIdentified, fileSettled:
			tally.failures = 0
			tally.identified++
			return res
		case fileMiss:
			tally.failures = 0
			tally.missed++
		}
	}
	return fileResult{kind: fileMiss}
}

// identifyFile 核对文件没变、算指纹并匹配；认出来就落成它的匹配缓存。
func (id *Identifier) identifyFile(ctx context.Context, clusterKey string, it library.Item) fileResult {
	hash, ok := id.fileHash(it)
	if !ok {
		return fileResult{kind: fileSkipped}
	}
	// 关键词与播放前的匹配一致（文件名里的标题）：指纹没命中时服务端靠它兜底。
	// 匹配接口要求带一个集号；没有集号的文件（剧场版一类）按第 1 集问，结果只用来认作品
	episode := itemEpisode(it)
	mctx, cancel := context.WithTimeout(ctx, identifyMatchTimeout)
	res, err := id.client.Match(mctx, animego.MatchInput{
		FileName: it.FileName,
		FileHash: hash,
		FileSize: it.Size,
		Episode:  max(episode, 1),
		Keyword:  itemTitle(it),
	})
	cancel()
	if err != nil {
		if ctx.Err() != nil {
			return fileResult{kind: fileSkipped, requested: true}
		}
		return id.matchFailed(it, hash, err)
	}
	if !res.Matched {
		id.recordAttempt(it.FileID, hash)
		return fileResult{kind: fileMiss, requested: true}
	}
	return id.saveMatch(clusterKey, it, hash, identifiedBinding(res, it, episode, id.now()))
}

// saveMatch 落盘认出来的结果并给出结局。
func (id *Identifier) saveMatch(clusterKey string, it library.Item, hash string, b store.Binding) fileResult {
	// 扫描之后用户可能已经给这个分组认定了作品，而重扫又可能把关联迁到了新键上：按文件再查一次
	if _, associated := id.lib.AssociationFor(it.FileID); associated {
		id.recordAttempt(it.FileID, hash)
		return fileResult{kind: fileSettled, requested: true}
	}
	outcome, err := id.st.SaveIdentified(clusterKey, it.FileID, hash, id.now().UnixMilli(), b)
	switch {
	case err != nil:
		log.Printf("library: 保存识别结果失败：%v", err)
		return fileResult{kind: fileSkipped, requested: true}
	case outcome == store.IdentifyConflict || outcome == store.IdentifyBlocked:
		return fileResult{kind: fileSettled, requested: true}
	case b.AnilistID <= 0:
		// 认出了番剧（封面已存下），但上游没对应到作品 ID：不能按作品归组，算问过没认出
		return fileResult{kind: fileMiss, requested: true}
	}
	return fileResult{kind: fileIdentified, requested: true}
}

// fileHash 取文件的指纹（缓存的或现算的）。读之前再核对一次文件：扫描时记下的大小与修改时间
// 必须没变，而且已经静置够久 —— 正在下载的文件首 16MB 可能还是空洞，算出的指纹会一直缓存在
// 一个仍然有效的 fileId 下，播放时也会照用。核对不过就这一轮跳过，下一次重扫会给出新的 fileId。
func (id *Identifier) fileHash(it library.Item) (string, bool) {
	if hash := id.st.Hash(it.FileID); hash != "" {
		return hash, true
	}
	info, err := id.stat(it.AbsPath)
	if err != nil {
		id.deferFile(it.FileID, fmt.Errorf("读取文件信息：%w", err))
		return "", false
	}
	if info.Size() != it.Size || info.ModTime().UnixMilli() != it.MTimeMs || !settled(info.ModTime(), id.now()) {
		return "", false
	}
	hash, err := id.hash(it.AbsPath)
	if err != nil {
		id.deferFile(it.FileID, err)
		return "", false
	}
	return hash, true
}

// deferFile 让读不了的文件过一阵再试（只记一次日志，不在每一轮刷屏）。
func (id *Identifier) deferFile(fileID string, err error) {
	if _, already := id.deferred[fileID]; !already {
		log.Printf("library: 识别时读取文件失败，%s 后再试：%v", identifyDeferFile, err)
	}
	id.deferred[fileID] = id.now().Add(identifyDeferFile)
}

// matchFailed 按失败的性质决定：整轮推迟、只推迟这个文件、还是算问过没认出。
func (id *Identifier) matchFailed(it library.Item, hash string, err error) fileResult {
	unavailable := fileResult{kind: fileBackoff, requested: true, backoff: identifyBackoffUnavailable, err: err,
		reason: "animego 暂时连不上，作品封面与名称稍后自动补上"}
	var ae *animego.Error
	if !errors.As(err, &ae) {
		return unavailable
	}
	switch {
	case ae.Kind == animego.ErrRateLimited:
		return fileResult{kind: fileBackoff, requested: true, backoff: identifyBackoffRateLimited, err: err,
			reason: "animego 限速中，作品识别稍后继续"}
	case ae.Kind == animego.ErrDecode:
		// 这一次的响应解析不了：多半是这一部作品的数据有问题，先跳过这个文件，别挡住后面的分组
		id.deferred[it.FileID] = id.now().Add(identifyDeferFile)
		return fileResult{kind: fileDeferred, requested: true, err: err}
	case ae.Kind == animego.ErrBadRequest && perFileRejection(ae.Status):
		// 请求的内容被拒（这个文件名、这个大小）：是这个文件的问题，按问过没认出记
		log.Printf("library: 识别请求被拒（%s）：%v", it.FileName, withoutURL(err))
		id.recordAttempt(it.FileID, hash)
		return fileResult{kind: fileMiss, requested: true}
	case ae.Kind == animego.ErrBadRequest:
		// 403 / 404 / 410 这一类说的是接口整体不让用（防火墙拦了、接口改了），不是这个文件的事：
		// 记成「没认出」会让每个文件白白一周不再问
		unavailable.reason = fmt.Sprintf("animego 拒绝了识别请求（HTTP %d），稍后自动重试；若持续出现，请检查 nagare 是否需要更新", ae.Status)
		return unavailable
	}
	return unavailable
}

// perFileRejection：哪些 4xx 是「这一次请求的内容不对」。
func perFileRejection(status int) bool {
	return status == 400 || status == 413 || status == 422
}

func (id *Identifier) recordAttempt(fileID, hash string) {
	if err := id.st.RecordIdentifyAttempt(fileID, hash, id.now().UnixMilli()); err != nil {
		log.Printf("library: 记录识别结果失败：%v", err)
	}
}

// sleep 在两次请求之间按限速等一等；ctx 取消时立刻返回。
func (id *Identifier) sleep(ctx context.Context) {
	if id.pace <= 0 {
		return
	}
	t := time.NewTimer(id.pace)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}

// identifiedBinding 把匹配结果翻成匹配缓存，规则与播放前的匹配一致（player.matchBinding）。
// 文件没有集号时只记作品与封面：没有集号的文件播放时本来就不匹配弹幕、不回写进度，
// 识别不该给它开出一条新的回写路径（猜成第 1 集，看完就会写进 animego 账号）。
func identifiedBinding(res animego.MatchResult, it library.Item, episode int, now time.Time) store.Binding {
	cover := res.CoverImageURL
	if len(cover) > maxIdentifyCoverBytes {
		cover = ""
	}
	b := store.Binding{
		AnilistID: res.AnilistID,
		Episode:   episode,
		Title:     truncateRunes(firstText(res.TitleChinese, res.TitleNative, res.TitleRomaji, itemTitle(it)), maxIdentifyTitleRunes),
		CoverURL:  cover,
		MatchedAt: now.UnixMilli(),
	}
	if ref, ok := res.EpisodeMap[episode]; ok && episode > 0 {
		b.DandanEpisodeID = ref.DandanEpisodeID
		b.EpisodeTitle = truncateRunes(ref.Title, maxIdentifyTitleRunes)
	}
	return b
}

// identifyJobs 列出这一轮要识别的分组；第二个返回值：有分组因为文件刚写入被跳过时，
// 多久之后该再来一轮。只在读锁内取快照、挑候选，不做任何 IO。
func (s *LibraryService) identifyJobs(snap store.Data, deferred map[string]time.Time, now time.Time) ([]identifyJob, time.Duration) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// 同一个 clusterKey 可能出现在多个库目录里（同一部番分在两块盘上）：合并成一个任务，
	// 两边一模一样的副本（同一个 fileId）只算一个
	byKey := map[string][]library.Item{}
	seen := map[string]map[string]bool{}
	var order []string
	for _, ce := range s.clusters {
		key := ce.cluster.ClusterKey
		if seen[key] == nil {
			seen[key] = map[string]bool{}
			order = append(order, key)
		}
		for _, it := range ce.cluster.Items {
			if !seen[key][it.FileID] {
				seen[key][it.FileID] = true
				byKey[key] = append(byKey[key], it)
			}
		}
	}

	var jobs []identifyJob
	var wait time.Duration
	for _, key := range order {
		if _, ok := snap.Associations[key]; ok {
			continue // 用户认定过（或标为不是目录作品）：以用户为准
		}
		items := byKey[key]
		if identifiedCluster(items, snap, now) {
			continue
		}
		budget := identifyPerCluster - attemptedFiles(items, snap, now)
		if budget <= 0 {
			continue // 这个分组这一周的预算用完了（上游没有这部番）
		}
		cands, soonest := identifyCandidates(items, snap, deferred, now)
		if soonest > 0 && (wait == 0 || soonest < wait) {
			wait = soonest
		}
		if len(cands) > budget {
			cands = cands[:budget]
		}
		if len(cands) > 0 {
			jobs = append(jobs, identifyJob{clusterKey: key, candidates: cands})
		}
	}
	return jobs, wait
}

// playableKind：正片与剧场版 —— 识别只拿它们去问（特典、NCOP 常常被匹配到别的条目上）。
func playableKind(kind string) bool { return kind == "main" || kind == "movie" }

func attemptedRecently(snap store.Data, fileID string, now time.Time) bool {
	at := snap.IdentifyAttempts[fileID]
	return at > 0 && now.Sub(time.UnixMilli(at)) < identifyRetry
}

// attemptedFiles：分组里这一周问过的文件数（分组的识别预算按它扣）。
func attemptedFiles(items []library.Item, snap store.Data, now time.Time) int {
	n := 0
	for _, it := range items {
		if attemptedRecently(snap, it.FileID, now) {
			n++
		}
	}
	return n
}

// identifiedCluster：分组里已经有正片（或剧场版）认出了作品，并且有封面，或者这一周已经问过 ——
// 问过还没封面的（上游没有封面、或已有一条指向别的作品的旧匹配）再问答案也一样。
// 只有作品没有封面、又没问过的（早期版本存下的匹配）还要再识别一次，把封面补上。
func identifiedCluster(items []library.Item, snap store.Data, now time.Time) bool {
	for _, it := range items {
		b := snap.Bindings[it.FileID]
		if playableKind(it.ParsedKind) && b.AnilistID > 0 && (b.CoverURL != "" || attemptedRecently(snap, it.FileID, now)) {
			return true
		}
	}
	return false
}

// identifyCandidates 按优先顺序列出这个分组可以拿去识别的文件（调用方按预算截取）：
// 已经匹配过作品的排前面（只缺封面，指纹多半也缓存了），其余按集号从小到大。
// 分组里一个带集号的正片（或剧场版）都没有时，拿一个没有集号的文件试。
// 第二个返回值：有文件因为刚写入被跳过时，再过多久它就可以读了。
func identifyCandidates(items []library.Item, snap store.Data, deferred map[string]time.Time, now time.Time) ([]library.Item, time.Duration) {
	var numbered, unnumbered []library.Item
	anyNumbered := false
	var soonest time.Duration
	for _, it := range items {
		if !playableKind(it.ParsedKind) || it.AbsPath == "" {
			continue
		}
		if itemEpisode(it) > 0 {
			anyNumbered = true
		}
		if mtime := time.UnixMilli(it.MTimeMs); !settled(mtime, now) {
			if left := identifyQuiet - now.Sub(mtime); soonest == 0 || left < soonest {
				soonest = left
			}
			continue
		}
		if _, wait := deferred[it.FileID]; wait || attemptedRecently(snap, it.FileID, now) {
			continue
		}
		if itemEpisode(it) > 0 {
			numbered = append(numbered, it)
		} else {
			unnumbered = append(unnumbered, it)
		}
	}
	if !anyNumbered {
		return unnumbered[:min(len(unnumbered), 1)], soonest
	}
	slices.SortStableFunc(numbered, func(a, b library.Item) int {
		ab, bb := snap.Bindings[a.FileID].AnilistID > 0, snap.Bindings[b.FileID].AnilistID > 0
		if ab != bb {
			if ab {
				return -1
			}
			return 1
		}
		return itemEpisode(a) - itemEpisode(b)
	})
	return numbered, soonest
}

// settled：文件的修改时间已经过去 identifyQuiet 了。修改时间在未来（exFAT 时区、时钟漂移）
// 的文件当作静置已久 —— 否则它要等到那个未来时刻才会被识别。
func settled(mtime, now time.Time) bool {
	age := now.Sub(mtime)
	return age < 0 || age >= identifyQuiet
}

// itemEpisode 取文件的集号；认不出来返回 0（与播放器的取法一致）。
func itemEpisode(it library.Item) int {
	if it.Episode != nil {
		return *it.Episode
	}
	if it.ParsedNumber != nil {
		return *it.ParsedNumber
	}
	return 0
}

func itemTitle(it library.Item) string {
	if it.ParsedTitle != nil {
		return *it.ParsedTitle
	}
	return ""
}
