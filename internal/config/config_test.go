package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 首次运行应生成含随机 token 的配置文件，且权限为 0600。
func TestLoadOrInitCreatesConfigWithToken(t *testing.T) {
	t.Setenv(EnvConfigDir, t.TempDir())

	cfg, err := LoadOrInit()
	require.NoError(t, err)

	assert.Equal(t, DefaultPort, cfg.Port)
	assert.Len(t, cfg.Token, tokenBytes*2, "token 应是 128 位的十六进制表示")

	path, err := Path()
	require.NoError(t, err)
	info, err := os.Stat(path)
	require.NoError(t, err)
	if runtime.GOOS != "windows" {
		assert.Equal(t, os.FileMode(0o600), info.Mode().Perm(), "配置文件含 token，必须 0600")
	}

	// 原子写不应留下临时文件。
	_, err = os.Stat(path + ".tmp")
	assert.True(t, os.IsNotExist(err), "写盘后不应残留 .tmp 文件")
}

// 第二次加载应复用同一份配置，而不是重新生成 token。
func TestLoadOrInitIsIdempotent(t *testing.T) {
	t.Setenv(EnvConfigDir, t.TempDir())

	first, err := LoadOrInit()
	require.NoError(t, err)
	second, err := LoadOrInit()
	require.NoError(t, err)

	assert.Equal(t, first.Token, second.Token, "第二次加载不应重新生成 token")
	assert.Equal(t, first.Port, second.Port)
}

// 手工编辑丢了 token 的配置应被自愈并落盘，而不是带着空 token 启动。
func TestLoadOrInitHealsMissingToken(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(EnvConfigDir, dir)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.toml"), []byte("port = 9000\n"), 0o600))

	cfg, err := LoadOrInit()
	require.NoError(t, err)
	assert.Len(t, cfg.Token, tokenBytes*2, "缺失的 token 应被补齐")
	assert.Equal(t, 9000, cfg.Port, "已有端口应保留")

	reloaded, err := LoadOrInit()
	require.NoError(t, err)
	assert.Equal(t, cfg.Token, reloaded.Token, "自愈结果应已落盘")
}

// 非法端口应回落到默认值。
func TestLoadOrInitHealsInvalidPort(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(EnvConfigDir, dir)
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "config.toml"),
		[]byte("port = -1\ntoken = \"deadbeefdeadbeefdeadbeefdeadbeef\"\n"),
		0o600,
	))

	cfg, err := LoadOrInit()
	require.NoError(t, err)
	assert.Equal(t, DefaultPort, cfg.Port, "非法端口应回落到默认值")
	assert.Equal(t, "deadbeefdeadbeefdeadbeefdeadbeef", cfg.Token, "已有 token 不应被动")
}

// 坏的 TOML 应报错而不是静默用默认值覆盖用户文件。
func TestLoadOrInitRejectsMalformedTOML(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(EnvConfigDir, dir)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.toml"), []byte("port = = broken"), 0o600))

	_, err := LoadOrInit()
	assert.Error(t, err, "解析失败必须显式报错，不能悄悄重建配置")
}

// token 生成应每次不同且长度正确。
func TestNewTokenIsRandom(t *testing.T) {
	a, b := NewToken(), NewToken()
	assert.Len(t, a, tokenBytes*2)
	assert.NotEqual(t, a, b, "两次生成不应相同")
}
