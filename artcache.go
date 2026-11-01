package main

import (
	"context"
	"log"
	"path/filepath"
	"time"

	"github.com/nagare-project/nagare/internal/api"
	"github.com/nagare-project/nagare/internal/artcache"
	"github.com/nagare-project/nagare/internal/httpserver"
)

// artCacheFlushWait 是退出时等封面缓存最后一次刷索引的上限。索引只影响淘汰顺序，
// 磁盘卡住时不该拖住退出（下次启动按目录重建）。
const artCacheFlushWait = 3 * time.Second

// setupArtCache 建封面缓存并接上：/art 端点、目录与媒体库的封面地址前缀、海报取色、设置页的占用与清空、
// 「最后才淘汰」的封面清单，再起后台维护（超限淘汰、定期刷索引）。
//
// 起不来只是界面没图（走无图版式），不阻断启动 —— 它和播放、扫描、搜索都没有关系。
// 返回的通道在后台维护收尾（ctx 结束后最后刷一次索引）之后关闭；没有封面缓存时已经关好。
func setupArtCache(ctx context.Context, configDir string, srv *httpserver.Server, apiHandler *api.Handler, svc *services) <-chan struct{} {
	done := make(chan struct{})
	art, err := artcache.New(filepath.Join(configDir, "cache", "art"))
	if err != nil {
		log.Printf("封面缓存不可用（界面将不显示封面）：%v", err)
		close(done)
		return done
	}
	apiHandler.SetCatalogArtPrefix("/art/" + srv.ArtCapability())
	srv.SetArtHandler(api.NewArtHandler(svc.store, art, apiHandler.RemoteArtwork()))
	svc.lib.SetArtPrefix("/art/" + srv.ArtCapability())
	// 海报色从同一份封面缓存里取（只读已缓存的封面，不额外出站）
	apiHandler.SetPosterTones(art)
	svc.lib.SetPosterTones(art)
	apiHandler.SetCoverCache(art)
	// 媒体库认出作品的封面、离线目录用到的图最后才淘汰（只在后台淘汰时问）
	art.SetPinned(apiHandler.PinnedArtSources)
	go func() {
		defer close(done)
		art.Run(ctx)
	}()
	return done
}

// waitArtCache 退出时等封面缓存刷完索引，最多 artCacheFlushWait。
func waitArtCache(done <-chan struct{}) {
	select {
	case <-done:
	case <-time.After(artCacheFlushWait):
		log.Print("封面缓存的索引没能在退出前写完（只影响淘汰顺序，下次启动按目录重建）")
	}
}
