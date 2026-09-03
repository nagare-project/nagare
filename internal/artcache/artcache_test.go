package artcache

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

// newTestCache 建一个把拨号闸换成直连的缓存 —— guardedDial 的职责正是
// 拦掉本机地址，而 httptest 就跑在本机上，两者必然冲突。
// guardedDial 自己的判断由 TestIsPrivate / TestGuardedDialRejectsLoopback 单测。
func newTestCache(t *testing.T) *Cache {
	t.Helper()
	prev := dialContext
	dialContext = (&net.Dialer{}).DialContext
	t.Cleanup(func() { dialContext = prev })

	c, err := New(t.TempDir())
	require.NoError(t, err)
	return c
}

// pngBytes 是一张最小的合法 PNG（内容不重要，只要不是零字节）。
var pngBytes = []byte("\x89PNG\r\n\x1a\n" + strings.Repeat("x", 64))

func TestGetDownloadsAndCaches(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(pngBytes)
	}))
	defer srv.Close()

	c := newTestCache(t)
	// httptest 是 http:// 而 validateURL 只放行 https —— 用 https 的 URL
	// 拼出来再让 DialContext 落到测试服务上是行不通的（会做 TLS 握手）。
	// 所以这里直接测 download，validateURL 由 TestValidateURL 覆盖。
	dest := filepath.Join(t.TempDir(), "img")
	require.NoError(t, c.download(context.Background(), srv.URL, dest))

	got, err := os.ReadFile(dest)
	require.NoError(t, err)
	require.Equal(t, pngBytes, got)
	require.EqualValues(t, 1, atomic.LoadInt32(&hits))

	// 落盘的图不该带组/其他人可读位：缓存目录在用户配置目录下。
	st, err := os.Stat(dest)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), st.Mode().Perm())
}

func TestDownloadRejectsNonImage(t *testing.T) {
	// 这一条挡的是：上游返回一份 HTML/JS，被我们缓存下来，
	// 再由【本机同源地址】吐回给页面 —— 那就把同源信任送给了外部内容。
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<script>alert(1)</script>"))
	}))
	defer srv.Close()

	c := newTestCache(t)
	dest := filepath.Join(t.TempDir(), "img")
	err := c.download(context.Background(), srv.URL, dest)
	require.ErrorContains(t, err, "不是图片")
	require.NoFileExists(t, dest)
}

func TestDownloadRejectsOversize(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		buf := make([]byte, 1<<20)
		for i := 0; i < (maxImageBytes>>20)+1; i++ {
			_, _ = w.Write(buf)
		}
	}))
	defer srv.Close()

	c := newTestCache(t)
	dest := filepath.Join(t.TempDir(), "img")
	err := c.download(context.Background(), srv.URL, dest)
	require.ErrorContains(t, err, "上限")
	require.NoFileExists(t, dest)
}

func TestDownloadRejectsEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
	}))
	defer srv.Close()

	c := newTestCache(t)
	err := c.download(context.Background(), srv.URL, filepath.Join(t.TempDir(), "img"))
	require.ErrorContains(t, err, "为空")
}

func TestDownloadRejectsNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "gone", http.StatusNotFound)
	}))
	defer srv.Close()

	c := newTestCache(t)
	err := c.download(context.Background(), srv.URL, filepath.Join(t.TempDir(), "img"))
	require.ErrorContains(t, err, "HTTP 404")
}

func TestGetIsSingleFlight(t *testing.T) {
	// 一屏媒体库会同时请求十几张图，其中同一部番的多集共用一个 URL。
	// 没有这层合并的话，同一张图会被并发下载 N 次。
	var hits int32
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&hits, 1)
		<-release // 卡住，保证后到的请求确实撞上"下载中"
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(pngBytes)
	}))
	defer srv.Close()

	c := newTestCache(t)
	// 绕过 validateURL 的 https 要求：直接压测 Get 的并发合并逻辑。
	c.hc.CheckRedirect = nil

	const n = 8
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = c.getUnvalidated(context.Background(), srv.URL)
		}(i)
	}
	// 等所有 goroutine 都进到"下载中"再放行
	close(release)
	wg.Wait()

	for i, err := range errs {
		require.NoErrorf(t, err, "第 %d 个请求", i)
	}
	require.EqualValues(t, 1, atomic.LoadInt32(&hits), "同一 URL 应当只下载一次")
}

func TestValidateURL(t *testing.T) {
	cases := []struct {
		name string
		url  string
		ok   bool
	}{
		{"https 放行", "https://cdn.example/a.jpg", true},
		{"http 拒绝", "http://cdn.example/a.jpg", false},
		{"file 拒绝", "file:///etc/passwd", false},
		{"没有主机名", "https:///a.jpg", false},
		{"畸形", "https://%zz", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateURL(tc.url)
			if tc.ok {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
		})
	}
}

func TestCheckRedirect(t *testing.T) {
	mk := func(raw string) *http.Request {
		r, err := http.NewRequest(http.MethodGet, raw, nil)
		require.NoError(t, err)
		return r
	}
	// CDN 常见的一跳：放行。
	require.NoError(t, checkRedirect(mk("https://edge.example/a.jpg"), nil))
	// 降级到明文：拒绝。地址是上游给的，不该有权把我们踢到 http。
	require.ErrorContains(t,
		checkRedirect(mk("http://edge.example/a.jpg"), nil), "非 https")
	// 跳数上限：防重定向环把请求拖到超时。
	via := make([]*http.Request, maxRedirects)
	require.ErrorContains(t,
		checkRedirect(mk("https://edge.example/a.jpg"), via), "超过")
}

func TestIsPrivate(t *testing.T) {
	// 这些是 SSRF 的目标面：上游返回一个解析到内网的域名，
	// 就能让 nagare 替攻击者去探测用户的局域网。
	blocked := []string{
		"127.0.0.1", "::1", "10.0.0.1", "172.16.0.1", "192.168.1.1",
		"169.254.169.254", // 云元数据服务，SSRF 的经典目标
		"0.0.0.0", "224.0.0.1", "fe80::1",
	}
	for _, s := range blocked {
		require.Truef(t, isPrivate(net.ParseIP(s)), "%s 应当被拦", s)
	}
	for _, s := range []string{"1.1.1.1", "93.184.216.34", "2606:4700::1111"} {
		require.Falsef(t, isPrivate(net.ParseIP(s)), "%s 是公网，不该被拦", s)
	}
}

func TestGuardedDialRejectsLoopback(t *testing.T) {
	// 端到端确认那道闸真的接在拨号上：localhost 必然解析到回环。
	_, err := guardedDial(context.Background(), "tcp", "localhost:80")
	require.ErrorContains(t, err, "没有可用的公网地址")
}

func TestPathIsStableAndDistinct(t *testing.T) {
	c := newTestCache(t)
	a1 := c.Path("https://a.example/x.jpg")
	a2 := c.Path("https://a.example/x.jpg")
	b := c.Path("https://a.example/y.jpg")
	require.Equal(t, a1, a2, "同一 URL 的缓存路径必须稳定")
	require.NotEqual(t, a1, b)
}
