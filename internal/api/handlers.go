// Package api 实现 /api/* 业务端点，经 httpserver.Options.RegisterAPI 挂进
// 鉴权链之内 —— 这里的每个路由天然带 Host 白名单 + token + CSRF 三层防护。
//
// 磁力、来源插件与磁力源规则的端点不在这里，在子包 api/sources，只由桌面版的 main 注册：
// 本包不依赖 torrentstream / sourceplugin / rules / rulesync / anacrolix（CI 守着这条依赖图）。
package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"

	"github.com/nagare-project/nagare/internal/animego"
	errs "github.com/nagare-project/nagare/internal/errors"
	"github.com/nagare-project/nagare/internal/httpserver"
	"github.com/nagare-project/nagare/internal/mpv"
	"github.com/nagare-project/nagare/internal/player"
	"github.com/nagare-project/nagare/internal/store"
)

// MaxBodyBytes 是请求体上限（本地 API，1MB 足够）。api/sources 用同一个值。
const MaxBodyBytes = 1 << 20

// PlayerAPI 是处理器需要的播放能力子集。
type PlayerAPI interface {
	Play(ctx context.Context, src player.MediaSource, subPath string) (player.PlayResult, error)
	Stop()
	SetPause(v bool) error
	Seek(seconds float64) error
	Status() player.Status
	// RefreshShaders 把当前的画质增强设置套到正在播放的窗口上；返回是否在播。
	RefreshShaders() (bool, error)
	// RetryPendingSyncs 补发看完了、还没记到账号的集（横幅上的「重试」）。
	RetryPendingSyncs(ctx context.Context) player.SyncRetryResult
}

// AnimegoAuth 是处理器需要的 animego 会话能力子集。
type AnimegoAuth interface {
	Login(ctx context.Context, email, password string) (animego.User, error)
	RestoreSession(s animego.Session)
	Session() animego.Session
	LoggedIn() bool
}

// Deps 是全部依赖注入点。
type Deps struct {
	Store          *store.Store
	Lib            *LibraryService
	Player         PlayerAPI
	Auth           AnimegoAuth
	Lists          ListsBackend
	Catalog        CatalogReader
	RemoteArt      *RemoteArt
	AnimegoBaseURL string
	// CatalogCacheDir 是公开目录的磁盘缓存目录（<配置目录>/cache/catalog）；空 = 只缓存在内存里。
	CatalogCacheDir string
	MPV             *mpv.Runtime // 共享探测状态：设置页读、/api/mpv/detect 刷新、播放取路径
	// EmbeddedMPV：播放在桌面窗口里的进程内 libmpv 上（internal/desktop），不要命令行 mpv。
	// 设置载荷里的 mpv 据此报「已就绪 · 内置」，界面不再提示安装。
	EmbeddedMPV bool
	// Desktop 报告桌面窗口此刻的状态（设置页的「播放窗口」一行）；nil = 这次运行没有桌面窗口可谈。
	Desktop func() DesktopView
	Version string
	// TorrentSettings 给设置载荷的 torrent 一项（磁力引擎、下载目录、tracker 的视图），
	// 由 api/sources 提供（sources.Handler.TorrentSettings）；nil = 这个构建没有磁力，设置载荷里不出这一项。
	TorrentSettings func() map[string]any
	// EpisodeSpaces 查作品的集号空间（与播放管线共用一份缓存）；nil 时集号偏移端点报 503。
	EpisodeSpaces *EpisodeSpaces
	// Shutdown 触发整个进程退出（主进程的根 cancel）；nil 表示不支持从界面退出。
	Shutdown func()
	// DataDir / LogPath 展示给用户：数据在哪、出问题看哪个文件。
	DataDir string
	LogPath string
	// BackgroundMode 是本次运行的后台形态（tray.Mode 的字符串值：none / menubar / dock / tray / window），
	// 界面据此告诉用户「关掉标签页（桌面窗口里是关掉窗口）后去哪里找它、怎么退出」。
	// window 是后加的：用桌面窗口时取代 dock / menubar / tray，其余四个值的含义不变。
	BackgroundMode string
}

// Handler 汇集全部业务端点。
type Handler struct {
	deps      Deps
	catalog   *DiscoverService
	lists     *ListsService
	remoteArt *RemoteArt
	episodes  *episodeMetadataService
	// coverCache 是封面缓存（cover_cache.go），启动时注入
	coverCache coverCacheSlot
}

// New 构造 Handler。
func New(deps Deps) *Handler {
	art := deps.RemoteArt
	if art == nil {
		art = NewRemoteArt(deps.AnimegoBaseURL, 4096)
	}
	catalog := NewDiscoverService(deps.Catalog, art)
	if deps.CatalogCacheDir != "" {
		// 起不来只是离线时发现页没有上一份可显示，在线功能不受影响
		if disk, err := newCatalogDisk(deps.CatalogCacheDir, catalogDiskMaxBytes); err != nil {
			log.Printf("目录磁盘缓存不可用（离线时发现页没有上一份可显示）：%v", err)
		} else {
			catalog.useDisk(disk)
		}
	}
	return &Handler{deps: deps, catalog: catalog, lists: NewListsService(deps.Lists, catalog), remoteArt: art, episodes: newEpisodeMetadataService(art)}
}

