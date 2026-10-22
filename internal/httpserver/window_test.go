package httpserver

import (
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nagare-project/nagare/internal/config"
)

// startWithWindow 在随机端口起一个真实的处理链，ShowWindow 记下被调了几次并返回 hasWindow。
// hasWindow 为假时不设 ShowWindow（与浏览器模式下 main 的接线一致：没有窗口就是 nil）。
func startWithWindow(t *testing.T, hasWindow bool) (port int, token string, calls *atomic.Int32) {
	t.Helper()
	calls = &atomic.Int32{}
	srv := httptest.NewUnstartedServer(http.NotFoundHandler())
	port = srv.Listener.Addr().(*net.TCPAddr).Port
	token = config.NewToken()
	opts := Options{Token: token, Port: port, Version: "test"}
	if hasWindow {
		opts.ShowWindow = func() bool { calls.Add(1); return true }
	}
	srv.Config.Handler = New(opts).Handler()
	srv.Start()
	t.Cleanup(srv.Close)
	return port, token, calls
}

// 第二次启动：已有实例有桌面窗口 → 它把窗口提到前面，新进程不必开浏览器。
func TestRequestShowWindowShowsExistingWindow(t *testing.T) {
	port, token, calls := startWithWindow(t, true)
	shown, err := RequestShowWindow(port, token)
	require.NoError(t, err)
	assert.True(t, shown)
	assert.Equal(t, int32(1), calls.Load())
}

// 已有实例没有桌面窗口（浏览器模式、没有窗口的平台）→ shown=false 且不算错误，新进程照旧开浏览器。
func TestRequestShowWindowWithoutWindowFallsBack(t *testing.T) {
	port, token, _ := startWithWindow(t, false)
	shown, err := RequestShowWindow(port, token)
	require.NoError(t, err)
	assert.False(t, shown)
}

// 不认识这个端点的旧版本（404）同样按「没有窗口」处理，不报错。
func TestRequestShowWindowOldVersion(t *testing.T) {
	port := startForeign(t, http.NotFoundHandler())
	shown, err := RequestShowWindow(port, config.NewToken())
	require.NoError(t, err)
	assert.False(t, shown)
}

// token 不对（另一份配置的 nagare）→ 401：报错、不动窗口，调用方退回开浏览器。
func TestRequestShowWindowWrongToken(t *testing.T) {
	port, _, calls := startWithWindow(t, true)
	shown, err := RequestShowWindow(port, config.NewToken())
	assert.Error(t, err)
	assert.False(t, shown)
	assert.Zero(t, calls.Load())
}

// 回的不是我们的信封、或是 success=false：报错，不当作「有窗口」。
func TestRequestShowWindowForeignResponse(t *testing.T) {
	for name, body := range map[string]string{
		"不是 JSON":       "hello",
		"success=false": `{"success":false,"data":{"shown":true}}`,
	} {
		t.Run(name, func(t *testing.T) {
			port := startForeign(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(body))
			}))
			shown, err := RequestShowWindow(port, config.NewToken())
			assert.Error(t, err)
			assert.False(t, shown)
		})
	}
}

// 没有实例在听：报错（连接被拒），不会挂住。
func TestRequestShowWindowNobodyListening(t *testing.T) {
	shown, err := RequestShowWindow(freePort(t), config.NewToken())
	assert.Error(t, err)
	assert.False(t, shown)
}

// 端点在 /api/* 的鉴权链之内（A4）：缺 token、token 只在查询参数里（跨站表单的形状）、Host 不在白名单、
// 用 GET 都碰不到窗口。
func TestShowWindowEndpointRequiresAuth(t *testing.T) {
	var calls atomic.Int32
	token := config.NewToken()
	s := New(Options{Token: token, Port: testPort, Version: "test", ShowWindow: func() bool { calls.Add(1); return true }})

	cases := []struct {
		name   string
		method string
		target string
		host   string
		header map[string]string
		want   int
	}{
		{"没带 token", http.MethodPost, ShowWindowPath, localHost, nil, http.StatusUnauthorized},
		{"token 只在查询参数里", http.MethodPost, ShowWindowPath + "?token=" + token, localHost, nil, http.StatusForbidden},
		{"Host 不在白名单", http.MethodPost, ShowWindowPath, "evil.com", map[string]string{TokenHeader: token}, http.StatusForbidden},
		{"token 不对", http.MethodPost, ShowWindowPath, localHost, map[string]string{TokenHeader: config.NewToken()}, http.StatusUnauthorized},
		{"GET 不是这个端点", http.MethodGet, ShowWindowPath, localHost, map[string]string{TokenHeader: token}, http.StatusMethodNotAllowed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := do(s, tc.method, tc.target, tc.host, tc.header)
			assert.Equal(t, tc.want, rec.Code)
		})
	}
	assert.Zero(t, calls.Load(), "鉴权没过的请求不能碰窗口")

	rec := do(s, http.MethodPost, ShowWindowPath, "localhost:8590", map[string]string{TokenHeader: token})
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, map[string]any{"shown": true}, decodeEnvelope(t, rec).Data)
	assert.Equal(t, int32(1), calls.Load())
}

// 端口上的程序回 307 指向别处：不跟过去（否则 token 头会被带到它指的任何主机），按出错处理、退回开浏览器。
func TestRequestShowWindowDoesNotFollowRedirects(t *testing.T) {
	var elsewhere atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { elsewhere.Add(1) }))
	t.Cleanup(target.Close)
	port := startForeign(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+r.URL.Path, http.StatusTemporaryRedirect)
	}))

	shown, err := RequestShowWindow(port, config.NewToken())
	require.Error(t, err)
	assert.False(t, shown)
	assert.Zero(t, elsewhere.Load(), "重定向不能把带 token 的请求带到别的主机")
}

// token 只走请求头，从不出现在地址里（地址会进日志、错误信息、代理）。
func TestRequestShowWindowSendsTokenOnlyInHeader(t *testing.T) {
	token := config.NewToken()
	var rawQuery, header string
	port := startForeign(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rawQuery, header = r.URL.RawQuery, r.Header.Get(TokenHeader)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"data":{"shown":true}}`))
	}))

	shown, err := RequestShowWindow(port, token)
	require.NoError(t, err)
	assert.True(t, shown)
	assert.Empty(t, rawQuery, "地址里不能带任何查询参数")
	assert.Equal(t, token, header)
}
