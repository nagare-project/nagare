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
	"github.com/nagare-project/nagare/internal/sourceplugin"
)

type fakeSourcePluginRuntime struct {
	status   sourceplugin.Status
	started  sourceplugin.LaunchConfig
	startErr error
	stopped  bool
	sources  []sourceplugin.Source
	events   []sourceplugin.Event
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

func (f *fakeSourcePluginRuntime) Candidates(_ context.Context, _ sourceplugin.ResolveRequest, emit func(sourceplugin.Event) error) error {
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
  "episode": 3
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

	rec = env.do(t, http.MethodPost, "/api/source-plugin/play", strings.Replace(body, `"type": "hls"`, `"type": "torrent", "magnet": "magnet:?xt=urn:btih:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"`, 1))
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}
