package api

import (
	"log"
	"net/http"
	"runtime"
	"time"

	"github.com/nagare-project/nagare/internal/httpserver"
	"github.com/nagare-project/nagare/internal/mpv"
)

// shutdownDelay 是 /api/shutdown 写完响应到真正开始退出的间隔：让响应先送达浏览器。
const shutdownDelay = 100 * time.Millisecond

// ── 系统：设置载荷、mpv 重探测、退出 ──

// settings 是设置页的一次性载荷（M4 阶段 A 契约）：
// version / platform / arch / dataDir / logPath / mpv / animego。
func (h *Handler) settings(w http.ResponseWriter, _ *http.Request) {
	httpserver.WriteJSON(w, http.StatusOK, map[string]any{
		"version":  h.deps.Version,
		"platform": runtime.GOOS,
		"arch":     runtime.GOARCH,
		"dataDir":  h.deps.DataDir,
		"logPath":  h.deps.LogPath,
		"mpv":      mpvView(h.deps.MPV),
		"torrent":  torrentView(h.deps.Store.TorrentConfig(), h.deps.Torrent, h.deps.TorrentCacheDir),
		"animego": map[string]any{
			"loggedIn": h.deps.Auth.LoggedIn(),
			"email":    h.deps.Store.AnimegoSession().Email,
			"baseUrl":  h.deps.AnimegoBaseURL,
		},
	})
}

// mpvView 把共享探测状态压成设置页用的视图；install 只在缺失时出现。
func mpvView(rt *mpv.Runtime) map[string]any {
	var (
		info mpv.Info
		err  error
	)
	if rt != nil {
		info, err = rt.Get()
	}
	found := err == nil && info.Path != ""
	view := map[string]any{"found": found}
	if found {
		view["path"] = info.Path
		view["version"] = info.Version
		view["source"] = info.Source
		return view
	}
	hint := "mpv 探测未初始化"
	if err != nil {
		hint = err.Error()
	}
	view["hint"] = hint
	view["install"] = mpv.InstallGuide()
	return view
}

// mpvDetect 重新探测 mpv（用户按指引装好后点「重新检测」），更新共享状态并返回新视图。
// 「没找到」是一种状态而非请求失败，照样 200 + found=false。
// 不接受自定义路径：显式路径要先有持久化的设置项，否则只活一次进程，是半成品。
func (h *Handler) mpvDetect(w http.ResponseWriter, _ *http.Request) {
	if h.deps.MPV == nil {
		httpserver.WriteError(w, http.StatusServiceUnavailable, "mpv 探测未初始化")
		return
	}
	if info, err := h.deps.MPV.Redetect(""); err != nil {
		log.Printf("api: 重新探测 mpv 未找到：%v", err)
	} else {
		log.Printf("api: 重新探测到 mpv %s（%s，来源：%s）", info.Version, info.Path, info.Source)
	}
	httpserver.WriteJSON(w, http.StatusOK, mpvView(h.deps.MPV))
}

// shutdown 让界面里的「退出」按钮（Linux 无托盘时的唯一出口）能收掉整个进程：
// 先写成功信封，shutdownDelay 之后再触发退出，保证响应送达。
func (h *Handler) shutdown(w http.ResponseWriter, _ *http.Request) {
	if h.deps.Shutdown == nil {
		httpserver.WriteError(w, http.StatusNotImplemented, "此实例不支持从界面退出")
		return
	}
	httpserver.WriteJSON(w, http.StatusOK, map[string]any{})
	log.Print("api: 收到界面退出请求")
	time.AfterFunc(shutdownDelay, h.deps.Shutdown)
}
