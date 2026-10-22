//go:build windows

package desktop

// IPC 转发输入：退路，只在 NAGARE_DESKTOP_INPUT=ipc 时用。
//
// 正常情况下宿主重新启用 mpv 的子窗口，输入由 mpv 自己的窗口收（悬停、光标隐藏、双击、键盘布局、输入法、
// 媒体键都是 mpv 原生的）。那不是 mpv 官方支持的用法：万一某个版本的 mpv 或某台机器上它出了问题，这条退路让
// mpv 的子窗口保持 disabled，输入落在画面层（宿主的窗口）上，由这里翻成 mpv 的输入命令发过去 ——
// mpv-examples 文档写的嵌入方式就是这个（mouse / keydown / keyup / keypress）。
//
// 转发时的几条规矩（docs/player-ui.md「宿主要做到的」）：键用 keydown / keyup 成对发，系统的按住重复丢掉
// （mpv 自己会重复）；「全部松开」写成 keyup ""（不带参数 mpv 0.41 会崩）；鼠标离开发 mouse -1 -1；
// 双击不用转发（mpv 拿两次按下自己合成）；光标停 1 秒由宿主自己藏（mpv 的 cursor-autohide 管不到宿主的窗口）。

import (
	"time"
	"unsafe"
)

const (
	cursorIdle   = time.Second
	cursorTimer  = 1
	cursorPoll   = 250 // 毫秒
	repeatBit    = 1 << 30
	appcmdMask   = 0xF000
	wheelPerStep = 120
)

// APPCOMMAND_* → mpv 的媒体键名（input.conf 绑的是 NEXT / PREV）。
var appCommands = map[uint32]string{
	11: "NEXT", 12: "PREV", 13: "STOP", 14: "PLAYPAUSE", 46: "PLAY", 47: "PAUSE",
	8: "MUTE", 9: "VOLUME_DOWN", 10: "VOLUME_UP",
}

// forwardState 只在界面线程上读写。
type forwardState struct {
	tracking  bool
	lastMove  time.Time
	hidden    bool
	timerOn   bool
	down      map[uint32]string // 按下时发出去的键名，松开时原样发 keyup
	unmatched bool              // 上一次按下没有对应的键名：接下来的 WM_CHAR 用字符补
}

var fwd = forwardState{down: map[uint32]string{}}

// forwardInput 把画面层收到的一条输入消息转给 mpv；返回是否处理了。
func (a *app) forwardInput(hwnd uintptr, m uint32, w, l uintptr) (uintptr, bool) {
	s := a.session
	if s == nil {
		return 0, false
	}
	c := s.ipc.Load()
	if c == nil {
		return 0, false
	}
	switch m {
	case wmMouseMove:
		if !fwd.tracking {
			tme := trackMouseEvent{CbSize: uint32(unsafe.Sizeof(trackMouseEvent{})), DwFlags: tmeLeave, HwndTrack: hwnd}
			fwd.tracking = boolCall(pTrackMouseEvent, uintptr(unsafe.Pointer(&tme)))
		}
		a.noteMouse(hwnd)
		c.send("mouse", loword(l), hiword(l))
		return 0, true
	case wmMouseLeave:
		fwd.tracking = false
		c.send("mouse", -1, -1)
		return 0, true
	case wmLButtonDown, wmRButtonDown, wmMButtonDown, wmXButtonDown:
		pSetCapture.Call(hwnd)
		pSetFocus.Call(hwnd)
		c.send("keydown", mouseButton(m, w))
		return 0, true
	case wmLButtonUp, wmRButtonUp, wmMButtonUp, wmXButtonUp:
		pReleaseCapture.Call()
		c.send("keyup", mouseButton(m, w))
		return 0, true
	case wmMouseWheel, wmMouseHWheel:
		delta := hiword(w)
		name := map[bool]string{true: "WHEEL_UP", false: "WHEEL_DOWN"}[delta > 0]
		if m == wmMouseHWheel {
			name = map[bool]string{true: "WHEEL_RIGHT", false: "WHEEL_LEFT"}[delta > 0]
		}
		for n := abs32(delta) / wheelPerStep; n > 0; n-- {
			c.send("keypress", name)
		}
		return 0, true
	case wmKeyDown, wmSysKeyDown:
		return a.keyDown(c, m, w, l)
	case wmKeyUp, wmSysKeyUp:
		if name, ok := fwd.down[uint32(w)]; ok {
			delete(fwd.down, uint32(w))
			c.send("keyup", name)
			return 0, true
		}
	case wmChar:
		if fwd.unmatched && w >= 0x20 {
			fwd.unmatched = false
			c.send("keypress", string(rune(w)))
			return 0, true
		}
	case wmAppCommand:
		if name, ok := appCommands[uint32(hiword(l))&^appcmdMask]; ok {
			c.send("keypress", name)
			return 1, true
		}
	case wmTimer:
		if w == cursorTimer {
			a.cursorTick(hwnd)
			return 0, true
		}
	case wmSetCursor:
		if fwd.hidden && loword(l) == htClient {
			pSetCursor.Call(0)
			return 1, true
		}
	case wmKillFocusMsg:
		fwd.down = map[uint32]string{}
		c.send("keyup", "") // 全部松开（必须带空串参数）
	}
	return 0, false
}

const wmKillFocusMsg = 0x0008

func (a *app) keyDown(c *hostIPC, m uint32, w, l uintptr) (uintptr, bool) {
	vk := uint32(w)
	if m == wmSysKeyDown && vk == vkF1+3 { // Alt+F4：交给系统关窗口
		return 0, false
	}
	if l&repeatBit != 0 {
		_, ours := fwd.down[vk]
		return 0, ours // 按住的重复：mpv 自己会重复
	}
	name, ok := mpvKeyName(vk, keyMods{Shift: keyDownNow(vkShift), Ctrl: keyDownNow(vkControl), Alt: keyDownNow(vkMenu)})
	if !ok {
		fwd.unmatched = vk != vkShift && vk != vkControl && vk != vkMenu
		return 0, false // 让 TranslateMessage 生成 WM_CHAR
	}
	fwd.unmatched = false
	fwd.down[vk] = name
	c.send("keydown", name)
	return 0, true
}

func mouseButton(m uint32, w uintptr) string {
	switch m {
	case wmLButtonDown, wmLButtonUp:
		return "MBTN_LEFT"
	case wmRButtonDown, wmRButtonUp:
		return "MBTN_RIGHT"
	case wmMButtonDown, wmMButtonUp:
		return "MBTN_MID"
	}
	if hiword(w) == 1 {
		return "MBTN_BACK"
	}
	return "MBTN_FORWARD"
}

// noteMouse：鼠标动了，光标露出来；第一次时起一个定时器检查闲置。
func (a *app) noteMouse(hwnd uintptr) {
	fwd.lastMove = time.Now()
	fwd.hidden = false
	if !fwd.timerOn {
		fwd.timerOn = boolCall(pSetTimer, hwnd, cursorTimer, cursorPoll, 0)
	}
}

func (a *app) cursorTick(hwnd uintptr) {
	if fwd.hidden || time.Since(fwd.lastMove) < cursorIdle || a.mode != modeVideo {
		return
	}
	fwd.hidden = true
	pSetCursor.Call(0)
}

func keyDownNow(vk uint32) bool {
	r, _, _ := pGetKeyState.Call(uintptr(vk))
	return int16(r) < 0
}

func abs32(v int32) int32 {
	if v < 0 {
		return -v
	}
	return v
}

var pSetTimer = user32.NewProc("SetTimer")
