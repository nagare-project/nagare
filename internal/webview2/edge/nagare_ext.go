//go:build windows

package edge

// nagare 自己加的（不是上游代码）：桌面窗口要的几个接口，上游 pkg/edge 只在虚表里留了位置、没有包装。
//   - NavigationStarting：窗口里只放行 nagare 自己的源，别的交给默认浏览器或拦下（internal/desktop 的导航规则）
//   - NewWindowRequested：target=_blank / window.open 永远不开第二个 WebView 窗口
//   - GetBrowserProcessID / Controller.Close：退出与自更新重启前等浏览器进程退出（用户数据目录同时只能有一个会话）
//   - Reload：渲染进程崩了重新载入
//
// COM 的 BOOL 是 4 字节：出参一律用 int32 接，不用 Go 的 bool（上游有几处用 bool，会写坏相邻的栈内存）。

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// ── ICoreWebView2NavigationStartingEventArgs ──

type _ICoreWebView2NavigationStartingEventArgsVtbl struct {
	_IUnknownVtbl
	GetUri             ComProc
	GetIsUserInitiated ComProc
	GetIsRedirected    ComProc
	GetRequestHeaders  ComProc
	GetCancel          ComProc
	PutCancel          ComProc
	GetNavigationId    ComProc
}

// ICoreWebView2NavigationStartingEventArgs 是一次顶层导航开始时的参数。
type ICoreWebView2NavigationStartingEventArgs struct {
	vtbl *_ICoreWebView2NavigationStartingEventArgsVtbl
}

// GetUri 是要去的地址。
func (i *ICoreWebView2NavigationStartingEventArgs) GetUri() (string, error) {
	return getString(i.vtbl.GetUri, unsafe.Pointer(i))
}

// PutCancel 取消这次导航。
func (i *ICoreWebView2NavigationStartingEventArgs) PutCancel(cancel bool) error {
	return putBool(i.vtbl.PutCancel, unsafe.Pointer(i), cancel)
}

type _ICoreWebView2NavigationStartingEventHandlerVtbl struct {
	_IUnknownVtbl
	Invoke ComProc
}

// NavigationStartingImpl 由使用方实现。
type NavigationStartingImpl interface {
	_IUnknownImpl
	NavigationStarting(sender *ICoreWebView2, args *ICoreWebView2NavigationStartingEventArgs) uintptr
}

// ICoreWebView2NavigationStartingEventHandler 是 NavigationStarting 的回调对象。调用方要一直持有它（COM 那边存的是裸指针）。
type ICoreWebView2NavigationStartingEventHandler struct {
	vtbl *_ICoreWebView2NavigationStartingEventHandlerVtbl
	impl NavigationStartingImpl
}

var navigationStartingFn = _ICoreWebView2NavigationStartingEventHandlerVtbl{
	_IUnknownVtbl{
		NewComProc(func(this *ICoreWebView2NavigationStartingEventHandler, refiid, object uintptr) uintptr {
			return this.impl.QueryInterface(refiid, object)
		}),
		NewComProc(func(this *ICoreWebView2NavigationStartingEventHandler) uintptr { return this.impl.AddRef() }),
		NewComProc(func(this *ICoreWebView2NavigationStartingEventHandler) uintptr { return this.impl.Release() }),
	},
	NewComProc(func(this *ICoreWebView2NavigationStartingEventHandler, sender *ICoreWebView2, args *ICoreWebView2NavigationStartingEventArgs) uintptr {
		return this.impl.NavigationStarting(sender, args)
	}),
}

// NewNavigationStartingHandler 包一个回调对象。
func NewNavigationStartingHandler(impl NavigationStartingImpl) *ICoreWebView2NavigationStartingEventHandler {
	return &ICoreWebView2NavigationStartingEventHandler{vtbl: &navigationStartingFn, impl: impl}
}

// AddNavigationStarting 注册顶层导航的回调。
func (i *ICoreWebView2) AddNavigationStarting(h *ICoreWebView2NavigationStartingEventHandler) error {
	var token _EventRegistrationToken
	hr, _, _ := i.vtbl.AddNavigationStarting.Call(uintptr(unsafe.Pointer(i)), uintptr(unsafe.Pointer(h)), uintptr(unsafe.Pointer(&token)))
	return hrErr(hr)
}

