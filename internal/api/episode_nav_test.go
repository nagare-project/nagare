package api

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	errs "github.com/nagare-project/nagare/internal/errors"
	"github.com/nagare-project/nagare/internal/library"
	"github.com/nagare-project/nagare/internal/player"
	"github.com/nagare-project/nagare/internal/store"
	"github.com/nagare-project/nagare/internal/torrentstream"
)

// writeVideos 在 dir 下写几个过得了扫描下限（1MB）的假视频，返回目录。
func writeVideos(t *testing.T, dir string, names ...string) string {
	t.Helper()
	require.NoError(t, os.MkdirAll(dir, 0o755))
	for _, name := range names {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), make([]byte, 1<<20), 0o644))
	}
	return dir
}

// navLibrary 扫描 roots 建一个媒体库。
func navLibrary(t *testing.T, roots ...string) *LibraryService {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "state.json"))
	require.NoError(t, err)
	lib := NewLibraryService(st)
	for _, root := range roots {
		_, _, err := lib.AddFolder(root)
		require.NoError(t, err)
	}
	return lib
}

// itemByName 按文件名找条目（找不到就失败）。
func itemByName(t *testing.T, lib *LibraryService, name string) library.Item {
	t.Helper()
	lib.mu.RLock()
	defer lib.mu.RUnlock()
	for _, it := range lib.items {
		if it.FileName == name {
			return it
		}
	}
	t.Fatalf("媒体库里没有 %s", name)
	return library.Item{}
}

func episodeOf(it *library.Item) int {
	if it == nil || it.Episode == nil {
		return 0
	}
	return *it.Episode
}

// 同一部番分在两块盘上（同一个 clusterKey）：邻居跨库目录找；外挂字幕跟着邻居走。
func TestEpisodeNeighborAcrossRootsWithSidecar(t *testing.T) {
	rootA := t.TempDir()
	rootB := t.TempDir()
	writeVideos(t, filepath.Join(rootA, "葬送的芙莉莲"), "[Sub] Frieren - 01 [1080p].mkv", "[Sub] Frieren - 02 [1080p].mkv")
	showB := writeVideos(t, filepath.Join(rootB, "葬送的芙莉莲"), "[Sub] Frieren - 03 [1080p].mkv")
	sub := filepath.Join(showB, "[Sub] Frieren - 03 [1080p].ass")
	require.NoError(t, os.WriteFile(sub, []byte("x"), 0o644))
	lib := navLibrary(t, rootA, rootB)

	ep1 := itemByName(t, lib, "[Sub] Frieren - 01 [1080p].mkv")
	ep2 := itemByName(t, lib, "[Sub] Frieren - 02 [1080p].mkv")
	ep3 := itemByName(t, lib, "[Sub] Frieren - 03 [1080p].mkv")
	lib.mu.RLock()
	require.Equal(t, lib.fileCluster[ep1.FileID], lib.fileCluster[ep3.FileID], "两块盘上的同一部番要是同一个作品分组")
	lib.mu.RUnlock()

	next, subPath, ok := lib.EpisodeNeighbor(ep2.FileID, 1)
	require.True(t, ok)
	assert.Equal(t, ep3.FileID, next.FileID)
	assert.Equal(t, sub, subPath, "邻居配对的外挂字幕一起给出")

	prev, subPath, ok := lib.EpisodeNeighbor(ep2.FileID, -1)
	require.True(t, ok)
	assert.Equal(t, ep1.FileID, prev.FileID)
	assert.Empty(t, subPath)

	_, _, ok = lib.EpisodeNeighbor(ep3.FileID, 1)
	assert.False(t, ok, "最后一集没有下一集")

	p, n := lib.EpisodeNeighbors(ep1.FileID)
	assert.Nil(t, p, "第一集没有上一集")
	assert.Equal(t, 2, episodeOf(n))
}

// 库根目录下的散文件也按同一部番找邻居。
func TestEpisodeNeighborRootLevelLooseFiles(t *testing.T) {
	root := writeVideos(t, t.TempDir(), "[Sub] Frieren - 01 [1080p].mkv", "[Sub] Frieren - 02 [1080p].mkv")
	lib := navLibrary(t, root)
	ep1 := itemByName(t, lib, "[Sub] Frieren - 01 [1080p].mkv")
	ep2 := itemByName(t, lib, "[Sub] Frieren - 02 [1080p].mkv")

	next, _, ok := lib.EpisodeNeighbor(ep1.FileID, 1)
	require.True(t, ok)
	assert.Equal(t, ep2.FileID, next.FileID)
}

