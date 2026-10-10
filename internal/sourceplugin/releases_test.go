package sourceplugin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// releaseServer 起一个只回放固定 NDJSON 行的 /v1/releases，并记下收到的请求。
func releaseServer(t *testing.T, lines ...string) (*Client, *ReleaseSearchRequest) {
	t.Helper()
	var got ReleaseSearchRequest
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/releases", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "1", r.Header.Get("X-Nagare-Protocol-Version"))
		// 在处理器的 goroutine 里不能 FailNow
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&got))
		w.Header().Set("Content-Type", "application/x-ndjson; charset=utf-8")
		for _, line := range lines {
			_, _ = fmt.Fprintln(w, line)
		}
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	client, err := NewClient(server.URL)
	require.NoError(t, err)
	return client, &got
}

func searchRequest(titles ...string) ReleaseSearchRequest {
	return ReleaseSearchRequest{Schema: ReleaseSearchSchema, Subject: ReleaseSubject{Titles: titles}}
}

func TestClientConsumesReleaseStream(t *testing.T) {
	client, got := releaseServer(t,
		`{"event":"release","release":{"id":"garden:0123456789abcdef0123456789abcdef01234567","sourceId":"garden","title":"[ANi] Show - 01 [1080P]","transport":{"type":"torrent","magnet":"magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567","infoHash":"0123456789abcdef0123456789abcdef01234567"},"fansub":"ANi","sizeBytes":1048576000,"seeders":12,"publishedAt":"2026-07-04T12:00:00Z","episode":"1"}}`,
		`{"event":"source_result","sourceId":"garden","state":"ok","count":1,"partial":false,"cached":false,"durationMs":812}`,
		`{"event":"source_result","sourceId":"nyaa","state":"failed","count":0,"category":"search_failed","message":"source request failed","retryable":true,"durationMs":1033}`,
		`{"event":"done","queried":2,"succeeded":1,"failed":1,"durationMs":1100}`,
	)
	var events []ReleaseEvent
	err := client.Releases(context.Background(), searchRequest("Show", "ショー"), func(event ReleaseEvent) error {
		events = append(events, event)
		return nil
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"Show", "ショー"}, got.Subject.Titles)
	assert.Nil(t, got.Subject.IDs, "没有作品 ID 时不发 ids（插件要求给了就不能为空）")
	require.Len(t, events, 4)
	assert.Equal(t, "ANi", events[0].Release.Fansub)
	assert.Equal(t, 12, *events[0].Release.Seeders)
	assert.Equal(t, SourceResult{SourceID: "garden", State: "ok", Count: 1, DurationMS: 812}, *events[1].Result)
	assert.Equal(t, "search_failed", events[2].Result.Category)
	assert.Equal(t, "done", events[3].Event)
}

