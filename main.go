// nagare（流れ）—— 本地动漫播放 agent 入口。
// 单二进制：Go 服务 + go:embed 内嵌的 React UI，浏览器即遥控器。
//
// 双击启动（macOS 的 .app / Windows 的 -H windowsgui exe）时没有终端：
// 日志落到配置目录下的文件，菜单栏 / 托盘图标是用户看见它、退出它的地方（M4 阶段 A）。
package main

import (
	"context"
	"embed"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"github.com/nagare-project/nagare/internal/animego"
	"github.com/nagare-project/nagare/internal/api"
	"github.com/nagare-project/nagare/internal/config"
	"github.com/nagare-project/nagare/internal/httpserver"
	"github.com/nagare-project/nagare/internal/logfile"
	"github.com/nagare-project/nagare/internal/mpv"
	"github.com/nagare-project/nagare/internal/player"
	"github.com/nagare-project/nagare/internal/rules"
	"github.com/nagare-project/nagare/internal/rulesync"
	"github.com/nagare-project/nagare/internal/store"
	"github.com/nagare-project/nagare/internal/tray"
	"github.com/nagare-project/nagare/internal/update"
)

// embeddedWeb 内嵌前端构建产物。all: 前缀确保带点号的文件（如 .gitkeep）也被收进来，
// 这样仓库里只有占位文件时依然能编译，运行时降级为 API-only 模式。
//
//go:embed all:web
var embeddedWeb embed.FS

// version 由 CI 通过 -ldflags "-X main.version=..." 注入，本地开发用默认值。
var version = "0.1.0-dev"

// shutdownGrace 是收到退出后等在途 HTTP 请求（如 /api/shutdown 自己的响应）完成的上限。
const shutdownGrace = 3 * time.Second

// flags 是命令行开关。
type flags struct {
	noBrowser bool // 启动时不自动开浏览器
	noTray    bool // headless：brew services / Linux 服务，不占主线程放托盘
}

// services 是主进程持有的全部业务对象。
type services struct {
	store   *store.Store
	mpv     *mpv.Runtime
	auth    api.AnimegoAuth
	player  *player.Manager
	lib     *api.LibraryService
	sources *api.SourcesService
}

// main 只负责解析开关与决定退出码；真正的启动逻辑在 start 里，
// 这样日志文件的 defer 一定跑得到 —— log.Fatal 会 os.Exit，直接跳过 defer。
func main() {
	var f flags
	flag.BoolVar(&f.noBrowser, "no-browser", false, "启动时不自动打开浏览器")
	flag.BoolVar(&f.noTray, "no-tray", false, "不显示菜单栏/托盘图标（headless：brew services、Linux 服务）")
	showVersion := flag.Bool("version", false, "打印版本号并退出")
	flag.Parse()
	if *showVersion {
		fmt.Println(version)
		return
	}
	if err := start(f); err != nil {
		log.Print(err)
		os.Exit(1)
	}
}

// start 走完整条启动路径：配置 → 日志文件 → 单实例判定 → 组装依赖 → 起服务。
func start(f flags) error {
	cfg, err := config.LoadOrInit()
	if err != nil {
		return fmt.Errorf("加载配置失败：%w", err)
	}
	configDir, err := config.Dir()
	if err != nil {
		return fmt.Errorf("定位配置目录失败：%w", err)
	}

	// 日志文件：双击启动没有控制台，这是唯一的诊断来源。打不开不致命，退回仅终端。
	if logF, err := logfile.Open(configDir); err != nil {
		log.Printf("打开日志文件失败（仅输出到终端）：%v", err)
	} else {
		defer logF.Close()
		log.SetOutput(io.MultiWriter(os.Stderr, logF))
	}
	log.Printf("nagare %s 启动（%s/%s）", version, runtime.GOOS, runtime.GOARCH)

	webFS, apiOnly := httpserver.SubWebFS(embeddedWeb)
	if apiOnly {
		log.Print("未找到内嵌 web 产物，进入 API-only 模式（先 cd frontend && bun run build 再重新编译）")
	}
	alreadyRunning, err := ensureSingleInstance(cfg, configDir, f.noBrowser || apiOnly)
	if err != nil {
		return err
	}
	if alreadyRunning {
		return nil
	}

	svc, err := buildServices(configDir)
	if err != nil {
		return err
	}
	return run(cfg, configDir, svc, webFS, f, apiOnly)
}