// Register 把业务路由注册进 /api/* 的鉴权链（httpserver.Options.RegisterAPI 的挂载点）。
func (h *Handler) Register(mux *http.ServeMux) {
	h.registerCatalog(mux)
	mux.HandleFunc("GET /api/library", h.getLibrary)
	mux.HandleFunc("POST /api/library/folders", h.addFolder)
	mux.HandleFunc("DELETE /api/library/folders/{id}", h.removeFolder)
	mux.HandleFunc("POST /api/library/rescan", h.rescan)
	mux.HandleFunc("PUT /api/library/association", h.putAssociation)
	mux.HandleFunc("POST /api/library/association/dismiss-synced", h.dismissSyncedElsewhere)
	mux.HandleFunc("GET /api/fs/dirs", h.browseDirs)
	mux.HandleFunc("POST /api/play", h.play)
	mux.HandleFunc("GET /api/player/status", h.playerStatus)
	mux.HandleFunc("POST /api/player/stop", h.playerStop)
	mux.HandleFunc("POST /api/player/pause", h.playerPause)
	mux.HandleFunc("POST /api/player/seek", h.playerSeek)
	mux.HandleFunc("POST /api/player/config", h.playerConfig)
	mux.HandleFunc("POST /api/player/sync/retry", h.playerSyncRetry)
	mux.HandleFunc("GET /api/settings", h.settings)
	mux.HandleFunc("POST /api/mpv/detect", h.mpvDetect)
	mux.HandleFunc("POST /api/shutdown", h.shutdown)
	mux.HandleFunc("POST /api/cover-cache/clear", h.clearCoverCache)
	mux.HandleFunc("POST /api/animego/login", h.login)
	mux.HandleFunc("POST /api/animego/logout", h.logout)
	// /api/search*、/api/sources*、/api/source-plugin/*、/api/torrent/*、/api/downloads 由 api/sources 注册
}

// DecodeBody 是给子包（api/sources）用的 decodeBody：请求体限长与错误措辞全站一份。
func DecodeBody(w http.ResponseWriter, r *http.Request, out any) bool { return decodeBody(w, r, out) }

// WriteErr 是给子包（api/sources）用的 writeErr：分类错误到状态码的映射全站一份。
func WriteErr(w http.ResponseWriter, err error) { writeErr(w, err) }

// decodeBody 解析 JSON 请求体（限长）。失败返回 false 且已写响应。
func decodeBody(w http.ResponseWriter, r *http.Request, out any) bool {
	body, err := io.ReadAll(io.LimitReader(r.Body, MaxBodyBytes))
	if err != nil {
		httpserver.WriteError(w, http.StatusBadRequest, "读取请求体失败")
		return false
	}
	if err := json.Unmarshal(body, out); err != nil {
		httpserver.WriteError(w, http.StatusBadRequest, "请求体不是合法的 JSON")
		return false
	}
	return true
}

// writeErr 把分类错误映射成 HTTP 状态码 + 用户可读中文。
func writeErr(w http.ResponseWriter, err error) {
	// 浏览器切页、刷新或关闭弹窗会主动取消在途读取。这是正常生命周期，
	// 连接已经断开，不再写 500，也不把日志刷成“内部错误”。
	if errors.Is(err, context.Canceled) {
		return
	}
	var ce *errs.E
	if errors.As(err, &ce) {
		status := http.StatusInternalServerError
		switch ce.Category {
		case errs.CategoryInput:
			status = http.StatusBadRequest
		case errs.CategoryFS:
			status = http.StatusNotFound
		case errs.CategoryAuth:
			status = http.StatusUnauthorized
		case errs.CategoryNetwork, errs.CategoryUpstream, errs.CategoryTorrent:
			// 磁力失败归 502：问题在 swarm 那一侧（没人分享、缓冲太慢），
			// 与本机无关，用户的恢复动作是换一条资源或稍后重试。
			status = http.StatusBadGateway
		case errs.CategoryPlayback, errs.CategoryInternal, errs.CategoryStorage:
			status = http.StatusInternalServerError
		}
		httpserver.WriteError(w, status, ce.UserFacing())
		return
	}
	var partial *animego.ListEditError
	if errors.As(err, &partial) {
		status := http.StatusBadGateway
		var cause *animego.Error
		if errors.As(partial, &cause) {
			switch cause.Kind {
			case animego.ErrRateLimited:
				status = 429
			case animego.ErrAuthExpired:
				status = 401
			case animego.ErrBadRequest:
				status = 400
			}
		}
		httpserver.WriteError(w, status, partial.Error())
		return
	}
	var ae *animego.Error
	if errors.As(err, &ae) {
		status := http.StatusBadGateway
		switch ae.Kind {
		case animego.ErrAuthExpired:
			status = http.StatusUnauthorized
		case animego.ErrRateLimited:
			status = http.StatusTooManyRequests
		case animego.ErrBadRequest:
			status = http.StatusBadRequest
		}
		httpserver.WriteError(w, status, ae.Error())
		return
	}
	log.Printf("api: 未分类错误：%v", err)
	httpserver.WriteError(w, http.StatusInternalServerError, "内部错误，请查看终端日志")
}

