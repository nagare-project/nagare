package update

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// quietLogs 屏蔽被测代码的日志输出，避免快节奏的后台循环刷屏。
func quietLogs(t *testing.T) {
	t.Helper()
	prev := log.Writer()
	log.SetOutput(io.Discard)
	t.Cleanup(func() { log.SetOutput(prev) })
}

// envelope 对应 httpserver 的统一信封。
type envelope struct {
	Success bool            `json:"success"`
	Data    json.RawMessage `json:"data"`
	Error   string          `json:"error"`
}

func do(t *testing.T, mux *http.ServeMux, method, path, body string) (int, envelope) {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	var env envelope
	if rec.Code != http.StatusMethodNotAllowed {
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env), "body=%s", rec.Body.String())
	}
	return rec.Code, env
}

func decodeView(t *testing.T, env envelope) View {
	t.Helper()
	var v View
	require.NoError(t, json.Unmarshal(env.Data, &v))
	return v
}

func TestEndpointsReturnEnvelopeWithView(t *testing.T) {
	s := newStub(t, http.StatusOK, goodRelease("v0.2.0"))
	dir := t.TempDir()
	c := newChecker(t, s, newClock(), Options{CacheDir: dir})
	mux := http.NewServeMux()
	c.Register(mux)

	// GET：不联网，字段齐全，checkedAt 为 null。
	code, env := do(t, mux, http.MethodGet, "/api/update", "")
	assert.Equal(t, http.StatusOK, code)
	assert.True(t, env.Success)
	var raw map[string]any
	require.NoError(t, json.Unmarshal(env.Data, &raw))
	for _, k := range []string{"enabled", "current", "latest", "available", "url", "checkedAt", "error"} {
		assert.Contains(t, raw, k)
	}
	assert.Nil(t, raw["checkedAt"])
	assert.Equal(t, "", raw["error"])
	assert.Equal(t, int32(0), s.hits.Load())

	// POST check：强制联网。
	code, env = do(t, mux, http.MethodPost, "/api/update/check", "")
	assert.Equal(t, http.StatusOK, code)
	assert.True(t, env.Success)
	v := decodeView(t, env)
	assert.Equal(t, "0.2.0", v.Latest)
	assert.True(t, v.Available)
	require.NotNil(t, v.CheckedAt)
	assert.Equal(t, int32(1), s.hits.Load())

	// POST config：开关落盘。
	code, env = do(t, mux, http.MethodPost, "/api/update/config", `{"enabled":false}`)
	assert.Equal(t, http.StatusOK, code)
	assert.True(t, env.Success)
	assert.False(t, decodeView(t, env).Enabled)
	disk, err := os.ReadFile(filepath.Join(dir, cacheFileName))
	require.NoError(t, err)
	assert.Contains(t, string(disk), `"enabled":false`)

	// 坏请求。
	code, env = do(t, mux, http.MethodPost, "/api/update/config", `{"enabled":`)
	assert.Equal(t, http.StatusBadRequest, code)
	assert.False(t, env.Success)
	assert.NotEmpty(t, env.Error)
	code, env = do(t, mux, http.MethodPost, "/api/update/config", `{}`)
	assert.Equal(t, http.StatusBadRequest, code)
	assert.Contains(t, env.Error, "enabled")

	// 超限的请求体明确拒绝，而不是截断后报「不是合法 JSON」。
	huge := `{"enabled":false,"pad":"` + strings.Repeat("a", maxRequestBytes) + `"}`
	code, env = do(t, mux, http.MethodPost, "/api/update/config", huge)
	assert.Equal(t, http.StatusRequestEntityTooLarge, code)
	assert.Contains(t, env.Error, "过大")
	assert.False(t, c.View().Enabled, "超限请求不改变设置")

	// 方法不匹配由 ServeMux 拦下。
	code, _ = do(t, mux, http.MethodGet, "/api/update/check", "")
	assert.Equal(t, http.StatusMethodNotAllowed, code)
}

func TestCheckEndpointReportsFailureInsideView(t *testing.T) {
	s := newStub(t, http.StatusInternalServerError, "")
	c := newChecker(t, s, nil, Options{})
	mux := http.NewServeMux()
	c.Register(mux)

	code, env := do(t, mux, http.MethodPost, "/api/update/check", "")
	assert.Equal(t, http.StatusOK, code, "检查失败仍是 200，错误在 View.error 里")
	assert.True(t, env.Success)
	assert.Contains(t, decodeView(t, env).Error, "返回错误")
}

func TestConfigEndpointSaveFailure(t *testing.T) {
	dir := t.TempDir()
	// 缓存路径被目录占住：读取失败按空处理，写入失败要报给用户。
	require.NoError(t, os.Mkdir(filepath.Join(dir, cacheFileName), 0o700))
	c := newChecker(t, nil, nil, Options{CacheDir: dir})
	assert.True(t, c.View().Enabled)
	mux := http.NewServeMux()
	c.Register(mux)

	code, env := do(t, mux, http.MethodPost, "/api/update/config", `{"enabled":false}`)
	assert.Equal(t, http.StatusInternalServerError, code)
	assert.False(t, env.Success)
	assert.Contains(t, env.Error, "保存更新设置失败")
	assert.False(t, c.View().Enabled, "本次运行内仍按用户意愿生效")
}

