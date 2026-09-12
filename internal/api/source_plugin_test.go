package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nagare-project/nagare/internal/player"
	"github.com/nagare-project/nagare/internal/rules"
	"github.com/nagare-project/nagare/internal/sourceplugin"
	"github.com/nagare-project/nagare/internal/store"
)

type fakeSourcePluginRuntime struct {
	status      sourceplugin.Status
	started     sourceplugin.LaunchConfig
	startErr    error
	stopped     bool
	sources     []sourceplugin.Source
	events      []sourceplugin.Event
	lastRequest sourceplugin.ResolveRequest
}

func (f *fakeSourcePluginRuntime) Start(config sourceplugin.LaunchConfig) error {
	f.started = config
	if f.startErr != nil {
		f.status = sourceplugin.Status{Phase: "failed", Error: f.startErr.Error()}
		return f.startErr
	}
	f.stopped = false
	f.status = sourceplugin.Status{Phase: "ready", Manifest: &sourceplugin.Manifest{
		ID: "org.example.plugin", Name: "Example", Version: "1", ProtocolVersions: []int{1}, SourceSchemaVersions: []int{1},
	}}
	return nil
}

func (f *fakeSourcePluginRuntime) Stop() {
	f.stopped = true
	f.status = sourceplugin.Status{Phase: "stopped"}
}

func (f *fakeSourcePluginRuntime) Status() sourceplugin.Status { return f.status }

func (f *fakeSourcePluginRuntime) Sources(context.Context) ([]sourceplugin.Source, error) {
	return append([]sourceplugin.Source(nil), f.sources...), nil
}

func (f *fakeSourcePluginRuntime) Candidates(_ context.Context, request sourceplugin.ResolveRequest, emit func(sourceplugin.Event) error) error {
	f.lastRequest = request
	for _, event := range f.events {
		if err := emit(event); err != nil {
			return err
		}
	}
	return nil
}

func TestSourcePluginConfigStartsAndPersistsExplicitPaths(t *testing.T) {
	env := newEnv(t)
	rec := env.do(t, http.MethodPost, "/api/source-plugin/config", `{"enabled":true,"executable":"/opt/nagare-source","root":"/srv/sources"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	config := env.store.SourcePluginConfig()
	assert.True(t, config.Enabled)
	assert.Equal(t, "/opt/nagare-source", config.Executable)
	assert.Equal(t, "/srv/sources", config.Root)
	assert.Equal(t, "test", env.plugin.started.Version)

	var view SourcePluginView
	require.NoError(t, json.Unmarshal(decode(t, rec).Data, &view))
	assert.Equal(t, "ready", view.Status.Phase)

	rec = env.do(t, http.MethodPost, "/api/source-plugin/config", `{"enabled":false}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.True(t, env.plugin.stopped)
	assert.False(t, env.store.SourcePluginConfig().Enabled)
}

func TestSourcePluginConfigRollsBackAfterStartFailure(t *testing.T) {
	env := newEnv(t)
	env.plugin.startErr = errors.New("boom")
	rec := env.do(t, http.MethodPost, "/api/source-plugin/config", `{"enabled":true,"executable":"/bad","root":"/bad"}`)
	require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	assert.False(t, env.store.SourcePluginConfig().Enabled)
	assert.True(t, env.plugin.stopped, "failed new config must restore the previous disabled runtime")
}

func TestSourcePluginCandidatesStreamsValidatedEvents(t *testing.T) {
	env := newEnv(t)
	env.plugin.status = sourceplugin.Status{Phase: "ready"}
	env.plugin.events = []sourceplugin.Event{
		{Event: "candidate", Candidate: &sourceplugin.Candidate{
			Schema: "nagare-candidate/v1", ID: "example:3", SourceID: "example-web", Tier: 1, MatchConfidence: 1,
			Match:     sourceplugin.Match{Basis: []string{"title_episode"}},
			Transport: sourceplugin.Transport{Type: "hls", URL: "https://media.example/3.m3u8"},
		}},
		{Event: "done", Queried: 1, Succeeded: 1, DurationMS: 5},
	}
	body := `{"schema":"nagare-resolve-request/v1","subject":{"ids":{"anilist":"123"},"titles":["Example"]},"episode":{"number":"3"}}`
	rec := env.do(t, http.MethodPost, "/api/source-plugin/candidates", body)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, "application/x-ndjson; charset=utf-8", rec.Header().Get("Content-Type"))
	assert.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
	lines := strings.Split(strings.TrimSpace(rec.Body.String()), "\n")
	require.Len(t, lines, 2)
	assert.Contains(t, lines[0], `"event":"candidate"`)
	assert.Contains(t, lines[1], `"event":"done"`)
	assert.Contains(t, lines[1], `"queried":1`)
}

