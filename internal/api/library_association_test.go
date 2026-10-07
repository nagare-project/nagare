package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nagare-project/nagare/internal/animego"
	"github.com/nagare-project/nagare/internal/artcache"
	"github.com/nagare-project/nagare/internal/library"
	"github.com/nagare-project/nagare/internal/player"
	"github.com/nagare-project/nagare/internal/store"
)

const (
	frierenCover = "https://s4.anilist.co/frieren.jpg"
	// missingWork 模拟上游没有这部作品；pollutedWork 模拟上游数据被污染（外部图床、超长标题）
	missingWork  = 404
	pollutedWork = 666
)

type detailStub struct{ catalogStub }

func (f *detailStub) Detail(_ context.Context, id int) (animego.CatalogMedia, error) {
	switch id {
	case missingWork:
		return animego.CatalogMedia{}, &animego.Error{Kind: animego.ErrBadRequest, Op: "detail", Err: errors.New("not found")}
	case pollutedWork:
		return animego.CatalogMedia{AnilistID: id, TitleChinese: strings.Repeat("长", 5000), CoverImageURL: "https://tracker.evil/p.png"}, nil
	}
	return animego.CatalogMedia{AnilistID: id, TitleChinese: "葬送的芙莉莲", CoverImageURL: frierenCover}, nil
}

type assocEnv struct {
	mux    *http.ServeMux
	store  *store.Store
	lib    *LibraryService
	player *fakePlayer
}

// newAssocEnv 起一个带目录源、扫描过 dirs（默认一份 makeMediaDir）的环境；catalog 为 nil 表示目录不可用。
func newAssocEnv(t *testing.T, catalog CatalogReader, dirs ...string) *assocEnv {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "state.json"))
	require.NoError(t, err)
	lib := NewLibraryService(st)
	lib.SetArtPrefix("/art/cap")
	if len(dirs) == 0 {
		dirs = []string{makeMediaDir(t)}
	}
	for _, dir := range dirs {
		_, _, err = lib.AddFolder(dir)
		require.NoError(t, err)
	}
	fp := &fakePlayer{}
	h := New(Deps{Store: st, Lib: lib, Player: fp, Catalog: catalog, AnimegoBaseURL: "https://example.test"})
	mux := http.NewServeMux()
	h.Register(mux)
	return &assocEnv{mux: mux, store: st, lib: lib, player: fp}
}

func (e *assocEnv) put(t *testing.T, body string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	e.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/api/library/association", strings.NewReader(body)))
	return rec
}

func (e *assocEnv) cluster(t *testing.T) ViewCluster {
	t.Helper()
	view := e.lib.View()
	require.NotEmpty(t, view.Clusters)
	return view.Clusters[0]
}

func (e *assocEnv) fileIDs(t *testing.T) []string {
	t.Helper()
	ids, ok := e.lib.ClusterFiles(e.cluster(t).ClusterKey)
	require.True(t, ok)
	require.Len(t, ids, 2)
	return ids
}

func assocBody(t *testing.T, key, mode string, id int) string {
	t.Helper()
	return string(mustJSON(t, map[string]any{"clusterKey": key, "mode": mode, "anilistId": id}))
}

func decodeAssoc(t *testing.T, rec *httptest.ResponseRecorder) *ViewAssociation {
	t.Helper()
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var res associationResult
	require.NoError(t, json.Unmarshal(decode(t, rec).Data, &res))
	return res.Association
}

// 认定为某部作品：标题与封面由服务端从目录取；媒体库视图带出关联，封面换成那部作品的。
func TestPutAssociationManualShowsInLibraryView(t *testing.T) {
	env := newAssocEnv(t, &detailStub{})
	key := env.cluster(t).ClusterKey

	a := decodeAssoc(t, env.put(t, assocBody(t, key, "manual", 154587)))

	require.NotNil(t, a)
	assert.Equal(t, ViewAssociation{Mode: "manual", AnilistID: 154587, Title: "葬送的芙莉莲", SetAt: a.SetAt}, *a)
	assert.NotZero(t, a.SetAt)

	stored, ok := env.store.Association(key)
	require.True(t, ok)
	assert.Equal(t, frierenCover, stored.CoverURL, "封面原始地址只存在服务端")
	assert.ElementsMatch(t, env.fileIDs(t), stored.Members)

	c := env.cluster(t)
	require.NotNil(t, c.Association)
	assert.Equal(t, 154587, c.Association.AnilistID)
	assert.True(t, strings.HasPrefix(c.Cover, "/art/cap/assoc/"+key+"?v="), c.Cover)
	assert.NotContains(t, c.Cover, "anilist.co", "客户端拿到的是键，不是图床地址")
}

