package mpv

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func libStr(name, value string) LibmpvOption {
	return LibmpvOption{Name: name, Kind: OptionString, String: value}
}

func libFlag(name, value string) LibmpvOption {
	return LibmpvOption{Name: name, Kind: OptionFlag, String: value}
}

// libmpv 的形态：列表整表设、值原样（逗号 / 冒号 / 分号 / 等号都不拆），PerFile 与 force-window
// 放到 loadfile 之前，媒体单独交出去，外加抵消 libmpv 默认值的几条。
func TestLibmpvKitchenSink(t *testing.T) {
	volume := 70
	got, err := NewLaunchSpec(kitchenSinkLaunch(&volume), "/run/nagare.sock").Libmpv()
	require.NoError(t, err)

	assert.Equal(t, []LibmpvOption{
		libFlag("config", "yes"),
		libFlag("input-default-bindings", "yes"),
		libFlag("osc", "yes"),
		libStr("input-ipc-server", "/run/nagare.sock"),
		libFlag("terminal", "no"),
		libStr("keep-open", "no"),
		libStr("config-dir", "/cfg/a,b:c=d/mpv-config"),
		{Name: "script-opts", Kind: OptionMap, Map: []KeyValue{
			// 同一个键追加两次：后者覆盖前者（与 argv 逐项 -append 一致）
			{Key: "thumbfast-mpv_path", Value: "/opt/override/mpv"},
			{Key: "other", Value: "1,2"},
		}},
		libStr("volume", "70"),
		libStr("cache", "yes"),
		libStr("demuxer-readahead-secs", "60"),
		libStr("demuxer-max-bytes", "256MiB"),
		libStr("cache-pause-initial", "yes"),
		libStr("cache-pause-wait", "5"),
		{Name: "glsl-shaders", Kind: OptionList, List: []string{"/s/a;b/1.glsl", "/s/c:d,e=f/2.glsl"}},
		libStr("idle", "once"),
	}, got.Init)

	assert.Equal(t, []LibmpvOption{
		libFlag("force-window", "yes"),
		libStr("title", "标题, 带逗号: 和冒号"),
		libStr("force-media-title", "标题, 带逗号: 和冒号"),
		{Name: "sub-files", Kind: OptionList, List: []string{"/sub/a,b:c=d.ass"}},
		libStr("start", "+12.500"),
		// 整表设时一条请求头就是一项：argv 的 -add 会在逗号处拆开，这里不会
		{Name: "http-header-fields", Kind: OptionList, List: []string{
			"Referer: https://x.test/a?b=c,d",
			"User-Agent: UA (KHTML, like Gecko)",
		}},
	}, got.BeforeLoad)
	assert.Equal(t, "https://media.example/ep.m3u8", got.Media)
}

// 键值列表里同一个键追加多次：旧的去掉、新的排到最后，与 mpv 对 --script-opts-append 的处理一致
// （mpv 0.41 实测 a=1、b=2、a=3 读回 b=2,a=3）。
func TestLibmpvMapRepeatedKeyMatchesMPV(t *testing.T) {
	got, err := NewLaunchSpec(LaunchOptions{ScriptOpts: []string{"a=1", "b=2", "a=3=x"}}, "/s").Libmpv()
	require.NoError(t, err)
	for _, o := range got.Init {
		if o.Name == "script-opts" {
			assert.Equal(t, []KeyValue{{"b", "2"}, {"a", "3=x"}}, o.Map)
			return
		}
	}
	t.Fatal("没有 script-opts")
}

// 不带文件：一直空闲（与 argv 的 --idle=yes 一致）；没有 PerFile 的东西，BeforeLoad 只剩 force-window。
func TestLibmpvIdle(t *testing.T) {
	got, err := NewLaunchSpec(LaunchOptions{}, "/s").Libmpv()
	require.NoError(t, err)
	assert.Equal(t, libStr("idle", "yes"), got.Init[len(got.Init)-1])
	assert.Equal(t, []LibmpvOption{libFlag("force-window", "yes")}, got.BeforeLoad)
	assert.Empty(t, got.Media)
}

// 界面配置没写出来时命令行带 --no-config：libmpv 也不能被 config=yes 盖回去去读用户配置。
func TestLibmpvNoConfigWins(t *testing.T) {
	got, err := NewLaunchSpec(LaunchOptions{MediaPath: "/v/a.mkv", NoConfig: true}, "/s").Libmpv()
	require.NoError(t, err)
	var configs []LibmpvOption
	for _, o := range got.Init {
		if o.Name == "config" {
			configs = append(configs, o)
		}
	}
	assert.Equal(t, []LibmpvOption{libFlag("config", "no")}, configs)
}

// 每条选项的 Name 都是 libmpv 认的名字：没有 -append / -add 后缀（mpv_set_option_string 不认）。
func TestLibmpvNamesHaveNoListSuffix(t *testing.T) {
	volume := 70
	got, err := NewLaunchSpec(kitchenSinkLaunch(&volume), "/s").Libmpv()
	require.NoError(t, err)
	for _, o := range append(got.Init, got.BeforeLoad...) {
		assert.NotRegexp(t, `-(append|add|pre|set|clr|remove)$`, o.Name)
		assert.NotContains(t, o.Name, "--")
	}
}

func TestLibmpvRejectsWhatCannotBeEncoded(t *testing.T) {
	for name, opts := range map[string]LaunchOptions{
		"脚本参数不是 key=value": {ScriptOpts: []string{"thumbfast"}},
		"脚本参数的键是空的":        {ScriptOpts: []string{"=v"}},
		"值里有 NUL":          {Title: "a\x00b"},
		"列表项里有 NUL":        {Shaders: []string{"/a\x00.glsl"}},
		"媒体路径里有 NUL":       {MediaPath: "/v/a\x00.mkv"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := NewLaunchSpec(opts, "/s").Libmpv()
			require.Error(t, err)
		})
	}
}

// Value 给经 JSON IPC 设属性用：列表是 JSON 数组、键值列表是 JSON 对象，返回的是副本。
func TestLibmpvOptionValue(t *testing.T) {
	list := LibmpvOption{Name: "glsl-shaders", Kind: OptionList, List: []string{"/a:b,c.glsl"}}
	v := list.Value().([]string)
	assert.Equal(t, []string{"/a:b,c.glsl"}, v)
	v[0] = "改了"
	assert.Equal(t, "/a:b,c.glsl", list.List[0], "Value 返回的是副本")

	m := LibmpvOption{Name: "script-opts", Kind: OptionMap, Map: []KeyValue{{"a", "x,y=z"}, {"b", ""}}}
	assert.Equal(t, map[string]string{"a": "x,y=z", "b": ""}, m.Value())
	assert.Equal(t, "+1.000", libStr("start", "+1.000").Value())
	assert.Equal(t, "no", libFlag("terminal", "no").Value())
}

// Libmpv 不改清单本身：渲染两次、先 libmpv 后 argv，结果都不变。
func TestLaunchSpecRenderingsDoNotMutate(t *testing.T) {
	volume := 70
	spec := NewLaunchSpec(kitchenSinkLaunch(&volume), "/s")
	before := spec.Argv()
	first, err := spec.Libmpv()
	require.NoError(t, err)
	first.Init[len(first.Init)-2].List[0] = "改了" // glsl-shaders
	second, err := spec.Libmpv()
	require.NoError(t, err)
	assert.Equal(t, "/s/a;b/1.glsl", second.Init[len(second.Init)-2].List[0])
	assert.Equal(t, before, spec.Argv())
}
