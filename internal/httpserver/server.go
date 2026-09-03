// Package httpserver 实现 nagare 的 HTTP 服务与分层鉴权（决议 A4）。
//
// 中间件从外到内的顺序，一层不过直接短路：
//
//  1. Host 白名单 —— 只认 127.0.0.1:<port> / localhost:<port>，挡 DNS rebinding
//  2. token 校验 —— /api/* 必须携带有效 token（X-Nagare-Token 头，GET 也可用查询参数）
//  3. CSRF 守卫 —— 非 GET/HEAD/OPTIONS 必须把 token 放在 X-Nagare-Token 自定义头里
//
// 流端点（/stream/*）不走 token 而走能力 URL：路径里带随机段、每次启动轮换，
// 外部播放器（mpv）不需要设置请求头就能拉流。
// 整个服务不发任何 Access-Control-Allow-* 头 —— 默认拒绝一切跨源读取。
package httpserver

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"io/fs"
	"log"
	"net"
	"net/http"
	"sync"
	"time"
)

// TokenHeader 是携带鉴权 token 的自定义请求头。
// 表单和 <img> 之类的被动请求设不了自定义头，所以它同时兼任 CSRF 防线。
const TokenHeader = "X-Nagare-Token"

// readHeaderTimeout 防御慢速请求头攻击（slowloris）。
const readHeaderTimeout = 10 * time.Second

// Options 是构造 Server 的全部输入。
type Options struct {
	// Token 是 128 位鉴权 token 的十六进制表示，不能为空。
	Token string
	// Port 是实际监听端口，用于 Host 白名单的精确匹配。
	Port int
	// WebFS 是内嵌前端的文件系统（根下直接是 index.html）；nil 表示 API-only 模式。
	WebFS fs.FS
	// Version 通过 /api/health 暴露给前端。
	Version string
	// RegisterAPI 在 /api/* 的鉴权链【之内】注册业务端点（internal/api 挂载点）。
	// 注册的所有路由自动获得 Host 白名单 + token + CSRF 三层防护。
	RegisterAPI func(mux *http.ServeMux)
}

// Server 承载路由、鉴权中间件与流端点能力注册表。
type Server struct {
	opts    Options
	caps    *Capabilities
	handler http.Handler
	// httpSrv 在 New 里就建好（不是等到 Serve）：Shutdown 可能先于 Serve 的
	// goroutine 被调度到 —— 那时若还是 nil，关闭就成了空操作，进程会一直挂着。
	httpSrv *http.Server

	// stream 是流端点的实际处理器，启动后才注册（见 SetStreamHandler）。
	streamMu sync.RWMutex
	stream   http.Handler
}

// New 组装完整的中间件链与路由。Token 为空是编程错误，直接 panic。
func New(opts Options) *Server {
	if opts.Token == "" {
		panic("httpserver: token 不能为空")
	}
	s := &Server{opts: opts, caps: NewCapabilities()}

	api := http.NewServeMux()
	api.HandleFunc("GET /api/health", s.handleHealth)
	if opts.RegisterAPI != nil {
		opts.RegisterAPI(api)
	}

	root := http.NewServeMux()
	// /api/* 在 Host 白名单之内再叠 token 校验与 CSRF 守卫。
	root.Handle("/api/", s.requireToken(s.requireHeaderOnMutation(api)))
	// 流端点只认能力 URL（见 capability.go），mpv 不用带任何请求头。
	root.HandleFunc("GET /stream/{capability}/{path...}", s.handleStream)
	root.Handle("/", s.staticHandler())

	s.handler = securityHeaders(checkHost(opts.Port, root))
	s.httpSrv = &http.Server{
		Handler:           s.handler,
		ReadHeaderTimeout: readHeaderTimeout,
	}
	return s
}

// Handler 暴露完整处理链，测试直接打这里。
func (s *Server) Handler() http.Handler { return s.handler }

// Serve 在给定 listener 上阻塞服务，直到出错或被 Shutdown 关闭（返回 http.ErrServerClosed）。
func (s *Server) Serve(ln net.Listener) error { return s.httpSrv.Serve(ln) }

// Shutdown 优雅关闭：停止接受新连接并等在途请求完成（受 ctx 限时），
// 让 POST /api/shutdown 的响应有机会送达后再收尾。
// 早于 Serve 调用也安全：net/http 会记住关闭状态，之后的 Serve 立刻返回 ErrServerClosed。
func (s *Server) Shutdown(ctx context.Context) error { return s.httpSrv.Shutdown(ctx) }

// StreamCapability 返回当前流端点的能力段（拼流 URL 用）。
func (s *Server) StreamCapability() string { return s.caps.Stream() }

// handleHealth 是鉴权链路的活性探针：前端拿它验证 token 与服务状态。
func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"status":  "ok",
		"version": s.opts.Version,
	})
}

// tokenEqual 用常数时间比较，避免逐字节短路造成的计时侧信道。
func (s *Server) tokenEqual(candidate string) bool {
	return subtle.ConstantTimeCompare([]byte(candidate), []byte(s.opts.Token)) == 1
}

// envelope 是统一响应壳：所有 API 响应都长这样，前端只需要一种解析。
type envelope struct {
	Success bool   `json:"success"`
	Data    any    `json:"data"`
	Error   string `json:"error,omitempty"`
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	writeEnvelope(w, status, envelope{Success: true, Data: data})
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeEnvelope(w, status, envelope{Success: false, Error: msg})
}

// WriteJSON 供业务端点（internal/api）复用统一信封的成功响应。
func WriteJSON(w http.ResponseWriter, status int, data any) { writeJSON(w, status, data) }

// WriteError 供业务端点复用统一信封的错误响应（msg 中文、面向用户）。
func WriteError(w http.ResponseWriter, status int, msg string) { writeError(w, status, msg) }

func writeEnvelope(w http.ResponseWriter, status int, env envelope) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(env); err != nil {
		// 状态码已发出无法收回，只能记日志（多半是客户端断连）。
		log.Printf("httpserver: 写响应失败: %v", err)
	}
}
