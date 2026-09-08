// Package animego 是 animegoclub.com 的 API 客户端。
//
// 项目红线：nagare 与 animego 之间只有三条允许的连线，全部是普通已认证 HTTPS ——
// 元数据（读）、弹幕（读）、观看进度（写）。本包只承载这三条；
// 磁力/资源相关的任何能力永远不进此包，也不得新增「按 anilistId 要资源」类接口。
//
// 关于限速：animego 全局限速 1 req/s（突发 60），登录端点另有 10 次/15 分钟。
// M1 的调用量极小（播放前一次 match + 一次弹幕拉取 + 零星进度写），
// 收藏的逐集撤销现在需要批量调用，由 lists.go 在每次请求前节流。
// 同理，本包不做自动重试 —— 尤其不能拿登录端点练手。
//
// 所有失败以 *Error 返回，调用方用 errors.As 取 Kind 决定降级策略（决议 CQ3）：
// 弹幕/匹配拿不到时播放必须照常进行，那是调用方的责任，本包保证错误可分类。
package animego

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// DefaultBaseURL 是生产环境地址；开发时用 Options.BaseURL 指向本地实例。
const DefaultBaseURL = "https://animegoclub.com"

const (
	// defaultTimeout 是未注入 Options.HTTPClient 时的整请求超时。
	defaultTimeout = 15 * time.Second
	// defaultUserAgent 是调用方未注入版本号时的兜底 UA。
	defaultUserAgent = "nagare/dev"
	// maxResponseBytes 是单个响应体的读取上限，防异常响应炸内存。
	maxResponseBytes = 8 << 20
)

// Options 是 New 的配置。零值可用（指向生产环境）。
type Options struct {
	// ListRequestInterval 控制批量收藏调用间隔；零值一秒，测试可设负数禁用。
	ListRequestInterval time.Duration
	// BaseURL 形如 https://animegoclub.com；空则用 DefaultBaseURL，末尾斜杠会被剥掉。
	BaseURL string
	// HTTPClient 可注入自定义客户端（测试/代理）；空则用 defaultTimeout 的默认值。
	// 注意不要带 CookieJar：refresh cookie 由本包自行管理，Jar 会重复携带。
	HTTPClient *http.Client
	// UserAgent 形如 "nagare/1.2.3"，版本号由调用方注入；空则用 defaultUserAgent。
	UserAgent string
	// OnSessionChange 在会话【真的变了】之后调用（登录、刷新轮换、refresh 失效清空），
	// 由调用方落盘。RestoreSession 不触发 —— 那是把盘上的东西读回内存，不是新状态。
	//
	// 为什么由本包主动通知，而不是让调用方「在可能刷新过之后记得去取」：
	// 后者是照着记性写的契约，漏一个调用点就是一次静默掉线（曾经漏过一个：
	// 播放开始时的 match 会触发 401 刷新，而用户看一半停掉时那条路径不落盘，
	// 轮换后的 cookie 从没写进磁盘，下次启动拿着已作废的旧 cookie）。
	//
	// 回调在会话锁外串行执行，参数是快照；可读取 Session()，但不能再次变更会话，
	// 也不要做慢活儿：刷新链路上所有等着的 goroutine 都在它后面排队。
	OnSessionChange func(Session)
}

// Client 是 animego API 客户端。并发安全：会话状态由锁保护，可被多 goroutine 共用。
// token 的持久化由调用方负责（登录/刷新后取 Session() 落 config），这里只管内存态。
type Client struct {
	sessionGeneration   uint64
	listRateMu          sync.Mutex
	nextListRequest     time.Time
	listRequestInterval time.Duration
	baseURL             string
	hc                  *http.Client
	ua                  string

	// sessionMu 保护 session 的读写（短持有，不跨网络请求）。
	sessionMu sync.Mutex
	notifyMu  sync.Mutex
	session   Session

	// refreshMu 串行化 token 刷新（持有期间会发网络请求）：多个 goroutine
	// 同时撞 401 时只有第一个真正打 refresh 端点，其余在锁上排队，醒来后
	// 发现 token 已换新就直接复用 —— 互斥锁在这里就是 singleflight。
	refreshMu sync.Mutex

	// onSessionChange 见 Options.OnSessionChange。构造后只读，无需加锁。
	onSessionChange func(Session)
}

// New 构造客户端。
func New(opts Options) *Client {
	base := opts.BaseURL
	if base == "" {
		base = DefaultBaseURL
	}
	hc := opts.HTTPClient
	if hc == nil {
		// 不跟随重定向：这三个端点都不该重定向；一旦跟随，Bearer/refresh cookie
		// 有跟着跑到别的主机的风险。让 3xx 原样返回、按普通响应分类，fail closed。
		hc = &http.Client{
			Timeout:       defaultTimeout,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		}
	}
	ua := opts.UserAgent
	if ua == "" {
		ua = defaultUserAgent
	}
	interval := opts.ListRequestInterval
	if interval == 0 {
		interval = time.Second
	}
	return &Client{
		listRequestInterval: interval,
		baseURL:             strings.TrimRight(base, "/"),
		hc:                  hc,
		ua:                  ua,
		onSessionChange:     opts.OnSessionChange,
	}
}

// httpResult 是一次已完成请求的原始结果；HTTP 状态码的分类交给上层。
type httpResult struct {
	status int
	header http.Header
	body   []byte
}

