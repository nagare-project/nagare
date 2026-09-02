package update

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testRepo = "nagare-project/nagare"

// fakeClock 是可推进的时钟；后台循环会在别的 goroutine 读它，所以加锁。
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func newClock() *fakeClock {
	return &fakeClock{t: time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// stub 是可随时换响应的假 GitHub，并记录最近一次请求的路径与头。
type stub struct {
	srv  *httptest.Server
	hits atomic.Int32

	mu         sync.Mutex
	status     int
	body       string
	lastPath   string
	lastHeader http.Header
}

func newStub(t *testing.T, status int, body string) *stub {
	t.Helper()
	s := &stub{status: status, body: body}
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.hits.Add(1)
		s.mu.Lock()
		s.lastPath, s.lastHeader = r.URL.Path, r.Header.Clone()
		status, body := s.status, s.body
		s.mu.Unlock()
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(s.srv.Close)
	return s
}

func (s *stub) set(status int, body string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status, s.body = status, body
}

func (s *stub) last() (string, http.Header) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastPath, s.lastHeader
}

func goodRelease(tag string) string {
	return fmt.Sprintf(`{"tag_name":%q,"html_url":"https://github.com/%s/releases/tag/%s","draft":false,"prerelease":false}`,
		tag, testRepo, tag)
}

// newChecker 构造指向假服务器的检查器；opts 的零值字段用测试默认值填。
func newChecker(t *testing.T, s *stub, clk *fakeClock, opts Options) *Checker {
	t.Helper()
	if opts.CacheDir == "" {
		opts.CacheDir = t.TempDir()
	}
	if opts.CurrentVersion == "" {
		opts.CurrentVersion = "0.1.0"
	}
	if opts.Client == nil && s != nil {
		opts.Client = s.srv.Client()
	}
	if opts.Now == nil && clk != nil {
		opts.Now = clk.Now
	}
	c, err := New(opts)
	require.NoError(t, err)
	if s != nil {
		c.apiBase = s.srv.URL
	}
	return c
}

func TestCheckFreshFetchesAndSendsOnlyDeclaredHeaders(t *testing.T) {
	s := newStub(t, http.StatusOK, goodRelease("v0.2.0"))
	clk := newClock()
	c := newChecker(t, s, clk, Options{CurrentVersion: "0.1.0"})

	v := c.Check(context.Background(), false)

	assert.Equal(t, int32(1), s.hits.Load())
	path, h := s.last()
	assert.Equal(t, "/repos/nagare-project/nagare/releases/latest", path)
	assert.Equal(t, "application/vnd.github+json", h.Get("Accept"))
	assert.Equal(t, "2022-11-28", h.Get("X-GitHub-Api-Version"))
	assert.Equal(t, "nagare/0.1.0", h.Get("User-Agent"))
	assert.Empty(t, h.Get("Authorization"), "隐私：不带任何凭证")
	assert.Empty(t, h.Get("Cookie"))

	assert.True(t, v.Enabled)
	assert.Equal(t, "0.1.0", v.Current)
	assert.Equal(t, "0.2.0", v.Latest)
	assert.True(t, v.Available)
	assert.Equal(t, "https://github.com/nagare-project/nagare/releases/tag/v0.2.0", v.URL)
	require.NotNil(t, v.CheckedAt)
	assert.Equal(t, clk.Now().UnixMilli(), *v.CheckedAt)
	assert.Empty(t, v.Error)
}

func TestViewBeforeAnyCheck(t *testing.T) {
	c := newChecker(t, nil, nil, Options{})
	v := c.View()
	assert.Nil(t, v.CheckedAt, "从未检查时 checkedAt 为 null")
	assert.Empty(t, v.Latest)
	assert.False(t, v.Available)
	assert.True(t, v.Enabled, "默认开启")
	assert.Empty(t, v.Error)
}

func TestCheckHitsCacheWithin24h(t *testing.T) {
	s := newStub(t, http.StatusOK, goodRelease("v0.2.0"))
	clk := newClock()
	c := newChecker(t, s, clk, Options{})
	ctx := context.Background()

	c.Check(ctx, false)
	clk.Advance(23 * time.Hour)
	v := c.Check(ctx, false)
	assert.Equal(t, int32(1), s.hits.Load(), "24 小时内不联网")
	assert.Equal(t, "0.2.0", v.Latest)

	clk.Advance(2 * time.Hour)
	s.set(http.StatusOK, goodRelease("v0.3.0"))
	v = c.Check(ctx, false)
	assert.Equal(t, int32(2), s.hits.Load(), "过期后重新联网")
	assert.Equal(t, "0.3.0", v.Latest)
}

func TestCheckForceAlwaysFetches(t *testing.T) {
	s := newStub(t, http.StatusOK, goodRelease("v0.2.0"))
	c := newChecker(t, s, newClock(), Options{})
	ctx := context.Background()

	c.Check(ctx, false)
	s.set(http.StatusOK, goodRelease("v0.3.0"))
	v := c.Check(ctx, true)
	assert.Equal(t, int32(2), s.hits.Load())
	assert.Equal(t, "0.3.0", v.Latest)
}

func TestRateLimitKeepsPreviousValue(t *testing.T) {
	for _, status := range []int{http.StatusForbidden, http.StatusTooManyRequests} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			s := newStub(t, http.StatusOK, goodRelease("v0.2.0"))
			clk := newClock()
			c := newChecker(t, s, clk, Options{})
			ctx := context.Background()
			c.Check(ctx, true)

			s.set(status, `{"message":"API rate limit exceeded"}`)
			clk.Advance(time.Hour)
			v := c.Check(ctx, true)
			assert.Equal(t, "0.2.0", v.Latest, "限流时保留旧值")
			assert.True(t, v.Available)
			assert.NotEmpty(t, v.URL)
			assert.Contains(t, v.Error, "限流")
			require.NotNil(t, v.CheckedAt)
			assert.Equal(t, clk.Now().UnixMilli(), *v.CheckedAt, "checkedAt 记录失败的这次尝试")

			s.set(http.StatusOK, goodRelease("v0.2.0"))
			v = c.Check(ctx, true)
			assert.Empty(t, v.Error, "成功后错误清空")
		})
	}
}