// ── ICoreWebView2NewWindowRequestedEventArgs ──

type _ICoreWebView2NewWindowRequestedEventArgsVtbl struct {
	_IUnknownVtbl
	GetUri             ComProc
	PutNewWindow       ComProc
	GetNewWindow       ComProc
	PutHandled         ComProc
	GetHandled         ComProc
	GetIsUserInitiated ComProc
	GetDeferral        ComProc
	GetWindowFeatures  ComProc
}

// ICoreWebView2NewWindowRequestedEventArgs 是 target=_blank / window.open 的参数。
type ICoreWebView2NewWindowRequestedEventArgs struct {
	vtbl *_ICoreWebView2NewWindowRequestedEventArgsVtbl
}

// GetUri 是新窗口要开的地址。
func (i *ICoreWebView2NewWindowRequestedEventArgs) GetUri() (string, error) {
	return getString(i.vtbl.GetUri, unsafe.Pointer(i))
}

// PutHandled 为真且不给 NewWindow：不开任何新窗口。
func (i *ICoreWebView2NewWindowRequestedEventArgs) PutHandled(handled bool) error {
	return putBool(i.vtbl.PutHandled, unsafe.Pointer(i), handled)
}

type _ICoreWebView2NewWindowRequestedEventHandlerVtbl struct {
	_IUnknownVtbl
	Invoke ComProc
}

// NewWindowRequestedImpl 由使用方实现。
type NewWindowRequestedImpl interface {
	_IUnknownImpl
	NewWindowRequested(sender *ICoreWebView2, args *ICoreWebView2NewWindowRequestedEventArgs) uintptr
}

// ICoreWebView2NewWindowRequestedEventHandler 是 NewWindowRequested 的回调对象。调用方要一直持有它。
type ICoreWebView2NewWindowRequestedEventHandler struct {
	vtbl *_ICoreWebView2NewWindowRequestedEventHandlerVtbl
	impl NewWindowRequestedImpl
}

var newWindowRequestedFn = _ICoreWebView2NewWindowRequestedEventHandlerVtbl{
	_IUnknownVtbl{
		NewComProc(func(this *ICoreWebView2NewWindowRequestedEventHandler, refiid, object uintptr) uintptr {
			return this.impl.QueryInterface(refiid, object)
		}),
		NewComProc(func(this *ICoreWebView2NewWindowRequestedEventHandler) uintptr { return this.impl.AddRef() }),
		NewComProc(func(this *ICoreWebView2NewWindowRequestedEventHandler) uintptr { return this.impl.Release() }),
	},
	NewComProc(func(this *ICoreWebView2NewWindowRequestedEventHandler, sender *ICoreWebView2, args *ICoreWebView2NewWindowRequestedEventArgs) uintptr {
		return this.impl.NewWindowRequested(sender, args)
	}),
}

// NewNewWindowRequestedHandler 包一个回调对象。
func NewNewWindowRequestedHandler(impl NewWindowRequestedImpl) *ICoreWebView2NewWindowRequestedEventHandler {
	return &ICoreWebView2NewWindowRequestedEventHandler{vtbl: &newWindowRequestedFn, impl: impl}
}

// AddNewWindowRequested 注册新窗口请求的回调。
func (i *ICoreWebView2) AddNewWindowRequested(h *ICoreWebView2NewWindowRequestedEventHandler) error {
	var token _EventRegistrationToken
	hr, _, _ := i.vtbl.AddNewWindowRequested.Call(uintptr(unsafe.Pointer(i)), uintptr(unsafe.Pointer(h)), uintptr(unsafe.Pointer(&token)))
	return hrErr(hr)
}

// ── 其余几个 ──

// GetBrowserProcessID 是这个 WebView 的浏览器进程（msedgewebview2.exe 主进程）的 PID。
func (i *ICoreWebView2) GetBrowserProcessID() (uint32, error) {
	var pid uint32
	hr, _, _ := i.vtbl.GetBrowserProcessID.Call(uintptr(unsafe.Pointer(i)), uintptr(unsafe.Pointer(&pid)))
	return pid, hrErr(hr)
}

