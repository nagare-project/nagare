package httpserver

import (
	"crypto/subtle"
	"net/http"
	"sync"

	"github.com/nagare-project/nagare/internal/random"
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

// SetStreamHandler 注册流端点的实际处理器（M3 由磁力引擎提供）。传 nil 即注销。
//
// 能力段校验通过后请求会被改写路径再转发：处理器活在自己的路径空间里
// （例如 /t/{infohash}/{index}），既不必知道能力段的存在，也就不可能把它
// 写进日志或错误信息 —— 能力段等同凭证，少一处流经就少一处泄露面。
func (s *Server) SetStreamHandler(h http.Handler) {
	s.streamMu.Lock()
	defer s.streamMu.Unlock()
	s.stream = h
}

// streamHandler 取当前注册的处理器；未注册返回 nil。
func (s *Server) streamHandler() http.Handler {
	s.streamMu.RLock()
	defer s.streamMu.RUnlock()
	return s.stream
}

// handleStream 校验能力段并把请求转给注册的流处理器。
// 能力段错误、或压根没有处理器，一律 404 —— 对探测者来说
// 「端点不存在」比「端点存在但你没权限」泄露得更少。
func (s *Server) handleStream(w http.ResponseWriter, r *http.Request) {
	if !s.caps.streamValid(r.PathValue("capability")) {
		writeError(w, http.StatusNotFound, "未找到")
		return
	}
	h := s.streamHandler()
	if h == nil {
		writeError(w, http.StatusNotFound, "未找到")
		return
	}
	// Clone 已深拷贝 URL，改 Path 不会影响原请求（中间件与日志仍看到原始路径）。
	fwd := r.Clone(r.Context())
	fwd.URL.Path = "/" + r.PathValue("path")
	fwd.URL.RawPath = ""
	h.ServeHTTP(w, fwd)
}
