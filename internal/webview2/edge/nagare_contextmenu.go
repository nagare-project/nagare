//go:build windows

package edge

// nagare 自己加的（不是上游代码）：ContextMenuRequested（ICoreWebView2_11，运行时 97.0.1072.69 起）。
// 桌面窗口只在可编辑的地方（输入框：粘贴磁力链接、地址）和选中的文字上留浏览器的右键菜单，别处不弹
// （返回、刷新、另存为、打印……在应用里没有意义）。
//
// 虚表按 WebView2.idl 的继承顺序一级一级接上：ICoreWebView2_3（上游已有）→ _4 … _11。每一级的方法与
// wailsapp/go-webview2 main 分支 pkg/webview2 下从 IDL 生成的 ICoreWebView2_4.go … _11.go 逐项核对过；
// AddContextMenuRequested 在第 100 个槽（IUnknown 3 + ICoreWebView2 58 + _2 7 + _3 5 + _4 4 + _5 2 + _6 1 + _7 1 +
// _8 7 + _9 9 + _10 2 + CallDevToolsProtocolMethodForSession 1），有测试守着。

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

type iCoreWebView2_11Vtbl struct {
	iCoreWebView2_3Vtbl
	// ICoreWebView2_4
	AddFrameCreated        ComProc
	RemoveFrameCreated     ComProc
	AddDownloadStarting    ComProc
	RemoveDownloadStarting ComProc
	// ICoreWebView2_5
	AddClientCertificateRequested    ComProc
	RemoveClientCertificateRequested ComProc
	// ICoreWebView2_6
	OpenTaskManagerWindow ComProc
	// ICoreWebView2_7
	PrintToPdf ComProc
	// ICoreWebView2_8
	AddIsMutedChanged                   ComProc
	RemoveIsMutedChanged                ComProc
	GetIsMuted                          ComProc
	PutIsMuted                          ComProc
	AddIsDocumentPlayingAudioChanged    ComProc
	RemoveIsDocumentPlayingAudioChanged ComProc
	GetIsDocumentPlayingAudio           ComProc
	// ICoreWebView2_9
	AddIsDefaultDownloadDialogOpenChanged    ComProc
	RemoveIsDefaultDownloadDialogOpenChanged ComProc
	GetIsDefaultDownloadDialogOpen           ComProc
	OpenDefaultDownloadDialog                ComProc
	CloseDefaultDownloadDialog               ComProc
	GetDefaultDownloadDialogCornerAlignment  ComProc
	PutDefaultDownloadDialogCornerAlignment  ComProc
	GetDefaultDownloadDialogMargin           ComProc
	PutDefaultDownloadDialogMargin           ComProc
	// ICoreWebView2_10
	AddBasicAuthenticationRequested    ComProc
	RemoveBasicAuthenticationRequested ComProc
	// ICoreWebView2_11
	CallDevToolsProtocolMethodForSession ComProc
	AddContextMenuRequested              ComProc
	RemoveContextMenuRequested           ComProc
}

// ICoreWebView2_11 只用来挂 ContextMenuRequested。
type ICoreWebView2_11 struct {
	vtbl *iCoreWebView2_11Vtbl
}

// GetICoreWebView2_11 问这个 WebView 要 ICoreWebView2_11；运行时太老（< 97）时为 nil。拿到的要 Release。
func (i *ICoreWebView2) GetICoreWebView2_11() *ICoreWebView2_11 {
	var result *ICoreWebView2_11
	iid := NewGUID("{0be78e56-c193-4051-b943-23b460c08bdb}")
	hr, _, _ := i.vtbl.QueryInterface.Call(uintptr(unsafe.Pointer(i)), uintptr(unsafe.Pointer(iid)), uintptr(unsafe.Pointer(&result)))
	if windows.Handle(hr) != windows.S_OK {
		return nil
	}
	return result
}

// Release 放掉 QueryInterface 给的那一份引用。
func (i *ICoreWebView2_11) Release() uint32 {
	ret, _, _ := i.vtbl.Release.Call(uintptr(unsafe.Pointer(i)))
	return uint32(ret)
}

// AddContextMenuRequested 注册右键菜单的回调。
func (i *ICoreWebView2_11) AddContextMenuRequested(h *ICoreWebView2ContextMenuRequestedEventHandler) error {
	var token _EventRegistrationToken
	hr, _, _ := i.vtbl.AddContextMenuRequested.Call(uintptr(unsafe.Pointer(i)), uintptr(unsafe.Pointer(h)), uintptr(unsafe.Pointer(&token)))
	return hrErr(hr)
}

// ── ICoreWebView2ContextMenuRequestedEventArgs ──

type _ICoreWebView2ContextMenuRequestedEventArgsVtbl struct {
	_IUnknownVtbl
	GetMenuItems         ComProc
	GetContextMenuTarget ComProc
	GetLocation          ComProc
	PutSelectedCommandId ComProc
	GetSelectedCommandId ComProc
	PutHandled           ComProc
	GetHandled           ComProc
	GetDeferral          ComProc
}

