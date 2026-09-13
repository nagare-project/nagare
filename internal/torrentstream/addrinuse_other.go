//go:build !windows

package torrentstream

import (
	"errors"
	"syscall"
)

// isAddrInUse 判断 err 是否是「监听地址已被占用」。
func isAddrInUse(err error) bool {
	return errors.Is(err, syscall.EADDRINUSE)
}
