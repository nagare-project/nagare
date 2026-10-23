package desktop

import "fmt"

// IPC 转发输入（Windows 的退路，见 input_windows.go）用的键名：Windows 的虚拟键码 → mpv 的键名
// （input.conf 里写的那种：ESC、LEFT、Shift+N、Ctrl+a）。只在「重新启用 mpv 子窗口」那条路出问题、
// 用户设了 NAGARE_DESKTOP_INPUT=ipc 时用；正常情况下按键由 mpv 自己的窗口直接收。

// Windows 虚拟键码（WinUser.h）。
const (
	vkBack     = 0x08
	vkTab      = 0x09
	vkReturn   = 0x0D
	vkShift    = 0x10
	vkControl  = 0x11
	vkMenu     = 0x12 // Alt
	vkPause    = 0x13
	vkEscape   = 0x1B
	vkSpace    = 0x20
	vkPrior    = 0x21 // PgUp
	vkNext     = 0x22 // PgDn
	vkEnd      = 0x23
	vkHome     = 0x24
	vkLeft     = 0x25
	vkUp       = 0x26
	vkRight    = 0x27
	vkDown     = 0x28
	vkSnapshot = 0x2C
	vkInsert   = 0x2D
	vkDelete   = 0x2E
	vkF1       = 0x70
	vkF24      = 0x87
	vkOEM1     = 0xBA // ;:
	vkOEMPlus  = 0xBB // =+
	vkOEMComma = 0xBC // ,<
	vkOEMMinus = 0xBD // -_
	vkOEMDot   = 0xBE // .>
	vkOEM2     = 0xBF // /?
	vkOEM3     = 0xC0 // `~
	vkOEM4     = 0xDB // [{
	vkOEM5     = 0xDC // \|
	vkOEM6     = 0xDD // ]}
	vkOEM7     = 0xDE // '"
)

var namedKeys = map[uint32]string{
	vkBack: "BS", vkTab: "TAB", vkReturn: "ENTER", vkPause: "PAUSE", vkEscape: "ESC", vkSpace: "SPACE",
	vkPrior: "PGUP", vkNext: "PGDWN", vkEnd: "END", vkHome: "HOME",
	vkLeft: "LEFT", vkUp: "UP", vkRight: "RIGHT", vkDown: "DOWN",
	vkSnapshot: "PRINT", vkInsert: "INS", vkDelete: "DEL",
}

// 美式键盘上的符号键：不按 / 按着 Shift。别的布局的符号由 WM_CHAR 那条路补（见 input_windows.go）。
var symbolKeys = map[uint32][2]string{
	vkOEM1: {";", ":"}, vkOEMPlus: {"=", "+"}, vkOEMComma: {",", "<"}, vkOEMMinus: {"-", "_"},
	vkOEMDot: {".", ">"}, vkOEM2: {"/", "?"}, vkOEM3: {"`", "~"}, vkOEM4: {"[", "{"},
	vkOEM5: {"\\", "|"}, vkOEM6: {"]", "}"}, vkOEM7: {"'", "\""},
}

// keyMods 是按键时按着的修饰键。
type keyMods struct{ Shift, Ctrl, Alt bool }

// mpvKeyName 把一个虚拟键翻成 mpv 的键名；修饰键本身、不认识的键返回假。
//
// mpv 的约定：字母与符号按 Shift 之后的字符写（Shift+n 是 "N"、Shift+[ 是 "{"），不再带 Shift+ 前缀；
// 有名字的键（ESC、LEFT……）与 F 键照常带 Shift+。Ctrl+ / Alt+ 一律作前缀。
func mpvKeyName(vk uint32, m keyMods) (string, bool) {
	base, shiftApplied := "", false
	switch {
	case vk >= 'A' && vk <= 'Z':
		base = string(rune(vk + ('a' - 'A')))
		if m.Shift {
			base, shiftApplied = string(rune(vk)), true
		}
	case vk >= '0' && vk <= '9':
		if m.Shift {
			return "", false // Shift+数字是符号（!@#…），布局不同字符不同，交给 WM_CHAR
		}
		base = string(rune(vk))
	case vk >= vkF1 && vk <= vkF24:
		base = fmt.Sprintf("F%d", vk-vkF1+1)
	default:
		if name, ok := namedKeys[vk]; ok {
			base = name
		} else if sym, ok := symbolKeys[vk]; ok {
			base = sym[0]
			if m.Shift {
				base, shiftApplied = sym[1], true
			}
		} else {
			return "", false
		}
	}
	prefix := ""
	if m.Shift && !shiftApplied {
		prefix += "Shift+"
	}
	if m.Ctrl {
		prefix = "Ctrl+" + prefix
	}
	if m.Alt {
		prefix = "Alt+" + prefix
	}
	return prefix + base, true
}
