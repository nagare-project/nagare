// 磁力边下边播的 HTTP 端点（M3）。
//
// POST /api/torrent/play 是【阻塞】的：等元数据 + 等起播缓冲可能耗上几十秒。
// 界面在请求在途期间并行轮询 GET /api/torrent/status 显示分阶段进展与实时
// peer/速度，并可随时 POST /api/torrent/stop 取消 —— 这样「响应即结果」的
// 简单语义与「等待可观测」两件事都拿到，不必引入异步任务状态机。
package api

import (
	"context"
	"net/http"
	"strings"

	"github.com/nagare-project/nagare/internal/httpserver"
	"github.com/nagare-project/nagare/internal/player"
	"github.com/nagare-project/nagare/internal/store"
	"github.com/nagare-project/nagare/internal/torrentstream"
)

// TorrentAPI 是处理器需要的磁力引擎能力子集（接口收窄便于注入测试替身）。
type TorrentAPI interface {
	Prepare(ctx context.Context, req torrentstream.PrepareRequest) (torrentstream.PrepareResult, error)
	Stop()
	Status() torrentstream.Status
	CacheBytes() int64
	ClearCache() error
	SetConfig(c torrentstream.Config) (bool, error)
	RestartRequired() bool
}

// 编译期断言：磁力来源必须满足播放管线的媒体来源接缝。两个包刻意互不 import
// （torrentstream 依赖 player 会形成反向依赖），这里是它们唯一的交汇点 ——
// 接口一旦漂移，编译在这一行就断，而不是等到运行时才发现磁力播不了。
var _ player.MediaSource = (*torrentstream.Source)(nil)

// torrentUnavailable 在未启用磁力引擎时统一作答（引擎初始化失败时降级运行）。
// 降级必须可见：界面据此禁用播放按钮并指向日志，而不是让按钮点了没反应。
func (h *Handler) torrentUnavailable(w http.ResponseWriter) bool {
	if h.deps.Torrent != nil {
		return false
	}
	httpserver.WriteError(w, http.StatusServiceUnavailable, "磁力播放未启用，请查看日志里的启动错误")
	return true
}

