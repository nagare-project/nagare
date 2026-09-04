package catalog

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestDetailConvertsAndCaches(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var body struct {
			Query     string
			Variables map[string]int
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		require.Equal(t, 154587, body.Variables["id"])
		require.Contains(t, body.Query, "recommendations")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"Media":{"id":154587,"title":{"romaji":"Sousou no Frieren","english":"Frieren","native":"葬送のフリーレン"},"coverImage":{"extraLarge":"https://s4.anilist.co/file/anilistcdn/media/a.jpg"},"bannerImage":"http://localhost/private","description":"<b>Journey</b> &amp; life","trailer":{"id":"tR8YH0G67Rk","site":"youtube"},"season":"FALL","episodes":28,"relations":{"edges":[{"relationType":"SEQUEL","node":{"id":2,"title":{"romaji":"Sequel"},"format":"TV"}},{"relationType":"ADAPTATION","node":{"id":3,"format":"MANGA"}}]},"characters":{"edges":[{"role":"MAIN","node":{"name":{"full":"Frieren"},"image":{"large":"https://s4.anilist.co/file/character.jpg"}},"voiceActors":[{"name":{"full":"Actor"},"image":{"large":"https://s4.anilist.co/file/actor.jpg"}}]}]}}}}`))
	}))
	defer server.Close()
	client := New(server.URL, server.Client())
	client.SetArtPrefix("/art/test")
	media, err := client.Detail(context.Background(), 154587)
	require.NoError(t, err)
	require.Equal(t, "Frieren", media.TitleEnglish)
	require.Equal(t, "Journey & life", media.Description)
	require.Equal(t, "秋", media.Season)
	require.Equal(t, "tR8YH0G67Rk", media.TrailerID)
	require.Empty(t, media.Banner)
	require.Len(t, media.Relations, 1)
	require.Equal(t, "Actor", media.Characters[0].Actor)
	require.True(t, strings.HasPrefix(media.Cover, "/art/test/catalog/"))
	source, ok := client.ImageSource(strings.TrimPrefix(media.Cover, "/art/test/catalog/"))
	require.True(t, ok)
	require.Contains(t, source, "s4.anilist.co")
	_, err = client.Detail(context.Background(), 154587)
	require.NoError(t, err)
	require.Equal(t, 1, calls)
	_, ok = client.ImageSource("invented")
	require.False(t, ok)
	for _, url := range []string{"https://s4.anilist.co.evil/a", "https://user@s4.anilist.co/a", "http://s4.anilist.co/a", "https://127.0.0.1/a"} {
		require.Empty(t, client.Artwork(url))
	}
}
func TestDiscoverSeasonBoundaryAndVariables(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Query     string
			Variables map[string]any
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		require.Equal(t, "FALL", body.Variables["pastSeason"])
		require.Equal(t, float64(2025), body.Variables["pastYear"])
		require.Equal(t, "Fantasy", body.Variables["genre"])
		require.NotContains(t, body.Query, "$now")
		require.NotContains(t, body.Variables, "season")
		require.Contains(t, body.Query, "pastSeason:Page")
		_, _ = w.Write([]byte(`{"data":{"pastSeason":{"media":[{"id":1,"title":{"romaji":"Anime"}}]}}}`))
	}))
	defer server.Close()
	client := New(server.URL, server.Client())
	data, err := client.Discover(context.Background(), "pastSeason", "Fantasy", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	require.NoError(t, err)
	require.Len(t, data["pastSeason"], 1)
	require.NotNil(t, data["pastSeason"][0].Genres)
	_, err = client.Discover(context.Background(), "unknown", "", time.Now())
	require.Error(t, err)
}
func TestCatalogErrorsDoNotBecomeEmptySuccess(t *testing.T) {
	for _, response := range []string{`{"errors":[{"message":"rate limited"}]}`, `{"data":null}`, `invalid`} {
		t.Run(response, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(response)) }))
			defer server.Close()
			_, err := New(server.URL, server.Client()).Detail(context.Background(), 1)
			require.Error(t, err)
		})
	}
}