// send 发出一次请求并读回响应（限读 maxResponseBytes）。
// 网络层失败归为 ErrUnavailable；body 传字节而非 io.Reader，
// 是为了 401 刷新后能原样重放同一份请求体。
func (c *Client) send(ctx context.Context, op, method, path string, body []byte, mod func(*http.Request)) (*httpResult, error) {
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, rd)
	if err != nil {
		return nil, &Error{Kind: ErrBadRequest, Op: op, Err: fmt.Errorf("构造请求: %w", err)}
	}
	req.Header.Set("User-Agent", c.ua)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if mod != nil {
		mod(req)
	}

	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, &Error{Kind: ErrUnavailable, Op: op, Err: err}
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return nil, &Error{Kind: ErrUnavailable, Op: op, Err: fmt.Errorf("读取响应: %w", err)}
	}
	if len(data) > maxResponseBytes {
		return nil, &Error{Kind: ErrDecode, Op: op, Err: fmt.Errorf("响应体超过 %d MB 上限", maxResponseBytes>>20)}
	}
	return &httpResult{status: resp.StatusCode, header: resp.Header.Clone(), body: data}, nil
}

// authedSend 在 send 之上叠加 Bearer 头与「401 → 刷新一次 → 重放一次」。
// access token 只有 15 分钟寿命，正常使用中撞 401 是常态而非异常。
// 返回的 httpResult 不含 401（都被翻译成刷新动作或 ErrAuthExpired），
// 其余状态码由调用方用 classifyStatus 分类。
func (c *Client) authedSend(ctx context.Context, op, method, path string, body []byte) (*httpResult, error) {
	ctx = c.accountContext(ctx)
	if err := c.checkAccount(ctx); err != nil {
		return nil, err
	}
	s := c.Session()
	if err := c.checkAccount(ctx); err != nil {
		return nil, err
	}
	if s.AccessToken == "" && s.RefreshCookie == "" {
		return nil, &Error{Kind: ErrAuthExpired, Op: op, Err: errors.New("尚未登录")}
	}
	token := s.AccessToken
	if token == "" {
		// 只剩 refresh cookie（比如上次存盘时 token 已被清）：先换新再发。
		t, err := c.refreshSession(ctx, op, "")
		if err != nil {
			return nil, err
		}
		token = t
	}

	if err := c.checkAccount(ctx); err != nil {
		return nil, err
	}
	res, err := c.send(ctx, op, method, path, body, withBearer(token))
	if accountErr := c.checkAccount(ctx); accountErr != nil {
		return nil, accountErr
	}
	if err != nil {
		return nil, err
	}
	if res.status != http.StatusUnauthorized {
		return res, nil
	}

	// 401：token 过期。刷新一次并重放原请求一次；再失败就交给用户重新登录。
	token, err = c.refreshSession(ctx, op, token)
	if err != nil {
		return nil, err
	}
	if err := c.checkAccount(ctx); err != nil {
		return nil, err
	}
	res, err = c.send(ctx, op, method, path, body, withBearer(token))
	if accountErr := c.checkAccount(ctx); accountErr != nil {
		return nil, accountErr
	}
	if err != nil {
		return nil, err
	}
	if res.status == http.StatusUnauthorized {
		return nil, &Error{Kind: ErrAuthExpired, Op: op, Err: errors.New("刷新 token 后服务端仍返回 401")}
	}
	return res, nil
}

// withBearer 返回给请求补 Authorization 头的修饰函数。
func withBearer(token string) func(*http.Request) {
	return func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+token) }
}

// classifyStatus 把非 2xx 状态码翻译成分类错误；2xx 返回 nil。
// authed 表示这是鉴权请求：401 应归为「会话过期」而不是「请求有误」
// （公开端点不该出现 401，出现了按 ErrBadRequest 处理）。
func classifyStatus(op string, res *httpResult, authed bool) error {
	if res.status >= 200 && res.status < 300 {
		return nil
	}
	cause := fmt.Errorf("HTTP %d", res.status)
	if msg := serverMessage(res.body); msg != "" {
		cause = fmt.Errorf("HTTP %d: %s", res.status, msg)
	}
	var kind ErrKind
	switch {
	case res.status == http.StatusTooManyRequests:
		kind = ErrRateLimited
	case res.status == http.StatusUnauthorized && authed:
		kind = ErrAuthExpired
	case res.status >= 400 && res.status < 500:
		kind = ErrBadRequest
	default:
		// 5xx 与其他意外状态码都按「暂时不可达」处理，稍后重试。
		kind = ErrUnavailable
	}
	return &Error{Kind: kind, Op: op, Err: cause}
}

// serverMessage 尽力从响应体里捞出服务端错误信息。animego 有两种错误信封：
// 鉴权/业务错误是 {"error":{"code":"...","message":"..."}}；
// dandanplay 代理的错误是 {"error":"..."} 的纯字符串。捞不到就返回空串。
func serverMessage(body []byte) string {
	var env struct {
		Error json.RawMessage `json:"error"`
	}
	if json.Unmarshal(body, &env) != nil || len(env.Error) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(env.Error, &s) == nil {
		return s
	}
	var obj struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if json.Unmarshal(env.Error, &obj) == nil {
		switch {
		case obj.Code != "" && obj.Message != "":
			return obj.Code + ": " + obj.Message
		case obj.Message != "":
			return obj.Message
		default:
			return obj.Code
		}
	}
	return ""
}

// decodeJSON 解析响应体，失败归为 ErrDecode。
func decodeJSON(op string, body []byte, v any) error {
	if err := json.Unmarshal(body, v); err != nil {
		return &Error{Kind: ErrDecode, Op: op, Err: fmt.Errorf("解析响应 JSON: %w", err)}
	}
	return nil
}

// marshalJSON 序列化请求体。纯数据结构不该失败，但错误不静默。
func marshalJSON(op string, v any) ([]byte, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, &Error{Kind: ErrBadRequest, Op: op, Err: fmt.Errorf("序列化请求 JSON: %w", err)}
	}
	return data, nil
}
