package player

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/nagare-project/nagare/internal/mpv"
	"github.com/nagare-project/nagare/internal/store"
	"github.com/stretchr/testify/require"
)

func TestRemoteProgressTimeoutRespectsPauseSeekAndAdvancingPlayback(t *testing.T) {
	now := time.Unix(100, 0)
	p := remoteProgress{lastChange: now}
	require.False(t, p.stalled(mpv.State{}, now.Add(44*time.Second), 45*time.Second))
	require.True(t, p.stalled(mpv.State{}, now.Add(45*time.Second), 45*time.Second))
	require.False(t, p.stalled(mpv.State{Paused: true}, now.Add(time.Minute), 45*time.Second))
	require.False(t, p.stalled(mpv.State{Paused: true}, now.Add(10*time.Minute), 45*time.Second))
	require.False(t, p.stalled(mpv.State{TimePos: 120}, now.Add(11*time.Minute), 45*time.Second))
	require.False(t, p.stalled(mpv.State{TimePos: 121}, now.Add(11*time.Minute+time.Second), 45*time.Second))
	require.True(t, p.stalled(mpv.State{TimePos: 121}, now.Add(11*time.Minute+46*time.Second), 45*time.Second))
}

func TestRemoteStalledMPVClosesAndPublishesFailure(t *testing.T) {
	info, err := mpv.Detect("")
	if err != nil {
		t.Skip("本机没有 mpv")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "state.json"))
	require.NoError(t, err)
	m := New(Options{Store: st, RuntimeDir: dir, RemoteStallTimeout: 500 * time.Millisecond,
		MPV: mpv.NewRuntimeWith(func(string) (mpv.Info, error) { return info, nil }, "")})
	defer m.Stop()
	result, err := m.Play(context.Background(), NewRemoteSource(RemoteSourceOptions{
		SourceID: "fixture", CandidateID: "stalled", Title: "停滞测试", Episode: 1, URL: server.URL + "/stalled.mp4",
	}), "")
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		status := m.Status()
		return !status.Playing && status.PlaybackFailure != nil && status.PlaybackFailure.FileID == result.FileID
	}, 8*time.Second, 50*time.Millisecond)
	require.Contains(t, m.Status().PlaybackFailure.Reason, "没有播放进度")
}
