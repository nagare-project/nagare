package player

import (
	"context"
	"log"
	"strings"
	"time"

	"github.com/nagare-project/nagare/internal/store"
)

// 补发：「看完了、还没记到账号」的集落在进度里（Completed && !Synced），但失败横幅只在内存里，
// 收尾那次回写又只发一次 —— 回写途中被强退、断网、关机，下次启动就什么都不说、也不再试，
// 账号上静默少一集。补发在启动时（桌面版 main）、登录成功之后、横幅上点「重试」时（以后移动端
// 回到前台时）把它们再写一遍，每集走的是与收尾完全相同的 syncEntry。

const (
	// syncRetryMax 是一轮补发最多试几集：攒了很多（断网看了一整季）也不一口气打上游，剩下的下一轮再补。
	syncRetryMax = 10
	// syncRetryPace 是补发两集之间的间隔，与媒体库识别作品同一个节奏，不挤占服务端的限速。
	syncRetryPace = 1100 * time.Millisecond
	// syncRetryWindow：只补发最近一个月里看完的。更早的「看完未同步」多半是登录之前看的 ——
	// 补发会先把作品加进追番列表，把很久以前看的番一股脑塞进列表不是用户要的。
	syncRetryWindow = 30 * 24 * time.Hour
	// syncHoldTTL：被规则挡下的集，补发隔这么久再试一次（总集数、集号偏移可能补上了）；
	// 还是挡下就再出一次横幅 —— 每周至多一次，不是每次启动都弹。
	syncHoldTTL = 7 * 24 * time.Hour
)

// SyncRetryResult 是一轮补发的结果（POST /api/player/sync/retry 的载荷）。
type SyncRetryResult struct {
	// Synced 是这一轮补记上的集数。
	Synced int `json:"synced"`
	// Remaining 是之后还会再试的集数（下次启动或再点「重试」）。
	Remaining int `json:"remaining"`
	// Reason 说明这一轮为什么没补完（没登录、连不上、被限流、登录过期）；补完了或只是到了单轮上限时为空。
	Reason string `json:"reason,omitempty"`
}

// pendingSync 是一集待补发的「看完」，带着补发要用的匹配。
type pendingSync struct {
	fileID  string
	binding store.Binding
}

// RetryPendingSyncs 补发「看完了、还没记到账号」的集：最早看完的先补，一轮最多 syncRetryMax 集，
// 两集之间隔 retryPace。同一时刻只跑一轮：后来的调用方等前一轮跑完再跑（ctx 先结束就不跑）。
//
// 没登录时什么都不做、不出横幅（登录之后再补）。连不上或被限流时停下、留到下次，也不出横幅 ——
// 开机自启时网络还没好是常事，不是要用户处理的事。被规则挡下、登录过期这类要用户知道的才进横幅；
// 一轮结束后横幅说的那一集记上了就撤掉，否则把「还有几集没记上」更新成现在的数。
func (m *Manager) RetryPendingSyncs(ctx context.Context) SyncRetryResult {
	return m.retryRound(ctx, nil)
}

// KickPendingSyncs 在后台补发一轮、立刻返回。登录成功之后调：启动时那一轮还没登录、什么都没补，
// 不该让用户等到下次启动或自己去点「重试」。
//
// 与 RetryPendingSyncs 过同一道闸（retrySem）：有一轮在跑就等它跑完再跑，单轮上限与节奏不变。
// 已经有一轮排着队、还没轮上时不再多排 —— 它轮上时才取待补列表与登录态，取到的就是最新的；
// 轮上之后再来的照常排一轮（比如补到一半换了账号）。这一轮挂在 retryLife 上而不是调用方的 ctx：
// 登录的答复一发出，请求的 ctx 就结束了；退出时（StopRetrying）它随 retryLife 停下。
func (m *Manager) KickPendingSyncs() {
	if m.retryLife.Err() != nil || !m.retryQueued.CompareAndSwap(false, true) {
		return
	}
	m.kicked.Go(func() {
		started := false
		m.retryRound(m.retryLife, func() {
			started = true
			m.retryQueued.Store(false)
		})
		// 没轮上就结束了（现在只有退出时会这样）：也放开标记，不让它挡住之后的 kick。
		// 轮上过的不在这里清：那时可能已经有更新的一轮排着队，清掉它的标记就会多排一轮。
		if !started {
			m.retryQueued.Store(false)
		}
	})
}