// 同一个季度文件夹里放了两部番（一个作品分组）：下一集只在同一部番里找，不会从《芙莉莲》跳到《胆大党》。
func TestEpisodeNeighborMixedFolderStaysInShow(t *testing.T) {
	root := t.TempDir()
	writeVideos(t, filepath.Join(root, "2026-01"),
		"[Sub] Frieren - 01 [1080p].mkv", "[Sub] Frieren - 02 [1080p].mkv", "[Sub] Frieren - 03 [1080p].mkv",
		"[Sub] Dandadan - 01 [1080p].mkv", "[Sub] Dandadan - 02 [1080p].mkv")
	lib := navLibrary(t, root)
	f1 := itemByName(t, lib, "[Sub] Frieren - 01 [1080p].mkv")
	f2 := itemByName(t, lib, "[Sub] Frieren - 02 [1080p].mkv")
	f3 := itemByName(t, lib, "[Sub] Frieren - 03 [1080p].mkv")
	d1 := itemByName(t, lib, "[Sub] Dandadan - 01 [1080p].mkv")
	d2 := itemByName(t, lib, "[Sub] Dandadan - 02 [1080p].mkv")
	lib.mu.RLock()
	require.Equal(t, lib.fileCluster[f1.FileID], lib.fileCluster[d2.FileID], "前提：混放的两部番在同一个作品分组里")
	lib.mu.RUnlock()

	next, _, ok := lib.EpisodeNeighbor(f1.FileID, 1)
	require.True(t, ok)
	assert.Equal(t, f2.FileID, next.FileID, "只看集号会挑《胆大党》第 2 集")
	next, _, ok = lib.EpisodeNeighbor(d1.FileID, 1)
	require.True(t, ok)
	assert.Equal(t, d2.FileID, next.FileID)
	_, _, ok = lib.EpisodeNeighbor(d2.FileID, 1)
	assert.False(t, ok, "《胆大党》第 2 集后面不能接《芙莉莲》第 3 集")

	p, n := lib.EpisodeNeighbors(f3.FileID)
	require.NotNil(t, p)
	assert.Equal(t, f2.FileID, p.FileID)
	assert.Nil(t, n)
	p, n = lib.EpisodeNeighbors(d2.FileID)
	require.NotNil(t, p)
	assert.Equal(t, d1.FileID, p.FileID)
	assert.Nil(t, n)
}

// 不在媒体库里的 fileId：没有邻居。
func TestEpisodeNeighborUnknownFile(t *testing.T) {
	lib := navLibrary(t, writeVideos(t, t.TempDir(), "[Sub] Frieren - 01 [1080p].mkv"))
	_, _, ok := lib.EpisodeNeighbor("nope|1|1", 1)
	assert.False(t, ok)
	p, n := lib.EpisodeNeighbors("nope|1|1")
	assert.Nil(t, p)
	assert.Nil(t, n)
}

func TestEpisodeNavigatorLocal(t *testing.T) {
	root := t.TempDir()
	show := writeVideos(t, filepath.Join(root, "葬送的芙莉莲"), "[Sub] Frieren - 01 [1080p].mkv", "[Sub] Frieren - 02 [1080p].mkv", "[Sub] Frieren - 03 [1080p].mkv")
	lib := navLibrary(t, root)
	nav := NewEpisodeNavigator(lib)
	ep2 := player.NewLocalSource(itemByName(t, lib, "[Sub] Frieren - 02 [1080p].mkv"))

	nb := nav.Neighbors(ep2)
	require.NotNil(t, nb.Prev)
	require.NotNil(t, nb.Next)
	assert.Equal(t, player.Neighbor{Episode: 1, Label: "第 1 集"}, *nb.Prev)
	assert.Equal(t, player.Neighbor{Episode: 3, Label: "第 3 集"}, *nb.Next)

	opened, err := nav.Open(context.Background(), ep2, player.DirectionNext)
	require.NoError(t, err)
	item, ok := player.LocalItem(opened.Source)
	require.True(t, ok, "打开的邻居是本地文件源")
	assert.Equal(t, filepath.Join(show, "[Sub] Frieren - 03 [1080p].mkv"), item.AbsPath)
	assert.Equal(t, filepath.Join(show, "[Sub] Frieren - 03 [1080p].mkv"), opened.Source.MPVPath())

	// 文件被删了（还没重扫）：分类错误，说清楚是哪个文件、能做什么
	require.NoError(t, os.Remove(item.AbsPath))
	_, err = nav.Open(context.Background(), ep2, player.DirectionNext)
	var ce *errs.E
	require.ErrorAs(t, err, &ce)
	assert.Equal(t, errs.CategoryFS, ce.Category)
	assert.Contains(t, ce.UserMsg, "[Sub] Frieren - 03 [1080p].mkv")
	assert.Equal(t, "重新扫描媒体库后再试", ce.Recovery)

	// 重扫之后它就不是邻居了：Open 重新找一遍，报「没有下一集了」
	lib.Rescan()
	assert.Nil(t, nav.Neighbors(ep2).Next)
	_, err = nav.Open(context.Background(), ep2, player.DirectionNext)
	require.ErrorAs(t, err, &ce)
	assert.Equal(t, "没有下一集了", ce.UserMsg)
	assert.NotEmpty(t, ce.Recovery)
}

