package api

import (
	"context"
	"log"
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

// IncludeDownloads 在一条下载完成后让它出现在媒体库里：下载目录已经是（或在）某个库目录里就重扫，
// 否则把它加成库目录（加的时候会扫一遍）。在下载器的后台 goroutine 里调。
func (s *LibraryService) IncludeDownloads(root string) {
	root = filepath.Clean(root)
	for _, folder := range s.st.Snapshot().Folders {
		if within(root, filepath.Clean(folder.Path)) {
			s.Rescan()
			return
		}
	}
	if _, _, err := s.AddFolder(root); err != nil {
		log.Printf("library: 把下载目录加进媒体库失败：%v", err)
		s.Rescan()
	}
}

// within 报告 path 是不是 base 本身或在 base 里面。
func within(path, base string) bool {
	rel, err := filepath.Rel(base, path)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}
