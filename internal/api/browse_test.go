package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func browseEnv() *testEnv {
	mux := http.NewServeMux()
	New(Deps{}).Register(mux)
	return &testEnv{mux: mux}
}

func getListing(t *testing.T, path string) (int, dirListing, string) {
	t.Helper()
	rec := browseEnv().do(t, http.MethodGet, "/api/fs/dirs?path="+url.QueryEscape(path), "")
	env := decode(t, rec)
	var listing dirListing
	if rec.Code == http.StatusOK {
		require.NoError(t, json.Unmarshal(env.Data, &listing))
	}
	return rec.Code, listing, env.Error
}

func mkdirs(t *testing.T, root string, names ...string) {
	t.Helper()
	for _, n := range names {
		require.NoError(t, os.MkdirAll(filepath.Join(root, n), 0o755))
	}
}

func TestBrowseListsVisibleSubdirectoriesInNaturalOrder(t *testing.T) {
	root := t.TempDir()
	mkdirs(t, root, "Season 10", "Season 2", ".hidden", "Anime")
	require.NoError(t, os.WriteFile(filepath.Join(root, "ep01.mkv"), []byte("x"), 0o644))
	elsewhere := t.TempDir()
	require.NoError(t, os.Symlink(elsewhere, filepath.Join(root, "Linked")))

	code, listing, _ := getListing(t, root)

	require.Equal(t, http.StatusOK, code)
	names := []string{}
	for _, d := range listing.Dirs {
		names = append(names, d.Name)
		assert.Equal(t, filepath.Join(root, d.Name), d.Path)
	}
	// 文件与隐藏目录不列；指向目录的符号链接要列（媒体库常用软链接）
	assert.Equal(t, []string{"Anime", "Linked", "Season 2", "Season 10"}, names)
	assert.Equal(t, filepath.Dir(root), listing.Parent)
	assert.False(t, listing.Truncated)
}

func TestBrowseRejectsRelativeMissingAndFilePaths(t *testing.T) {
	file := filepath.Join(t.TempDir(), "a.mkv")
	require.NoError(t, os.WriteFile(file, []byte("x"), 0o644))

	cases := []struct {
		name string
		path string
		code int
		msg  string
	}{
		{"相对路径", "Movies/Anime", http.StatusBadRequest, "请输入绝对路径"},
		{"不存在", filepath.Join(t.TempDir(), "nope"), http.StatusNotFound, "文件夹不存在"},
		{"是文件", file, http.StatusBadRequest, "这不是文件夹"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, _, msg := getListing(t, tc.path)
			assert.Equal(t, tc.code, code)
			assert.Contains(t, msg, tc.msg)
		})
	}
}

func TestBrowseFilesystemRootHasNoParent(t *testing.T) {
	root := string(filepath.Separator)
	if v := filepath.VolumeName(os.TempDir()); v != "" {
		root = v + string(filepath.Separator)
	}
	listing, err := listDir(root)
	require.NoError(t, err)
	assert.Equal(t, "", listing.Parent)
}

func TestBrowseTruncatesHugeDirectories(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < maxBrowseEntries+5; i++ {
		require.NoError(t, os.Mkdir(filepath.Join(root, fmt.Sprintf("d%04d", i)), 0o755))
	}
	listing, err := listDir(root)
	require.NoError(t, err)
	assert.Len(t, listing.Dirs, maxBrowseEntries)
	assert.True(t, listing.Truncated)
}

func TestBrowsePlacesListExistingHomeFoldersAndVolumes(t *testing.T) {
	home := t.TempDir()
	mkdirs(t, home, "Movies", "Downloads") // 没有 Desktop：不列
	volumes := t.TempDir()
	mkdirs(t, volumes, "Samsung_T5", ".Trashes")
	// macOS 的系统盘在 /Volumes 里是指回 / 的符号链接，不该列出来
	require.NoError(t, os.Symlink(string(filepath.Separator), filepath.Join(volumes, "Macintosh HD")))
	orig := volumeRoots
	volumeRoots = func(string) []string { return []string{volumes} }
	t.Cleanup(func() { volumeRoots = orig })

	got := placeDirs("darwin", home)

	assert.Equal(t, []dirEntry{
		{Name: "主目录", Path: home},
		{Name: "影片", Path: filepath.Join(home, "Movies")},
		{Name: "下载", Path: filepath.Join(home, "Downloads")},
		{Name: "Samsung_T5", Path: filepath.Join(volumes, "Samsung_T5")},
	}, got)
}

func TestBrowseWithoutPathReturnsPlaces(t *testing.T) {
	code, listing, _ := getListing(t, "")
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, "", listing.Path)
	assert.Equal(t, "", listing.Parent)
	assert.NotNil(t, listing.Dirs)
}
