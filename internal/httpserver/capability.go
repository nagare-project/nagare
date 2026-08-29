package httpserver

import (
	"crypto/subtle"
	"net/http"
	"sync"

	"github.com/nagare-player/nagare/internal/random"
)

// capabilityBytes 是能力段的随机字节数（128 位，与主 token 同强度）。
const capabilityBytes = 16

// Capabilities 管理流端点的能力 URL（决议 A4）。
//
// 能力 = 一段随机路径前缀，知道完整 URL 即有权访问该流。它存在的意义：
// mpv 之类的外部播放器只认 URL、设不了自定义请求头（不然得靠
// --http-header-fields 这种脆弱姿势）。每次进程启动重新生成，
// Rotate 留给后续里程碑做会话级轮换。
type Capabilities struct {
	mu     sync.RWMutex
	stream string
}

// NewCapabilities 生成初始能力集。
func NewCapabilities() *Capabilities {
	return &Capabilities{stream: random.Hex(capabilityBytes)}
}

// Stream 返回当前流能力段。
func (c *Capabilities) Stream() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.stream
}

// Rotate 更换流能力段，旧 URL 立即失效。
func (c *Capabilities) Rotate() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.stream = random.Hex(capabilityBytes)
}

// streamValid 用常数时间比较校验能力段。
func (c *Capabilities) streamValid(candidate string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return subtle.ConstantTimeCompare([]byte(candidate), []byte(c.stream)) == 1
}

// handleStream 是流端点占位：能力段错误一律 404 —— 对探测者来说
// "端点不存在"比"端点存在但你没权限"泄露得更少。
// 真正的媒体流在 M1（本地文件）/ M3（磁力）接入。
func (s *Server) handleStream(w http.ResponseWriter, r *http.Request) {
	if !s.caps.streamValid(r.PathValue("capability")) {
		writeError(w, http.StatusNotFound, "未找到")
		return
	}
	// 占位：能力校验通过，但还没有可服务的媒体。
	w.WriteHeader(http.StatusNoContent)
}