// 单条发布不合规只跳过（交出 invalid 事件，带上能认出的来源）；结构性错误才中断整个流。
func TestClientSkipsInvalidReleases(t *testing.T) {
	const hash = `"infoHash":"0123456789abcdef0123456789abcdef01234567"`
	release := func(extra string) string {
		return `{"event":"release","release":{"id":"garden:1","sourceId":"garden","title":"[G] Show - 01","transport":{"type":"torrent",` + hash + `}` + extra + `}}`
	}
	cases := map[string]string{
		"不是 torrent":     `{"event":"release","release":{"id":"g:1","sourceId":"garden","title":"x","transport":{"type":"hls"}}}`,
		"没有定位":           `{"event":"release","release":{"id":"g:1","sourceId":"garden","title":"x","transport":{"type":"torrent"}}}`,
		"空标题":            `{"event":"release","release":{"id":"g:1","sourceId":"garden","title":" ","transport":{"type":"torrent",` + hash + `}}}`,
		"transport 多余字段": `{"event":"release","release":{"id":"g:1","sourceId":"garden","title":"x","transport":{"type":"torrent",` + hash + `,"trackers":["udp://t"]}}}`,
		"集号 0":           release(`,"episode":"0"`),
		"集号 Inf":         release(`,"episode":"Inf"`),
		"集号 NaN":         release(`,"episode":"NaN"`),
		"集号 1e300":       release(`,"episode":"1e300"`),
		"集号 0x1p4":       release(`,"episode":"0x1p4"`),
		"集号超出上限":         release(`,"episode":"10000"`),
		"种子地址带账号":        `{"event":"release","release":{"id":"g:1","sourceId":"garden","title":"x","transport":{"type":"torrent","torrentUrl":"https://u:p@host.example/x.torrent"}}}`,
	}
	for name, line := range cases {
		t.Run(name, func(t *testing.T) {
			client, _ := releaseServer(t, line, `{"event":"done","queried":1,"succeeded":1,"failed":0,"durationMs":1}`)
			var events []ReleaseEvent
			require.NoError(t, client.Releases(context.Background(), searchRequest("Show"), func(event ReleaseEvent) error {
				events = append(events, event)
				return nil
			}))
			require.Len(t, events, 2)
			assert.Equal(t, ReleaseEvent{Event: "invalid", Skipped: "garden"}, events[0])
		})
	}

	client, _ := releaseServer(t, release(`,"episode":"12.5"`), `{"event":"done","queried":1,"succeeded":1,"failed":0,"durationMs":1}`)
	var got []ReleaseEvent
	require.NoError(t, client.Releases(context.Background(), searchRequest("Show"), func(event ReleaseEvent) error { got = append(got, event); return nil }))
	require.NotNil(t, got[0].Release, "总集篇 12.5 是合法集号")
}

func TestClientRejectsMalformedReleaseStreams(t *testing.T) {
	cases := map[string][]string{
		"不是 JSON":   {`not json`},
		"未知事件":      {`{"event":"surprise"}`},
		"未知状态":      {`{"event":"source_result","sourceId":"garden","state":"maybe","count":0,"durationMs":1}`},
		"失败没类别":     {`{"event":"source_result","sourceId":"garden","state":"failed","count":0,"durationMs":1}`},
		"ok 却没有条数":  {`{"event":"source_result","sourceId":"garden","state":"ok","count":0,"durationMs":1}`},
		"zero 却有条数": {`{"event":"source_result","sourceId":"garden","state":"zero","count":2,"durationMs":1}`},
		"条数为负":      {`{"event":"source_result","sourceId":"garden","state":"zero","count":-1,"durationMs":1}`},
		"多余字段":      {`{"event":"source_result","sourceId":"garden","state":"ok","count":1,"durationMs":1,"extra":true}`},
		"没有 done":   {`{"event":"source_result","sourceId":"garden","state":"zero","count":0,"durationMs":1}`},
		"done 之后还有": {`{"event":"done","queried":0,"succeeded":0,"failed":0,"durationMs":1}`, `{"event":"done","queried":0,"succeeded":0,"failed":0,"durationMs":1}`},
	}
	for name, lines := range cases {
		t.Run(name, func(t *testing.T) {
			client, _ := releaseServer(t, lines...)
			err := client.Releases(context.Background(), searchRequest("Show"), func(ReleaseEvent) error { return nil })
			require.Error(t, err)
		})
	}
}

func TestManagerReleasesRequiresReadyPlugin(t *testing.T) {
	err := (&Manager{}).Releases(context.Background(), searchRequest("Show"), func(ReleaseEvent) error { return nil })
	require.Error(t, err)
}

func TestClientRefusesIncompleteReleaseRequests(t *testing.T) {
	client, err := NewClient("http://127.0.0.1:9")
	require.NoError(t, err)
	noop := func(ReleaseEvent) error { return nil }
	require.Error(t, client.Releases(context.Background(), ReleaseSearchRequest{Schema: "x", Subject: ReleaseSubject{Titles: []string{"a"}}}, noop))
	require.Error(t, client.Releases(context.Background(), searchRequest(), noop))
	require.Error(t, client.Releases(context.Background(), searchRequest("a", "b", "c", "d", "e"), noop))
}

func TestManifestSupports(t *testing.T) {
	manifest := Manifest{Capabilities: []string{"bt", CapabilityReleases}}
	assert.True(t, manifest.Supports(CapabilityReleases))
	assert.False(t, Manifest{Capabilities: []string{"bt"}}.Supports(CapabilityReleases))
}
