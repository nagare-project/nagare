// Match：请求形状（episodes 大坑）、三个 phase 的 anime 多态、miss 与形状错误。
package animego_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nagare-player/nagare/internal/animego"
)

// matchServer 起一个对 /api/dandanplay/match 固定应答的客户端。
func matchServer(t *testing.T, rec *recorder, status int, body string) *animego.Client {
	t.Helper()
	return newTestClient(t, rec, func(w http.ResponseWriter, r *http.Request) {
		respond(w, status, body)
	})
}

func TestMatchRequestShape(t *testing.T) {
	rec := &recorder{}
	c := matchServer(t, rec, http.StatusOK, `{"matched":false}`)

	_, err := c.Match(context.Background(), animego.MatchInput{
		FileName: "[Sub] Frieren - 07 [1080p].mkv",
		FileHash: "ABCDEF0123456789ABCDEF0123456789", // 故意大写，应被归一化
		FileSize: 123456789,
		Episode:  7,
		Keyword:  "芙莉莲",
	})
	require.NoError(t, err)

	reqs := rec.all()
	require.Len(t, reqs, 1)
	assert.Equal(t, http.MethodPost, reqs[0].Method)
	assert.Equal(t, "/api/dandanplay/match", reqs[0].Path)
	assert.Equal(t, "application/json", reqs[0].CT)
	assert.Empty(t, reqs[0].Auth, "match 是公开端点，不该带鉴权头")

	var body map[string]any
	require.NoError(t, json.Unmarshal(reqs[0].Body, &body))
	assert.Equal(t, "[Sub] Frieren - 07 [1080p].mkv", body["fileName"])
	assert.Equal(t, "abcdef0123456789abcdef0123456789", body["fileHash"], "hash 必须小写")
	assert.Equal(t, float64(123456789), body["fileSize"])
	assert.Equal(t, "芙莉莲", body["keyword"])
	// 大坑：episodes 必须非空且 files[].episode 对应，否则 hash 命中也 matched:false。
	assert.Equal(t, []any{float64(7)}, body["episodes"])
	files, ok := body["files"].([]any)
	require.True(t, ok, "files 必须是数组")
	require.Len(t, files, 1)
	f := files[0].(map[string]any)
	assert.Equal(t, float64(7), f["episode"])
	assert.Equal(t, "[Sub] Frieren - 07 [1080p].mkv", f["fileName"])
	assert.Equal(t, "abcdef0123456789abcdef0123456789", f["fileHash"])
	assert.Equal(t, float64(123456789), f["fileSize"])
}

func TestMatchKeywordOmittedWhenEmpty(t *testing.T) {
	rec := &recorder{}
	c := matchServer(t, rec, http.StatusOK, `{"matched":false}`)

	_, err := c.Match(context.Background(), animego.MatchInput{FileName: "a.mkv", Episode: 1})
	require.NoError(t, err)

	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.all()[0].Body, &body))
	_, hasKeyword := body["keyword"]
	assert.False(t, hasKeyword, "空 keyword 不该出现在请求里")
}

func TestMatchPhase1(t *testing.T) {
	// phase1：anime 只有 {titleNative,coverImageUrl}，siteAnime 为 null。
	// 匹配到了但没有 anilistId —— 调用方要能接受这种局面（进度同步不可用）。
	c := matchServer(t, nil, http.StatusOK, `{
		"matched":true,"source":"dandanplay",
		"anime":{"titleNative":"葬送のフリーレン","coverImageUrl":"https://img/f.jpg"},
		"siteAnime":null,
		"episodeMap":{"7":{"dandanEpisodeId":184300007,"title":"第7话 おとぎ話みたいな"}}}`)

	got, err := c.Match(context.Background(), animego.MatchInput{FileName: "f07.mkv", Episode: 7})
	require.NoError(t, err)
	assert.Equal(t, animego.MatchResult{
		Matched:       true,
		Source:        "dandanplay",
		TitleNative:   "葬送のフリーレン",
		CoverImageURL: "https://img/f.jpg",
		EpisodeMap: map[int]animego.EpisodeRef{
			7: {DandanEpisodeID: 184300007, Title: "第7话 おとぎ話みたいな"},
		},
	}, got)
}

func TestMatchPhase2SiteAnimeNullFallsBackToAnime(t *testing.T) {
	// phase2：siteAnime 为 null，完整元数据（含 anilistId）在 anime 里 —— 必须捞得到。
	c := matchServer(t, nil, http.StatusOK, `{
		"matched":true,"source":"dandanplay",
		"anime":{"anilistId":154587,"titleChinese":"葬送的芙莉莲",
		         "titleNative":"葬送のフリーレン","titleRomaji":"Sousou no Frieren",
		         "coverImageUrl":"https://img/f2.jpg","episodes":28},
		"siteAnime":null,
		"episodeMap":{"7":{"dandanEpisodeId":184300007,"title":"第7话"}}}`)

	got, err := c.Match(context.Background(), animego.MatchInput{FileName: "f07.mkv", Episode: 7})
	require.NoError(t, err)
	assert.Equal(t, 154587, got.AnilistID, "siteAnime 为 null 时 anilistId 从 anime 里捞")
	assert.Equal(t, "葬送的芙莉莲", got.TitleChinese)
	assert.Equal(t, "Sousou no Frieren", got.TitleRomaji)
	assert.Equal(t, 28, got.TotalEpisodes)
}

