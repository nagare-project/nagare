// 客户端通用行为：网络失败分类、响应体限读、UA 缺省、BaseURL 归一化、错误类型。
package animego_test

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nagare-player/nagare/internal/animego"
)

// closedPortURL 拿一个刚释放的本地端口 —— 连接必被拒绝。
func closedPortURL(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := ln.Addr().String()
	require.NoError(t, ln.Close())
	return "http://" + addr
}

func TestNetworkRefusedIsUnavailable(t *testing.T) {
	// 决议 CQ3 的失败模式表：「API 不可达 → 本地库全功能，弹幕标为不可用」。
	// 前提就是调用方能从错误里认出「不可达」—— 公开与鉴权端点都要覆盖。
	c := animego.New(animego.Options{BaseURL: closedPortURL(t)})
	c.RestoreSession(animego.Session{AccessToken: "at", RefreshCookie: "rt"})
	ctx := context.Background()

	_, err := c.Comments(ctx, 184300007)
	assertKind(t, err, animego.ErrUnavailable)

	_, err = c.Match(ctx, animego.MatchInput{FileName: "f.mkv", Episode: 1})
	assertKind(t, err, animego.ErrUnavailable)

	assertKind(t, c.EnsureSubscription(ctx, 1), animego.ErrUnavailable)
}

func TestResponseBodyLimit(t *testing.T) {
	// 超过 8MB 的响应按形状异常拒收，防止异常服务端炸内存。
	huge := strings.Repeat("a", 8<<20+16)
	c := newTestClient(t, nil, func(w http.ResponseWriter, r *http.Request) {
		respond(w, http.StatusOK, huge)
	})

	_, err := c.Comments(context.Background(), 184300007)
	ae := assertKind(t, err, animego.ErrDecode)
	assert.Contains(t, ae.Error(), "上限")
}

func TestDefaultUserAgent(t *testing.T) {
	rec := &recorder{}
	url := newRecordingServer(t, rec, func(w http.ResponseWriter, r *http.Request) {
		respond(w, http.StatusOK, `{"count":0,"comments":[]}`)
	})

	// 不注入 UserAgent：应回落到兜底 UA。
	c := animego.New(animego.Options{BaseURL: url})
	_, err := c.Comments(context.Background(), 1)
	require.NoError(t, err)

	reqs := rec.all()
	require.Len(t, reqs, 1)
	assert.Equal(t, "nagare/dev", reqs[0].UA, "未注入版本时用兜底 UA")
}

func TestTrailingSlashBaseURL(t *testing.T) {
	rec := &recorder{}
	url := newRecordingServer(t, rec, func(w http.ResponseWriter, r *http.Request) {
		respond(w, http.StatusOK, `{"count":0,"comments":[]}`)
	})

	c := animego.New(animego.Options{BaseURL: url + "/", UserAgent: "nagare/test"})
	_, err := c.Comments(context.Background(), 1)
	require.NoError(t, err)
	assert.Equal(t, "/api/dandanplay/comments/1", rec.all()[0].Path, "末尾斜杠不该产生双斜杠路径")
}

func TestErrorTypeBehavior(t *testing.T) {
	underlying := errors.New("connection refused")
	err := &animego.Error{Kind: animego.ErrUnavailable, Op: "comments", Err: underlying}

	// errors.As / Unwrap 链路完整。
	var ae *animego.Error
	require.ErrorAs(t, error(err), &ae)
	assert.Equal(t, animego.ErrUnavailable, ae.Kind)
	assert.ErrorIs(t, err, underlying)

	// 中文信息含操作名、恢复动作与底层原因 —— 错误必须可行动。
	msg := err.Error()
	assert.Contains(t, msg, "comments")
	assert.Contains(t, msg, "本地播放不受影响")
	assert.Contains(t, msg, "connection refused")

	// 各分类都有恢复提示，且 String() 可读（日志/测试输出用）。
	kinds := map[animego.ErrKind]string{
		animego.ErrUnavailable: "unavailable",
		animego.ErrAuthExpired: "auth-expired",
		animego.ErrRateLimited: "rate-limited",
		animego.ErrBadRequest:  "bad-request",
		animego.ErrDecode:      "decode",
	}
	for kind, str := range kinds {
		assert.Equal(t, str, kind.String())
		e := &animego.Error{Kind: kind, Op: "op"}
		assert.NotEmpty(t, e.Error())
		assert.NotContains(t, e.Error(), "未知错误")
	}
}
