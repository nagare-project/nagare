package sourceplugin

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeBundle(t *testing.T, dir, binaryName string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "repo", "schema"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "repo", "schema", "source-v1.schema.json"), []byte("{}"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, binaryName), []byte("#!/bin/sh\n"), 0o755))
}

func TestDetectBundledPrefersArchSpecificEngineNextToExecutable(t *testing.T) {
	exeDir := t.TempDir()
	suffix := ""
	if runtime.GOOS == "windows" {
		suffix = ".exe"
	}
	writeBundle(t, filepath.Join(exeDir, "nagare-source"), "nagare-source-"+runtime.GOARCH+suffix)
	got, ok := DetectBundled(exeDir)
	require.True(t, ok)
	assert.Equal(t, filepath.Join(exeDir, "nagare-source", "nagare-source-"+runtime.GOARCH+suffix), got.Executable)
	assert.Equal(t, filepath.Join(exeDir, "nagare-source", "repo"), got.Root)
}

func TestDetectBundledFallsBackToLinuxPackageLayout(t *testing.T) {
	prefix := t.TempDir()
	exeDir := filepath.Join(prefix, "bin")
	require.NoError(t, os.MkdirAll(exeDir, 0o755))
	writeBundle(t, filepath.Join(prefix, "lib", "nagare", "nagare-source"), "nagare-source")
	got, ok := DetectBundled(exeDir)
	require.True(t, ok)
	assert.Equal(t, filepath.Join(prefix, "lib", "nagare", "nagare-source", "repo"), got.Root)
}

func TestDetectBundledIgnoresPlaceholderAndMissingRepo(t *testing.T) {
	exeDir := t.TempDir()
	// lock 未钉版本时 fetch 脚本只留一个 README.txt
	require.NoError(t, os.MkdirAll(filepath.Join(exeDir, "nagare-source"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(exeDir, "nagare-source", "README.txt"), []byte("no plugin"), 0o644))
	_, ok := DetectBundled(exeDir)
	assert.False(t, ok)
	// 有引擎没 repo 也不算
	require.NoError(t, os.WriteFile(filepath.Join(exeDir, "nagare-source", "nagare-source"), []byte("x"), 0o755))
	_, ok = DetectBundled(exeDir)
	assert.False(t, ok)
	_, ok = DetectBundled("")
	assert.False(t, ok)
}