// HasFullScreenElement 报告页面里有没有元素在全屏（预告片的全屏按钮）。上游的 GetContainsFullScreenElement
// 拿 1 字节的 Go bool 接 4 字节的 BOOL，这里用 int32。
func (i *ICoreWebView2) HasFullScreenElement() (bool, error) {
	var v int32
	hr, _, _ := i.vtbl.GetContainsFullScreenElement.Call(uintptr(unsafe.Pointer(i)), uintptr(unsafe.Pointer(&v)))
	return v != 0, hrErr(hr)
}

// scriptResult 收 ExecuteScript 的结果（JSON 文本）。
type scriptResult struct {
	done func(result string, err error)
}

func (*scriptResult) QueryInterface(_, _ uintptr) uintptr { return 0 }
func (*scriptResult) AddRef() uintptr                     { return 1 }
func (*scriptResult) Release() uintptr                    { return 1 }

func (r *scriptResult) ExecuteScriptCompleted(errorCode uintptr, result *uint16) uintptr {
	if err := hrErr(errorCode); err != nil {
		r.done("", err)
		return 0
	}
	r.done(windows.UTF16PtrToString(result), nil)
	return 0
}

// liveScripts 持有在途的回调对象（COM 那边存的是裸指针，结果回来之前不能被 GC 收走）。只在界面线程上碰。
var liveScripts = map[*iCoreWebView2ExecuteScriptCompletedHandler]struct{}{}

// RunScript 在页面里执行 js，结果（JSON 文本）经 done 回来（界面线程上）。只给测试遥控用。
func (i *ICoreWebView2) RunScript(js string, done func(result string, err error)) error {
	var h *iCoreWebView2ExecuteScriptCompletedHandler
	h = newICoreWebView2ExecuteScriptCompletedHandler(&scriptResult{done: func(res string, err error) {
		delete(liveScripts, h)
		done(res, err)
	}})
	liveScripts[h] = struct{}{}
	if err := i.ExecuteScript(js, h); err != nil {
		delete(liveScripts, h)
		return err
	}
	return nil
}

// UserAgentString 是当前的 User-Agent（ICoreWebView2Settings2::get_UserAgent）。上游的 GetUserAgent 把出参写到
// 空指针上（传的是指针的值而不是它的地址），一调就崩，这里另写一份。
func (i *ICoreWebViewSettings) UserAgentString() (string, error) {
	return getString(i.vtbl.GetUserAgent, unsafe.Pointer(i))
}

// Reload 重新载入当前页面。
func (i *ICoreWebView2) Reload() error {
	hr, _, _ := i.vtbl.Reload.Call(uintptr(unsafe.Pointer(i)))
	return hrErr(hr)
}

// Close 关掉控制器（WebView 随之销毁；最后一个 WebView 关掉之后浏览器进程退出）。
func (i *ICoreWebView2Controller) Close() error {
	hr, _, _ := i.vtbl.Close.Call(uintptr(unsafe.Pointer(i)))
	return hrErr(hr)
}

// CoreWebView 是控制器建好之后的 ICoreWebView2（之前为 nil）。
func (e *Chromium) CoreWebView() *ICoreWebView2 { return e.webview }

func hrErr(hr uintptr) error {
	if windows.Handle(hr) != windows.S_OK {
		return windows.Errno(hr)
	}
	return nil
}

// getString 调一个 [out] LPWSTR 的 getter，转成 Go 字符串并释放 COM 分配的内存。
func getString(proc ComProc, this unsafe.Pointer) (string, error) {
	var p *uint16
	hr, _, _ := proc.Call(uintptr(this), uintptr(unsafe.Pointer(&p)))
	if err := hrErr(hr); err != nil {
		return "", err
	}
	if p == nil {
		return "", nil
	}
	s := windows.UTF16PtrToString(p)
	windows.CoTaskMemFree(unsafe.Pointer(p))
	return s, nil
}

func putBool(proc ComProc, this unsafe.Pointer, v bool) error {
	var b uintptr
	if v {
		b = 1
	}
	hr, _, _ := proc.Call(uintptr(this), b)
	return hrErr(hr)
}
