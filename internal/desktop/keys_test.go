package desktop

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// 虚拟键 → mpv 键名（IPC 转发输入那条退路用）。写法要与 internal/mpv/ui/input.conf 里绑的一致：
// Shift+N 是 "N"、Esc 是 "ESC"、PgDn 是 "PGDWN"。
func TestMpvKeyName(t *testing.T) {
	for _, tc := range []struct {
		vk   uint32
		mods keyMods
		want string
		ok   bool
	}{
		{'N', keyMods{}, "n", true},
		{'N', keyMods{Shift: true}, "N", true},
		{'A', keyMods{Ctrl: true}, "Ctrl+a", true},
		{'A', keyMods{Ctrl: true, Shift: true}, "Ctrl+A", true},
		{'5', keyMods{}, "5", true},
		{'1', keyMods{Shift: true}, "", false},
		{vkEscape, keyMods{}, "ESC", true},
		{vkSpace, keyMods{}, "SPACE", true},
		{vkReturn, keyMods{}, "ENTER", true},
		{vkNext, keyMods{}, "PGDWN", true},
		{vkPrior, keyMods{}, "PGUP", true},
		{vkLeft, keyMods{Shift: true}, "Shift+LEFT", true},
		{vkF1 + 3, keyMods{Alt: true}, "Alt+F4", true},
		{vkOEM4, keyMods{}, "[", true},
		{vkOEM4, keyMods{Shift: true}, "{", true},
		{vkTab, keyMods{}, "TAB", true},
		{vkBack, keyMods{}, "BS", true},
		{vkShift, keyMods{Shift: true}, "", false},
		{vkControl, keyMods{Ctrl: true}, "", false},
		{0xFF, keyMods{}, "", false},
	} {
		got, ok := mpvKeyName(tc.vk, tc.mods)
		assert.Equal(t, tc.ok, ok, "vk %#x %+v", tc.vk, tc.mods)
		assert.Equal(t, tc.want, got, "vk %#x %+v", tc.vk, tc.mods)
	}
}
