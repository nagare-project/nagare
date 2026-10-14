package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nagare-project/nagare/internal/sourceinstall"
	"github.com/nagare-project/nagare/internal/sourceplugin"
)

const (
	demoSourceURL  = "https://sources.example/demo-bt.yaml"
	demoSourceYAML = "id: demo-bt\nname: Demo BT\nkind: bt\n"
	demoPackURL    = "https://sources.example/packs/demo.json"
	demoPackID     = "nagare-source-demo"
)

// packContent 造一个来源包；members 是成员来源文档。
func packContent(t *testing.T, schema string, members ...map[string]any) string {
	t.Helper()
	content, err := json.Marshal(map[string]any{
		"schema": schema, "id": demoPackID, "name": "示例来源包", "description": "测试用", "version": "abc123",
		"sources": members,
	})
	require.NoError(t, err)
	return string(content)
}

func member(id, kind string) map[string]any {
	return map[string]any{"id": id, "name": strings.ToUpper(id), "kind": kind}
}

type fakeSourceFetcher struct {
	files map[string]string
	calls int
}

func (f *fakeSourceFetcher) Fetch(_ context.Context, rawURL string) (sourceinstall.Download, error) {
	f.calls++
	content, ok := f.files[rawURL]
	if !ok {
		return sourceinstall.Download{}, errors.New("下载来源文件失败：服务器返回 HTTP 404")
	}
	return sourceinstall.Download{
		URL: rawURL, Content: []byte(content), SHA256: sha256Hex(content), Format: sourceinstall.DetectFormat([]byte(content)),
	}, nil
}

func sha256Hex(content string) string {
	digest := sha256.Sum256([]byte(content))
	return hex.EncodeToString(digest[:])
}

func demoSummary() sourceplugin.SourceSummary {
	return sourceplugin.SourceSummary{
		ID: "demo-bt", Name: "Demo BT", Kind: "bt", Tier: 2, Version: "1.0.0", Enabled: true,
		Ecosystem: "community", Upstream: "https://upstream.example", License: "MIT", AllowedHosts: []string{"bt.example"},
	}
}

type installEnv struct {
	*testEnv
	installs *sourceinstall.Store
	fetcher  *fakeSourceFetcher
	// repo 是插件仓库自带的来源；独立安装的来源由 sourcesFn 从安装目录读出。
	repo []sourceplugin.Source
}

// newInstallEnv 造一个已启用、支持 installed_sources 的插件，并接上真实的安装存储。
func newInstallEnv(t *testing.T) *installEnv {
	t.Helper()
	env := &installEnv{
		testEnv:  newEnv(t),
		installs: sourceinstall.NewStore(t.TempDir()),
		fetcher: &fakeSourceFetcher{files: map[string]string{
			demoSourceURL: demoSourceYAML,
			demoPackURL:   packContent(t, sourceinstall.PackSchema, member("alpha-bt", "bt"), member("beta-web", "web")),
		}},
		repo: []sourceplugin.Source{{ID: "repo-bt", Name: "Repo BT", Kind: "bt", Enabled: true, Status: "healthy"}},
	}
	env.pluginService.SetSourceInstaller(env.installs, env.fetcher)
	env.plugin.capabilities = []string{"bt", sourceplugin.CapabilityInstalledSources}
	env.plugin.inspect = func(format, content string) (sourceplugin.SourceSummary, error) {
		if format == "yaml" && content == demoSourceYAML {
			return demoSummary(), nil
		}
		var document struct {
			ID      string `json:"id"`
			Name    string `json:"name"`
			Kind    string `json:"kind"`
			Enabled *bool  `json:"enabled"`
		}
		if format == "json" && json.Unmarshal([]byte(content), &document) == nil && document.ID != "" && document.ID != "broken" {
			return sourceplugin.SourceSummary{
				ID: document.ID, Name: document.Name, Kind: document.Kind, Tier: 1, Version: "2.0.0",
				Enabled: document.Enabled == nil || *document.Enabled, AllowedHosts: []string{document.ID + ".example"},
			}, nil
		}
		return sourceplugin.SourceSummary{}, &sourceplugin.InspectError{Category: "invalid_source", Message: "unexpected document"}
	}
	env.plugin.sourcesFn = func(config sourceplugin.LaunchConfig) []sourceplugin.Source {
		installed := map[string]bool{}
		entries, _ := os.ReadDir(config.InstalledDir)
		for _, entry := range entries {
			installed[strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))] = true
		}
		sources := []sourceplugin.Source{}
		for _, source := range env.repo {
			if !installed[source.ID] {
				sources = append(sources, source)
			}
		}
		for id := range installed {
			sources = append(sources, sourceplugin.Source{ID: id, Name: id, Kind: "bt", Enabled: true, Status: "healthy"})
		}
		return sources
	}
	rec := env.do(t, http.MethodPost, "/api/source-plugin/config", `{"enabled":true,"executable":"/opt/nagare-source","root":"/srv/sources"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	return env
}

func installBody(url, sha string) string {
	body, _ := json.Marshal(sourceInstallRequest{URL: url, SHA256: sha})
	return string(body)
}

func TestSourcePluginPassesInstalledDirToPlugin(t *testing.T) {
	env := newInstallEnv(t)
	assert.Equal(t, env.installs.SourcesDir(), env.plugin.started.InstalledDir)

	rec := env.do(t, http.MethodGet, "/api/source-plugin", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var view SourcePluginView
	require.NoError(t, json.Unmarshal(decode(t, rec).Data, &view))
	assert.True(t, view.InstallSupported)
	assert.Empty(t, view.Installed)

	env.plugin.capabilities = nil
	rec = env.do(t, http.MethodPost, "/api/source-plugin/config", `{"enabled":true}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.NoError(t, json.Unmarshal(decode(t, rec).Data, &view))
	assert.False(t, view.InstallSupported, "plugins without installed_sources cannot install")
}

