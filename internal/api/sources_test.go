package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nagare-project/nagare/internal/rules"
)

const rssBody = `<?xml version="1.0"?><rss version="2.0"><channel>
<item><title>[G] Show - 01</title><enclosure url="magnet:?xt=urn:btih:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" length="1500000000"/></item>
</channel></rss>`

// 起一个假源站，并写一条指向它的规则到临时目录。
func fakeSource(t *testing.T) (srvURL string, ruleDir string) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("q") == "nothing" {
			_, _ = w.Write([]byte(`<rss><channel></channel></rss>`))
			return
		}
		_, _ = w.Write([]byte(rssBody))
	}))
	t.Cleanup(srv.Close)
	dir := t.TempDir()
	rule := strings.ReplaceAll(`schema: 1
id: fake
name: Fake Source
homepage: https://example.test
request: { url: "{{base}}/rss?q={{query}}" }
format: xml
items: rss/channel/item
fields:
  title: title
  magnet: "enclosure@url"
  size: { path: "enclosure@length", transforms: [format_bytes] }
  fansub: { path: "$title", transforms: [parse_fansub] }
selftest: { query: anything }
`, "{{base}}", srv.URL)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "fake.yaml"), []byte(rule), 0o600))
	return srv.URL, dir
}

// 零内置源：没配置规则时源列表为空，搜索返回空但不报错。
func TestSourcesEmptyByDefault(t *testing.T) {
	env := newEnv(t)
	var view SourcesView
	require.NoError(t, json.Unmarshal(decode(t, env.do(t, http.MethodGet, "/api/sources", "")).Data, &view))
	assert.Empty(t, view.Sources)
	assert.Equal(t, 0, view.Rules.Loaded)

	var res rules.SearchResult
	require.NoError(t, json.Unmarshal(decode(t, env.do(t, http.MethodGet, "/api/search?q=x", "")).Data, &res))
	assert.Empty(t, res.Items)
	assert.Empty(t, res.Sources)
}

// 配置本地规则目录 → 加载 → 搜索出结果 → 源状态 ok；禁用后源状态 disabled 且持久化。
func TestSourcesLocalDirSearchAndToggle(t *testing.T) {
	env := newEnv(t)
	_, dir := fakeSource(t)

	rec := env.do(t, http.MethodPost, "/api/sources/config", `{"localDir":`+string(mustJSON(t, dir))+`}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var rv RulesView
	require.NoError(t, json.Unmarshal(decode(t, rec).Data, &rv))
	assert.Equal(t, 1, rv.Loaded)
	assert.Empty(t, rv.Errors)

	var view SourcesView
	require.NoError(t, json.Unmarshal(decode(t, env.do(t, http.MethodGet, "/api/sources", "")).Data, &view))
	require.Len(t, view.Sources, 1)
	assert.Equal(t, "fake", view.Sources[0].ID)
	assert.True(t, view.Sources[0].Enabled)
	assert.True(t, view.Sources[0].HasSelfTest)

	var res rules.SearchResult
	require.NoError(t, json.Unmarshal(decode(t, env.do(t, http.MethodGet, "/api/search?q=%E8%8A%99%E8%8E%89%E8%8E%B2", "")).Data, &res))
	require.Len(t, res.Items, 1)
	assert.Equal(t, "1.5 GB", res.Items[0].Size)
	assert.Equal(t, "G", *res.Items[0].Fansub)
	require.Len(t, res.Sources, 1)
	assert.Equal(t, rules.StateOK, res.Sources[0].State)

	// 零结果与源异常可辨：q=nothing 上游正常返回空。
	require.NoError(t, json.Unmarshal(decode(t, env.do(t, http.MethodGet, "/api/search?q=nothing", "")).Data, &res))
	assert.Equal(t, rules.StateZero, res.Sources[0].State)

	// 禁用：结果集缩小，状态持久化。
	rec = env.do(t, http.MethodPost, "/api/sources/fake/enabled", `{"enabled":false}`)
	require.Equal(t, http.StatusOK, rec.Code)
	require.NoError(t, json.Unmarshal(decode(t, env.do(t, http.MethodGet, "/api/search?q=x", "")).Data, &res))
	assert.Empty(t, res.Items)
	assert.Equal(t, rules.StateDisabled, res.Sources[0].State)
	assert.Equal(t, []string{"fake"}, env.store.RulesConfig().Disabled)

	rec = env.do(t, http.MethodPost, "/api/sources/ghost/enabled", `{"enabled":true}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)

	// 自检。
	rec = env.do(t, http.MethodPost, "/api/sources/fake/selfcheck", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var out rules.Outcome
	require.NoError(t, json.Unmarshal(decode(t, rec).Data, &out))
	assert.Equal(t, rules.StateOK, out.State)
}

// 规则被改坏：搜索结果里该源标 dead（源异常），而不是 zero（无结果）。
func TestSourcesDeadRuleIsNotZero(t *testing.T) {
	env := newEnv(t)
	_, dir := fakeSource(t)
	// 把 magnet 路径改到一个不存在的属性上 → 上游有条目但一条都解不出。
	path := filepath.Join(dir, "fake.yaml")
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, []byte(strings.Replace(string(raw), `magnet: "enclosure@url"`, `magnet: "enclosure@gone"`, 1)), 0o600))

	env.do(t, http.MethodPost, "/api/sources/config", `{"localDir":`+string(mustJSON(t, dir))+`}`)
	var res rules.SearchResult
	require.NoError(t, json.Unmarshal(decode(t, env.do(t, http.MethodGet, "/api/search?q=x", "")).Data, &res))
	require.Len(t, res.Sources, 1)
	assert.Equal(t, rules.StateDead, res.Sources[0].State)
	assert.Contains(t, res.Sources[0].Reason, "失效")
	assert.Empty(t, res.Items)
}

