package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mediaEnv 建一个扫过一个真实目录的库服务 + 挂上媒体处理器。
func mediaEnv(t *testing.T) (*MediaHandler, *LibraryService, []ViewItem) {
	t.Helper()
	env := newEnv(t)
	dir := makeMediaDir(t)

	rec := env.do(t, http.MethodPost, "/api/library/folders", `{"path":`+string(mustJSON(t, dir))+`}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	rec = env.do(t, http.MethodGet, "/api/library", "")
	require.Equal(t, http.StatusOK, rec.Code)
	var view LibraryView
	require.NoError(t, json.Unmarshal(decode(t, rec).Data, &view))
	require.NotEmpty(t, view.Clusters)
	items := view.Clusters[0].Groups[0].Items
	require.NotEmpty(t, items)

	return NewMediaHandler(env.lib), env.lib, items
}

// serve 直接打处理器（能力段已由 httpserver 剥掉，处理器只看到 /<fileId>）。
//
// fileID 里有空格、`|`、`[]`，不是合法的 URL 路径字符 —— 生产路径上视图侧用
// url.PathEscape 编码、路由侧解码后交给处理器，这里照做，否则测的就不是同一条路。
func serve(h *MediaHandler, fileID string, hdr map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/"+url.PathEscape(fileID), nil)
	// 路由把能力段剥掉时给的是【已解码】的路径，这里对齐那个形状。
	req.URL.Path = "/" + fileID
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestMediaServesLibraryFile(t *testing.T) {
	h, lib, items := mediaEnv(t)
	fileID := items[0].FileID

	item, ok := lib.Item(fileID)
	require.True(t, ok)
	want, err := os.ReadFile(item.AbsPath)
	require.NoError(t, err)

	rec := serve(h, fileID, nil)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, want, rec.Body.Bytes())
	// 外部内容，必须禁止嗅探
	assert.Equal(t, "nosniff", rec.Header().Get("X-Content-Type-Options"))
	// 本机文件不该被任何中间层缓存
	assert.Contains(t, rec.Header().Get("Cache-Control"), "no-store")
	// 拖进度条靠 Range，ServeContent 会宣告它
	assert.Equal(t, "bytes", rec.Header().Get("Accept-Ranges"))
}

func TestMediaSupportsRange(t *testing.T) {
	// 没有 Range 就没有「拖进度条」：<video> 靠它跳转。
	h, lib, items := mediaEnv(t)
	fileID := items[0].FileID
	item, _ := lib.Item(fileID)
	full, err := os.ReadFile(item.AbsPath)
	require.NoError(t, err)
	require.Greater(t, len(full), 10)

	rec := serve(h, fileID, map[string]string{"Range": "bytes=2-5"})
	require.Equal(t, http.StatusPartialContent, rec.Code)
	assert.Equal(t, full[2:6], rec.Body.Bytes())
}

func TestMediaRejectsUnknownID(t *testing.T) {
	h, _, _ := mediaEnv(t)
	assert.Equal(t, http.StatusNotFound, serve(h, "nope", nil).Code)
	assert.Equal(t, http.StatusNotFound, serve(h, "", nil).Code)
}

// 这一条是这个端点的核心安全属性：客户端【只能】给 fileId，给不了路径。
// 路径由 LibraryService 从已扫描的条目里查，所以：
//   - 拼路径穿越无效（我们从不拼接客户端字符串）
//   - 不在媒体库里的文件取不到，哪怕知道它的绝对路径
func TestMediaRefusesPathsAndFilesOutsideLibrary(t *testing.T) {
	h, _, _ := mediaEnv(t)

	secret := filepath.Join(t.TempDir(), "secret.mp4")
	require.NoError(t, os.WriteFile(secret, []byte("不该被读到"), 0o600))

	for _, probe := range []string{
		secret,                   // 绝对路径
		"../../../etc/passwd",    // 相对穿越
		"..%2F..%2Fetc%2Fpasswd", // 编码穿越
		"/etc/passwd",            // 绝对系统路径
		strings.Repeat("../", 12) + "etc/passwd",
	} {
		rec := serve(h, probe, nil)
		assert.Equalf(t, http.StatusNotFound, rec.Code, "探测 %q 应当 404", probe)
		assert.NotContains(t, rec.Body.String(), "不该被读到")
	}
}

func TestMediaMissingFileIsNotFound(t *testing.T) {
	// 文件被移走/改名是常态（软 id 挂 name|size|mtime），不是服务器错误。
	h, lib, items := mediaEnv(t)
	fileID := items[0].FileID
	item, ok := lib.Item(fileID)
	require.True(t, ok)
	require.NoError(t, os.Remove(item.AbsPath))

	rec := serve(h, fileID, nil)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestLibraryViewCarriesStreamURL(t *testing.T) {
	env := newEnv(t)
	dir := makeMediaDir(t)
	rec := env.do(t, http.MethodPost, "/api/library/folders", `{"path":`+string(mustJSON(t, dir))+`}`)
	require.Equal(t, http.StatusOK, rec.Code)

	// 未挂载媒体端点：视图里不该出现 stream 字段（界面据此隐藏「浏览器播」入口）
	rec = env.do(t, http.MethodGet, "/api/library", "")
	var view LibraryView
	require.NoError(t, json.Unmarshal(decode(t, rec).Data, &view))
	assert.Empty(t, view.Clusters[0].Groups[0].Items[0].Stream)

	// 挂载之后每个条目带上自己的地址，且【只含 fileId】，不含真实路径
	env.lib.SetMediaPrefix("/media/abc123")
	rec = env.do(t, http.MethodGet, "/api/library", "")
	require.NoError(t, json.Unmarshal(decode(t, rec).Data, &view))
	item := view.Clusters[0].Groups[0].Items[0]
	require.NotEmpty(t, item.Stream)
	assert.True(t, strings.HasPrefix(item.Stream, "/media/abc123/"))
	assert.NotContains(t, item.Stream, dir, "地址里不该出现真实路径")
}
