//go:build !windows

package torrentstream

import "syscall"

// testAddrInUse 是本平台 bind 撞端口时真正返回的错误码，注入给重试测试用。
var testAddrInUse error = syscall.EADDRINUSE
