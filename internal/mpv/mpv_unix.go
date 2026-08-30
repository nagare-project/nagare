//go:build unix

package mpv

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// darwin 的 sun_path 上限 104 字节（linux 108），留出余量提前给出可操作的错误，
// 而不是让 bind 失败时抛出一条看不懂的系统错误。
const maxSocketPathLen = 100

// ipcEndpoint 在 dir 下生成唯一的 unix socket 路径（含 pid + 128 位随机段）。
//
// dir 太深导致路径超限时回落到临时目录 —— 用户名很长的 macOS 账户或深层
// TempDir 会踩到 sun_path 上限。但绝不把 socket 直接丢进 /tmp（1777 世界可写）：
// 谁能连上这条 IPC 谁就能通过 mpv 的 run/subprocess 命令以当前用户执行任意程序。
// 回落时先建一个 0700 私有子目录，socket 放里面 —— 沿用 ssh-agent 的做法。
func ipcEndpoint(dir string) (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("生成 IPC socket 随机名失败：%w", err)
	}
	name := fmt.Sprintf("mpv-%d-%s.sock", os.Getpid(), hex.EncodeToString(buf))

	if path := filepath.Join(dir, name); len(path) <= maxSocketPathLen {
		return path, nil
	}
	// 回落：在临时根下开一个仅自己可进的私有目录，socket 放其中。
	priv, err := os.MkdirTemp(shortestTempRoot(), "nagare-mpv-")
	if err != nil {
		return "", fmt.Errorf("创建私有 socket 目录失败：%w", err)
	}
	path := filepath.Join(priv, name)
	if len(path) > maxSocketPathLen {
		return "", fmt.Errorf("IPC socket 路径过长（%d 字符，上限 %d）且临时目录也不可用", len(path), maxSocketPathLen)
	}
	return path, nil
}

// shortestTempRoot 选一个尽量短的临时根，给回落路径腾出预算。
func shortestTempRoot() string {
	root := os.TempDir()
	if len("/tmp") < len(root) {
		if info, err := os.Stat("/tmp"); err == nil && info.IsDir() {
			return "/tmp"
		}
	}
	return root
}

// dialIPC 连接 unix socket。socket 尚未创建时快速失败，由上层轮询重试。
func dialIPC(path string) (net.Conn, error) {
	return net.DialTimeout("unix", path, time.Second)
}

// removeIPCEndpoint 清理 socket 文件。mpv 正常退出会自删，这里兜底；
// 文件不存在属预期，其余错误也不值得让退出流程失败（下次启动用新的随机名）。
// 若 socket 位于回落时建的私有子目录（nagare-mpv-*），把空目录一并删掉。
func removeIPCEndpoint(path string) {
	_ = os.Remove(path)
	if parent := filepath.Dir(path); strings.HasPrefix(filepath.Base(parent), "nagare-mpv-") {
		_ = os.Remove(parent) // 仅在空目录时成功，非空不动
	}
}
