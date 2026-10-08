package store

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func openTemp(t *testing.T) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "state.json")
	s, err := Open(path)
	require.NoError(t, err)
	return s, path
}

// 识别结果的落盘：一把锁内核对关联、合并已有的匹配、记下问过了、缓存指纹，并且真的写到了盘上。
func TestSaveIdentifiedOutcomes(t *testing.T) {
	s, path := openTemp(t)
	full := Binding{AnilistID: 1, DandanEpisodeID: 11, Episode: 1, Title: "作品", CoverURL: "https://c/1", MatchedAt: 5}

	out, err := s.SaveIdentified("k", "f1", "hash1", 100, full)
	require.NoError(t, err)
	assert.Equal(t, IdentifySaved, out)
	out, err = s.SaveIdentified("k", "f1", "hash1", 101, full)
	require.NoError(t, err)
	assert.Equal(t, IdentifyUnchanged, out)

	// 已有指向别的作品的匹配：不改
	require.NoError(t, s.SetBinding("f2", Binding{AnilistID: 9, Episode: 1}))
	out, err = s.SaveIdentified("k", "f2", "hash2", 102, full)
	require.NoError(t, err)
	assert.Equal(t, IdentifyConflict, out)
	b, _ := s.Binding("f2")
	assert.Equal(t, 9, b.AnilistID)

	// 同一部作品、缺封面：只补缺的，并换匹配时间
	require.NoError(t, s.SetBinding("f3", Binding{AnilistID: 1, DandanEpisodeID: 33, Episode: 3, MatchedAt: 1}))
	out, err = s.SaveIdentified("k", "f3", "hash3", 103, full)
	require.NoError(t, err)
	assert.Equal(t, IdentifySaved, out)
	b, _ = s.Binding("f3")
	assert.Equal(t, "https://c/1", b.CoverURL)
	assert.Equal(t, int64(33), b.DandanEpisodeID)
	assert.Equal(t, int64(5), b.MatchedAt)

	// 分组有关联：不写匹配，但记下问过了
	_, err = s.ApplyAssociation("assoc", &Association{Mode: AssociationNone}, nil)
	require.NoError(t, err)
	out, err = s.SaveIdentified("assoc", "f4", "hash4", 104, full)
	require.NoError(t, err)
	assert.Equal(t, IdentifyBlocked, out)
	_, had := s.Binding("f4")
	assert.False(t, had)

	reopened, err := Open(path)
	require.NoError(t, err)
	for id, at := range map[string]int64{"f1": 101, "f2": 102, "f3": 103, "f4": 104} {
		assert.Equal(t, at, reopened.IdentifyAttempt(id), id)
	}
	assert.Equal(t, "hash4", reopened.Hash("f4"), "指纹随同一次写入缓存")
}

// 记录按时间清（不按文件在不在）；快照是深拷贝。
func TestIdentifyAttemptsPruneByAgeAndSnapshotCopies(t *testing.T) {
	s, _ := openTemp(t)
	require.NoError(t, s.RecordIdentifyAttempt("old", "", 10))
	require.NoError(t, s.RecordIdentifyAttempt("new", "h", 30))
	assert.Equal(t, "h", s.Hash("new"))

	snap := s.Snapshot()
	snap.IdentifyAttempts["new"] = 999
	assert.Equal(t, int64(30), s.IdentifyAttempt("new"), "快照与内部不共享")

	require.NoError(t, s.PruneIdentifyAttempts(20))
	assert.Zero(t, s.IdentifyAttempt("old"))
	assert.Equal(t, int64(30), s.IdentifyAttempt("new"))
}
