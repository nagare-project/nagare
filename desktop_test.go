package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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
