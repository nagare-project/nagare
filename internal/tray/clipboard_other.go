//go:build !windows

package tray

import "os/exec"

// hideConsole 只在 Windows 上有事可做。
func hideConsole(*exec.Cmd) {}
