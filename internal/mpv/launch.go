package mpv

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"sync"
	"time"
)

const (
	// launchWaitTimeout：mpv 的 IPC socket 是启动后异步创建的，
	// 轮询连接的总等待上限（与调用方 ctx 取更早者）。
	launchWaitTimeout = 15 * time.Second
	// launchPollInterval 是连接轮询间隔。
	launchPollInterval = 100 * time.Millisecond
	// stderrTailSize 是保留的 mpv stderr 字节数（诊断用）。
	stderrTailSize = 4096
)

// LaunchOptions 是 Launch 的启动参数。
type LaunchOptions struct {
	MPVPath   string  // mpv 可执行文件路径（用 Detect 的结果）
	MediaPath string  // 要播放的文件；留空则以 --idle=yes 启动（集成测试/预热用）
	SubPath   string  // 可选：外挂字幕（弹幕 ASS），--sub-file
	Title     string  // 可选：窗口标题
	StartAt   float64 // 可选：起播秒数（>0 生效），--start=+N
	SocketDir string  // socket/管道所在目录（调用方给运行时目录；留空用系统临时目录）
}

// Launch 启动 mpv 进程并建立 IPC 连接。ctx 只约束启动阶段（等 socket 就绪），
// 不绑定进程生命周期 —— 进程的退出由 Close 或用户操作决定。
func Launch(ctx context.Context, opts LaunchOptions) (*Player, error) {
	if opts.MPVPath == "" {
		return nil, fmt.Errorf("缺少 mpv 路径：请先调用 Detect 探测 mpv 安装")
	}
	if opts.MediaPath != "" {
		if _, err := os.Stat(opts.MediaPath); err != nil {
			return nil, fmt.Errorf("找不到要播放的文件 %q：%w。请确认文件仍在原位置", opts.MediaPath, err)
		}
	}
	socketDir := opts.SocketDir
	if socketDir == "" {
		socketDir = os.TempDir()
	}
	endpoint, err := ipcEndpoint(socketDir) // Windows 在 M1 阶段于此返回明确错误
	if err != nil {
		return nil, err
	}

	stderrTail := newBoundedBuffer(stderrTailSize)
	cmd := exec.Command(opts.MPVPath, buildArgs(opts, endpoint)...)
	cmd.Stdout = io.Discard
	cmd.Stderr = stderrTail
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("启动 mpv（%s）失败：%w。请用 Detect 重新探测 mpv 安装", opts.MPVPath, err)
	}

	// 唯一的一次 cmd.Wait 放在这里：启动阶段用它发现「秒退」，
	// 启动成功后把通道交给 Player 的进程守望。
	waitCh := make(chan error, 1)
	go func() { waitCh <- cmd.Wait() }()

	conn, err := connectWithRetry(ctx, endpoint, waitCh, stderrTail)
	if err != nil {
		_ = cmd.Process.Kill()
		<-waitCh // 收尸，不留僵尸进程
		removeIPCEndpoint(endpoint)
		return nil, err
	}

	p, err := newPlayer(conn, cmd, waitCh, endpoint, opts.MediaPath, stderrTail)
	if err != nil {
		return nil, err // newPlayer 失败时已自行 Close 清理
	}
	return p, nil
}

// buildArgs 组装 mpv 启动参数。
func buildArgs(opts LaunchOptions, endpoint string) []string {
	args := []string{
		"--input-ipc-server=" + endpoint,
		"--no-terminal",
		"--force-window",
		"--keep-open=no",
	}
	if opts.Title != "" {
		args = append(args, "--title="+opts.Title)
	}
	if opts.SubPath != "" {
		args = append(args, "--sub-file="+opts.SubPath)
	}
	if opts.StartAt > 0 {
		args = append(args, fmt.Sprintf("--start=+%.3f", opts.StartAt))
	}
	if opts.MediaPath != "" {
		args = append(args, "--", opts.MediaPath) // "--" 防御以 "-" 开头的文件名
	} else {
		args = append(args, "--idle=yes")
	}
	return args
}

// connectWithRetry 轮询连接 mpv 的 IPC 端点，同时监听「进程秒退」与 ctx 取消。
func connectWithRetry(ctx context.Context, endpoint string, waitCh <-chan error, stderrTail *boundedBuffer) (net.Conn, error) {
	ctx, cancel := context.WithTimeout(ctx, launchWaitTimeout)
	defer cancel()

	for {
		conn, dialErr := dialIPC(endpoint)
		if dialErr == nil {
			return conn, nil
		}
		select {
		case werr := <-waitCh:
			msg := fmt.Sprintf("mpv 启动后立即退出（%v）", werr)
			if tail := stderrTail.String(); tail != "" {
				msg += fmt.Sprintf("，stderr：%s", tail)
			}
			return nil, fmt.Errorf("%s。请检查 mpv 安装与启动参数", msg)
		case <-ctx.Done():
			return nil, fmt.Errorf("等待 mpv IPC 就绪超时或被取消（%v，最近一次连接错误：%v）。mpv 可能启动异常，请重试或检查安装", ctx.Err(), dialErr)
		case <-time.After(launchPollInterval):
		}
	}
}

// boundedBuffer 是只保留前 max 字节的并发安全 Writer，用于捕获 stderr 摘要。
// 超出部分直接丢弃 —— 启动失败的关键信息（加载器/参数错误）都在开头。
type boundedBuffer struct {
	mu  sync.Mutex
	max int
	buf []byte
}

func newBoundedBuffer(max int) *boundedBuffer {
	return &boundedBuffer{max: max}
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	n := len(p)
	b.mu.Lock()
	defer b.mu.Unlock()
	if room := b.max - len(b.buf); room > 0 {
		if len(p) > room {
			p = p[:room]
		}
		b.buf = append(b.buf, p...)
	}
	return n, nil // 始终宣告写入成功，超出部分丢弃
}

func (b *boundedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return string(b.buf)
}
