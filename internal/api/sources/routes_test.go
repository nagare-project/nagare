package sources

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

// 从 internal/api 拆出来之后路由路径一字不差：前端按这些路径调用，改了就是静默断掉一个按钮。
// 与 api.Handler 挂在同一个 mux 上不冲突（newEnv 每次都两边一起注册，重复注册会直接 panic）。
func TestRegisterKeepsRoutePaths(t *testing.T) {
	mux := http.NewServeMux()
	New(Deps{}).Register(mux)
	routes := []struct{ method, target, pattern string }{
		{http.MethodGet, "/api/search?q=x", "GET /api/search"},
		{http.MethodGet, "/api/search/plugin?q=x", "GET /api/search/plugin"},
		{http.MethodGet, "/api/sources", "GET /api/sources"},
		{http.MethodPost, "/api/sources/reload", "POST /api/sources/reload"},
		{http.MethodPost, "/api/sources/sync", "POST /api/sources/sync"},
		{http.MethodPost, "/api/sources/config", "POST /api/sources/config"},
		{http.MethodPost, "/api/sources/fake/enabled", "POST /api/sources/{id}/enabled"},
		{http.MethodPost, "/api/sources/fake/selfcheck", "POST /api/sources/{id}/selfcheck"},
		{http.MethodGet, "/api/source-plugin", "GET /api/source-plugin"},
		{http.MethodPost, "/api/source-plugin/config", "POST /api/source-plugin/config"},
		{http.MethodPost, "/api/source-plugin/candidates", "POST /api/source-plugin/candidates"},
		{http.MethodPost, "/api/source-plugin/play", "POST /api/source-plugin/play"},
		{http.MethodPost, "/api/torrent/play", "POST /api/torrent/play"},
		{http.MethodGet, "/api/torrent/status", "GET /api/torrent/status"},
		{http.MethodPost, "/api/torrent/stop", "POST /api/torrent/stop"},
		{http.MethodPost, "/api/torrent/cache/clear", "POST /api/torrent/cache/clear"},
		{http.MethodPost, "/api/torrent/config", "POST /api/torrent/config"},
		{http.MethodGet, "/api/downloads", "GET /api/downloads"},
		{http.MethodPost, "/api/downloads", "POST /api/downloads"},
		{http.MethodDelete, "/api/downloads/abc", "DELETE /api/downloads/{id}"},
	}
	for _, route := range routes {
		_, pattern := mux.Handler(httptest.NewRequest(route.method, route.target, nil))
		assert.Equal(t, route.pattern, pattern, "%s %s", route.method, route.target)
	}
}