func TestNoReleaseYet(t *testing.T) {
	s := newStub(t, http.StatusNotFound, `{"message":"Not Found"}`)
	c := newChecker(t, s, nil, Options{})
	v := c.Check(context.Background(), true)
	assert.Empty(t, v.Latest)
	assert.Empty(t, v.URL)
	assert.False(t, v.Available)
	assert.Empty(t, v.Error, "尚无发布不是错误")
	require.NotNil(t, v.CheckedAt)
}

func TestDraftAndPrereleaseIgnored(t *testing.T) {
	cases := map[string]string{
		"draft":      `{"tag_name":"v0.2.0","html_url":"https://github.com/nagare-project/nagare/releases/tag/v0.2.0","draft":true}`,
		"prerelease": `{"tag_name":"v0.2.0","html_url":"https://github.com/nagare-project/nagare/releases/tag/v0.2.0","prerelease":true}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			s := newStub(t, http.StatusOK, body)
			c := newChecker(t, s, nil, Options{})
			v := c.Check(context.Background(), true)
			assert.Empty(t, v.Latest)
			assert.Empty(t, v.Error)
		})
	}
}

func TestAbnormalResponsesRejected(t *testing.T) {
	const good = "https://github.com/nagare-project/nagare/releases/tag/v0.2.0"
	cases := []struct {
		name   string
		status int
		body   string
		want   string
	}{
		{"标签缺 v 前缀", 200, `{"tag_name":"0.2.0","html_url":"` + good + `"}`, "响应异常"},
		{"标签不是三段", 200, goodRelease("v0.2"), "响应异常"},
		{"标签带注入字符", 200, goodRelease("v0.2.0 <script>"), "响应异常"},
		{"发布页不在 github.com", 200, `{"tag_name":"v0.2.0","html_url":"https://evil.example/nagare-project/nagare/releases/tag/v0.2.0"}`, "响应异常"},
		{"发布页是别的仓库", 200, `{"tag_name":"v0.2.0","html_url":"https://github.com/evil/nagare/releases/tag/v0.2.0"}`, "响应异常"},
		{"发布页是 http", 200, `{"tag_name":"v0.2.0","html_url":"http://github.com/nagare-project/nagare/releases/tag/v0.2.0"}`, "响应异常"},
		{"发布页是 javascript 伪协议", 200, `{"tag_name":"v0.2.0","html_url":"javascript:alert(1)"}`, "响应异常"},
		{"发布页是 data 伪协议", 200, `{"tag_name":"v0.2.0","html_url":"data:text/html,<script>alert(1)</script>"}`, "响应异常"},
		{"发布页缺失", 200, `{"tag_name":"v0.2.0"}`, "响应异常"},
		{"不是 JSON", 200, "<html>", "响应异常"},
		{"服务端 500", 500, "", "返回错误"},
		{"服务端 502", 502, "", "返回错误"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newStub(t, tc.status, tc.body)
			c := newChecker(t, s, nil, Options{})
			v := c.Check(context.Background(), true)
			assert.Contains(t, v.Error, tc.want)
			assert.Empty(t, v.Latest)
			assert.Empty(t, v.URL)
			assert.False(t, v.Available)
		})
	}
}

func TestBodyTooLargeRejected(t *testing.T) {
	body := `{"tag_name":"v0.2.0","html_url":"https://github.com/nagare-project/nagare/releases/tag/v0.2.0","body":"` +
		strings.Repeat("a", maxBodyBytes) + `"}`
	s := newStub(t, http.StatusOK, body)
	c := newChecker(t, s, nil, Options{})
	v := c.Check(context.Background(), true)
	assert.Contains(t, v.Error, "响应异常")
	assert.Empty(t, v.Latest)
}

func TestRedirectRejected(t *testing.T) {
	target := newStub(t, http.StatusOK, goodRelease("v9.9.9"))
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.srv.URL+r.URL.Path, http.StatusFound)
	}))
	t.Cleanup(redirect.Close)
	// 注入一个会跟随重定向的普通客户端，验证 New 会给它套上拒绝策略。
	c := newChecker(t, nil, nil, Options{Client: redirect.Client()})
	c.apiBase = redirect.URL

	v := c.Check(context.Background(), true)
	assert.Contains(t, v.Error, "响应异常")
	assert.Equal(t, int32(0), target.hits.Load(), "不跟随重定向")
	assert.Empty(t, v.Latest)
	assert.Nil(t, redirect.Client().CheckRedirect, "不改动调用方传入的客户端")
}

