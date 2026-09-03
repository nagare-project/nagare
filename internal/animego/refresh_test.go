// 401 自动刷新：刷新一次并重放、refresh 失效的分类、并发 singleflight。
package animego_test

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nagare-project/nagare/internal/animego"
)

// refreshBackend 模拟「旧 token 一律 401、refresh 换新 token」的服务端。
type refreshBackend struct {
	goodToken   string // 刷新后发放、且被业务端点接受的 token
	goodCookie  string // 被 refresh 端点接受的 refresh cookie
	newCookie   string // 刷新时轮换下发的新 cookie（空则不轮换）
	failRefresh bool   // refresh 端点直接 401（refresh token 失效场景）
	alwaysDeny  bool   // 业务端点无论什么 token 都 401（刷新后仍 401 场景）
}

func (b *refreshBackend) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/auth/refresh" {
			if b.failRefresh {
				respond(w, http.StatusUnauthorized, `{"error":{"code":"INVALID_TOKEN","message":"refresh token 无效"}}`)
				return
			}
			ck, err := r.Cookie("refreshToken")
			if err != nil || ck.Value != b.goodCookie {
				respond(w, http.StatusUnauthorized, `{"error":{"code":"NO_TOKEN","message":"缺少或错误的 cookie"}}`)
				return
			}
			if b.newCookie != "" {
				http.SetCookie(w, &http.Cookie{Name: "refreshToken", Value: b.newCookie, HttpOnly: true})
			}
			respond(w, http.StatusOK, `{"data":{"accessToken":"`+b.goodToken+`"}}`)
			return
		}
		// 业务端点：只认新 token。
		if b.alwaysDeny || r.Header.Get("Authorization") != "Bearer "+b.goodToken {
			respond(w, http.StatusUnauthorized, `{"error":{"code":"INVALID_TOKEN","message":"token 过期"}}`)
			return
		}
		respond(w, http.StatusOK, `{"data":{}}`)
	}
}

func TestAuthed401RefreshReplaySuccess(t *testing.T) {
	rec := &recorder{}
	b := &refreshBackend{goodToken: "at-new", goodCookie: "rt-old", newCookie: "rt-new"}
	c := newTestClient(t, rec, b.handler())
	c.RestoreSession(animego.Session{AccessToken: "at-expired", RefreshCookie: "rt-old"})

	require.NoError(t, c.EnsureSubscription(context.Background(), 123))

	// 序列：业务 401 → refresh（带旧 cookie）→ 业务重放（带新 token）。
	subs := rec.filter("/api/subscriptions")
	refreshes := rec.filter("/api/auth/refresh")
	require.Len(t, subs, 2, "原请求 + 重放一次")
	require.Len(t, refreshes, 1)
	assert.Equal(t, "Bearer at-expired", subs[0].Auth)
	assert.Equal(t, "rt-old", refreshes[0].Cookie, "refresh 只认 cookie")
	assert.Equal(t, "Bearer at-new", subs[1].Auth)

	// 会话已更新且 cookie 轮换被覆盖保存 —— 调用方此时应把 Session() 落盘。
	assert.Equal(t, animego.Session{AccessToken: "at-new", RefreshCookie: "rt-new"}, c.Session())
}

func TestRefreshWithoutRotationKeepsCookie(t *testing.T) {
	b := &refreshBackend{goodToken: "at-new", goodCookie: "rt-keep"} // 不轮换
	c := newTestClient(t, nil, b.handler())
	c.RestoreSession(animego.Session{AccessToken: "at-expired", RefreshCookie: "rt-keep"})

	require.NoError(t, c.MarkWatched(context.Background(), 123, 7))
	assert.Equal(t, animego.Session{AccessToken: "at-new", RefreshCookie: "rt-keep"}, c.Session())
}

func TestRefreshRejectedMeansAuthExpired(t *testing.T) {
	b := &refreshBackend{failRefresh: true}
	c := newTestClient(t, nil, b.handler())
	c.RestoreSession(animego.Session{AccessToken: "at-expired", RefreshCookie: "rt-dead"})

	err := c.EnsureSubscription(context.Background(), 123)
	assertKind(t, err, animego.ErrAuthExpired)
	// refresh token 已失效：会话清空，LoggedIn 如实反映「需要重新登录」。
	assert.False(t, c.LoggedIn())
	assert.Equal(t, animego.Session{}, c.Session())
}

