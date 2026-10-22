//go:build darwin && cgo && libmpv && desktoptest

package desktop

/*
#include <stdlib.h>
#include "testctl_darwin.h"
#include "desktop_darwin.h"
*/
import "C"

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"unsafe"
)

// 测试构建（-tags "libmpv desktoptest"）才有：设了 NAGARE_DESKTOP_TEST_ADDR（只许回环地址）就在那里
// 开一个遥控，给真机脚本截窗口、合成输入、读状态。正式构建没有这个文件。
//
// 遥控能在网页层执行脚本、往任意路径写截图，所以和正式的 API 一样把门：Host 头必须正好是这个监听地址
// （挡 DNS rebinding），每个请求还要带每次运行随机生成的密钥（挡开发机浏览器里的网页随手发 GET）。
// 密钥写进 NAGARE_DESKTOP_TEST_SECRET_FILE（0600）；没给这个文件就不开遥控。
func init() {
	addr := os.Getenv("NAGARE_DESKTOP_TEST_ADDR")
	if addr == "" {
		return
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() {
		log.Printf("desktop: NAGARE_DESKTOP_TEST_ADDR 只能是回环地址，忽略：%v", err)
		return
	}
	secret, err := writeTestSecret(os.Getenv("NAGARE_DESKTOP_TEST_SECRET_FILE"))
	if err != nil {
		log.Printf("desktop: 测试遥控不开：%v", err)
		return
	}
	// NAGARE_DESKTOP_TEST_SOFTWARE_GL：1 = 没有硬件 OpenGL 时退到软件渲染器并给 mpv 设 gpu-sw=yes（CI 的 runner
	// 有时没有 GPU），force = 直接用软件渲染器。只有测试构建认它。
	softwareGL := 0
	switch os.Getenv("NAGARE_DESKTOP_TEST_SOFTWARE_GL") {
	case "1":
		softwareGL = 1
	case "force":
		softwareGL = 2
	}
	C.nagare_test_init(C.int(softwareGL))
	mux := http.NewServeMux()
	str := func(r *http.Request, k string) (*C.char, func()) {
		c := C.CString(r.URL.Query().Get(k))
		return c, func() { C.free(unsafe.Pointer(c)) }
	}
	num := func(r *http.Request, k string) float64 {
		v, _ := strconv.ParseFloat(r.URL.Query().Get(k), 64)
		return v
	}
	mux.HandleFunc("/snap", func(w http.ResponseWriter, r *http.Request) {
		p, free := str(r, "path")
		defer free()
		fmt.Fprintln(w, C.nagare_test_snap(p))
	})
	mux.HandleFunc("/mouse", func(w http.ResponseWriter, r *http.Request) {
		k, free := str(r, "kind")
		defer free()
		clicks := int(num(r, "clicks"))
		if clicks == 0 {
			clicks = 1
		}
		C.nagare_test_mouse(k, C.double(num(r, "x")), C.double(num(r, "y")), C.int(clicks))
		fmt.Fprintln(w, "ok")
	})
	mux.HandleFunc("/key", func(w http.ResponseWriter, r *http.Request) {
		c, free := str(r, "chars")
		defer free()
		C.nagare_test_key(C.int(num(r, "code")), c, C.int(num(r, "mods")), C.int(num(r, "up")))
		fmt.Fprintln(w, "ok")
	})
	mux.HandleFunc("/js", func(w http.ResponseWriter, r *http.Request) {
		c, free := str(r, "code")
		defer free()
		C.nagare_test_js(c)
		fmt.Fprintln(w, "ok")
	})
	mux.HandleFunc("/screen", func(w http.ResponseWriter, r *http.Request) {
		C.nagare_test_move_to_screen(C.int(num(r, "index")))
		fmt.Fprintln(w, "ok")
	})
	// 等价于点 Dock / 托盘的「打开 nagare」
	mux.HandleFunc("/show", func(w http.ResponseWriter, r *http.Request) {
		C.nagare_desktop_show()
		fmt.Fprintln(w, "ok")
	})
	mux.HandleFunc("/state", func(w http.ResponseWriter, r *http.Request) {
		s := C.nagare_test_state()
		defer C.free(unsafe.Pointer(s))
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, C.GoString(s))
	})
	go func() {
		log.Printf("desktop: 测试遥控在 %s", addr)
		if err := http.ListenAndServe(addr, guardTestControl(addr, secret, mux)); err != nil {
			log.Printf("desktop: 测试遥控停了：%v", err)
		}
	}()
}
