package player

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nagare-project/nagare/internal/mpv"
	"github.com/nagare-project/nagare/internal/store"
)

// noMPV 是一台没装命令行 mpv 的机器：探测永远找不到。
func noMPV() *mpv.Runtime {
	return mpv.NewRuntimeWith(func(string) (mpv.Info, error) { return mpv.Info{}, errors.New("未找到 mpv") }, "")
}

// bundledMPV 是一台带着命令行 mpv 的机器（比如 Nagare.app 内置的 Contents/MacOS/mpv）。
func bundledMPV(path string) *mpv.Runtime {
	return mpv.NewRuntimeWith(func(string) (mpv.Info, error) {
		return mpv.Info{Path: path, Version: "0.41.0", Source: mpv.SourceBundled}, nil
	}, "")
}

// embeddedManager 造一个 Manager：没有命令行 mpv、Launch 由 launch 接管（记下参数）。
func embeddedManager(t *testing.T, embedded bool, launch Launcher) (*Manager, string) {
	t.Helper()
	return embeddedManagerWith(t, embedded, noMPV(), launch)
}

// embeddedManagerWith 同 embeddedManager，命令行 mpv 的探测结果由 rt 给。
func embeddedManagerWith(t *testing.T, embedded bool, rt *mpv.Runtime, launch Launcher) (*Manager, string) {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "state.json"))
	require.NoError(t, err)
	uiDir := filepath.Join(dir, "mpv-config")
	m := New(Options{
		Store:       st,
		MPV:         rt,
		RuntimeDir:  dir,
		Launch:      launch,
		EmbeddedMPV: embedded,
		UIConfigDir: func() (string, error) { return uiDir, nil },
	})
	waitResolving(t, m)
	return m, dir
}

var errLaunchFailed = errors.New("模拟：起不来")

// 桌面窗口（进程内 libmpv）：没有命令行 mpv 也照样交给 Launch，不带 mpv 路径，也不把路径交给 thumbfast。
func TestPlayEmbeddedNeedsNoMPVExecutable(t *testing.T) {
	var got mpv.LaunchOptions
	calls := 0
	m, dir := embeddedManager(t, true, func(_ context.Context, o mpv.LaunchOptions) (*mpv.Player, error) {
		calls++
		got = o
		return nil, errLaunchFailed // 只看参数，不真起播
	})
	item := testItem(t, dir, 1)
	_, err := m.Play(context.Background(), NewLocalSource(item), "")
	require.Error(t, err)
	assert.Equal(t, 1, calls, "探测不到命令行 mpv 也要交给桌面窗口的 Launch")
	assert.Empty(t, got.MPVPath)
	assert.Empty(t, got.ScriptOpts, "没有命令行 mpv 可交给 thumbfast")
	assert.Equal(t, filepath.Join(dir, "mpv-config"), got.ConfigDir)
}

// 桌面窗口里有命令行 mpv（.app 内置的、或开发时 Homebrew 的）：播放仍是进程内 libmpv（不带 MPVPath），
// 但把命令行 mpv 交给 thumbfast 生成缩略图。
func TestPlayEmbeddedHandsCLIMPVToThumbfast(t *testing.T) {
	const cli = "/Applications/Nagare.app/Contents/MacOS/mpv"
	var got mpv.LaunchOptions
	m, dir := embeddedManagerWith(t, true, bundledMPV(cli), func(_ context.Context, o mpv.LaunchOptions) (*mpv.Player, error) {
		got = o
		return nil, errLaunchFailed
	})
	_, err := m.Play(context.Background(), NewLocalSource(testItem(t, dir, 1)), "")
	require.Error(t, err)
	assert.Empty(t, got.MPVPath, "桌面窗口不起命令行 mpv")
	assert.Equal(t, []string{"thumbfast-mpv_path=" + cli}, got.ScriptOpts)
}

// 命令行 mpv 的老路：thumbfast 用的就是起播的那个 mpv。
func TestPlayCommandLineHandsSameMPVToThumbfast(t *testing.T) {
	const cli = "/opt/homebrew/bin/mpv"
	var got mpv.LaunchOptions
	m, dir := embeddedManagerWith(t, false, bundledMPV(cli), func(_ context.Context, o mpv.LaunchOptions) (*mpv.Player, error) {
		got = o
		return nil, errLaunchFailed
	})
	_, err := m.Play(context.Background(), NewLocalSource(testItem(t, dir, 1)), "")
	require.Error(t, err)
	assert.Equal(t, cli, got.MPVPath)
	assert.Equal(t, []string{"thumbfast-mpv_path=" + cli}, got.ScriptOpts)
}

// 命令行 mpv 的老路不变：探测不到就不起播，给安装指引。
func TestPlayCommandLineStillRequiresMPV(t *testing.T) {
	calls := 0
	m, dir := embeddedManager(t, false, func(context.Context, mpv.LaunchOptions) (*mpv.Player, error) {
		calls++
		return nil, errLaunchFailed
	})
	item := testItem(t, dir, 1)
	_, err := m.Play(context.Background(), NewLocalSource(item), "")
	require.Error(t, err)
	assert.Zero(t, calls)
	assert.Contains(t, err.Error(), "mpv 不可用")
}

// 起播失败的恢复办法按形态给：桌面窗口里没有可装的 mpv，不能叫人去装。
func TestLaunchRecoveryByMode(t *testing.T) {
	failing := func(context.Context, mpv.LaunchOptions) (*mpv.Player, error) { return nil, errLaunchFailed }

	m, dir := embeddedManager(t, true, failing)
	_, err := m.Play(context.Background(), NewLocalSource(testItem(t, dir, 1)), "")
	require.Error(t, err)
	assert.Equal(t, recoveryEmbedded, m.launchRecovery())
	assert.Contains(t, err.Error(), "启动 mpv 失败")
	assert.NotContains(t, err.Error(), "mpv --version")

	cli, _ := embeddedManager(t, false, failing)
	assert.Equal(t, recoveryCheckMPV, cli.launchRecovery())
}