// 封面按 immutable 缓存：改认成别的作品后地址必须变，否则浏览器一直显示旧作品的封面。
func TestAssociationCoverURLChangesWhenReassigned(t *testing.T) {
	env := newAssocEnv(t, &detailStub{})
	key := env.cluster(t).ClusterKey
	decodeAssoc(t, env.put(t, assocBody(t, key, "manual", 1)))
	first := env.cluster(t).Cover

	decodeAssoc(t, env.put(t, assocBody(t, key, "manual", 2)))

	assert.NotEqual(t, first, env.cluster(t).Cover)
}

// 自动匹配的封面同理：作废后重新匹配到别的作品，地址要跟着变。
func TestMatchedCoverURLChangesWhenRematched(t *testing.T) {
	env := newAssocEnv(t, nil)
	id := env.fileIDs(t)[0]
	require.NoError(t, env.store.SetBinding(id, store.Binding{AnilistID: 1, CoverURL: frierenCover, MatchedAt: 1}))
	first := env.cluster(t).Cover
	require.NoError(t, env.store.SetBinding(id, store.Binding{AnilistID: 2, CoverURL: frierenCover, MatchedAt: 2}))

	assert.NotEqual(t, first, env.cluster(t).Cover)
}

// 改关联：作废指向别处的匹配；已经回写到别的作品的集记进关联（不撤销，只告诉用户），
// 媒体库视图里一直看得到，不只在那一次响应里。
func TestPutAssociationRecordsEpisodesSyncedToTheOldWork(t *testing.T) {
	env := newAssocEnv(t, &detailStub{})
	ids := env.fileIDs(t)
	for i, id := range ids {
		require.NoError(t, env.store.SetBinding(id, store.Binding{AnilistID: 999, Episode: i + 1, Title: "认错的番", CoverURL: "https://s4.anilist.co/wrong.jpg"}))
	}
	require.NoError(t, env.store.SetProgress(ids[0], store.Progress{Completed: true, Synced: true}))
	require.NoError(t, env.store.SetProgress(ids[1], store.Progress{PositionSec: 300}))

	a := decodeAssoc(t, env.put(t, assocBody(t, env.cluster(t).ClusterKey, "manual", 154587)))

	want := []store.SyncedRecord{{AnilistID: 999, Title: "认错的番", Episodes: []int{1}}}
	assert.Equal(t, want, a.SyncedElsewhere)
	assert.Equal(t, want, env.cluster(t).Association.SyncedElsewhere)
	for _, id := range ids {
		_, ok := env.store.Binding(id)
		assert.False(t, ok, "指向别的作品的匹配要作废，下次播放重新匹配")
	}
	p, _ := env.store.Progress(ids[0])
	assert.True(t, p.Completed)
	assert.False(t, p.Synced, "回写到的是旧作品，重看看完时按新关联再回写")
}

// 上游数据被污染：不在白名单里的封面地址不落盘，超长标题截断。
func TestPutAssociationSanitizesUpstreamFields(t *testing.T) {
	env := newAssocEnv(t, &detailStub{})
	key := env.cluster(t).ClusterKey

	a := decodeAssoc(t, env.put(t, assocBody(t, key, "manual", pollutedWork)))

	assert.Equal(t, maxAssocTitleRunes, utf8.RuneCountInString(a.Title))
	stored, _ := env.store.Association(key)
	assert.Empty(t, stored.CoverURL, "不在图床白名单里的地址不能变成每次打开媒体库都去请求的外联")
	assert.Empty(t, env.cluster(t).Cover)
}

