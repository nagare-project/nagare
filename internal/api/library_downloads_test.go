package api

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIncludeDownloadsAddsFolderOnceAndRescans(t *testing.T) {
	env := newEnv(t)
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "番"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(root, "番", "01.mkv"), make([]byte, 2<<20), 0o600))

	env.lib.IncludeDownloads(root)
	folders := env.store.Snapshot().Folders
	require.Len(t, folders, 1)
	assert.Equal(t, root, folders[0].Path)
	assert.Equal(t, 1, libraryItems(env.lib))

	require.NoError(t, os.WriteFile(filepath.Join(root, "番", "02.mkv"), make([]byte, 2<<20), 0o600))
	env.lib.IncludeDownloads(root + string(filepath.Separator))
	assert.Len(t, env.store.Snapshot().Folders, 1, "已经在库里就只重扫")
	assert.Equal(t, 2, libraryItems(env.lib), "重扫后新下完的文件进库")
}

func TestIncludeDownloadsInsideExistingFolderOnlyRescans(t *testing.T) {
	env := newEnv(t)
	library := t.TempDir()
	_, _, err := env.lib.AddFolder(library)
	require.NoError(t, err)
	inner := filepath.Join(library, "nagare")
	require.NoError(t, os.MkdirAll(inner, 0o700))
	env.lib.IncludeDownloads(inner)
	assert.Len(t, env.store.Snapshot().Folders, 1, "下载目录在已有库目录里面，不再重复加")
}

func libraryItems(lib *LibraryService) int {
	lib.mu.RLock()
	defer lib.mu.RUnlock()
	return len(lib.items)
}
