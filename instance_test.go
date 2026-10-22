package main

import (
	"errors"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nagare-project/nagare/internal/config"
	"github.com/nagare-project/nagare/internal/httpserver"
)

// instanceStubs 记下注入点被怎么调了。
type instanceStubs struct {
	opened      []string // 开浏览器的地址
	showAsked   int      // 请已有实例显示窗口的次数
	showedToken string   // 那个请求带的 token
}

// stubInstanceProbes 替换单实例判定的注入点，返回值在 t.Cleanup 里还原。
// 已有实例「有没有窗口」由 shown / showErr 决定。
func stubInstanceProbes(t *testing.T, marker, occupied bool, result httpserver.ProbeResult, shown bool, showErr error) *instanceStubs {
	t.Helper()
	oldHas, oldOcc, oldProbe, oldShow, oldOpen := hasInstance, portOccupied, probePort, requestShowWindow, openBrowserFn
	t.Cleanup(func() {
		hasInstance, portOccupied, probePort, requestShowWindow, openBrowserFn = oldHas, oldOcc, oldProbe, oldShow, oldOpen
	})
	s := &instanceStubs{}
	hasInstance = func(string) bool { return marker }
	portOccupied = func(int) bool { return occupied }
	probePort = func(int, string) httpserver.ProbeResult { return result }
	requestShowWindow = func(_ int, token string) (bool, error) {
		s.showAsked++
		s.showedToken = token
		return shown, showErr
	}
	openBrowserFn = func(url string) error { s.opened = append(s.opened, url); return nil }
	return s
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
			name:               "有标记且探测到我方实例（没有窗口）：把浏览器指过去并退出",
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
			stubs := stubInstanceProbes(t, tc.marker, tc.occupied, tc.probe, false, nil)

			got, err := ensureSingleInstance(cfg, t.TempDir(), surfaceWindow)

			require.NoError(t, err)
			require.Equal(t, tc.wantAlreadyRunning, got)
			if tc.wantTokenRotated {
				require.NotEqual(t, before, cfg.Token, "token 应该已轮换")
				require.Len(t, cfg.Token, len(before), "轮换后的 token 长度应一致")
			} else {
				require.Equal(t, before, cfg.Token, "token 不该被动")
			}
			if tc.wantBrowserOpened {
				require.Len(t, stubs.opened, 1)
				require.Contains(t, stubs.opened[0], cfg.Token, "首启 URL 必须带 token")
			} else {
				require.Empty(t, stubs.opened)
			}
			if !tc.wantAlreadyRunning {
				assert.Zero(t, stubs.showAsked, "没确认是我方实例之前不能带着 token 去请它显示窗口")
			}
		})
	}
}

// 第二次启动时已有实例在跑：怎么把它摆到前面。
func TestSecondLaunchSurfacesRunningInstance(t *testing.T) {
	tests := []struct {
		name        string
		how         surface
		shown       bool  // 已有实例有没有窗口
		showErr     error // 请求本身失败
		wantAsked   bool
		wantBrowser bool
	}{
		{name: "已有实例有窗口：提到前面，不开浏览器", how: surfaceWindow, shown: true, wantAsked: true},
		{name: "已有实例没有窗口（浏览器模式 / 旧版本）：开浏览器", how: surfaceWindow, wantAsked: true, wantBrowser: true},
		{name: "请求失败：开浏览器兜底", how: surfaceWindow, showErr: errors.New("超时"), wantAsked: true, wantBrowser: true},
		{name: "--no-window：用户要浏览器，不去请窗口", how: surfaceBrowser, shown: true, wantBrowser: true},
		{name: "--no-browser / API-only：什么都不弹", how: surfaceNone, shown: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := newTestConfig(t)
			stubs := stubInstanceProbes(t, true, true, httpserver.ProbeRunning, tc.shown, tc.showErr)

			running, err := ensureSingleInstance(cfg, t.TempDir(), tc.how)

			require.NoError(t, err)
			require.True(t, running, "已有实例在跑：这个进程要退出")
			assert.Equal(t, tc.wantAsked, stubs.showAsked == 1)
			if tc.wantAsked {
				assert.Equal(t, cfg.Token, stubs.showedToken, "请求要带已有实例的 token（同一个配置目录）")
			}
			if tc.wantBrowser {
				require.Len(t, stubs.opened, 1)
				assert.Equal(t, launchURL(cfg.Port, cfg.Token), stubs.opened[0])
			} else {
				assert.Empty(t, stubs.opened)
			}
		})
	}
}

// 开关 → 第二次启动的做法：--no-browser 与 API-only 什么都不弹，--no-window 只开浏览器，默认先请窗口。
func TestSecondLaunchSurface(t *testing.T) {
	assert.Equal(t, surfaceWindow, secondLaunchSurface(flags{}, false))
	assert.Equal(t, surfaceBrowser, secondLaunchSurface(flags{noWindow: true}, false))
	assert.Equal(t, surfaceBrowser, secondLaunchSurface(flags{noTray: true}, false), "--no-tray 与第一次启动同义：不用桌面窗口")
	assert.Equal(t, surfaceNone, secondLaunchSurface(flags{noBrowser: true}, false))
	assert.Equal(t, surfaceNone, secondLaunchSurface(flags{noBrowser: true, noWindow: true}, false))
	assert.Equal(t, surfaceNone, secondLaunchSurface(flags{}, true), "API-only")
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
