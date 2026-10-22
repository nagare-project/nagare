package mpv

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// 选项清单改造之前 buildArgs 的输出，逐字节钉死：命令行 mpv（Linux、浏览器模式、今天的所有平台）
// 的启动参数不能因为多了 libmpv 那一种渲染而变。这张表是改造前的代码跑出来的，改造后必须一字不差。
func TestBuildArgsGolden(t *testing.T) {
	const sock = "/tmp/nagare.sock"
	volume := 70
	zero := 0
	head := []string{"--input-ipc-server=" + sock, "--no-terminal", "--force-window", "--keep-open=no"}
	cache := []string{"--cache=yes", "--demuxer-readahead-secs=60", "--demuxer-max-bytes=256MiB", "--cache-pause-initial=yes", "--cache-pause-wait=5"}
	join := func(parts ...[]string) []string {
		var out []string
		for _, p := range parts {
			out = append(out, p...)
		}
		return out
	}

	cases := []struct {
		name string
		opts LaunchOptions
		want []string
	}{
		{
			name: "不带文件（集成测试 / 预热）",
			opts: LaunchOptions{},
			want: join(head, []string{"--idle=yes"}),
		},
		{
			name: "本地文件、什么都不带",
			opts: LaunchOptions{MediaPath: "/v/ep01.mkv"},
			want: join(head, []string{"--", "/v/ep01.mkv"}),
		},
		{
			name: "以 - 开头的文件名照样跟在 -- 后面",
			opts: LaunchOptions{MediaPath: "-rf.mkv"},
			want: join(head, []string{"--", "-rf.mkv"}),
		},
		{
			name: "nagare 界面：配置目录 + 脚本参数 + 音量 + 标题 + 字幕 + 续播 + 着色器",
			opts: LaunchOptions{
				MediaPath:  "/Volumes/媒体/番剧/ep01.mkv",
				ConfigDir:  "/cfg/runtime/mpv-config",
				ScriptOpts: []string{"thumbfast-mpv_path=/opt/mpv, with comma/mpv", "k=v=w"},
				Volume:     &volume,
				Title:      "葬送的芙莉莲 第 11 集",
				SubPath:    "/cfg/runtime/sub,1:2.ass",
				StartAt:    62.5,
				Shaders:    []string{"/a;b/1.glsl", "/c:d/2.glsl"},
			},
			want: join(head, []string{
				"--config-dir=/cfg/runtime/mpv-config",
				"--script-opts-append=thumbfast-mpv_path=/opt/mpv, with comma/mpv",
				"--script-opts-append=k=v=w",
				"--volume=70",
				"--title=葬送的芙莉莲 第 11 集",
				"--force-media-title=葬送的芙莉莲 第 11 集",
				"--sub-file=/cfg/runtime/sub,1:2.ass",
				"--start=+62.500",
				"--glsl-shaders-append=/a;b/1.glsl",
				"--glsl-shaders-append=/c:d/2.glsl",
				"--", "/Volumes/媒体/番剧/ep01.mkv",
			}),
		},
		{
			name: "界面配置没准备好：--no-config",
			opts: LaunchOptions{MediaPath: "/v/ep01.mkv", NoConfig: true},
			want: join(head, []string{"--no-config", "--", "/v/ep01.mkv"}),
		},
		{
			name: "配置目录优先于 NoConfig",
			opts: LaunchOptions{MediaPath: "/v/ep01.mkv", ConfigDir: "/cfg", NoConfig: true},
			want: join(head, []string{"--config-dir=/cfg", "--", "/v/ep01.mkv"}),
		},
		{
			name: "音量 0 也要传（静音起播），起播秒数 0 / 负数不传",
			opts: LaunchOptions{MediaPath: "/v/ep01.mkv", Volume: &zero, StartAt: -3},
			want: join(head, []string{"--volume=0", "--", "/v/ep01.mkv"}),
		},
		{
			name: "磁力边下边播：流式缓存参数",
			opts: LaunchOptions{MediaPath: "http://127.0.0.1:8823/stream/cap/t/hash/0", NetworkStream: true, StartAt: 0.0004},
			want: join(head, cache, []string{"--start=+0.000", "--", "http://127.0.0.1:8823/stream/cap/t/hash/0"}),
		},
		{
			name: "在线来源：请求头按名字排序逐条追加",
			opts: LaunchOptions{
				MediaPath:              "https://media.example/ep.m3u8?sig=a,b",
				NetworkStream:          true,
				RedactMediaDiagnostics: true,
				HTTPHeaders: map[string]string{
					"User-Agent": "Mozilla/5.0 (KHTML, like Gecko)",
					"Referer":    "https://source.example/?a=1&b=2",
					"Cookie":     "k=v; t=x:y",
				},
				Title: "第 2 集",
			},
			want: join(head, cache, []string{
				"--title=第 2 集",
				"--force-media-title=第 2 集",
				"--http-header-fields-add=Cookie: k=v; t=x:y",
				"--http-header-fields-add=Referer: https://source.example/?a=1&b=2",
				"--http-header-fields-add=User-Agent: Mozilla/5.0 (KHTML, like Gecko)",
				"--", "https://media.example/ep.m3u8?sig=a,b",
			}),
		},
		{
			name: "全都带上（顺序与改造前一致）",
			opts: kitchenSinkLaunch(&volume),
			want: join(head, []string{
				"--config-dir=/cfg/a,b:c=d/mpv-config",
				"--script-opts-append=thumbfast-mpv_path=/opt/a,b:c=d/mpv",
				"--script-opts-append=thumbfast-mpv_path=/opt/override/mpv",
				"--script-opts-append=other=1,2",
				"--volume=70",
			}, cache, []string{
				"--title=标题, 带逗号: 和冒号",
				"--force-media-title=标题, 带逗号: 和冒号",
				"--sub-file=/sub/a,b:c=d.ass",
				"--start=+12.500",
				"--glsl-shaders-append=/s/a;b/1.glsl",
				"--glsl-shaders-append=/s/c:d,e=f/2.glsl",
				"--http-header-fields-add=Referer: https://x.test/a?b=c,d",
				"--http-header-fields-add=User-Agent: UA (KHTML, like Gecko)",
				"--", "https://media.example/ep.m3u8",
			}),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, buildArgs(tc.opts, sock))
		})
	}
}

// kitchenSinkLaunch 是把每个字段都用上、值里塞满逗号 / 冒号 / 等号的启动参数（golden 与 libmpv 渲染共用）。
func kitchenSinkLaunch(volume *int) LaunchOptions {
	return LaunchOptions{
		MPVPath:       "/usr/local/bin/mpv",
		MediaPath:     "https://media.example/ep.m3u8",
		SubPath:       "/sub/a,b:c=d.ass",
		Title:         "标题, 带逗号: 和冒号",
		StartAt:       12.5,
		SocketDir:     "/run",
		NetworkStream: true,
		Shaders:       []string{"/s/a;b/1.glsl", "/s/c:d,e=f/2.glsl"},
		ConfigDir:     "/cfg/a,b:c=d/mpv-config",
		ScriptOpts:    []string{"thumbfast-mpv_path=/opt/a,b:c=d/mpv", "thumbfast-mpv_path=/opt/override/mpv", "other=1,2"},
		Volume:        volume,
		HTTPHeaders: map[string]string{
			"User-Agent": "UA (KHTML, like Gecko)",
			"Referer":    "https://x.test/a?b=c,d",
		},
	}
}
