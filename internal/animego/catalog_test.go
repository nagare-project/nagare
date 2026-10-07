package animego_test

import (
	"context"
	"github.com/nagare-project/nagare/internal/animego"
	"github.com/stretchr/testify/require"
	"net/http"
	"testing"
)

func TestCatalogPublicRoutesAndDecode(t *testing.T) {
	rec := &recorder{}
	client := newTestClient(t, rec, func(w http.ResponseWriter, r *http.Request) {
		require.Empty(t, r.Header.Get("Authorization"))
		switch r.URL.Path {
		case "/api/anime/seasonal":
			require.Equal(t, "WINTER", r.URL.Query().Get("season"))
			require.Equal(t, "2027", r.URL.Query().Get("year"))
			require.Equal(t, "200", r.URL.Query().Get("perPage"))
			respond(w, 200, `{"data":[{"anilistId":1,"titleChinese":"中文","episodes":null,"trailerId":"abcdefghijk","trailerSite":"youtube"}]}`)
		case "/api/anime/schedule":
			respond(w, 200, `{"data":{"groups":{"2026-09-06":[{"anilistId":1,"episode":3,"airingAt":1788656400}]}}}`)
		case "/api/anime/1":
			respond(w, 200, `{"data":{"anilistId":1,"trailer":{"id":"abcdefghijk","site":"youtube"}}}`)
		default:
			respond(w, 200, `{"data":[]}`)
		}
	})
	client.RestoreSession(animego.Session{AccessToken: "must-stay-private"})
	ctx := context.Background()
	items, err := client.Seasonal(ctx, "WINTER", 2027)
	require.NoError(t, err)
	require.Nil(t, items[0].Episodes)
	_, err = client.Trending(ctx)
	require.NoError(t, err)
	_, err = client.Gems(ctx)
	require.NoError(t, err)
	_, err = client.YearlyTop(ctx, 2026)
	require.NoError(t, err)
	schedule, err := client.Schedule(ctx)
	require.NoError(t, err)
	require.Equal(t, int64(1788656400), schedule.Groups["2026-09-06"][0].AiringAt)
	detail, err := client.Detail(ctx, 1)
	require.NoError(t, err)
	require.Equal(t, "abcdefghijk", detail.Trailer.ID)
	require.Len(t, rec.all(), 6)
}
func TestCatalogRejectsMissingMalformedAndNullData(t *testing.T) {
	for _, body := range []string{`{}`, `{"data":null}`, `{"data":{}}`, `not-json`} {
		t.Run(body, func(t *testing.T) {
			c := newTestClient(t, nil, func(w http.ResponseWriter, r *http.Request) { respond(w, 200, body) })
			_, err := c.Trending(context.Background())
			var classified *animego.Error
			require.ErrorAs(t, err, &classified)
			require.Equal(t, animego.ErrDecode, classified.Kind)
		})
	}
}

func TestCatalogSearchSendsKeywordAndDecodesPagedEnvelope(t *testing.T) {
	rec := &recorder{}
	client := newTestClient(t, rec, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/anime/search", r.URL.Path)
		require.Equal(t, "葬送的芙莉莲", r.URL.Query().Get("q"))
		require.Equal(t, "1", r.URL.Query().Get("page"))
		require.Equal(t, "20", r.URL.Query().Get("perPage"))
		// 搜索响应比其余目录多一个 pagination；只要 data
		respond(w, 200, `{"data":[{"anilistId":154587,"titleChinese":"葬送的芙莉莲"}],"pagination":{"page":1,"perPage":20,"total":1,"totalPages":1}}`)
	})

	rows, err := client.Search(context.Background(), "葬送的芙莉莲")

	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, 154587, rows[0].AnilistID)
	require.Equal(t, "葬送的芙莉莲", rows[0].TitleChinese)
}

// 集号偏移：公开读、不带凭证；known 与 offset 原样带回（known=false 不能变成 0）。
func TestEpisodeOffsetReadsKnownFlag(t *testing.T) {
	rec := &recorder{}
	client := newTestClient(t, rec, func(w http.ResponseWriter, r *http.Request) {
		require.Empty(t, r.Header.Get("Authorization"))
		switch r.URL.Path {
		case "/api/anime/182255/episode-offset":
			respond(w, 200, `{"data":{"known":true,"offset":28}}`)
		case "/api/anime/9/episode-offset":
			respond(w, 200, `{"data":{"known":false,"offset":0}}`)
		default:
			respond(w, 404, `{"error":"not found"}`)
		}
	})
	client.RestoreSession(animego.Session{AccessToken: "must-stay-private"})

	got, err := client.EpisodeOffset(context.Background(), 182255)
	require.NoError(t, err)
	require.Equal(t, animego.EpisodeOffset{Known: true, Offset: 28}, got)

	got, err = client.EpisodeOffset(context.Background(), 9)
	require.NoError(t, err)
	require.False(t, got.Known)

	_, err = client.EpisodeOffset(context.Background(), 0)
	var classified *animego.Error
	require.ErrorAs(t, err, &classified)
	require.Equal(t, animego.ErrBadRequest, classified.Kind)
}
