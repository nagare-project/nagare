package api

import (
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nagare-project/nagare/internal/releasetitle"
	"github.com/nagare-project/nagare/internal/rules"
	"github.com/nagare-project/nagare/internal/sourceplugin"
)

const (
	hashA = "0123456789abcdef0123456789abcdef01234567"
	hashB = "89abcdef0123456789abcdef0123456789abcdef"
	hashC = "fedcba9876543210fedcba9876543210fedcba98"
)

func btCandidate(source, infohash, title string, episode float64) *sourceplugin.Candidate {
	return &sourceplugin.Candidate{
		Schema: "nagare-candidate/v1", ID: source + ":" + infohash, SourceID: source, Tier: 3, MatchConfidence: 0.95,
		Match:     sourceplugin.Match{Basis: []string{"title_episode"}},
		Transport: sourceplugin.Transport{Type: "torrent", InfoHash: infohash},
		Metadata:  sourceplugin.Metadata{Episode: episode, Title: title},
	}
}

// pluginSearch 打一次流式端点，按事件拆成条目与来源结论。
func pluginSearch(t *testing.T, env *testEnv, query string) ([]SearchItemView, []rules.Outcome) {
	t.Helper()
	rec := env.do(t, http.MethodGet, "/api/search/plugin?"+query, "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var items []SearchItemView
	var outcomes []rules.Outcome
	lines := strings.Split(strings.TrimSpace(rec.Body.String()), "\n")
	for i, line := range lines {
		var event PluginSearchEvent
		require.NoError(t, json.Unmarshal([]byte(line), &event))
		if i == len(lines)-1 {
			assert.Equal(t, "done", event.Event, "最后一行必须是 done")
			continue
		}
		if event.Item != nil {
			items = append(items, *event.Item)
		}
		if event.Outcome != nil {
			outcomes = append(outcomes, *event.Outcome)
		}
	}
	return items, outcomes
}

// 跨季连续编号：插件一次请求只认一种编号（absolute 优先于 number），所以按季编号的「03」
// 与连续编号的「15」各问一次，number 都是作品集号；两次都回来的同一条发布只交出一次，
// 每个来源只有一个结论。
func TestSearchPluginAsksBothNumberingsAndMerges(t *testing.T) {
	env := newEnv(t)
	env.plugin.status = sourceplugin.Status{Phase: "ready"}
	batch := btCandidate("garden", hashC, "[G] 某作品 [01-24 Fin][1080p]", 0)
	env.plugin.eventsFor = func(request sourceplugin.ResolveRequest) []sourceplugin.Event {
		if request.Episode.Absolute == 15 {
			return []sourceplugin.Event{
				{Event: "candidate", Candidate: btCandidate("garden", hashB, "[ANi] 某作品 第二季 - 15 [1080P]", 15)},
				{Event: "candidate", Candidate: batch},
				{Event: "source_error", SourceID: "nyaa", Category: "search_failed", Message: "dns", Retryable: true},
				{Event: "done", Queried: 2, Succeeded: 1, Failed: 1},
			}
		}
		return []sourceplugin.Event{
			{Event: "candidate", Candidate: btCandidate("garden", hashA, "[LoliHouse] 某作品 S2 - 03 [1080p]", 3)},
			{Event: "candidate", Candidate: batch},
			{Event: "candidate", Candidate: btCandidate("nyaa", hashA, "[LoliHouse] 某作品 S2 - 03 [1080p]", 3)},
			{Event: "done", Queried: 2, Succeeded: 2},
		}
	}

	items, outcomes := pluginSearch(t, env, "q=%E6%9F%90%E4%BD%9C%E5%93%81&episode=3&absolute=15&anilist=182255")

	require.Len(t, env.plugin.requests, 2)
	absolutes := []float64{env.plugin.requests[0].Episode.Absolute, env.plugin.requests[1].Episode.Absolute}
	sort.Float64s(absolutes)
	assert.Equal(t, []float64{3, 15}, absolutes)
	for _, request := range env.plugin.requests {
		assert.Equal(t, "3", request.Episode.Number, "number 始终是作品集号")
		assert.Equal(t, "182255", request.Subject.IDs["anilist"])
	}

	got := map[string]int{}
	for _, item := range items {
		got[item.Source+" "+item.Infohash]++
	}
	assert.Equal(t, map[string]int{
		"plugin:garden " + hashA: 1, "plugin:garden " + hashB: 1, "plugin:garden " + hashC: 1, "plugin:nyaa " + hashA: 1,
	}, got, "两次都回来的合集只交出一次")

	states := map[string][]rules.State{}
	for _, outcome := range outcomes {
		states[outcome.Source] = append(states[outcome.Source], outcome.State)
	}
	assert.Equal(t, []rules.State{rules.StateOK}, states["plugin:garden"])
	assert.Equal(t, []rules.State{rules.StateOK}, states["plugin:nyaa"], "一次失败一次有结果：只报有结果，不挂一条不存在的故障")
	for _, outcome := range outcomes {
		if outcome.Source == "plugin:garden" {
			assert.Equal(t, 3, outcome.Count, "条数按去重后算")
		}
	}

	// 两种编号相同（第一季）或没给连续编号：只问一次
	env.plugin.requests = nil
	pluginSearch(t, env, "q=x&episode=3&absolute=3")
	assert.Len(t, env.plugin.requests, 1)
	env.plugin.requests = nil
	pluginSearch(t, env, "q=x&episode=3")
	assert.Len(t, env.plugin.requests, 1)
}

// 「来源里没有这一集」是零结果，不是故障：用户换着集号点，没发布的集多得是，
// 不能每次都说「源连接失败」。两次请求都没有、才算这个来源零结果。
func TestSearchPluginEpisodeNotFoundIsZeroNotFailure(t *testing.T) {
	env := newEnv(t)
	env.plugin.status = sourceplugin.Status{Phase: "ready"}
	env.plugin.events = []sourceplugin.Event{
		{Event: "source_error", SourceID: "mikan", Category: "episode_not_found", Message: "none", Retryable: true},
		{Event: "source_error", SourceID: "nyaa", Category: "search_timeout", Message: "slow", Retryable: true},
		{Event: "done", Queried: 2, Failed: 2},
	}
	_, outcomes := pluginSearch(t, env, "q=x&episode=3&absolute=15")
	byID := map[string]rules.Outcome{}
	for _, outcome := range outcomes {
		_, dup := byID[outcome.Source]
		require.False(t, dup, "每个来源只有一个结论：%s", outcome.Source)
		byID[outcome.Source] = outcome
	}
	assert.Equal(t, rules.StateZero, byID["plugin:mikan"].State)
	assert.Equal(t, "episode_not_found", byID["plugin:mikan"].Detail)
	assert.Equal(t, rules.StateFailed, byID["plugin:nyaa"].State)

	// 一次没有这一集、一次真故障：说不清有没有，按故障报
	env.plugin.eventsFor = func(request sourceplugin.ResolveRequest) []sourceplugin.Event {
		category := "episode_not_found"
		if request.Episode.Absolute == 15 {
			category = "search_failed"
		}
		return []sourceplugin.Event{
			{Event: "source_error", SourceID: "mikan", Category: category, Message: "x", Retryable: true},
			{Event: "done", Queried: 1, Failed: 1},
		}
	}
	_, outcomes = pluginSearch(t, env, "q=x&episode=3&absolute=15")
	require.Len(t, outcomes, 1)
	assert.Equal(t, rules.StateFailed, outcomes[0].State)
}

// 插件的请求格式要求标题不重复：原名与英文名写法相同（或只差大小写）时要先去重，
// 否则整次请求被插件拒掉，用户看到的是「候选流中断」。
func TestSearchPluginDeduplicatesTitles(t *testing.T) {
	env := newEnv(t)
	env.plugin.status = sourceplugin.Status{Phase: "ready"}
	env.plugin.events = []sourceplugin.Event{{Event: "done"}}
	pluginSearch(t, env, "q=Frieren&episode=3&title=frieren&title=%20Frieren%20&title=%E8%91%AC%E9%80%81&title=&title=A&title=B")
	require.Len(t, env.plugin.requests, 1)
	assert.Equal(t, []string{"Frieren", "葬送", "A", "B"}, env.plugin.requests[0].Subject.Titles, "去空白、不分大小写去重、最多四种写法")
}

func TestSearchPluginRejectsInvalidAbsolute(t *testing.T) {
	env := newEnv(t)
	env.plugin.status = sourceplugin.Status{Phase: "ready"}
	for _, query := range []string{"q=x&episode=3&absolute=0", "q=x&episode=3&absolute=abc", "q=x&episode=3&absolute=100000", "q=x&episode=100000"} {
		rec := env.do(t, http.MethodGet, "/api/search/plugin?"+query, "")
		assert.Equal(t, http.StatusBadRequest, rec.Code, query)
	}
	assert.Empty(t, env.plugin.requests)
}

// 插件给的选集线索不能丢：合集里哪个文件是这一集（fileIndex）、只给 infohash 时找人用的
// tracker（拼进磁力的 tr=）。tracker 只补给磁力，种子文件地址那条路一律不补（私有种子）。
func TestPluginTorrentItemKeepsFileIndexAndTrackers(t *testing.T) {
	fileIndex := 4
	candidate := sourceplugin.Candidate{
		Schema: "nagare-candidate/v1", ID: "garden:x", SourceID: "garden", Tier: 3, MatchConfidence: 0.9,
		Match: sourceplugin.Match{Basis: []string{"title_episode"}},
		Transport: sourceplugin.Transport{Type: "torrent", InfoHash: strings.ToUpper(hashA), FileIndex: &fileIndex,
			Trackers: []string{"udp://tracker.example:1337/announce", "file:///etc/passwd", " https://t.example/a?x=1&y=2 ", "udp://tracker.example:1337/announce"}},
		Metadata: sourceplugin.Metadata{Title: "[G] 某作品 [01-12][1080p]"},
	}
	item, ok := pluginTorrentItem(candidate, nil)
	require.True(t, ok)
	require.NotNil(t, item.FileIndex)
	assert.Equal(t, 4, *item.FileIndex)
	assert.Equal(t, hashA, item.Infohash)
	assert.Equal(t, "magnet:?xt=urn:btih:"+hashA+"&tr=udp%3A%2F%2Ftracker.example%3A1337%2Fannounce&tr=https%3A%2F%2Ft.example%2Fa%3Fx%3D1%26y%3D2", item.Magnet,
		"合法的 tracker 去重后逐个编码进 tr=，file:// 一类丢掉")
	assert.Equal(t, rules.ParseInfohash(item.Magnet), hashA, "tracker 里的 & 不能把磁力的参数搅乱")
	assert.Equal(t, "batch", item.Kind)
	require.NotNil(t, item.EpisodeRange)
	assert.Equal(t, releasetitle.Range{Low: 1, High: 12}, *item.EpisodeRange)

	// 来源自己给了磁力：原样用，不往里补
	candidate.Transport.Magnet = "magnet:?xt=urn:btih:" + hashB
	item, ok = pluginTorrentItem(candidate, nil)
	require.True(t, ok)
	assert.Equal(t, "magnet:?xt=urn:btih:"+hashB, item.Magnet)

	// 只有种子文件地址：没有磁力可补
	candidate.Transport = sourceplugin.Transport{Type: "torrent", TorrentURL: "https://acg.example/1.torrent", Trackers: []string{"udp://tracker.example:1337/announce"}}
	item, ok = pluginTorrentItem(candidate, nil)
	require.True(t, ok)
	assert.Empty(t, item.Magnet)
	assert.Equal(t, "https://acg.example/1.torrent", item.TorrentURL)
	assert.Nil(t, item.FileIndex)

	// 形状不对的 infohash 不拼磁力：没有别的定位方式时整条丢掉
	candidate.Transport = sourceplugin.Transport{Type: "torrent", InfoHash: "abc&tr=udp://evil"}
	_, ok = pluginTorrentItem(candidate, nil)
	assert.False(t, ok)
}

// 合集不能冒充单集：插件给了集号也不行（标题写着 01-12 时，那个数不是这条发布的集号）。
func TestPluginTorrentItemBatchIgnoresPluginEpisode(t *testing.T) {
	candidate := *btCandidate("garden", hashA, "[G] 某作品 第二季 全12集 [1080P]", 12)
	item, ok := pluginTorrentItem(candidate, nil)
	require.True(t, ok)
	assert.Equal(t, "batch", item.Kind)
	assert.Nil(t, item.Episode)

	candidate = *btCandidate("garden", hashA, "[G] 某作品 - 07 [1080P]", 7)
	item, ok = pluginTorrentItem(candidate, nil)
	require.True(t, ok)
	require.NotNil(t, item.Episode)
	assert.Equal(t, 7, *item.Episode)
}
