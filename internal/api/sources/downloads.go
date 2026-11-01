package sources

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/nagare-project/nagare/internal/httpserver"
	"github.com/nagare-project/nagare/internal/store"
	"github.com/nagare-project/nagare/internal/torrentstream"
)

// DownloadsAPI 是磁力下载（*torrentstream.Downloader 实现）：完整下到下载目录，下完进媒体库。
type DownloadsAPI interface {
	Add(ctx context.Context, req torrentstream.DownloadRequest) (torrentstream.DownloadView, error)
	List() []torrentstream.DownloadView
	Remove(id string) (bool, error)
}

// DownloadDir 是实际生效的下载目录：设置里填了就用它，否则用默认目录。
func DownloadDir(c store.TorrentConfig, fallback string) string {
	if dir := strings.TrimSpace(c.DownloadDir); dir != "" {
		return dir
	}
	return fallback
}

// DefaultDownloadDir 是没设置时的下载目录：用户的「下载」文件夹下的 nagare；取不到家目录就放在数据目录里。
func DefaultDownloadDir(dataDir string) string {
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return filepath.Join(home, "Downloads", "nagare")
	}
	return filepath.Join(dataDir, "downloads")
}

func (h *Handler) downloadsUnavailable(w http.ResponseWriter) bool {
	if h.deps.Downloads != nil {
		return false
	}
	httpserver.WriteError(w, http.StatusServiceUnavailable, "磁力下载未启用，请查看日志里的启动错误")
	return true
}

func (h *Handler) downloadsList(w http.ResponseWriter, _ *http.Request) {
	if h.downloadsUnavailable(w) {
		return
	}
	httpserver.WriteJSON(w, http.StatusOK, map[string]any{
		"downloads": h.deps.Downloads.List(),
		"dir":       DownloadDir(h.deps.Store.TorrentConfig(), h.deps.DefaultDownloadDir),
	})
}

func (h *Handler) downloadsAdd(w http.ResponseWriter, r *http.Request) {
	if h.downloadsUnavailable(w) {
		return
	}
	var req struct {
		Magnet     string `json:"magnet"`
		TorrentURL string `json:"torrentUrl"`
		Title      string `json:"title"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	view, err := h.deps.Downloads.Add(r.Context(), torrentstream.DownloadRequest{
		Magnet: req.Magnet, TorrentURL: req.TorrentURL, Title: req.Title,
	})
	if err != nil {
		writeErr(w, err)
		return
	}
	httpserver.WriteJSON(w, http.StatusOK, map[string]any{"download": view})
}

func (h *Handler) downloadsRemove(w http.ResponseWriter, r *http.Request) {
	if h.downloadsUnavailable(w) {
		return
	}
	ok, err := h.deps.Downloads.Remove(r.PathValue("id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	if !ok {
		httpserver.WriteError(w, http.StatusNotFound, "找不到这条下载")
		return
	}
	httpserver.WriteJSON(w, http.StatusOK, map[string]any{})
}
