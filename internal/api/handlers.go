// Package api 实现 /api/* 业务端点，经 httpserver.Options.RegisterAPI 挂进
// 鉴权链之内 —— 这里的每个路由天然带 Host 白名单 + token + CSRF 三层防护。
package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strconv"

	"github.com/nagare-project/nagare/internal/animego"
	errs "github.com/nagare-project/nagare/internal/errors"
	"github.com/nagare-project/nagare/internal/httpserver"
	"github.com/nagare-project/nagare/internal/mpv"
	"github.com/nagare-project/nagare/internal/player"
	"github.com/nagare-project/nagare/internal/store"
)

// maxBodyBytes 是请求体上限（本地 API，1MB 足够）。
const maxBodyBytes = 1 << 20

// PlayerAPI 是处理器需要的播放能力子集。
type PlayerAPI interface {
	Play(ctx context.Context, src player.MediaSource, subPath string) (player.PlayResult, error)
	Stop()
	SetPause(v bool) error
	Seek(seconds float64) error
	Status() player.Status
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
	MPV            *mpv.Runtime // 共享探测状态：设置页读、/api/mpv/detect 刷新、播放取路径
	Version        string
	Sources        *SourcesService
	SourcePlugin   *SourcePluginService
	// Torrent 是磁力边下边播引擎；nil 表示引擎启动失败，磁力端点整体降级
	// （返回 503 并在设置页显示原因），其余功能不受影响。
	Torrent TorrentAPI
	// TorrentCacheDir 展示给用户：分片落在哪，清空缓存清的是哪个目录。
	TorrentCacheDir string
	// Shutdown 触发整个进程退出（主进程的根 cancel）；nil 表示不支持从界面退出。
	Shutdown func()
	// DataDir / LogPath 展示给用户：数据在哪、出问题看哪个文件。
	DataDir string
	LogPath string
}

// Handler 汇集全部业务端点。
type Handler struct {
	deps      Deps
	catalog   *DiscoverService
	lists     *ListsService
	remoteArt *RemoteArt
}

// New 构造 Handler。
func New(deps Deps) *Handler {
	art := deps.RemoteArt
	if art == nil {
		art = NewRemoteArt(deps.AnimegoBaseURL, 4096)
	}
	catalog := NewDiscoverService(deps.Catalog, art)
	return &Handler{deps: deps, catalog: catalog, lists: NewListsService(deps.Lists, catalog), remoteArt: art}
}

// Register 把业务路由注册进 /api/* 的鉴权链（httpserver.Options.RegisterAPI 的挂载点）。
func (h *Handler) Register(mux *http.ServeMux) {
	h.registerCatalog(mux)
	mux.HandleFunc("GET /api/library", h.getLibrary)
	mux.HandleFunc("POST /api/library/folders", h.addFolder)
	mux.HandleFunc("DELETE /api/library/folders/{id}", h.removeFolder)
	mux.HandleFunc("POST /api/library/rescan", h.rescan)
	mux.HandleFunc("POST /api/play", h.play)
	mux.HandleFunc("GET /api/player/status", h.playerStatus)
	mux.HandleFunc("POST /api/player/stop", h.playerStop)
	mux.HandleFunc("POST /api/player/pause", h.playerPause)
	mux.HandleFunc("POST /api/player/seek", h.playerSeek)
	mux.HandleFunc("GET /api/settings", h.settings)
	mux.HandleFunc("POST /api/mpv/detect", h.mpvDetect)
	mux.HandleFunc("POST /api/shutdown", h.shutdown)
	mux.HandleFunc("POST /api/animego/login", h.login)
	mux.HandleFunc("POST /api/animego/logout", h.logout)
	mux.HandleFunc("GET /api/search", h.search)
	mux.HandleFunc("GET /api/sources", h.sources)
	mux.HandleFunc("POST /api/sources/reload", h.sourcesReload)
	mux.HandleFunc("POST /api/sources/sync", h.sourcesSync)
	mux.HandleFunc("POST /api/sources/config", h.sourcesConfig)
	mux.HandleFunc("POST /api/sources/{id}/enabled", h.sourceEnabled)
	mux.HandleFunc("POST /api/sources/{id}/selfcheck", h.sourceSelfCheck)
	mux.HandleFunc("GET /api/source-plugin", h.sourcePluginView)
	mux.HandleFunc("POST /api/source-plugin/config", h.sourcePluginConfig)
	mux.HandleFunc("POST /api/source-plugin/candidates", h.sourcePluginCandidates)
	mux.HandleFunc("POST /api/source-plugin/play", h.sourcePluginPlay)
	mux.HandleFunc("POST /api/torrent/play", h.torrentPlay)
	mux.HandleFunc("GET /api/torrent/status", h.torrentStatus)
	mux.HandleFunc("POST /api/torrent/stop", h.torrentStop)
	mux.HandleFunc("POST /api/torrent/cache/clear", h.torrentCacheClear)
	mux.HandleFunc("POST /api/torrent/config", h.torrentConfig)
}

