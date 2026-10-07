package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// skipIfPermissionsIgnored：Windows 的目录只读语义不同，root 无视 chmod —— 这两处造不出写失败。
func skipIfPermissionsIgnored(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("造不出目录不可写")
	}
}

// blockSave 在临时文件的位置放一个同名目录，让下一次落盘必然失败（save 会先把目录权限
// 改回 0700，只读目录造不出失败）。返回的函数撤掉它。
func blockSave(t *testing.T, path string) func() {
	t.Helper()
	require.NoError(t, os.Mkdir(path+".tmp", 0o700))
	return func() { require.NoError(t, os.Remove(path+".tmp")) }
}

// readOnlyDir 把目录改成只读，测试结束时恢复（否则 TempDir 清不掉）。
func readOnlyDir(t *testing.T, dir string) {
	t.Helper()
	require.NoError(t, os.Chmod(dir, 0o500))
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
}

const oldFormat = `{"folders":[{"id":"f-1","path":"/a","addedAt":1}],"bindings":{"id1":{"anilistId":9,"matchedAt":1}},"animego":{"email":"a@b.c","accessToken":"secret-access","refreshCookie":"secret-refresh"}}`

// 旧格式（没有 schemaVersion）第一次打开：备份成 .v1.bak（去掉会话凭证），内容照常读出来，
// 下一次写回带上新版本号。
func TestOpenBacksUpOldFormatWithoutCredentials(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	require.NoError(t, os.WriteFile(path, []byte(oldFormat), 0o600))

	s, err := Open(path)
	require.NoError(t, err)

	bak, err := os.ReadFile(path + ".v1.bak")
	require.NoError(t, err)
	assert.NotContains(t, string(bak), "secret", "备份不该留一份明文会话凭证")
	var doc map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(bak, &doc))
	assert.Contains(t, doc, "folders")
	assert.Contains(t, doc, "bindings")
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path + ".v1.bak")
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	}
	_, err = os.Stat(path + ".v1.bak.tmp")
	assert.True(t, os.IsNotExist(err), "不应残留临时文件")

	b, ok := s.Binding("id1")
	require.True(t, ok)
	assert.Equal(t, 9, b.AnilistID)
	assert.Equal(t, "secret-access", s.AnimegoSession().AccessToken, "状态文件本身照常保留会话")

	require.NoError(t, s.SetHash("id1", "h"))
	var onDisk struct {
		SchemaVersion int `json:"schemaVersion"`
	}
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(raw, &onDisk))
	assert.Equal(t, SchemaVersion, onDisk.SchemaVersion)
}

// 已有一份可用的备份就不覆盖（降级再升级时，最早那份才是用户真正的旧数据）；
// 空文件、半截 JSON 不算可用，会被重写。新格式的文件不再备份。
func TestOpenBackupKeepsGoodOneAndReplacesBrokenOne(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "good.json")
	require.NoError(t, os.WriteFile(good+".v1.bak", []byte(`{"earliest":true}`), 0o600))
	require.NoError(t, os.WriteFile(good, []byte(`{"folders":[]}`), 0o600))
	_, err := Open(good)
	require.NoError(t, err)
	bak, err := os.ReadFile(good + ".v1.bak")
	require.NoError(t, err)
	assert.JSONEq(t, `{"earliest":true}`, string(bak))

	for name, broken := range map[string]string{"empty": "", "truncated": `{"folders":[`} {
		path := filepath.Join(dir, name+".json")
		require.NoError(t, os.WriteFile(path+".v1.bak", []byte(broken), 0o600))
		require.NoError(t, os.WriteFile(path, []byte(`{"folders":[]}`), 0o600))
		_, err := Open(path)
		require.NoError(t, err)
		bak, err := os.ReadFile(path + ".v1.bak")
		require.NoError(t, err)
		assert.JSONEq(t, `{"folders":[]}`, string(bak), name)
	}

	current := filepath.Join(dir, "current.json")
	require.NoError(t, os.WriteFile(current, []byte(`{"schemaVersion":2,"folders":[]}`), 0o600))
	_, err = Open(current)
	require.NoError(t, err)
	_, err = os.Stat(current + ".v2.bak")
	assert.True(t, os.IsNotExist(err), "已经是当前格式就不该备份")
}

