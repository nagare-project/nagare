package httpserver

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nagare-project/nagare/internal/config"
)

const (
	testPort  = 8590
	localHost = "127.0.0.1:8590"
)

// newTestServer 构造一个固定端口、随机 token 的测试服务。
func newTestServer(t *testing.T, webFS fs.FS) (*Server, string) {
	t.Helper()
	token := config.NewToken()
	s := New(Options{Token: token, Port: testPort, WebFS: webFS, Version: "test"})
	return s, token
}

// do 直接打完整中间件链（不开真实端口），Host 与请求头可控。
func do(s *Server, method, target, host string, header map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, nil)
	req.Host = host
	for k, v := range header {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	return rec
}

// decodeEnvelope 解析统一响应壳。
func decodeEnvelope(t *testing.T, rec *httptest.ResponseRecorder) envelope {
	t.Helper()
	var env envelope
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env), "响应不是合法的信封 JSON: %s", rec.Body.String())
	return env
}

// ---------- M0 验收：四个攻击面 ----------

// 验收 1：不带 token 请求 API → 401。
func TestAPIWithoutTokenIsUnauthorized(t *testing.T) {
	s, _ := newTestServer(t, nil)
	rec := do(s, http.MethodGet, "/api/health", localHost, nil)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.False(t, decodeEnvelope(t, rec).Success)
}

// 验收 2：Host: evil.com → 403（挡 DNS rebinding），token 正确也不行。
func TestForeignHostIsForbidden(t *testing.T) {
	s, token := newTestServer(t, nil)
	rec := do(s, http.MethodGet, "/api/health", "evil.com", map[string]string{TokenHeader: token})
	assert.Equal(t, http.StatusForbidden, rec.Code)
}

// 验收 3：变更请求缺 X-Nagare-Token 头 → 403，查询参数里的合法 token 救不了
// —— 跨站表单能提交查询参数，但设不了自定义头。
func TestMutationWithoutHeaderIsForbidden(t *testing.T) {
	s, token := newTestServer(t, nil)
	rec := do(s, http.MethodPost, "/api/health?token="+token, localHost, nil)
	assert.Equal(t, http.StatusForbidden, rec.Code)
}

