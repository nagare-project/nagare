package store

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newStore(t *testing.T) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "state.json")
	s, err := Open(path)
	require.NoError(t, err)
	return s, path
}

// 首次打开不存在的文件应得到空状态且不落盘；首次写入才建文件（0600）。
func TestOpenMissingAndFirstWrite(t *testing.T) {
	s, path := newStore(t)
	_, err := os.Stat(path)
	assert.True(t, os.IsNotExist(err), "只读打开不应创建文件")

	f, err := s.AddFolder("/tmp/anime")
	require.NoError(t, err)
	assert.NotEmpty(t, f.ID)

	info, err := os.Stat(path)
	require.NoError(t, err)
	if runtime.GOOS != "windows" {
		assert.Equal(t, os.FileMode(0o600), info.Mode().Perm(), "状态文件含会话凭证，必须 0600")
	}
	_, err = os.Stat(path + ".tmp")
	assert.True(t, os.IsNotExist(err), "原子写不应残留 .tmp")
}

// 全字段写入 → 重新打开 → 数据一致。
func TestRoundTrip(t *testing.T) {
	s, path := newStore(t)
	_, err := s.AddFolder("/tmp/a")
	require.NoError(t, err)
	require.NoError(t, s.SetHash("id1", "deadbeef"))
	require.NoError(t, s.SetBinding("id1", Binding{AnilistID: 9, DandanEpisodeID: 184300007, Episode: 7, Title: "芙莉莲", MatchedAt: 1}))
	require.NoError(t, s.SetProgress("id1", Progress{PositionSec: 12.5, DurationSec: 1420, UpdatedAt: 2, Completed: false}))
	require.NoError(t, s.SetAnimegoSession(AnimegoSession{Email: "a@b.c", AccessToken: "tok", RefreshCookie: "rc"}))

	re, err := Open(path)
	require.NoError(t, err)
	snap := re.Snapshot()
	assert.Len(t, snap.Folders, 1)
	assert.Equal(t, "deadbeef", re.Hash("id1"))
	b, ok := re.Binding("id1")
	require.True(t, ok)
	assert.Equal(t, int64(184300007), b.DandanEpisodeID)
	p, ok := re.Progress("id1")
	require.True(t, ok)
	assert.InDelta(t, 12.5, p.PositionSec, 0.001)
	assert.Equal(t, "a@b.c", re.AnimegoSession().Email)
}

// 重复目录报错；移除后可重加。
func TestFolderDuplicateAndRemove(t *testing.T) {
	s, _ := newStore(t)
	f, err := s.AddFolder("/tmp/x")
	require.NoError(t, err)
	_, err = s.AddFolder("/tmp/x")
	assert.Error(t, err, "重复路径应报错")

	ok, err := s.RemoveFolder(f.ID)
	require.NoError(t, err)
	assert.True(t, ok)
	ok, err = s.RemoveFolder(f.ID)
	require.NoError(t, err)
	assert.False(t, ok, "重复移除应返回不存在")

	_, err = s.AddFolder("/tmp/x")
	assert.NoError(t, err)
}

// 坏 JSON 是硬错误：绝不静默重建用户状态。
func TestOpenMalformedFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	require.NoError(t, os.WriteFile(path, []byte("{broken"), 0o600))
	_, err := Open(path)
	assert.Error(t, err)
}

// Snapshot 是深拷贝：外部改动不应影响内部状态。
func TestSnapshotIsCopy(t *testing.T) {
	s, _ := newStore(t)
	require.NoError(t, s.SetHash("id1", "aaaa"))
	snap := s.Snapshot()
	snap.Hashes["id1"] = "bbbb"
	assert.Equal(t, "aaaa", s.Hash("id1"))
}
