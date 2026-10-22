//go:build darwin && cgo && libmpv

package desktop

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework Cocoa -framework WebKit -framework OpenGL -framework QuartzCore -framework MediaPlayer -lmpv.2
#include <stdlib.h>
#include "desktop_darwin.h"
*/
import "C"

import (
	"context"
	"errors"
	"fmt"
	"log"
	"unsafe"

	"github.com/nagare-project/nagare/internal/mpv"
)

// Available 报告这个构建里有没有桌面窗口：带 libmpv 标签的 macOS cgo 构建恒为真。
// 能不能用还要看有没有 Cocoa 事件循环（托盘），由调用方判断。
func Available() bool { return true }

// Init 记下界面地址与允许的源，不建窗口（NSApp 那时可能还没跑起来）。
func Init(o Options) {
	url, origin := C.CString(o.URL), C.CString(o.Origin)
	defer C.free(unsafe.Pointer(url))
	defer C.free(unsafe.Pointer(origin))
	C.nagare_desktop_init(url, origin)
}

// Show 把窗口提到前面；第一次调用时建窗口。
func Show() { C.nagare_desktop_show() }

// Open 在窗口里载入 url 并把窗口提到前面。
func Open(url string) {
	c := C.CString(url)
	defer C.free(unsafe.Pointer(c))
	C.nagare_desktop_open(c)
}

// Launch 是交给 player.Options.Launch 的启动器：在桌面窗口里用进程内 libmpv 起播。
//
// 顺序：建句柄并设 Init 选项 → 初始化 → 渲染上下文（主线程）→ 连 IPC（mpv.AttachEndpoint）→
// 以属性设 BeforeLoad → loadfile。IPC 先连上再 loadfile：播放器从第一个事件开始都看得见。
// ctx 只约束起播阶段。
func Launch(ctx context.Context, opts mpv.LaunchOptions) (*mpv.Player, error) {
	if err := mpv.CheckMedia(opts.MediaPath); err != nil {
		return nil, err
	}
	endpoint, err := mpv.NewIPCEndpoint(opts.SocketDir)
	if err != nil {
		return nil, err
	}
	spec, err := libmpvSpec(opts, endpoint)
	if err != nil {
		return nil, err
	}
	if err := call(spec.Init, func(o *C.nagare_mpv_opt, n C.int, e **C.char) C.int {
		return C.nagare_desktop_start(o, n, e)
	}); err != nil {
		return nil, fmt.Errorf("桌面窗口里的 mpv 没起来：%w", err)
	}
	p, err := mpv.AttachEndpoint(ctx, endpoint, opts.MediaPath)
	if err != nil {
		C.nagare_desktop_abort()
		return nil, fmt.Errorf("连不上桌面窗口里的 mpv：%w", err)
	}
	media := C.CString(spec.Media)
	defer C.free(unsafe.Pointer(media))
	if err := call(spec.BeforeLoad, func(o *C.nagare_mpv_opt, n C.int, e **C.char) C.int {
		return C.nagare_desktop_load(o, n, media, e)
	}); err != nil {
		_ = p.Close()
		return nil, fmt.Errorf("桌面窗口里的 mpv 没能打开这个文件：%w", err)
	}
	return p, nil
}

// call 把选项交给 C（marshal 出来的内存在 fn 返回后释放；C 那边先拷进自己的对象再切主线程）。
func call(opts []mpv.LibmpvOption, fn func(*C.nagare_mpv_opt, C.int, **C.char) C.int) error {
	arr, free := marshal(opts)
	defer free()
	var cerr *C.char
	if fn(arr, C.int(len(opts)), &cerr) == 0 {
		return nil
	}
	if cerr == nil {
		return errors.New("未知错误")
	}
	defer C.free(unsafe.Pointer(cerr))
	return errors.New(C.GoString(cerr))
}

// marshal 把 Go 的选项翻成 C 数组。返回的 free 释放全部 C 内存。
func marshal(opts []mpv.LibmpvOption) (*C.nagare_mpv_opt, func()) {
	var allocs []unsafe.Pointer
	cstr := func(s string) *C.char {
		p := C.CString(s)
		allocs = append(allocs, unsafe.Pointer(p))
		return p
	}
	cstrs := func(list []string) **C.char {
		if len(list) == 0 {
			return nil
		}
		p := (**C.char)(C.malloc(C.size_t(len(list)) * C.size_t(unsafe.Sizeof(uintptr(0)))))
		allocs = append(allocs, unsafe.Pointer(p))
		s := unsafe.Slice(p, len(list))
		for i, v := range list {
			s[i] = cstr(v)
		}
		return p
	}
	free := func() {
		for _, p := range allocs {
			C.free(p)
		}
	}
	if len(opts) == 0 {
		return nil, free
	}
	arr := (*C.nagare_mpv_opt)(C.malloc(C.size_t(len(opts)) * C.size_t(unsafe.Sizeof(C.nagare_mpv_opt{}))))
	allocs = append(allocs, unsafe.Pointer(arr))
	out := unsafe.Slice(arr, len(opts))
	for i, src := range opts {
		dst := &out[i]
		*dst = C.nagare_mpv_opt{name: cstr(src.Name), kind: C.NAGARE_OPT_STRING}
		switch src.Kind {
		case mpv.OptionList:
			dst.kind = C.NAGARE_OPT_LIST
			dst.items = cstrs(src.List)
			dst.count = C.int(len(src.List))
		case mpv.OptionMap:
			keys, values := make([]string, len(src.Map)), make([]string, len(src.Map))
			for j, kv := range src.Map {
				keys[j], values[j] = kv.Key, kv.Value
			}
			dst.kind = C.NAGARE_OPT_MAP
			dst.items = cstrs(keys)
			dst.values = cstrs(values)
			dst.count = C.int(len(src.Map))
		default:
			dst.value = cstr(src.String)
		}
	}
	return arr, free
}

//export nagareDesktopLog
func nagareDesktopLog(msg *C.char) {
	log.Printf("desktop: %s", C.GoString(msg))
}