func TestSourcePluginEndpointsRejectUnknownJSONAndUnreadyRuntime(t *testing.T) {
	env := newEnv(t)
	rec := env.do(t, http.MethodPost, "/api/source-plugin/config", `{"enabled":false,"unknown":true}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	rec = env.do(t, http.MethodPost, "/api/source-plugin/candidates", `{}`)
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
}

func TestSourcePluginPlayPassesEphemeralURLAndHeadersToPlayer(t *testing.T) {
	env := newEnv(t)
	body := `{
  "candidate": {
    "schema": "nagare-candidate/v1",
    "id": "example-web:3",
    "sourceId": "example-web",
    "tier": 1,
    "matchConfidence": 1,
    "match": {"basis": ["title_episode"], "episodeNumber": 3},
    "transport": {"type": "hls", "url": "https://media.example/3.m3u8?token=secret", "headers": {"Referer": "https://source.example/"}},
    "metadata": {"episode": 3}
  },
  "title": "Example",
  "episode": 3,
  "anilistId": 123,
  "altTitles": ["エグザンプル", "Example (TV)"]
}`
	rec := env.do(t, http.MethodPost, "/api/source-plugin/play", body)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var result player.PlayResult
	require.NoError(t, json.Unmarshal(decode(t, rec).Data, &result))
	assert.NotEmpty(t, result.FileID)
	assert.NotContains(t, result.FileID, "secret")
	assert.Equal(t, "https://media.example/3.m3u8?token=secret", env.player.lastPath)
	assert.Equal(t, "https://source.example/", env.player.lastHeaders["Referer"])
	assert.NotContains(t, env.player.lastItem.FileID, "secret")
	assert.Empty(t, env.player.lastItem.AbsPath)
	// 目录身份要原样交给播放器做弹幕匹配校验；省略时也要能播。
	assert.Equal(t, 123, env.player.lastAnilist)
	assert.Equal(t, []string{"エグザンプル", "Example (TV)"}, env.player.lastAlts)
	rec = env.do(t, http.MethodPost, "/api/source-plugin/play", strings.Replace(strings.Replace(body, `,
  "anilistId": 123`, "", 1), `,
  "altTitles": ["エグザンプル", "Example (TV)"]`, "", 1))
	assert.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	rec = env.do(t, http.MethodPost, "/api/source-plugin/play", strings.Replace(body, `"anilistId": 123`, `"anilistId": -1`, 1))
	assert.Equal(t, http.StatusBadRequest, rec.Code)

	rec = env.do(t, http.MethodPost, "/api/source-plugin/play", strings.Replace(body, `"type": "hls"`, `"type": "torrent", "magnet": "magnet:?xt=urn:btih:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"`, 1))
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// 磁力选集带集号搜索时，插件的 BT 来源要以「只要 torrent」的允许列表参与，
// 候选压成与本机规则同形的条目；在线候选与失败来源分别被忽略/记入状态。
func TestSearchWithEpisodeMergesPluginTorrentCandidates(t *testing.T) {
	env := newEnv(t)
	env.plugin.status = sourceplugin.Status{Phase: "ready"}
	env.plugin.sources = []sourceplugin.Source{{ID: "garden", Name: "Anime Garden", Kind: "bt", Tier: 3, Enabled: true}}
	seeders := 12
	env.plugin.events = []sourceplugin.Event{
		{Event: "candidate", Candidate: &sourceplugin.Candidate{
			Schema: "nagare-candidate/v1", ID: "garden:abc", SourceID: "garden", Tier: 3, MatchConfidence: 0.95,
			Match:     sourceplugin.Match{Basis: []string{"title_episode"}, SubjectTitle: "幼女战记 第二季", EpisodeNumber: 5},
			Transport: sourceplugin.Transport{Type: "torrent", InfoHash: "0123456789ABCDEF0123456789ABCDEF01234567"},
			Metadata:  sourceplugin.Metadata{Fansub: "LoliHouse", Resolution: "1080P", Episode: 5, SizeBytes: 734003200, Seeders: &seeders},
		}},
		{Event: "candidate", Candidate: &sourceplugin.Candidate{
			Schema: "nagare-candidate/v1", ID: "web:5", SourceID: "web-a", Tier: 1, MatchConfidence: 1,
			Match:     sourceplugin.Match{Basis: []string{"title_episode"}},
			Transport: sourceplugin.Transport{Type: "hls", URL: "https://media.example/5.m3u8"},
		}},
		{Event: "source_error", SourceID: "bt-dead", Category: "search_failed", Message: "boom", Retryable: true},
		{Event: "done", Queried: 3, Succeeded: 2, Failed: 1},
	}

	rec := env.do(t, http.MethodGet, "/api/search?q=%E5%B9%BC%E5%A5%B3%E6%88%98%E8%AE%B0%20%E7%AC%AC%E4%BA%8C%E5%AD%A3&episode=5&anilist=135865&year=2026&title=%E5%B9%BC%E5%A5%B3%E6%88%A6%E8%A8%98%E2%85%A1", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var view SearchView
	require.NoError(t, json.Unmarshal(decode(t, rec).Data, &view))

	assert.Equal(t, []string{"torrent"}, env.plugin.lastRequest.Preferences.Transports, "只要 BT，不能触发浏览器嗅探")
	assert.Equal(t, "135865", env.plugin.lastRequest.Subject.IDs["anilist"])
	assert.Equal(t, []string{"幼女战记 第二季", "幼女戦記Ⅱ"}, env.plugin.lastRequest.Subject.Titles)
	assert.Equal(t, "5", env.plugin.lastRequest.Episode.Number)

	require.Len(t, view.Items, 1, "在线候选不进磁力选集")
	item := view.Items[0]
	assert.Equal(t, "plugin:garden", item.Source)
	assert.Equal(t, "magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567", item.Magnet)
	assert.Equal(t, "幼女战记 第二季 - 05", item.Title)
	assert.Equal(t, "LoliHouse", item.Group)
	assert.Equal(t, "1080p", item.Resolution)
	require.NotNil(t, item.Episode)
	assert.Equal(t, 5, *item.Episode)
	assert.Equal(t, "734 MB", item.Size)
	assert.Equal(t, 12, *item.Seeders)
	assert.Equal(t, "Anime Garden", *item.Provider)

	states := map[string]rules.State{}
	for _, outcome := range view.Sources {
		states[outcome.Source] = outcome.State
	}
	assert.Equal(t, rules.StateOK, states["plugin:garden"])
	assert.Equal(t, rules.StateFailed, states["plugin:bt-dead"])

	// 只给 .torrent 地址的候选（acg.rip）也要进选集，标出 torrentUrl 供播放端下载。
	env.plugin.events = []sourceplugin.Event{
		{Event: "candidate", Candidate: &sourceplugin.Candidate{
			Schema: "nagare-candidate/v1", ID: "acg:url:abc", SourceID: "acg", Tier: 3, MatchConfidence: 0.95,
			Match:     sourceplugin.Match{Basis: []string{"title_episode"}, SubjectTitle: "幼女战记 第二季", EpisodeNumber: 5},
			Transport: sourceplugin.Transport{Type: "torrent", TorrentURL: "https://acg.rip/t/1.torrent"},
			Metadata:  sourceplugin.Metadata{Fansub: "LoliHouse", Episode: 5},
		}},
		{Event: "done", Queried: 1, Succeeded: 1},
	}
	rec = env.do(t, http.MethodGet, "/api/search?q=x&episode=5", "")
	require.Equal(t, http.StatusOK, rec.Code)
	require.NoError(t, json.Unmarshal(decode(t, rec).Data, &view))
	require.Len(t, view.Items, 1)
	assert.Empty(t, view.Items[0].Magnet)
	assert.Equal(t, "https://acg.rip/t/1.torrent", view.Items[0].TorrentURL)

	// 没有集号：不问插件。
	env.plugin.lastRequest = sourceplugin.ResolveRequest{}
	rec = env.do(t, http.MethodGet, "/api/search?q=x", "")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Empty(t, env.plugin.lastRequest.Schema)
}

// 安装包捆了插件时首次运行直接采用并启动；用户改过配置（哪怕只是关掉）后就不再自动接管。
func TestSourcePluginAdoptsBundledOnFirstRunOnly(t *testing.T) {
	env := newEnv(t)
	svc := NewSourcePluginService(env.store, env.plugin, "test")
	svc.SetBundled(sourceplugin.Bundled{Executable: "/Applications/Nagare.app/Contents/MacOS/nagare-source/nagare-source-arm64", Root: "/Applications/Nagare.app/Contents/MacOS/nagare-source/repo"}, true)
	require.NoError(t, svc.StartConfigured())
	config := env.store.SourcePluginConfig()
	assert.True(t, config.Enabled)
	assert.Equal(t, "/Applications/Nagare.app/Contents/MacOS/nagare-source/repo", config.Root)
	assert.Equal(t, config.Executable, env.plugin.started.Executable)
	view := svc.View(context.Background())
	require.NotNil(t, view.Bundled)
	assert.True(t, view.Bundled.Active)

	// 用户显式关掉：下次启动不能又给打开
	_, err := svc.Configure(boolPtr(false), nil, nil, false)
	require.NoError(t, err)
	env.plugin.started = sourceplugin.LaunchConfig{}
	require.NoError(t, svc.StartConfigured())
	assert.False(t, env.store.SourcePluginConfig().Enabled)
	assert.Empty(t, env.plugin.started.Executable)

	// 用户换成自己的路径后，「使用内置」能切回来
	_, err = svc.Configure(boolPtr(true), strPtr("/opt/mine"), strPtr("/srv/mine"), false)
	require.NoError(t, err)
	assert.False(t, svc.View(context.Background()).Bundled.Active)
	_, err = svc.Configure(nil, nil, nil, true)
	require.NoError(t, err)
	assert.True(t, svc.View(context.Background()).Bundled.Active)

	// 没捆的构建：不自动启用，useBundled 报错
	bare := NewSourcePluginService(env.store, &fakeSourcePluginRuntime{status: sourceplugin.Status{Phase: "disabled"}}, "test")
	require.NoError(t, env.store.SetSourcePluginConfig(store.SourcePluginConfig{}))
	require.NoError(t, bare.StartConfigured())
	assert.False(t, env.store.SourcePluginConfig().Enabled)
	_, err = bare.Configure(nil, nil, nil, true)
	require.Error(t, err)
}

func boolPtr(v bool) *bool    { return &v }
func strPtr(v string) *string { return &v }
