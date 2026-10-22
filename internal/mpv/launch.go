package mpv

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"sort"
	"strings"
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
	// HTTPHeaders 只用于当前远程媒体会话，不写入配置文件或日志。
	HTTPHeaders map[string]string
	// RedactMediaDiagnostics 禁止捕获 mpv stderr，避免签名 URL 或临时请求头
	// 被 mpv 原样回显后进入 API 错误或日志。
	RedactMediaDiagnostics bool
	// NetworkStream 表示 MediaPath 是一条边下边播 / 在线的 HTTP 流，而不是本地文件：
	// 给 mpv 加上流式播放的缓存参数（见 streamCacheOptions）。
	NetworkStream bool
	// Shaders 是按顺序加载的 GLSL 着色器（Anime4K，见 Anime4KShaders）。逐个用 --glsl-shaders-append 传：
	// 整条列表要按系统用 : 或 ; 分隔，路径里带这两个字符就会被拆错。
	Shaders []string
	// ConfigDir 是 nagare 自己的 mpv 配置目录（见 PrepareUIConfig）；留空则 mpv 照常读用户自己的配置。
	ConfigDir string
	// ScriptOpts 是交给界面脚本的 key=value（比如 thumbfast-mpv_path），逐条用 --script-opts-append 传：
	// 合并成一条 --script-opts 时值里的逗号会被拆开，路径里完全可能有逗号。
	ScriptOpts []string
	// NoConfig：不读任何配置文件（--no-config），用在 nagare 的界面配置没准备好时 ——
	// 退回 mpv 自带的控制条，但也不去读用户自己的配置（那会把自动接下一集、自己续播的冲突带回来）。
	NoConfig bool
	// Volume 是起播音量（nil = mpv 默认）；界面里调过的音量由 nagare 记住，下次起播带回来。
	Volume *int
}

// streamCacheOptions 是网络流专用的 mpv 缓存参数。
//
// mpv 打开 HTTP 流的默认行为是「只预读 1 秒、不等缓冲直接起播」，对磁力这种
// 供给速度先慢后快的源正好踩坑：本地已经到齐的起播缓冲一读完就追上下载前沿，
// 头几秒必然一顿一顿。真机实测（2026-09-14）用户报的「刚打开卡顿」就是这条。
//
//	--cache=yes                 强制开缓存（对 http 默认就是 auto=yes，写明防以后改默认）
//	--demuxer-readahead-secs=60 目标预读 60 秒：让 mpv 主动把读位置推到下载前沿之前，
//	                            种子那边的优先级窗口正是跟着这个读位置走的
//	--demuxer-max-bytes=256MiB  预读上限，别为 60 秒把 4K 片的内存吃穿
//	--cache-pause-initial=yes   起播前先攒够 cache-pause-wait 秒再放，攒不够时
//	                            mpv 自己显示「缓冲中」，而不是放两秒停一秒
//	--cache-pause-wait=5        起播 / 断流恢复都以 5 秒内容为门槛
var streamCacheOptions = []Option{
	stringOption("cache", "yes"),
	stringOption("demuxer-readahead-secs", "60"),
	stringOption("demuxer-max-bytes", "256MiB"),
	stringOption("cache-pause-initial", "yes"),
	stringOption("cache-pause-wait", "5"),
}

