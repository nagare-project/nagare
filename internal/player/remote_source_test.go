package player

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRemoteSourceKeepsCredentialsOutOfPersistentItem(t *testing.T) {
	options := RemoteSourceOptions{
		CandidateID: "candidate-3", SourceID: "example-web",
		URL:     "https://media.example/3.m3u8?token=secret",
		Headers: map[string]string{"Cookie": "session=secret"},
		Title:   "Example", Episode: 3, SizeBytes: 42,
	}
	source := NewRemoteSource(options)
	require.NoError(t, source.Probe(context.Background()))
	item := source.Item()
	assert.NotContains(t, item.FileID, "candidate-3")
	assert.NotContains(t, item.FileID, "secret")
	assert.Empty(t, item.AbsPath)
	assert.Equal(t, "Example", item.FileName)
	assert.Equal(t, 3, *item.Episode)
	assert.Equal(t, options.URL, source.MPVPath())

	withHeaders := source.(httpHeaderSource)
	headers := withHeaders.HTTPHeaders()
	headers["Cookie"] = "changed"
	assert.Equal(t, "session=secret", withHeaders.HTTPHeaders()["Cookie"])
	assert.True(t, sourceRedactsDiagnostics(source))
}

func TestRemoteSourceRejectsExpiredOrInvalidCandidate(t *testing.T) {
	expired := NewRemoteSource(RemoteSourceOptions{
		CandidateID: "old", SourceID: "example-web", URL: "https://media.example/old.m3u8",
		Title: "Example", Episode: 1, ExpiresAt: time.Now().Add(-time.Second).UnixMilli(),
	})
	require.ErrorContains(t, expired.Probe(context.Background()), "过期")

	invalid := NewRemoteSource(RemoteSourceOptions{
		CandidateID: "bad", SourceID: "example-web", URL: "file:///tmp/video.mkv", Title: "Example", Episode: 1,
	})
	require.ErrorContains(t, invalid.Probe(context.Background()), "地址无效")
}
