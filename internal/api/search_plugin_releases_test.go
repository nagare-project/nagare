package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nagare-project/nagare/internal/rules"
	"github.com/nagare-project/nagare/internal/sourceplugin"
)

func releasesReady() sourceplugin.Status {
	return sourceplugin.Status{Phase: "ready", Manifest: &sourceplugin.Manifest{
		ID: "org.example", Name: "Example", Version: "1", ProtocolVersions: []int{1}, SourceSchemaVersions: []int{1},
		Capabilities: []string{"bt", sourceplugin.CapabilityReleases},
	}}
}

func btRelease(source, infohash, title string) *sourceplugin.Release {
	return &sourceplugin.Release{ID: source + ":" + infohash, SourceID: source, Title: title,
		Transport: sourceplugin.ReleaseTransport{Type: "torrent", InfoHash: infohash}}
}

// pluginEvents 打一次流式端点，返回全部事件（含首行 scope 与末行 done）。
func pluginEvents(t *testing.T, env *testEnv, query string) []PluginSearchEvent {
	t.Helper()
	rec := env.do(t, http.MethodGet, "/api/search/plugin?"+query, "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var events []PluginSearchEvent
	for _, line := range strings.Split(strings.TrimSpace(rec.Body.String()), "\n") {
		var event PluginSearchEvent
		require.NoError(t, json.Unmarshal([]byte(line), &event))
		events = append(events, event)
	}
	return events
}

// 插件能按作品搜时：一次搜整部作品（不带集号），首行告诉界面结果覆盖全部集；
// 插件认不出集号的发布（「[01v2]」）也照样交出，由本机解析链认集号。
func TestSearchPluginUsesReleaseSearchWhenSupported(t *testing.T) {
	env := newEnv(t)
	env.plugin.status = releasesReady()
	seeders := 7
	withSeeders := btRelease("garden", hashB, "[ANi] 某作品 - 02 [1080P]")
	withSeeders.Seeders = &seeders
	env.plugin.releaseEvents = []sourceplugin.ReleaseEvent{
		{Event: "release", Release: btRelease("garden", hashA, "[绿茶字幕组] 某作品 [01v2][1080p]")},
		{Event: "release", Release: withSeeders},
		{Event: "release", Release: btRelease("garden", hashA, "[绿茶字幕组] 某作品 [01v2][1080p]")},
		{Event: "release", Release: btRelease("nyaa", hashA, "[绿茶字幕组] 某作品 [01v2][1080p]")},
		{Event: "source_result", Result: &sourceplugin.SourceResult{SourceID: "garden", State: "ok", Count: 2, Partial: true, DurationMS: 800}},
		{Event: "source_result", Result: &sourceplugin.SourceResult{SourceID: "nyaa", State: "ok", Count: 1, DurationMS: 900}},
		{Event: "source_result", Result: &sourceplugin.SourceResult{SourceID: "dmhy", State: "zero", DurationMS: 300}},
		{Event: "source_result", Result: &sourceplugin.SourceResult{SourceID: "acg", State: "failed", Category: "search_timeout", Message: "timed out", Retryable: true, DurationMS: 8000}},
		{Event: "done"},
	}

	events := pluginEvents(t, env, "q=某作品&title=Kore&title=%E6%9F%90%E4%BD%9C%E5%93%81&anilist=42")

	require.NotEmpty(t, events)
	assert.Equal(t, PluginSearchEvent{Event: "scope", Scope: pluginScopeAll}, events[0])
	assert.Equal(t, "done", events[len(events)-1].Event)
	require.Len(t, env.plugin.releaseRequests, 1, "整部作品只搜一次")
	request := env.plugin.releaseRequests[0]
	assert.Equal(t, sourceplugin.ReleaseSearchSchema, request.Schema)
	assert.Equal(t, []string{"某作品", "Kore"}, request.Subject.Titles, "标题去重后原样交给插件")
	assert.Equal(t, map[string]string{"anilist": "42"}, request.Subject.IDs)
	assert.Empty(t, env.plugin.requests, "不再按集问候选")

	var items []SearchItemView
	outcomes := map[string]rules.Outcome{}
	for _, event := range events {
		if event.Item != nil {
			items = append(items, *event.Item)
		}
		if event.Outcome != nil {
			outcomes[event.Outcome.Source] = *event.Outcome
		}
	}
	require.Len(t, items, 3, "同一来源的重复发布只交一次；跨来源的同一种子留给界面按优先级折叠")
	assert.Equal(t, "plugin:garden", items[0].Source)
	require.NotNil(t, items[0].Episode, "[01v2] 由本机解析链认出集号")
	assert.Equal(t, 1, *items[0].Episode)
	require.NotNil(t, items[1].Seeders)
	assert.Equal(t, 7, *items[1].Seeders)
	assert.Equal(t, rules.StateOK, outcomes["plugin:garden"].State)
	assert.NotEmpty(t, outcomes["plugin:garden"].Detail, "部分请求失败要留痕")
	assert.Equal(t, rules.StateZero, outcomes["plugin:dmhy"].State)
	assert.Equal(t, rules.StateFailed, outcomes["plugin:acg"].State)
	assert.Equal(t, "search_timeout", outcomes["plugin:acg"].Detail)
}

// 不合规、拼不出定位的发布跳过并记在来源状态里；插件说 ok 却一条都用不了的源算失败；
// 交了发布却没报结论的源补一条结论；总集篇（12.5）不冒充第 13 集。
func TestSearchPluginReleaseStreamAccountsForSkips(t *testing.T) {
	env := newEnv(t)
	env.plugin.status = releasesReady()
	recap := btRelease("garden", hashB, "[G] 某作品 - 12.5 [1080p]")
	recap.Episode = "12.5"
	env.plugin.releaseEvents = []sourceplugin.ReleaseEvent{
		{Event: "release", Release: btRelease("garden", hashA, "[G] 某作品 - 01 [1080p]")},
		{Event: "release", Release: recap},
		{Event: "invalid", Skipped: "garden"},
		{Event: "source_result", Result: &sourceplugin.SourceResult{SourceID: "garden", State: "ok", Count: 3, DurationMS: 10}},
		{Event: "release", Release: btRelease("mikan", "not-a-hash", "[G] 某作品 - 02")},
		{Event: "source_result", Result: &sourceplugin.SourceResult{SourceID: "mikan", State: "ok", Count: 1, DurationMS: 10}},
		{Event: "release", Release: btRelease("dmhy", hashC, "[G] 某作品 - 03")},
		{Event: "done"},
	}

	events := pluginEvents(t, env, "q=某作品")

	var items []SearchItemView
	outcomes := map[string]rules.Outcome{}
	for _, event := range events {
		if event.Item != nil {
			items = append(items, *event.Item)
		}
		if event.Outcome != nil {
			outcomes[event.Outcome.Source] = *event.Outcome
		}
	}
	require.Len(t, items, 3)
	assert.Nil(t, items[1].Episode, "12.5 不是第 12 集也不是第 13 集")
	garden := outcomes["plugin:garden"]
	assert.Equal(t, rules.StateOK, garden.State)
	assert.Equal(t, 2, garden.Count, "条数按实际交出的算")
	assert.Equal(t, 3, garden.RawCount)
	assert.Contains(t, garden.Detail, "1 条发布")
	assert.Equal(t, rules.StateFailed, outcomes["plugin:mikan"].State, "说有结果却一条都用不了")
	assert.Equal(t, rules.StateOK, outcomes["plugin:dmhy"].State, "没报结论的来源补一条")
	assert.Equal(t, 1, outcomes["plugin:dmhy"].Count)
}

// 插件的发布流半路断了：已交出的照常展示，并说清楚结果可能不全。
func TestSearchPluginReportsBrokenReleaseStream(t *testing.T) {
	env := newEnv(t)
	env.plugin.status = releasesReady()
	env.plugin.releaseEvents = []sourceplugin.ReleaseEvent{{Event: "release", Release: btRelease("garden", hashA, "[G] 某作品 - 01")}}
	env.plugin.releaseErr = assert.AnError

	events := pluginEvents(t, env, "q=某作品")

	var failed []rules.Outcome
	for _, event := range events {
		if event.Outcome != nil && event.Outcome.State == rules.StateFailed {
			failed = append(failed, *event.Outcome)
		}
	}
	require.Len(t, failed, 1)
	assert.Equal(t, "plugin:bt", failed[0].Source)
	assert.Contains(t, failed[0].Reason, "结果可能不全")
	assert.NotNil(t, events[1].Item, "断之前交出的发布照常展示")
	assert.Equal(t, "done", events[len(events)-1].Event)
}

// 老插件（没有 bt_releases）：照旧按集问，首行说明结果只含这一集；不给集号是 400。
func TestSearchPluginFallsBackToEpisodeSearch(t *testing.T) {
	env := newEnv(t)
	env.plugin.status = sourceplugin.Status{Phase: "ready"}
	env.plugin.events = []sourceplugin.Event{{Event: "done", Queried: 0}}

	rec := env.do(t, http.MethodGet, "/api/search/plugin?q=某作品", "")
	assert.Equal(t, http.StatusBadRequest, rec.Code)

	events := pluginEvents(t, env, "q=某作品&episode=3&anilist=42")
	assert.Equal(t, PluginSearchEvent{Event: "scope", Scope: pluginScopeEpisode}, events[0])
	assert.Empty(t, env.plugin.releaseRequests)
	require.Len(t, env.plugin.requests, 1)
	assert.Equal(t, "3", env.plugin.requests[0].Episode.Number)
}