func TestSourcePluginViewWithoutInstallerKeepsInstallOff(t *testing.T) {
	env := newEnv(t)
	rec := env.do(t, http.MethodGet, "/api/source-plugin", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `"installed":[]`)
	assert.Contains(t, rec.Body.String(), `"installSupported":false`)

	rec = env.do(t, http.MethodPost, "/api/source-plugin/sources/preview", installBody(demoSourceURL, ""))
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code, rec.Body.String())
}

func TestSourcePreviewReturnsSummaryContentAndReplacement(t *testing.T) {
	env := newInstallEnv(t)
	env.repo = append(env.repo, sourceplugin.Source{ID: "demo-bt", Name: "Repo Demo", Kind: "bt", Version: "0.9.0", Enabled: true})

	rec := env.do(t, http.MethodPost, "/api/source-plugin/sources/preview", installBody(demoSourceURL, strings.ToUpper(sha256Hex(demoSourceYAML))))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var preview SourcePreview
	require.NoError(t, json.Unmarshal(decode(t, rec).Data, &preview))
	assert.Equal(t, demoSourceURL, preview.URL)
	assert.Equal(t, sha256Hex(demoSourceYAML), preview.SHA256)
	assert.Equal(t, len(demoSourceYAML), preview.Bytes)
	assert.Equal(t, "yaml", preview.Format)
	assert.Equal(t, demoSourceYAML, preview.Content)
	assert.Equal(t, SourcePackageInfo{ID: "demo-bt", Name: "Demo BT", Version: "1.0.0"}, preview.Package)
	require.Len(t, preview.Sources, 1)
	assert.Equal(t, demoSummary(), preview.Sources[0].Source)
	assert.Nil(t, preview.Installed)
	assert.Empty(t, preview.Conflicts)
	require.NotNil(t, preview.Sources[0].Existing)
	assert.Equal(t, "0.9.0", preview.Sources[0].Existing.Version)

	entries, _ := os.ReadDir(env.installs.SourcesDir())
	assert.Empty(t, entries, "preview must not write anything")
}

