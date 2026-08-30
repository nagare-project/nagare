// 进度写：请求形状断言（method/path/body/Bearer 头）与本地参数校验。
package animego_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nagare-player/nagare/internal/animego"
)

// loggedInClient 返回一个已持有效会话、服务端固定 200 的客户端。
func loggedInClient(t *testing.T, rec *recorder) *animego.Client {
	t.Helper()
	c := newTestClient(t, rec, func(w http.ResponseWriter, r *http.Request) {
		respond(w, http.StatusOK, `{"data":{}}`)
	})
	c.RestoreSession(animego.Session{AccessToken: "at-valid", RefreshCookie: "rt"})
	return c
}

func TestEnsureSubscriptionRequestShape(t *testing.T) {
	rec := &recorder{}
	c := loggedInClient(t, rec)

	require.NoError(t, c.EnsureSubscription(context.Background(), 154587))

	reqs := rec.all()
	require.Len(t, reqs, 1)
	assert.Equal(t, http.MethodPost, reqs[0].Method)
	assert.Equal(t, "/api/subscriptions", reqs[0].Path)
	assert.Equal(t, "Bearer at-valid", reqs[0].Auth)
	assert.Equal(t, "application/json", reqs[0].CT)

	var body map[string]any
	require.NoError(t, json.Unmarshal(reqs[0].Body, &body))
	// ifAbsent 必带 true：否则会把已弃番/已完结状态覆盖回 watching。
	assert.Equal(t, map[string]any{
		"anilistId": float64(154587),
		"status":    "watching",
		"ifAbsent":  true,
	}, body)
}

func TestMarkWatchedRequestShape(t *testing.T) {
	rec := &recorder{}
	c := loggedInClient(t, rec)

	require.NoError(t, c.MarkWatched(context.Background(), 154587, 7))

	reqs := rec.all()
	require.Len(t, reqs, 1)
	assert.Equal(t, http.MethodPut, reqs[0].Method)
	assert.Equal(t, "/api/subscriptions/154587/episodes/7", reqs[0].Path)
	assert.Equal(t, "Bearer at-valid", reqs[0].Auth)
	assert.False(t, reqs[0].HasBody, "标记看过不需要请求体")
}

func TestProgressLocalValidation(t *testing.T) {
	rec := &recorder{}
	c := loggedInClient(t, rec)
	ctx := context.Background()

	assertKind(t, c.EnsureSubscription(ctx, 0), animego.ErrBadRequest)
	assertKind(t, c.EnsureSubscription(ctx, -1), animego.ErrBadRequest)
	assertKind(t, c.MarkWatched(ctx, 0, 7), animego.ErrBadRequest)
	assertKind(t, c.MarkWatched(ctx, 154587, 0), animego.ErrBadRequest)
	assert.Empty(t, rec.all(), "非法参数本地即拒，不发请求")
}

func TestProgressServerError(t *testing.T) {
	// 5xx → 暂时不可达：调用方应缓存进度稍后补写，而不是丢弃或重登。
	c := newTestClient(t, nil, func(w http.ResponseWriter, r *http.Request) {
		respond(w, http.StatusServiceUnavailable, `{"error":"maintenance"}`)
	})
	c.RestoreSession(animego.Session{AccessToken: "at-valid", RefreshCookie: "rt"})

	assertKind(t, c.MarkWatched(context.Background(), 154587, 7), animego.ErrUnavailable)
}
