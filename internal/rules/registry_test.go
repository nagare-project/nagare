package rules

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 两个源并发搜索：合并按注册顺序、跨源同 hash 去重、禁用源缩小结果集、坏源单独标注。
func TestRegistrySearch(t *testing.T) {
	shared := "magnet:?xt=urn:btih:" + strings.Repeat("ab", 20)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/a"):
			_, _ = w.Write(rss(`<item><title>A1</title><enclosure url="` + shared + `"/></item><item><title>A2</title><enclosure url="magnet:?xt=urn:btih:` + strings.Repeat("cd", 20) + `"/></item>`))
		case strings.HasPrefix(r.URL.Path, "/b"):
			_, _ = w.Write(rss(`<item><title>B1</title><enclosure url="` + shared + `"/></item>`))
		default:
			w.WriteHeader(503)
		}
	}))
	defer srv.Close()

	mk := func(id, path string) *Rule {
		src := strings.Replace(rssRule, "id: rsstest", "id: "+id, 1)
		src = strings.Replace(src, "{{base}}/rss?term={{query}}", srv.URL+path+"?term={{query}}", 1)
		r, err := Parse([]byte(src))
		require.NoError(t, err)
		return r
	}
	reg := NewRegistry(&Fetcher{Client: srv.Client()}, mk("a", "/a"), mk("b", "/b"), mk("c", "/dead"))

	res := reg.Search(context.Background(), "  q ")
	require.Len(t, res.Sources, 3)
	assert.Equal(t, StateOK, res.Sources[0].State)
	assert.Equal(t, StateOK, res.Sources[1].State)
	assert.Equal(t, StateFailed, res.Sources[2].State, "坏源单独标注，不拖累整体")
	assert.Len(t, res.Items, 2, "A1 与 B1 同 hash 去重")
	assert.Equal(t, "a", res.Items[0].Source, "同分按注册顺序，a 胜出")

	reg.SetEnabled("a", false)
	res = reg.Search(context.Background(), "q")
	assert.Equal(t, StateDisabled, res.Sources[0].State)
	require.Len(t, res.Items, 1, "禁用 a 后只剩 b 的一条")
	assert.Equal(t, "b", res.Items[0].Source)

	assert.Empty(t, reg.Search(context.Background(), "   ").Items, "空关键词不打上游")

	out, err := reg.SelfCheck(context.Background(), "b")
	require.NoError(t, err)
	assert.Equal(t, StateOK, out.State)
	_, err = reg.SelfCheck(context.Background(), "nope")
	assert.Error(t, err)
}
