//go:build windows && desktoptest

package desktop

// 测试遥控的底层：从屏幕截图（GDI）、注入真实输入（SendInput / SetCursorPos）、抢前台。只在测试构建里。

import (
	"fmt"
	"image"
	"image/png"
	"os"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	pDwmGetWindowAttribute = dwmapi.NewProc("DwmGetWindowAttribute")
	pGetWindowTextW        = user32.NewProc("GetWindowTextW")
	pGetDC                 = user32.NewProc("GetDC")
	pReleaseDC             = user32.NewProc("ReleaseDC")
	pGetSystemMetrics      = user32.NewProc("GetSystemMetrics")
	pClientToScreen        = user32.NewProc("ClientToScreen")
	pSetCursorPos          = user32.NewProc("SetCursorPos")
	pSendInput             = user32.NewProc("SendInput")
	pAttachThreadInput     = user32.NewProc("AttachThreadInput")
	pCreateCompatibleDC    = gdi32.NewProc("CreateCompatibleDC")
	pCreateCompatibleBmp   = gdi32.NewProc("CreateCompatibleBitmap")
	pSelectObject          = gdi32.NewProc("SelectObject")
	pBitBlt                = gdi32.NewProc("BitBlt")
	pGetDIBits             = gdi32.NewProc("GetDIBits")
	pDeleteObject          = gdi32.NewProc("DeleteObject")
	pDeleteDC              = gdi32.NewProc("DeleteDC")
)

const (
	srcCopy        = 0x00CC0020
	captureBlt     = 0x40000000
	dwmaFrameBound = 9 // DWMWA_EXTENDED_FRAME_BOUNDS

	inputMouse    = 0
	inputKeyboard = 1

	mouseLeftDown   = 0x0002
	mouseLeftUp     = 0x0004
	mouseRightDown  = 0x0008
	mouseRightUp    = 0x0010
	mouseMiddleDown = 0x0020
	mouseMiddleUp   = 0x0040
	mouseWheel      = 0x0800
	keyEventUp      = 0x0002
)

func unsafePtr[T any](p *T) unsafe.Pointer { return unsafe.Pointer(p) }

func utf16ToString(b []uint16) string { return windows.UTF16ToString(b) }

// frameRect 是窗口在屏幕上看得见的那一块（不含 Win10 起窗口四周看不见的拖动边框）。
func frameRect(hwnd uintptr) rect {
	var r rect
	if hr, _, _ := pDwmGetWindowAttribute.Call(hwnd, dwmaFrameBound, uintptr(unsafe.Pointer(&r)), unsafe.Sizeof(r)); hr != 0 {
		return windowRect(hwnd)
	}
	return r
}

func virtualScreen() rect {
	m := func(i uintptr) int32 { v, _, _ := pGetSystemMetrics.Call(i); return int32(v) }
	x, y := m(76), m(77) // SM_XVIRTUALSCREEN / SM_YVIRTUALSCREEN
	return rect{x, y, x + m(78), y + m(79)}
}

type bitmapInfoHeader struct {
	Size          uint32
	Width, Height int32
	Planes        uint16
	BitCount      uint16
	Compression   uint32
	SizeImage     uint32
	XPels, YPels  int32
	ClrUsed       uint32
	ClrImportant  uint32
}

