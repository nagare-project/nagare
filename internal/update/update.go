// Package update 检查 GitHub Releases 上有没有 nagare 的新版本 —— 只提示，不自动更新（M4 阶段 A）。
//
// 隐私：唯一的对外请求是 GET https://api.github.com/repos/<Repo>/releases/latest，
// 请求头只有 Accept / X-GitHub-Api-Version / User-Agent（nagare/<版本>）。
// 除了出口 IP 和这个 UA，不向 GitHub 暴露任何信息：不带 cookie、不带 token、不带机器标识。
// 用户可在设置里关闭（enabled=false），关闭后后台不再联网；手动点「检查更新」仍会查。
//
// 结果缓存在 <CacheDir>/update.json：24 小时内的非强制检查直接命中缓存，不打 GitHub。
// 检查失败（网络 / 限流 / 响应异常）不覆盖缓存里的旧值，只在 View.Error 里报告。
package update

import (
	"context"
	"errors"
	"log"
	"net/http"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	errs "github.com/nagare-project/nagare/internal/errors"
	"golang.org/x/mod/semver"
)

const (
	// DefaultRepo 是官方仓库（owner/name）。
	DefaultRepo = "nagare-project/nagare"
	// cacheTTL 是非强制检查的缓存有效期。
	cacheTTL = 24 * time.Hour
	// defaultStartDelay 是启动后首次后台检查的延迟（让播放器先起来，再做不急的事）。
	defaultStartDelay = 3 * time.Second
	// defaultInterval 是后台检查的间隔。
	defaultInterval = 24 * time.Hour
	// defaultTimeout 是未注入 Client 时的整请求超时。
	defaultTimeout = 10 * time.Second
)

var repoRE = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)

// Options 是 New 的配置。
type Options struct {
	CurrentVersion string           // 如 "0.1.0" 或 "0.1.0-dev"
	Repo           string           // 默认 DefaultRepo
	CacheDir       string           // 落盘目录（配置目录），文件 <CacheDir>/update.json
	Client         *http.Client     // nil 用默认（10s 超时）；注入的客户端也会被套上「拒绝重定向」
	Now            func() time.Time // 测试注入；nil 用 time.Now
}

// View 是给界面的只读快照。
type View struct {
	Enabled   bool   `json:"enabled"`
	Current   string `json:"current"`
	Latest    string `json:"latest"` // 形如 "0.2.0"（不带 v 前缀）；尚无发布时为空
	Available bool   `json:"available"`
	URL       string `json:"url"`
	CheckedAt *int64 `json:"checkedAt"` // 上次检查（含失败）的毫秒时间戳；从未检查为 null
	Error     string `json:"error"`     // 上次检查的用户可读错误；成功为空串
}

// Checker 是更新检查器。并发安全：状态由 mu 保护，网络请求由 fetchMu 串行化。
type Checker struct {
	current   string
	repo      string
	apiBase   string // 测试改成 httptest 地址
	cachePath string
	client    *http.Client
	now       func() time.Time

	startDelay time.Duration
	interval   time.Duration

	mu        sync.Mutex
	st        cacheFile // 落盘的部分
	lastErr   error     // 本进程内上次检查的错误（不落盘）
	attemptAt int64     // 本进程内上次检查（含失败）的毫秒时间戳（不落盘）
	gen       uint64    // 每完成一次网络检查加一，让排队的调用方复用结果

	// fetchSem 串行化网络请求（容量 1 的信号量）：多个 Check 同时到达时只有第一个
	// 真正联网，其余排队，醒来后发现 gen 变了就直接用那次的结果（即 singleflight）。
	// 用 channel 而非 Mutex 是为了排队期间也能响应 ctx 取消。
	fetchSem chan struct{}
}

