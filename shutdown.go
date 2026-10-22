package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/nagare-project/nagare/internal/api"
	"github.com/nagare-project/nagare/internal/httpserver"
	"github.com/nagare-project/nagare/internal/player"
	"github.com/nagare-project/nagare/internal/torrentstream"
)

// 退出路径：三个入口（信号 / 托盘「退出」/ POST /api/shutdown）都收敛到同一个 cancel，收尾只有这一条。

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
	downloads *torrentstream.Downloader,
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
		// 没下完的下载停下来，记录留着，下次启动接着下
		if downloads != nil {
			if err := downloads.Close(); err != nil {
				log.Printf("关闭磁力下载：%v", err)
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