// 标为「不是目录里的作品」：不显示任何作品封面（哪怕自动匹配曾经给过一张）。
func TestPutAssociationNoneHidesMatchedCover(t *testing.T) {
	env := newAssocEnv(t, nil)
	ids := env.fileIDs(t)
	require.NoError(t, env.store.SetBinding(ids[0], store.Binding{AnilistID: 999, Episode: 1, CoverURL: "https://s4.anilist.co/wrong.jpg"}))
	require.NotEmpty(t, env.cluster(t).Cover)

	a := decodeAssoc(t, env.put(t, assocBody(t, env.cluster(t).ClusterKey, "none", 0)))

	require.NotNil(t, a)
	assert.Equal(t, "none", a.Mode)
	c := env.cluster(t)
	require.NotNil(t, c.Association)
	assert.Empty(t, c.Cover)
}

// 回到自动匹配：关联记录删掉，响应里 association 为 null。
func TestPutAssociationAutoClears(t *testing.T) {
	env := newAssocEnv(t, nil)
	key := env.cluster(t).ClusterKey
	decodeAssoc(t, env.put(t, assocBody(t, key, "none", 0)))

	assert.Nil(t, decodeAssoc(t, env.put(t, assocBody(t, key, "auto", 0))))

	_, ok := env.store.Association(key)
	assert.False(t, ok)
	assert.Nil(t, env.cluster(t).Association)
}

// 分组已经不在库里（盘没插、目录删了）：别的改动一律 409，但回到自动匹配可以把残留的关联删掉。
func TestPutAssociationOnVanishedCluster(t *testing.T) {
	env := newAssocEnv(t, nil)
	_, err := env.store.ApplyAssociation("gone", &store.Association{Mode: store.AssociationNone}, nil)
	require.NoError(t, err)

	rec := env.put(t, assocBody(t, "gone", "none", 0))
	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.Contains(t, decode(t, rec).Error, "已经没有这个作品分组")

	assert.Nil(t, decodeAssoc(t, env.put(t, assocBody(t, "gone", "auto", 0))))
	_, ok := env.store.Association("gone")
	assert.False(t, ok)

	assert.Equal(t, http.StatusConflict, env.put(t, assocBody(t, "never-existed", "auto", 0)).Code)
}

// 正在播放分组里的某一集：那一集结束时会按开播时的匹配回写，这时改关联会被它绕过。
func TestPutAssociationRefusesWhilePlayingTheGroup(t *testing.T) {
	env := newAssocEnv(t, nil)
	env.player.status = player.Status{Playing: true, FileID: env.fileIDs(t)[1]}

	rec := env.put(t, assocBody(t, env.cluster(t).ClusterKey, "none", 0))

	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.Contains(t, decode(t, rec).Error, "正在播放")
	_, ok := env.store.Association(env.cluster(t).ClusterKey)
	assert.False(t, ok)

	env.player.status = player.Status{Playing: true, FileID: "别的作品的文件"}
	decodeAssoc(t, env.put(t, assocBody(t, env.cluster(t).ClusterKey, "none", 0)))
}

func TestPutAssociationValidation(t *testing.T) {
	env := newAssocEnv(t, &detailStub{})
	key := env.cluster(t).ClusterKey
	noCatalog := newAssocEnv(t, nil)

	cases := []struct {
		name   string
		env    *assocEnv
		body   string
		status int
		msg    string
	}{
		{"缺分组", env, assocBody(t, "", "none", 0), 400, "缺少作品分组"},
		{"分组过长", env, assocBody(t, strings.Repeat("k", maxClusterKeyBytes+1), "none", 0), 400, "缺少作品分组"},
		{"未知方式", env, assocBody(t, key, "<guess>", 0), 400, "未知的关联方式"},
		{"作品 ID 为 0", env, assocBody(t, key, "manual", 0), 400, "无效的作品 ID"},
		{"作品 ID 越界", env, `{"clusterKey":"` + key + `","mode":"manual","anilistId":4294967296}`, 400, "无效的作品 ID"},
		{"目录不可用", noCatalog, assocBody(t, noCatalog.cluster(t).ClusterKey, "manual", 1), 503, "作品目录暂不可用"},
		{"上游没有这部作品", env, assocBody(t, key, "manual", missingWork), 400, ""},
		{"不是 JSON", env, `{`, 400, "不是合法的 JSON"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := tc.env.put(t, tc.body)
			assert.Equal(t, tc.status, rec.Code, rec.Body.String())
			res := decode(t, rec)
			assert.Contains(t, res.Error, tc.msg)
			assert.NotContains(t, res.Error, "<guess>", "不回显请求里的值")
		})
	}
	_, ok := env.store.Association(key)
	assert.False(t, ok, "失败的请求不能留下关联")
}

