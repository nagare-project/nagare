// Package artcache 缓存并转发番剧封面图。
//
// 为什么要自己转发，而不是让页面直接 <img src="https://…animego…/cover.jpg">：
//
//  1. CSP 是 `img-src 'self' data:`（见 httpserver/middleware.go）。放宽它去
//     容纳一个外部图床，等于在整条 XSS 防线上开一个洞，换来的只是省一次转发。
//  2. 隐私：直连意味着用户每看一次媒体库，浏览器就把 IP 送到 animego 的图床一次。
//     经本机转发之后，只有 nagare 进程知道那些地址。
//  3. 限速：animego 全局 1 req/s。缓存落盘之后每部番只下一次，之后永远命中本地。
//
// 调用方【不】把 URL 交给客户端 —— 客户端只给作品键，URL 由服务端从自己的
// store 里查出来。这样客户端侧压根没有「让 nagare 去请求任意地址」的入口。
//
// 缓存有总量上限（MaxBytes），超过时在后台按最近使用淘汰，媒体库与离线目录在用的封面最后才删
// （evict.go）；使用记录见 index.go。
package artcache

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	// maxImageBytes 是单张图的落盘上限。封面通常 50–300KB，8MB 已经极宽松。
	maxImageBytes = 8 << 20
	// fetchTimeout 是一次下载的整体超时。
	fetchTimeout = 20 * time.Second
)

// Cache 是封面图的磁盘缓存。零值不可用，用 New 构造。
type Cache struct {
	dir string
	hc  *http.Client

	// inflight 让同一张图的并发请求只下载一次：媒体库一屏会同时请求
	// 十几张图，其中同一部番的多个条目共用一个 URL。
	mu       sync.Mutex
	inflight map[string]chan struct{}

	// 海报取色（tone.go）：内存里的结果、排队中的封面、同时解码一张的信号量。
	// toneWG 让测试能等后台取色做完再收拾临时目录。
	toneMu      sync.Mutex
	toneMemo    map[string]toneResult
	tonePending map[string]bool
	toneSem     chan struct{}
	toneWG      sync.WaitGroup
	now         func() time.Time

	// 上限与淘汰（index.go / evict.go）。maxBytes / maxEntries 构造后不变（测试在用之前改小）。
	idx        coverIndex
	maxBytes   int64
	maxEntries int
	evictCh    chan struct{}
	pinMu      sync.Mutex
	pinned     func() []string
	pinGen     int             // SetPinned 一次加一，作废旧回调的缓存结果
	pinCache   map[string]bool // 上一次问到的在用封面（文件名），见 pinnedNames
	pinAt      time.Time
}

// File 是 Open 交出去的封面文件：Close 之前它不会被淘汰或清空。
type File interface {
	io.ReadSeekCloser
	Stat() (fs.FileInfo, error)
}

// New 构造缓存；dir 会被创建（0700，与配置目录同权限）。已有的封面按目录与 index.json 对账后接着用
// （上次没写完的临时文件、没有主人的取色边车顺手删掉）。淘汰要等 Run 起来才做。
func New(dir string) (*Cache, error) {
	dir = filepath.Clean(dir) // coverName 按 filepath.Dir 比对目录，要与 Path 拼出来的一致
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("创建封面缓存目录: %w", err)
	}
	c := &Cache{
		dir: dir,
		hc: &http.Client{
			Timeout: fetchTimeout,
			// 跟随重定向，但每一跳都要重新过 https 这一关。
			//
			// 一开始这里是「一律不跟随」，那条定过头了：图床走 CDN 是常态
			//（实测 302 到边缘节点），不跟随等于所有封面静默失败。
			// 跟随是安全的，因为真正拦内网的是 guardedDial —— 它在
			// 【每一次拨号】上生效，重定向后的那一跳同样过闸。
			// 这里只补 guardedDial 看不见的那一半：协议降级到明文。
			CheckRedirect: checkRedirect,
			Transport:     &http.Transport{DialContext: dialContext},
		},
		inflight:    map[string]chan struct{}{},
		toneMemo:    map[string]toneResult{},
		tonePending: map[string]bool{},
		toneSem:     make(chan struct{}, 1),
		now:         time.Now,
		idx:         coverIndex{entries: map[string]coverEntry{}, inUse: map[string]int{}},
		maxBytes:    MaxBytes,
		maxEntries:  maxCacheEntries,
		evictCh:     make(chan struct{}, 1),
	}
	if err := c.loadIndex(); err != nil {
		return nil, err
	}
	return c, nil
}

