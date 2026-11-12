// 播放路径的磁力参数加固回归测试。
//
// 复用 integration_test.go 的脚手架（替换包级 newClient / 超时变量），
// 因此同样【不能】用 t.Parallel()。
package torrentstream

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// hitCounter 记一个本机端点被碰了几次，以及被碰到的路径（失败时打出来好查）。
type hitCounter struct {
	n     atomic.Int64
	mu    sync.Mutex
	paths []string
}

func (h *hitCounter) note(path string) {
	h.n.Add(1)
	h.mu.Lock()
	h.paths = append(h.paths, path)
	h.mu.Unlock()
}

func (h *hitCounter) seen() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.paths...)
}

// listenTCPCounter 起一个只计数、立刻关掉连接的 TCP 监听，冒充 x.pe 指向的「对端」。
func listenTCPCounter(t *testing.T) (addr string, hits *hitCounter) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	hits = &hitCounter{}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			hits.note(conn.RemoteAddr().String())
			_ = conn.Close()
		}
	}()
	t.Cleanup(func() {
		_ = ln.Close()
		<-done
	})
	return ln.Addr().String(), hits
}

// 播放（边下边播）路径绝不能执行磁力里的 xs= / as=（从任意 URL 拉 .torrent）与
// x.pe（直连指定地址的对端）：那等于让一条粘进来的链接指使 nagare 去碰链接作者
// 选的地址 —— 追踪信标、内网探测，与 M3 修掉的 ws= 同类。下载进库那条路早就剥掉了，
// 这条用例守的是播放这一边。
//
// 改动前（AddMagnet 原样交给 anacrolix）：sourcer 与拨号都在加种子的当下就发出，
// 两个端点在一秒内各被碰到；改动后两者都必须是 0。
func TestPlaybackMagnetIgnoresSourceAndPeerParams(t *testing.T) {
	shortenWaits(t, 2*time.Second, 2*time.Second)
	engine := newIntegrationEngine(t, filepath.Join(t.TempDir(), "torrent"))

	var httpHits hitCounter
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		httpHits.note(r.URL.Path)
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	peerAddr, peerHits := listenTCPCounter(t)

	q := url.Values{}
	q.Set("dn", "beacon")
	q.Add("xs", srv.URL+"/xs.torrent")
	q.Add("as", srv.URL+"/as.torrent")
	q.Add("x.pe", peerAddr)
	magnet := "magnet:?xt=urn:btih:" + strings.Repeat("ef", 20) + "&" + q.Encode()

	_, err := engine.Prepare(context.Background(), PrepareRequest{Magnet: magnet, FileIndex: -1})
	require.Error(t, err, "没有任何真实对端，最终应等元数据超时")

	assert.Zero(t, httpHits.n.Load(), "xs= / as= 不能被拿去拉种子文件：%v", httpHits.seen())
	assert.Zero(t, peerHits.n.Load(), "x.pe 不能被拿去拨号：%v", peerHits.seen())
}