// 继续观看：认定过作品就显示那部作品的标题与封面，而不是自动匹配来的（可能正是认错的那部）。
func TestContinueWatchingUsesAssociatedWork(t *testing.T) {
	env := newAssocEnv(t, &detailStub{})
	ids := env.fileIDs(t)
	require.NoError(t, env.store.SetBinding(ids[0], store.Binding{AnilistID: 154587, Episode: 1, Title: "自动匹配的标题", CoverURL: frierenCover}))
	require.NoError(t, env.store.SetProgress(ids[0], store.Progress{PositionSec: 300, DurationSec: 1400, UpdatedAt: 1}))
	key := env.cluster(t).ClusterKey
	decodeAssoc(t, env.put(t, assocBody(t, key, "manual", 154587)))

	cw := env.lib.View().ContinueWatching
	require.Len(t, cw, 1)
	assert.Equal(t, "葬送的芙莉莲", cw[0].Title)
	assert.Equal(t, env.cluster(t).Cover, cw[0].Cover)
}

// 重扫时 clusterKey 变了（解析语料更新 / 目录改名）：按成员文件把关联迁到新键上。
func TestRescanMovesAssociationToTheNewClusterKey(t *testing.T) {
	env := newAssocEnv(t, nil)
	key := env.cluster(t).ClusterKey
	ids := env.fileIDs(t)
	require.NoError(t, env.store.UpdateAssociations(func(all map[string]store.Association) bool {
		all["旧的键"] = store.Association{Mode: store.AssociationManual, AnilistID: 154587, Title: "葬送的芙莉莲", Members: ids}
		return true
	}))

	env.lib.Rescan()

	_, stale := env.store.Association("旧的键")
	assert.False(t, stale)
	moved, ok := env.store.Association(key)
	require.True(t, ok)
	assert.Equal(t, 154587, moved.AnilistID)
	require.NotNil(t, env.cluster(t).Association)
}

// 同一部番的同一批文件出现在两个库目录里：clusterKey 相同、fileId 相同，成员按 id 去重。
func TestClusterFilesDeduplicatesAcrossFolders(t *testing.T) {
	a, b := makeMediaDir(t), makeMediaDir(t)
	stamp := time.Unix(1_790_000_000, 0)
	for _, dir := range []string{a, b} {
		entries, err := filepath.Glob(filepath.Join(dir, "葬送的芙莉莲", "*.mkv"))
		require.NoError(t, err)
		for _, f := range entries {
			require.NoError(t, os.Chtimes(f, stamp, stamp))
		}
	}
	env := newAssocEnv(t, nil, a, b)
	view := env.lib.View()
	require.Len(t, view.Clusters, 2, "每个库目录各自成簇")
	require.Equal(t, view.Clusters[0].ClusterKey, view.Clusters[1].ClusterKey)

	ids, ok := env.lib.ClusterFiles(view.Clusters[0].ClusterKey)
	require.True(t, ok)
	assert.Len(t, ids, 2)
}