// 配置校验与同步。
func TestSourcesConfigAndSync(t *testing.T) {
	env := newEnv(t)
	rec := env.do(t, http.MethodPost, "/api/sources/config", `{"remoteUrl":"http://example.com/rules"}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code, "非本机 http 拒绝")
	rec = env.do(t, http.MethodPost, "/api/sources/config", `{"localDir":"relative/dir"}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)

	rec = env.do(t, http.MethodPost, "/api/sources/sync", "")
	assert.Equal(t, http.StatusBadRequest, rec.Code, "未配置仓库地址不能同步")
	assert.Contains(t, decode(t, rec).Error, "规则仓库地址")

	// 假仓库：清单 + 一条规则。
	srvURL, dir := fakeSource(t)
	ruleBody, err := os.ReadFile(filepath.Join(dir, "fake.yaml"))
	require.NoError(t, err)
	repo := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/index.json":
			_, _ = w.Write([]byte(`{"schema":1,"rules":[{"file":"fake.yaml"}]}`))
		case "/fake.yaml":
			_, _ = w.Write(ruleBody)
		default:
			w.WriteHeader(404)
		}
	}))
	defer repo.Close()
	_ = srvURL

	rec = env.do(t, http.MethodPost, "/api/sources/config", `{"remoteUrl":`+string(mustJSON(t, repo.URL))+`}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	rec = env.do(t, http.MethodPost, "/api/sources/sync", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var rep struct {
		Added  int      `json:"added"`
		Errors []string `json:"errors"`
	}
	require.NoError(t, json.Unmarshal(decode(t, rec).Data, &rep))
	assert.Equal(t, 1, rep.Added)
	assert.Empty(t, rep.Errors)

	var view SourcesView
	require.NoError(t, json.Unmarshal(decode(t, env.do(t, http.MethodGet, "/api/sources", "")).Data, &view))
	require.Len(t, view.Sources, 1, "同步后规则应已加载")
	assert.NotNil(t, view.Rules.LastSyncAt)
	assert.Equal(t, repo.URL, view.Rules.RemoteURL)
}