// ICoreWebView2ContextMenuRequestedEventArgs 是一次右键菜单请求的参数。
type ICoreWebView2ContextMenuRequestedEventArgs struct {
	vtbl *_ICoreWebView2ContextMenuRequestedEventArgsVtbl
}

// GetContextMenuTarget 是右键点到的东西；拿到的要 Release。
func (i *ICoreWebView2ContextMenuRequestedEventArgs) GetContextMenuTarget() (*ICoreWebView2ContextMenuTarget, error) {
	var t *ICoreWebView2ContextMenuTarget
	hr, _, _ := i.vtbl.GetContextMenuTarget.Call(uintptr(unsafe.Pointer(i)), uintptr(unsafe.Pointer(&t)))
	if err := hrErr(hr); err != nil {
		return nil, err
	}
	return t, nil
}

// PutHandled 为真且宿主不自己画菜单：这一次什么菜单都不弹。
func (i *ICoreWebView2ContextMenuRequestedEventArgs) PutHandled(handled bool) error {
	return putBool(i.vtbl.PutHandled, unsafe.Pointer(i), handled)
}

// ── ICoreWebView2ContextMenuTarget ──

type _ICoreWebView2ContextMenuTargetVtbl struct {
	_IUnknownVtbl
	GetKind                    ComProc
	GetIsEditable              ComProc
	GetIsRequestedForMainFrame ComProc
	GetPageUri                 ComProc
	GetFrameUri                ComProc
	GetHasLinkUri              ComProc
	GetLinkUri                 ComProc
	GetHasLinkText             ComProc
	GetLinkText                ComProc
	GetHasSourceUri            ComProc
	GetSourceUri               ComProc
	GetHasSelection            ComProc
	GetSelectionText           ComProc
}

// ICoreWebView2ContextMenuTarget 描述右键点到的东西（输入框、选中的文字、链接、图片……）。
type ICoreWebView2ContextMenuTarget struct {
	vtbl *_ICoreWebView2ContextMenuTargetVtbl
}

// Release 放掉 GetContextMenuTarget 给的那一份引用。
func (i *ICoreWebView2ContextMenuTarget) Release() uint32 {
	ret, _, _ := i.vtbl.Release.Call(uintptr(unsafe.Pointer(i)))
	return uint32(ret)
}

// IsEditable 报告点到的是不是可编辑的地方（输入框、文本框、contenteditable）。
func (i *ICoreWebView2ContextMenuTarget) IsEditable() (bool, error) {
	return getBool(i.vtbl.GetIsEditable, unsafe.Pointer(i))
}

// HasSelection 报告点的时候有没有选中的文字。
func (i *ICoreWebView2ContextMenuTarget) HasSelection() (bool, error) {
	return getBool(i.vtbl.GetHasSelection, unsafe.Pointer(i))
}

// ── 回调对象 ──

type _ICoreWebView2ContextMenuRequestedEventHandlerVtbl struct {
	_IUnknownVtbl
	Invoke ComProc
}

// ContextMenuRequestedImpl 由使用方实现。
type ContextMenuRequestedImpl interface {
	_IUnknownImpl
	ContextMenuRequested(sender *ICoreWebView2, args *ICoreWebView2ContextMenuRequestedEventArgs) uintptr
}

// ICoreWebView2ContextMenuRequestedEventHandler 是 ContextMenuRequested 的回调对象。调用方要一直持有它。
type ICoreWebView2ContextMenuRequestedEventHandler struct {
	vtbl *_ICoreWebView2ContextMenuRequestedEventHandlerVtbl
	impl ContextMenuRequestedImpl
}

var contextMenuRequestedFn = _ICoreWebView2ContextMenuRequestedEventHandlerVtbl{
	_IUnknownVtbl{
		NewComProc(func(this *ICoreWebView2ContextMenuRequestedEventHandler, refiid, object uintptr) uintptr {
			return this.impl.QueryInterface(refiid, object)
		}),
		NewComProc(func(this *ICoreWebView2ContextMenuRequestedEventHandler) uintptr { return this.impl.AddRef() }),
		NewComProc(func(this *ICoreWebView2ContextMenuRequestedEventHandler) uintptr { return this.impl.Release() }),
	},
	NewComProc(func(this *ICoreWebView2ContextMenuRequestedEventHandler, sender *ICoreWebView2, args *ICoreWebView2ContextMenuRequestedEventArgs) uintptr {
		return this.impl.ContextMenuRequested(sender, args)
	}),
}

// NewContextMenuRequestedHandler 包一个回调对象。
func NewContextMenuRequestedHandler(impl ContextMenuRequestedImpl) *ICoreWebView2ContextMenuRequestedEventHandler {
	return &ICoreWebView2ContextMenuRequestedEventHandler{vtbl: &contextMenuRequestedFn, impl: impl}
}

// getBool 调一个 [out] BOOL 的 getter（4 字节，用 int32 接）。
func getBool(proc ComProc, this unsafe.Pointer) (bool, error) {
	var v int32
	hr, _, _ := proc.Call(uintptr(this), uintptr(unsafe.Pointer(&v)))
	return v != 0, hrErr(hr)
}
