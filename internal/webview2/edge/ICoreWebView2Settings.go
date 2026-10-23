//go:build windows

package edge

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

type _ICoreWebView2SettingsVtbl struct {
	_IUnknownVtbl
	GetIsScriptEnabled                ComProc
	PutIsScriptEnabled                ComProc
	GetIsWebMessageEnabled            ComProc
	PutIsWebMessageEnabled            ComProc
	GetAreDefaultScriptDialogsEnabled ComProc
	PutAreDefaultScriptDialogsEnabled ComProc
	GetIsStatusBarEnabled             ComProc
	PutIsStatusBarEnabled             ComProc
	GetAreDevToolsEnabled             ComProc
	PutAreDevToolsEnabled             ComProc
	GetAreDefaultContextMenusEnabled  ComProc
	PutAreDefaultContextMenusEnabled  ComProc
	GetAreHostObjectsAllowed          ComProc
	PutAreHostObjectsAllowed          ComProc
	GetIsZoomControlEnabled           ComProc
	PutIsZoomControlEnabled           ComProc
	GetIsBuiltInErrorPageEnabled      ComProc
	PutIsBuiltInErrorPageEnabled      ComProc
}

type ICoreWebView2Settings struct {
	vtbl *_ICoreWebView2SettingsVtbl
}

func (i *ICoreWebView2Settings) AddRef() uintptr {
	ret, _, _ := i.vtbl.AddRef.Call(uintptr(unsafe.Pointer(i)))

	return ret
}

func (i *ICoreWebView2Settings) PutIsScriptEnabled(isScriptEnabled bool) error {

	hr, _, _ := i.vtbl.PutIsScriptEnabled.Call(
		uintptr(unsafe.Pointer(i)),
		uintptr(boolToInt(isScriptEnabled)),
	)
	if windows.Handle(hr) != windows.S_OK {
		return windows.Errno(hr)
	}
	return nil
}

func (i *ICoreWebView2Settings) PutIsWebMessageEnabled(isWebMessageEnabled bool) error {

	hr, _, _ := i.vtbl.PutIsWebMessageEnabled.Call(
		uintptr(unsafe.Pointer(i)),
		uintptr(boolToInt(isWebMessageEnabled)),
	)
	if windows.Handle(hr) != windows.S_OK {
		return windows.Errno(hr)
	}
	return nil
}

func (i *ICoreWebView2Settings) PutAreDefaultScriptDialogsEnabled(areDefaultScriptDialogsEnabled bool) error {

	hr, _, _ := i.vtbl.PutAreDefaultScriptDialogsEnabled.Call(
		uintptr(unsafe.Pointer(i)),
		uintptr(boolToInt(areDefaultScriptDialogsEnabled)),
	)
	if windows.Handle(hr) != windows.S_OK {
		return windows.Errno(hr)
	}
	return nil
}

func (i *ICoreWebView2Settings) PutIsStatusBarEnabled(isStatusBarEnabled bool) error {

	hr, _, _ := i.vtbl.PutIsStatusBarEnabled.Call(
		uintptr(unsafe.Pointer(i)),
		uintptr(boolToInt(isStatusBarEnabled)),
	)
	if windows.Handle(hr) != windows.S_OK {
		return windows.Errno(hr)
	}
	return nil
}

func (i *ICoreWebView2Settings) PutAreDevToolsEnabled(areDevToolsEnabled bool) error {

	hr, _, _ := i.vtbl.PutAreDevToolsEnabled.Call(
		uintptr(unsafe.Pointer(i)),
		uintptr(boolToInt(areDevToolsEnabled)),
	)
	if windows.Handle(hr) != windows.S_OK {
		return windows.Errno(hr)
	}
	return nil
}

func (i *ICoreWebView2Settings) PutAreDefaultContextMenusEnabled(enabled bool) error {

	hr, _, _ := i.vtbl.PutAreDefaultContextMenusEnabled.Call(
		uintptr(unsafe.Pointer(i)),
		uintptr(boolToInt(enabled)),
	)
	if windows.Handle(hr) != windows.S_OK {
		return windows.Errno(hr)
	}
	return nil
}

func (i *ICoreWebView2Settings) PutAreHostObjectsAllowed(allowed bool) error {

	hr, _, _ := i.vtbl.PutAreHostObjectsAllowed.Call(
		uintptr(unsafe.Pointer(i)),
		uintptr(boolToInt(allowed)),
	)
	if windows.Handle(hr) != windows.S_OK {
		return windows.Errno(hr)
	}
	return nil
}

func (i *ICoreWebView2Settings) PutIsZoomControlEnabled(enabled bool) error {

	hr, _, _ := i.vtbl.PutIsZoomControlEnabled.Call(
		uintptr(unsafe.Pointer(i)),
		uintptr(boolToInt(enabled)),
	)
	if windows.Handle(hr) != windows.S_OK {
		return windows.Errno(hr)
	}
	return nil
}

func (i *ICoreWebView2Settings) PutIsBuiltInErrorPageEnabled(enabled bool) error {

	hr, _, _ := i.vtbl.PutIsBuiltInErrorPageEnabled.Call(
		uintptr(unsafe.Pointer(i)),
		uintptr(boolToInt(enabled)),
	)
	if windows.Handle(hr) != windows.S_OK {
		return windows.Errno(hr)
	}
	return nil
}