// 这三个变量是单实例判定的注入点：测试替换它们即可覆盖四种分支组合，
// 不需要真的监听端口或写标记文件。
var (
	hasInstance   = httpserver.HasInstance
	portOccupied  = httpserver.PortOccupied
	probePort     = httpserver.Probe
	openBrowserFn = openBrowser
)

// ensureSingleInstance 处理「双击两次」：已在运行 → 把浏览器指过去并返回 true（调用方退出）。
//
// 先用实例标记文件把门（它在 0700 的配置目录里，别的 OS 用户伪造不了），再做带 token 的
// 探测：没有标记就说明我方实例没起过，此时端口上的任何人都不是我们，直接按外来处理
// —— 绝不把 token 发给它。冒充者能照抄 /api/health 的公开信封，「响应对得上」证明不了身份。
func ensureSingleInstance(cfg *config.Config, configDir string, skipBrowser bool) (alreadyRunning bool, err error) {
	if !hasInstance(configDir) {
		if portOccupied(cfg.Port) {
			return false, rotateToken(cfg, "端口 %d 上有其他程序（本机没有我方实例在跑），已轮换鉴权 token")
		}
		return false, nil
	}
	switch probePort(cfg.Port, cfg.Token) {
	case httpserver.ProbeRunning:
		log.Printf("nagare 已在运行（端口 %d），本实例退出", cfg.Port)
		if !skipBrowser {
			openBrowserLogged(launchURL(cfg.Port, cfg.Token))
		}
		return true, nil
	case httpserver.ProbeForeign:
		// 探测已把 token 交给了对端（它没能证明自己是我们）—— 视为已泄露，立即轮换。
		return false, rotateToken(cfg, "端口 %d 被其他程序占用，已轮换鉴权 token")
	}
	return false, nil
}

// rotateToken 换一个新 token 并落盘；存不下去就没法安全启动（旧 token 可能已泄露
// 给端口上那个程序），返回错误让调用方终止。
func rotateToken(cfg *config.Config, format string) error {
	log.Printf(format, cfg.Port)
	cfg.Token = config.NewToken()
	if err := cfg.Save(); err != nil {
		return fmt.Errorf("保存轮换后的配置失败：%w", err)
	}
	return nil
}

// newAnimegoClient 还原已保存的会话，并返回一个把新会话落盘的回调。
//
// access token 15 分钟、refresh cookie 单次有效：任意一次鉴权调用都可能把两者
// 换成新的，不及时落盘就等于用户下次启动要重新登录。
func newAnimegoClient(st *store.Store) (*animego.Client, func()) {
	client := animego.New(animego.Options{UserAgent: "nagare/" + version})
	if sess := st.AnimegoSession(); sess.AccessToken != "" || sess.RefreshCookie != "" {
		client.RestoreSession(animego.Session{
			AccessToken:   sess.AccessToken,
			RefreshCookie: sess.RefreshCookie,
		})
	}
	persist := func() {
		cur := client.Session()
		prev := st.AnimegoSession()
		if cur.AccessToken == prev.AccessToken && cur.RefreshCookie == prev.RefreshCookie {
			return
		}
		prev.AccessToken, prev.RefreshCookie = cur.AccessToken, cur.RefreshCookie
		if err := st.SetAnimegoSession(prev); err != nil {
			log.Printf("持久化 animego 会话失败：%v", err)
		}
	}
	return client, persist
}