// New 构造检查器并读入缓存。缓存文件缺失或损坏不是错误（见 loadCache）。
func New(opts Options) (*Checker, error) {
	repo := opts.Repo
	if repo == "" {
		repo = DefaultRepo
	}
	if !repoRE.MatchString(repo) {
		return nil, errs.New(errs.CategoryInput, "update.new", "更新仓库名不合法（应为 owner/name）", "")
	}
	if strings.TrimSpace(opts.CacheDir) == "" {
		return nil, errs.New(errs.CategoryInput, "update.new", "更新缓存目录不能为空", "")
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	c := &Checker{
		current:    strings.TrimSpace(opts.CurrentVersion),
		repo:       repo,
		apiBase:    githubAPIBase,
		cachePath:  filepath.Join(opts.CacheDir, cacheFileName),
		client:     newClient(opts.Client),
		now:        now,
		startDelay: defaultStartDelay,
		interval:   defaultInterval,
		fetchSem:   make(chan struct{}, 1),
	}
	c.st = loadCache(c.cachePath, repo)
	return c, nil
}

// newClient 给客户端套上「拒绝重定向」：latest 端点不该 3xx，一旦跟随就有被引到别处的风险，
// 让 3xx 原样返回、按响应异常处理（与 animego 客户端同一做法）。不改动调用方传入的对象。
func newClient(base *http.Client) *http.Client {
	hc := &http.Client{Timeout: defaultTimeout}
	if base != nil {
		cp := *base
		hc = &cp
	}
	hc.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return hc
}

// View 返回当前快照，不联网。
func (c *Checker) View() View {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.viewLocked()
}

func (c *Checker) viewLocked() View {
	v := View{
		Enabled:   c.st.enabled(),
		Current:   c.current,
		Latest:    c.st.Latest,
		Available: isNewer(c.current, c.st.Latest),
		URL:       c.st.URL,
		Error:     userMessage(c.lastErr),
	}
	if at := max(c.attemptAt, c.st.LastCheckAt); at != 0 {
		v.CheckedAt = &at
	}
	return v
}

// Check 检查更新。force=false 时 24 小时内的缓存直接返回，不联网；force=true 总是联网。
// 失败不覆盖缓存里的旧值（限流时尤其重要），只记进 View.Error。
//
// 同一时刻只会有一个网络请求在飞：后来者等前者结束后直接复用其结果 —— 因此返回的结果
// 可能来自并发的另一个调用方、由它的 ctx 决定成败，而不是本次调用的 ctx。
// 排队期间 ctx 被取消则立刻返回当前快照，不记录这次尝试。
func (c *Checker) Check(ctx context.Context, force bool) View {
	v, _ := c.check(ctx, force)
	return v
}

// check 是 Check 的内部版本，额外返回底层错误供后台循环写日志（View.Error 只有用户提示）。
func (c *Checker) check(ctx context.Context, force bool) (View, error) {
	c.mu.Lock()
	if !force && c.freshLocked() {
		defer c.mu.Unlock()
		return c.viewLocked(), c.lastErr
	}
	startGen := c.gen
	c.mu.Unlock()

	select {
	case c.fetchSem <- struct{}{}:
	case <-ctx.Done():
		// 没发起任何检查，不记录尝试，原样返回当前快照。
		return c.View(), ctx.Err()
	}
	defer func() { <-c.fetchSem }()

	c.mu.Lock()
	if c.gen != startGen || (!force && c.freshLocked()) {
		// 排队期间别人已经查过一次，直接用那次的结果。
		defer c.mu.Unlock()
		return c.viewLocked(), c.lastErr
	}
	c.mu.Unlock()

	rel, err := c.fetchLatest(ctx)

	c.mu.Lock()
	defer c.mu.Unlock()
	c.gen++
	c.attemptAt = c.now().UnixMilli()
	c.lastErr = err
	if err != nil {
		return c.viewLocked(), err
	}
	c.st.LastCheckAt = c.attemptAt
	c.st.Latest = rel.Version
	c.st.URL = rel.URL
	if err := saveCache(c.cachePath, c.st); err != nil {
		// 缓存写不进去只是下次启动多查一次，不该让这次检查失败；但不能静默。
		log.Printf("update: 写入更新缓存失败（下次启动会重新检查）：%v", err)
	}
	return c.viewLocked(), nil
}

// freshLocked 判断上次成功检查是否还在缓存有效期内。
func (c *Checker) freshLocked() bool {
	if c.st.LastCheckAt == 0 {
		return false
	}
	return c.now().Sub(time.UnixMilli(c.st.LastCheckAt)) < cacheTTL
}

// SetEnabled 开关后台检查并落盘。关闭后 Start 的定时检查不再联网；手动 Check 不受影响。
// 落盘失败时内存态仍已切换（本次运行内生效），错误返回给调用方提示用户。
func (c *Checker) SetEnabled(enabled bool) (View, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.st.Enabled = &enabled
	if err := saveCache(c.cachePath, c.st); err != nil {
		return c.viewLocked(), errs.Wrap(errs.CategoryFS, "update.set_enabled",
			"保存更新设置失败", "请检查配置目录是否可写", err)
	}
	return c.viewLocked(), nil
}

// Start 启动后台循环：启动 3 秒后首次检查，之后每 24 小时一次；ctx 取消即退出。
// 关闭（enabled=false）时跳过联网。每次检查记一行日志。
func (c *Checker) Start(ctx context.Context) {
	go c.loop(ctx)
}

func (c *Checker) loop(ctx context.Context) {
	timer := time.NewTimer(c.startDelay)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		c.safeCheck(ctx)
		timer.Reset(c.interval)
	}
}