// Launch 启动 mpv 进程并建立 IPC 连接。ctx 只约束启动阶段（等 socket 就绪），
// 不绑定进程生命周期 —— 进程的退出由 Close 或用户操作决定。
func Launch(ctx context.Context, opts LaunchOptions) (*Player, error) {
	if opts.MPVPath == "" {
		return nil, fmt.Errorf("缺少 mpv 路径：请先调用 Detect 探测 mpv 安装")
	}
	// 存在性预检只对本地路径有意义：磁力边下边播传进来的是本机流端点
	// （http://127.0.0.1:<port>/stream/...），对它 os.Stat 必然失败。
	if opts.MediaPath != "" && !isRemoteURL(opts.MediaPath) {
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

	var stderrTail *boundedBuffer
	cmd := exec.Command(opts.MPVPath, buildArgs(opts, endpoint)...)
	cmd.Stdout = io.Discard
	if opts.RedactMediaDiagnostics {
		cmd.Stderr = io.Discard
	} else {
		stderrTail = newBoundedBuffer(stderrTailSize)
		cmd.Stderr = stderrTail
	}
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

// isRemoteURL 判断媒体路径是不是 HTTP(S) 地址。只认这两个 scheme：
// 其余（file://、裸路径）都当本地路径走存在性预检，宁可多检查也不放过
// 「文件被移走」这个最常见的失效方式。
func isRemoteURL(p string) bool {
	lower := strings.ToLower(p)
	return strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://")
}

// buildArgs 组装命令行 mpv 的启动参数。
func buildArgs(opts LaunchOptions, endpoint string) []string {
	return NewLaunchSpec(opts, endpoint).Argv()
}

// NewLaunchSpec 把 LaunchOptions 翻成选项清单（见 LaunchSpec）。endpoint 是 IPC 的 socket / 命名管道。
// 顺序就是 argv 的顺序，改了会让命令行 mpv 的启动参数变样（options_golden_test.go 会拦下）。
func NewLaunchSpec(opts LaunchOptions, endpoint string) LaunchSpec {
	o := []Option{
		stringOption("input-ipc-server", endpoint),
		flagOption("terminal", false),
		flagOption("force-window", true).afterRenderContext(),
		stringOption("keep-open", "no"),
	}
	if opts.ConfigDir != "" {
		o = append(o, stringOption("config-dir", opts.ConfigDir))
	} else if opts.NoConfig {
		o = append(o, flagOption("config", false))
	}
	if len(opts.ScriptOpts) > 0 {
		o = append(o, mapOption("script-opts", "script-opts-append", opts.ScriptOpts))
	}
	if opts.Volume != nil {
		o = append(o, stringOption("volume", fmt.Sprintf("%d", *opts.Volume)))
	}
	if opts.NetworkStream {
		o = append(o, streamCacheOptions...)
	}
	if opts.Title != "" {
		o = append(o, stringOption("title", opts.Title).perFile())
		// 窗口标题之外，界面顶栏读的是 media-title：不强制的话那里显示的是文件名
		o = append(o, stringOption("force-media-title", opts.Title).perFile())
	}
	if opts.SubPath != "" {
		o = append(o, listOption("sub-files", "sub-file", []string{opts.SubPath}).perFile())
	}
	if opts.StartAt > 0 {
		o = append(o, stringOption("start", fmt.Sprintf("+%.3f", opts.StartAt)).perFile())
	}
	if len(opts.Shaders) > 0 {
		o = append(o, listOption("glsl-shaders", "glsl-shaders-append", opts.Shaders))
	}
	if len(opts.HTTPHeaders) > 0 {
		names := make([]string, 0, len(opts.HTTPHeaders))
		for name := range opts.HTTPHeaders {
			names = append(names, name)
		}
		sort.Strings(names)
		headers := make([]string, 0, len(names))
		for _, name := range names {
			headers = append(headers, name+": "+opts.HTTPHeaders[name])
		}
		// 逐条用 -append（一次追加一项、不解析）：-add 按 -set 的语法在逗号处拆项，「(KHTML, like Gecko)」
		// 那种 User-Agent 到对端会变成两行（mpv 0.41 实测，第二行以空格开头被当成续行折回去，逗号丢了）
		o = append(o, listOption("http-header-fields", "http-header-fields-append", headers).perFile())
	}
	return LaunchSpec{Options: o, Media: opts.MediaPath}
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
			if stderrTail != nil {
				if tail := stderrTail.String(); tail != "" {
					msg += fmt.Sprintf("，stderr：%s", tail)
				}
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