// captureScreen 把屏幕上 box 那一块存成 PNG。
func captureScreen(box rect, path string) error {
	w, h := box.width(), box.height()
	if w <= 0 || h <= 0 {
		return fmt.Errorf("截图区域是空的（%dx%d）：窗口藏着或最小化了？", w, h)
	}
	screen, _, _ := pGetDC.Call(0)
	if screen == 0 {
		return fmt.Errorf("拿不到屏幕的 DC")
	}
	defer pReleaseDC.Call(0, screen)
	mem, _, _ := pCreateCompatibleDC.Call(screen)
	defer pDeleteDC.Call(mem)
	bmp, _, _ := pCreateCompatibleBmp.Call(screen, uintptr(w), uintptr(h))
	if bmp == 0 {
		return fmt.Errorf("建不了 %dx%d 的位图", w, h)
	}
	defer pDeleteObject.Call(bmp)
	old, _, _ := pSelectObject.Call(mem, bmp)
	ok, _, err := pBitBlt.Call(mem, 0, 0, uintptr(w), uintptr(h), screen, uintptr(box.Left), uintptr(box.Top), srcCopy|captureBlt)
	pSelectObject.Call(mem, old)
	if ok == 0 {
		return fmt.Errorf("BitBlt 失败：%w", err)
	}
	hdr := bitmapInfoHeader{Size: uint32(unsafe.Sizeof(bitmapInfoHeader{})), Width: w, Height: -h, Planes: 1, BitCount: 32}
	buf := make([]byte, int(w)*int(h)*4)
	if n, _, _ := pGetDIBits.Call(mem, bmp, 0, uintptr(h), uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&hdr)), 0); n == 0 {
		return fmt.Errorf("GetDIBits 失败")
	}
	img := image.NewRGBA(image.Rect(0, 0, int(w), int(h)))
	for i := 0; i < len(buf); i += 4 { // BGRA → RGBA
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = buf[i+2], buf[i+1], buf[i], 0xFF
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}

func clientToScreen(hwnd uintptr, x, y int32) point {
	p := point{x, y}
	pClientToScreen.Call(hwnd, uintptr(unsafe.Pointer(&p)))
	return p
}

// sendInput 的结构（INPUT）：64 位下 type 之后对齐 8 字节，联合体按最大的 MOUSEINPUT 算是 32 字节。
type inputUnion struct {
	Type uint32
	_    uint32
	Data [4]uint64 // 32 字节，按 8 对齐（联合体里有 ULONG_PTR）
}

type mouseInput struct {
	Dx, Dy    int32
	MouseData uint32
	Flags     uint32
	Time      uint32
	_         uint32
	ExtraInfo uintptr
}

type keybdInput struct {
	Vk, Scan  uint16
	Flags     uint32
	Time      uint32
	_         uint32
	ExtraInfo uintptr
}

func send(inputs ...inputUnion) error {
	n, _, err := pSendInput.Call(uintptr(len(inputs)), uintptr(unsafe.Pointer(&inputs[0])), unsafe.Sizeof(inputs[0]))
	if int(n) != len(inputs) {
		return fmt.Errorf("SendInput 只送出 %d/%d 条（前台不是我们？UIPI？）：%v", n, len(inputs), err)
	}
	return nil
}

func mouseEvent(flags, data uint32) inputUnion {
	in := inputUnion{Type: inputMouse}
	*(*mouseInput)(unsafe.Pointer(&in.Data)) = mouseInput{Flags: flags, MouseData: data}
	return in
}

func keyEvent(vk uint16, up bool) inputUnion {
	in := inputUnion{Type: inputKeyboard}
	ki := keybdInput{Vk: vk}
	if up {
		ki.Flags = keyEventUp
	}
	*(*keybdInput)(unsafe.Pointer(&in.Data)) = ki
	return in
}

func buttonFlags(button string) (down, up uint32) {
	switch button {
	case "right":
		return mouseRightDown, mouseRightUp
	case "middle":
		return mouseMiddleDown, mouseMiddleUp
	}
	return mouseLeftDown, mouseLeftUp
}

// mouseAt 把光标移到 pt 再按需按键。dblclick 两次点击间隔 60ms（在系统的双击时间之内）。
func mouseAt(kind string, pt point, button string) error {
	if !boolCall(pSetCursorPos, uintptr(pt.X), uintptr(pt.Y)) {
		return fmt.Errorf("SetCursorPos 失败")
	}
	down, up := buttonFlags(button)
	switch kind {
	case "down":
		return send(mouseEvent(down, 0))
	case "up":
		return send(mouseEvent(up, 0))
	case "click":
		return send(mouseEvent(down, 0), mouseEvent(up, 0))
	case "dblclick":
		if err := send(mouseEvent(down, 0), mouseEvent(up, 0)); err != nil {
			return err
		}
		time.Sleep(60 * time.Millisecond)
		return send(mouseEvent(down, 0), mouseEvent(up, 0))
	}
	return nil
}

