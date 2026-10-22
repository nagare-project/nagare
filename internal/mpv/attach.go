package mpv

import (
	"context"
	"fmt"
	"net"
	"os"
)

// 进程内 libmpv（macOS 桌面窗口）用的三件事：它不经 Launch 起子进程，但 IPC、媒体预检、
// Player 的协议层都和命令行 mpv 是同一套。

// NewIPCEndpoint 在 dir 下生成一个唯一的 IPC 端点（unix socket 路径 / Windows 命名管道），
// 与 Launch 用的同一套规则（权限、长度回落见 ipcEndpoint）。dir 为空用系统临时目录。
func NewIPCEndpoint(dir string) (string, error) {
	if dir == "" {
		dir = os.TempDir()
	}
	return ipcEndpoint(dir)
}

// CheckMedia 是起播前的存在性预检（Launch 也用它）：本地路径必须在，HTTP(S) 地址不查
// （磁力边下边播传的是本机流端点，对它 os.Stat 必然失败）。
func CheckMedia(path string) error {
	if path == "" || isRemoteURL(path) {
		return nil
	}
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("找不到要播放的文件 %q：%w。请确认文件仍在原位置", path, err)
	}
	return nil
}

// AttachEndpoint 连上一个已经在跑、自己开了 IPC 端点的 mpv（进程内 libmpv 的 input-ipc-server），
// 返回 Player。端点是 mpv 初始化之后异步建好的，这里按 Launch 同样的节奏重试，ctx 约束等待。
//
// 这个 Player 不管进程（没有子进程可管）：Close 发 quit；mpv 的 IPC 从不把 shutdown 发给客户端，
// 所以报过 end-file 之后的干净断开就判为会话正常结束（见 Player.inProcess）—— 与命令行下用户关掉
// mpv 窗口落到同一个终态。initialPath 同 Launch 的 MediaPath。
func AttachEndpoint(ctx context.Context, endpoint, initialPath string) (*Player, error) {
	conn, err := connectWithRetry(ctx, endpoint, nil, nil)
	if err != nil {
		return nil, err
	}
	return attachInProcess(conn, endpoint, initialPath)
}

// DialEndpoint 另开一条连到 mpv IPC 端点的连接（mpv 的命名管道 / unix socket 都支持多个客户端）：Windows 的
// 桌面窗口用它观察窗口状态（全屏、置顶……），不经 Player 那条由 internal/player 消费的连接。端点还没建好时按
// Launch 同样的节奏重试，ctx 约束等待。
func DialEndpoint(ctx context.Context, endpoint string) (net.Conn, error) {
	return connectWithRetry(ctx, endpoint, nil, nil)
}

// attachInProcess 是 AttachEndpoint 连上之后的部分（测试经假 mpv 的连接直接调它）。
func attachInProcess(conn net.Conn, endpoint, initialPath string) (*Player, error) {
	p, err := newPlayer(conn, nil, nil, endpoint, initialPath, nil)
	if err != nil {
		return nil, err
	}
	p.inProcess.Store(true)
	return p, nil
}
