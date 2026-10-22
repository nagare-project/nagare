//go:build windows

package desktop

// Windows 的桌面窗口：一个顶层窗口两个子窗口 —— 网页（WebView2，从 127.0.0.1 载入）与画面（黑底，外部
// mpv.exe --wid 把自己的窗口挂进来）。播放时藏网页、露画面；mpv 的会话结束换回网页。
//
//   - 线程：窗口、WebView2、COM 全在主线程上（托盘用 systray.Register，消息循环是这里的 run），别的 goroutine
//     经 uiQueue 把活排过来（loop_windows.go）。
//   - mpv 仍是独立进程：internal/mpv 的启动、IPC、OnSessionEnd 原样用，只多带 --wid（LaunchOptions.ParentWindow）。
//     mpv 在 --wid 下把自己的窗口建成 disabled（w32_common.c 的 EnableWindow(w32->window, 0)），宿主重新启用它
//     并给焦点，鼠标键盘才进得去 Lua 界面（video_windows.go）；退路是 NAGARE_DESKTOP_INPUT=ipc：不启用，宿主经
//     IPC 转发输入（input_windows.go）。
//   - 有父窗口时 mpv 不管全屏 / 最大化 / 最小化 / 置顶：宿主经自己的一条 IPC 连接观察这几个属性、落到顶层窗口上
//     （winstate.go 是规则，window_windows.go 执行）。
//   - WebView2 起不来（没有运行时、用户数据目录坏了、浏览器进程反复崩）就退回浏览器模式：界面在默认浏览器里，
//     mpv 像以前一样自己开窗口；原因写进日志与设置页（State）。

import (
	"context"
	"log"
	"os"
	"sync"
	"time"

	"github.com/nagare-project/nagare/internal/mpv"
)

const (
	// callTimeout 是 Go 这边等界面线程做完一件事（建窗口、换到画面层）的上限。
	callTimeout = 10 * time.Second
	// quitPumpLimit 是退出时等收尾（停播放、进度回写、关磁力引擎）的上限；这期间界面线程照常处理消息 ——
	// mpv 的子窗口挂在我们的窗口下，它退出时要和我们的线程打交道，不处理消息它就关不掉。
	quitPumpLimit = 20 * time.Second
	// browserExitLimit 是退出前等 WebView2 浏览器进程退出的上限：用户数据目录同时只能有一个会话，自更新之后
	// 马上起来的新版本要用同一个目录。
	browserExitLimit = 5 * time.Second
	// endSessionLimit 是注销 / 关机时等收尾的上限（Windows 大约给 5 秒，再久就显示「此应用阻止关机」）。
	endSessionLimit = 5 * time.Second
)

// ipcInput：NAGARE_DESKTOP_INPUT=ipc 时不重新启用 mpv 的子窗口，输入由宿主经 IPC 转发（退路，见 input_windows.go）。
var ipcInput = os.Getenv("NAGARE_DESKTOP_INPUT") == "ipc"

// testHooks 只由测试构建（-tags desktoptest）的遥控设置：模拟「没有运行时」与「WebView2 起不来」两种退路。
var testHooks struct {
	noRuntime bool
	failWeb   bool
}

// ui 是唯一的桌面窗口。
var ui = &app{}

// Available 报告这个构建里有没有桌面窗口：Windows 构建恒为真。这台机器上能不能用看 Ready。
func Available() bool { return true }

// InProcess 报告 mpv 是不是跑在 nagare 进程里：Windows 用的是外部 mpv.exe（要可执行文件，thumbfast 照常）。
func InProcess() bool { return false }

// Init 记下界面地址与选项，不建窗口（窗口在消息循环起来后、第一次显示时才建）。
func Init(o Options) {
	ui.mu.Lock()
	ui.opts = o
	ui.info = Info{Active: true}
	ui.mu.Unlock()
}

// Show 把窗口提到前面；第一次调用时建网页层。窗口用不了时改在浏览器里打开界面。
func Show() { ui.request("") }

// Open 在窗口的网页层载入 url 并把窗口提到前面；窗口用不了时在浏览器里打开 url。
func Open(url string) { ui.request(url) }

// State 是窗口此刻的状态（任意 goroutine 可调）。
func State() Info {
	ui.mu.Lock()
	defer ui.mu.Unlock()
	return ui.info
}

