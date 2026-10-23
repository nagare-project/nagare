//go:build windows

package edge

import (
	"testing"
	"unsafe"
)

// 虚表的槽位数错一个就是调到别的方法上（崩溃或者静默做错事）：按 WebView2.idl 的继承顺序核对几个关键位置。
func TestCoreWebView2VtableSlots(t *testing.T) {
	slot := func(off uintptr) uintptr { return off / unsafe.Sizeof(ComProc(0)) }
	var v iCoreWebView2_11Vtbl
	for _, c := range []struct {
		name string
		got  uintptr
		want uintptr
	}{
		// ICoreWebView2 的最后一个方法：IUnknown 3 + 58 - 1
		{"RemoveWindowCloseRequested", slot(unsafe.Offsetof(v.RemoveWindowCloseRequested)), 60},
		// ICoreWebView2_2 的最后一个
		{"GetEnvironment", slot(unsafe.Offsetof(v.GetEnvironment)), 67},
		// ICoreWebView2_3 的最后一个
		{"ClearVirtualHostNameToFolderMapping", slot(unsafe.Offsetof(v.ClearVirtualHostNameToFolderMapping)), 72},
		{"AddFrameCreated（_4 的第一个）", slot(unsafe.Offsetof(v.AddFrameCreated)), 73},
		{"AddBasicAuthenticationRequested（_10 的第一个）", slot(unsafe.Offsetof(v.AddBasicAuthenticationRequested)), 97},
		{"AddContextMenuRequested", slot(unsafe.Offsetof(v.AddContextMenuRequested)), 100},
	} {
		if c.got != c.want {
			t.Errorf("%s 在第 %d 个槽，应为 %d", c.name, c.got, c.want)
		}
	}
	var a _ICoreWebView2ContextMenuRequestedEventArgsVtbl
	if got := slot(unsafe.Offsetof(a.PutHandled)); got != 8 {
		t.Errorf("ContextMenuRequestedEventArgs.PutHandled 在第 %d 个槽，应为 8", got)
	}
	var tg _ICoreWebView2ContextMenuTargetVtbl
	if got := slot(unsafe.Offsetof(tg.GetIsEditable)); got != 4 {
		t.Errorf("ContextMenuTarget.IsEditable 在第 %d 个槽，应为 4", got)
	}
	if got := slot(unsafe.Offsetof(tg.GetHasSelection)); got != 14 {
		t.Errorf("ContextMenuTarget.HasSelection 在第 %d 个槽，应为 14", got)
	}
}
