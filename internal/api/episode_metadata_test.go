package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEpisodeMetadataUsesRealStillsAndCachesPublicRequests(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		require.Empty(t, r.Header.Get("Authorization"))
		require.Empty(t, r.Header.Get("Cookie"))
		require.Empty(t, r.Header.Get("X-Nagare-Token"))
		require.Equal(t, "7", r.URL.Query().Get("anilist_id"))
		fmt.Fprint(w, `{"mappings":{"anilist_id":7},"episodes":{"2":{"title":{"en":"Second"},"image":"https://artworks.thetvdb.com/banners/two.jpg","overview":"<b>Summary</b>","runtime":24,"airDate":"2026-09-18","airDateUtc":"2026-09-18T14:00:00Z"},"1":{"title":{"en":"First"},"image":"https://evil.test/image.jpg"},"S1":{"title":{"en":"Special"}}}}`)
	}))
	defer server.Close()
	art := NewRemoteArt("https://catalog.example", 4096)
	art.SetPrefix("/art/test")
	service := newEpisodeMetadataService(art)
	service.endpoint = server.URL + "?anilist_id="
	rows, err := service.view(context.Background(), 7)
	require.NoError(t, err)
	require.Len(t, rows, 2)
	require.Equal(t, 1, rows[0].Episode)
	require.Empty(t, rows[0].Image)
	require.Equal(t, 2, rows[1].Episode)
	require.Equal(t, "Summary", rows[1].Description)
	require.Equal(t, "2026-09-18", rows[1].AirDate)
	require.Equal(t, "2026-09-18T14:00:00Z", rows[1].AiredAt)
	source, ok := art.Source(strings.TrimPrefix(rows[1].Image, "/art/test/remote/"))
	require.True(t, ok)
	require.Equal(t, "https://artworks.thetvdb.com/banners/two.jpg", source)
	_, err = service.view(context.Background(), 7)
	require.NoError(t, err)
	require.Equal(t, 1, calls)
}

func TestEpisodeMetadataRejectsWrongIdentityAndRetriesFailures(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		fmt.Fprint(w, `{"mappings":{"anilist_id":8},"episodes":{}}`)
	}))
	defer server.Close()
	service := newEpisodeMetadataService(NewRemoteArt("", 1))
	service.endpoint = server.URL + "?anilist_id="
	for range 2 {
		_, err := service.view(context.Background(), 7)
		require.Error(t, err)
	}
	require.Equal(t, 2, calls)
}

func TestEpisodeArtworkAllowlist(t *testing.T) {
	art := NewRemoteArt("", 10)
	art.SetPrefix("/art/test")
	require.NotEmpty(t, art.Register("https://artworks.thetvdb.com/banners/a.jpg"))
	for _, source := range []string{"https://artworks.thetvdb.com.evil.test/a", "http://artworks.thetvdb.com/a", "https://artworks.thetvdb.com:8443/a", "https://user@artworks.thetvdb.com/a"} {
		require.Empty(t, art.Register(source))
	}
}
