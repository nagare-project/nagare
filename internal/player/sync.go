package player

import (
	"context"
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/nagare-project/nagare/internal/animego"
	"github.com/nagare-project/nagare/internal/store"
)

// 「看完」回写账号。播放收尾（syncWatched）与启动 / 「重试」时的补发（sync_retry.go）走同一条路
// （syncEntry）：同样的关联核对、集号换算、写前写后的复核，两边不分叉。

// syncKind 是一次回写的结果类别：收尾据此决定挂不挂横幅，补发据此决定继续、停下还是跳过。
type syncKind int

const (
	// syncSkipped：这次不写，也不算失败（已回写、没登录、没有匹配、关联改了、别处正在写）。
	syncSkipped syncKind = iota
	// syncDone：写进账号了。
	syncDone
	// syncLater：一时写不了（集号偏移这次没查到），留着下次再试。
	syncLater
	// syncHeld：被规则挡下（集号换算不出来、服务端拒收这一集），再试也一样：补发不再拿同一条匹配去试。
	syncHeld
	// syncOffline：连不上或被限流 —— 补发时后面几集也会一样，就此停下。
	syncOffline
	// syncHalt：登录过期或服务端整体不接受这个请求：补发停下，横幅说清楚。
	syncHalt
)

// syncResult 是一次回写的结果；失败时 reason / recovery 是给用户看的原因与一步动作。
type syncResult struct {
	kind     syncKind
	reason   string
	recovery string
}

func (r syncResult) failed() bool { return r.kind != syncSkipped && r.kind != syncDone }

// 回写失败时给用户的恢复动作。
const (
	recoveryPlaceManually = "先确认它是作品的第几集，再在 animego 网站上手动标记（不要按文件里的编号标）"
	recoveryRetryLater    = "下次启动 nagare 时会自动再试，也可以过一会儿点「重试」"
)

// syncWatched 把「这一集看完了」回写账号（播放收尾经 kickSync 放到后台跑，不拖住窗口的事件循环）。
//
// 失败只记日志是不够的：它发生在 mpv 已经退出之后，用户面前什么都不会变 —— 他以为这一集记上了，
// 下次打开网站才发现没有。所以挂进 Status 让界面说出来；没记上的这一集留在进度里，
// 下次启动时补发，横幅上的「重试」也能当场再试（sync_retry.go）。
func (m *Manager) syncWatched(sess *session) {
	ctx, cancel := context.WithTimeout(context.Background(), markWatchedTimeout)
	defer cancel()
	b := sess.bindingSnapshot()
	if r := m.syncEntry(ctx, sess.item.FileID, b, true); r.failed() {
		m.reportSync(sess.item.FileID, b, r.reason, r.recovery)
	}
}

