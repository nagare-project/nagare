package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

// 目录（发现 / 季度 / 放送 / 详情 / 搜索 / 逐集资料 / 跨季编号）与收藏的黄金用例。

func catalogGoldenCases() []goldenCase {
	get := func(target string) func(t *testing.T) []byte {
		return func(t *testing.T) []byte {
			return newGoldenEnv(t, goldenOptions{}).call(t, http.StatusOK, http.MethodGet, target, "")
		}
	}
	return []goldenCase{
		{"catalog-discover", func(t *testing.T) []byte {
			env := newGoldenEnv(t, goldenOptions{})
			// 精选板块取不到：派生的「近期精选剧场版」带上部分失败的说明（板块独立降级）
			env.catalog.failGems = true
			return env.call(t, http.StatusOK, http.MethodGet, "/api/discover", "")
		}},
		{"catalog-seasonal", func(t *testing.T) []byte {
			env := newGoldenEnv(t, goldenOptions{})
			env.warmSchedule(t)
			return env.call(t, http.StatusOK, http.MethodGet, "/api/seasonal?season=FALL&year=2026", "")
		}},
		{"catalog-seasonal-bad-season", func(t *testing.T) []byte {
			return newGoldenEnv(t, goldenOptions{}).call(t, http.StatusBadRequest, http.MethodGet, "/api/seasonal?season=AUTUMN&year=2026", "")
		}},
		{"catalog-schedule", func(t *testing.T) []byte {
			env := newGoldenLibrary(t)
			return env.call(t, http.StatusOK, http.MethodGet, "/api/schedule", "")
		}},
		{"catalog-detail", func(t *testing.T) []byte {
			env := newGoldenEnv(t, goldenOptions{})
			env.warmSchedule(t)
			return env.call(t, http.StatusOK, http.MethodGet, "/api/anime/154587", "")
		}},
		{"catalog-search", get("/api/catalog/search?q=%E8%8A%99%E8%8E%89%E8%8E%B2%20%20")},
		{"catalog-unavailable", func(t *testing.T) []byte {
			env := newGoldenEnv(t, goldenOptions{deps: func(d *Deps) { d.Catalog = nil }})
			return env.call(t, http.StatusServiceUnavailable, http.MethodGet, "/api/discover", "")
		}},
		{"catalog-episodes", func(t *testing.T) []byte {
			env := newGoldenEnv(t, goldenOptions{})
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(goldenAniZip))
			}))
			t.Cleanup(srv.Close)
			env.h.episodes.client = srv.Client()
			env.h.episodes.endpoint = srv.URL + "/v1/episodes?anilist_id="
			return env.call(t, http.StatusOK, http.MethodGet, "/api/anime/154587/episodes", "")
		}},
		{"catalog-episode-offset-known", get("/api/anime/180003/episode-offset")},
		{"catalog-episode-offset-unknown", get("/api/anime/154587/episode-offset")},
		{"lists", get("/api/lists")},
		{"lists-logged-out", func(t *testing.T) []byte {
			env := newGoldenEnv(t, goldenOptions{})
			env.lists.epoch = 0
			return env.call(t, http.StatusOK, http.MethodGet, "/api/lists", "")
		}},
		{"lists-save", func(t *testing.T) []byte {
			return newGoldenEnv(t, goldenOptions{}).call(t, http.StatusOK, http.MethodPost, "/api/lists/154587",
				`{"status":"watching","currentEpisode":7,"score":9}`)
		}},
		{"lists-delete", func(t *testing.T) []byte {
			return newGoldenEnv(t, goldenOptions{}).call(t, http.StatusOK, http.MethodDelete, "/api/lists/130003", "")
		}},
	}
}

// warmSchedule 先取一次放送表：季度页与详情页只用已缓存的放送快照补倒计时，不为它多发请求。
func (e *goldenEnv) warmSchedule(t *testing.T) {
	t.Helper()
	_, err := e.h.catalog.schedule(context.Background())
	require.NoError(t, err)
}

// goldenAniZip 是逐集资料上游的一份响应（只有映射键是正片集号的条目会被采用）。
const goldenAniZip = `{
  "mappings": {"anilist_id": 154587},
  "episodes": {
    "1": {"title": {"en": "The Journey's End", "ja": "冒険の終わり"}, "image": "https://artworks.thetvdb.com/banners/v4/episode/1/screencap.jpg",
          "overview": "<b>勇者</b>一行凯旋。", "runtime": 24, "airDate": "2026-10-03", "airDateUtc": "2026-10-03T14:00:00Z"},
    "2": {"title": {"zh-Hans": "不用魔法也可以"}},
    "S1": {"title": {"en": "Special"}}
  }
}`
