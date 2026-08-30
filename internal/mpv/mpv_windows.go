//go:build windows

package mpv

import (
	"errors"
	"net"
)

// errWindowsNotYet：Windows 上 mpv 的 IPC 走命名管道（\\.\pipe\...），
// 连接需要专门的管道拨号实现，按计划在打包里程碑（M4）随自带 mpv 一起接入。
// M1 先保证可交叉编译并给出明确指引。
var errWindowsNotYet = errors.New(
	"Windows 上的 mpv 播放尚未接入（命名管道 IPC 将在打包版本提供）；目前请在 macOS 或 Linux 上使用 nagare")

// ipcEndpoint 在 Windows 上应返回命名管道路径；M1 直接返回明确错误。
func ipcEndpoint(_ string) (string, error) {
	return "", errWindowsNotYet
}

// dialIPC 在 Windows 上需要 named pipe 客户端；M1 直接返回明确错误。
func dialIPC(_ string) (net.Conn, error) {
	return nil, errWindowsNotYet
}

// removeIPCEndpoint 命名管道无文件残留，无需清理。
func removeIPCEndpoint(_ string) {}
