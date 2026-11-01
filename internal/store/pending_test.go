package store

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 只列「看完了、还没记到账号」的，最早更新的在前；since 之前的不要。
func TestPendingSyncsOldestFirst(t *testing.T) {
	s, _ := newStore(t)
	require.NoError(t, s.SetProgress("late", Progress{Completed: true, UpdatedAt: 300}))
	require.NoError(t, s.SetProgress("early", Progress{Completed: true, UpdatedAt: 200}))
	require.NoError(t, s.SetProgress("tie-b", Progress{Completed: true, UpdatedAt: 250}))
	require.NoError(t, s.SetProgress("tie-a", Progress{Completed: true, UpdatedAt: 250}))
	require.NoError(t, s.SetProgress("synced", Progress{Completed: true, Synced: true, UpdatedAt: 100}))
	require.NoError(t, s.SetProgress("half", Progress{PositionSec: 600, UpdatedAt: 100}))
	require.NoError(t, s.SetProgress("ancient", Progress{Completed: true, UpdatedAt: 50}))

	var ids []string
	for _, p := range s.PendingSyncs(100) {
		ids = append(ids, p.FileID)
	}
	assert.Equal(t, []string{"early", "tie-a", "tie-b", "late"}, ids)
	assert.Len(t, s.PendingSyncs(0), 5, "since 为 0 时不按时间筛")
}

// 「挡下过」要能落盘读回；没有挡下时文件里不出现这个字段（零值不写）。
func TestSyncHoldRoundTrip(t *testing.T) {
	s, path := newStore(t)
	require.NoError(t, s.SetProgress("held", Progress{Completed: true, SyncHold: SyncHold{AnilistID: 9, Episode: 38}}))
	require.NoError(t, s.SetProgress("plain", Progress{Completed: true}))

	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, 1, strings.Count(string(raw), `"syncHold"`))

	reopened, err := Open(path)
	require.NoError(t, err)
	p, ok := reopened.Progress("held")
	require.True(t, ok)
	assert.True(t, p.SyncHold.Holds(9, 38))
	assert.False(t, p.SyncHold.Holds(9, 37))
	assert.False(t, SyncHold{}.Holds(0, 0), "零值不挡任何匹配")
}
