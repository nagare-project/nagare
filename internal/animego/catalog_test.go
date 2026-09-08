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
