// 会话管理：登录、access token 刷新、refresh cookie 的捕获与轮换。
//
// 服务端契约：access token 15 分钟过期；refresh token 7 天、装在 httpOnly 的
// `refreshToken` cookie 里，/api/auth/refresh 只认 cookie 且可能轮换它。
// 本包把 cookie 值当普通字符串保存在 Session 里，由调用方决定怎么落盘。
package animego

import (
	"context"
	"errors"
	"fmt"
	"net/http"
)

// refreshCookieName 与服务端 auth/cookies.go 的 RefreshCookieName 保持一致。
const refreshCookieName = "refreshToken"

// Session 是可持久化的会话状态。登录/刷新后内容会变化，
// 调用方应在成功的鉴权操作后读 Session() 并存盘（与 token 同级敏感，0600）。
type Session struct {
	// AccessToken 是 15 分钟寿命的 JWT，请求时装进 Authorization: Bearer。
	AccessToken string
	// RefreshCookie 是 refreshToken cookie 的值（7 天寿命），只用于刷新。
	RefreshCookie string
}

// User 是登录响应里的用户信息（服务端 SafeUser 的子集，宽松解析、多余字段忽略）。
type User struct {
	ID        string `json:"id"` // 服务端用 UUID 字符串
	Username  string `json:"username"`
	Email     string `json:"email"`
	AvatarURL string `json:"avatarUrl"`
}

// Login 用邮箱密码登录，成功后客户端内部持有会话。
// 登录端点限速 10 次/15 分钟 —— 本方法不做任何自动重试，失败直接返回分类错误。
func (c *Client) Login(ctx context.Context, email, password string) (User, error) {
	const op = "login"
	if email == "" || password == "" {
		return User{}, &Error{Kind: ErrBadRequest, Op: op, Err: errors.New("邮箱与密码不能为空")}
	}
	body, err := marshalJSON(op, struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}{email, password})
	if err != nil {
		return User{}, err
	}

	res, err := c.send(ctx, op, http.MethodPost, "/api/auth/login", body, nil)
	if err != nil {
		return User{}, err
	}
	if err := classifyStatus(op, res, false); err != nil {
		return User{}, err
	}

	// 成功响应带 data 信封：{"data":{"accessToken":"...","user":{...}}}。
	var env struct {
		Data struct {
			AccessToken string `json:"accessToken"`
			User        User   `json:"user"`
		} `json:"data"`
	}
	if err := decodeJSON(op, res.body, &env); err != nil {
		return User{}, err
	}
	if env.Data.AccessToken == "" {
		return User{}, &Error{Kind: ErrDecode, Op: op, Err: errors.New("登录响应缺少 data.accessToken")}
	}

	// 登录建立全新会话：旧 cookie 一律丢弃（可能属于另一个账号）。
	// 契约上服务端一定 Set-Cookie；万一缺席也不算登录失败 —— access token
	// 仍可用 15 分钟，只是到期后刷新会以 ErrAuthExpired 浮出，不会静默。
	c.setSession(Session{
		AccessToken:   env.Data.AccessToken,
		RefreshCookie: refreshCookieFrom(res.header),
	})
	return env.Data.User, nil
}

// RestoreSession 恢复此前存盘的会话（比如进程重启后从 config 读回）。
// 刻意【不】触发 OnSessionChange：这是把盘上的东西读回内存，不是新状态。
func (c *Client) RestoreSession(s Session) {
	c.notifyMu.Lock()
	defer c.notifyMu.Unlock()
	c.sessionMu.Lock()
	c.session = s
	c.sessionGeneration++
	c.sessionMu.Unlock()
}

// Session 返回当前会话快照。登录/刷新后内容可能已变化，调用方负责存盘。
func (c *Client) Session() Session {
	c.sessionMu.Lock()
	defer c.sessionMu.Unlock()
	return c.session
}

// LoggedIn 报告是否持有可用（或可刷新）的会话。
func (c *Client) LoggedIn() bool {
	s := c.Session()
	return s.AccessToken != "" || s.RefreshCookie != ""
}

// setSession 整体覆盖会话，变了就通知调用方落盘。
func (c *Client) setSession(s Session) {
	c.sessionMu.Lock()
	changed := c.session != s
	c.session = s
	c.sessionGeneration++
	c.sessionMu.Unlock()
	if changed {
		c.notifySession(s)
	}
}

// notifySession 在【锁外】把新会话交给调用方持久化。
// 必须锁外：回调若回头调 Session() 就会自锁死（sync.Mutex 不可重入）。
func (c *Client) notifySession(s Session) {
	c.notifyMu.Lock()
	defer c.notifyMu.Unlock()
	// 旧通知不能在较新的登录或退出之后覆盖磁盘会话。
	if c.Session() != s {
		return
	}
	if c.onSessionChange != nil {
		c.onSessionChange(s)
	}
}

