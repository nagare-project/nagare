package httpserver

import (
	"fmt"
	"net/http"
	"strings"
)

// checkHost 校验 Host 头白名单，挡 DNS rebinding：
// 恶意域名把解析指到 127.0.0.1 时，浏览器发出的 Host 是那个域名，这里直接 403。
// 只认精确的 127.0.0.1:<port> 与 localhost:<port>（服务只绑 127.0.0.1，不涉及 [::1]）。
//
// ⚠️ 这道防线对将来的 WebSocket 端点【不够用】：WS 握手不受同源策略/CORS 约束，
// 恶意页面直连 ws://127.0.0.1:<port> 时 Host 是合法的。任何 WS/upgrade 端点
// 必须额外校验 Origin 头（同样只认 127.0.0.1/localhost），并配攻击面测试。
func checkHost(port int, next http.Handler) http.Handler {
	allowed := map[string]struct{}{
		fmt.Sprintf("127.0.0.1:%d", port): {},
		fmt.Sprintf("localhost:%d", port): {},
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := allowed[strings.ToLower(r.Host)]; !ok {
			writeError(w, http.StatusForbidden, "Host 不在白名单内")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// contentSecurityPolicy 收紧同源执行边界。token 放在 sessionStorage 里，
// 一旦前端将来渲染外部字符串（种子名/弹幕）时出了 XSS，脚本就能拿走 token
// —— CSP 是那种场景下的最后一道闸，必须趁界面还小的时候立好。
// style 需要 'unsafe-inline'：React 内联 style 属性依赖它；脚本不放行内联。
const contentSecurityPolicy = "default-src 'self'; script-src 'self'; " +
	"style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; " +
	"media-src 'self'; object-src 'none'; base-uri 'self'; form-action 'self'; " +
	"frame-ancestors 'none'"

// securityHeaders 给所有响应补上基础安全头。
// 注意这里刻意【不】设置任何 Access-Control-Allow-* —— 不发 CORS 头
// 就是拒绝一切跨源读取，这是决议 A4 的默认拒绝姿态。
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Content-Security-Policy", contentSecurityPolicy)
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		next.ServeHTTP(w, r)
	})
}

// requireToken 是 /api/* 的 token 校验：自定义头优先，也接受查询参数
// （方便浏览器地址栏调试 GET）；两个来源都走常数时间比较。
//
// 保留查询参数通道是权衡过的：攻击者不知道 token，此通道给不了他任何东西；
// 代价是手动调试的 URL 会留在浏览器历史里。约束：本服务【不得】记录请求 URL，
// 将来加任何访问日志/错误上报都必须先脱敏 query 里的 token。
func (s *Server) requireToken(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// API 响应一律不落缓存：这一层之内的内容都视为敏感。
		w.Header().Set("Cache-Control", "no-store")
		if !s.tokenEqual(tokenFromRequest(r)) {
			writeError(w, http.StatusUnauthorized, "缺少或错误的鉴权 token")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// requireHeaderOnMutation 是 CSRF 守卫：变更操作必须把 token 放在自定义头里。
// 查询参数对非 GET 无效 —— 跨站表单能提交查询参数，但设不了自定义头。
func (s *Server) requireHeaderOnMutation(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			next.ServeHTTP(w, r)
			return
		}
		if !s.tokenEqual(r.Header.Get(TokenHeader)) {
			writeError(w, http.StatusForbidden, "变更操作必须携带 "+TokenHeader+" 请求头")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// tokenFromRequest 提取候选 token：自定义头优先，其次查询参数。
func tokenFromRequest(r *http.Request) string {
	if t := r.Header.Get(TokenHeader); t != "" {
		return t
	}
	return r.URL.Query().Get("token")
}