// torrentPlay 走完整条链：加入磁力 → 选集（可能要用户手选）→ 起播缓冲 →
// 交给播放管线（弹幕、进度、看完同步与本地文件完全同一条路）。
func (h *Handler) torrentPlay(w http.ResponseWriter, r *http.Request) {
	if h.torrentUnavailable(w) {
		return
	}
	var req struct {
		Magnet      string `json:"magnet"`
		TorrentURL  string `json:"torrentUrl"`
		Title       string `json:"title"`
		EpisodeHint int    `json:"episodeHint"`
		// FileIndex 用指针：0 是合法下标，零值分不出「用户选了第 0 个」与「还没选」。
		FileIndex *int `json:"fileIndex"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	if (strings.TrimSpace(req.Magnet) == "") == (strings.TrimSpace(req.TorrentURL) == "") {
		httpserver.WriteError(w, http.StatusBadRequest, "需要提供磁力链接或种子文件地址")
		return
	}
	fileIndex := -1
	if req.FileIndex != nil {
		fileIndex = *req.FileIndex
	}

	// 先停掉现有播放会话，再准备新种子。顺序不能反：Player.Stop 会触发会话终结
	// 回调去停磁力引擎，若放在 Prepare 之后，旧会话的收尾会把刚建好的新种子掐掉。
	h.deps.Player.Stop()

	res, err := h.deps.Torrent.Prepare(r.Context(), torrentstream.PrepareRequest{
		Magnet:      req.Magnet,
		TorrentURL:  req.TorrentURL,
		Title:       req.Title,
		EpisodeHint: req.EpisodeHint,
		FileIndex:   fileIndex,
	})
	if err != nil {
		writeErr(w, err)
		return
	}
	if res.NeedSelection {
		// 种子仍留着：用户马上会带 fileIndex 重发，重新等一轮元数据是白等。
		// 用户放弃选集时由界面 POST /api/torrent/stop 释放。
		httpserver.WriteJSON(w, http.StatusOK, map[string]any{
			"needSelection": true,
			"files":         res.Files,
		})
		return
	}

	pr, err := h.deps.Player.Play(r.Context(), res.Source, "")
	if err != nil {
		// mpv 起不来就别把种子挂着继续占带宽和磁盘。
		h.deps.Torrent.Stop()
		writeErr(w, err)
		return
	}
	httpserver.WriteJSON(w, http.StatusOK, map[string]any{
		"needSelection": false,
		"title":         pr.Title,
		"danmaku":       pr.Danmaku,
	})
}

func (h *Handler) torrentStatus(w http.ResponseWriter, _ *http.Request) {
	if h.torrentUnavailable(w) {
		return
	}
	httpserver.WriteJSON(w, http.StatusOK, h.deps.Torrent.Status())
}

// torrentStop 同时收掉播放器与种子：只停一边会留下一个还在下载的种子，
// 或者一个对着已断流地址空转的 mpv。
func (h *Handler) torrentStop(w http.ResponseWriter, _ *http.Request) {
	if h.torrentUnavailable(w) {
		return
	}
	h.deps.Player.Stop()
	h.deps.Torrent.Stop()
	httpserver.WriteJSON(w, http.StatusOK, map[string]any{})
}

// torrentCacheClear 清空缓存。返回的是【实际剩余】字节数而不是写死的 0：
// 文件被占用（Windows 常见）时只清掉了一部分，报 0 就是撒谎。
func (h *Handler) torrentCacheClear(w http.ResponseWriter, _ *http.Request) {
	if h.torrentUnavailable(w) {
		return
	}
	h.deps.Player.Stop()
	if err := h.deps.Torrent.ClearCache(); err != nil {
		writeErr(w, err)
		return
	}
	httpserver.WriteJSON(w, http.StatusOK, map[string]any{"cacheBytes": h.deps.Torrent.CacheBytes()})
}

// torrentConfig 落盘用户配置并更新引擎。端口与端口映射类改动在没有播放会话时
// 会就地重建引擎，正在播时不动 —— 那种情况如实回 restartRequired，绝不假装已生效。
func (h *Handler) torrentConfig(w http.ResponseWriter, r *http.Request) {
	if h.torrentUnavailable(w) {
		return
	}
	var req struct {
		Seeding            *bool     `json:"seeding"`
		Trackers           *[]string `json:"trackers"`
		UseDefaultTrackers *bool     `json:"useDefaultTrackers"`
		PortForwarding     *bool     `json:"portForwarding"`
		ListenPort         *int      `json:"listenPort"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	if req.ListenPort != nil && (*req.ListenPort < 0 || *req.ListenPort > 65535) {
		httpserver.WriteError(w, http.StatusBadRequest, "监听端口要在 0–65535 之间（0 表示由系统分配）")
		return
	}
	// tracker 的合法性判定只有一份实现（引擎侧），在边界上复用它，
	// 这样被丢掉的行能当场告诉用户，而不是静默消失。
	var rejected []string
	if req.Trackers != nil {
		kept, dropped := torrentstream.NormalizeTrackers(*req.Trackers)
		rejected = dropped
		*req.Trackers = kept
	}

	cfg, err := h.deps.Store.UpdateTorrentConfig(func(c *store.TorrentConfig) {
		if req.Seeding != nil {
			c.Seeding = *req.Seeding
		}
		if req.Trackers != nil {
			c.Trackers = *req.Trackers
		}
		if req.UseDefaultTrackers != nil {
			c.DisableDefaultTrackers = !*req.UseDefaultTrackers
		}
		if req.PortForwarding != nil {
			c.PortForwarding = *req.PortForwarding
		}
		if req.ListenPort != nil {
			c.ListenPort = *req.ListenPort
		}
	})
	if err != nil {
		writeErr(w, err)
		return
	}

	if _, err := h.deps.Torrent.SetConfig(engineConfig(cfg)); err != nil {
		writeErr(w, err)
		return
	}
	view := torrentView(cfg, h.deps.Torrent, h.deps.TorrentCacheDir)
	if len(rejected) > 0 {
		view["rejectedTrackers"] = rejected
	}
	httpserver.WriteJSON(w, http.StatusOK, view)
}

// engineConfig 把持久化配置翻成引擎配置。两个类型故意不共用：store 管落盘形状，
// torrentstream 管运行时形状，各自可以独立演进而不牵动对方。
func engineConfig(c store.TorrentConfig) torrentstream.Config {
	return torrentstream.Config{
		Seeding:        c.Seeding,
		Trackers:       torrentstream.EffectiveTrackers(!c.DisableDefaultTrackers, c.Trackers),
		PortForwarding: c.PortForwarding,
		ListenPort:     c.ListenPort,
	}
}

// torrentView 是设置页与配置端点共用的磁力视图。引擎为 nil（未启用）时
// 只回配置并把 enabled 标成 false，界面据此显示降级提示。
func torrentView(c store.TorrentConfig, eng TorrentAPI, cacheDir string) map[string]any {
	trackers := c.Trackers
	if trackers == nil {
		trackers = []string{} // 前端要数组，不要 null
	}
	view := map[string]any{
		"enabled":            eng != nil,
		"seeding":            c.Seeding,
		"trackers":           trackers,
		"useDefaultTrackers": !c.DisableDefaultTrackers,
		"defaultTrackers":    append([]string(nil), torrentstream.DefaultTrackers...),
		"portForwarding":     c.PortForwarding,
		"listenPort":         c.ListenPort,
		"cacheDir":           cacheDir,
		"cacheBytes":         int64(0),
		"restartRequired":    false,
	}
	if eng != nil {
		view["cacheBytes"] = eng.CacheBytes()
		// 粘性标志：改了端口却正在播时重启才生效，这个提示必须活过一次页面刷新，
		// 否则用户刷新一下就以为改动已经生效了。
		view["restartRequired"] = eng.RestartRequired()
	}
	return view
}