// refreshSession 在鉴权请求撞上 401 后刷新 access token，返回可用的新 token。
// staleToken 是触发 401 时所用的旧 token：拿到 refreshMu 后若发现当前 token
// 已经不是它，说明别的 goroutine 刚刷新过，直接复用，不再打 refresh 端点。
// 这就是「并发 401 只 refresh 一次」的全部实现 —— 排队者醒来即得新 token。
func (c *Client) refreshSession(ctx context.Context, op, staleToken string) (string, error) {
	c.refreshMu.Lock()
	defer c.refreshMu.Unlock()

	if err := c.checkAccount(ctx); err != nil {
		return "", err
	}
	cur := c.Session()
	if cur.AccessToken != "" && cur.AccessToken != staleToken {
		return cur.AccessToken, nil
	}
	if cur.RefreshCookie == "" {
		return "", &Error{Kind: ErrAuthExpired, Op: op, Err: errors.New("会话已过期且没有 refresh cookie")}
	}
	return c.doRefresh(ctx, op, cur.RefreshCookie)
}

// doRefresh 真正调用 /api/auth/refresh。只能在持有 refreshMu 时调用。
// 端点只认 cookie；响应可能轮换 Set-Cookie，新值要覆盖保存。
func (c *Client) doRefresh(ctx context.Context, op, cookie string) (string, error) {
	res, err := c.send(ctx, op, http.MethodPost, "/api/auth/refresh", nil, func(r *http.Request) {
		r.AddCookie(&http.Cookie{Name: refreshCookieName, Value: cookie})
	})
	if err != nil {
		return "", err
	}
	if err := c.checkAccount(ctx); err != nil {
		return "", err
	}
	if res.status == http.StatusUnauthorized {
		// refresh token 本身已失效：清空会话，让 LoggedIn() 如实反映现状。
		c.sessionMu.Lock()
		if epoch, ok := ctx.Value(accountContextKey{}).(uint64); ok && epoch != c.sessionGeneration {
			c.sessionMu.Unlock()
			return "", &Error{Kind: ErrAuthExpired, Op: op, Err: errors.New("账号已变更")}
		}
		c.session = Session{}
		c.sessionGeneration++
		c.sessionMu.Unlock()
		c.notifySession(Session{})
		cause := fmt.Errorf("refresh token 已失效（HTTP 401）")
		if msg := serverMessage(res.body); msg != "" {
			cause = fmt.Errorf("refresh token 已失效（HTTP 401: %s）", msg)
		}
		return "", &Error{Kind: ErrAuthExpired, Op: op, Err: cause}
	}
	if err := classifyStatus(op, res, false); err != nil {
		// 5xx/429 等暂时性失败不清会话：refresh cookie 可能仍然有效。
		return "", err
	}

	var env struct {
		Data struct {
			AccessToken string `json:"accessToken"`
		} `json:"data"`
	}
	if err := decodeJSON(op, res.body, &env); err != nil {
		return "", err
	}
	if env.Data.AccessToken == "" {
		return "", &Error{Kind: ErrDecode, Op: op, Err: errors.New("刷新响应缺少 data.accessToken")}
	}

	c.sessionMu.Lock()
	if epoch, ok := ctx.Value(accountContextKey{}).(uint64); ok && epoch != c.sessionGeneration {
		c.sessionMu.Unlock()
		return "", &Error{Kind: ErrAuthExpired, Op: op, Err: errors.New("账号已变更")}
	}
	c.session.AccessToken = env.Data.AccessToken
	if rotated := refreshCookieFrom(res.header); rotated != "" {
		c.session.RefreshCookie = rotated
	}
	updated := c.session
	c.sessionMu.Unlock()
	// 刷新总是换掉 access token，必然是新状态 —— 不比较直接通知。
	// 这里落盘发生在 refreshMu 之内，排队的 401 会多等一次小文件写；
	// 换来的是「轮换后的 cookie 一定在磁盘上」，这笔交易划算：
	// 丢一次轮换＝用户下次启动静默掉线，多等几毫秒＝没人察觉。
	c.notifySession(updated)
	return env.Data.AccessToken, nil
}

// refreshCookieFrom 从响应头里解析 refreshToken cookie 的值，没有则返回空串。
func refreshCookieFrom(h http.Header) string {
	resp := http.Response{Header: h}
	for _, ck := range resp.Cookies() {
		if ck.Name == refreshCookieName && ck.Value != "" {
			return ck.Value
		}
	}
	return ""
}