func TestNetworkFailureReportedWithoutURL(t *testing.T) {
	s := newStub(t, http.StatusOK, goodRelease("v0.2.0"))
	c := newChecker(t, s, nil, Options{})
	s.srv.Close()

	v := c.Check(context.Background(), true)
	assert.Contains(t, v.Error, "无法连接")
	require.NotNil(t, v.CheckedAt)
	require.Error(t, c.lastErr)
	assert.NotContains(t, c.lastErr.Error(), "/repos/", "日志不记请求地址")
}

func TestIsNewer(t *testing.T) {
	cases := []struct {
		current, latest string
		want            bool
	}{
		{"0.1.0", "0.2.0", true},
		{"v0.1.0", "0.2.0", true},
		{"0.1.0", "0.1.0", false},
		{"0.3.0", "0.2.0", false},
		{"0.1.0", "", false},
		{"0.1.0-dev", "0.2.0", false},
		{"0.1.0-dev.abc123", "0.2.0", false},
		{"0.1.0-development", "0.2.0", true},
		{"0.1.0-devfoo", "0.2.0", true},
		{"0.1.0-rc.1", "0.1.0", true},
		{"abc", "0.2.0", false},
		{"", "0.2.0", false},
		{"0.1.0", "garbage", false},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, isNewer(tc.current, tc.latest), "current=%q latest=%q", tc.current, tc.latest)
	}
}

func TestDevVersionReportsLatestButNotAvailable(t *testing.T) {
	s := newStub(t, http.StatusOK, goodRelease("v0.2.0"))
	c := newChecker(t, s, nil, Options{CurrentVersion: "0.1.0-dev"})
	v := c.Check(context.Background(), true)
	assert.Equal(t, "0.2.0", v.Latest, "latest 照常报告")
	assert.False(t, v.Available, "本地构建不提示更新")
	assert.Equal(t, "0.1.0-dev", v.Current)
}