// 关联封面走 /art：只有手动认定且记录里有封面才给图；标为 none 的一律 404。
func TestArtHandlerServesAssociationCover(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "state.json"))
	require.NoError(t, err)
	cache, err := artcache.New(t.TempDir())
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(cache.Path(frierenCover), []byte("cover"), 0o600))
	_, err = st.ApplyAssociation("a/b#S1", &store.Association{Mode: store.AssociationManual, CoverURL: frierenCover}, nil)
	require.NoError(t, err)
	_, err = st.ApplyAssociation("none", &store.Association{Mode: store.AssociationNone, CoverURL: frierenCover}, nil)
	require.NoError(t, err)
	h := NewArtHandler(st, cache, NoRemoteArt{})

	serve := func(path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		return rec
	}
	// 能力段剥掉之后，处理器拿到的是已解码的路径（键里的 / 原样保留）；版本参数不影响查找
	rec := serve("/assoc/a/b%23S1?v=1-2")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "cover", rec.Body.String())
	assert.Equal(t, http.StatusNotFound, serve("/assoc/none").Code)
	assert.Equal(t, http.StatusNotFound, serve("/assoc/missing").Code)
}

func TestReconcileAssociations(t *testing.T) {
	manual := func(members ...string) store.Association {
		return store.Association{Mode: store.AssociationManual, AnilistID: 7, Members: members, MemberCount: len(members)}
	}
	cases := []struct {
		name    string
		all     map[string]store.Association
		current map[string][]string
		want    map[string][]string // 期望留下的键 → 成员
		changed bool
	}{
		{
			name:    "键还在、成员没变：不落盘",
			all:     map[string]store.Association{"k": manual("a", "b")},
			current: map[string][]string{"k": {"a", "b"}},
			want:    map[string][]string{"k": {"a", "b"}},
		},
		{
			name:    "键还在、新增了集：成员刷新",
			all:     map[string]store.Association{"k": manual("a", "b")},
			current: map[string][]string{"k": {"a", "b", "c"}},
			want:    map[string][]string{"k": {"a", "b", "c"}},
			changed: true,
		},
		{
			name:    "键变了、两边都过半：迁到新键",
			all:     map[string]store.Association{"old": manual("a", "b", "c")},
			current: map[string][]string{"new": {"a", "b", "x"}, "other": {"c"}},
			want:    map[string][]string{"new": {"a", "b", "x"}},
			changed: true,
		},
		{
			name:    "旧成员这边不过半：原样留着",
			all:     map[string]store.Association{"old": manual("a", "b", "c")},
			current: map[string][]string{"new": {"a", "x"}},
			want:    map[string][]string{"old": {"a", "b", "c"}},
		},
		{
			name:    "两季被合进一个分组：第一季的关联不盖住第二季",
			all:     map[string]store.Association{"s1": manual("s1e1", "s1e2")},
			current: map[string][]string{"merged": {"s1e1", "s1e2", "s2e1", "s2e2"}},
			want:    map[string][]string{"s1": {"s1e1", "s1e2"}},
		},
		{
			name: "上百集的长篇遇上两季合簇：抽样的 64 个全对也不迁",
			all: map[string]store.Association{
				"s1": {Mode: store.AssociationManual, AnilistID: 7, Members: seq("s1e", 64), MemberCount: 100},
			},
			current: map[string][]string{"merged": append(seq("s1e", 100), seq("s2e", 100)...)},
			want:    map[string][]string{"s1": seq("s1e", 64)},
		},
		{
			name:    "一个分组拆成两半：哪边都不过半，不猜",
			all:     map[string]store.Association{"old": manual("a", "b")},
			current: map[string][]string{"x": {"a"}, "y": {"b"}},
			want:    map[string][]string{"old": {"a", "b"}},
		},
		{
			name: "两个旧键抢一个新分组：重合多的先配，与键的排序无关",
			all: map[string]store.Association{
				"a-old": manual("a", "b"),
				"b-old": manual("a", "b", "c"),
			},
			current: map[string][]string{"new": {"a", "b", "c"}},
			want: map[string][]string{
				"a-old": {"a", "b"},
				"new":   {"a", "b", "c"},
			},
			changed: true,
		},
		{
			name:    "目标分组已有自己的关联：不覆盖",
			all:     map[string]store.Association{"old": manual("a", "b"), "new": manual("a", "b")},
			current: map[string][]string{"new": {"a", "b"}},
			want:    map[string][]string{"old": {"a", "b"}, "new": {"a", "b"}},
		},
		{
			name:    "旧键没有记成员：无从判断，原样留着",
			all:     map[string]store.Association{"old": manual()},
			current: map[string][]string{"new": {"a"}},
			want:    map[string][]string{"old": nil},
		},
		{
			name:    "盘没插（这次扫描里根本没有这些文件）：原样留着",
			all:     map[string]store.Association{"old": manual("a", "b")},
			current: map[string][]string{},
			want:    map[string][]string{"old": {"a", "b"}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			changed := reconcileAssociations(tc.all, tc.current)
			assert.Equal(t, tc.changed, changed)
			got := map[string][]string{}
			for k, a := range tc.all {
				got[k] = a.Members
			}
			assert.Equal(t, tc.want, got)
		})
	}
}

