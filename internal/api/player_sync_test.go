package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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
