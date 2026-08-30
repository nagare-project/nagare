// nagare（流れ）—— 本地动漫播放 agent 入口。
// 单二进制：Go 服务 + go:embed 内嵌的 React UI，浏览器即遥控器。
package main

import (
	"context"
	"embed"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"

	"github.com/nagare-player/nagare/internal/animego"
	"github.com/nagare-player/nagare/internal/api"
	"github.com/nagare-player/nagare/internal/config"
	"github.com/nagare-player/nagare/internal/httpserver"
	"github.com/nagare-player/nagare/internal/mpv"
	"github.com/nagare-player/nagare/internal/player"
	"github.com/nagare-player/nagare/internal/store"
)

// embeddedWeb 内嵌前端构建产物。all: 前缀确保带点号的文件（如 .gitkeep）也被收进来，
// 这样仓库里只有占位文件时依然能编译，运行时降级为 API-only 模式。
//
//go:embed all:web
var embeddedWeb embed.FS

// version 由 CI 通过 -ldflags "-X main.version=..." 注入，本地开发用默认值。
var version = "0.1.0-dev"

func main() {
	noBrowser := flag.Bool("no-browser", false, "启动时不自动打开浏览器")
	showVersion := flag.Bool("version", false, "打印版本号并退出")
	flag.Parse()

	if *showVersion {
		fmt.Println(version)
		return
	}

	cfg, err := config.LoadOrInit()
	if err != nil {
		log.Fatalf("加载配置失败：%v", err)
	}
	configDir, err := config.Dir()
	if err != nil {
		log.Fatalf("定位配置目录失败：%v", err)
	}

	// 状态与运行时目录（弹幕 ASS、mpv socket）都收在配置目录下。
	st, err := store.Open(filepath.Join(configDir, "state.json"))
	if err != nil {
		log.Fatalf("加载本地状态失败：%v", err)
	}
	runtimeDir := filepath.Join(configDir, "runtime")
	if err := os.MkdirAll(runtimeDir, 0o700); err != nil {
		log.Fatalf("创建运行时目录失败：%v", err)
	}
	// 清掉上次崩溃/被强杀时残留的弹幕 ASS 与 socket。
	player.SweepRuntimeDir(runtimeDir)

	// mpv 探测失败不阻断启动：库管理照常可用，播放时给出安装指引（A5）。
	mpvInfo, mpvErr := mpv.Detect("")
	if mpvErr != nil {
		log.Printf("mpv 不可用：%v", mpvErr)
	} else {
		log.Printf("mpv %s（%s）", mpvInfo.Version, mpvInfo.Path)
	}

	client := animego.New(animego.Options{UserAgent: "nagare/" + version})
	if sess := st.AnimegoSession(); sess.AccessToken != "" || sess.RefreshCookie != "" {
		client.RestoreSession(animego.Session{
			AccessToken:   sess.AccessToken,
			RefreshCookie: sess.RefreshCookie,
		})
	}
	// 会话可能在任意鉴权调用后轮换（refresh cookie 单次有效），及时落盘。
	persistSession := func() {
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

	mgr := player.New(player.Options{
		Store:          st,
		Client:         client,
		MPV:            mpvInfo,
		RuntimeDir:     runtimeDir,
		PersistSession: persistSession,
	})

	lib := api.NewLibraryService(st)
	if stats := lib.Rescan(); stats.Videos > 0 {
		log.Printf("媒体库就绪：%d 个视频，%d 个剧集簇", stats.Videos, stats.Clusters)
	}

	apiHandler := api.New(api.Deps{
		Store:          st,
		Lib:            lib,
		Player:         mgr,
		Auth:           client,
		AnimegoBaseURL: animego.DefaultBaseURL,
		MPV:            mpvInfo,
		MPVErr:         mpvErr,
		Version:        version,
	})

	ln, port, err := httpserver.Listen(cfg.Port)
	if err != nil {
		log.Fatalf("无法监听本地端口：%v", err)
	}
	if port != cfg.Port {
		log.Printf("端口 %d 被占用，改用 %d", cfg.Port, port)
		cfg.Port = port
		if err := cfg.Save(); err != nil {
			// 回写失败不致命：下次启动会重新探测，只是少了端口稳定性。
			log.Printf("回写实际端口到配置失败：%v", err)
		}
	}

	webFS, apiOnly := httpserver.SubWebFS(embeddedWeb)
	if apiOnly {
		log.Print("未找到内嵌 web 产物，进入 API-only 模式（先 cd frontend && bun run build 再重新编译）")
	}

	srv := httpserver.New(httpserver.Options{
		Token:       cfg.Token,
		Port:        port,
		WebFS:       webFS,
		Version:     version,
		RegisterAPI: apiHandler.Register,
	})

	// Ctrl-C / SIGTERM：先停播放会话（触发最终进度回写），再关监听退出。
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	go func() {
		<-ctx.Done()
		log.Print("正在退出：停止播放并回写进度…")
		mgr.Stop()
		_ = ln.Close()
	}()

	// 带 token 的首启 URL：前端读取后会存入 sessionStorage 并从地址栏抹掉。
	url := fmt.Sprintf("http://127.0.0.1:%d/?token=%s", port, cfg.Token)
	log.Printf("nagare %s 已启动：%s", version, url)

	if !*noBrowser && !apiOnly {
		if err := openBrowser(url); err != nil {
			log.Printf("自动打开浏览器失败（请手动访问上面的地址）：%v", err)
		}
	}

	err = srv.Serve(ln)
	if ctx.Err() != nil {
		log.Print("已退出")
		return
	}
	if err != nil {
		log.Fatalf("HTTP 服务退出：%v", err)
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
	go func() { _ = cmd.Wait() }()
	return nil
}