// MainLoop 返回托盘要跑的消息循环（托盘用 systray.Register 而不是 systray.Run，消息由这里处理）：
// 收到 WM_QUIT（托盘退出、ctx 取消）之后接着处理消息，直到 done 关闭（主进程收尾完成）或超时，再关 WebView2、
// 等浏览器进程退出、拆窗口，然后返回。
func MainLoop(done <-chan struct{}) func() {
	return func() { ui.run(done) }
}

// BeforeHandoff 在「第二次启动」的那个新进程里、请求已在运行的那一份显示窗口之前调：新进程是用户刚启动的、
// 有资格抢前台，把这个资格让出去（AllowSetForegroundWindow），老进程的 SetForegroundWindow 才不会被系统拦下
// （拦下时只是任务栏按钮闪一下）。
func BeforeHandoff() {
	if r, _, err := pAllowSetForegroundWindow.Call(asfwAny); r == 0 {
		log.Printf("desktop: 让出前台失败（窗口可能只在任务栏上闪）：%v", err)
	}
}

// Launch 是交给 player.Options.Launch 的启动器：在桌面窗口里起 mpv（--wid=画面层）。窗口用不了（网页层起不来、
// 界面线程没响应）时照以前单独开 mpv 窗口，播放本身不受影响。ctx 只约束起播阶段。
func Launch(ctx context.Context, opts mpv.LaunchOptions) (*mpv.Player, error) {
	if err := mpv.CheckMedia(opts.MediaPath); err != nil {
		return nil, err
	}
	var prep sessionPrep
	if err := ui.q.call(callTimeout, func() { prep = ui.beginSession() }); err != nil {
		log.Printf("desktop: 窗口没响应，这次用单独的 mpv 窗口播：%v", err)
		return mpv.Launch(ctx, opts)
	}
	if !prep.ok {
		return mpv.Launch(ctx, opts)
	}
	opts.ParentWindow = uint32(prep.host)
	p, err := mpv.Launch(ctx, opts)
	if err != nil {
		ui.q.post(func() { ui.endSession(prep.id) })
		return nil, err
	}
	s := newVideoSession(prep.id, p, prep.host)
	ui.q.post(func() { ui.attachSession(s) })
	go s.run()
	return p, nil
}

// app 是窗口的全部状态。mu 只管 opts 与 info（任意 goroutine 会读）；其余字段只在界面线程上读写。
type app struct {
	q uiQueue

	mu   sync.Mutex
	opts Options
	info Info

	threadID                uint32
	dispatch                uintptr // 只收消息的窗口：uiQueue 的投递、等待的唤醒
	top, webHost, videoHost uintptr
	done                    <-chan struct{}
	shownOnce               bool // 第一次显示时按记下的位置决定要不要最大化
	startMaximized          bool
	quitting                bool
	endingSession           bool

	web       *webLayer
	webFailed string // 非空：网页层起不来（原因）
	webTries  int
	pending   string // 网页层还没建好时要载入的地址

	mode      viewMode
	fs        fullscreenState
	topmost   bool
	sessionID uint64 // 当前会话（0 = 没有）；会话的消息带着自己的号，对不上就是过期的
	session   *videoSession
	seq       uint64
}

type viewMode int

const (
	modeWeb viewMode = iota
	modeVideo
)

func (a *app) options() Options {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.opts
}

func (a *app) setInfo(i Info) {
	a.mu.Lock()
	a.info = i
	a.mu.Unlock()
}

// request 是 Show / Open 的共同入口：窗口那边用不了（消息循环没起来、已经拆了）就直接开浏览器。
func (a *app) request(url string) {
	if a.q.post(func() { a.show(url) }) {
		return
	}
	a.fallback(url, "桌面窗口已经关闭")
}

// fallback 在浏览器里打开 url（空 = 首个地址）。不在界面线程上等：开浏览器要起一个进程。
func (a *app) fallback(url, why string) {
	o := a.options()
	if url == "" {
		url = o.URL
	}
	if o.OpenBrowser == nil || url == "" {
		log.Printf("desktop: 窗口用不了（%s），也没有可开的浏览器地址", why)
		return
	}
	go o.OpenBrowser(url)
}
