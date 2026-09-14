package tray

import (
	"os"
	"strings"
)

// inAppBundle 判断当前进程是不是从 macOS 的 .app 里启动的。
// 决定两件事：要不要升成 Dock 应用（裸二进制没有图标资源），以及通知中心认不认。
func inAppBundle() bool {
	exe, err := os.Executable()
	if err != nil {
		return false
	}
	return isBundlePath(exe)
}

// isBundlePath 只看路径形状：<X>.app/Contents/MacOS/<可执行文件>。
func isBundlePath(exe string) bool {
	return strings.Contains(exe, ".app/Contents/MacOS/")
}