// Path 返回某个 URL 的缓存文件路径（不保证存在）。
func (c *Cache) Path(rawURL string) string {
	return filepath.Join(c.dir, coverFileName(rawURL))
}

// Open 取回封面（必要时先下载）并打开，供 /art 端点转发；记一次使用。
// 返回的文件在 Close 之前不会被淘汰或清空 —— 调用方必须 Close。
func (c *Cache) Open(ctx context.Context, rawURL string) (File, error) {
	if err := validateURL(rawURL); err != nil {
		return nil, err
	}
	return c.openUnvalidated(ctx, rawURL)
}

// openAttempts：下载完（或命中）到打开之间被淘汰 / 清空了就重新取，最多这么多次。
// 缓存塞满了在用的封面时，刚下好的不在用的那张最先被淘汰，一次重试可能不够。
const openAttempts = 3

// openUnvalidated 是 Open 去掉入口校验的那一半（理由同 getUnvalidated）。
func (c *Cache) openUnvalidated(ctx context.Context, rawURL string) (File, error) {
	var err error
	for attempt := 0; attempt < openAttempts; attempt++ {
		var path string
		if path, err = c.getUnvalidated(ctx, rawURL); err != nil {
			return nil, err
		}
		var f *coverFile
		if f, err = c.openTracked(path); err == nil {
			return f, nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			break
		}
	}
	return nil, fmt.Errorf("打开封面: %w", err)
}

// getUnvalidated 是 Open 的前一半去掉入口校验：命中缓存 / 合并并发 / 下载，返回缓存文件路径。
// 拆出来是为了让并发合并这条路径可测 —— httptest 起的是 http://，
// 过不了 validateURL 的 https 门槛。生产路径只经 Open，不直接调它。
func (c *Cache) getUnvalidated(ctx context.Context, rawURL string) (string, error) {
	path := c.Path(rawURL)
	if st, err := os.Stat(path); err == nil && st.Size() > 0 {
		return path, nil
	}

	// 同一张图只下一次：后到的请求等前一个下完，再走上面的命中分支。
	c.mu.Lock()
	if ch, busy := c.inflight[rawURL]; busy {
		c.mu.Unlock()
		select {
		case <-ch:
		case <-ctx.Done():
			return "", ctx.Err()
		}
		if st, err := os.Stat(path); err == nil && st.Size() > 0 {
			return path, nil
		}
		return "", errors.New("封面下载失败")
	}
	done := make(chan struct{})
	c.inflight[rawURL] = done
	c.mu.Unlock()

	defer func() {
		c.mu.Lock()
		delete(c.inflight, rawURL)
		c.mu.Unlock()
		close(done)
	}()

	if err := c.download(ctx, rawURL, path); err != nil {
		return "", err
	}
	// 新封面落盘就排队取色：等下一次拼目录响应时，颜色多半已经算好了
	c.scheduleTone(path)
	return path, nil
}

