//go:build windows

package tray

import (
	"os/exec"
	"syscall"
)

// createNoWindow 是 CREATE_NO_WINDOW：nagare 自己以 -H windowsgui 跑、没有控制台，
// 直接起 clip.exe 会闪一个黑窗。
const createNoWindow = 0x08000000

func hideConsole(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
}