func TestSourcePreviewRejectsBadInputBeforeInspecting(t *testing.T) {
	env := newInstallEnv(t)
	cases := []struct {
		name, body string
		status     int
		want       string
	}{
		{"http url", installBody("http://sources.example/a.yaml", ""), http.StatusBadRequest, "https"},
		{"bad sha", installBody(demoSourceURL, "abc"), http.StatusBadRequest, "格式不对"},
		{"sha mismatch", installBody(demoSourceURL, strings.Repeat("0", 64)), http.StatusBadRequest, "摘要不符"},
		{"download failure", installBody("https://sources.example/missing.yaml", ""), http.StatusBadGateway, "HTTP 404"},
		{"unknown field", `{"url":"` + demoSourceURL + `","extra":1}`, http.StatusBadRequest, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := env.do(t, http.MethodPost, "/api/source-plugin/sources/preview", tc.body)
			require.Equal(t, tc.status, rec.Code, rec.Body.String())
			assert.Contains(t, decode(t, rec).Error, tc.want)
		})
	}
	assert.Zero(t, env.plugin.inspected)
}

func TestSourcePreviewRequiresCapableReadyPlugin(t *testing.T) {
	env := newInstallEnv(t)
	env.plugin.capabilities = nil
	require.Equal(t, http.StatusOK, env.do(t, http.MethodPost, "/api/source-plugin/config", `{"enabled":true}`).Code)
	rec := env.do(t, http.MethodPost, "/api/source-plugin/sources/preview", installBody(demoSourceURL, ""))
	require.Equal(t, http.StatusServiceUnavailable, rec.Code, rec.Body.String())
	assert.Contains(t, decode(t, rec).Error, "版本过旧")

	require.Equal(t, http.StatusOK, env.do(t, http.MethodPost, "/api/source-plugin/config", `{"enabled":false}`).Code)
	rec = env.do(t, http.MethodPost, "/api/source-plugin/sources/preview", installBody(demoSourceURL, ""))
	require.Equal(t, http.StatusServiceUnavailable, rec.Code, rec.Body.String())
	assert.Contains(t, decode(t, rec).Error, "尚未就绪")
	assert.Zero(t, env.fetcher.calls, "nothing is downloaded while the plugin cannot inspect it")
}

func TestSourcePreviewReportsPluginRejection(t *testing.T) {
	env := newInstallEnv(t)
	env.fetcher.files[demoSourceURL] = "id: broken\n"
	rec := env.do(t, http.MethodPost, "/api/source-plugin/sources/preview", installBody(demoSourceURL, ""))
	require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	assert.Contains(t, decode(t, rec).Error, "没有通过插件校验：unexpected document")
}

func TestSourceInstallSavesFileRecordsOriginAndRestartsPlugin(t *testing.T) {
	env := newInstallEnv(t)
	startsBefore := len(env.plugin.starts)

	rec := env.do(t, http.MethodPost, "/api/source-plugin/sources/install", installBody(demoSourceURL, ""))
	require.Equal(t, http.StatusBadRequest, rec.Code, "install requires the digest the user confirmed")

	rec = env.do(t, http.MethodPost, "/api/source-plugin/sources/install", installBody(demoSourceURL, sha256Hex(demoSourceYAML)))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var view SourcePluginView
	require.NoError(t, json.Unmarshal(decode(t, rec).Data, &view))
	require.Len(t, view.Installed, 1)
	record := view.Installed[0]
	assert.Equal(t, "demo-bt", record.ID)
	assert.Equal(t, "Demo BT", record.Name)
	assert.Equal(t, "1.0.0", record.Version)
	assert.False(t, record.Pack)
	assert.Equal(t, demoSourceURL, record.URL)
	assert.Equal(t, sha256Hex(demoSourceYAML), record.SHA256)
	assert.False(t, record.InstalledAt.IsZero())
	assert.Equal(t, []sourceinstall.PackageSource{{ID: "demo-bt", Name: "Demo BT", Kind: "bt", Version: "1.0.0", Format: "yaml"}}, record.Sources)

	content, err := os.ReadFile(filepath.Join(env.installs.SourcesDir(), "demo-bt.yaml"))
	require.NoError(t, err)
	assert.Equal(t, demoSourceYAML, string(content))
	require.Len(t, env.plugin.starts, startsBefore+1, "install restarts the plugin once")
	assert.Equal(t, env.installs.SourcesDir(), env.plugin.started.InstalledDir)
	assert.Equal(t, "/opt/nagare-source", env.plugin.started.Executable)
	assert.True(t, containsSource(view.Sources, "demo-bt"))

	rec = env.do(t, http.MethodPost, "/api/source-plugin/sources/preview", installBody(demoSourceURL, ""))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var preview SourcePreview
	require.NoError(t, json.Unmarshal(decode(t, rec).Data, &preview))
	require.NotNil(t, preview.Installed, "a second preview shows the installed version it would update")
	assert.Equal(t, "1.0.0", preview.Installed.Version)
}