// seq 造 n 个 fileId：prefix0, prefix1, …
func seq(prefix string, n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = prefix + strconv.Itoa(i)
	}
	return out
}

// 迁移和刷新都只记前 MaxAssociationMembers 个成员；上千集的长篇换了键照样迁得过去。
func TestReconcileCapsMembers(t *testing.T) {
	files := seq("f", store.MaxAssociationMembers*10)
	all := map[string]store.Association{"old": {Mode: store.AssociationNone, Members: files[:store.MaxAssociationMembers], MemberCount: len(files)}}
	require.True(t, reconcileAssociations(all, map[string][]string{"new": files}))
	assert.Equal(t, files[:store.MaxAssociationMembers], all["new"].Members)
	assert.Equal(t, len(files), all["new"].MemberCount)
}

// 从没认过的分组回到自动匹配：什么都不动（一次重复点击不该清掉它的匹配缓存）。
func TestPutAutoOnNeverAssociatedGroupIsNoop(t *testing.T) {
	env := newAssocEnv(t, nil)
	id := env.fileIDs(t)[0]
	require.NoError(t, env.store.SetBinding(id, store.Binding{AnilistID: 9, Episode: 1}))
	require.NoError(t, env.store.SetProgress(id, store.Progress{Completed: true, Synced: true}))

	assert.Nil(t, decodeAssoc(t, env.put(t, assocBody(t, env.cluster(t).ClusterKey, "auto", 0))))

	_, ok := env.store.Binding(id)
	assert.True(t, ok)
	p, _ := env.store.Progress(id)
	assert.True(t, p.Synced)
}

// 放送表的「在库」：手动认定的作品也算（改关联作废了旧匹配，要等播过才会有新的）。
func TestScheduleCountsAssociatedWorksAsInLibrary(t *testing.T) {
	env := newAssocEnv(t, &detailStub{})
	decodeAssoc(t, env.put(t, assocBody(t, env.cluster(t).ClusterKey, "manual", 1)))

	rec := httptest.NewRecorder()
	env.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/schedule", nil))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var v ScheduleView
	require.NoError(t, json.Unmarshal(decode(t, rec).Data, &v))
	require.NotEmpty(t, v.Airings)
	assert.True(t, v.Airings[0].InLibrary)
}

// 播放时按文件查关联：按所在分组查，不在任何分组里的文件（磁力、在线候选）没有关联。
func TestAssociationForLooksUpByCluster(t *testing.T) {
	env := newAssocEnv(t, nil)
	ids := env.fileIDs(t)
	decodeAssoc(t, env.put(t, assocBody(t, env.cluster(t).ClusterKey, "none", 0)))
	// 成员只记前 64 个，长篇后面的集靠分组索引查：把成员清空也照样查得到
	require.NoError(t, env.store.UpdateAssociations(func(all map[string]store.Association) bool {
		for k, a := range all {
			a.Members = nil
			all[k] = a
		}
		return true
	}))

	for _, id := range ids {
		a, ok := env.lib.AssociationFor(id)
		require.True(t, ok)
		assert.Equal(t, store.AssociationNone, a.Mode)
	}
	_, ok := env.lib.AssociationFor("magnet-file|1|1")
	assert.False(t, ok)
}