// syncEntry 按匹配 b 把 fileID「看完了」写进账号：进度确实看完且没回写、登录着、匹配与文件现在的
// 作品关联不冲突、集号换算得出来，才写。查集数与写请求都可能等一阵，这期间用户可能在媒体库里
// 改了关联（它会作废这条匹配），所以写之前、写之后各再核对一次。
//
// 同一个文件同一时刻只有一处在写：wait 为真时等别处写完再接着判断（那时多半已经回写过了，
// 直接跳过）；为假时直接跳过（补发：别处正在写，就交给它）。
func (m *Manager) syncEntry(ctx context.Context, fileID string, b store.Binding, wait bool) syncResult {
	if !m.beginSync(ctx, fileID, wait) {
		return syncResult{}
	}
	defer m.endSync(fileID)

	p, ok := m.opts.Store.Progress(fileID)
	if !ok || !p.Completed || p.Synced {
		return syncResult{}
	}
	if b.AnilistID <= 0 || b.Episode <= 0 || m.opts.Client == nil || !m.opts.Client.LoggedIn() {
		return syncResult{}
	}
	if !m.bindingStillCurrent(fileID, b) {
		log.Printf("player: 作品关联已改，不按旧匹配回写（anilistId=%d 第%d集）", b.AnilistID, b.Episode)
		return syncResult{}
	}

	// 文件里的集号 → 作品自己的集号（跨季连续编号的第二季「38」是第 10 集）
	place := m.placeEpisode(ctx, fileID, b.AnilistID, b.Episode)
	if place.Episode <= 0 {
		log.Printf("player: 集号无法换算成作品集号，不回写（anilistId=%d 文件里第%d集）", b.AnilistID, b.Episode)
		if place.Retry {
			return syncResult{kind: syncLater, reason: place.Reason, recovery: recoveryRetryLater}
		}
		m.recordHold(fileID, b)
		return syncResult{kind: syncHeld, reason: place.Reason, recovery: recoveryPlaceManually}
	}
	// 查集数可能等了一阵，再核对一次：这期间关联被改了就不写
	if !m.bindingStillCurrent(fileID, b) {
		log.Printf("player: 作品关联已改，不按旧匹配回写（anilistId=%d 第%d集）", b.AnilistID, b.Episode)
		return syncResult{}
	}
	if err := m.markWatched(ctx, b.AnilistID, place.Episode); err != nil {
		r := classifySyncErr(err)
		if r.kind == syncHeld {
			m.recordHold(fileID, b)
		}
		log.Printf("player: 回写看完标记失败（下次启动或点「重试」时会再试）：%v", err)
		return r
	}
	// 成功就把这一集上一次的失败留痕清掉，横幅跟着消失，不用用户手动关。留痕说的是别的集
	// （同一个窗口里连着看，上一集没记上）就留着：那件事仍然成立，不能被这一集的成功盖掉。
	m.clearSyncFailure(fileID)
	// 回写途中关联被改了：这一集已经写到了旧作品上，但不能标成「已回写」——
	// 否则重看这一集看完时也不会再按新关联写一次。
	if !m.bindingStillCurrent(fileID, b) {
		log.Printf("player: 回写途中作品关联已改，这一集不标记为已回写")
		return syncResult{}
	}
	m.recordSynced(fileID, place.Episode)
	if place.Episode != b.Episode {
		log.Printf("player: 已回写 animego：anilistId=%d 第%d集（文件里是第%d集）", b.AnilistID, place.Episode, b.Episode)
	} else {
		log.Printf("player: 已回写 animego：anilistId=%d 第%d集", b.AnilistID, place.Episode)
	}
	return syncResult{kind: syncDone}
}

// beginSync 占住 fileID 的回写权。别处正在写时：wait 为假直接返回 false；为真则等它写完再抢，
// ctx 先结束就返回 false。返回 true 的调用方写完必须调 endSync。
func (m *Manager) beginSync(ctx context.Context, fileID string, wait bool) bool {
	for {
		m.mu.Lock()
		busy, taken := m.syncing[fileID]
		if !taken {
			if m.syncing == nil {
				m.syncing = map[string]chan struct{}{}
			}
			m.syncing[fileID] = make(chan struct{})
			m.mu.Unlock()
			return true
		}
		m.mu.Unlock()
		if !wait {
			return false
		}
		select {
		case <-busy:
		case <-ctx.Done():
			return false
		}
	}
}

// endSync 交还 fileID 的回写权，叫醒在等的那一处。
func (m *Manager) endSync(fileID string) {
	m.mu.Lock()
	if done, ok := m.syncing[fileID]; ok {
		close(done)
		delete(m.syncing, fileID)
	}
	m.mu.Unlock()
}

// markWatched 先确保订阅存在，再标记这一集看完（episode 是作品集号）。
func (m *Manager) markWatched(ctx context.Context, anilistID, episode int) error {
	if err := m.opts.Client.EnsureSubscription(ctx, anilistID); err != nil {
		return err
	}
	return m.opts.Client.MarkWatched(ctx, anilistID, episode)
}

