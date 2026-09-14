//go:build windows

package mpv

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"time"

	winio "github.com/Microsoft/go-winio"
)

// Windows 上 mpv 的 `--input-ipc-server` 是命名管道（\\.\pipe\<name>），不是文件系统
// 里的 socket：没有路径长度上限、没有残留文件要清、也不受 SocketDir 影响。
// 客户端要 overlapped I/O 才能像 net.Conn 一样读写，用 go-winio 的 DialPipe
// （Docker / containerd 同款），不自己拼 CreateFile。
//
// 名字带 pid + 128 位随机段：同机多实例不撞名，也让别的进程猜不到（管道默认 DACL
// 给 Everyone 只读，写入需要同一用户；与 seanime 的做法一致）。

const pipePrefix = `\\.\pipe\nagare-mpv-`

// ipcEndpoint 生成唯一的命名管道名；dir 对管道无意义，忽略。
func ipcEndpoint(_ string) (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("生成 IPC 管道随机名失败：%w", err)
	}
	return fmt.Sprintf("%s%d-%s", pipePrefix, os.Getpid(), hex.EncodeToString(buf)), nil
}

// dialIPC 连接命名管道。管道尚未由 mpv 创建时快速失败，由上层轮询重试。
func dialIPC(path string) (net.Conn, error) {
	timeout := time.Second
	return winio.DialPipe(path, &timeout)
}

// removeIPCEndpoint 命名管道随最后一个句柄关闭而消失，没有东西要清。
func removeIPCEndpoint(_ string) {}
