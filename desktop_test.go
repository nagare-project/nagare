package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nagare-project/nagare/internal/config"
	"github.com/nagare-project/nagare/internal/desktop"
	"github.com/nagare-project/nagare/internal/tray"
)

// 什么时候用桌面窗口：构建得带着它，还要有托盘（Cocoa 事件循环）与内嵌界面；--no-window 退回浏览器。
func TestDesktopUsable(t *testing.T) {
	for _, tc := range []struct {
		name      string
		available bool
		f         flags
		apiOnly   bool
		want      bool
	}{
		{"带窗口的构建、默认开关", true, flags{}, false, true},
		{"--no-browser 不影响用不用窗口（只是启动时不弹）", true, flags{noBrowser: true}, false, true},
		{"没带窗口的构建", false, flags{}, false, false},
		{"--no-tray：没有 Cocoa 事件循环", true, flags{noTray: true}, false, false},
		{"--no-window：退回浏览器", true, flags{noWindow: true}, false, false},
		{"API-only：窗口里没东西可看", true, flags{}, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, desktopUsable(tc.available, tc.f, tc.apiOnly))
		})
	}
	// 默认构建（这个测试跑的就是）不带窗口：怎么都不用
	if !desktop.Available() {
		assert.False(t, desktopEnabled(flags{}, false))
	}
}

// 桌面模式下托盘的「打开」开窗口，另加「在浏览器中打开」（带 token 的首个地址），关于页开在窗口里；
// --no-browser 时启动后不弹窗口。原来的退出与收尾回调原样保留。
func TestWithDesktopRewiresTray(t *testing.T) {
	var opened []string
	oldOpen := openBrowserFn
	t.Cleanup(func() { openBrowserFn = oldOpen })
	openBrowserFn = func(url string) error { opened = append(opened, url); return nil }
	quit := func() {}
	done := make(chan struct{})
	base := tray.Options{URL: "u", Address: "a", Version: "v", OnQuit: quit, Done: done}

	got := withDesktop(base, "http://127.0.0.1:8591/?token=t", "http://127.0.0.1:8591/settings?token=t#about", true)
	assert.True(t, got.Desktop)
	require.NotNil(t, got.OnOpen)
	require.NotNil(t, got.OnAbout)
	require.NotNil(t, got.OnReady, "启动后弹出窗口")
	require.NotNil(t, got.OnOpenBrowser)
	got.OnOpenBrowser()
	assert.Equal(t, []string{"http://127.0.0.1:8591/?token=t"}, opened)
	assert.NotNil(t, got.OnQuit)
	assert.Equal(t, (<-chan struct{})(done), got.Done)
	assert.Equal(t, "a", got.Address)

	quiet := withDesktop(base, "x", "y", false)
	assert.Nil(t, quiet.OnReady, "--no-browser：启动后不弹窗口")
	assert.True(t, quiet.Desktop)
}

// 第二次启动在这个实例上做的事：用桌面窗口时有 ShowWindow（提到前面、报 shown），浏览器模式下为 nil
// —— httpserver 据此回 shown=false，新进程开浏览器。
func TestWindowShower(t *testing.T) {
	assert.Nil(t, windowShower(false))
	show := windowShower(true)
	require.NotNil(t, show)
	if !desktop.Available() {
		// 桩里没有窗口（State 不 Active）：报「没有窗口」，新进程开浏览器（这个构建本来就不会走到 window=true）。
		// 有窗口的构建里，窗口中途用不了（Windows 的 WebView2 起不来）时同样报没有窗口
		assert.False(t, show())
	}
}

// WebView2 的用户数据目录：平时在本机缓存目录（不漫游）；指定了配置目录（测试、便携）时跟着配置目录走。
func TestDesktopCacheDir(t *testing.T) {
	t.Setenv(config.EnvConfigDir, "")
	if base, err := os.UserCacheDir(); err == nil {
		assert.Equal(t, filepath.Join(base, "nagare"), desktopCacheDir("/cfg"))
	}
	t.Setenv(config.EnvConfigDir, "/tmp/kit/config")
	assert.Equal(t, filepath.Join("/tmp/kit/config", "cache"), desktopCacheDir("/tmp/kit/config"))
}