func TestStill401AfterRefresh(t *testing.T) {
	rec := &recorder{}
	b := &refreshBackend{goodToken: "at-new", goodCookie: "rt-old", alwaysDeny: true}
	c := newTestClient(t, rec, b.handler())
	c.RestoreSession(animego.Session{AccessToken: "at-expired", RefreshCookie: "rt-old"})

	err := c.MarkWatched(context.Background(), 123, 7)
	assertKind(t, err, animego.ErrAuthExpired)
	// 只重放一次，不无限循环。
	assert.Len(t, rec.filter("/api/subscriptions"), 2)
	assert.Len(t, rec.filter("/api/auth/refresh"), 1)
}

func TestNoSessionNoRefreshCookie(t *testing.T) {
	rec := &recorder{}
	c := newTestClient(t, rec, func(w http.ResponseWriter, r *http.Request) {
		respond(w, http.StatusOK, `{}`)
	})

	// 完全未登录：本地即拒，不发任何请求。
	err := c.EnsureSubscription(context.Background(), 123)
	assertKind(t, err, animego.ErrAuthExpired)
	assert.Empty(t, rec.all())

	// 只有过期 token、没有 cookie：撞 401 后同样归为会话过期。
	b := &refreshBackend{goodToken: "at-new"}
	c2 := newTestClient(t, nil, b.handler())
	c2.RestoreSession(animego.Session{AccessToken: "at-expired"})
	err = c2.EnsureSubscription(context.Background(), 123)
	assertKind(t, err, animego.ErrAuthExpired)
}

func TestOnlyRefreshCookieProactiveRefresh(t *testing.T) {
	// 恢复的会话只剩 cookie（token 曾被清空存盘）：先刷新再发业务请求。
	b := &refreshBackend{goodToken: "at-new", goodCookie: "rt-only"}
	c := newTestClient(t, nil, b.handler())
	c.RestoreSession(animego.Session{RefreshCookie: "rt-only"})

	require.NoError(t, c.MarkWatched(context.Background(), 123, 7))
	assert.Equal(t, "at-new", c.Session().AccessToken)
}

func TestConcurrent401RefreshesOnlyOnce(t *testing.T) {
	rec := &recorder{}
	b := &refreshBackend{goodToken: "at-new", goodCookie: "rt-old", newCookie: "rt-new"}
	c := newTestClient(t, rec, b.handler())
	c.RestoreSession(animego.Session{AccessToken: "at-expired", RefreshCookie: "rt-old"})

	// 8 个 goroutine 同时用过期 token 发鉴权请求：
	// 无论 401 怎么交错，refresh 端点只允许被打一次（互斥即 singleflight）。
	const workers = 8
	start := make(chan struct{})
	errs := make([]error, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			<-start
			errs[idx] = c.MarkWatched(context.Background(), 1, idx+1)
		}(i)
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		require.NoError(t, err, "goroutine %d", i)
	}
	assert.Len(t, rec.filter("/api/auth/refresh"), 1, "并发 401 只应触发一次 refresh")
	assert.Equal(t, animego.Session{AccessToken: "at-new", RefreshCookie: "rt-new"}, c.Session())

	// 每个业务请求最多重放一次：总次数 ≤ 2×workers，且至少 workers 次。
	marks := rec.filter("/api/subscriptions/1/episodes/")
	assert.GreaterOrEqual(t, len(marks), workers)
	assert.LessOrEqual(t, len(marks), workers*2)
	for _, m := range marks {
		assert.True(t, strings.HasPrefix(m.Auth, "Bearer "), "鉴权头缺失: %+v", m)
	}
}

// ---------- 会话变更通知（OnSessionChange） ----------

// sessionSpy 记录客户端主动交出来的每一次会话。
type sessionSpy struct {
	mu   sync.Mutex
	seen []animego.Session
}

func (s *sessionSpy) record(sess animego.Session) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seen = append(s.seen, sess)
}

func (s *sessionSpy) last() (animego.Session, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.seen) == 0 {
		return animego.Session{}, false
	}
	return s.seen[len(s.seen)-1], true
}

func (s *sessionSpy) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.seen)
}

func newSpyClient(t *testing.T, spy *sessionSpy, h http.HandlerFunc) *animego.Client {
	t.Helper()
	return animego.New(animego.Options{
		BaseURL:         newRecordingServer(t, nil, h),
		UserAgent:       "nagare/test",
		OnSessionChange: spy.record,
	})
}