// retryRound 是 RetryPendingSyncs 的本体。onTurn 不为 nil 时，在这一轮拿到 retrySem、真正开跑时调一次
// （没轮上就结束了不调）。
func (m *Manager) retryRound(ctx context.Context, onTurn func()) SyncRetryResult {
	// 退出时（StopRetrying）在跑的这一轮跟着停：HTTP 服务关闭时不取消在途请求的 ctx
	if m.retryLife.Err() != nil {
		return SyncRetryResult{Remaining: len(m.pendingSyncs())}
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	defer context.AfterFunc(m.retryLife, cancel)()
	select {
	case m.retrySem <- struct{}{}:
	case <-ctx.Done():
		return SyncRetryResult{Remaining: len(m.pendingSyncs())}
	}
	defer func() { <-m.retrySem }()
	if onTurn != nil {
		onTurn()
	}

	res := m.retryPass(ctx)
	pending := m.pendingSyncs()
	res.Remaining = len(pending)
	m.settleSyncBanner(pending)
	if res.Synced > 0 || res.Reason != "" {
		log.Printf("player: 补记看完：补上 %d 集，还剩 %d 集 %s", res.Synced, res.Remaining, res.Reason)
	}
	return res
}

// StopRetrying 在退出时调：在跑的补发立刻停下（没补的留到下次启动），之后的补发调用直接返回。
func (m *Manager) StopRetrying() { m.endRetries() }

// retryPass 跑一轮补发（调用方持 retrySem）。只有真去写了（或者写不成）的集占单轮名额；
// 某部作品的集号偏移一时查不到时，这一轮跳过它剩下的集（结果一样，还白打上游）。
func (m *Manager) retryPass(ctx context.Context) SyncRetryResult {
	var res SyncRetryResult
	if m.opts.Client == nil || !m.opts.Client.LoggedIn() {
		res.Reason = "还没有登录账号：到设置里登录之后会自动补记"
		return res
	}
	attempted := 0
	deferred := map[int]bool{}
	for _, c := range m.pendingSyncs() {
		if attempted >= syncRetryMax {
			break
		}
		if deferred[c.binding.AnilistID] {
			continue
		}
		if attempted > 0 && !sleepCtx(ctx, m.retryPace) {
			return res
		}
		r := m.retryOne(ctx, c)
		if ctx.Err() != nil {
			// 退出或网页不等了：这一集算不上失败，留到下次
			return res
		}
		if r.kind != syncSkipped {
			attempted++
		}
		switch r.kind {
		case syncDone:
			res.Synced++
		case syncLater:
			deferred[c.binding.AnilistID] = true
		case syncHeld:
			m.reportSync(c.fileID, c.binding, r.reason, r.recovery)
		case syncOffline:
			res.Reason = r.reason + "，没补记的下次启动时会再试"
			return res
		case syncHalt:
			m.reportSync(c.fileID, c.binding, r.reason, r.recovery)
			res.Reason = r.reason
			return res
		}
	}
	return res
}

// retryOne 补发一集：与收尾同样的检查与写法，同样的时限；别处正在写这一集就交给它。
// 列表是这一轮开头取的，前面几集每集最多等十秒 —— 这期间媒体库可能重扫过（集号、类型、关联都可能变），
// 所以轮到它时再核对一次缓存的匹配是否仍然站得住。
func (m *Manager) retryOne(ctx context.Context, c pendingSync) syncResult {
	if b, ok := m.retryTarget(c.fileID); !ok || b != c.binding {
		return syncResult{}
	}
	ctx, cancel := context.WithTimeout(ctx, markWatchedTimeout)
	defer cancel()
	return m.syncEntry(ctx, c.fileID, c.binding, false)
}

// pendingSyncs 列出补发会去试的集，最早看完的在前：最近 syncRetryWindow 里看完、还没记到账号、
// 缓存的匹配今天仍然站得住（retryTarget），且没有按这条匹配被规则挡下过。
func (m *Manager) pendingSyncs() []pendingSync {
	now := time.Now()
	since := now.Add(-syncRetryWindow).UnixMilli()
	var out []pendingSync
	for _, e := range m.opts.Store.PendingSyncs(since) {
		b, ok := m.retryTarget(e.FileID)
		if !ok || holdStands(e.Progress.SyncHold, b, now) {
			continue
		}
		out = append(out, pendingSync{fileID: e.FileID, binding: b})
	}
	return out
}

// retryTarget 取补发要用的匹配：store 里这个文件的匹配，且播放路径今天仍会照用它 ——
// 与作品关联不冲突（标为「不是目录里的作品」的一律不写）；文件在媒体库里时，集号还是文件现在
// 解析出的那个（共享语料更新后播放路径会重新匹配），认定了作品的分组里附加内容不回写
// （特典 / OVA 不是那部作品的第 N 集，见 extraKinds）。
// 补发不重新匹配（要读文件头、问服务端）：今天会被播放路径作废的匹配，留给重看时处理。
func (m *Manager) retryTarget(fileID string) (store.Binding, bool) {
	b, ok := m.opts.Store.Binding(fileID)
	if !ok || b.AnilistID <= 0 || b.Episode <= 0 || !m.agreesWithAssociation(fileID, b) {
		return store.Binding{}, false
	}
	if m.opts.LibraryItem == nil {
		return b, true
	}
	item, inLibrary := m.opts.LibraryItem(fileID)
	if !inLibrary {
		// 磁力、在线候选本来就不在媒体库里，播放路径对它们也只认缓存的匹配。其余不在媒体库里的
		// 是本地文件此刻不在（外接盘没插、文件删了或挪了）：查不到它的作品关联（可能标过「不是
		// 目录里的作品」），也核对不了集号与类型 —— 不补，等它回到媒体库里再说。
		return b, streamFileID(fileID)
	}
	if episodeNumber(item) != b.Episode {
		return store.Binding{}, false
	}
	if _, associated := m.association(fileID); associated && extraKinds[item.ParsedKind] {
		return store.Binding{}, false
	}
	return b, true
}

// settleSyncBanner 在一轮补发之后整理横幅：它说的那一集仍待补发就更新「还有几集」，仍被规则挡下
// （要用户去手动标）就留着，其余情况（已经记上、关联改了不再该写）撤掉。
func (m *Manager) settleSyncBanner(pending []pendingSync) {
	m.mu.Lock()
	cur := m.lastSync
	m.mu.Unlock()
	if cur == nil {
		return
	}
	keep := m.stillHeld(cur.FileID)
	for _, c := range pending {
		if c.fileID == cur.FileID {
			keep = true
			break
		}
	}
	var next *SyncInfo
	if keep {
		updated := *cur
		updated.Pending = len(pending)
		next = &updated
	}
	m.mu.Lock()
	// 这期间收尾又挂了一条新的失败：以新的为准
	if m.lastSync == cur {
		m.lastSync = next
	}
	m.mu.Unlock()
}

// stillHeld：fileID 还没记到账号，且按它现在的匹配回写已经被规则挡下过（只能用户自己去标）。
func (m *Manager) stillHeld(fileID string) bool {
	p, ok := m.opts.Store.Progress(fileID)
	if !ok || !p.Completed || p.Synced {
		return false
	}
	b, ok := m.retryTarget(fileID)
	return ok && holdStands(p.SyncHold, b, time.Now())
}

// holdStands：按匹配 b 回写被挡下过，且还没到再试一次的时候（syncHoldTTL）。
func holdStands(h store.SyncHold, b store.Binding, now time.Time) bool {
	return h.Holds(b.AnilistID, b.Episode) && now.Sub(time.UnixMilli(h.At)) < syncHoldTTL
}

// streamFileID：不在媒体库里的媒体源的 FileID —— 磁力（torrentstream 的 torrentFileID：「t:」+ infohash）
// 与在线候选（remoteFileIDPrefix + 摘要）。本地文件的 FileID 是「文件名|大小|修改时间」。
func streamFileID(fileID string) bool {
	if strings.HasPrefix(fileID, "t:") {
		return true
	}
	rest, ok := strings.CutPrefix(fileID, remoteFileIDPrefix)
	return ok && !strings.Contains(rest, "|")
}

// sleepCtx 睡 d；ctx 先结束返回 false。
func sleepCtx(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-ctx.Done():
		return false
	}
}