func TestNewValidation(t *testing.T) {
	dir := t.TempDir()
	_, err := New(Options{Repo: "bad repo!", CacheDir: dir})
	assert.Error(t, err)
	_, err = New(Options{Repo: "a/b/c", CacheDir: dir})
	assert.Error(t, err)
	_, err = New(Options{CacheDir: "  "})
	assert.Error(t, err)

	c, err := New(Options{CacheDir: dir})
	require.NoError(t, err)
	assert.Equal(t, "https://api.github.com/repos/nagare-project/nagare/releases/latest", c.releaseURL())
	assert.Equal(t, "nagare/dev", c.userAgent(), "版本号为空时的兜底 UA")
	assert.NotNil(t, c.client.CheckRedirect)
	assert.Equal(t, defaultTimeout, c.client.Timeout)
}

func TestConcurrentChecksShareOneRequest(t *testing.T) {
	arrived := make(chan struct{}, 4)
	release := make(chan struct{})
	releaseOnce := sync.OnceFunc(func() { close(release) })
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		arrived <- struct{}{}
		<-release
		_, _ = io.WriteString(w, goodRelease("v0.2.0"))
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(releaseOnce) // 先放行 handler 再关服务器，失败时不会卡死
	c := newChecker(t, nil, nil, Options{Client: srv.Client()})
	c.apiBase = srv.URL
	ctx := context.Background()

	var wg sync.WaitGroup
	views := make([]View, 2)
	wg.Add(1)
	go func() { defer wg.Done(); views[0] = c.Check(ctx, true) }()
	<-arrived // 第一个请求已在飞
	wg.Add(1)
	go func() { defer wg.Done(); views[1] = c.Check(ctx, true) }()
	time.Sleep(50 * time.Millisecond) // 让第二个调用排到 fetchMu 上
	releaseOnce()
	wg.Wait()

	assert.Equal(t, int32(1), hits.Load(), "第二个调用复用第一个的结果，不发第二个请求")
	assert.Equal(t, "0.2.0", views[0].Latest)
	assert.Equal(t, "0.2.0", views[1].Latest)
}

// panicRT 是必定 panic 的 RoundTripper，用来验证后台循环的 recover 兜底。
type panicRT struct{}

func (panicRT) RoundTrip(*http.Request) (*http.Response, error) { panic("boom") }

func TestSafeCheckRecoversFromPanic(t *testing.T) {
	var buf bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(prev) })

	c := newChecker(t, nil, nil, Options{Client: &http.Client{Transport: panicRT{}}})
	assert.NotPanics(t, func() { c.safeCheck(context.Background()) }, "panic 不得逃出后台循环")
	assert.Contains(t, buf.String(), "不影响播放")

	// 信号量必须已经释放，后续检查不会被永久卡住。
	done := make(chan struct{})
	go func() { defer close(done); c.safeCheck(context.Background()) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("panic 后信号量未释放")
	}
}

func TestCheckAbortsWhileQueuedOnCanceledContext(t *testing.T) {
	arrived := make(chan struct{}, 1)
	release := make(chan struct{})
	releaseOnce := sync.OnceFunc(func() { close(release) })
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		arrived <- struct{}{}
		<-release
		_, _ = io.WriteString(w, goodRelease("v0.2.0"))
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(releaseOnce)
	c := newChecker(t, nil, nil, Options{Client: srv.Client()})
	c.apiBase = srv.URL

	leader := make(chan struct{})
	go func() { defer close(leader); c.Check(context.Background(), true) }()
	<-arrived // 第一个请求占住信号量

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	done := make(chan View, 1)
	go func() { done <- c.Check(ctx, true) }()
	select {
	case v := <-done:
		assert.Empty(t, v.Latest, "排队期间被取消，返回的是当前快照")
		assert.Nil(t, v.CheckedAt, "没发起检查就不记录尝试")
	case <-time.After(time.Second):
		t.Fatal("ctx 已取消却仍在信号量上等待")
	}
	releaseOnce()
	<-leader // 等 leader 写完缓存再结束，否则 TempDir 清理会撞上它
}
