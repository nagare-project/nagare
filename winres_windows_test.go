//go:build windows

package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"golang.org/x/sys/windows"
)

// 真 Windows 上：这个测试二进制也链着 rsrc_windows_amd64.syso，进程的 DPI 感知应当就是 manifest 写的 PerMonitorV2。
func TestProcessIsPerMonitorV2(t *testing.T) {
	user32 := windows.NewLazySystemDLL("user32.dll")
	getCtx := user32.NewProc("GetThreadDpiAwarenessContext")
	equal := user32.NewProc("AreDpiAwarenessContextsEqual")
	if getCtx.Find() != nil || equal.Find() != nil {
		t.Skip("系统太老，没有 DPI 感知上下文的接口")
	}
	ctx, _, _ := getCtx.Call()
	pmv2 := ^uintptr(3) // DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2
	r, _, _ := equal.Call(ctx, pmv2)
	assert.NotZero(t, r, "manifest 没生效：进程不是 PerMonitorV2")
}