// 认定过作品的分组：后台匹配迟到时写回的、指向别处的匹配，不能借它的封面与集标题。
func TestViewIgnoresStrayBindingsForAssociatedCluster(t *testing.T) {
	env := newAssocEnv(t, &detailStub{})
	ids := env.fileIDs(t)
	key := env.cluster(t).ClusterKey
	decodeAssoc(t, env.put(t, assocBody(t, key, "manual", pollutedWork))) // 封面被白名单滤掉
	require.NoError(t, env.store.SetBinding(ids[0], store.Binding{AnilistID: 999, Episode: 1, EpisodeTitle: "别的番的集标题", CoverURL: "https://s4.anilist.co/wrong.jpg", MatchedAt: 1}))
	require.NoError(t, env.store.SetProgress(ids[0], store.Progress{PositionSec: 300, DurationSec: 1400, UpdatedAt: 1}))

	assert.Empty(t, env.cluster(t).Cover)
	cw := env.lib.View().ContinueWatching
	require.Len(t, cw, 1)
	assert.Empty(t, cw[0].Cover)
	assert.Empty(t, cw[0].EpisodeTitle)

	// 与认定作品一致的匹配照用
	require.NoError(t, env.store.SetBinding(ids[0], store.Binding{AnilistID: pollutedWork, Episode: 1, EpisodeTitle: "第1话", CoverURL: frierenCover, MatchedAt: 2}))
	assert.NotEmpty(t, env.cluster(t).Cover)
	assert.Equal(t, "第1话", env.lib.View().ContinueWatching[0].EpisodeTitle)
}

// 重扫把关联迁到新键之后，播放按文件查到的是迁过去的那条（索引与迁移一起换上）。
func TestAssociationForFollowsKeyMigration(t *testing.T) {
	env := newAssocEnv(t, nil)
	ids := env.fileIDs(t)
	require.NoError(t, env.store.UpdateAssociations(func(all map[string]store.Association) bool {
		all["旧的键"] = store.Association{Mode: store.AssociationManual, AnilistID: 154587, Members: ids, MemberCount: len(ids)}
		return true
	}))
	_, ok := env.lib.AssociationFor(ids[0])
	require.False(t, ok, "迁移之前按新键查不到")

	env.lib.Rescan()

	a, ok := env.lib.AssociationFor(ids[0])
	require.True(t, ok)
	assert.Equal(t, 154587, a.AnilistID)
}

// 认定时记下目录里的总集数（只凭关联回写进度时用来挡住超出范围的集号）。
func TestManualAssociationRecordsEpisodeCount(t *testing.T) {
	env := newAssocEnv(t, &episodesStub{episodes: 28})
	key := env.cluster(t).ClusterKey
	decodeAssoc(t, env.put(t, assocBody(t, key, "manual", 154587)))
	stored, _ := env.store.Association(key)
	assert.Equal(t, 28, stored.Episodes)
}

type episodesStub struct {
	detailStub
	episodes int
}

func (f *episodesStub) Detail(ctx context.Context, id int) (animego.CatalogMedia, error) {
	m, err := f.detailStub.Detail(ctx, id)
	m.Episodes = &f.episodes
	return m, err
}

// 没认过的分组带上自动匹配到的作品（簇内匹配最多的那一部）；认定过就不再给。
func TestViewExposesAutoMatchedWorkUntilAssociated(t *testing.T) {
	env := newAssocEnv(t, nil)
	ids := env.fileIDs(t)
	assert.Nil(t, env.cluster(t).Matched, "没播过就没有自动匹配")

	require.NoError(t, env.store.SetBinding(ids[0], store.Binding{AnilistID: 7, Title: "多的那部"}))
	require.NoError(t, env.store.SetBinding(ids[1], store.Binding{AnilistID: 7, Title: "多的那部"}))
	assert.Equal(t, &ViewMatch{AnilistID: 7, Title: "多的那部"}, env.cluster(t).Matched)

	// 认过之后（不论 none 还是 manual）就算又有匹配写进来，也不再给自动匹配
	for _, mode := range []string{"none", "manual"} {
		env := newAssocEnv(t, &detailStub{})
		key := env.cluster(t).ClusterKey
		decodeAssoc(t, env.put(t, assocBody(t, key, mode, 7)))
		require.NoError(t, env.store.SetBinding(env.fileIDs(t)[0], store.Binding{AnilistID: 7, Title: "多的那部"}))
		assert.Nil(t, env.cluster(t).Matched, mode)
	}
}

