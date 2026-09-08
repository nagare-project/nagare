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

// 从未配置过磁力时读到的是默认值：端口映射【开】、默认端口非零。
// 这条是指针存储的意义所在 —— 用零值结构体表示「缺席」会让老状态文件
// 加载后把 PortForwarding 静默变成关。
func TestTorrentConfigDefaultsWhenNeverSet(t *testing.T) {
	s, _ := newStore(t)
	c := s.TorrentConfig()
	assert.True(t, c.PortForwarding, "未配置时端口映射应为默认开")
	assert.False(t, c.Seeding, "持续做种默认关")
	assert.Equal(t, defaultListenPort, c.ListenPort)
	assert.Empty(t, c.Trackers, "本体不内置任何 tracker")
}

// 显式关掉的 PortForwarding 必须在重新加载后保持关 —— 不能被默认值覆盖回去。
func TestTorrentConfigExplicitFalseSurvivesReload(t *testing.T) {
	s, path := newStore(t)
	got, err := s.UpdateTorrentConfig(func(c *TorrentConfig) {
		c.PortForwarding = false
		c.Seeding = true
		c.ListenPort = 51413
		c.Trackers = []string{"udp://tracker.example:6969"}
	})
	require.NoError(t, err)
	assert.False(t, got.PortForwarding)

	reopened, err := Open(path)
	require.NoError(t, err)
	c := reopened.TorrentConfig()
	assert.False(t, c.PortForwarding, "显式关掉的开关不能被默认值覆盖")
	assert.True(t, c.Seeding)
	assert.Equal(t, 51413, c.ListenPort)
	assert.Equal(t, []string{"udp://tracker.example:6969"}, c.Trackers)
}

// 读出来的 Trackers 是副本：调用方改它不能影响 store 里的状态。
func TestTorrentConfigTrackersAreCopied(t *testing.T) {
	s, _ := newStore(t)
	_, err := s.UpdateTorrentConfig(func(c *TorrentConfig) {
		c.Trackers = []string{"udp://a:1", "udp://b:2"}
	})
	require.NoError(t, err)

	got := s.TorrentConfig()
	got.Trackers[0] = "udp://tampered:9"
	assert.Equal(t, "udp://a:1", s.TorrentConfig().Trackers[0], "返回的切片必须是副本")
}

func TestSourcePluginConfigRoundTrip(t *testing.T) {
	s, path := newStore(t)
	want := SourcePluginConfig{Enabled: true, Executable: "/opt/nagare-source", Root: "/srv/nagare-sources"}
	require.NoError(t, s.SetSourcePluginConfig(want))

	reopened, err := Open(path)
	require.NoError(t, err)
	assert.Equal(t, want, reopened.SourcePluginConfig())
}