// safeCheck 给后台的一次检查兜底 recover：更新检查是可选功能，
// 绝不能因为它 panic 把整个进程（连同正在播放的 mpv 会话）带走。
func (c *Checker) safeCheck(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("update: 后台检查异常，已忽略（不影响播放）：%v", r)
		}
	}()
	c.scheduledCheck(ctx)
}

// scheduledCheck 是后台循环的一次检查；结果各记一行日志（不含 URL）。
func (c *Checker) scheduledCheck(ctx context.Context) {
	if !c.View().Enabled {
		log.Printf("update: 更新检查已关闭，跳过本次后台检查")
		return
	}
	v, err := c.check(ctx, false)
	switch {
	case ctx.Err() != nil:
		// 退出时取消掉的检查不算失败，不刷日志。
		return
	case err != nil:
		log.Printf("update: 检查更新失败：%v", err)
	case v.Latest == "":
		log.Printf("update: 尚无正式发布版本（当前 %s）", v.Current)
	case v.Available:
		log.Printf("update: 发现新版本 %s（当前 %s）", v.Latest, v.Current)
	default:
		log.Printf("update: 已是最新版本（当前 %s，最新 %s）", v.Current, v.Latest)
	}
}

// isNewer 判断 latest 是否比 current 新。current 不是合法 semver、或带 -dev 预发布标记
// （本地构建）时一律 false —— 但 latest 照常报告给界面。
func isNewer(current, latest string) bool {
	if latest == "" {
		return false
	}
	cur, lat := canon(current), canon(latest)
	if !semver.IsValid(cur) || !semver.IsValid(lat) {
		return false
	}
	// 只有本地构建约定的 -dev / -dev.xxx 才压掉提示；-development 这类是普通预发布。
	if pre := semver.Prerelease(cur); pre == "-dev" || strings.HasPrefix(pre, "-dev.") {
		return false
	}
	return semver.Compare(lat, cur) > 0
}

// canon 给版本号补上 semver 包要求的 v 前缀。
func canon(v string) string {
	v = strings.TrimSpace(v)
	if v == "" || strings.HasPrefix(v, "v") {
		return v
	}
	return "v" + v
}

// userMessage 取错误的用户可读提示；nil 返回空串。
func userMessage(err error) string {
	if err == nil {
		return ""
	}
	var ce *errs.E
	if errors.As(err, &ce) {
		return ce.UserFacing()
	}
	return err.Error()
}