// 从更新的版本降级回来：这个版本不认识的字段下次写入会丢，同样先备份。
func TestOpenBacksUpNewerFormat(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"schemaVersion":9,"folders":[],"future":{"x":1}}`), 0o600))

	_, err := Open(path)
	require.NoError(t, err)
	bak, err := os.ReadFile(path + ".v9.bak")
	require.NoError(t, err)
	assert.Contains(t, string(bak), "future")
}

// 全新安装不需要备份（没有旧数据）。
func TestFreshStoreHasNoBackup(t *testing.T) {
	s, path := newStore(t)
	require.NoError(t, s.SetHash("id", "h"))
	_, err := os.Stat(path + ".v1.bak")
	assert.True(t, os.IsNotExist(err))
}

// 备份写不出来就不升级：宁可启动报错，也不在没有退路的情况下改写用户状态。
func TestOpenRefusesUpgradeWhenBackupFails(t *testing.T) {
	skipIfPermissionsIgnored(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"folders":[]}`), 0o600))
	readOnlyDir(t, dir)

	_, err := Open(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "备份旧状态文件")
}

// 关联落盘后重开仍在；Members 超过上限只留前 MaxAssociationMembers 个；读出来的是副本。
func TestAssociationRoundTripAndMemberCap(t *testing.T) {
	s, path := newStore(t)
	members := make([]string, MaxAssociationMembers+5)
	for i := range members {
		members[i] = string(rune('a'+i%26)) + "|" + string(rune('0'+i%10))
	}
	_, err := s.ApplyAssociation("k1", &Association{Mode: AssociationManual, AnilistID: 154587, Title: "葬送的芙莉莲", Members: members, SetAt: 7}, nil)
	require.NoError(t, err)

	re, err := Open(path)
	require.NoError(t, err)
	a, ok := re.Association("k1")
	require.True(t, ok)
	assert.Equal(t, AssociationManual, a.Mode)
	assert.Equal(t, 154587, a.AnilistID)
	assert.Len(t, a.Members, MaxAssociationMembers)

	a.Members[0] = "被改掉了"
	again, _ := re.Association("k1")
	assert.NotEqual(t, "被改掉了", again.Members[0], "调用方改副本不能影响内部状态")
	assert.Contains(t, re.Snapshot().Associations, "k1")
}

// 手动关联到 X：只作废指向别处的匹配，已经匹配到 X 的保留；被作废且已回写过的那几集
// 记进 SyncedElsewhere（持久化，不只在一次响应里出现），并清掉 Synced。
func TestApplyManualAssociationDropsOnlyMismatchedBindings(t *testing.T) {
	s, path := newStore(t)
	require.NoError(t, s.SetBinding("ep1", Binding{AnilistID: 111, Episode: 1, Title: "认错的番"}))
	require.NoError(t, s.SetBinding("ep2", Binding{AnilistID: 111, Episode: 2, Title: "认错的番"}))
	require.NoError(t, s.SetBinding("ep3", Binding{AnilistID: 222, Episode: 3, Title: "对的番"}))
	require.NoError(t, s.SetProgress("ep1", Progress{Completed: true, Synced: true}))
	require.NoError(t, s.SetProgress("ep2", Progress{PositionSec: 300}))
	require.NoError(t, s.SetProgress("ep3", Progress{Completed: true, Synced: true}))

	got, err := s.ApplyAssociation("k", &Association{Mode: AssociationManual, AnilistID: 222}, []string{"ep1", "ep2", "ep3", "ep4"})
	require.NoError(t, err)

	want := []SyncedRecord{{AnilistID: 111, Title: "认错的番", Episodes: []int{1}}}
	assert.Equal(t, want, got.SyncedElsewhere, "没看完的那集没有回写过，不算")
	_, ok := s.Binding("ep1")
	assert.False(t, ok)
	_, ok = s.Binding("ep2")
	assert.False(t, ok)
	kept, ok := s.Binding("ep3")
	require.True(t, ok, "已经匹配到对的作品，弹幕与集号照用")
	assert.Equal(t, 222, kept.AnilistID)

	p1, _ := s.Progress("ep1")
	assert.True(t, p1.Completed, "进度本身不动")
	assert.False(t, p1.Synced, "回写到的是旧作品，要按新关联重来")
	p3, _ := s.Progress("ep3")
	assert.True(t, p3.Synced, "保留的匹配不受影响")

	re, err := Open(path)
	require.NoError(t, err)
	persisted, _ := re.Association("k")
	assert.Equal(t, want, persisted.SyncedElsewhere)
}

