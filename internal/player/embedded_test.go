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

// embeddedManager 造一个 Manager：没有命令行 mpv、Launch 由 launch 接管（记下参数）。
func embeddedManager(t *testing.T, embedded bool, launch Launcher) (*Manager, string) {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "state.json"))
	require.NoError(t, err)
	uiDir := filepath.Join(dir, "mpv-config")
	m := New(Options{
		Store:       st,
		MPV:         noMPV(),
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