func TestMatchedWork(t *testing.T) {
	main := func(id string) library.Item { return library.Item{FileID: id, ParsedKind: "main"} }
	ova := library.Item{FileID: "ova", ParsedKind: "ova"}
	cases := []struct {
		name     string
		items    []library.Item
		bindings map[string]store.Binding
		want     *ViewMatch
	}{
		{"多数票", []library.Item{main("a"), main("b"), main("c"), main("d")},
			map[string]store.Binding{"a": {AnilistID: 9, Title: "少"}, "b": {AnilistID: 5, Title: "多"}, "c": {AnilistID: 5}},
			&ViewMatch{AnilistID: 5, Title: "多"}},
		{"平票取 ID 小的", []library.Item{main("a"), main("b")},
			map[string]store.Binding{"a": {AnilistID: 9}, "b": {AnilistID: 5}}, &ViewMatch{AnilistID: 5}},
		{"OVA 不与正片同票", []library.Item{main("a"), ova},
			map[string]store.Binding{"a": {AnilistID: 100, Title: "正片"}, "ova": {AnilistID: 50, Title: "OVA"}},
			&ViewMatch{AnilistID: 100, Title: "正片"}},
		{"只有 OVA 匹配过：照样给建议", []library.Item{main("a"), ova},
			map[string]store.Binding{"ova": {AnilistID: 50, Title: "OVA"}}, &ViewMatch{AnilistID: 50, Title: "OVA"}},
		{"没有匹配", []library.Item{main("a")}, map[string]store.Binding{"a": {AnilistID: 0}}, nil},
		{"空分组", nil, nil, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, matchedWork(tc.items, tc.bindings))
		})
	}
}

// 改完旧作品的进度后，去掉那一条记录。
func TestDismissSyncedElsewhereEndpoint(t *testing.T) {
	env := newAssocEnv(t, &detailStub{})
	ids := env.fileIDs(t)
	require.NoError(t, env.store.SetBinding(ids[0], store.Binding{AnilistID: 999, Episode: 1, Title: "认错的番"}))
	require.NoError(t, env.store.SetProgress(ids[0], store.Progress{Completed: true, Synced: true}))
	key := env.cluster(t).ClusterKey
	require.NotEmpty(t, decodeAssoc(t, env.put(t, assocBody(t, key, "manual", 154587))).SyncedElsewhere)

	dismiss := func(body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		env.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/library/association/dismiss-synced", strings.NewReader(body)))
		return rec
	}
	a := decodeAssoc(t, dismiss(`{"clusterKey":`+string(mustJSON(t, key))+`,"anilistId":999}`))
	assert.Empty(t, a.SyncedElsewhere)
	assert.Equal(t, 154587, a.AnilistID)

	// 不在清单里的作品：照常返回，关联不变
	a = decodeAssoc(t, dismiss(`{"clusterKey":`+string(mustJSON(t, key))+`,"anilistId":12345}`))
	assert.Equal(t, 154587, a.AnilistID)

	rec := dismiss(`{"clusterKey":"gone","anilistId":999}`)
	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.Contains(t, decode(t, rec).Error, "已经没有作品关联")
	for _, body := range []string{
		`{"clusterKey":"gone","anilistId":0}`,
		`{"clusterKey":"","anilistId":1}`,
		`{"clusterKey":"` + strings.Repeat("k", maxClusterKeyBytes+1) + `","anilistId":1}`,
		`{"clusterKey":"k","anilistId":4294967296}`,
		`{`,
	} {
		assert.Equal(t, http.StatusBadRequest, dismiss(body).Code, body)
	}
}
