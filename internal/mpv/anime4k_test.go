package mpv

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAnime4KPresetsWriteShadersInOfficialOrder(t *testing.T) {
	dir := t.TempDir()
	for preset, want := range map[string][]string{
		Anime4KFast: {"Anime4K_Clamp_Highlights.glsl", "Anime4K_Restore_CNN_M.glsl", "Anime4K_Upscale_CNN_x2_M.glsl", "Anime4K_AutoDownscalePre_x2.glsl", "Anime4K_AutoDownscalePre_x4.glsl", "Anime4K_Upscale_CNN_x2_S.glsl"},
		Anime4KHQ:   {"Anime4K_Clamp_Highlights.glsl", "Anime4K_Restore_CNN_VL.glsl", "Anime4K_Upscale_CNN_x2_VL.glsl", "Anime4K_AutoDownscalePre_x2.glsl", "Anime4K_AutoDownscalePre_x4.glsl", "Anime4K_Upscale_CNN_x2_M.glsl"},
	} {
		paths, err := Anime4KShaders(dir, preset)
		require.NoError(t, err)
		require.Len(t, paths, len(want))
		for i, path := range paths {
			assert.True(t, filepath.IsAbs(path))
			assert.Equal(t, want[i], filepath.Base(path), "%s 的第 %d 个着色器", preset, i)
			got, err := os.ReadFile(path)
			require.NoError(t, err)
			embedded, err := anime4kFiles.ReadFile("anime4k/" + want[i])
			require.NoError(t, err)
			assert.Equal(t, embedded, got)
			// 上游逐个文件声明许可：AutoDownscalePre 是 Unlicense（公有领域），其余是 MIT（见 THIRD_PARTY_NOTICES.md）
			head := string(got[:200])
			assert.True(t, strings.Contains(head, "MIT License") || strings.Contains(head, "released into the public domain"),
				"%s 要带着上游的许可证头", want[i])
		}
	}
}

func TestAnime4KShadersRewritesOnlyWhenChanged(t *testing.T) {
	dir := t.TempDir()
	paths, err := Anime4KShaders(dir, Anime4KFast)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(paths[0], []byte("被改坏了"), 0o600))
	again, err := Anime4KShaders(dir, Anime4KFast)
	require.NoError(t, err)
	assert.Equal(t, paths, again)
	got, err := os.ReadFile(paths[0])
	require.NoError(t, err)
	assert.NotEqual(t, "被改坏了", string(got), "内容不一致就重写")
	entries, err := os.ReadDir(filepath.Join(dir, "anime4k"))
	require.NoError(t, err)
	for _, entry := range entries {
		assert.NotContains(t, entry.Name(), ".tmp", "不留临时文件")
	}
}

func TestAnime4KOffAndUnknown(t *testing.T) {
	paths, err := Anime4KShaders(t.TempDir(), Anime4KOff)
	require.NoError(t, err)
	assert.Nil(t, paths)
	_, err = Anime4KShaders(t.TempDir(), "ultra")
	require.Error(t, err)
	assert.True(t, ValidAnime4K(""))
	assert.True(t, ValidAnime4K("fast"))
	assert.True(t, ValidAnime4K("hq"))
	assert.False(t, ValidAnime4K("ultra"))
}

func TestBuildArgsAppendsShadersOneByOne(t *testing.T) {
	args := buildArgs(LaunchOptions{MediaPath: "/v/ep01.mkv", Shaders: []string{"/a;b/1.glsl", "/c:d/2.glsl"}}, "/tmp/nagare.sock")
	first, second := -1, -1
	for i, arg := range args {
		switch arg {
		case "--glsl-shaders-append=/a;b/1.glsl":
			first = i
		case "--glsl-shaders-append=/c:d/2.glsl":
			second = i
		}
	}
	require.NotEqual(t, -1, first, "路径里带 ; 也原样作为一项传过去")
	require.NotEqual(t, -1, second, "路径里带 : 也原样作为一项传过去")
	assert.Less(t, first, second, "按顺序加载")
	assert.Equal(t, []string{"--", "/v/ep01.mkv"}, args[len(args)-2:])

	for _, arg := range buildArgs(LaunchOptions{MediaPath: "/v/ep01.mkv"}, "/tmp/nagare.sock") {
		assert.NotContains(t, arg, "glsl-shaders", "关掉时一个着色器参数都不带")
	}
}
