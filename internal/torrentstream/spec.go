// 加种子用的 spec：播放与下载进库两条路共用，统一剥掉「链接作者指定的直连地址」。
package torrentstream

import (
	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"
)

// withoutDirectEndpoints 返回一份去掉了「让 client 直接去碰某个指定地址」的字段的副本。
//
// 这些地址全部来自磁力链接或 .torrent 文件本身，也就是由链接作者选定。照单执行等于
// 让用户粘进来的一行链接指使 nagare 对任意地址发请求：追踪信标（谁在什么时候点了播放）、
// 内网盲探（127.0.0.1 / 192.168.x 上的服务）。逐条对照 anacrolix v1.62.0
// （spec.go TorrentSpecFromMagnetUri / TorrentSpecFromMetaInfoErr、client.go MergeSpec）：
//
//   - Webseeds：磁力 ws= / .torrent url-list → HTTP 请求任意 URL（client 已 DisableWebseeds，这里再剥一层）；
//   - Sources：磁力 xs= 与 as= → 用 client 的默认 HTTP 客户端去拉 .torrent（sources.go），没有任何内网限制；
//   - PeerAddrs：磁力 x.pe → 当作受信任的直连对端立刻拨号（TCP / uTP）；
//   - DhtNodes：.torrent 的 nodes → 塞进 DHT 路由表并发 UDP 查询（磁力不产生这一项）。
//
// 保留的：Trackers（磁力 tr= / .torrent announce，见 trackers.go 的政策）、DisplayName（dn=，
// 只是显示名）、infohash 与 info 本身。磁力的其余参数（xt 以外的 urn、so= 等）anacrolix 只存不用。
//
// 返回副本而不是原地改：调用方手上的 spec 保持原样，便于测试对照。
func withoutDirectEndpoints(spec *torrent.TorrentSpec) *torrent.TorrentSpec {
	clean := *spec
	clean.Webseeds = nil
	clean.Sources = nil
	clean.PeerAddrs = nil
	clean.DhtNodes = nil
	return &clean
}

// magnetSpec 解析磁力并剥掉直连地址。播放与下载进库都必须经过这里，不能直接 AddMagnet。
func magnetSpec(uri string) (*torrent.TorrentSpec, error) {
	spec, err := torrent.TorrentSpecFromMagnetUri(uri)
	if err != nil {
		return nil, err
	}
	return withoutDirectEndpoints(spec), nil
}

// metaInfoSpec 由 .torrent 组 spec 并剥掉直连地址。不能直接 AddTorrent。
func metaInfoSpec(mi *metainfo.MetaInfo) (*torrent.TorrentSpec, error) {
	spec, err := torrent.TorrentSpecFromMetaInfoErr(mi)
	if err != nil {
		return nil, err
	}
	return withoutDirectEndpoints(spec), nil
}
