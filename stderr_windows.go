//go:build windows

package main

import (
	"log"
	"os"

	"golang.org/x/sys/windows"
)

// panicsToLog 让 Go 运行时的崩溃信息（panic 的堆栈、fatal error）也写进日志文件。
//
// -H=windowsgui 的程序双击启动时没有控制台，标准错误是无效句柄：运行时写崩溃信息时每次都现取 STD_ERROR_HANDLE，
// 写不出去就整个丢了 —— 用户只看到 nagare 消失，日志里什么都没有。把标准错误指到日志文件上（只在它本来无效时；
// 从终端启动、或者被重定向到文件时照旧）。
func panicsToLog(f *os.File) {
	h, err := windows.GetStdHandle(windows.STD_ERROR_HANDLE)
	if err == nil && h != 0 && h != windows.InvalidHandle {
		if t, terr := windows.GetFileType(h); terr == nil && t != windows.FILE_TYPE_UNKNOWN {
			return
		}
	}
	if err := windows.SetStdHandle(windows.STD_ERROR_HANDLE, windows.Handle(f.Fd())); err != nil {
		log.Printf("把标准错误指到日志文件失败（崩溃信息会丢）：%v", err)
	}
}
