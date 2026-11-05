package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nagare-project/nagare/internal/animego"
	"github.com/nagare-project/nagare/internal/httpserver"
	"github.com/nagare-project/nagare/internal/player"
)

// 横幅上的「重试」：当场补发，把这一轮补上几集、还剩几集、为什么没补完原样交给界面。
func TestPlayerSyncRetryReturnsTheRoundResult(t *testing.T) {
	env := newEnv(t)
	env.player.retryRes = player.SyncRetryResult{Synced: 2, Remaining: 1, Reason: "连不上服务端，没补记的下次启动时会再试"}

	rec := env.do(t, http.MethodPost, "/api/player/sync/retry", "")

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var got player.SyncRetryResult
	require.NoError(t, json.Unmarshal(decode(t, rec).Data, &got))
	assert.Equal(t, env.player.retryRes, got)
	assert.Equal(t, []string{"player.sync-retry"}, *env.calls)
}

// 它是变更操作（会写账号），必须挂在鉴权链之内：没 token 401；token 只在查询参数里（跨站表单
// 能做到的）403 —— 两种都不能触发补发。只在裸 mux 上测的话，注册位置挪到鉴权链外面也照样全绿。
func TestPlayerSyncRetryRequiresTheCustomHeaderThroughTheRealServerChain(t *testing.T) {
	const token = "9f86d081884c7d659a2feaa0c55ad015"
	fake := &fakePlayer{}
	srv := httpserver.New(httpserver.Options{Token: token, Port: 8591, RegisterAPI: New(Deps{Player: fake}).Register})
	retried := 0
	fake.log = func(op string) {
		if op == "player.sync-retry" {
			retried++
		}
	}
	call := func(target string, header bool) int {
		req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8591"+target, nil)
		req.Host = "127.0.0.1:8591"
		if header {
			req.Header.Set(httpserver.TokenHeader, token)
		}
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)
		return rec.Code
	}

	assert.Equal(t, http.StatusUnauthorized, call("/api/player/sync/retry", false))
	assert.Equal(t, http.StatusForbidden, call("/api/player/sync/retry?token="+token, false))
	assert.Zero(t, retried, "没过鉴权就补发了")

	assert.Equal(t, http.StatusOK, call("/api/player/sync/retry", true))
	assert.Equal(t, 1, retried)
}

// 登录成功之后在后台补发一轮：启动时那一轮还没登录、什么都没补，不能等到下次启动。
// 会话先落盘、再起补发：补发里的请求可能刷新会话（轮换 refresh cookie），这时登录这边再拿
// 登录那一刻的会话快照落盘，就把刷新后的盖掉了 —— 下次启动拿着作废的 cookie 静默掉线。
func TestLoginKicksAPendingSyncRound(t *testing.T) {
	env := newEnv(t)
	var atKick string
	env.player.onKick = func() { atKick = env.store.AnimegoSession().RefreshCookie }

	rec := env.do(t, http.MethodPost, "/api/animego/login", `{"email":"a@b.c","password":"pw"}`)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, []string{"player.sync-kick"}, *env.calls, "登录成功要补发一轮，且只补一轮")
	assert.Equal(t, "rc", atKick, "补发起来之前，会话就该已经落盘")
}

// 登录没成功（密码错、被限流、连不上、请求本身不对）：账号还是写不进去，不补发。
func TestFailedLoginDoesNotKickASyncRound(t *testing.T) {
	for _, tc := range []struct {
		name     string
		loginErr error
		body     string
		status   int
	}{
		{"密码错", &animego.Error{Kind: animego.ErrBadRequest, Op: "login", Status: http.StatusUnauthorized}, `{"email":"a@b.c","password":"x"}`, http.StatusBadRequest},
		{"被限流", &animego.Error{Kind: animego.ErrRateLimited, Op: "login"}, `{"email":"a@b.c","password":"x"}`, http.StatusTooManyRequests},
		{"连不上", &animego.Error{Kind: animego.ErrUnavailable, Op: "login"}, `{"email":"a@b.c","password":"x"}`, http.StatusBadGateway},
		{"请求体不是 JSON", nil, `{broken`, http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := newEnv(t)
			env.auth.loginErr = tc.loginErr

			rec := env.do(t, http.MethodPost, "/api/animego/login", tc.body)

			assert.Equal(t, tc.status, rec.Code, rec.Body.String())
			assert.Empty(t, *env.calls, "登录没成功就补发了")
		})
	}
}

// 退出登录不补发（账号都没了）；没接播放器的装配里登录照常成功，不因为补发 panic。
func TestLogoutAndPlayerlessLoginDoNotKick(t *testing.T) {
	env := newEnv(t)
	rec := env.do(t, http.MethodPost, "/api/animego/logout", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Empty(t, *env.calls, "退出登录不该补发")

	bare := newEnvDeps(t, func(d *Deps) { d.Player = nil })
	rec = bare.do(t, http.MethodPost, "/api/animego/login", `{"email":"a@b.c","password":"pw"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, "rc", bare.store.AnimegoSession().RefreshCookie)
}