func sendWheel(delta int32) error {
	return send(mouseEvent(mouseWheel, uint32(delta)))
}

func sendKey(vk uint16, up bool) error { return send(keyEvent(vk, up)) }

// tapKey 按一下 vk（mods 里的 shift / ctrl / alt 先按下、后松开）。
func tapKey(vk uint16, mods []string) error {
	modVK := map[string]uint16{"shift": vkShift, "ctrl": vkControl, "alt": vkMenu}
	var seq []inputUnion
	var held []uint16
	for _, m := range mods {
		if v, ok := modVK[m]; ok {
			seq = append(seq, keyEvent(v, false))
			held = append(held, v)
		}
	}
	seq = append(seq, keyEvent(vk, false), keyEvent(vk, true))
	for i := len(held) - 1; i >= 0; i-- {
		seq = append(seq, keyEvent(held[i], true))
	}
	return send(seq...)
}

var (
	pWindowFromPoint = user32.NewProc("WindowFromPoint")
	pGetAncestor     = user32.NewProc("GetAncestor")
)

// ownedByUs：这个窗口的顶层祖先是 nagare 进程的（mpv 的子窗口属于 mpv 进程，但它挂在我们的窗口下面）。
func ownedByUs(h uintptr) bool {
	if h == 0 {
		return false
	}
	root, _, _ := pGetAncestor.Call(h, 2) // GA_ROOT
	if root == 0 {
		root = h
	}
	return windowPID(root) == uint32(os.Getpid())
}

func foregroundIsOurs() bool {
	fg, _, _ := pGetForegroundWindow.Call()
	return ownedByUs(fg)
}

// ensureOurForeground：前台不是我们就抢一次；还不是就报错，这一下不注入。
func ensureOurForeground() error {
	if foregroundIsOurs() {
		return nil
	}
	if err := onUI(func() { forceForeground(ui.top) }); err != nil {
		return err
	}
	time.Sleep(200 * time.Millisecond)
	if foregroundIsOurs() {
		return nil
	}
	return fmt.Errorf("前台不是 nagare 的窗口（别的程序抢走了焦点），这一下没注入，免得按到别的程序里")
}

// pointIsOurs：屏幕上这个点露出来的是 nagare 的窗口（或挂在它下面的 mpv 画面）。
func pointIsOurs(pt point) bool {
	h, _, _ := pWindowFromPoint.Call(uintptr(*(*uint64)(unsafe.Pointer(&pt))))
	return ownedByUs(h)
}

// forceForeground 是测试用的抢前台（正式代码里不这么做）：挂到当前前台线程的输入队列上再 SetForegroundWindow；
// 还不行才按住 Alt 再抢一次。CI 的会话里没有人点窗口，不这样 SendInput 落不到我们的窗口上。
// Alt 不能单按一下：单按 Alt 会让窗口进「菜单模式」，接下来的一下按键（空格、t、Enter）就被吃掉了 ——
// 所以中间夹一下 F24（mpv 里没有绑定），Alt 松开时就不是「单按」。
func forceForeground(hwnd uintptr) {
	fg, _, _ := pGetForegroundWindow.Call()
	if fg == hwnd {
		return
	}
	self := currentThreadID()
	var fgThread uintptr
	if fg != 0 {
		fgThread, _, _ = pGetWindowThreadProcessId.Call(fg, 0)
	}
	if fgThread != 0 && uint32(fgThread) != self {
		pAttachThreadInput.Call(uintptr(self), fgThread, 1)
		defer pAttachThreadInput.Call(uintptr(self), fgThread, 0)
	}
	if boolCall(pIsIconic, hwnd) {
		pShowWindow.Call(hwnd, swRestore)
	}
	pBringWindowToTop.Call(hwnd)
	if boolCall(pSetForegroundWindow, hwnd) {
		return
	}
	_ = send(keyEvent(vkMenu, false))
	pSetForegroundWindow.Call(hwnd)
	_ = send(keyEvent(vkF24, false), keyEvent(vkF24, true), keyEvent(vkMenu, true))
}
