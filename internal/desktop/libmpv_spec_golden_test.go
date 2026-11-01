package desktop

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nagare-project/nagare/internal/mpv"
)

func goldStr(name, value string) mpv.LibmpvOption {
	return mpv.LibmpvOption{Name: name, Kind: mpv.OptionString, String: value}
}

func goldFlag(name, value string) mpv.LibmpvOption {
	return mpv.LibmpvOption{Name: name, Kind: mpv.OptionFlag, String: value}
}

// 桌面窗口进程内 libmpv 的完整输出，逐项钉死：Init / BeforeLoad / Media 的每一条、每一条的顺序。
// desktop_test.go 只挑着查几项；这张表是 libmpvSpec 挪到 internal/mpv（与 iOS 共用）之前的代码跑出来的，
// 挪完必须一字不差 —— macOS 桌面窗口的起播选项不能因为多了一个 iOS 变体而变。
func TestLibmpvSpecGolden(t *testing.T) {
	t.Setenv("NAGARE_DESKTOP_MPV_LOG", "")
	cli, _ := fakeCLI(t)
	cfg := filepath.Join("/cfg", "runtime", "mpv-config")
	volume := 70
	profile := []mpv.LibmpvOption{goldFlag("config", "yes"), goldFlag("input-default-bindings", "yes"), goldFlag("osc", "yes")}
	join := func(parts ...[]mpv.LibmpvOption) []mpv.LibmpvOption {
		var out []mpv.LibmpvOption
		for _, p := range parts {
			out = append(out, p...)
		}
		return out
	}

	cases := []struct {
		name  string
		opts  mpv.LaunchOptions
		hidpi float64
		want  mpv.LibmpvSpec
	}{
		{
			name:  "本地文件 + 界面，没有命令行 mpv（不加载 thumbfast）",
			hidpi: 2,
			opts: mpv.LaunchOptions{
				MediaPath: "/v/ep01.mkv",
				ConfigDir: cfg,
				Title:     "第 1 集",
				SubPath:   "/run/danmaku.ass",
				StartAt:   62.5,
				Volume:    &volume,
			},
			want: mpv.LibmpvSpec{
				Init: join(profile, []mpv.LibmpvOption{
					goldStr("input-ipc-server", "/run/mpv.sock"),
					goldFlag("terminal", "no"),
					goldStr("keep-open", "no"),
					goldStr("config-dir", cfg),
					goldStr("volume", "70"),
					goldStr("vo", "libmpv"),
					goldFlag("load-scripts", "no"),
					{Name: "scripts", Kind: mpv.OptionList, List: []string{filepath.Join(cfg, "scripts", "nagare")}},
					goldStr("idle", "once"),
				}),
				BeforeLoad: []mpv.LibmpvOption{
					goldFlag("force-window", "yes"),
					goldStr("title", "第 1 集"),
					goldStr("force-media-title", "第 1 集"),
					{Name: "sub-files", Kind: mpv.OptionList, List: []string{"/run/danmaku.ass"}},
					goldStr("start", "+62.500"),
				},
				Media: "/v/ep01.mkv",
			},
		},
		{
			name:  "磁力流 + 命令行 mpv（加载 thumbfast，缩略图按 2 倍）+ 着色器 + 请求头",
			hidpi: 2,
			opts: mpv.LaunchOptions{
				MediaPath:     "http://127.0.0.1:8823/stream/cap/t/hash/0",
				ConfigDir:     cfg,
				NetworkStream: true,
				ScriptOpts:    []string{"thumbfast-mpv_path=" + cli, "other=1,2"},
				Shaders:       []string{"/s/a;b/1.glsl", "/s/c:d/2.glsl"},
				HTTPHeaders:   map[string]string{"User-Agent": "UA (KHTML, like Gecko)", "Referer": "https://x.test/"},
				Title:         "标题, 带逗号",
			},
			want: mpv.LibmpvSpec{
				Init: join(profile, []mpv.LibmpvOption{
					goldStr("input-ipc-server", "/run/mpv.sock"),
					goldFlag("terminal", "no"),
					goldStr("keep-open", "no"),
					goldStr("config-dir", cfg),
					{Name: "script-opts", Kind: mpv.OptionMap, Map: []mpv.KeyValue{
						{Key: "thumbfast-mpv_path", Value: cli},
						{Key: "other", Value: "1,2"},
						{Key: "thumbfast-max_width", Value: "384"},
						{Key: "thumbfast-max_height", Value: "384"},
					}},
					goldStr("cache", "yes"),
					goldStr("demuxer-readahead-secs", "60"),
					goldStr("demuxer-max-bytes", "256MiB"),
					goldStr("cache-pause-initial", "yes"),
					goldStr("cache-pause-wait", "5"),
					{Name: "glsl-shaders", Kind: mpv.OptionList, List: []string{"/s/a;b/1.glsl", "/s/c:d/2.glsl"}},
					goldStr("vo", "libmpv"),
					goldFlag("load-scripts", "no"),
					{Name: "scripts", Kind: mpv.OptionList, List: []string{
						filepath.Join(cfg, "scripts", "nagare"),
						filepath.Join(cfg, "scripts", "thumbfast.lua"),
					}},
					goldStr("idle", "once"),
				}),
				BeforeLoad: []mpv.LibmpvOption{
					goldFlag("force-window", "yes"),
					goldStr("title", "标题, 带逗号"),
					goldStr("force-media-title", "标题, 带逗号"),
					{Name: "http-header-fields", Kind: mpv.OptionList, List: []string{
						"Referer: https://x.test/",
						"User-Agent: UA (KHTML, like Gecko)",
					}},
				},
				Media: "http://127.0.0.1:8823/stream/cap/t/hash/0",
			},
		},
		{
			// 没有配置目录就没有 thumbfast 可加载：交给它的 mpv 路径也去掉
			name:  "界面配置没写出来（NoConfig）：不点名脚本，退回 mpv 自带的控制条",
			hidpi: 1,
			opts:  mpv.LaunchOptions{MediaPath: "/v/ep01.mkv", NoConfig: true, ScriptOpts: []string{"thumbfast-mpv_path=" + cli}},
			want: mpv.LibmpvSpec{
				Init: []mpv.LibmpvOption{
					goldFlag("input-default-bindings", "yes"),
					goldFlag("osc", "yes"),
					goldStr("input-ipc-server", "/run/mpv.sock"),
					goldFlag("terminal", "no"),
					goldStr("keep-open", "no"),
					goldFlag("config", "no"),
					goldStr("vo", "libmpv"),
					goldStr("idle", "once"),
				},
				BeforeLoad: []mpv.LibmpvOption{goldFlag("force-window", "yes")},
				Media:      "/v/ep01.mkv",
			},
		},
		{
			name:  "不带文件（空闲）",
			hidpi: 1,
			opts:  mpv.LaunchOptions{},
			want: mpv.LibmpvSpec{
				Init: join(profile, []mpv.LibmpvOption{
					goldStr("input-ipc-server", "/run/mpv.sock"),
					goldFlag("terminal", "no"),
					goldStr("keep-open", "no"),
					goldStr("vo", "libmpv"),
					goldStr("idle", "yes"),
				}),
				BeforeLoad: []mpv.LibmpvOption{goldFlag("force-window", "yes")},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := libmpvSpec(tc.opts, "/run/mpv.sock", tc.hidpi)
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

// NAGARE_DESKTOP_MPV_LOG（开发时排查用的 mpv 日志）只在显式设置时排在最后、idle 之前。
func TestLibmpvSpecGoldenDevLog(t *testing.T) {
	t.Setenv("NAGARE_DESKTOP_MPV_LOG", "/tmp/mpv dev.log")
	got, err := libmpvSpec(mpv.LaunchOptions{MediaPath: "/v/ep01.mkv"}, "/run/mpv.sock", 1)
	require.NoError(t, err)
	assert.Equal(t, []mpv.LibmpvOption{
		goldFlag("config", "yes"),
		goldFlag("input-default-bindings", "yes"),
		goldFlag("osc", "yes"),
		goldStr("input-ipc-server", "/run/mpv.sock"),
		goldFlag("terminal", "no"),
		goldStr("keep-open", "no"),
		goldStr("vo", "libmpv"),
		goldStr("log-file", "/tmp/mpv dev.log"),
		goldStr("idle", "once"),
	}, got.Init)
}