// ── 库 ──

func (h *Handler) getLibrary(w http.ResponseWriter, _ *http.Request) {
	httpserver.WriteJSON(w, http.StatusOK, h.deps.Lib.View())
}

func (h *Handler) addFolder(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path string `json:"path"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	folder, stats, err := h.deps.Lib.AddFolder(req.Path)
	if err != nil {
		writeErr(w, err)
		return
	}
	httpserver.WriteJSON(w, http.StatusOK, map[string]any{"folder": folder, "stats": stats})
}

func (h *Handler) removeFolder(w http.ResponseWriter, r *http.Request) {
	ok, err := h.deps.Lib.RemoveFolder(r.PathValue("id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	if !ok {
		httpserver.WriteError(w, http.StatusNotFound, "找不到该库目录")
		return
	}
	httpserver.WriteJSON(w, http.StatusOK, map[string]any{})
}

func (h *Handler) rescan(w http.ResponseWriter, _ *http.Request) {
	httpserver.WriteJSON(w, http.StatusOK, map[string]any{"stats": h.deps.Lib.Rescan()})
}

// ── 播放 ──

func (h *Handler) play(w http.ResponseWriter, r *http.Request) {
	var req struct {
		FileID string `json:"fileId"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	item, ok := h.deps.Lib.Item(req.FileID)
	if !ok {
		httpserver.WriteError(w, http.StatusNotFound, "文件不在当前库里，试试重新扫描")
		return
	}
	res, err := h.deps.Player.Play(r.Context(), player.NewLocalSource(item), h.deps.Lib.SubtitlePath(req.FileID))
	if err != nil {
		writeErr(w, err)
		return
	}
	httpserver.WriteJSON(w, http.StatusOK, res)
}

func (h *Handler) playerStatus(w http.ResponseWriter, _ *http.Request) {
	httpserver.WriteJSON(w, http.StatusOK, h.deps.Player.Status())
}

func (h *Handler) playerStop(w http.ResponseWriter, _ *http.Request) {
	h.deps.Player.Stop()
	httpserver.WriteJSON(w, http.StatusOK, map[string]any{})
}

// playerSyncRetry 是横幅上的「重试」：当场补发没记到账号的集，答这一轮补上几集、还剩几集、为什么没补完。
// 一轮最多十来集、两集之间隔一秒多，通常几秒内答复；网页不等了就随请求一起停下，剩下的留到下次。
func (h *Handler) playerSyncRetry(w http.ResponseWriter, r *http.Request) {
	res := h.deps.Player.RetryPendingSyncs(r.Context())
	if r.Context().Err() != nil {
		return // 浏览器已经不等了
	}
	httpserver.WriteJSON(w, http.StatusOK, res)
}

func (h *Handler) playerPause(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Paused bool `json:"paused"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	if err := h.deps.Player.SetPause(req.Paused); err != nil {
		writeErr(w, err)
		return
	}
	httpserver.WriteJSON(w, http.StatusOK, map[string]any{})
}

func (h *Handler) playerSeek(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Position float64 `json:"position"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	if req.Position < 0 {
		httpserver.WriteError(w, http.StatusBadRequest, "position 不能为负")
		return
	}
	if err := h.deps.Player.Seek(req.Position); err != nil {
		writeErr(w, err)
		return
	}
	httpserver.WriteJSON(w, http.StatusOK, map[string]any{})
}

// ── 账号 ──（设置载荷在 system.go）

func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	h.lists.Invalidate()
	defer h.lists.Invalidate()
	user, err := h.deps.Auth.Login(r.Context(), req.Email, req.Password)
	if err != nil {
		writeErr(w, err)
		return
	}
	sess := h.deps.Auth.Session()
	if err := h.deps.Store.SetAnimegoSession(store.AnimegoSession{
		Email:         user.Email,
		AccessToken:   sess.AccessToken,
		RefreshCookie: sess.RefreshCookie,
	}); err != nil {
		log.Printf("api: 持久化 animego 会话失败：%v", err)
	}
	httpserver.WriteJSON(w, http.StatusOK, map[string]any{"user": map[string]string{"email": user.Email}})
}

func (h *Handler) logout(w http.ResponseWriter, _ *http.Request) {
	h.lists.Invalidate()
	h.deps.Auth.RestoreSession(animego.Session{})
	if err := h.deps.Store.SetAnimegoSession(store.AnimegoSession{}); err != nil {
		log.Printf("api: 清除 animego 会话失败：%v", err)
	}
	httpserver.WriteJSON(w, http.StatusOK, map[string]any{})
}
