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
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/nagare-project/nagare/internal/animego"
	"github.com/nagare-project/nagare/internal/api"
	"github.com/nagare-project/nagare/internal/artcache"
	"github.com/nagare-project/nagare/internal/config"
	"github.com/nagare-project/nagare/internal/httpserver"
	"github.com/nagare-project/nagare/internal/logfile"
	"github.com/nagare-project/nagare/internal/mpv"
	"github.com/nagare-project/nagare/internal/player"
	"github.com/nagare-project/nagare/internal/rules"
	"github.com/nagare-project/nagare/internal/rulesync"
	"github.com/nagare-project/nagare/internal/selfupdate"
	"github.com/nagare-project/nagare/internal/sourceplugin"
	"github.com/nagare-project/nagare/internal/store"
	"github.com/nagare-project/nagare/internal/torrentstream"
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
	update    bool // 装上最新版本后退出（无界面场景：systemd / brew services / 纯终端）
	// validateRules 是规则作者与规则仓库 CI 用的：校验一个目录里的规则并退出。
	validateRules string
}

// cliUpdateTimeout 是 -update 的总时限。Windows 包内置 mpv 有 120MB，
// 慢网络下几分钟很正常，给足余量；它只是防挂死的兜底。
const cliUpdateTimeout = 15 * time.Minute

// services 是主进程持有的全部业务对象。
type services struct {
	lists   *animego.Client
	store   *store.Store
	mpv     *mpv.Runtime
	auth    api.AnimegoAuth
	player  *player.Manager
	lib     *api.LibraryService
	sources *api.SourcesService
	// sourcePlugin 始终存在；未配置时保持 disabled，不启动任何子进程。
	sourcePlugin *api.SourcePluginService
	// torrent 为 nil 表示磁力引擎启动失败：其余功能照常，磁力端点整体返回 503，
	// 设置页据此显示降级原因（决议 CQ3：降级必须可见）。
	torrent         *torrentstream.Engine
	torrentCacheDir string
	// streamBase 是流地址前缀的延迟绑定，见 streamBaseHolder。
	streamBase *streamBaseHolder
	// selfUpdate 为 nil 表示这个构建不带自更新（没配更新公钥，或构造失败）。
	selfUpdate *selfupdate.Updater
	// restartWanted 由 /api/update/apply 在装好新版本后置位：退出收尾跑完之后
	// 用新二进制替换本进程。不在 HTTP 处理器里直接 exec —— 那会跳过停播放与进度回写。
	restartWanted atomic.Bool
}

// streamBaseHolder 解决一个时序问题：磁力引擎必须在组装阶段就建好（它要清空缓存
// 目录、起 BT client），而流地址前缀依赖那时还不存在的监听端口与能力段。
// 引擎拿到的是一个取值函数，服务起来之后再把真值填进来。
type streamBaseHolder struct {
	mu   sync.RWMutex
	base string
}

func (h *streamBaseHolder) get() string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.base
}

func (h *streamBaseHolder) set(v string) {
	h.mu.Lock()
	h.base = v
	h.mu.Unlock()
}

