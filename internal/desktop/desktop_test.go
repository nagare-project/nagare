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

// fakeCLI 造一个假的命令行 mpv（普通文件），并把「nagare 自己」换成另一个文件。
func fakeCLI(t *testing.T) (cli, self string) {
	t.Helper()
	dir := t.TempDir()
	cli, self = filepath.Join(dir, "mpv"), filepath.Join(dir, "nagare")
	require.NoError(t, os.WriteFile(cli, []byte("#!/bin/sh\n"), 0o755))
	require.NoError(t, os.WriteFile(self, []byte("#!/bin/sh\n"), 0o755))
	// t.TempDir 在 macOS 上是 /var → /private/var 的符号链接，selfExecutable 照真实实现解析过
	resolved, err := filepath.EvalSymlinks(self)
	require.NoError(t, err)
	orig := selfExecutable
	selfExecutable = func() (string, error) { return resolved, nil }
	t.Cleanup(func() { selfExecutable = orig })
	return cli, self
}

// 进程内 libmpv 的选项：画面走 render API；不自动加载配置目录里的脚本，只点名加载 nagare 的界面；
// 没有命令行 mpv 时不加载 thumbfast（它会把 nagare 自己当 mpv 拉起来）；命令行那份照旧在（IPC、配置目录、标题……）。
func TestLibmpvSpecForEmbeddedWindow(t *testing.T) {
	fakeCLI(t)
	cfg := filepath.Join("/cfg", "runtime", "mpv-config")
	spec, err := libmpvSpec(mpv.LaunchOptions{
		MediaPath: "/v/ep01.mkv",
		ConfigDir: cfg,
		Title:     "第 1 集",
	}, "/run/mpv.sock", 2)
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
	_, ok = findOpt(spec.Init, "script-opts")
	assert.False(t, ok, "不加载 thumbfast 时也不传它的设置")
}

// 有命令行 mpv（.app 内置的 Contents/MacOS/mpv）：点名加载 thumbfast，把那个 mpv 交给它，
// 缩略图最大尺寸按屏幕缩放放大（render API 下 thumbfast 读不到 display-hidpi-scale）。
func TestLibmpvSpecLoadsThumbfastWithCLIMPV(t *testing.T) {
	cli, _ := fakeCLI(t)
	cfg := filepath.Join("/cfg", "runtime", "mpv-config")
	for _, tc := range []struct {
		hidpi float64
		size  string
	}{{1, "192"}, {2, "384"}} {
		spec, err := libmpvSpec(mpv.LaunchOptions{
			MediaPath:  "/v/ep01.mkv",
			ConfigDir:  cfg,
			ScriptOpts: []string{"thumbfast-mpv_path=" + cli},
		}, "/run/mpv.sock", tc.hidpi)
		require.NoError(t, err)

		scripts, ok := findOpt(spec.Init, "scripts")
		require.True(t, ok)
		assert.Equal(t, []string{filepath.Join(cfg, "scripts", "nagare"), filepath.Join(cfg, "scripts", "thumbfast.lua")}, scripts.List)
		load, _ := findOpt(spec.Init, "load-scripts")
		assert.Equal(t, "no", load.String, "仍不自动加载配置目录里的脚本")
		so, ok := findOpt(spec.Init, "script-opts")
		require.True(t, ok)
		assert.Equal(t, []mpv.KeyValue{
			{Key: "thumbfast-mpv_path", Value: cli},
			{Key: "thumbfast-max_width", Value: tc.size},
			{Key: "thumbfast-max_height", Value: tc.size},
		}, so.Map, "hidpi=%v", tc.hidpi)
	}
}

// 交给 thumbfast 的 mpv 用不了（不存在、相对路径、是目录、就是 nagare 自己）：不加载 thumbfast，
// 也不把那个路径传下去 —— 绝不能让 thumbfast 把 nagare 当 mpv 拉起来。
func TestLibmpvSpecSkipsThumbfastWhenCLIUnusable(t *testing.T) {
	cli, self := fakeCLI(t)
	link := filepath.Join(t.TempDir(), "mpv-link")
	require.NoError(t, os.Symlink(self, link))
	cfg := filepath.Join("/cfg", "runtime", "mpv-config")
	cases := map[string]string{
		"不存在":           filepath.Join(filepath.Dir(cli), "missing"),
		"相对路径":          "mpv",
		"是目录":           filepath.Dir(cli),
		"就是 nagare 自己":  self,
		"指向 nagare 的链接": link,
	}
	for name, path := range cases {
		t.Run(name, func(t *testing.T) {
			spec, err := libmpvSpec(mpv.LaunchOptions{
				MediaPath:  "/v/ep01.mkv",
				ConfigDir:  cfg,
				ScriptOpts: []string{"thumbfast-mpv_path=" + path, "other=1"},
			}, "/run/mpv.sock", 2)
			require.NoError(t, err)
			scripts, _ := findOpt(spec.Init, "scripts")
			assert.Equal(t, []string{filepath.Join(cfg, "scripts", "nagare")}, scripts.List)
			so, ok := findOpt(spec.Init, "script-opts")
			require.True(t, ok)
			assert.Equal(t, []mpv.KeyValue{{Key: "other", Value: "1"}}, so.Map)
		})
	}
}

// 改写 script-opts 不能动调用方的切片。
func TestThumbnailScriptOptsDoesNotMutateInput(t *testing.T) {
	in := []string{"thumbfast-mpv_path=/a/mpv", "x=1"}
	_ = thumbnailScriptOpts(in, false, 2)
	_ = thumbnailScriptOpts(in, true, 2)
	assert.Equal(t, []string{"thumbfast-mpv_path=/a/mpv", "x=1"}, in)
}

// 界面配置没写出来（NoConfig）时没有脚本目录可点名：不加 load-scripts / scripts，退回 mpv 自带的控制条。
func TestLibmpvSpecWithoutUIConfig(t *testing.T) {
	spec, err := libmpvSpec(mpv.LaunchOptions{MediaPath: "/v/ep01.mkv", NoConfig: true}, "/run/mpv.sock", 1)
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
	for _, o := range embeddedOptions("/cfg", true) {
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
