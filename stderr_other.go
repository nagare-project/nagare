//go:build !windows

package main

import "os"

// panicsToLog 在 Windows 以外什么都不做：双击启动的 .app 的标准错误照样有效（系统日志收着），终端启动就在终端里。
func panicsToLog(*os.File) {}