// buildServices 组装状态、mpv 探测、animego 客户端、播放编排、媒体库与磁力源规则。
func buildServices(configDir string) (*services, error) {
	// 状态与运行时目录（弹幕 ASS、mpv socket）都收在配置目录下。
	st, err := store.Open(filepath.Join(configDir, "state.json"))
	if err != nil {
		return nil, fmt.Errorf("加载本地状态失败：%w", err)
	}
	runtimeDir := filepath.Join(configDir, "runtime")
	if err := os.MkdirAll(runtimeDir, 0o700); err != nil {
		return nil, fmt.Errorf("创建运行时目录失败：%w", err)
	}
	// 清掉上次崩溃/被强杀时残留的弹幕 ASS 与 socket。
	player.SweepRuntimeDir(runtimeDir)

	// mpv 探测失败不阻断启动：库管理照常可用，设置页给安装指引，装好后可重探测（A5）。
	mpvRT := mpv.NewRuntime("")
	if info, err := mpvRT.Get(); err != nil {
		log.Printf("mpv 不可用：%v", err)
	} else {
		log.Printf("mpv %s（%s，来源：%s）", info.Version, info.Path, info.Source)
	}

	client, persistSession := newAnimegoClient(st)

	mgr := player.New(player.Options{
		Store:          st,
		Client:         client,
		MPV:            mpvRT,
		RuntimeDir:     runtimeDir,
		PersistSession: persistSession,
	})

	lib := api.NewLibraryService(st)
	if stats := lib.Rescan(); stats.Videos > 0 {
		log.Printf("媒体库就绪：%d 个视频，%d 个剧集簇", stats.Videos, stats.Clusters)
	}

	// 磁力源规则：零内置，规则目录空就是空；来源由用户在设置里指定（A3 / 红线 1）。
	sourceClient := &http.Client{Timeout: 20 * time.Second}
	sources := api.NewSourcesService(st,
		&rules.Fetcher{Client: sourceClient, UserAgent: "nagare/" + version},
		&rulesync.Syncer{Client: sourceClient, UserAgent: "nagare/" + version},
		filepath.Join(configDir, "rules"))
	if n, loadErrs := sources.Load(); n > 0 || len(loadErrs) > 0 {
		log.Printf("磁力源规则：%d 条已加载，%d 个文件有问题", n, len(loadErrs))
		for _, e := range loadErrs {
			log.Printf("  规则错误：%s", e)
		}
	}

	return &services{store: st, mpv: mpvRT, auth: client, player: mgr, lib: lib, sources: sources}, nil
}

// run 起 HTTP 服务、放托盘、等退出。
//
// 三个退出入口（信号 / 托盘「退出」/ POST /api/shutdown）都收敛到同一个 cancel，
// 收尾路径只有一条：停播放（触发最终进度回写）→ 优雅关 HTTP → Serve 返回 → 函数返回。
func run(cfg *config.Config, configDir string, svc *services, webFS fs.FS, f flags, apiOnly bool) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	apiHandler, updater := buildHandlers(configDir, svc, cancel)

	srv, ln, port, err := listenAndBuild(cfg, configDir, webFS, apiHandler, updater)
	if err != nil {
		return err
	}
	defer httpserver.ClearInstance(configDir)

	// 后台检查随 ctx 退出；关闭时不需要额外等待（只有一个 HTTP GET）。
	if updater != nil {
		updater.Start(ctx)
	}

	go watchSignals(ctx, cancel)
	serveErr := make(chan error, 1)
	go func() {
		serveErr <- srv.Serve(ln)
		cancel() // 服务意外退出时也要让托盘 / 主循环收工
	}()
	stopped := teardownOnCancel(ctx, srv, svc.player)

	// 带 token 的首启 URL 只交给浏览器与托盘；日志（会落盘）里只打端口，token 到配置文件里取。
	url := launchURL(port, cfg.Token)
	configPath, _ := config.Path()
	log.Printf("nagare %s 已启动：http://127.0.0.1:%d/（token 见 %s）", version, port, configPath)
	if !f.noBrowser && !apiOnly {
		openBrowserLogged(url)
	}

	// 托盘必须占着主 goroutine（macOS 要求 Cocoa 事件循环在主线程）；HTTP 服务已在后台。
	if f.noTray {
		<-ctx.Done()
	} else {
		tray.Run(ctx, tray.Options{
			URL:     url,
			Version: version,
			OnOpen:  func() { openBrowserLogged(url) },
			OnQuit: func() {
				log.Print("用户从托盘退出")
				cancel()
			},
		})
	}

	<-stopped
	if err := <-serveErr; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("HTTP 服务异常退出：%w", err)
	}
	log.Print("已退出")
	return nil
}

// buildHandlers 构造业务处理器与更新检查器。
//
// 更新检查构造失败不阻断启动 —— 播放是主线功能，少一个「有新版本」提示
// 不该让用户打不开 nagare；此时返回的 updater 为 nil，端点自然缺席。
func buildHandlers(configDir string, svc *services, cancel context.CancelFunc) (*api.Handler, *update.Checker) {
	h := api.New(api.Deps{
		Store:          svc.store,
		Lib:            svc.lib,
		Player:         svc.player,
		Auth:           svc.auth,
		AnimegoBaseURL: animego.DefaultBaseURL,
		MPV:            svc.mpv,
		Version:        version,
		Sources:        svc.sources,
		Shutdown:       cancel,
		DataDir:        configDir,
		LogPath:        logfile.Path(configDir),
	})
	updater, err := update.New(update.Options{CurrentVersion: version, CacheDir: configDir})
	if err != nil {
		log.Printf("更新检查不可用（不影响其余功能）：%v", err)
		return h, nil
	}
	return h, updater
}