// download 取回一张图并原子落盘。
func (c *Cache) download(ctx context.Context, rawURL, dest string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return fmt.Errorf("构造封面请求: %w", err)
	}
	// 优先要 JPEG / PNG：取色只用标准库的解码器，图床按 Accept 换成 WebP 就取不到颜色了。
	// 其他格式仍然收（界面照样能显示），只是没有海报色。
	req.Header.Set("Accept", "image/jpeg,image/png;q=0.9,image/*;q=0.1")

	resp, err := c.hc.Do(req)
	if err != nil {
		return fmt.Errorf("下载封面: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("下载封面: HTTP %d", resp.StatusCode)
	}
	// 只收图片：否则一个被改过的响应就能让我们把 HTML/JS 当图片缓存下来，
	// 再由本机同源地址吐回给页面。
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "image/") {
		return fmt.Errorf("封面响应不是图片：%q", ct)
	}

	tmp, err := os.CreateTemp(c.dir, dlTempPrefix+"*")
	if err != nil {
		return fmt.Errorf("创建封面临时文件: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // 成功路径上已经 Rename 走了，这里是失败清理

	n, err := io.Copy(tmp, io.LimitReader(resp.Body, maxImageBytes+1))
	closeErr := tmp.Close()
	if err != nil {
		return fmt.Errorf("写入封面: %w", err)
	}
	if closeErr != nil {
		return fmt.Errorf("写入封面: %w", closeErr)
	}
	if n > maxImageBytes {
		return fmt.Errorf("封面超过 %d MB 上限", maxImageBytes>>20)
	}
	if n == 0 {
		return errors.New("封面响应为空")
	}
	if err := os.Chmod(tmpName, 0o600); err != nil {
		return fmt.Errorf("设置封面权限: %w", err)
	}
	// 改名与记账在同一把锁里（见 index.go 的 coverIndex），超限时通知后台淘汰
	if err := c.commitFile(tmpName, dest, n); err != nil {
		return fmt.Errorf("落盘封面: %w", err)
	}
	return nil
}

// maxRedirects 是允许的最大跳数。CDN 一般一跳，5 已经很宽松；
// 上限存在的意义是不让一个重定向环把请求拖到超时。
const maxRedirects = 5

// checkRedirect 校验每一跳：只允许 https，且跳数有上限。
// 内网地址由 guardedDial 在拨号时拦下，这里不重复判断 —— 在这里判会有
// TOCTOU：判断用的是域名，真正连接时再解析一次可能已经换了答案。
func checkRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= maxRedirects {
		return fmt.Errorf("封面重定向超过 %d 跳", maxRedirects)
	}
	if req.URL.Scheme != "https" {
		return fmt.Errorf("封面重定向到非 https 地址（%s）", req.URL.Scheme)
	}
	return nil
}

// validateURL 做请求【发出前】的检查。
// 与 guardedDial 是两道独立的闸：这一道挡明文与畸形地址，
// 那一道挡「域名解析到内网」——后者是前者看不见的。
func validateURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("封面地址无法解析: %w", err)
	}
	if u.Scheme != "https" {
		return fmt.Errorf("封面地址必须是 https，收到 %q", u.Scheme)
	}
	if u.Host == "" {
		return errors.New("封面地址缺少主机名")
	}
	return nil
}

// dialContext 是包级注入点：测试要连 httptest 起的本机服务，
// 而 guardedDial 的全部职责就是拦掉本机与内网。两者天然冲突，
// 所以把它做成变量，测试替换后再单独测 guardedDial 自己的判断。
var dialContext = guardedDial

// guardedDial 在真正连接前拒绝内网地址。
//
// 这道闸防的是：animego 的接口返回一个解析到 127.0.0.1 或 192.168.x 的域名，
// 让 nagare 替攻击者去探测用户的内网（SSRF）。地址是从上游响应里来的，
// 不是用户输入的，所以这是「上游被攻破」这一档的防御，但代价只有几行。
func guardedDial(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	var d net.Dialer
	for _, ip := range ips {
		if isPrivate(ip.IP) {
			continue
		}
		// 直接连已判定过的那个 IP，不再走一次域名解析 —— 否则两次解析之间
		// 上游可以换答案（DNS rebinding），判断就白做了。
		conn, err := d.DialContext(ctx, network, net.JoinHostPort(ip.IP.String(), port))
		if err == nil {
			return conn, nil
		}
	}
	return nil, fmt.Errorf("封面主机 %s 没有可用的公网地址", host)
}

// isPrivate 判断是否为不该被访问的地址段。
func isPrivate(ip net.IP) bool {
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast()
}
