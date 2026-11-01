package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nagare-project/nagare/internal/api"
	"github.com/nagare-project/nagare/internal/api/sources"
	"github.com/nagare-project/nagare/internal/torrentstream"
)

// 桌面版把 api 与 api/sources 两边的路由都挂上：少挂 api/sources，磁力、搜索、来源插件的按钮全部 404，
// 而两个包各自的测试都还是绿的 —— 只有这里看得见。
func TestRegisterRoutesMountsCoreAndSourcesEndpoints(t *testing.T) {
	mux := http.NewServeMux()
	registerRoutes(api.New(api.Deps{}), sources.New(sourcesDeps(&services{})), nil)(mux)
	for _, route := range []struct{ method, target, pattern string }{
		{http.MethodGet, "/api/library", "GET /api/library"},
		{http.MethodGet, "/api/settings", "GET /api/settings"},
		{http.MethodPost, "/api/cover-cache/clear", "POST /api/cover-cache/clear"},
		{http.MethodPost, "/api/torrent/play", "POST /api/torrent/play"},
		{http.MethodGet, "/api/search?q=x", "GET /api/search"},
		{http.MethodPost, "/api/source-plugin/play", "POST /api/source-plugin/play"},
		{http.MethodGet, "/api/sources", "GET /api/sources"},
		{http.MethodGet, "/api/downloads", "GET /api/downloads"},
	} {
		_, pattern := mux.Handler(httptest.NewRequest(route.method, route.target, nil))
		assert.Equal(t, route.pattern, pattern, "%s %s", route.method, route.target)
	}
}

// 磁力引擎 / 下载器没建起来（nil 指针）时，接口字段必须是真正的 nil：
// 「非 nil 接口包着 nil 指针」会让降级判断失效，端点不答 503 而是调用时 panic。
func TestSourcesDepsKeepsMissingEngineNil(t *testing.T) {
	deps := sourcesDeps(&services{})
	assert.Nil(t, deps.Torrent)
	assert.Nil(t, deps.Downloads)

	mux := http.NewServeMux()
	sources.New(deps).Register(mux)
	for _, target := range []string{"/api/torrent/status", "/api/downloads"} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
		assert.Equal(t, http.StatusServiceUnavailable, rec.Code, target)
	}

	built := sourcesDeps(&services{torrent: &torrentstream.Engine{}, downloads: &torrentstream.Downloader{}})
	require.NotNil(t, built.Torrent)
	require.NotNil(t, built.Downloads)
}
