package api

import (
	"github.com/nagare-project/nagare/internal/artcache"
	"github.com/stretchr/testify/require"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestRemoteArtAllowlistAndLRUEviction(t *testing.T) {
	art := NewRemoteArt("https://animego.example", 2)
	art.SetPrefix("/art/cap")
	for _, url := range []string{"http://s4.anilist.co/a", "https://evil.test/a", "https://s4.anilist.co.evil/a", "https://user@s4.anilist.co/a", "https://s4.anilist.co/a#x", "https://s4.anilist.co:444/a"} {
		require.Empty(t, art.Register(url))
	}
	first := art.Register("https://s4.anilist.co/1")
	second := art.Register("https://animego.example/2")
	key := func(s string) string { return strings.TrimPrefix(s, "/art/cap/remote/") }
	require.Len(t, key(first), 64)
	source, ok := art.Source(key(first))
	require.True(t, ok)
	require.Equal(t, "https://s4.anilist.co/1", source)
	require.NotEmpty(t, art.Register("https://s4.anilist.co/3"))
	_, ok = art.Source(key(second))
	require.False(t, ok)
	_, ok = art.Source(key(first))
	require.True(t, ok)
	require.Len(t, art.entries, 2)
}
func TestRemotePrefixNeverUsesFileBinding(t *testing.T) {
	// nil store 会在误走 Binding 时崩溃，远程路径必须完全独立。
	h := NewArtHandler(nil, nil, NoRemoteArt{})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/remote/unknown", nil))
	require.Equal(t, 404, w.Code)
}

func TestRegisteredArtServesCacheAndUnknownKey404(t *testing.T) {
	art := NewRemoteArt("https://animego.example", 2)
	art.SetPrefix("/art/cap")
	source := "https://s4.anilist.co/cover.png"
	address := art.Register(source)
	cache, err := artcache.New(t.TempDir())
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(cache.Path(source), []byte("cached-image"), 0600))
	handler := NewArtHandler(nil, cache, art)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("GET", strings.TrimPrefix(address, "/art/cap"), nil))
	require.Equal(t, 200, w.Code)
	require.Equal(t, "cached-image", w.Body.String())
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("GET", "/remote/missing", nil))
	require.Equal(t, 404, w.Code)
}
