package api

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// MediaHandler 把媒体库里的文件按 fileId 通过 HTTP 提供出去（支持 Range）。
//
// ⚠️ 这个端点是给【浏览器内播放】用的（决议 A5 原本不做浏览器内播放，
// 用户 2026-09-03 明确要求接上）。正常播放路径仍然是 mpv：
// 它不解码、不转码，只是原样把字节喂给 <video>，所以能不能播完全取决于
// 浏览器认不认这个编码 —— HEVC/AV1 的 MKV 在多数浏览器上放不了，
// 界面必须如实说明并给出「用 mpv 播」的出路。
//
// 安全形状与封面端点一致：客户端给的是 fileId，【绝不】接受路径。
// 真实路径由 LibraryService 从已扫描的条目里查出来，所以：
//   - 不可能有目录穿越（我们从不拼接客户端字符串）
//   - 不在媒体库里的文件一律取不到，哪怕知道绝对路径
type MediaHandler struct {
	lib *LibraryService
}

// NewMediaHandler 构造媒体流处理器。
func NewMediaHandler(lib *LibraryService) *MediaHandler {
	return &MediaHandler{lib: lib}
}

// contentTypes 是容器扩展名到 MIME 的映射。
// 只列浏览器【可能】播得动的；其余交给 application/octet-stream，
// 让 <video> 直接报错而不是装作能播。
var contentTypes = map[string]string{
	".mp4":  "video/mp4",
	".m4v":  "video/mp4",
	".mov":  "video/quicktime",
	".webm": "video/webm",
	".mkv":  "video/x-matroska",
}

func (h *MediaHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	fileID := strings.TrimPrefix(r.URL.Path, "/")
	if fileID == "" {
		http.NotFound(w, r)
		return
	}
	item, ok := h.lib.Item(fileID)
	if !ok || item.AbsPath == "" {
		http.NotFound(w, r)
		return
	}

	f, err := os.Open(item.AbsPath)
	if err != nil {
		// 文件被移走/改名是常态（软 id 挂的是 name|size|mtime），
		// 不是服务器错误 —— 界面据此提示「重新扫描」。
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || st.IsDir() {
		http.NotFound(w, r)
		return
	}

	if ct, known := contentTypes[strings.ToLower(filepath.Ext(item.AbsPath))]; known {
		w.Header().Set("Content-Type", ct)
	}
	// 外部内容，禁止浏览器嗅探成别的类型。
	w.Header().Set("X-Content-Type-Options", "nosniff")
	// 本机文件，不该被任何中间层缓存。
	w.Header().Set("Cache-Control", "private, no-store")
	// ServeContent 负责 Range / 206 / If-Modified-Since —— 拖进度条靠它。
	http.ServeContent(w, r, filepath.Base(item.AbsPath), st.ModTime(), f)
}
