package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nagare-project/nagare/internal/animego"
	"github.com/nagare-project/nagare/internal/artcache"
	"github.com/nagare-project/nagare/internal/httpserver"
	"github.com/nagare-project/nagare/internal/store"
)

// fakeCoverCache 记下清空次数，占用按清空前后两档给。
type fakeCoverCache struct {
	usage   artcache.Usage
	after   artcache.Usage
	skipped int
	clears  int
}

func (f *fakeCoverCache) Usage() artcache.Usage { return f.usage }

func (f *fakeCoverCache) Clear() (artcache.Usage, int) {
	f.clears++
	f.usage = f.after
	return f.after, f.skipped
}

// coverCacheEnv 建一个带封面缓存注入点的 Handler（testEnv 不暴露 Handler 本身）。
func coverCacheEnv(t *testing.T, deps Deps) (*Handler, *testEnv) {
	t.Helper()
	if deps.Store == nil {
		st, err := store.Open(filepath.Join(t.TempDir(), "state.json"))
		require.NoError(t, err)
		deps.Store = st
	}
	if deps.Auth == nil {
		deps.Auth = &fakeAuth{}
	}
	h := New(deps)
	mux := http.NewServeMux()
	h.Register(mux)
	return h, &testEnv{mux: mux, store: deps.Store}
}

// settingsCoverCache 取设置载荷里的 coverCache 原文（null 时为 "null"）。
func settingsCoverCache(t *testing.T, env *testEnv) json.RawMessage {
	t.Helper()
	rec := env.do(t, http.MethodGet, "/api/settings", "")
	require.Equal(t, http.StatusOK, rec.Code)
	var payload struct {
		CoverCache json.RawMessage `json:"coverCache"`
	}
	require.NoError(t, json.Unmarshal(decode(t, rec).Data, &payload))
	return payload.CoverCache
}

func TestSettingsCoverCacheIsNullUntilInjected(t *testing.T) {
	h, env := coverCacheEnv(t, Deps{})
	assert.JSONEq(t, "null", string(settingsCoverCache(t, env)), "封面缓存还没建：字段在、值为 null")

	h.SetCoverCache(&fakeCoverCache{usage: artcache.Usage{Bytes: 12 << 20, Count: 80, MaxBytes: artcache.MaxBytes}})
	assert.JSONEq(t, `{"bytes":12582912,"count":80,"maxBytes":314572800}`, string(settingsCoverCache(t, env)))
}

