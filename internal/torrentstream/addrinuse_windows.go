//go:build windows

package torrentstream

import (
	"errors"

	"golang.org/x/sys/windows"
)

// isAddrInUse 判断 err 是否是「监听地址已被占用」。
//
// Windows 上 syscall.EADDRINUSE 是 Go 自造的占位值，bind 真正返回的是
// WSAEADDRINUSE（10048），用前者比对永远不会命中。
func isAddrInUse(err error) bool {
	return errors.Is(err, windows.WSAEADDRINUSE)
}
