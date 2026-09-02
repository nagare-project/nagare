// 登录：data 信封解析、Set-Cookie 捕获、错误信封分类。
package animego_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nagare-project/nagare/internal/animego"
)

func TestLoginSuccess(t *testing.T) {
	rec := &recorder{}
	c := newTestClient(t, rec, func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{
			Name: "refreshToken", Value: "rt-1", HttpOnly: true, Path: "/api/auth",
		})
		respond(w, http.StatusOK, `{"data":{
			"accessToken":"at-1",
			"user":{"id":"7f9c0a1e-0000-0000-0000-000000000042",
			        "username":"lawrence","email":"u@example.com",
			        "avatarUrl":"https://img/av.png","role":null,"extra":"ignored"}}}`)
	})

	user, err := c.Login(context.Background(), "u@example.com", "pw123456")
	require.NoError(t, err)

	// 用户信息宽松解析：已知字段取到、未知字段忽略、null 不炸。
	assert.Equal(t, "7f9c0a1e-0000-0000-0000-000000000042", user.ID)
	assert.Equal(t, "lawrence", user.Username)
	assert.Equal(t, "u@example.com", user.Email)
	assert.Equal(t, "https://img/av.png", user.AvatarURL)

	// 会话建立：access token + 捕获到的 refresh cookie。
	assert.True(t, c.LoggedIn())
	assert.Equal(t, animego.Session{AccessToken: "at-1", RefreshCookie: "rt-1"}, c.Session())

	// 请求形状：POST /api/auth/login、JSON body、UA。
	reqs := rec.all()
	require.Len(t, reqs, 1)
	assert.Equal(t, http.MethodPost, reqs[0].Method)
	assert.Equal(t, "/api/auth/login", reqs[0].Path)
	assert.Equal(t, "application/json", reqs[0].CT)
	assert.Equal(t, "nagare/test", reqs[0].UA)
	var body map[string]any
	require.NoError(t, json.Unmarshal(reqs[0].Body, &body))
	assert.Equal(t, map[string]any{"email": "u@example.com", "password": "pw123456"}, body)
}

func TestLoginWrongPassword(t *testing.T) {
	c := newTestClient(t, nil, func(w http.ResponseWriter, r *http.Request) {
		respond(w, http.StatusUnauthorized,
			`{"error":{"code":"INVALID_CREDENTIALS","message":"邮箱或密码错误"}}`)
	})

	_, err := c.Login(context.Background(), "u@example.com", "wrong")
	ae := assertKind(t, err, animego.ErrBadRequest)
	// 错误信息要带上服务端信封内容，用户才知道是密码错而不是网络坏。
	assert.Contains(t, ae.Error(), "邮箱或密码错误")
	assert.False(t, c.LoggedIn())
}

func TestLoginRateLimited(t *testing.T) {
	// 登录端点限速 10 次/15 分钟：429 必须归为 ErrRateLimited，
	// 调用方看到它就不该安排任何自动重试。
	c := newTestClient(t, nil, func(w http.ResponseWriter, r *http.Request) {
		respond(w, http.StatusTooManyRequests,
			`{"error":{"code":"RATE_LIMITED","message":"尝试过于频繁"}}`)
	})

	_, err := c.Login(context.Background(), "u@example.com", "pw")
	assertKind(t, err, animego.ErrRateLimited)
}

func TestLoginMissingAccessToken(t *testing.T) {
	// 200 但信封里没有 accessToken：形状不对 → ErrDecode，且说清缺了什么。
	c := newTestClient(t, nil, func(w http.ResponseWriter, r *http.Request) {
		respond(w, http.StatusOK, `{"data":{"user":{"id":"x"}}}`)
	})

	_, err := c.Login(context.Background(), "u@example.com", "pw")
	ae := assertKind(t, err, animego.ErrDecode)
	assert.Contains(t, ae.Error(), "accessToken")
	assert.False(t, c.LoggedIn())
}

func TestLoginEmptyInput(t *testing.T) {
	// 本地即拒：不该为空邮箱/密码浪费一次限速额度。
	c := newTestClient(t, nil, func(w http.ResponseWriter, r *http.Request) {
		respond(w, http.StatusOK, `{}`)
	})
	_, err := c.Login(context.Background(), "", "pw")
	assertKind(t, err, animego.ErrBadRequest)
	_, err = c.Login(context.Background(), "u@example.com", "")
	assertKind(t, err, animego.ErrBadRequest)
}

func TestLoginMissingRefreshCookieStillSucceeds(t *testing.T) {
	// 契约上服务端一定 Set-Cookie；万一缺席，access token 仍有 15 分钟可用，
	// 不算登录失败 —— 之后刷新会以 ErrAuthExpired 浮出，不会静默丢功能。
	c := newTestClient(t, nil, func(w http.ResponseWriter, r *http.Request) {
		respond(w, http.StatusOK, `{"data":{"accessToken":"at-only","user":{}}}`)
	})

	_, err := c.Login(context.Background(), "u@example.com", "pw")
	require.NoError(t, err)
	assert.Equal(t, animego.Session{AccessToken: "at-only", RefreshCookie: ""}, c.Session())
	assert.True(t, c.LoggedIn())
}
