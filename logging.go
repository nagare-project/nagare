package main

import "io"

// logOutput 是日志的去处：先写日志文件，再尽力写终端。
//
// 不能用 io.MultiWriter(os.Stderr, file)：它在第一个出错的 writer 处就停下。Windows 的 -H=windowsgui 程序双击启动时
// 没有控制台，os.Stderr 的句柄是无效的、每次写都报错 —— 那样日志文件一行都写不进去（CI 的 Windows 冒烟测试抓到的：
// 日志是空的）。日志文件是双击启动时唯一的诊断来源，它必须先写、写成功；终端写不了无所谓。
func logOutput(file, terminal io.Writer) io.Writer {
	return logTee{file: file, terminal: terminal}
}

type logTee struct {
	file, terminal io.Writer
}

func (t logTee) Write(p []byte) (int, error) {
	n, err := t.file.Write(p)
	_, _ = t.terminal.Write(p) // 没有控制台时必然失败，不影响日志文件
	return n, err
}