// 轮换后的 cookie 必须【由客户端主动交出来】，不能等调用方想起来去取。
//
// 这条守的是一次真实的静默掉线：播放开始时的 match 撞 401 → 刷新 → cookie 轮换，
// 用户看一半停掉 → 收尾路径提前 return → 轮换后的 cookie 从没落盘 →
// 下次启动拿着已作废的旧 cookie，界面显示已登录，每个请求都失败。
func TestSessionChangeFiresOnRotation(t *testing.T) {
	spy := &sessionSpy{}
	b := &refreshBackend{goodToken: "at-new", goodCookie: "rt-old", newCookie: "rt-new"}
	c := newSpyClient(t, spy, b.handler())
	c.RestoreSession(animego.Session{AccessToken: "at-expired", RefreshCookie: "rt-old"})

	// RestoreSession 是把盘上的读回内存，不该触发落盘
	assert.Equal(t, 0, spy.count(), "RestoreSession 不该触发 OnSessionChange")

	require.NoError(t, c.EnsureSubscription(context.Background(), 123))

	got, ok := spy.last()
	require.True(t, ok, "刷新轮换了 cookie，客户端却一次都没通知")
	assert.Equal(t, "at-new", got.AccessToken)
	assert.Equal(t, "rt-new", got.RefreshCookie, "交出来的必须是轮换后的新 cookie")
	// 通知里的内容与客户端内存态一致 —— 落盘的和在用的不能是两个东西
	assert.Equal(t, c.Session(), got)
}

// 服务端没轮换 cookie 时，access token 换了照样要落盘（15 分钟的那个也值钱）。
func TestSessionChangeFiresWhenOnlyTokenRotates(t *testing.T) {
	spy := &sessionSpy{}
	b := &refreshBackend{goodToken: "at-new", goodCookie: "rt-old"} // newCookie 空 = 不轮换
	c := newSpyClient(t, spy, b.handler())
	c.RestoreSession(animego.Session{AccessToken: "at-expired", RefreshCookie: "rt-old"})

	require.NoError(t, c.EnsureSubscription(context.Background(), 123))

	got, ok := spy.last()
	require.True(t, ok)
	assert.Equal(t, "at-new", got.AccessToken)
	assert.Equal(t, "rt-old", got.RefreshCookie, "没轮换就保留旧 cookie")
}

// refresh token 本身失效时会话被清空 —— 那也是一次变更，必须落盘。
// 不落盘的话下次启动又拿着这份已经作废的东西去试，界面还显示已登录。
func TestSessionChangeFiresWhenRefreshTokenDies(t *testing.T) {
	spy := &sessionSpy{}
	b := &refreshBackend{goodToken: "at-new", goodCookie: "rt-old", failRefresh: true}
	c := newSpyClient(t, spy, b.handler())
	c.RestoreSession(animego.Session{AccessToken: "at-expired", RefreshCookie: "rt-old"})

	err := c.EnsureSubscription(context.Background(), 123)
	require.Error(t, err)

	got, ok := spy.last()
	require.True(t, ok, "会话被清空却没有通知")
	assert.Equal(t, animego.Session{}, got)
	assert.False(t, c.LoggedIn())
}

// 并发 401 只刷新一次，所以也只该通知一次 —— 每次通知都是一次磁盘写。
func TestSessionChangeFiresOncePerRefresh(t *testing.T) {
	spy := &sessionSpy{}
	b := &refreshBackend{goodToken: "at-new", goodCookie: "rt-old", newCookie: "rt-new"}
	c := newSpyClient(t, spy, b.handler())
	c.RestoreSession(animego.Session{AccessToken: "at-expired", RefreshCookie: "rt-old"})

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = c.EnsureSubscription(context.Background(), 123)
		}()
	}
	wg.Wait()

	assert.Equal(t, 1, spy.count(), "singleflight 只刷新一次，就该只落盘一次")
}

// 回调在锁外跑：实现里回头调 Session() 不能死锁。
// 这不是假想 —— 「拿到通知后去客户端确认一下当前状态」是很自然的写法。
func TestSessionChangeCallbackMayCallSession(t *testing.T) {
	b := &refreshBackend{goodToken: "at-new", goodCookie: "rt-old", newCookie: "rt-new"}
	var c *animego.Client
	var reentered animego.Session
	c = animego.New(animego.Options{
		BaseURL:   newRecordingServer(t, nil, b.handler()),
		UserAgent: "nagare/test",
		OnSessionChange: func(animego.Session) {
			reentered = c.Session() // 自锁的话这里直接卡死，测试超时
		},
	})
	c.RestoreSession(animego.Session{AccessToken: "at-expired", RefreshCookie: "rt-old"})

	require.NoError(t, c.EnsureSubscription(context.Background(), 123))
	assert.Equal(t, "rt-new", reentered.RefreshCookie)
}
