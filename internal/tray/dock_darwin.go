//go:build darwin && cgo

package tray

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework Cocoa
#include <stdlib.h>
#include "dock_darwin.h"
*/
import "C"

import (
	"errors"
	"log"
	"unsafe"
)

// appDisplayName 是应用菜单 / 「退出 …」里显示的名字，与 Info.plist 的 CFBundleName 一致。
const appDisplayName = "Nagare"

// dockOpts 是 Dock 层回调要用的输入；只在 platformReady 里写，之后由主线程回调读。
var dockOpts Options

// dockTerminate 由 applicationShouldTerminate: 触发；带一格缓冲，系统连发两次也不阻塞主线程。
var dockTerminate = make(chan struct{}, 1)

// platformReady 在 systray 就绪后决定是否升成 Dock 应用：只有装成 .app 才升 ——
// 裸二进制（Homebrew、tar.gz）没有图标资源，Dock 里会是一个空白的通用图标。
func platformReady(opts Options) (<-chan struct{}, func()) {
	dockOpts = opts
	if !inAppBundle() {
		return nil, func() {}
	}
	name := C.CString(appDisplayName)
	defer C.free(unsafe.Pointer(name))
	addr := C.CString(opts.Address)
	defer C.free(unsafe.Pointer(addr))
	C.nagare_dock_setup(name, addr)
	return dockTerminate, func() {
		log.Print("tray: 收尾完成，答复系统可以终止")
		C.nagare_dock_reply_terminate()
	}
}

//export nagareDockOpen
func nagareDockOpen() {
	if dockOpts.OnOpen != nil {
		dockOpts.OnOpen()
	}
}

//export nagareDockCopyAddress
func nagareDockCopyAddress() {
	if err := copyToClipboard(dockOpts.Address); err != nil {
		log.Printf("tray: 复制地址失败：%v", err)
	}
}

//export nagareDockTerminate
func nagareDockTerminate() {
	log.Print("系统请求终止（⌘Q / Dock / 注销 / 关机），开始收尾")
	select {
	case dockTerminate <- struct{}{}:
	default:
	}
}

// showNotice 经通知中心弹一条；裸二进制没有 bundle 时通知中心不认，报错交给调用方记日志。
func showNotice(n Notice) error {
	title := C.CString(n.Title)
	defer C.free(unsafe.Pointer(title))
	body := C.CString(n.Body)
	defer C.free(unsafe.Pointer(body))
	if !bool(C.nagare_notify(title, body)) {
		return errors.New("不在 .app 里运行，通知中心不接受")
	}
	return nil
}

// Available 在 macOS（cgo 构建）总是可用：菜单栏不依赖任何会话服务。
func Available() bool { return true }

// CurrentMode 见 Mode：.app 里是 Dock 应用，裸二进制只有菜单栏图标。
func CurrentMode() Mode {
	if inAppBundle() {
		return ModeDock
	}
	return ModeMenuBar
}
