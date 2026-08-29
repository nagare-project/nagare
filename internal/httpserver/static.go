package httpserver

import (
	"io/fs"
	"log"
	"net/http"
	"strings"
)

// SubWebFS 从 go:embed 的根 FS 里取 web 子目录。
// index.html 不存在即视为前端未构建，返回 (nil, true) 进入 API-only 模式
// —— 这让纯后端开发不需要先跑一遍前端构建。
func SubWebFS(embedded fs.FS) (fs.FS, bool) {
	sub, err := fs.Sub(embedded, "web")
	if err != nil {
		return nil, true
	}
	if _, err := fs.Stat(sub, "index.html"); err != nil {
		return nil, true
	}
	return sub, false
}

// assetsPrefix 是 Vite 构建产物里静态资源的目录前缀。
const assetsPrefix = "assets/"

// apiOnlyNotice 是未嵌入前端时根路径的提示文案。
const apiOnlyNotice = "nagare 处于 API-only 模式：未内嵌 web 产物。\n" +
	"先执行 cd frontend && bun install && bun run build，再重新编译。\n"

// staticHandler 提供内嵌前端；SPA 路由（无扩展名且文件不存在的路径）
// 回落到 index.html 交给前端路由，静态资源缺失则如实 404。
func (s *Server) staticHandler() http.Handler {
	if s.opts.WebFS == nil {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			if _, err := w.Write([]byte(apiOnlyNotice)); err != nil {
				log.Printf("httpserver: 写响应失败: %v", err)
			}
		})
	}

	fileServer := http.FileServerFS(s.opts.WebFS)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/")
		if name != "" {
			if _, err := fs.Stat(s.opts.WebFS, name); err == nil {
				fileServer.ServeHTTP(w, r)
				return
			}
			// 只有 Vite 产物目录下的缺失才如实 404；其余路径一律回落 index.html。
			// 刻意不用"带扩展名"来判定资源请求 —— SPA 路由段里可能有点号
			//（如半集编号 12.5），那种路径必须交给前端路由。
			if strings.HasPrefix(name, assetsPrefix) {
				http.NotFound(w, r)
				return
			}
		}
		http.ServeFileFS(w, r, s.opts.WebFS, "index.html")
	})
}