// reportSync 把「这一集没记到账号」挂进 Status，媒体库页据此出横幅（只留最近一条，
// 另外几集没记上的用 Pending 说个数）。Episode 是文件里的集号 —— 用户在界面上看到的是它。
func (m *Manager) reportSync(fileID string, b store.Binding, reason, recovery string) {
	pending := len(m.pendingSyncs())
	m.setSyncFailure(&SyncInfo{
		State:    "failed",
		FileID:   fileID,
		Title:    b.Title,
		Episode:  b.Episode,
		Reason:   reason,
		Recovery: recovery,
		At:       time.Now().UnixMilli(),
		Pending:  pending,
	})
}

// recordSynced 标记这一集已回写，并记下写进账号的作品集号。重读一次再置位：
// 网络请求期间进度可能被别处改过，不能用开头那份整个盖回去。
func (m *Manager) recordSynced(fileID string, episode int) {
	p, ok := m.opts.Store.Progress(fileID)
	if !ok {
		return
	}
	p.Synced, p.SyncedEpisode, p.SyncHold = true, episode, store.SyncHold{}
	if err := m.opts.Store.SetProgress(fileID, p); err != nil {
		log.Printf("player: 记录同步状态失败：%v", err)
	}
}

// recordHold 记下这一集按匹配 b 回写被规则挡下了：补发不再拿同一条匹配去试
// （每次都会以同样的理由失败，横幅天天弹）。同样重读一次再改。
func (m *Manager) recordHold(fileID string, b store.Binding) {
	p, ok := m.opts.Store.Progress(fileID)
	if !ok || p.Synced {
		return
	}
	p.SyncHold = store.SyncHold{AnilistID: b.AnilistID, Episode: b.Episode}
	if err := m.opts.Store.SetProgress(fileID, p); err != nil {
		log.Printf("player: 记录回写被挡下失败：%v", err)
	}
}

// bindingStillCurrent：store 里这个文件的匹配仍是会话开播时那一条（同一部作品、同一集），
// 并且与文件现在的作品关联不冲突。改关联会作废不一致的匹配；后台匹配迟到时
// 又可能把它写回来，所以关联本身也要再看一眼。
func (m *Manager) bindingStillCurrent(fileID string, b store.Binding) bool {
	cur, ok := m.opts.Store.Binding(fileID)
	return ok && cur.AnilistID == b.AnilistID && cur.Episode == b.Episode && m.agreesWithAssociation(fileID, b)
}

// classifySyncErr 把回写失败翻成「用户看到什么、能做什么」，并归类：补发据此决定继续还是停下。
// 与 classifyAnimegoErr 分开：弹幕失败的落款是「本地播放不受影响」，而这里丢的是账号进度，
// 恢复动作完全不同。
func classifySyncErr(err error) syncResult {
	var ae *animego.Error
	if errors.As(err, &ae) {
		switch ae.Kind {
		case animego.ErrUnavailable:
			return syncResult{kind: syncOffline, reason: "animego 服务暂不可达", recovery: "确认网络后点「重试」；下次启动 nagare 时也会自动再试"}
		case animego.ErrRateLimited:
			return syncResult{kind: syncOffline, reason: "请求过于频繁被限流", recovery: "过几分钟后点「重试」；下次启动 nagare 时也会自动再试"}
		case animego.ErrAuthExpired:
			return syncResult{kind: syncHalt, reason: "登录已过期", recovery: "到设置里重新登录，然后点「重试」"}
		case animego.ErrBadRequest:
			// 400 说的是「这一次请求的内容不对」：这一集再试也一样。403 / 404 这类说的是接口整体
			// 不让用，可能是服务端变了 —— 停下、不挡这一集，升级之后还能再补。
			if ae.Status == http.StatusBadRequest {
				return syncResult{kind: syncHeld, reason: "回写失败：" + err.Error(), recovery: "可以到 animego 网站上手动标记这一集"}
			}
		}
	}
	return syncResult{kind: syncHalt, reason: "回写失败：" + err.Error(), recovery: "可以到 animego 网站上手动标记这一集"}
}