// 待修正的记录跨多次改关联累积；改成认定为那部作品时，回写到它上面的就不再算「写错了」。
func TestSyncedElsewhereCarriesOverAndClearsForTheTarget(t *testing.T) {
	s, _ := newStore(t)
	require.NoError(t, s.SetBinding("ep1", Binding{AnilistID: 111, Episode: 1, Title: "甲"}))
	require.NoError(t, s.SetProgress("ep1", Progress{Completed: true, Synced: true}))
	_, err := s.ApplyAssociation("k", &Association{Mode: AssociationNone}, []string{"ep1", "ep2"})
	require.NoError(t, err)

	require.NoError(t, s.SetBinding("ep2", Binding{AnilistID: 333, Episode: 2, Title: "乙"}))
	require.NoError(t, s.SetProgress("ep2", Progress{Completed: true, Synced: true}))
	got, err := s.ApplyAssociation("k", &Association{Mode: AssociationManual, AnilistID: 222}, []string{"ep1", "ep2"})
	require.NoError(t, err)
	assert.Equal(t, []SyncedRecord{
		{AnilistID: 111, Title: "甲", Episodes: []int{1}},
		{AnilistID: 333, Title: "乙", Episodes: []int{2}},
	}, got.SyncedElsewhere)

	got, err = s.ApplyAssociation("k", &Association{Mode: AssociationManual, AnilistID: 111}, []string{"ep1", "ep2"})
	require.NoError(t, err)
	assert.Equal(t, []SyncedRecord{{AnilistID: 333, Title: "乙", Episodes: []int{2}}}, got.SyncedElsewhere)
}

// 标为「不是目录里的作品」或回到自动匹配：分组内的匹配全部作废、Synced 清掉；回到自动匹配会删掉关联。
func TestApplyNoneAndAutoDropAllBindings(t *testing.T) {
	s, _ := newStore(t)
	require.NoError(t, s.SetBinding("ep1", Binding{AnilistID: 222, Episode: 1}))
	require.NoError(t, s.SetProgress("ep1", Progress{Completed: true, Synced: true}))
	_, err := s.ApplyAssociation("k", &Association{Mode: AssociationNone}, []string{"ep1"})
	require.NoError(t, err)
	_, ok := s.Binding("ep1")
	assert.False(t, ok)
	p, _ := s.Progress("ep1")
	assert.False(t, p.Synced)
	a, ok := s.Association("k")
	require.True(t, ok)
	assert.Equal(t, AssociationNone, a.Mode)

	require.NoError(t, s.SetBinding("ep1", Binding{AnilistID: 222, Episode: 1}))
	got, err := s.ApplyAssociation("k", nil, []string{"ep1"})
	require.NoError(t, err)
	assert.Equal(t, Association{}, got)
	_, ok = s.Binding("ep1")
	assert.False(t, ok)
	_, ok = s.Association("k")
	assert.False(t, ok, "回到自动匹配 = 不再有关联记录")
}

// 落盘失败：内存里的匹配、进度标记、关联整体回滚，重试时还能看到同样的待修正记录。
func TestApplyAssociationRollsBackWhenSaveFails(t *testing.T) {
	s, path := newStore(t)
	require.NoError(t, s.SetBinding("ep1", Binding{AnilistID: 111, Episode: 1, Title: "甲"}))
	require.NoError(t, s.SetProgress("ep1", Progress{Completed: true, Synced: true}))
	unblock := blockSave(t, path)

	_, err := s.ApplyAssociation("k", &Association{Mode: AssociationManual, AnilistID: 222}, []string{"ep1"})
	require.Error(t, err)

	b, ok := s.Binding("ep1")
	require.True(t, ok)
	assert.Equal(t, 111, b.AnilistID)
	p, _ := s.Progress("ep1")
	assert.True(t, p.Synced)
	_, ok = s.Association("k")
	assert.False(t, ok)

	unblock()
	got, err := s.ApplyAssociation("k", &Association{Mode: AssociationManual, AnilistID: 222}, []string{"ep1"})
	require.NoError(t, err)
	assert.Len(t, got.SyncedElsewhere, 1)
}

