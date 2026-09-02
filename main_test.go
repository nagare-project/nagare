package main

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/nagare-project/nagare/internal/config"
	"github.com/nagare-project/nagare/internal/httpserver"
)

// stubInstanceProbes 替换单实例判定的三个注入点，返回值在 t.Cleanup 里还原。
func stubInstanceProbes(t *testing.T, marker bool, occupied bool, result httpserver.ProbeResult) *[]string {
	t.Helper()
	oldHas, oldOcc, oldProbe, oldOpen := hasInstance, portOccupied, probePort, openBrowserFn
	t.Cleanup(func() {
		hasInstance, portOccupied, probePort, openBrowserFn = oldHas, oldOcc, oldProbe, oldOpen
	})
	opened := &[]string{}
	hasInstance = func(string) bool { return marker }
	portOccupied = func(int) bool { return occupied }
	probePort = func(int, string) httpserver.ProbeResult { return result }
	openBrowserFn = func(url string) error { *opened = append(*opened, url); return nil }
	return opened
}

// newTestConfig 造一份落在临时目录里的配置（rotateToken 会真的写盘）。
func newTestConfig(t *testing.T) *config.Config {
	t.Helper()
	dir := t.TempDir()
	t.Setenv(config.EnvConfigDir, dir)
	cfg, err := config.LoadOrInit()
	require.NoError(t, err)
	return cfg
}

func TestEnsureSingleInstance(t *testing.T) {
	tests := []struct {
		name string
		// 输入：实例标记是否存在、端口是否被占、探测结果
		marker   bool
		occupied bool
		probe    httpserver.ProbeResult
		// 期望
		wantAlreadyRunning bool
		wantTokenRotated   bool
		wantBrowserOpened  bool
	}{
		{
			name: "无标记且端口空闲：正常启动，不动 token",
		},
		{
			name:             "无标记但端口被占：不是我方实例，轮换 token 后继续启动",
			occupied:         true,
			wantTokenRotated: true,
		},
		{
			name:               "有标记且探测到我方实例：把浏览器指过去并退出",
			marker:             true,
			probe:              httpserver.ProbeRunning,
			wantAlreadyRunning: true,
			wantBrowserOpened:  true,
		},
		{
			name:             "有标记但端口上是外来程序：token 已发出去，视为泄露立即轮换",
			marker:           true,
			probe:            httpserver.ProbeForeign,
			wantTokenRotated: true,
		},
		{
			name:   "有标记但端口空闲（上次崩溃留下的陈旧标记）：正常启动，不动 token",
			marker: true,
			probe:  httpserver.ProbeNone,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := newTestConfig(t)
			before := cfg.Token
			opened := stubInstanceProbes(t, tc.marker, tc.occupied, tc.probe)

			got, err := ensureSingleInstance(cfg, t.TempDir(), false)

			require.NoError(t, err)
			require.Equal(t, tc.wantAlreadyRunning, got)
			if tc.wantTokenRotated {
				require.NotEqual(t, before, cfg.Token, "token 应该已轮换")
				require.Len(t, cfg.Token, len(before), "轮换后的 token 长度应一致")
			} else {
				require.Equal(t, before, cfg.Token, "token 不该被动")
			}
			if tc.wantBrowserOpened {
				require.Len(t, *opened, 1)
				require.Contains(t, (*opened)[0], cfg.Token, "首启 URL 必须带 token")
			} else {
				require.Empty(t, *opened)
			}
		})
	}
}

// skipBrowser 为真时（--no-browser / API-only）不该开浏览器，但仍然要退出。
func TestEnsureSingleInstanceSkipBrowser(t *testing.T) {
	cfg := newTestConfig(t)
	opened := stubInstanceProbes(t, true, false, httpserver.ProbeRunning)

	running, err := ensureSingleInstance(cfg, t.TempDir(), true)
	require.NoError(t, err)
	require.True(t, running)
	require.Empty(t, *opened)
}

// 轮换后的 token 必须已经落盘：否则下次启动会拿着旧 token 起服务。
func TestRotateTokenPersists(t *testing.T) {
	cfg := newTestConfig(t)
	before := cfg.Token

	require.NoError(t, rotateToken(cfg, "端口 %d 测试轮换"))

	path, err := config.Path()
	require.NoError(t, err)
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NotContains(t, string(raw), before, "旧 token 不该还留在配置里")
	require.Contains(t, string(raw), cfg.Token)
}

// launchURL 必须指向 127.0.0.1（不是 localhost：Host 白名单两者都收，但
// 127.0.0.1 不经 DNS，少一层被劫持的可能）并带上 token。
func TestLaunchURL(t *testing.T) {
	url := launchURL(8590, "deadbeef")
	require.Equal(t, "http://127.0.0.1:8590/?token=deadbeef", url)
}