func TestMatchPhase3SiteAnimePreferred(t *testing.T) {
	// phase3：anime 是空对象 {}，数据全在 siteAnime；siteAnime 优先于 anime。
	c := matchServer(t, nil, http.StatusOK, `{
		"matched":true,"source":"animeCache",
		"anime":{},
		"siteAnime":{"anilistId":154587,"titleChinese":"葬送的芙莉莲",
		             "titleNative":"葬送のフリーレン","titleRomaji":"Sousou no Frieren",
		             "coverImageUrl":"https://img/site.jpg","episodes":28},
		"episodeMap":{"7":{"dandanEpisodeId":184300007,"title":"第7话"},
		              "8":{"dandanEpisodeId":184300008,"title":"第8话"}}}`)

	got, err := c.Match(context.Background(), animego.MatchInput{FileName: "f.mkv", Episode: 7})
	require.NoError(t, err)
	assert.Equal(t, "animeCache", got.Source)
	assert.Equal(t, 154587, got.AnilistID)
	assert.Equal(t, "https://img/site.jpg", got.CoverImageURL)
	// episodeMap 的字符串键转回 int，多键齐全。
	assert.Equal(t, map[int]animego.EpisodeRef{
		7: {DandanEpisodeID: 184300007, Title: "第7话"},
		8: {DandanEpisodeID: 184300008, Title: "第8话"},
	}, got.EpisodeMap)
}

func TestMatchCompleteMiss(t *testing.T) {
	// 完全 miss：{"matched":false} 且其余字段缺席 —— 零值结果、nil 错误。
	c := matchServer(t, nil, http.StatusOK, `{"matched":false}`)
	got, err := c.Match(context.Background(), animego.MatchInput{FileName: "unknown.mkv", Episode: 1})
	require.NoError(t, err)
	assert.Equal(t, animego.MatchResult{}, got)
}

func TestMatchMalformedRequestGets200Miss(t *testing.T) {
	// 服务端对它眼中格式错误的请求也答 200 的 {"matched":false}（不是 4xx）。
	// 客户端不做集号本地校验（0 也照发），这条路径必须走「miss」而非报错。
	c := matchServer(t, nil, http.StatusOK, `{"matched":false}`)
	got, err := c.Match(context.Background(), animego.MatchInput{FileName: "weird.mkv", Episode: 0})
	require.NoError(t, err)
	assert.False(t, got.Matched)
}

func TestMatchMatchedButNoEpisodeMap(t *testing.T) {
	c := matchServer(t, nil, http.StatusOK, `{"matched":true,"source":"dandanplay","anime":{"titleNative":"x"}}`)
	_, err := c.Match(context.Background(), animego.MatchInput{FileName: "f.mkv", Episode: 1})
	ae := assertKind(t, err, animego.ErrDecode)
	assert.Contains(t, ae.Error(), "episodeMap", "错误要说清缺了什么")
}

func TestMatchBadEpisodeMapShape(t *testing.T) {
	// 键不是集号 → ErrDecode（静默丢弃会变成「为什么这集没弹幕」的谜案）。
	c := matchServer(t, nil, http.StatusOK,
		`{"matched":true,"episodeMap":{"movie":{"dandanEpisodeId":1,"title":"t"}}}`)
	_, err := c.Match(context.Background(), animego.MatchInput{FileName: "f.mkv", Episode: 1})
	assertKind(t, err, animego.ErrDecode)

	// 条目缺 dandanEpisodeId → 同样是形状错误。
	c2 := matchServer(t, nil, http.StatusOK,
		`{"matched":true,"episodeMap":{"7":{"title":"第7话"}}}`)
	_, err = c2.Match(context.Background(), animego.MatchInput{FileName: "f.mkv", Episode: 7})
	ae := assertKind(t, err, animego.ErrDecode)
	assert.Contains(t, ae.Error(), "dandanEpisodeId")
}

func TestMatchServerTimeout(t *testing.T) {
	// 服务端 20s 超时给 500 {"error":"match timed out"} → 暂时不可达，可稍后再试。
	c := matchServer(t, nil, http.StatusInternalServerError, `{"error":"match timed out"}`)
	_, err := c.Match(context.Background(), animego.MatchInput{FileName: "f.mkv", Episode: 1})
	ae := assertKind(t, err, animego.ErrUnavailable)
	assert.Contains(t, ae.Error(), "match timed out")
}

func TestMatchEmptyFileName(t *testing.T) {
	rec := &recorder{}
	c := matchServer(t, rec, http.StatusOK, `{"matched":false}`)
	_, err := c.Match(context.Background(), animego.MatchInput{Episode: 1})
	assertKind(t, err, animego.ErrBadRequest)
	assert.Empty(t, rec.all(), "本地即拒，不发请求")
}