// decodeBody 解析 JSON 请求体（限长）。失败返回 false 且已写响应。
func decodeBody(w http.ResponseWriter, r *http.Request, out any) bool {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBodyBytes))
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

// ── 磁力源：搜索与规则管理 ──

// search 聚合本机规则的结果；带 episode 时再向来源插件要 BT 候选（只要 torrent，
// 不会触发浏览器嗅探），让只装了插件、没配规则的用户也能在磁力选集里看到资源。
func (h *Handler) search(w http.ResponseWriter, r *http.Request) {
	params := r.URL.Query()
	q := params.Get("q")
	view := h.deps.Sources.Search(r.Context(), q)
	if episode, err := strconv.Atoi(params.Get("episode")); err == nil && episode > 0 {
		anilist, _ := strconv.Atoi(params.Get("anilist"))
		year, _ := strconv.Atoi(params.Get("year"))
		titles := append([]string{q}, params["title"]...)
		h.deps.SourcePlugin.appendPluginTorrents(r.Context(), &view, pluginTorrentQuery{Titles: titles, Episode: episode, AnilistID: anilist, Year: year})
	}
	httpserver.WriteJSON(w, http.StatusOK, view)
}

func (h *Handler) sources(w http.ResponseWriter, _ *http.Request) {
	httpserver.WriteJSON(w, http.StatusOK, h.deps.Sources.View())
}

func (h *Handler) sourcesReload(w http.ResponseWriter, _ *http.Request) {
	n, errsList := h.deps.Sources.Load()
	if errsList == nil {
		errsList = []string{}
	}
	httpserver.WriteJSON(w, http.StatusOK, map[string]any{"loaded": n, "errors": errsList})
}

func (h *Handler) sourcesSync(w http.ResponseWriter, r *http.Request) {
	rep, err := h.deps.Sources.Sync(r.Context())
	if err != nil {
		writeErr(w, err)
		return
	}
	httpserver.WriteJSON(w, http.StatusOK, rep)
}

func (h *Handler) sourcesConfig(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RemoteURL *string `json:"remoteUrl"`
		LocalDir  *string `json:"localDir"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	view, err := h.deps.Sources.SetConfig(req.RemoteURL, req.LocalDir)
	if err != nil {
		writeErr(w, err)
		return
	}
	httpserver.WriteJSON(w, http.StatusOK, view)
}

func (h *Handler) sourceEnabled(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	if err := h.deps.Sources.SetEnabled(r.PathValue("id"), req.Enabled); err != nil {
		writeErr(w, err)
		return
	}
	httpserver.WriteJSON(w, http.StatusOK, map[string]any{})
}

func (h *Handler) sourceSelfCheck(w http.ResponseWriter, r *http.Request) {
	out, err := h.deps.Sources.SelfCheck(r.Context(), r.PathValue("id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	httpserver.WriteJSON(w, http.StatusOK, out)
}
