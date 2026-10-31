// Package sources 是磁力、来源插件与磁力源规则的 HTTP 端点（/api/torrent/*、/api/downloads、
// /api/search*、/api/sources*、/api/source-plugin/*）。
//
// 这些端点从 internal/api 拆出来，只由桌面版的 main 注册：internal/api 本身因此不依赖
// torrentstream、sourceplugin、rules、rulesync 与 anacrolix/torrent，不带磁力的构建只要不
// import 本包，二进制里就一行磁力代码都没有（CI 用 scripts/ci/check-api-deps.sh 守着这条依赖图）。
//
// 本包挂进与 internal/api 同一条 /api/* 鉴权链（Host 白名单 + token + CSRF 头），
// 请求体解析与错误映射复用 api 的同一份实现，响应形状与拆分前逐字节一致。
package sources

import (
	"net/http"
	"strconv"

	"github.com/nagare-project/nagare/internal/api"
	"github.com/nagare-project/nagare/internal/httpserver"
	"github.com/nagare-project/nagare/internal/store"
)

// maxBodyBytes 与 internal/api 的请求体上限是同一个值。
const maxBodyBytes = api.MaxBodyBytes

// Deps 是本包端点的依赖注入点。
type Deps struct {
	Store *store.Store
	// Player 与 internal/api 共用同一个播放管线（磁力、在线候选与本地文件走同一条路）。
	Player       api.PlayerAPI
	Sources      *SourcesService
	SourcePlugin *SourcePluginService
	// Torrent 是磁力边下边播引擎；nil 表示引擎启动失败，磁力端点整体降级
	// （返回 503 并在设置页显示原因），其余功能不受影响。
	Torrent TorrentAPI
	// TorrentCacheDir 展示给用户：分片落在哪，清空缓存清的是哪个目录。
	TorrentCacheDir string
	// Downloads 是磁力下载（完整下到下载目录、下完进媒体库）；nil 时下载端点报 503。
	Downloads DownloadsAPI
	// DefaultDownloadDir 是设置里没填下载目录时用的目录。
	DefaultDownloadDir string
}

// Handler 汇集本包的端点。
type Handler struct {
	deps Deps
}

// New 构造 Handler。
func New(deps Deps) *Handler {
	return &Handler{deps: deps}
}

// Register 把端点注册进 /api/* 的鉴权链（与 api.Handler.Register 挂在同一个 mux 上）。
// 路由路径与拆分前一字不差：前端与既有测试都按这些路径调用。
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/search", h.search)
	mux.HandleFunc("GET /api/search/plugin", h.searchPlugin)
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
	mux.HandleFunc("GET /api/downloads", h.downloadsList)
	mux.HandleFunc("POST /api/downloads", h.downloadsAdd)
	mux.HandleFunc("DELETE /api/downloads/{id}", h.downloadsRemove)
}

// TorrentSettings 是 GET /api/settings 里的 torrent 一项（接到 api.Deps.TorrentSettings 上）。
// 与 POST /api/torrent/config 的响应是同一个视图。
func (h *Handler) TorrentSettings() map[string]any {
	return torrentView(h.deps.Store.TorrentConfig(), h.deps.Torrent, h.deps.TorrentCacheDir, h.deps.DefaultDownloadDir)
}

// decodeBody / writeErr 转给 internal/api 的同一份实现：请求体限长、错误措辞与状态码映射全站一致。
func decodeBody(w http.ResponseWriter, r *http.Request, out any) bool {
	return api.DecodeBody(w, r, out)
}

func writeErr(w http.ResponseWriter, err error) { api.WriteErr(w, err) }

// ── 磁力源：搜索与规则管理 ──

// search 聚合本机规则的结果；带 episode 时再向来源插件要 BT 候选（只要 torrent，
// 不会触发浏览器嗅探），让只装了插件、没配规则的用户也能在磁力选集里看到资源。
// title 是同一部作品的其他写法（原名、英文名）：本机规则与插件都按「q + 这些写法」各搜一次再合并。
func (h *Handler) search(w http.ResponseWriter, r *http.Request) {
	params := r.URL.Query()
	q := params.Get("q")
	titles := append([]string{q}, params["title"]...)
	view := h.deps.Sources.SearchTitles(r.Context(), titles)
	if episode, err := strconv.Atoi(params.Get("episode")); err == nil && episode > 0 && episode <= maxEpisodeNumber {
		anilist, _ := strconv.Atoi(params.Get("anilist"))
		year, _ := strconv.Atoi(params.Get("year"))
		// 与本端点其余可选参数一样宽松：不合法的连续编号当作没给
		absolute, _ := optionalEpisodeParam(params.Get("absolute"))
		h.deps.SourcePlugin.appendPluginTorrents(r.Context(), &view, pluginTorrentQuery{Titles: titles, Episode: episode, Absolute: absolute, AnilistID: anilist, Year: year})
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
