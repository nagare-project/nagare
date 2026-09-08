package torrentstream

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTorrentMetaInfoDownloadIsBounded(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write([]byte(strings.Repeat("x", maximumTorrentBytes+1)))
	}))
	defer server.Close()

	_, err := fetchTorrentMetaInfoWithClient(context.Background(), server.URL, server.Client())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "4 MiB")
}

func TestTorrentURLRejectsCredentialsAndPrivateAddresses(t *testing.T) {
	require.Error(t, validateTorrentURL("https://user:secret@example.com/release.torrent"))
	require.Error(t, validateTorrentURL("file:///tmp/release.torrent"))
	for _, raw := range []string{"127.0.0.1", "10.0.0.1", "169.254.169.254", "100.64.0.1", "255.255.255.255", "::1"} {
		assert.Truef(t, unsafeTorrentAddress(net.ParseIP(raw)), "%s 应被拦截", raw)
	}
}
