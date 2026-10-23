//go:build !windows

package mpv

import "os"

// bindToNagare 在 Windows 以外什么都不做：mpv 是 nagare 的子进程，nagare 退出时由它自己收掉；被硬杀时 mpv 的 IPC
// 断开、播完或关窗即退出。
func bindToNagare(*os.Process) {}