// 验收 4：伪造的流端点能力 URL → 404。
func TestForgedStreamCapabilityIsNotFound(t *testing.T) {
	s, _ := newTestServer(t, nil)
	rec := do(s, http.MethodGet, "/stream/"+strings.Repeat("f", 32)+"/ep1.mkv", localHost, nil)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

// ---------- 正向路径 ----------

func TestHealthWithTokenHeader(t *testing.T) {
	s, token := newTestServer(t, nil)
	rec := do(s, http.MethodGet, "/api/health", localHost, map[string]string{TokenHeader: token})
	require.Equal(t, http.StatusOK, rec.Code)

	env := decodeEnvelope(t, rec)
	assert.True(t, env.Success)
	data, ok := env.Data.(map[string]any)
	require.True(t, ok, "data 应是对象")
	assert.Equal(t, "ok", data["status"])
	assert.Equal(t, "test", data["version"])
}

// GET 允许查询参数携带 token（方便地址栏调试），变更操作不允许（见验收 3）。
func TestHealthWithQueryTokenOnGET(t *testing.T) {
	s, token := newTestServer(t, nil)
	rec := do(s, http.MethodGet, "/api/health?token="+token, localHost, nil)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestLocalhostHostIsAllowed(t *testing.T) {
	s, token := newTestServer(t, nil)
	rec := do(s, http.MethodGet, "/api/health", "localhost:8590", map[string]string{TokenHeader: token})
	assert.Equal(t, http.StatusOK, rec.Code)
}

// 端口也参与白名单匹配：本机另一个端口上的页面同样是异源。
func TestWrongPortHostIsForbidden(t *testing.T) {
	s, token := newTestServer(t, nil)
	rec := do(s, http.MethodGet, "/api/health", "127.0.0.1:9999", map[string]string{TokenHeader: token})
	assert.Equal(t, http.StatusForbidden, rec.Code)
}

// Host 白名单必须覆盖【全部】路由，不只 /api/*：
// 静态页与流端点若漏出白名单，DNS rebinding 就能绕道进来。
func TestForeignHostBlockedOnAllRoutes(t *testing.T) {
	s, _ := newTestServer(t, nil)
	for _, target := range []string{"/", "/stream/" + s.StreamCapability() + "/ep1.mkv"} {
		rec := do(s, http.MethodGet, target, "evil.com", nil)
		assert.Equal(t, http.StatusForbidden, rec.Code, "路径 %s 未被 Host 白名单保护", target)
	}
}

func TestWrongTokenIsUnauthorized(t *testing.T) {
	s, _ := newTestServer(t, nil)
	rec := do(s, http.MethodGet, "/api/health", localHost, map[string]string{TokenHeader: strings.Repeat("0", 32)})
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

// 带上自定义头后应穿透全部鉴权层：405 来自路由（health 只注册了 GET），
// 说明请求已到达业务路由而不是被中间件拦下。
func TestMutationWithHeaderPassesGuards(t *testing.T) {
	s, token := newTestServer(t, nil)
	rec := do(s, http.MethodPost, "/api/health", localHost, map[string]string{TokenHeader: token})
	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}

// 决议 A4：不发任何 CORS 头。就算请求带了 Origin，响应里也不能出现
// Access-Control-Allow-*，否则等于把读取权限让渡给那个源。
// 对所有路由类型断言，防止未来某个新端点绕开中间件链。
func TestNoCORSHeadersEver(t *testing.T) {
	s, token := newTestServer(t, nil)
	for _, target := range []string{"/api/health", "/", "/stream/" + s.StreamCapability() + "/ep1.mkv"} {
		rec := do(s, http.MethodGet, target, localHost, map[string]string{
			TokenHeader: token,
			"Origin":    "https://evil.example",
		})
		for k := range rec.Header() {
			assert.NotContains(t, strings.ToLower(k), "access-control", "路径 %s 出现了 CORS 头: %s", target, k)
		}
	}
}

func TestSecurityHeadersPresent(t *testing.T) {
	s, _ := newTestServer(t, nil)
	rec := do(s, http.MethodGet, "/", localHost, nil)
	assert.Equal(t, "nosniff", rec.Header().Get("X-Content-Type-Options"))
	assert.Equal(t, "DENY", rec.Header().Get("X-Frame-Options"))
	assert.Equal(t, contentSecurityPolicy, rec.Header().Get("Content-Security-Policy"))
	assert.NotEmpty(t, rec.Header().Get("Permissions-Policy"))
}

// API 响应不落缓存：这一层之内的内容都视为敏感。
func TestAPIResponsesAreNoStore(t *testing.T) {
	s, token := newTestServer(t, nil)
	rec := do(s, http.MethodGet, "/api/health", localHost, map[string]string{TokenHeader: token})
	assert.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
}

// ---------- 流端点 ----------

func TestValidStreamCapability(t *testing.T) {
	s, _ := newTestServer(t, nil)
	rec := do(s, http.MethodGet, "/stream/"+s.StreamCapability()+"/ep1.mkv", localHost, nil)
	assert.Equal(t, http.StatusNoContent, rec.Code, "合法能力段应通过校验（占位实现回 204）")
}

func TestRotateInvalidatesOldCapability(t *testing.T) {
	s, _ := newTestServer(t, nil)
	old := s.StreamCapability()
	s.caps.Rotate()

	rec := do(s, http.MethodGet, "/stream/"+old+"/ep1.mkv", localHost, nil)
	assert.Equal(t, http.StatusNotFound, rec.Code, "轮换后旧能力 URL 必须立即失效")

	rec = do(s, http.MethodGet, "/stream/"+s.StreamCapability()+"/ep1.mkv", localHost, nil)
	assert.Equal(t, http.StatusNoContent, rec.Code)
}

// ---------- 静态资源 ----------

func TestAPIOnlyModeNotice(t *testing.T) {
	s, _ := newTestServer(t, nil)
	rec := do(s, http.MethodGet, "/", localHost, nil)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "API-only")
}

func TestStaticServesIndexAndAssets(t *testing.T) {
	web := fstest.MapFS{
		"index.html":    {Data: []byte("<html>nagare-shell</html>")},
		"assets/app.js": {Data: []byte("console.log('ok')")},
	}
	s, _ := newTestServer(t, web)

	rec := do(s, http.MethodGet, "/", localHost, nil)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "nagare-shell")

	rec = do(s, http.MethodGet, "/assets/app.js", localHost, nil)
	assert.Equal(t, http.StatusOK, rec.Code)

	// SPA 路由回落到 index.html，交给前端路由渲染。
	rec = do(s, http.MethodGet, "/library", localHost, nil)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "nagare-shell")

	// 路由段里带点号（如半集编号 12.5）不是资源请求，同样要回落 SPA。
	rec = do(s, http.MethodGet, "/library/ep/12.5", localHost, nil)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "nagare-shell")

	// 缺失的静态资源（assets/ 前缀）如实 404，不被 index.html 掩盖。
	rec = do(s, http.MethodGet, "/assets/missing.js", localHost, nil)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

