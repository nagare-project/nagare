package selfupdate

// 替换与回滚。这一层的每条失败路径都必须把已经动过的东西撤回去 ——
// 半个装好的版本比装不上要糟糕得多。

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestInstallDirectRollsBackMPV：二进制那一步失败时，先换好的 mpv/ 必须被撤回去。
func TestInstallDirectRollsBackMPV(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, mpvDirName, "mpv.exe"), "OLD MPV", 0o755)

	unpacked := filepath.Join(t.TempDir(), "unpacked")
	writeFile(t, filepath.Join(unpacked, "nagare.exe"), "NEW BINARY", 0o755)
	writeFile(t, filepath.Join(unpacked, mpvDirName, "mpv.exe"), "NEW MPV", 0o755)

	// target 指向一个不存在的子目录，让二进制那一步必定失败。
	inst := install{
		channel: ChannelDirect,
		exePath: filepath.Join(dir, "missing", "nagare.exe"),
		target:  filepath.Join(dir, "missing", "nagare.exe"),
		dir:     dir,
	}
	err := installDirect(inst, unpacked, "windows")
	require.Error(t, err)

	require.Equal(t, "OLD MPV", readFile(t, filepath.Join(dir, mpvDirName, "mpv.exe")),
		"mpv 必须被回滚成旧的")
	require.NoDirExists(t, filepath.Join(dir, mpvDirName+oldSuffix), "回滚后不该留下备份")
}

// TestSwapWithoutExistingLive：目标原本不存在时（用户删过 mpv/），装完再回滚要干净收场。
func TestSwapWithoutExistingLive(t *testing.T) {
	dir := t.TempDir()
	staged := filepath.Join(dir, "staged")
	live := filepath.Join(dir, "live")
	writeFile(t, staged, "NEW", 0o644)

	restore, err := swap(staged, live)
	require.NoError(t, err)
	require.Equal(t, "NEW", readFile(t, live))
	require.NoFileExists(t, live+oldSuffix)

	restore()
	require.NoFileExists(t, live)
	require.NoFileExists(t, live+oldSuffix)
}

// TestSwapOverwritesStaleBackup：上一次留下的 .old 不能挡住这次备份
// （Windows 的 rename 不允许覆盖已存在的目标）。
func TestSwapOverwritesStaleBackup(t *testing.T) {
	dir := t.TempDir()
	staged := filepath.Join(dir, "staged")
	live := filepath.Join(dir, "live")
	writeFile(t, staged, "NEW", 0o644)
	writeFile(t, live, "CURRENT", 0o644)
	writeFile(t, live+oldSuffix, "ANCIENT", 0o644)

	_, err := swap(staged, live)
	require.NoError(t, err)
	require.Equal(t, "NEW", readFile(t, live))
	require.Equal(t, "CURRENT", readFile(t, live+oldSuffix))
}

func TestFindFile(t *testing.T) {
	root := t.TempDir()
	t.Run("在归档根上", func(t *testing.T) {
		writeFile(t, filepath.Join(root, "nagare"), "bin", 0o755)
		got, err := findFile(root, "nagare")
		require.NoError(t, err)
		require.Equal(t, filepath.Join(root, "nagare"), got)
	})
	t.Run("在子目录里", func(t *testing.T) {
		nested := t.TempDir()
		writeFile(t, filepath.Join(nested, "nagare-0.2.0", "nagare"), "bin", 0o755)
		got, err := findFile(nested, "nagare")
		require.NoError(t, err)
		require.Equal(t, filepath.Join(nested, "nagare-0.2.0", "nagare"), got)
	})
	t.Run("根本没有", func(t *testing.T) {
		_, err := findFile(t.TempDir(), "nagare")
		require.Error(t, err)
		require.Contains(t, userFacing(err), "没有 nagare 可执行文件")
	})
}

func TestFindAppBundleAndVerify(t *testing.T) {
	t.Run("刚好一个 .app", func(t *testing.T) {
		root := t.TempDir()
		writeFile(t, filepath.Join(root, "Nagare.app", "Contents", "Info.plist"), "x", 0o644)
		writeFile(t, filepath.Join(root, "Nagare.app", "Contents", "MacOS", "nagare"), "bin", 0o755)
		app, err := findAppBundle(root)
		require.NoError(t, err)
		require.NoError(t, verifyBundle(app))
	})
	t.Run("一个都没有", func(t *testing.T) {
		_, err := findAppBundle(t.TempDir())
		require.Error(t, err)
	})
	t.Run("有两个说不清该装哪个", func(t *testing.T) {
		root := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(root, "A.app"), 0o755))
		require.NoError(t, os.MkdirAll(filepath.Join(root, "B.app"), 0o755))
		_, err := findAppBundle(root)
		require.Error(t, err)
	})
	t.Run("缺 Info.plist", func(t *testing.T) {
		root := t.TempDir()
		writeFile(t, filepath.Join(root, "Nagare.app", "Contents", "MacOS", "nagare"), "bin", 0o755)
		app, err := findAppBundle(root)
		require.NoError(t, err)
		require.Error(t, verifyBundle(app))
	})
	t.Run("缺可执行文件", func(t *testing.T) {
		root := t.TempDir()
		writeFile(t, filepath.Join(root, "Nagare.app", "Contents", "Info.plist"), "x", 0o644)
		app, err := findAppBundle(root)
		require.NoError(t, err)
		err = verifyBundle(app)
		require.Error(t, err)
		require.Contains(t, userFacing(err), "不完整")
	})
}

func TestInstallRefusesUnknownChannel(t *testing.T) {
	u := &Updater{env: defaultEnvironment()}
	err := u.install(install{channel: ChannelUnknown}, t.TempDir())
	require.Error(t, err)
	require.Contains(t, userFacing(err), "无法确定")
}