// listenAndBuild 挂路由、监听端口、写实例标记，返回可服务的 Server 与 listener。
//
// 更新端点必须挂在 /api/* 的鉴权链之内（Host 白名单 + token + CSRF 头）；
// 挂到根路由上会绕开这三层防护。
func listenAndBuild(
	cfg *config.Config,
	configDir string,
	webFS fs.FS,
	apiHandler *api.Handler,
	updater *update.Checker,
) (*httpserver.Server, net.Listener, int, error) {
	registerAPI := func(mux *http.ServeMux) {
		apiHandler.Register(mux)
		// 更新检查构造失败时 updater 为 nil，三个端点自然缺席（前端有对应错误态）。
		if updater != nil {
			updater.Register(mux)
		}
	}

	ln, port, err := httpserver.Listen(cfg.Port)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("无法监听本地端口：%w", err)
	}
	if port != cfg.Port {
		log.Printf("端口 %d 被占用，改用 %d", cfg.Port, port)
		cfg.Port = port
		if err := cfg.Save(); err != nil {
			// 回写失败不致命：下次启动会重新探测，只是少了端口稳定性。
			log.Printf("回写实际端口到配置失败：%v", err)
		}
	}

	// 标记「我方实例已占住这个端口」：下次启动据此决定能否做带 token 的探测。
	if err := httpserver.MarkInstance(configDir); err != nil {
		log.Printf("写入实例标记失败（下次启动会多做一次端口探测）：%v", err)
	}

	return httpserver.New(httpserver.Options{
		Token:       cfg.Token,
		Port:        port,
		WebFS:       webFS,
		Version:     version,
		RegisterAPI: registerAPI,
	}), ln, port, nil
}

// teardownOnCancel 起一个收尾 goroutine，返回的 channel 在收尾完成后关闭。
//
// 先关 HTTP 再停播放：Shutdown 会等在途请求（含 /api/shutdown 自己的响应）收尾，
// 之后不再有新的 /api/play 能插进来 —— 否则它会趁 Stop 之后另起一个 mpv，
// 而收尾只跑一次，那个进程就没人管了。
func teardownOnCancel(ctx context.Context, srv *httpserver.Server, p *player.Manager) <-chan struct{} {
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		<-ctx.Done()
		log.Print("正在退出：停止播放并回写进度…")
		graceCtx, cancelGrace := context.WithTimeout(context.Background(), shutdownGrace)
		defer cancelGrace()
		if err := srv.Shutdown(graceCtx); err != nil {
			log.Printf("关闭 HTTP 服务：%v", err)
		}
		p.Stop()
	}()
	return stopped
}

// watchSignals 把 Ctrl-C / SIGTERM 接到统一的 cancel；触发后即停止接管，
// 这样第二次 Ctrl-C 走默认处理（强杀）—— 收尾卡死时的逃生口。
func watchSignals(ctx context.Context, cancel context.CancelFunc) {
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sig)
	select {
	case s := <-sig:
		log.Printf("收到 %v，准备退出", s)
		cancel()
	case <-ctx.Done():
	}
}

// launchURL 拼带 token 的首启地址：前端读取后会存入 sessionStorage 并从地址栏抹掉。
func launchURL(port int, token string) string {
	return fmt.Sprintf("http://127.0.0.1:%d/?token=%s", port, token)
}

// openBrowserLogged 开浏览器，失败只记日志（日志里不带 URL —— 它含 token）。
func openBrowserLogged(url string) {
	if err := openBrowserFn(url); err != nil {
		log.Printf("自动打开浏览器失败（请手动访问 http://127.0.0.1:<端口>/ 并按配置文件里的 token 登录）：%v", err)
	}
}

// openBrowser 用系统默认浏览器打开 URL。
// 不变量：url 只能是我们自己拼出来的 http://127.0.0.1:<port>/ 地址
// —— exec.Command 不经 shell，但若将来把任何外部字符串传进来，要先防参数注入。
// Start 之后在后台 Wait：open/xdg-open 几毫秒就退出，不 Wait 会留僵尸进程
// —— nagare 是常驻进程，这个进程表残留会一直挂到退出。
func openBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	// open/xdg-open 几毫秒就退出，不 Wait 会留僵尸进程；非零退出要说一声
	// （xdg-open 找不到浏览器就是这种），否则用户只会看到「什么都没发生」。
	go func() {
		if err := cmd.Wait(); err != nil {
			log.Printf("打开浏览器的命令返回错误（请手动访问 http://127.0.0.1:<端口>/）：%v", err)
		}
	}()
	return nil
}