// ---------- 其他 ----------

func TestSubWebFS(t *testing.T) {
	// 只有 .gitkeep（未构建前端）时应进入 API-only 模式。
	empty := fstest.MapFS{"web/.gitkeep": {}}
	sub, apiOnly := SubWebFS(empty)
	assert.True(t, apiOnly)
	assert.Nil(t, sub)

	built := fstest.MapFS{"web/index.html": {Data: []byte("x")}}
	sub, apiOnly = SubWebFS(built)
	assert.False(t, apiOnly)
	require.NotNil(t, sub)
}

func TestNewPanicsOnEmptyToken(t *testing.T) {
	assert.Panics(t, func() { New(Options{Port: testPort}) }, "空 token 是编程错误，必须在启动即暴露")
}

// Serve 的真实 TCP 冒烟：确认 http.Server 的组装（handler、超时）没接错线，
// Host 校验在真实 socket 上（而不只是手搓的 httptest 请求）同样生效。
func TestServeOverRealTCP(t *testing.T) {
	ln, port, err := Listen(48590)
	require.NoError(t, err)

	token := config.NewToken()
	s := New(Options{Token: token, Port: port, Version: "tcp-test"})
	done := make(chan struct{})
	go func() {
		// listener 被关闭时 Serve 返回错误，属预期收尾。
		_ = s.Serve(ln)
		close(done)
	}()
	t.Cleanup(func() {
		_ = ln.Close()
		<-done
	})

	base := fmt.Sprintf("http://127.0.0.1:%d", port)

	resp, err := http.Get(base + "/api/health")
	require.NoError(t, err)
	_ = resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode, "无 token 应 401")

	req, err := http.NewRequest(http.MethodGet, base+"/api/health", nil)
	require.NoError(t, err)
	req.Header.Set(TokenHeader, token)
	resp, err = http.DefaultClient.Do(req)
	require.NoError(t, err)
	_ = resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode, "带 token 应 200")
}

func TestListenFallsBackToNextPort(t *testing.T) {
	ln1, port1, err := Listen(38590)
	require.NoError(t, err)
	defer func() { _ = ln1.Close() }()

	ln2, port2, err := Listen(38590)
	require.NoError(t, err)
	defer func() { _ = ln2.Close() }()

	assert.Greater(t, port2, port1, "首选端口被占时应向上顺延")
	assert.LessOrEqual(t, port2, port1+maxPortProbes)
}