func TestCachePersistsAcrossCheckers(t *testing.T) {
	dir := t.TempDir()
	s := newStub(t, http.StatusOK, goodRelease("v0.2.0"))
	clk := newClock()
	ctx := context.Background()

	a := newChecker(t, s, clk, Options{CacheDir: dir})
	a.Check(ctx, true)
	_, err := a.SetEnabled(false)
	require.NoError(t, err)
	info, err := os.Stat(filepath.Join(dir, cacheFileName))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	_, err = os.Stat(filepath.Join(dir, cacheFileName+".tmp"))
	assert.True(t, os.IsNotExist(err), "原子写不留临时文件")

	clk.Advance(time.Hour)
	b := newChecker(t, s, clk, Options{CacheDir: dir})
	v := b.View()
	assert.Equal(t, "0.2.0", v.Latest)
	assert.Equal(t, "https://github.com/nagare-project/nagare/releases/tag/v0.2.0", v.URL)
	assert.True(t, v.Available)
	assert.False(t, v.Enabled)
	require.NotNil(t, v.CheckedAt)
	assert.Equal(t, *a.View().CheckedAt, *v.CheckedAt)

	b.Check(ctx, false)
	assert.Equal(t, int32(1), s.hits.Load(), "新实例读回缓存后 24 小时内不联网")
}

func TestCorruptOrTamperedCacheTreatedAsEmpty(t *testing.T) {
	const evil = `"url":"https://evil.example/nagare-project/nagare/releases/tag/v0.2.0"`
	cases := []struct {
		name        string
		raw         string
		wantEnabled bool
	}{
		{"不是 JSON", "{not json", true},
		{"发布页地址被篡改", `{"enabled":true,"lastCheckAt":123,"latest":"0.2.0",` + evil + `}`, true},
		{"版本号不合法但开关保留", `{"enabled":false,"lastCheckAt":123,"latest":"bad tag","url":"x"}`, false},
		{"负时间戳", `{"enabled":false,"lastCheckAt":-5,"latest":"","url":"leftover"}`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(dir, cacheFileName), []byte(tc.raw), 0o600))
			c := newChecker(t, nil, nil, Options{CacheDir: dir})
			v := c.View()
			assert.Equal(t, tc.wantEnabled, v.Enabled)
			assert.Empty(t, v.Latest)
			assert.Empty(t, v.URL)
			assert.False(t, v.Available)
			assert.Nil(t, v.CheckedAt, "丢掉版本信息时时间戳一并丢掉")
		})
	}
}

func TestStartSkipsWhenDisabled(t *testing.T) {
	quietLogs(t)
	s := newStub(t, http.StatusOK, goodRelease("v0.2.0"))
	c := newChecker(t, s, nil, Options{})
	_, err := c.SetEnabled(false)
	require.NoError(t, err)
	c.startDelay, c.interval = 5*time.Millisecond, 5*time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	c.Start(ctx)
	time.Sleep(60 * time.Millisecond)
	assert.Equal(t, int32(0), s.hits.Load(), "关闭时后台不联网")
	assert.Nil(t, c.View().CheckedAt)
}

func TestStartChecksWhenEnabledAndRespectsCache(t *testing.T) {
	quietLogs(t)
	s := newStub(t, http.StatusOK, goodRelease("v0.2.0"))
	c := newChecker(t, s, nil, Options{})
	c.startDelay, c.interval = 5*time.Millisecond, 5*time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	c.Start(ctx)
	require.Eventually(t, func() bool { return s.hits.Load() == 1 }, time.Second, 5*time.Millisecond)
	assert.Equal(t, "0.2.0", c.View().Latest)
	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, int32(1), s.hits.Load(), "后续周期命中 24 小时缓存，不再联网")
	cancel()
}

func TestScheduledCheckLogsOneLinePerRun(t *testing.T) {
	var buf bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(prev) })

	cases := []struct {
		name    string
		status  int
		body    string
		current string
		want    string
	}{
		{"发现新版本", 200, goodRelease("v0.2.0"), "0.1.0", "发现新版本"},
		{"已是最新", 200, goodRelease("v0.1.0"), "0.1.0", "已是最新"},
		{"尚无发布", 404, "", "0.1.0", "尚无正式发布"},
		{"检查失败", 500, "", "0.1.0", "检查更新失败"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			buf.Reset()
			s := newStub(t, tc.status, tc.body)
			c := newChecker(t, s, nil, Options{CurrentVersion: tc.current})
			c.scheduledCheck(context.Background())
			out := buf.String()
			assert.Equal(t, 1, strings.Count(out, "\n"), "恰好一行：%q", out)
			assert.Contains(t, out, tc.want)
			assert.NotContains(t, out, "http", "日志不记 URL")
		})
	}

	t.Run("已关闭", func(t *testing.T) {
		buf.Reset()
		s := newStub(t, http.StatusOK, goodRelease("v0.2.0"))
		c := newChecker(t, s, nil, Options{})
		_, err := c.SetEnabled(false)
		require.NoError(t, err)
		c.scheduledCheck(context.Background())
		assert.Contains(t, buf.String(), "已关闭")
		assert.Equal(t, int32(0), s.hits.Load())
	})
}