func TestSourceInstallRejectsDisabledSource(t *testing.T) {
	env := newInstallEnv(t)
	env.plugin.inspect = func(string, string) (sourceplugin.SourceSummary, error) {
		summary := demoSummary()
		summary.Enabled = false
		return summary, nil
	}
	rec := env.do(t, http.MethodPost, "/api/source-plugin/sources/install", installBody(demoSourceURL, sha256Hex(demoSourceYAML)))
	require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	assert.Contains(t, decode(t, rec).Error, "停用")
	records, err := env.installs.List()
	require.NoError(t, err)
	assert.Empty(t, records)
}

func TestSourceInstallRollsBackWhenPluginCannotRestart(t *testing.T) {
	env := newInstallEnv(t)
	env.plugin.startErrs = []error{errors.New("plugin crashed")}
	rec := env.do(t, http.MethodPost, "/api/source-plugin/sources/install", installBody(demoSourceURL, sha256Hex(demoSourceYAML)))
	require.Equal(t, http.StatusBadGateway, rec.Code, rec.Body.String())
	assert.Contains(t, decode(t, rec).Error, "已撤销安装")
	assertNotInstalled(t, env)
	assert.Equal(t, "ready", env.plugin.status.Phase, "the previous plugin is started again")
}

func TestSourceInstallRollsBackWhenPluginSkipsTheSource(t *testing.T) {
	env := newInstallEnv(t)
	env.plugin.sourcesFn = func(sourceplugin.LaunchConfig) []sourceplugin.Source { return env.repo }
	rec := env.do(t, http.MethodPost, "/api/source-plugin/sources/install", installBody(demoSourceURL, sha256Hex(demoSourceYAML)))
	require.Equal(t, http.StatusBadGateway, rec.Code, rec.Body.String())
	assertNotInstalled(t, env)
}

func TestSourceInstallRequiresEnabledPlugin(t *testing.T) {
	env := newInstallEnv(t)
	// 插件进程仍在，但用户配置已关（例如另一个请求刚关掉它）：不能落盘。
	cfg := env.store.SourcePluginConfig()
	cfg.Enabled = false
	require.NoError(t, env.store.SetSourcePluginConfig(cfg))
	rec := env.do(t, http.MethodPost, "/api/source-plugin/sources/install", installBody(demoSourceURL, sha256Hex(demoSourceYAML)))
	require.Equal(t, http.StatusServiceUnavailable, rec.Code, rec.Body.String())
	assertNotInstalled(t, env)
}

