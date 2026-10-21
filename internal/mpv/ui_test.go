package mpv

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPrepareUIConfigWritesBundledTree(t *testing.T) {
	dir := t.TempDir()
	got, err := PrepareUIConfig(dir)
	require.NoError(t, err)
	assert.True(t, filepath.IsAbs(got))
	assert.Equal(t, filepath.Join(dir, uiDirName), got)
	for _, rel := range []string{"mpv.conf", "input.conf", "scripts/nagare/main.lua"} {
		content, err := os.ReadFile(filepath.Join(got, filepath.FromSlash(rel)))
		require.NoError(t, err, rel)
		want, err := uiFiles.ReadFile("ui/" + rel)
		require.NoError(t, err)
		assert.Equal(t, want, content, rel)
	}
}

// 升级 nagare 之后，旧版本留下的脚本必须清掉：mpv 会加载 scripts/ 下的每一个文件。
func TestPrepareUIConfigRemovesStaleFilesOnlyInManagedPaths(t *testing.T) {
	dir := t.TempDir()
	root, err := PrepareUIConfig(dir)
	require.NoError(t, err)

	stale := filepath.Join(root, "scripts", "old-skin.lua")
	require.NoError(t, os.WriteFile(stale, []byte("-- old"), 0o600))
	staleDir := filepath.Join(root, "scripts", "gone", "main.lua")
	require.NoError(t, os.MkdirAll(filepath.Dir(staleDir), 0o700))
	require.NoError(t, os.WriteFile(staleDir, []byte("-- old"), 0o600))
	// mpv 自己往配置目录里写的东西不在我们管的路径下，不能误删
	unmanaged := filepath.Join(root, "watch_later", "ABC")
	require.NoError(t, os.MkdirAll(filepath.Dir(unmanaged), 0o700))
	require.NoError(t, os.WriteFile(unmanaged, []byte("x"), 0o600))

	_, err = PrepareUIConfig(dir)
	require.NoError(t, err)
	assert.NoFileExists(t, stale)
	assert.NoDirExists(t, filepath.Dir(staleDir))
	assert.FileExists(t, unmanaged)
	assert.FileExists(t, filepath.Join(root, "scripts", "nagare", "main.lua"))
}

func TestPrepareUIConfigRepairsModifiedFiles(t *testing.T) {
	dir := t.TempDir()
	root, err := PrepareUIConfig(dir)
	require.NoError(t, err)
	conf := filepath.Join(root, "mpv.conf")
	require.NoError(t, os.WriteFile(conf, []byte("osc=yes\n"), 0o600))

	_, err = PrepareUIConfig(dir)
	require.NoError(t, err)
	want, err := uiFiles.ReadFile("ui/mpv.conf")
	require.NoError(t, err)
	got, err := os.ReadFile(conf)
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestBuildArgsUsesConfigDirAndVolume(t *testing.T) {
	volume := 70
	args := buildArgs(LaunchOptions{
		MediaPath: "/media/a.mkv",
		ConfigDir: "/cfg/mpv-config",
		Volume:    &volume,
	}, "/tmp/sock")
	assert.Contains(t, args, "--config-dir=/cfg/mpv-config")
	assert.Contains(t, args, "--volume=70")
	// 媒体路径永远在最后、跟在 "--" 后面
	assert.Equal(t, []string{"--", "/media/a.mkv"}, args[len(args)-2:])

	titled := buildArgs(LaunchOptions{MediaPath: "/media/a.mkv", Title: "葬送的芙莉莲 第 11 集"}, "/tmp/sock")
	assert.Contains(t, titled, "--force-media-title=葬送的芙莉莲 第 11 集", "界面顶栏读 media-title，不强制就显示文件名")

	fallback := buildArgs(LaunchOptions{MediaPath: "/media/a.mkv", NoConfig: true}, "/tmp/sock")
	assert.Contains(t, fallback, "--no-config", "界面配置没准备好时也不能去读用户自己的配置")

	plain := buildArgs(LaunchOptions{MediaPath: "/media/a.mkv"}, "/tmp/sock")
	for _, a := range plain {
		assert.NotContains(t, a, "--config-dir")
		assert.NotContains(t, a, "--no-config")
		assert.NotContains(t, a, "--volume")
	}
}
