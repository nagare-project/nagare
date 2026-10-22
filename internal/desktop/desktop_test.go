package desktop

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nagare-project/nagare/internal/mpv"
)

func findOpt(opts []mpv.LibmpvOption, name string) (mpv.LibmpvOption, bool) {
	for _, o := range opts {
		if o.Name == name {
			return o, true
		}
	}
	return mpv.LibmpvOption{}, false
}

// 进程内 libmpv 的选项：画面走 render API；不自动加载配置目录里的脚本（thumbfast 会把 nagare 自己
// 当 mpv 拉起来），只点名加载 nagare 的界面；命令行那份照旧在（IPC、配置目录、标题……）。
func TestLibmpvSpecForEmbeddedWindow(t *testing.T) {
	cfg := filepath.Join("/cfg", "runtime", "mpv-config")
	spec, err := libmpvSpec(mpv.LaunchOptions{
		MediaPath:  "/v/ep01.mkv",
		ConfigDir:  cfg,
		Title:      "第 1 集",
		ScriptOpts: []string{"thumbfast-mpv_path=/usr/local/bin/mpv"},
	}, "/run/mpv.sock")
	require.NoError(t, err)

	vo, ok := findOpt(spec.Init, "vo")
	require.True(t, ok)
	assert.Equal(t, "libmpv", vo.String)
	load, ok := findOpt(spec.Init, "load-scripts")
	require.True(t, ok)
	assert.Equal(t, "no", load.String)
	scripts, ok := findOpt(spec.Init, "scripts")
	require.True(t, ok)
	assert.Equal(t, []string{filepath.Join(cfg, "scripts", "nagare")}, scripts.List)

	ipc, ok := findOpt(spec.Init, "input-ipc-server")
	require.True(t, ok)
	assert.Equal(t, "/run/mpv.sock", ipc.String)
	dir, ok := findOpt(spec.Init, "config-dir")
	require.True(t, ok)
	assert.Equal(t, cfg, dir.String)
	idle, _ := findOpt(spec.Init, "idle")
	assert.Equal(t, "once", idle.String, "loadfile 之后播完即退出，会话照旧以 mpv 结束收尾")
	_, ok = findOpt(spec.BeforeLoad, "force-media-title")
	assert.True(t, ok)
	assert.Equal(t, "/v/ep01.mkv", spec.Media)
}

// 界面配置没写出来（NoConfig）时没有脚本目录可点名：不加 load-scripts / scripts，退回 mpv 自带的控制条。
func TestLibmpvSpecWithoutUIConfig(t *testing.T) {
	spec, err := libmpvSpec(mpv.LaunchOptions{MediaPath: "/v/ep01.mkv", NoConfig: true}, "/run/mpv.sock")
	require.NoError(t, err)
	_, ok := findOpt(spec.Init, "scripts")
	assert.False(t, ok)
	_, ok = findOpt(spec.Init, "load-scripts")
	assert.False(t, ok)
	vo, _ := findOpt(spec.Init, "vo")
	assert.Equal(t, "libmpv", vo.String)
}

// libmpv 在 mpv_initialize 里才解析 mpv.conf，会盖过之前设的选项（与命令行相反，见 mpv.LibmpvSpec.Init）。
// 桌面窗口多带的那几条、以及宿主在 C 里先设的 fullscreen / window-maximized，界面配置里都不能出现。
func TestUIConfigDoesNotOverrideEmbeddedOptions(t *testing.T) {
	conf, err := os.ReadFile(filepath.Join("..", "mpv", "ui", "mpv.conf"))
	require.NoError(t, err)
	names := []string{"fullscreen", "window-maximized"}
	for _, o := range embeddedOptions("/cfg") {
		names = append(names, o.Name)
	}
	for _, line := range strings.Split(string(conf), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, _, _ := strings.Cut(line, "=")
		assert.NotContains(t, names, strings.TrimSpace(key), "ui/mpv.conf 会在 libmpv 里盖过宿主设的 %s", key)
	}
}

func TestStubIsUnavailable(t *testing.T) {
	if Available() {
		t.Skip("这个构建带着桌面窗口")
	}
	_, err := Launch(t.Context(), mpv.LaunchOptions{})
	assert.Error(t, err)
}