func TestSourceUninstallRemovesFileAndRestartsPlugin(t *testing.T) {
	env := newInstallEnv(t)
	rec := env.do(t, http.MethodPost, "/api/source-plugin/sources/install", installBody(demoSourceURL, sha256Hex(demoSourceYAML)))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	startsBefore := len(env.plugin.starts)

	rec = env.do(t, http.MethodDelete, "/api/source-plugin/sources/installed/demo-bt", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var view SourcePluginView
	require.NoError(t, json.Unmarshal(decode(t, rec).Data, &view))
	assert.Empty(t, view.Installed)
	assert.False(t, containsSource(view.Sources, "demo-bt"))
	assert.Len(t, env.plugin.starts, startsBefore+1)
	assertNotInstalled(t, env)

	rec = env.do(t, http.MethodDelete, "/api/source-plugin/sources/installed/demo-bt", "")
	assert.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
	rec = env.do(t, http.MethodDelete, "/api/source-plugin/sources/installed/Bad_ID", "")
	assert.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
}

func TestSourceUninstallRestoresSourceWhenRestartFails(t *testing.T) {
	env := newInstallEnv(t)
	rec := env.do(t, http.MethodPost, "/api/source-plugin/sources/install", installBody(demoSourceURL, sha256Hex(demoSourceYAML)))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	env.plugin.startErrs = []error{errors.New("plugin crashed")}
	rec = env.do(t, http.MethodDelete, "/api/source-plugin/sources/installed/demo-bt", "")
	require.Equal(t, http.StatusBadGateway, rec.Code, rec.Body.String())
	_, ok, err := env.installs.Get("demo-bt")
	require.NoError(t, err)
	assert.True(t, ok)
	_, err = os.Stat(filepath.Join(env.installs.SourcesDir(), "demo-bt.yaml"))
	assert.NoError(t, err)
}

func TestSourceUninstallWorksWhilePluginDisabled(t *testing.T) {
	env := newInstallEnv(t)
	rec := env.do(t, http.MethodPost, "/api/source-plugin/sources/install", installBody(demoSourceURL, sha256Hex(demoSourceYAML)))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, http.StatusOK, env.do(t, http.MethodPost, "/api/source-plugin/config", `{"enabled":false}`).Code)
	startsBefore := len(env.plugin.starts)

	rec = env.do(t, http.MethodDelete, "/api/source-plugin/sources/installed/demo-bt", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Len(t, env.plugin.starts, startsBefore, "a disabled plugin is not started by uninstall")
	assertNotInstalled(t, env)
}

func TestSourcePreviewSplitsPackAndInspectsEveryMember(t *testing.T) {
	env := newInstallEnv(t)
	rec := env.do(t, http.MethodPost, "/api/source-plugin/sources/preview", installBody(demoPackURL, ""))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var preview SourcePreview
	require.NoError(t, json.Unmarshal(decode(t, rec).Data, &preview))
	assert.Equal(t, SourcePackageInfo{ID: demoPackID, Name: "示例来源包", Description: "测试用", Version: "abc123", Pack: true}, preview.Package)
	require.Len(t, preview.Sources, 2)
	assert.Equal(t, "alpha-bt", preview.Sources[0].Source.ID)
	assert.Equal(t, "web", preview.Sources[1].Source.Kind)
	assert.Equal(t, 2, env.plugin.inspected)
	assert.Empty(t, preview.Conflicts)
	assert.Nil(t, preview.Installed)
	entries, _ := os.ReadDir(env.installs.SourcesDir())
	assert.Empty(t, entries, "preview must not write anything")
}

func TestSourceInstallPackSavesEveryMemberAndUninstallRemovesAll(t *testing.T) {
	env := newInstallEnv(t)
	startsBefore := len(env.plugin.starts)
	pack := env.fetcher.files[demoPackURL]
	rec := env.do(t, http.MethodPost, "/api/source-plugin/sources/install", installBody(demoPackURL, sha256Hex(pack)))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var view SourcePluginView
	require.NoError(t, json.Unmarshal(decode(t, rec).Data, &view))
	require.Len(t, view.Installed, 1)
	installed := view.Installed[0]
	assert.Equal(t, demoPackID, installed.ID)
	assert.True(t, installed.Pack)
	assert.Equal(t, "abc123", installed.Version)
	require.Len(t, installed.Sources, 2)
	assert.Equal(t, sourceinstall.PackageSource{ID: "beta-web", Name: "BETA-WEB", Kind: "web", Version: "2.0.0", Format: "json"}, installed.Sources[1])
	require.Len(t, env.plugin.starts, startsBefore+1, "the whole pack needs one restart")
	assert.True(t, containsSource(view.Sources, "alpha-bt"))
	assert.True(t, containsSource(view.Sources, "beta-web"))
	content, err := os.ReadFile(filepath.Join(env.installs.SourcesDir(), "beta-web.json"))
	require.NoError(t, err)
	assert.Equal(t, "{\n  \"id\": \"beta-web\",\n  \"kind\": \"web\",\n  \"name\": \"BETA-WEB\"\n}\n", string(content))

	rec = env.do(t, http.MethodPost, "/api/source-plugin/sources/preview", installBody(demoPackURL, ""))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var preview SourcePreview
	require.NoError(t, json.Unmarshal(decode(t, rec).Data, &preview))
	require.NotNil(t, preview.Installed, "previewing an installed pack offers an update")
	assert.Empty(t, preview.Conflicts)

	rec = env.do(t, http.MethodDelete, "/api/source-plugin/sources/installed/"+demoPackID, "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.NoError(t, json.Unmarshal(decode(t, rec).Data, &view))
	assert.False(t, containsSource(view.Sources, "alpha-bt"))
	assert.False(t, containsSource(view.Sources, "beta-web"))
	assertNotInstalled(t, env)
}

func TestSourceInstallPackRollsBackWhenAnyMemberIsNotLoaded(t *testing.T) {
	env := newInstallEnv(t)
	listInstalled := env.plugin.sourcesFn
	env.plugin.sourcesFn = func(config sourceplugin.LaunchConfig) []sourceplugin.Source {
		sources := []sourceplugin.Source{}
		for _, source := range listInstalled(config) {
			if source.ID != "beta-web" {
				sources = append(sources, source)
			}
		}
		return sources
	}
	pack := env.fetcher.files[demoPackURL]
	rec := env.do(t, http.MethodPost, "/api/source-plugin/sources/install", installBody(demoPackURL, sha256Hex(pack)))
	require.Equal(t, http.StatusBadGateway, rec.Code, rec.Body.String())
	assert.Contains(t, decode(t, rec).Error, "已撤销安装")
	assertNotInstalled(t, env)
}

func TestSourcePackConflictsWithSourceInstalledSeparately(t *testing.T) {
	env := newInstallEnv(t)
	rec := env.do(t, http.MethodPost, "/api/source-plugin/sources/install", installBody(demoSourceURL, sha256Hex(demoSourceYAML)))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	pack := packContent(t, sourceinstall.PackSchema, member("demo-bt", "bt"), member("beta-web", "web"))
	env.fetcher.files[demoPackURL] = pack

	rec = env.do(t, http.MethodPost, "/api/source-plugin/sources/preview", installBody(demoPackURL, ""))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var preview SourcePreview
	require.NoError(t, json.Unmarshal(decode(t, rec).Data, &preview))
	assert.Equal(t, []SourceConflict{{SourceID: "demo-bt", PackageID: "demo-bt", PackageName: "Demo BT"}}, preview.Conflicts)

	startsBefore := len(env.plugin.starts)
	rec = env.do(t, http.MethodPost, "/api/source-plugin/sources/install", installBody(demoPackURL, sha256Hex(pack)))
	require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	assert.Contains(t, decode(t, rec).Error, "已由「Demo BT」安装")
	assert.Len(t, env.plugin.starts, startsBefore, "a rejected install does not restart the plugin")
	packages, err := env.installs.List()
	require.NoError(t, err)
	require.Len(t, packages, 1)
	assert.Equal(t, "demo-bt", packages[0].ID)
	_, err = os.Stat(filepath.Join(env.installs.SourcesDir(), "beta-web.json"))
	assert.ErrorIs(t, err, os.ErrNotExist)
}

func TestSourcePackRejectsBadMembers(t *testing.T) {
	disabled := member("gamma-bt", "bt")
	disabled["enabled"] = false
	cases := []struct {
		name, endpoint, content, want string
	}{
		{"rejected member", "preview", packContent(t, sourceinstall.PackSchema, member("alpha-bt", "bt"), member("broken", "bt")), "来源包第 2 个来源没有通过插件校验"},
		{"duplicate member", "preview", packContent(t, sourceinstall.PackSchema, member("alpha-bt", "bt"), member("alpha-bt", "web")), "重复的来源 alpha-bt"},
		{"future schema", "preview", packContent(t, "nagare-source-pack/v9", member("alpha-bt", "bt")), "不支持的来源包格式"},
		{"disabled member", "install", packContent(t, sourceinstall.PackSchema, member("alpha-bt", "bt"), disabled), "gamma-bt 在文件里标记为停用"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := newInstallEnv(t)
			env.fetcher.files[demoPackURL] = tc.content
			rec := env.do(t, http.MethodPost, "/api/source-plugin/sources/"+tc.endpoint, installBody(demoPackURL, sha256Hex(tc.content)))
			require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
			assert.Contains(t, decode(t, rec).Error, tc.want)
			assertNotInstalled(t, env)
		})
	}
}

func assertNotInstalled(t *testing.T, env *installEnv) {
	t.Helper()
	records, err := env.installs.List()
	require.NoError(t, err)
	assert.Empty(t, records)
	entries, _ := os.ReadDir(env.installs.SourcesDir())
	assert.Empty(t, entries)
}

func containsSource(sources []sourceplugin.Source, id string) bool {
	for _, source := range sources {
		if source.ID == id {
			return true
		}
	}
	return false
}
