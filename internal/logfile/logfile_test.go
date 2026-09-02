package logfile

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPath(t *testing.T) {
	assert.Equal(t, filepath.Join("/cfg", "logs", "nagare.log"), Path("/cfg"))
}

// 首次打开：目录与文件被创建，权限收紧，可追加写。
func TestOpen_CreatesWithTightPermissions(t *testing.T) {
	dir := t.TempDir()
	f, err := Open(dir)
	require.NoError(t, err)
	_, err = f.WriteString("第一行\n")
	require.NoError(t, err)
	require.NoError(t, f.Close())

	data, err := os.ReadFile(Path(dir))
	require.NoError(t, err)
	assert.Equal(t, "第一行\n", string(data))

	if runtime.GOOS == "windows" {
		return // Windows 没有 POSIX 权限位
	}
	dirInfo, err := os.Stat(filepath.Dir(Path(dir)))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o700), dirInfo.Mode().Perm())
	fileInfo, err := os.Stat(Path(dir))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), fileInfo.Mode().Perm())
}

// 再次打开是追加，不清空旧内容。
func TestOpen_Appends(t *testing.T) {
	dir := t.TempDir()
	for _, line := range []string{"a\n", "b\n"} {
		f, err := Open(dir)
		require.NoError(t, err)
		_, err = f.WriteString(line)
		require.NoError(t, err)
		require.NoError(t, f.Close())
	}
	data, err := os.ReadFile(Path(dir))
	require.NoError(t, err)
	assert.Equal(t, "a\nb\n", string(data))
}

// 超过 2MB：旧文件整体改名为 .1，新文件从空开始；.1 里的更旧一代被覆盖。
func TestOpen_RotatesWhenLarge(t *testing.T) {
	dir := t.TempDir()
	path := Path(dir)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path+rotatedSuffix, []byte("ancient"), 0o600))
	big := bytes.Repeat([]byte("x"), maxSize+1)
	require.NoError(t, os.WriteFile(path, big, 0o600))

	f, err := Open(dir)
	require.NoError(t, err)
	_, err = f.WriteString("fresh\n")
	require.NoError(t, err)
	require.NoError(t, f.Close())

	rotated, err := os.ReadFile(path + rotatedSuffix)
	require.NoError(t, err)
	assert.Equal(t, big, rotated, "上一份日志应完整搬到 .1，更旧的一代被覆盖")
	current, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "fresh\n", string(current))
}

// 恰好等于阈值不轮转。
func TestOpen_NoRotationAtThreshold(t *testing.T) {
	dir := t.TempDir()
	path := Path(dir)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	exact := bytes.Repeat([]byte("y"), maxSize)
	require.NoError(t, os.WriteFile(path, exact, 0o600))

	f, err := Open(dir)
	require.NoError(t, err)
	require.NoError(t, f.Close())

	_, err = os.Stat(path + rotatedSuffix)
	assert.ErrorIs(t, err, os.ErrNotExist)
	current, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Len(t, current, maxSize)
}

// 目录创建失败要报错，不能静默退回终端。
func TestOpen_DirCreationFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows 下用文件占位目录名的行为不同")
	}
	dir := t.TempDir()
	// 用一个普通文件占住 logs 这个名字，MkdirAll 必然失败。
	require.NoError(t, os.WriteFile(filepath.Join(dir, dirName), []byte("x"), 0o600))
	_, err := Open(dir)
	require.ErrorContains(t, err, "创建日志目录")
}
