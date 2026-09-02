package httpserver

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nagare-project/nagare/internal/config"
)

// startNagare 在随机端口起一个真实的 nagare 处理链（Host 白名单按实际端口配置）。
func startNagare(t *testing.T) (port int, token string) {
	t.Helper()
	srv := httptest.NewUnstartedServer(http.NotFoundHandler())
	port = srv.Listener.Addr().(*net.TCPAddr).Port
	token = config.NewToken()
	srv.Config.Handler = New(Options{Token: token, Port: port, Version: "test"}).Handler()
	srv.Start()
	t.Cleanup(srv.Close)
	return port, token
}

// startForeign 在随机端口起一个任意 HTTP 服务（模拟被别的程序占着的端口）。
func startForeign(t *testing.T, h http.Handler) int {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv.Listener.Addr().(*net.TCPAddr).Port
}

// freePort 找一个此刻没人监听的端口。
func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := ln.Addr().(*net.TCPAddr).Port
	require.NoError(t, ln.Close())
	return port
}

// 同 token 的 nagare 在跑 → 命中。
func TestProbe_RunningInstance(t *testing.T) {
	port, token := startNagare(t)
	assert.Equal(t, ProbeRunning, Probe(port, token))
	assert.True(t, ProbeExisting(port, token))
}

// 端口上是 nagare 但 token 不同（另一份配置）→ 401 → 视为外来。
func TestProbe_WrongToken(t *testing.T) {
	port, _ := startNagare(t)
	assert.Equal(t, ProbeForeign, Probe(port, config.NewToken()))
	assert.False(t, ProbeExisting(port, config.NewToken()))
}

// 端口被非 nagare 服务占着：各种「像但不是」的响应都不能算命中。
func TestProbe_ForeignService(t *testing.T) {
	cases := []struct {
		name string
		h    http.HandlerFunc
	}{
		{"纯文本 200", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("hello")) }},
		{"信封形状但 status 不对", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"success":true,"data":{"status":"degraded"}}`))
		}},
		{"success=false", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"success":false,"data":{"status":"ok"}}`))
		}},
		{"非 200", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusServiceUnavailable) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			port := startForeign(t, tc.h)
			assert.Equal(t, ProbeForeign, Probe(port, config.NewToken()))
			assert.False(t, ProbeExisting(port, config.NewToken()))
		})
	}
}

// 没人监听 → None，且不会尝试发送 token。
func TestProbe_NothingListening(t *testing.T) {
	port := freePort(t)
	assert.Equal(t, ProbeNone, Probe(port, config.NewToken()))
	assert.False(t, ProbeExisting(port, config.NewToken()))
}

// 端口有人但不响应（挂起）→ 1 秒内放弃，视为外来。
func TestProbe_HangingServiceTimesOut(t *testing.T) {
	block := make(chan struct{})
	t.Cleanup(func() { close(block) })
	port := startForeign(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-block:
		case <-r.Context().Done():
		}
	}))

	start := time.Now()
	assert.Equal(t, ProbeForeign, Probe(port, config.NewToken()))
	assert.Less(t, time.Since(start), 3*time.Second)
}

// H1 回归（评审 2026-09-02）：Shutdown 抢在 Serve 之前发生时，Serve 必须立刻
// 以 ErrServerClosed 返回。早期实现把 *http.Server 建在 Serve 里，这种时序下
// Shutdown 变成空操作 → 监听不关、Serve 永不返回 → 整个进程挂死。
func TestShutdownBeforeServe(t *testing.T) {
	s, _ := newTestServer(t, nil)
	require.NoError(t, s.Shutdown(context.Background()))

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() { done <- s.Serve(ln) }()
	select {
	case err := <-done:
		assert.ErrorIs(t, err, http.ErrServerClosed)
	case <-time.After(2 * time.Second):
		t.Fatal("Shutdown 先于 Serve 时进程会挂死：Serve 没有返回")
	}
}

// Shutdown 让正在服务的 Serve 以 ErrServerClosed 返回。
func TestShutdownStopsServe(t *testing.T) {
	s, _ := newTestServer(t, nil)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() { done <- s.Serve(ln) }()

	// 等到端口可连再关，确保 Serve 已经在跑。
	require.Eventually(t, func() bool {
		c, err := net.Dial("tcp", ln.Addr().String())
		if err != nil {
			return false
		}
		_ = c.Close()
		return true
	}, 2*time.Second, 10*time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	require.NoError(t, s.Shutdown(ctx))
	select {
	case err := <-done:
		assert.ErrorIs(t, err, http.ErrServerClosed)
	case <-time.After(2 * time.Second):
		t.Fatal("Shutdown 后 Serve 没有返回")
	}
}

// ── 实例标记：决定「能不能把 token 发出去」的那道门 ──

// 标记文件的生命周期：没起过 → 无；MarkInstance → 有且 0600；ClearInstance → 无。
func TestInstanceMarkerLifecycle(t *testing.T) {
	dir := t.TempDir()
	assert.False(t, HasInstance(dir), "没起过实例时不应有标记")

	require.NoError(t, MarkInstance(dir))
	assert.True(t, HasInstance(dir))

	data, err := os.ReadFile(InstancePath(dir))
	require.NoError(t, err)
	pid, err := strconv.Atoi(string(data))
	require.NoError(t, err)
	assert.Equal(t, os.Getpid(), pid, "标记里应是本进程 PID")

	if runtime.GOOS != "windows" {
		st, err := os.Stat(InstancePath(dir))
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o600), st.Mode().Perm())
	}

	ClearInstance(dir)
	assert.False(t, HasInstance(dir), "干净退出后标记应被清掉")
	ClearInstance(dir) // 重复清理是空操作，不得 panic
}

// 目录不可写时 MarkInstance 报错而不是静默放过。
func TestMarkInstanceFailure(t *testing.T) {
	require.Error(t, MarkInstance(filepath.Join(t.TempDir(), "不存在的子目录")))
}

// PortOccupied 只裸连、不发任何凭据：有人 true，没人 false。
func TestPortOccupied(t *testing.T) {
	var gotAuth bool
	port := startForeign(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(TokenHeader) != "" {
			gotAuth = true
		}
	}))
	assert.True(t, PortOccupied(port))
	assert.False(t, gotAuth, "裸连探测绝不能发送 token")
	assert.False(t, PortOccupied(freePort(t)))
}

// 安全回归（评审 2026-09-02 的 CRITICAL）：冒充者照抄公开的 health 信封也骗不到 token。
// 主进程的策略是「先看标记文件，没有就绝不做带 token 的探测」——
// 这里断言那条策略下 token 一次都没发出去。
func TestSquatterNeverReceivesToken(t *testing.T) {
	var sawToken bool
	port := startForeign(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(TokenHeader) != "" {
			sawToken = true
		}
		// 冒充：无需知道 token 就能抄出这个公开形状。
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"data":{"status":"ok"}}`))
	}))
	dir := t.TempDir() // 没有标记文件 = 本机没有我方实例在跑

	// 主进程在这种情形下只允许裸连，不允许 Probe。
	require.False(t, HasInstance(dir))
	assert.True(t, PortOccupied(port))
	assert.False(t, sawToken, "没有实例标记时，token 绝不能发给端口占用者")

	// 反证：一旦真的调了 Probe，冒充者确实会被判成 Running 并拿到 token
	// —— 所以那道门必须留在调用方（main.go 的 ensureSingleInstance）。
	assert.Equal(t, ProbeRunning, Probe(port, "secret-token"))
	assert.True(t, sawToken)
}
