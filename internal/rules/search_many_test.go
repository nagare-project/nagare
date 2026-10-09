package rules

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestQueryVariants(t *testing.T) {
	got := QueryVariants([]string{" 葬送的芙莉莲 ", "", "Sousou no Frieren", "sousou no frieren", "葬送のフリーレン", "Frieren", "第五个"})
	assert.Equal(t, []string{"葬送的芙莉莲", "Sousou no Frieren", "葬送のフリーレン", "Frieren"}, got, "去空白、跳空串、不分大小写去重保留首个写法、最多四个")
	assert.Empty(t, QueryVariants([]string{"  ", ""}))
}

// 几种写法各搜一次：同一条发布被两种写法都搜到只留一条，排序沿用做种数优先；
// 每个源只给一个结论 —— 一种写法连不上、另一种有结果，就是有结果。
func TestRegistrySearchManyMergesVariants(t *testing.T) {
	shared := "magnet:?xt=urn:btih:" + strings.Repeat("ab", 20)
	other := "magnet:?xt=urn:btih:" + strings.Repeat("cd", 20)
	var mu sync.Mutex
	terms := map[string][]string{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		term := r.URL.Query().Get("term")
		mu.Lock()
		terms[r.URL.Path] = append(terms[r.URL.Path], term)
		mu.Unlock()
		switch {
		case r.URL.Path == "/a" && term == "芙莉莲":
			_, _ = w.Write(rss(`<item><title>[G] 芙莉莲 - 01</title><enclosure url="` + shared + `"/></item>`))
		case r.URL.Path == "/a" && term == "Frieren":
			_, _ = w.Write(rss(`<item><title>[G] Frieren - 01</title><enclosure url="` + shared + `"/></item><item><title>[H] Frieren - 02</title><enclosure url="` + other + `"/></item>`))
		case r.URL.Path == "/flaky" && term == "Frieren":
			w.WriteHeader(503)
		case r.URL.Path == "/flaky":
			_, _ = w.Write(rss(`<item><title>[K] 芙莉莲 - 03</title><enclosure url="magnet:?xt=urn:btih:` + strings.Repeat("ef", 20) + `"/></item>`))
		case r.URL.Path == "/half" && term == "Frieren":
			w.WriteHeader(503)
		default:
			_, _ = w.Write(rss(``))
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
	reg := NewRegistry(&Fetcher{Client: srv.Client()}, mk("a", "/a"), mk("flaky", "/flaky"), mk("half", "/half"), mk("empty", "/empty"))

	res := reg.SearchMany(context.Background(), []string{" 芙莉莲", "Frieren", "frieren", ""})
	assert.Equal(t, "芙莉莲", res.Query)
	assert.ElementsMatch(t, []string{"芙莉莲", "Frieren"}, terms["/a"], "每种写法每个源只打一次")
	require.Len(t, res.Items, 3, "两种写法都搜到的那条只留一条")
	hashes := map[string]bool{}
	for _, item := range res.Items {
		hashes[item.Infohash] = true
	}
	assert.Len(t, hashes, 3)

	states := map[string]State{}
	for _, outcome := range res.Sources {
		states[outcome.Source] = outcome.State
	}
	assert.Equal(t, StateOK, states["a"])
	assert.Equal(t, StateOK, states["flaky"], "一种写法连不上、另一种有结果：有结果")
	assert.Equal(t, StateFailed, states["half"], "一种零结果、一种连不上：说不清有没有，按连不上报")
	assert.Equal(t, StateZero, states["empty"])
	for _, outcome := range res.Sources {
		switch outcome.Source {
		case "a":
			assert.Equal(t, 3, outcome.Count, "条数按各写法累加（去重在合并之后）")
			assert.Empty(t, outcome.Reason)
			assert.Empty(t, outcome.Detail)
		case "flaky":
			assert.Empty(t, outcome.Reason, "有结果时界面不报故障")
			assert.Contains(t, outcome.Detail, "部分写法查询失败", "另一种写法失败要留下痕迹")
		}
	}
}
