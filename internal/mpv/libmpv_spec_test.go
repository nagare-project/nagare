package mpv

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// iOS 变体（R5 / R12）的完整输出，逐项钉死。与桌面窗口的差别：
//   - 不加载任何脚本（load-scripts=no、没有 scripts）、mpv 自带的 osc 关掉 —— 控制条由 SwiftUI 画；
//   - ytdl=no —— 自带的 ytdl_hook 不归 load-scripts 管，遇到 http(s) 地址会起 yt-dlp 子进程；
//   - 不加载 thumbfast、不传 script-opts —— iOS 起不了子进程，缩略图由 Swift 另做；
//   - 预读一律 64MiB 向前 / 16MiB 向后，网络流那份 256MiB 就地换掉；
//   - 着色器照传：Anime4K 默认关（设置的零值），用户在 iOS 上打开画质增强才有。
func TestIOSLibmpvSpecGolden(t *testing.T) {
	const cfg = "/var/mobile/Containers/Data/Application/X/Library/nagare/runtime/mpv-config"
	volume := 70
	ios := []LibmpvOption{
		libStr("vo", "libmpv"),
		libFlag("load-scripts", "no"),
		libFlag("osc", "no"),
		libFlag("ytdl", "no"),
	}
	budget := []LibmpvOption{
		libStr("demuxer-max-bytes", "64MiB"),
		libStr("demuxer-max-back-bytes", "16MiB"),
	}
	head := []LibmpvOption{
		libStr("input-ipc-server", "/run/mpv.sock"),
		libFlag("terminal", "no"),
		libStr("keep-open", "no"),
	}
	join := func(parts ...[]LibmpvOption) []LibmpvOption {
		var out []LibmpvOption
		for _, p := range parts {
			out = append(out, p...)
		}
		return out
	}

	cases := []struct {
		name string
		opts LaunchOptions
		want LibmpvSpec
	}{
		{
			name: "本地文件 + 界面配置目录（mpv.conf 照读），不加载任何脚本，Anime4K 默认关",
			opts: LaunchOptions{
				MediaPath:  "/v/ep01.mkv",
				ConfigDir:  cfg,
				Title:      "第 1 集",
				SubPath:    "/run/danmaku.ass",
				StartAt:    62.5,
				Volume:     &volume,
				ScriptOpts: []string{"thumbfast-mpv_path=/usr/bin/mpv"},
			},
			want: LibmpvSpec{
				Init: join(
					[]LibmpvOption{libFlag("config", "yes"), libFlag("input-default-bindings", "yes")},
					head,
					[]LibmpvOption{libStr("config-dir", cfg), libStr("volume", "70")},
					ios, budget,
					[]LibmpvOption{libStr("idle", "once")},
				),
				BeforeLoad: []LibmpvOption{
					libFlag("force-window", "yes"),
					libStr("title", "第 1 集"),
					libStr("force-media-title", "第 1 集"),
					{Name: "sub-files", Kind: OptionList, List: []string{"/run/danmaku.ass"}},
					libStr("start", "+62.500"),
				},
				Media: "/v/ep01.mkv",
			},
		},
		{
			name: "本机流（NAS 经 /stream）：网络流的 256MiB 就地换成 64MiB，其余流式缓存参数不变",
			opts: LaunchOptions{
				MediaPath:     "http://127.0.0.1:8823/stream/cap/nas/0",
				ConfigDir:     cfg,
				NetworkStream: true,
				ScriptOpts:    []string{"thumbfast-mpv_path=/usr/bin/mpv", "other=1"},
				Title:         "第 2 集",
			},
			want: LibmpvSpec{
				Init: join(
					[]LibmpvOption{libFlag("config", "yes"), libFlag("input-default-bindings", "yes")},
					head,
					[]LibmpvOption{
						libStr("config-dir", cfg),
						libStr("cache", "yes"),
						libStr("demuxer-readahead-secs", "60"),
						libStr("demuxer-max-bytes", "64MiB"),
						libStr("cache-pause-initial", "yes"),
						libStr("cache-pause-wait", "5"),
					},
					ios,
					[]LibmpvOption{libStr("demuxer-max-back-bytes", "16MiB"), libStr("idle", "once")},
				),
				BeforeLoad: []LibmpvOption{
					libFlag("force-window", "yes"),
					libStr("title", "第 2 集"),
					libStr("force-media-title", "第 2 集"),
				},
				Media: "http://127.0.0.1:8823/stream/cap/nas/0",
			},
		},
		{
			name: "用户在 iOS 上打开了画质增强：着色器照传",
			opts: LaunchOptions{
				MediaPath: "/v/ep01.mkv",
				ConfigDir: cfg,
				Shaders:   []string{"/a;b/Anime4K_Clamp_Highlights.glsl", "/c:d/Anime4K_Upscale_CNN_x2_S.glsl"},
			},
			want: LibmpvSpec{
				Init: join(
					[]LibmpvOption{libFlag("config", "yes"), libFlag("input-default-bindings", "yes")},
					head,
					[]LibmpvOption{
						libStr("config-dir", cfg),
						{Name: "glsl-shaders", Kind: OptionList, List: []string{"/a;b/Anime4K_Clamp_Highlights.glsl", "/c:d/Anime4K_Upscale_CNN_x2_S.glsl"}},
					},
					ios, budget,
					[]LibmpvOption{libStr("idle", "once")},
				),
				BeforeLoad: []LibmpvOption{libFlag("force-window", "yes")},
				Media:      "/v/ep01.mkv",
			},
		},
		{
			name: "界面配置没写出来（NoConfig）：照样不加载脚本、不出 mpv 自带的控制条",
			opts: LaunchOptions{MediaPath: "/v/ep01.mkv", NoConfig: true},
			want: LibmpvSpec{
				Init: join(
					[]LibmpvOption{libFlag("input-default-bindings", "yes")},
					head,
					[]LibmpvOption{libFlag("config", "no")},
					ios, budget,
					[]LibmpvOption{libStr("idle", "once")},
				),
				BeforeLoad: []LibmpvOption{libFlag("force-window", "yes")},
				Media:      "/v/ep01.mkv",
			},
		},
		{
			name: "不带文件（空闲）",
			opts: LaunchOptions{},
			want: LibmpvSpec{
				Init: join(
					[]LibmpvOption{libFlag("config", "yes"), libFlag("input-default-bindings", "yes")},
					head, ios, budget,
					[]LibmpvOption{libStr("idle", "yes")},
				),
				BeforeLoad: []LibmpvOption{libFlag("force-window", "yes")},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := IOSLibmpvSpec(tc.opts, "/run/mpv.sock")
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

// iosShapes 是 iOS 上可能碰到的几种起播参数（含桌面那边会让 thumbfast / Lua 界面加载的全部条件）。
func iosShapes() map[string]LaunchOptions {
	volume := 70
	return map[string]LaunchOptions{
		"全部选项（含配置目录与 thumbfast-mpv_path）": kitchenSinkLaunch(&volume),
		"本地文件 + 配置目录":                     {MediaPath: "/v/a.mkv", ConfigDir: "/cfg/mpv-config", ScriptOpts: []string{"thumbfast-mpv_path=/usr/bin/mpv"}},
		"不读配置":                            {MediaPath: "/v/a.mkv", NoConfig: true},
		"空闲":                              {},
	}
}

// 不管起播参数长什么样，iOS 上都不加载 Lua 界面与 thumbfast、不出 mpv 自带的 osc、不起 ytdl、预读按预算、不自己加着色器；
// 桌面窗口开发用的 mpv 日志（NAGARE_DESKTOP_MPV_LOG）也不带过来。
func TestIOSLibmpvSpecInvariants(t *testing.T) {
	t.Setenv("NAGARE_DESKTOP_MPV_LOG", "/tmp/desktop-mpv.log")
	for name, opts := range iosShapes() {
		t.Run(name, func(t *testing.T) {
			got, err := IOSLibmpvSpec(opts, "/run/mpv.sock")
			require.NoError(t, err)
			all := append(append([]LibmpvOption{}, got.Init...), got.BeforeLoad...)

			only := func(name string) LibmpvOption {
				var hits []LibmpvOption
				for _, o := range all {
					if o.Name == name {
						hits = append(hits, o)
					}
				}
				require.Len(t, hits, 1, "%s 应当恰好一条", name)
				return hits[0]
			}
			assert.Equal(t, "libmpv", only("vo").String)
			assert.Equal(t, "no", only("load-scripts").String, "配置目录里的 Lua 界面与 thumbfast 一个都不自动加载")
			assert.Equal(t, "no", only("osc").String, "控制条由 SwiftUI 画，mpv 自带的不能叠在上面")
			assert.Equal(t, "no", only("ytdl").String, "ytdl_hook 会起 yt-dlp 子进程，iOS 起不了")
			assert.Equal(t, iosDemuxerMaxBytes, only("demuxer-max-bytes").String)
			assert.Equal(t, iosDemuxerMaxBackBytes, only("demuxer-max-back-bytes").String)

			for _, o := range all {
				assert.NotContains(t, []string{"scripts", "script-opts", "log-file"}, o.Name)
				for _, v := range append(append([]string{o.String}, o.List...), keysAndValues(o.Map)...) {
					assert.NotContains(t, strings.ToLower(v), "thumbfast", "%s 里带着 thumbfast 的东西", o.Name)
				}
			}
			if len(opts.Shaders) == 0 {
				assert.False(t, slices.ContainsFunc(all, func(o LibmpvOption) bool { return o.Name == "glsl-shaders" }),
					"设置里没开 Anime4K，就不带着色器")
			}
		})
	}
}

func keysAndValues(m []KeyValue) []string {
	out := make([]string, 0, 2*len(m))
	for _, kv := range m {
		out = append(out, kv.Key, kv.Value)
	}
	return out
}

// 两个宿主共用 NewLaunchSpec：除了上面列出的那几条差别，其余每一项（IPC、配置目录、音量、标题、字幕、续播、
// 着色器、请求头……）两边一字不差 —— 以后往 NewLaunchSpec 里加的选项自动同时到两边，不会「Mac 改了 iOS 忘改」。
func TestIOSAndDesktopShareLaunchSpec(t *testing.T) {
	t.Setenv("NAGARE_DESKTOP_MPV_LOG", "")
	differs := []string{"script-opts", "scripts", "load-scripts", "osc", "ytdl", "demuxer-max-bytes", "demuxer-max-back-bytes"}
	strip := func(in []LibmpvOption) []LibmpvOption {
		return slices.DeleteFunc(slices.Clone(in), func(o LibmpvOption) bool { return slices.Contains(differs, o.Name) })
	}
	noSelf := func() (string, error) { return "", errors.New("测试里不认 nagare 自己") }
	for name, opts := range iosShapes() {
		t.Run(name, func(t *testing.T) {
			desktop, err := DesktopLibmpvSpec(opts, "/run/mpv.sock", 2, noSelf)
			require.NoError(t, err)
			ios, err := IOSLibmpvSpec(opts, "/run/mpv.sock")
			require.NoError(t, err)
			assert.Equal(t, strip(desktop.Init), strip(ios.Init))
			assert.Equal(t, desktop.BeforeLoad, ios.BeforeLoad)
			assert.Equal(t, desktop.Media, ios.Media)
		})
	}
}

// iOS 变体不改调用方的起播参数（script-opts 是在副本上去掉的）。
func TestIOSLibmpvSpecDoesNotMutateInput(t *testing.T) {
	opts := LaunchOptions{MediaPath: "/v/a.mkv", ConfigDir: "/cfg", ScriptOpts: []string{"thumbfast-mpv_path=/usr/bin/mpv", "x=1"}}
	_, err := IOSLibmpvSpec(opts, "/run/mpv.sock")
	require.NoError(t, err)
	assert.Equal(t, []string{"thumbfast-mpv_path=/usr/bin/mpv", "x=1"}, opts.ScriptOpts)
}

// libmpv 在 mpv_initialize 里才解析 mpv.conf，会盖过之前设的 Init 选项（见 LibmpvSpec.Init）。iOS 多带 / 换掉的
// 那几条，界面配置里一条都不能出现（否则 osc 会被打开、预算会被悄悄放大）；其余 Init 选项照
// TestUIConfigDoesNotOverrideLaunchOptions 的规矩：同名就必须同值，列表不许出现。
func TestUIConfigDoesNotOverrideIOSOptions(t *testing.T) {
	conf := uiConfigOptions(t)
	for _, o := range iosEmbeddedOptions() {
		_, inConf := conf[o.Name]
		assert.False(t, inConf, "ui/mpv.conf 会在 libmpv 里盖过 iOS 设的 %s", o.Name)
	}
	for name, opts := range iosShapes() {
		spec, err := IOSLibmpvSpec(opts, "/run/mpv.sock")
		require.NoError(t, err, name)
		for _, o := range spec.Init {
			value, inConf := conf[o.Name]
			if !inConf {
				continue
			}
			if o.Kind == OptionList || o.Kind == OptionMap {
				t.Errorf("%s：mpv.conf 里设了列表选项 %s，libmpv 下会整表盖过启动时传的值", name, o.Name)
				continue
			}
			assert.Equalf(t, o.String, value, "%s：mpv.conf 里的 %s 与 iOS 启动选项取值不同", name, o.Name)
		}
	}
}

// withOptions 同名就地换掉、其余接在后面，返回新切片，不动传进来的。
func TestWithOptions(t *testing.T) {
	base := []Option{stringOption("a", "1"), stringOption("b", "2")}
	got := withOptions(base, []Option{stringOption("b", "20"), stringOption("c", "3")})
	assert.Equal(t, []Option{stringOption("a", "1"), stringOption("b", "20"), stringOption("c", "3")}, got)
	assert.Equal(t, []Option{stringOption("a", "1"), stringOption("b", "2")}, base)
}

// thumbfastCLI 在这个包里也测一遍（桌面窗口那边经 libmpvSpec 测过整条路径）：只有绝对路径、普通文件、
// 而且不是 nagare 自己的命令行 mpv 才交给 thumbfast；取不到 nagare 自己的路径（含没给）一律不交。
func TestThumbfastCLI(t *testing.T) {
	dir := t.TempDir()
	cli, self := filepath.Join(dir, "mpv"), filepath.Join(dir, "nagare")
	require.NoError(t, os.WriteFile(cli, []byte("#!/bin/sh\n"), 0o755))
	require.NoError(t, os.WriteFile(self, []byte("#!/bin/sh\n"), 0o755))
	resolvedSelf, err := filepath.EvalSymlinks(self)
	require.NoError(t, err)
	link := filepath.Join(t.TempDir(), "mpv-link")
	require.NoError(t, os.Symlink(self, link))
	selfFn := func() (string, error) { return resolvedSelf, nil }

	path, why := thumbfastCLI([]string{"thumbfast-mpv_path=" + cli}, selfFn)
	assert.Equal(t, cli, path)
	assert.Empty(t, why)

	for name, tc := range map[string]struct {
		opts []string
		self func() (string, error)
	}{
		"没有命令行 mpv":     {nil, selfFn},
		"相对路径":          {[]string{"thumbfast-mpv_path=mpv"}, selfFn},
		"不存在":           {[]string{"thumbfast-mpv_path=" + filepath.Join(dir, "missing")}, selfFn},
		"是目录":           {[]string{"thumbfast-mpv_path=" + dir}, selfFn},
		"就是 nagare 自己":  {[]string{"thumbfast-mpv_path=" + self}, selfFn},
		"指向 nagare 的链接": {[]string{"thumbfast-mpv_path=" + link}, selfFn},
		"取不到自己的路径":      {[]string{"thumbfast-mpv_path=" + cli}, func() (string, error) { return "", errors.New("x") }},
		"没给取自己路径的函数":    {[]string{"thumbfast-mpv_path=" + cli}, nil},
	} {
		t.Run(name, func(t *testing.T) {
			path, why := thumbfastCLI(tc.opts, tc.self)
			assert.Empty(t, path)
			assert.NotEmpty(t, why, "不能用时要说清楚原因（写进日志）")
		})
	}
}

// DesktopLibmpvSpec 在这个包里的最小覆盖：有能用的命令行 mpv 时点名加载 thumbfast，缩略图按屏幕缩放放大。
// 完整输出的黄金在 internal/desktop（libmpv_spec_golden_test.go）。
func TestDesktopLibmpvSpecLoadsThumbfast(t *testing.T) {
	t.Setenv("NAGARE_DESKTOP_MPV_LOG", "")
	dir := t.TempDir()
	cli, self := filepath.Join(dir, "mpv"), filepath.Join(dir, "nagare")
	require.NoError(t, os.WriteFile(cli, []byte("#!/bin/sh\n"), 0o755))
	require.NoError(t, os.WriteFile(self, []byte("#!/bin/sh\n"), 0o755))
	spec, err := DesktopLibmpvSpec(LaunchOptions{
		MediaPath:  "/v/a.mkv",
		ConfigDir:  "/cfg",
		ScriptOpts: []string{"thumbfast-mpv_path=" + cli},
	}, "/run/mpv.sock", 2, func() (string, error) { return filepath.EvalSymlinks(self) })
	require.NoError(t, err)
	var scripts, so LibmpvOption
	for _, o := range spec.Init {
		switch o.Name {
		case "scripts":
			scripts = o
		case "script-opts":
			so = o
		}
	}
	assert.Equal(t, []string{filepath.Join("/cfg", "scripts", "nagare"), filepath.Join("/cfg", "scripts", "thumbfast.lua")}, scripts.List)
	assert.Equal(t, []KeyValue{
		{Key: "thumbfast-mpv_path", Value: cli},
		{Key: "thumbfast-max_width", Value: "384"},
		{Key: "thumbfast-max_height", Value: "384"},
	}, so.Map)
}