func TestApplyAssociationRejectsEmptyKey(t *testing.T) {
	s, _ := newStore(t)
	_, err := s.ApplyAssociation("", &Association{Mode: AssociationNone}, nil)
	assert.Error(t, err)
}

// UpdateAssociations：返回 false 不落盘；返回 true 整体替换并落盘；落盘失败回滚。
func TestUpdateAssociations(t *testing.T) {
	s, path := newStore(t)
	require.NoError(t, s.UpdateAssociations(func(map[string]Association) bool { return false }))
	_, err := os.Stat(path)
	assert.True(t, os.IsNotExist(err), "没有改动不该写文件")

	require.NoError(t, s.UpdateAssociations(func(all map[string]Association) bool {
		all["new"] = Association{Mode: AssociationNone, Members: []string{"a"}}
		return true
	}))
	re, err := Open(path)
	require.NoError(t, err)
	_, ok := re.Association("new")
	assert.True(t, ok)

	defer blockSave(t, path)()
	require.Error(t, s.UpdateAssociations(func(all map[string]Association) bool {
		delete(all, "new")
		return true
	}))
	_, ok = s.Association("new")
	assert.True(t, ok, "落盘失败不能只改了内存")
}

// 按作品归拢：作品按首次出现排，集号升序去重；标题缺的用后来的补上。
func TestMergeSynced(t *testing.T) {
	var got []SyncedRecord
	for _, step := range []struct {
		id, ep int
		title  string
	}{{2, 3, ""}, {1, 2, "甲"}, {2, 1, "乙"}, {2, 1, "乙"}, {2, 2, "乙"}} {
		got = mergeSynced(got, step.id, step.title, step.ep)
	}
	assert.Equal(t, []SyncedRecord{
		{AnilistID: 2, Title: "乙", Episodes: []int{1, 2, 3}},
		{AnilistID: 1, Title: "甲", Episodes: []int{2}},
	}, got)
}

// 去掉一部作品的「已回写到别处」记录：只动那一条，其余与关联本身不变；没有关联时报 false。
func TestDismissSyncedElsewhere(t *testing.T) {
	s, path := newStore(t)
	require.NoError(t, s.UpdateAssociations(func(all map[string]Association) bool {
		all["k"] = Association{Mode: AssociationManual, AnilistID: 1, SyncedElsewhere: []SyncedRecord{
			{AnilistID: 2, Episodes: []int{1}}, {AnilistID: 3, Episodes: []int{2}},
		}}
		return true
	}))

	got, ok, err := s.DismissSyncedElsewhere("k", 2)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, []SyncedRecord{{AnilistID: 3, Episodes: []int{2}}}, got.SyncedElsewhere)
	assert.Equal(t, 1, got.AnilistID)

	got, _, err = s.DismissSyncedElsewhere("k", 3)
	require.NoError(t, err)
	assert.Nil(t, got.SyncedElsewhere)
	re, err := Open(path)
	require.NoError(t, err)
	persisted, _ := re.Association("k")
	assert.Nil(t, persisted.SyncedElsewhere)

	_, ok, err = s.DismissSyncedElsewhere("missing", 2)
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestDismissSyncedElsewhereRollsBackWhenSaveFails(t *testing.T) {
	s, path := newStore(t)
	require.NoError(t, s.UpdateAssociations(func(all map[string]Association) bool {
		all["k"] = Association{Mode: AssociationNone, SyncedElsewhere: []SyncedRecord{{AnilistID: 2, Episodes: []int{1}}}}
		return true
	}))
	defer blockSave(t, path)()

	_, _, err := s.DismissSyncedElsewhere("k", 2)
	require.Error(t, err)
	a, _ := s.Association("k")
	assert.Len(t, a.SyncedElsewhere, 1)
}
