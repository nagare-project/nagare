//go:build windows

package torrentstream

import "golang.org/x/sys/windows"

// testAddrInUse 是本平台 bind 撞端口时真正返回的错误码，注入给重试测试用。
var testAddrInUse error = windows.WSAEADDRINUSE
