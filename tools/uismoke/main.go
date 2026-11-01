// uismoke 是界面冒烟测试（frontend/e2e，Playwright）用的本机服务。
//
// 用真实的 internal/httpserver 托管构建好的界面 —— Host 白名单、token、变更请求的自定义头、
// CSP、SPA 回落都与正式版一致；/api/* 一律回答假数据目录里的固定 JSON：
// 不连网络、不起 mpv 与磁力引擎、不读写任何配置目录。
//
// 用法（Playwright 的 webServer 会这样起它）：
//
//	NAGARE_SMOKE_TOKEN=<32 位十六进制> uismoke -web ../web -fixtures e2e/fixtures -port 8599
//
// 假数据目录里的 routes.json 把「方法 + 路径」映射到同目录下的数据文件（http.ServeMux 的模式写法，
// 可带 {id} 通配）；数据文件里只写信封里的 data 一段。没写进 routes.json 的接口答 404 并打一行日志，
// 冒烟测试会把这种失败请求当成错误 —— 新页面多调了一个接口，就得在这里补上假数据。
//
// 时间写成占位串，回答时换成当时的时间：放送表只显示近七日、媒体库扫描太久会触发自动重扫，
// 写死的时间戳过几天就让页面换了样子。"@now" 是 Unix 秒，"@nowms" 是毫秒，后面可跟 +N / -N 秒，
// 例如 "@now+7200" 是两小时后。
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"syscall"
	"time"

	"github.com/nagare-project/nagare/internal/httpserver"
)

// tokenEnv 是传 token 的环境变量：不走命令行参数，免得出现在进程列表里。
const tokenEnv = "NAGARE_SMOKE_TOKEN"

// shutdownTimeout 是收到退出信号后等在途请求的上限。
const shutdownTimeout = 5 * time.Second

var tokenShape = regexp.MustCompile(`^[0-9a-f]{32}$`)

// nowToken 匹配假数据里的时间占位串（连同引号，换成数字）。
var nowToken = regexp.MustCompile(`"@now(ms)?([+-][0-9]+)?"`)

func main() {
	log.SetFlags(0)
	log.SetPrefix("uismoke: ")
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	webDir := flag.String("web", "web", "构建好的界面目录（里面要有 index.html）")
	fixturesDir := flag.String("fixtures", "frontend/e2e/fixtures", "假数据目录（routes.json + 数据文件）")
	port := flag.Int("port", 8599, "监听端口（只监听 127.0.0.1）")
	flag.Parse()

	token := os.Getenv(tokenEnv)
	if !tokenShape.MatchString(token) {
		return fmt.Errorf("环境变量 %s 要是 32 位小写十六进制", tokenEnv)
	}
	webFS := os.DirFS(*webDir)
	if _, err := fs.Stat(webFS, "index.html"); err != nil {
		return fmt.Errorf("%s 里没有 index.html（先 cd frontend && bun run build）：%w", *webDir, err)
	}
	routes, err := loadFixtures(*fixturesDir)
	if err != nil {
		return err
	}

	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", *port))
	if err != nil {
		return fmt.Errorf("监听 127.0.0.1:%d：%w", *port, err)
	}
	srv := httpserver.New(httpserver.Options{
		Token:       token,
		Port:        *port,
		WebFS:       webFS,
		Version:     "0.0.0-smoke",
		RegisterAPI: register(routes),
	})

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Printf("关闭时出错：%v", err)
		}
	}()

	log.Printf("在 http://127.0.0.1:%d 上提供界面与 %d 条假数据接口", *port, len(routes))
	if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("服务异常退出：%w", err)
	}
	return nil
}

// fixture 是一条假数据接口：ServeMux 模式 + 预先校验过的 JSON。
type fixture struct {
	pattern string
	data    json.RawMessage
}

// loadFixtures 读 routes.json 与它引用的每个数据文件；任何一个读不到或不是合法 JSON 都直接失败，
// 不要等到浏览器里才发现某一页的数据是坏的。
func loadFixtures(dir string) ([]fixture, error) {
	raw, err := os.ReadFile(filepath.Join(dir, "routes.json"))
	if err != nil {
		return nil, fmt.Errorf("读假数据路由表：%w", err)
	}
	var table map[string]string
	if err := json.Unmarshal(raw, &table); err != nil {
		return nil, fmt.Errorf("解析 routes.json：%w", err)
	}
	patterns := make([]string, 0, len(table))
	for pattern := range table {
		patterns = append(patterns, pattern)
	}
	sort.Strings(patterns)

	out := make([]fixture, 0, len(patterns))
	for _, pattern := range patterns {
		name := table[pattern]
		if filepath.Base(name) != name {
			return nil, fmt.Errorf("routes.json 里 %q 的数据文件 %q 只能是同目录下的文件名", pattern, name)
		}
		body, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return nil, fmt.Errorf("读 %s 的假数据：%w", pattern, err)
		}
		if !json.Valid(body) {
			return nil, fmt.Errorf("%s 的假数据 %s 不是合法 JSON", pattern, name)
		}
		out = append(out, fixture{pattern: pattern, data: json.RawMessage(body)})
	}
	return out, nil
}

// register 把假数据挂进 /api/* 的鉴权链；兜底的 /api/ 对没写假数据的接口答 404 并记一行日志。
func register(routes []fixture) func(*http.ServeMux) {
	return func(mux *http.ServeMux) {
		for _, f := range routes {
			data := f.data
			mux.HandleFunc(f.pattern, func(w http.ResponseWriter, _ *http.Request) {
				httpserver.WriteJSON(w, http.StatusOK, json.RawMessage(expandNow(data, time.Now())))
			})
		}
		mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
			// 只记方法与路径：查询串可能带 token
			log.Printf("没有假数据：%s %s", r.Method, r.URL.Path)
			httpserver.WriteError(w, http.StatusNotFound, "冒烟测试的假数据里没有这个接口")
		})
	}
}

// expandNow 把时间占位串换成 now 加偏移后的 Unix 时间（秒或毫秒）。
func expandNow(body []byte, now time.Time) []byte {
	return nowToken.ReplaceAllFunc(body, func(match []byte) []byte {
		sub := nowToken.FindSubmatch(match)
		var offset int64
		if len(sub[2]) > 0 {
			// 正则已保证是带符号的十进制数；超出 int64 的写法本来就没有意义，按 0 处理
			offset, _ = strconv.ParseInt(string(sub[2]), 10, 64)
		}
		at := now.Add(time.Duration(offset) * time.Second)
		if len(sub[1]) > 0 {
			return strconv.AppendInt(nil, at.UnixMilli(), 10)
		}
		return strconv.AppendInt(nil, at.Unix(), 10)
	})
}
