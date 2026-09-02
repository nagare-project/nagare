package rules

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const rssRule = `
schema: 1
id: rsstest
name: RSS test
request: { url: "{{base}}/rss?term={{query}}", timeout_seconds: 2 }
format: xml
namespaces: { nyaa: "https://nyaa.si/xmlns/nyaa", mikan: "https://mikanani.me/0.1/" }
items: rss/channel/item
fields:
  title: title
  magnet:
    any:
      - { path: "enclosure@url" }
      - { path: link }
  size: { path: "enclosure@length", transforms: [format_bytes] }
  date: pubDate
  fansub: { path: "$title", transforms: [parse_fansub] }
selftest: { query: "frieren" }
`

func mustRule(t *testing.T, src, base string) *Rule {
	t.Helper()
	r, err := Parse([]byte(strings.ReplaceAll(src, "{{base}}", base)))
	require.NoError(t, err)
	return r
}

func rss(items string) []byte {
	return []byte(`<?xml version="1.0"?><rss version="2.0"><channel><title>f</title>` + items + `</channel></rss>`)
}

// 健康分类：ok / zero / dead / failed 四态各自可辨（CQ3 的核心）。
func TestEvaluateStates(t *testing.T) {
	r := mustRule(t, rssRule, "http://x")

	ok := Evaluate(r, rss(`<item><title>[G] A - 01</title><enclosure url="magnet:?xt=urn:btih:aa" length="1500000000"/><pubDate>Sat, 01 Jan 2026 00:00:00 +0800</pubDate></item>`))
	assert.Equal(t, StateOK, ok.State)
	require.Len(t, ok.Items, 1)
	assert.Equal(t, "1.5 GB", ok.Items[0].Size)
	assert.Equal(t, "G", *ok.Items[0].Fansub)
	assert.Equal(t, "rsstest", ok.Items[0].Source)
	assert.Nil(t, ok.Items[0].Seeders)

	zero := Evaluate(r, rss(``))
	assert.Equal(t, StateZero, zero.State)
	assert.Equal(t, 0, zero.RawCount)

	// 上游有条目但规则解不出 magnet → 规则失效，而不是"无结果"。
	dead := Evaluate(r, rss(`<item><title>A</title><link>https://x/no-magnet</link></item><item><title></title></item>`))
	assert.Equal(t, StateDead, dead.State)
	assert.Equal(t, 2, dead.RawCount)
	assert.Contains(t, dead.Detail, "1 条标题为空，1 条 magnet 无效")

	failed := Evaluate(r, []byte("<not-xml"))
	assert.Equal(t, StateFailed, failed.State)
	assert.NotEmpty(t, failed.Detail)
}

// any 候选：enclosure 缺失回落 link；字段全空时报 FieldGaps。
func TestEvaluateFallbackAndGaps(t *testing.T) {
	r := mustRule(t, rssRule, "http://x")
	out := Evaluate(r, rss(`<item><title>A</title><link>magnet:?xt=urn:btih:bb</link></item>`))
	require.Equal(t, StateOK, out.State)
	assert.Equal(t, "magnet:?xt=urn:btih:bb", out.Items[0].Magnet)
	assert.ElementsMatch(t, []string{"size", "date", "fansub"}, out.FieldGaps)
}

const jsonRule = `
schema: 1
id: jsontest
name: JSON test
request: { url: "{{base}}/api?q={{query}}" }
format: json
items: $
fields:
  title: title
  magnet: magnet_uri
  size: { path: total_size, transforms: [format_bytes] }
  date: { path: timestamp, transforms: [unix_rfc3339] }
  seeders: seeders
  infohash: { path: info_hash, transforms: [trim, lower] }
capabilities: { seeders: true, priority: 10 }
`

