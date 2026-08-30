// Comments：裸响应解析、count=0 降级不是错误、400 分类、本地参数校验。
package animego_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nagare-player/nagare/internal/animego"
	"github.com/nagare-player/nagare/internal/danmaku"
)

func TestCommentsOK(t *testing.T) {
	rec := &recorder{}
	c := newTestClient(t, rec, func(w http.ResponseWriter, r *http.Request) {
		respond(w, http.StatusOK, `{"count":2,"comments":[
			{"cid":900001,"p":"12.34,1,16777215,userhash1","m":"前方高能"},
			{"cid":900002,"p":"13.00,5,16777215,userhash2","m":"名场面","extra":"ignored"}]}`)
	})

	got, err := c.Comments(context.Background(), 184300007)
	require.NoError(t, err)
	// 直接解到 danmaku.Comment：cid/p/m 的 JSON 形状由 danmaku 包约定。
	assert.Equal(t, []danmaku.Comment{
		{CID: 900001, P: "12.34,1,16777215,userhash1", Text: "前方高能"},
		{CID: 900002, P: "13.00,5,16777215,userhash2", Text: "名场面"},
	}, got)

	reqs := rec.all()
	require.Len(t, reqs, 1)
	assert.Equal(t, http.MethodGet, reqs[0].Method)
	assert.Equal(t, "/api/dandanplay/comments/184300007", reqs[0].Path)
	assert.Empty(t, reqs[0].Auth, "弹幕是公开端点，不该带鉴权头")
}

func TestCommentsUpstreamMissIsNotAnError(t *testing.T) {
	// 上游 miss 降级为 200 的 {"count":0,"comments":[]} ——「这集没弹幕」是
	// 正常结果；只有分类错误才该让调用方把弹幕标为「不可用」。
	c := newTestClient(t, nil, func(w http.ResponseWriter, r *http.Request) {
		respond(w, http.StatusOK, `{"count":0,"comments":[]}`)
	})

	got, err := c.Comments(context.Background(), 184300007)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Empty(t, got)
}

func TestCommentsBadRequest(t *testing.T) {
	// 服务端对无效 episodeId 给 400 {"error":"..."}（纯字符串信封）。
	c := newTestClient(t, nil, func(w http.ResponseWriter, r *http.Request) {
		respond(w, http.StatusBadRequest, `{"error":"invalid episode id"}`)
	})

	_, err := c.Comments(context.Background(), 999)
	ae := assertKind(t, err, animego.ErrBadRequest)
	assert.Contains(t, ae.Error(), "invalid episode id")
}

func TestCommentsInvalidIDLocally(t *testing.T) {
	rec := &recorder{}
	c := newTestClient(t, rec, func(w http.ResponseWriter, r *http.Request) {
		respond(w, http.StatusOK, `{}`)
	})

	for _, id := range []int64{0, -5} {
		_, err := c.Comments(context.Background(), id)
		assertKind(t, err, animego.ErrBadRequest)
	}
	assert.Empty(t, rec.all(), "非法 episodeId 本地即拒，不发请求")
}

func TestCommentsGarbageBody(t *testing.T) {
	c := newTestClient(t, nil, func(w http.ResponseWriter, r *http.Request) {
		respond(w, http.StatusOK, `<!doctype html><p>not json</p>`)
	})
	_, err := c.Comments(context.Background(), 184300007)
	assertKind(t, err, animego.ErrDecode)
}