// main 只负责解析开关与决定退出码；真正的启动逻辑在 start 里，
// 这样日志文件的 defer 一定跑得到 —— log.Fatal 会 os.Exit，直接跳过 defer。
func main() {
	var f flags
	flag.BoolVar(&f.noBrowser, "no-browser", false, "启动时不自动打开浏览器")
	flag.BoolVar(&f.noTray, "no-tray", false, "不显示菜单栏/托盘图标（headless：brew services、Linux 服务）")
	flag.BoolVar(&f.update, "update", false, "下载并安装最新版本后退出（无界面场景用；不自动重启）")
	flag.StringVar(&f.validateRules, "validate-rules", "", "校验目录里的磁力源规则并退出（规则作者与规则仓库 CI 用）")
	showVersion := flag.Bool("version", false, "打印版本号并退出")
	flag.Parse()
	if *showVersion {
		fmt.Println(version)
		return
	}
	// 校验规则不需要配置、状态、端口，什么都不用装配，直接跑完退出。
	if f.validateRules != "" {
		if err := validateRuleDir(f.validateRules); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
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

	// -update 是一次性动作，走完就退出：不起服务、不碰媒体库、不动播放状态。
	if f.update {
		return runCLIUpdate(configDir)
	}

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

// validateRuleDir 校验一个目录里的全部规则，把每条错误逐行打出来。
//
// 存在的理由：规则的权威校验器是引擎自己（internal/rules），而它是 internal 包，
// 独立的规则仓库 import 不了。手写一份 JSON Schema 放在那边必然与引擎漂移 ——
// 漂移的方向还特别糟：schema 说合法、引擎加载失败，规则作者查不出原因。
// 把校验做成一个开关，规则仓库的 CI 直接跑这个二进制，校验器与引擎永远是同一份代码。
func validateRuleDir(dir string) error {
	info, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("打不开规则目录 %s：%w", dir, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("%s 不是目录", dir)
	}

	loaded, loadErrs := rules.LoadDir(dir)
	for _, e := range loadErrs {
		fmt.Fprintf(os.Stderr, "  ✗ %v\n", e)
	}
	for _, r := range loaded {
		fmt.Printf("  ✓ %s（%s）\n", r.ID, r.Name)
	}
	if len(loadErrs) > 0 {
		return fmt.Errorf("%d 条规则有问题，%d 条通过", len(loadErrs), len(loaded))
	}
	fmt.Printf("%d 条规则全部通过校验\n", len(loaded))
	return nil
}

// runCLIUpdate 是无界面场景的一次性更新：查最新版本 → 装上 → 退出。
//
// 刻意【不】自动重启：这条路径的调用方是 systemd / brew services 之类的服务管理器，
// 它们自己记着进程账，我们擅自 exec 只会让它对不上账。装好就退出，由它决定何时重启。
func runCLIUpdate(configDir string) error {
	su, err := selfupdate.New(selfupdate.Options{
		CurrentVersion: version,
		PublicKey:      updatePublicKey,
	})
	if err != nil {
		return fmt.Errorf("自更新不可用：%w", err)
	}
	if c := su.Capability(); !c.Supported {
		return errors.New(c.Reason)
	}
	checker, err := update.New(update.Options{CurrentVersion: version, CacheDir: configDir})
	if err != nil {
		return fmt.Errorf("构造更新检查失败：%w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), cliUpdateTimeout)
	defer cancel()
	v := checker.Check(ctx, true)
	if v.Error != "" {
		return errors.New(v.Error)
	}
	if !v.Available {
		fmt.Printf("已经是最新版本（%s）\n", version)
		return nil
	}

	fmt.Printf("正在更新 %s → %s …\n", version, v.Latest)
	if err := su.Apply(ctx, v.Latest); err != nil {
		return err
	}
	fmt.Printf("已更新到 %s。重新启动 nagare 即可生效。\n", v.Latest)
	return nil
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

// newAnimegoClient 还原已保存的会话，并挂上「会话一变就落盘」的回调。
//
// access token 15 分钟、refresh cookie 单次有效：任意一次鉴权调用都可能把两者
// 换成新的，不及时落盘就等于用户下次启动要重新登录。
//
// 落盘由客户端【主动通知】而不是由各调用点记得来取。后者漏过一次：
// 播放开始时的 match 会触发 401 刷新并轮换 cookie，而用户看一半停掉时
// 那条路径不落盘 —— 轮换后的 cookie 从没写进磁盘，下次启动静默掉线。
func newAnimegoClient(st *store.Store) *animego.Client {
	persist := func(cur animego.Session) {
		prev := st.AnimegoSession()
		if cur.AccessToken == prev.AccessToken && cur.RefreshCookie == prev.RefreshCookie {
			return
		}
		prev.AccessToken, prev.RefreshCookie = cur.AccessToken, cur.RefreshCookie
		if err := st.SetAnimegoSession(prev); err != nil {
			// 这里失败只能记日志：它跑在刷新链路里，没有用户面前的上下文。
			// 后果是下次启动要重新登录，界面上的登录态会如实反映。
			log.Printf("持久化 animego 会话失败：%v", err)
		}
	}
	client := animego.New(animego.Options{
		UserAgent:       "nagare/" + version,
		OnSessionChange: persist,
	})
	if sess := st.AnimegoSession(); sess.AccessToken != "" || sess.RefreshCookie != "" {
		client.RestoreSession(animego.Session{
			AccessToken:   sess.AccessToken,
			RefreshCookie: sess.RefreshCookie,
		})
	}
	return client
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

	client := newAnimegoClient(st)

	// 磁力引擎：起不来不阻断启动（本地库与播放照常），磁力端点整体降级。
	streamBase := &streamBaseHolder{}
	cacheDir := filepath.Join(configDir, "cache", "torrent")
	engine, err := newTorrentEngine(st, cacheDir, streamBase)
	if err != nil {
		log.Printf("磁力播放不可用（不影响本地文件播放）：%v", err)
	}

	mgr := player.New(player.Options{
		Store:      st,
		Client:     client,
		MPV:        mpvRT,
		RuntimeDir: runtimeDir,
		// 用户直接关掉 mpv 窗口时没有任何 API 请求发生，没有这个回调，
		// 种子会一直挂在那里下载和上传（决议 M3-2 / M3-4：停播即停）。
		OnSessionEnd: func() {
			if engine != nil {
				engine.Stop()
			}
		},
	})

	// 自更新：公钥为空（未配置签名密钥）时 New 仍然成功，只是 Capability 报不支持
	// —— 失败关闭，界面如实说明而不是按钮点了没反应。构造失败同样不阻断启动。
	su, err := selfupdate.New(selfupdate.Options{
		CurrentVersion: version,
		PublicKey:      updatePublicKey,
	})
	if err != nil {
		log.Printf("自更新不可用（不影响其余功能）：%v", err)
		su = nil
	} else {
		su.SweepOldFiles() // 清掉上次更新留下的 .old 残留
	}

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

	// 统一来源插件同样默认关闭。只有 state.json 中保存了用户显式启用的
	// 绝对路径才会启动；失败只降级插件，不影响本地库与原有磁力规则。
	pluginService := api.NewSourcePluginService(st, sourceplugin.NewManager(), version)
	if err := pluginService.StartConfigured(); err != nil {
		log.Printf("来源插件未能启动（不影响本地播放）：%v", err)
	}

	return &services{
		store: st, mpv: mpvRT, auth: client, lists: client, player: mgr, lib: lib, sources: sources, sourcePlugin: pluginService,
		torrent: engine, torrentCacheDir: cacheDir, streamBase: streamBase,
		selfUpdate: su,
	}, nil
}

// newTorrentEngine 按持久化配置起磁力引擎。
func newTorrentEngine(st *store.Store, cacheDir string, base *streamBaseHolder) (*torrentstream.Engine, error) {
	c := st.TorrentConfig()
	return torrentstream.New(torrentstream.Options{
		CacheDir:   cacheDir,
		StreamBase: base.get,
		Config: torrentstream.Config{
			Seeding:        c.Seeding,
			Trackers:       torrentstream.EffectiveTrackers(!c.DisableDefaultTrackers, c.Trackers),
			PortForwarding: c.PortForwarding,
			ListenPort:     c.ListenPort,
		},
	})
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

	// 流端点接上磁力引擎，并把地址前缀回填给它（能力段与端口到这一步才确定）。
	// 能力段等同凭证：只交给引擎用于拼给 mpv 的地址，不进日志。
	if svc.torrent != nil {
		svc.streamBase.set(fmt.Sprintf("http://127.0.0.1:%d/stream/%s", port, srv.StreamCapability()))
		srv.SetStreamHandler(svc.torrent.Handler())
	}

	// 封面端点。起不来只是界面没图（走无图版式），不阻断启动 ——
	// 它和播放、扫描、搜索都没有关系。
	if art, err := artcache.New(filepath.Join(configDir, "cache", "art")); err != nil {
		log.Printf("封面缓存不可用（界面将不显示封面）：%v", err)
	} else {
		apiHandler.SetCatalogArtPrefix("/art/" + srv.ArtCapability())
		srv.SetArtHandler(api.NewArtHandler(svc.store, art, apiHandler.RemoteArtwork()))
		svc.lib.SetArtPrefix("/art/" + srv.ArtCapability())
	}

	// 本地媒体流（浏览器内播放）。决议 A5 原本不做这件事，用户 2026-09-03
	// 要求接上；正常播放路径仍是 mpv，这条只是补一个「手边没有 mpv 时」的出路。
	srv.SetMediaHandler(api.NewMediaHandler(svc.lib))
	svc.lib.SetMediaPrefix("/media/" + srv.MediaCapability())

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
	stopped := teardownOnCancel(ctx, srv, svc.player, svc.torrent, svc.sourcePlugin)

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
	// 自更新装好之后才走到这里：收尾已经跑完（播放停了、进度写了、磁力引擎关了），
	// 现在才能安全地把自己换成新版本。Restart 正常情况下不返回。
	if svc.restartWanted.Load() && svc.selfUpdate != nil {
		log.Print("以新版本重启…")
		if err := svc.selfUpdate.Restart(); err != nil {
			return fmt.Errorf("以新版本重启失败（新版本已经装好，手动重新打开 nagare 即可）：%w", err)
		}
	}
	log.Print("已退出")
	return nil
}

// buildHandlers 构造业务处理器与更新检查器。
//
// 更新检查构造失败不阻断启动 —— 播放是主线功能，少一个「有新版本」提示
// 不该让用户打不开 nagare；此时返回的 updater 为 nil，端点自然缺席。
func buildHandlers(configDir string, svc *services, cancel context.CancelFunc) (*api.Handler, *update.Checker) {
	deps := api.Deps{
		Store:           svc.store,
		Lib:             svc.lib,
		Player:          svc.player,
		Auth:            svc.auth,
		Lists:           svc.lists,
		Catalog:         svc.lists,
		RemoteArt:       api.NewRemoteArt(animego.DefaultBaseURL, 4096),
		AnimegoBaseURL:  animego.DefaultBaseURL,
		MPV:             svc.mpv,
		Version:         version,
		Sources:         svc.sources,
		SourcePlugin:    svc.sourcePlugin,
		Shutdown:        cancel,
		DataDir:         configDir,
		LogPath:         logfile.Path(configDir),
		TorrentCacheDir: svc.torrentCacheDir,
	}
	// 只在引擎真的建起来时赋值：把一个 nil 的 *Engine 装进接口字段会得到
	// 「非 nil 接口包着 nil 指针」，降级判断会失效并在调用时 panic。
	if svc.torrent != nil {
		deps.Torrent = svc.torrent
	}
	h := api.New(deps)
	updater, err := update.New(update.Options{
		CurrentVersion: version,
		CacheDir:       configDir,
		SelfUpdate:     svc.selfUpdate,
		// 装好新版本后不直接 exec：置位并触发根 cancel，让退出收尾（停播放、
		// 回写进度、关 HTTP、停磁力引擎）照常跑完，收尾之后 run 再 exec 新二进制。
		OnRestart: func() {
			svc.restartWanted.Store(true)
			cancel()
		},
		// 更新前把占着文件的东西停掉：mpv 进程（Windows 上会锁住内置的 mpv\），
		// 以及磁力会话（它在缓存目录里持续写盘）。
		BeforeApply: func() {
			svc.player.Stop()
			if svc.torrent != nil {
				svc.torrent.Stop()
			}
		},
	})
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
// 而收尾只跑一次，那个进程就没人管了。磁力引擎放最后关，理由同上：
// 停播放会触发会话终结回调，那里还要用到引擎。
func teardownOnCancel(
	ctx context.Context,
	srv *httpserver.Server,
	p *player.Manager,
	engine *torrentstream.Engine,
	plugin *api.SourcePluginService,
) <-chan struct{} {
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
		if plugin != nil {
			plugin.Stop()
		}
		// 播放停完再关引擎：Stop 会触发会话终结回调，那里还要碰引擎。
		// 关闭时停做种并清空缓存目录（决议 M3-2 / M3-4：退出即停、退出即清）。
		if engine != nil {
			if err := engine.Close(); err != nil {
				log.Printf("关闭磁力引擎：%v", err)
			}
		}
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