// ctx 已经取消：不等文件系统，直接返回。
func TestEpisodeNavigatorOpenHonoursCancel(t *testing.T) {
	root := writeVideos(t, filepath.Join(t.TempDir(), "show"), "[Sub] Frieren - 01 [1080p].mkv", "[Sub] Frieren - 02 [1080p].mkv")
	lib := navLibrary(t, filepath.Dir(root))
	nav := NewEpisodeNavigator(lib)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := nav.Open(ctx, player.NewLocalSource(itemByName(t, lib, "[Sub] Frieren - 01 [1080p].mkv")), player.DirectionNext)
	// stat 可能比取消先回来（本地盘很快）：两种结果都对，只是不能卡住
	if err != nil {
		var ce *errs.E
		require.ErrorAs(t, err, &ce)
		assert.Equal(t, "player.nav", ce.Op)
	}
}

// 不在媒体库里的本地文件、在线候选：没有邻居，Open 给分类错误。
func TestEpisodeNavigatorOtherSources(t *testing.T) {
	lib := navLibrary(t, writeVideos(t, filepath.Join(t.TempDir(), "show"), "[Sub] Frieren - 01 [1080p].mkv"))
	nav := NewEpisodeNavigator(lib)
	ep := 1
	sources := map[string]player.MediaSource{
		"不在媒体库里的本地文件": player.NewLocalSource(library.Item{FileID: "x|1|1", FileName: "x.mkv", Episode: &ep, ParsedKind: "main"}),
		"在线候选":        player.NewRemoteSource(player.RemoteSourceOptions{SourceID: "s", CandidateID: "c", URL: "https://example.test/a.m3u8", Title: "t", Episode: 1}),
	}
	for name, src := range sources {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, player.Neighbors{}, nav.Neighbors(src))
			_, err := nav.Open(context.Background(), src, player.DirectionNext)
			var ce *errs.E
			require.ErrorAs(t, err, &ce)
			assert.Equal(t, "player.nav", ce.Op)
			assert.NotEmpty(t, ce.UserMsg)
			assert.NotEmpty(t, ce.Recovery)
		})
	}
}

// 磁力来源交给引擎：会话已经不在（零值 Source）时没有邻居，Open 原样交出引擎的分类错误，
// 屏幕提示与错误卡片直接用它的说法与恢复动作。有会话时的邻居与换集见 torrentstream 的集成测试。
func TestEpisodeNavigatorTorrentDelegatesToEngine(t *testing.T) {
	nav := NewEpisodeNavigator(navLibrary(t))
	src := &torrentstream.Source{}
	assert.Equal(t, player.Neighbors{}, nav.Neighbors(src))
	for dir, word := range map[player.Direction]string{player.DirectionNext: "下一集", player.DirectionPrev: "上一集"} {
		opened, err := nav.Open(context.Background(), src, dir)
		assert.Nil(t, opened.Source)
		var ce *errs.E
		require.ErrorAs(t, err, &ce)
		assert.Equal(t, "torrentstream.switch", ce.Op)
		assert.Equal(t, errs.CategoryTorrent, ce.Category)
		assert.Contains(t, ce.UserMsg, word)
		assert.NotEmpty(t, ce.Recovery)
	}
}

// 种子里的候选翻成界面上的邻居：「第 N 集」；没有集号的不出按钮。
func TestChoiceNeighbor(t *testing.T) {
	ep := 7
	assert.Equal(t, &player.Neighbor{Episode: 7, Label: "第 7 集"}, choiceNeighbor(&torrentstream.FileChoice{Index: 3, Episode: &ep}))
	assert.Nil(t, choiceNeighbor(&torrentstream.FileChoice{Index: 3}))
	assert.Nil(t, choiceNeighbor(nil))
}
