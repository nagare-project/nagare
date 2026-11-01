package main

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
)

func TestExpandNowReplacesPlaceholders(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"秒", `{"at":"@now"}`, `{"at":1800000000}`},
		{"毫秒", `{"at":"@nowms"}`, `{"at":1800000000000}`},
		{"往后", `{"at":"@now+7200"}`, `{"at":1800007200}`},
		{"往前（毫秒）", `{"at":"@nowms-60"}`, `{"at":1799999940000}`},
		{"普通字符串不动", `{"title":"@nowhere","at":"now"}`, `{"title":"@nowhere","at":"now"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, string(expandNow([]byte(tc.in), now)))
		})
	}
}

func writeFile(t *testing.T, dir, name, body string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600))
}

func TestLoadFixturesRejectsBadInput(t *testing.T) {
	cases := []struct {
		name   string
		routes string
		files  map[string]string
	}{
		{"路由表不是 JSON", `{`, nil},
		{"数据文件缺失", `{"GET /api/x": "x.json"}`, nil},
		{"数据文件不是 JSON", `{"GET /api/x": "x.json"}`, map[string]string{"x.json": `{"a":`}},
		{"数据文件不在同一目录", `{"GET /api/x": "../x.json"}`, nil},
		{"事件流里有一行不是 JSON", `{"GET /api/x": "x.ndjson"}`, map[string]string{"x.ndjson": "{\"event\":\"scope\"}\n{\"event\":\n"}},
		{"事件流是空的", `{"GET /api/x": "x.ndjson"}`, map[string]string{"x.ndjson": "\n\n"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeFile(t, dir, "routes.json", tc.routes)
			for name, body := range tc.files {
				writeFile(t, dir, name, body)
			}
			_, err := loadFixtures(dir)
			assert.Error(t, err)
		})
	}
}

// 仓库里真正用的那份假数据：每个文件都要读得出来，路由模式都要能挂进 ServeMux（重复或冲突会 panic）。
func TestRepositoryFixturesLoadAndServe(t *testing.T) {
	routes, err := loadFixtures(filepath.Join("..", "..", "frontend", "e2e", "fixtures"))
	require.NoError(t, err)
	require.NotEmpty(t, routes)

	mux := http.NewServeMux()
	register(routes)(mux)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/library", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	var env struct {
		Success bool `json:"success"`
		Data    struct {
			ScannedAt int64 `json:"scannedAt"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
	assert.True(t, env.Success)
	assert.InDelta(t, time.Now().UnixMilli(), env.Data.ScannedAt, float64(time.Minute.Milliseconds()), "@nowms 要换成当时的毫秒")

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/not-in-fixtures", nil))
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

// 事件流原样回答：不套信封、Content-Type 是 application/x-ndjson，时间占位串照样换掉。
func TestNDJSONFixtureServedRaw(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "routes.json", `{"GET /api/stream": "stream.ndjson"}`)
	writeFile(t, dir, "stream.ndjson", "{\"event\":\"scope\",\"at\":\"@now\"}\n\n{\"event\":\"done\"}\n")
	routes, err := loadFixtures(dir)
	require.NoError(t, err)

	mux := http.NewServeMux()
	register(routes)(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/stream", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/x-ndjson; charset=utf-8", rec.Header().Get("Content-Type"))
	assert.NotContains(t, rec.Body.String(), "success")
	assert.NotContains(t, rec.Body.String(), "@now")
	assert.Contains(t, rec.Body.String(), `{"event":"done"}`)
}