// JSON 源：做种数三态、时间戳、infohash 归一。
func TestEvaluateJSON(t *testing.T) {
	r := mustRule(t, jsonRule, "http://x")
	body := []byte(`[
	 {"title":"A","magnet_uri":"magnet:?xt=urn:btih:aa","total_size":1500000000,"timestamp":1735689600,"seeders":42,"info_hash":" AAAA1111 "},
	 {"title":"B","magnet_uri":"magnet:?xt=urn:btih:bb","total_size":0,"timestamp":0,"seeders":0},
	 {"title":"C","magnet_uri":"magnet:?xt=urn:btih:cc","seeders":null}
	]`)
	out := Evaluate(r, body)
	require.Equal(t, StateOK, out.State)
	require.Len(t, out.Items, 3)
	a, b, c := out.Items[0], out.Items[1], out.Items[2]
	assert.Equal(t, "1.5 GB", a.Size)
	assert.Equal(t, "2025-01-01T00:00:00Z", *a.Date)
	assert.Equal(t, 42, *a.Seeders)
	assert.Equal(t, "aaaa1111", a.Infohash)
	assert.Equal(t, "", b.Size)
	assert.Nil(t, b.Date, "timestamp 0 → null")
	require.NotNil(t, b.Seeders)
	assert.Equal(t, 0, *b.Seeders, "0 是已知为零")
	assert.Nil(t, c.Seeders, "null 是未知")
}

// magnet 转换：从 infohash + $title 拼装，带 tracker。
func TestEvaluateMagnetSynthesis(t *testing.T) {
	src := `
schema: 1
id: mk
name: mk
request: { url: "http://x?q={{query}}" }
format: xml
items: rss/channel/item
fields:
  title: title
  magnet:
    any:
      - { path: "enclosure@url", transforms: [{regex: "[0-9a-fA-F]{40}"}] }
      - { path: link, transforms: [{regex: "[0-9a-fA-F]{40}"}] }
    transforms: [{magnet: {dn: $title, trackers: [http://t/announce]}}]
`
	r, err := Parse([]byte(src))
	require.NoError(t, err)
	hash := strings.Repeat("ab", 20)
	out := Evaluate(r, rss(`<item><title>T</title><enclosure url="https://x/no-hash.torrent"/><link>https://x/`+hash+`</link></item>`))
	require.Equal(t, StateOK, out.State)
	assert.Equal(t, "magnet:?xt=urn:btih:"+hash+"&dn=T&tr=http%3A%2F%2Ft%2Fannounce", out.Items[0].Magnet)
}

// Fetcher：UA、查询编码、非 2xx、超时都落进 Outcome 而非 error。
func TestFetcherRun(t *testing.T) {
	var gotUA, gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA, gotQuery = r.Header.Get("User-Agent"), r.URL.Query().Get("term")
		switch r.URL.Query().Get("mode") {
		case "500":
			w.WriteHeader(500)
		case "slow":
			time.Sleep(3 * time.Second)
		default:
			_, _ = w.Write(rss(`<item><title>A</title><enclosure url="magnet:?xt=urn:btih:aa"/></item>`))
		}
	}))
	defer srv.Close()
	f := &Fetcher{Client: srv.Client(), UserAgent: "nagare/test"}

	r := mustRule(t, rssRule, srv.URL)
	out := f.Run(context.Background(), r, "葬送的芙莉莲 & co")
	assert.Equal(t, StateOK, out.State)
	assert.Equal(t, "nagare/test", gotUA)
	assert.Equal(t, "葬送的芙莉莲 & co", gotQuery, "关键词应正确编码往返")

	r500 := mustRule(t, strings.Replace(rssRule, "term={{query}}", "mode=500&term={{query}}", 1), srv.URL)
	assert.Equal(t, StateFailed, f.Run(context.Background(), r500, "x").State)
	assert.Contains(t, f.Run(context.Background(), r500, "x").Reason, "HTTP 500")

	rSlow := mustRule(t, strings.Replace(rssRule, "term={{query}}", "mode=slow&term={{query}}", 1), srv.URL)
	slow := f.Run(context.Background(), rSlow, "x")
	assert.Equal(t, StateFailed, slow.State)
	assert.Contains(t, slow.Reason, "超时")
}