func TestClearCoverCache(t *testing.T) {
	h, env := coverCacheEnv(t, Deps{})
	rec := env.do(t, http.MethodPost, "/api/cover-cache/clear", "")
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code, "没有封面缓存时如实说不可用")

	fake := &fakeCoverCache{
		usage:   artcache.Usage{Bytes: 5000, Count: 20, MaxBytes: 9000},
		after:   artcache.Usage{Bytes: 100, Count: 1, MaxBytes: 9000},
		skipped: 1,
	}
	h.SetCoverCache(fake)
	rec = env.do(t, http.MethodPost, "/api/cover-cache/clear", "")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"bytes":100,"count":1,"maxBytes":9000,"skipped":1}`, string(decode(t, rec).Data))
	assert.Equal(t, 1, fake.clears)

	rec = env.do(t, http.MethodGet, "/api/cover-cache/clear", "")
	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code, "清空只认 POST")
}

// 走真实的 httpserver 处理链：清空是变更操作，要 token，而且 token 必须在自定义头里
// （跨站表单能带查询参数，设不了自定义头）。
func TestClearCoverCacheRequiresTokenHeaderThroughTheRealServerChain(t *testing.T) {
	const token = "9f86d081884c7d659a2feaa0c55ad015"
	h := New(Deps{})
	fake := &fakeCoverCache{}
	h.SetCoverCache(fake)
	srv := httpserver.New(httpserver.Options{Token: token, Port: 8591, RegisterAPI: h.Register})
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

	assert.Equal(t, http.StatusUnauthorized, call("/api/cover-cache/clear", false))
	assert.Equal(t, http.StatusForbidden, call("/api/cover-cache/clear?token="+token, false))
	assert.Zero(t, fake.clears, "被拦下的请求不能清空")
	assert.Equal(t, http.StatusOK, call("/api/cover-cache/clear", true))
	assert.Equal(t, 1, fake.clears)
}

// 在用的封面 = 媒体库的匹配封面与手动认定的封面 + 盘上离线目录里出现的图片地址。
func TestPinnedArtSources(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "state.json"))
	require.NoError(t, err)
	require.NoError(t, st.SetBinding("f1", store.Binding{AnilistID: 1, CoverURL: "https://s4.anilist.co/matched.jpg"}))
	require.NoError(t, st.SetBinding("f2", store.Binding{AnilistID: 2})) // 没有封面
	_, err = st.ApplyAssociation("k/manual", &store.Association{Mode: store.AssociationManual, AnilistID: 3, CoverURL: "https://s4.anilist.co/manual.jpg"}, nil)
	require.NoError(t, err)
	_, err = st.ApplyAssociation("k/none", &store.Association{Mode: store.AssociationNone, CoverURL: "https://s4.anilist.co/none.jpg"}, nil)
	require.NoError(t, err)

	h, _ := coverCacheEnv(t, Deps{Store: st, CatalogCacheDir: filepath.Join(t.TempDir(), "catalog")})
	disk := h.catalog.cache.disk
	require.NotNil(t, disk)
	disk.store("trending:", []animego.CatalogMedia{{
		AnilistID: 9, CoverImageURL: "https://s4.anilist.co/cover9.jpg", BannerImageURL: "https://s4.anilist.co/banner9.jpg",
		Description: "http://not-https.example/x",
	}}, time.Now())
	disk.store("detail:9", animego.CatalogMedia{AnilistID: 9, CoverImageURL: "https://s4.anilist.co/cover9.jpg"}, time.Now())

	assert.ElementsMatch(t, []string{
		"https://s4.anilist.co/matched.jpg",
		"https://s4.anilist.co/manual.jpg",
		"https://s4.anilist.co/cover9.jpg",
		"https://s4.anilist.co/banner9.jpg",
	}, h.PinnedArtSources(), "标为「不是目录作品」的关联不算；非 https 字符串不算；去重")
}

func TestPinnedArtSourcesWithoutStoreOrDisk(t *testing.T) {
	assert.Empty(t, New(Deps{}).PinnedArtSources())
}

// 接真实的封面缓存：设置页看得到占用，清空之后封面文件不在了。
func TestCoverCacheSettingsWithRealCache(t *testing.T) {
	h, env := coverCacheEnv(t, Deps{})
	cache, err := artcache.New(t.TempDir())
	require.NoError(t, err)
	src := "https://s4.anilist.co/cover.png"
	require.NoError(t, os.WriteFile(cache.Path(src), []byte("cached-image"), 0o600))
	// 别处直接写进目录的封面要在用到时才进索引：先经 /art 取一次
	art := NewRemoteArt("https://example.test", 4)
	art.SetPrefix("/art/cap")
	key := art.Register(src)
	rec := httptest.NewRecorder()
	NewArtHandler(nil, cache, art).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, key[len("/art/cap"):], nil))
	require.Equal(t, http.StatusOK, rec.Code)

	h.SetCoverCache(cache)
	assert.JSONEq(t, `{"bytes":12,"count":1,"maxBytes":314572800}`, string(settingsCoverCache(t, env)))
	rec = env.do(t, http.MethodPost, "/api/cover-cache/clear", "")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"bytes":0,"count":0,"maxBytes":314572800,"skipped":0}`, string(decode(t, rec).Data))
	assert.NoFileExists(t, cache.Path(src))
}
