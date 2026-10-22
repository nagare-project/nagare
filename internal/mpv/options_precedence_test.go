package mpv

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// uiConfigOptions 读出内置 mpv.conf 里设的选项（名字去掉 -append 一类的后缀）。
func uiConfigOptions(t *testing.T) map[string]string {
	t.Helper()
	conf, err := uiFiles.ReadFile("ui/mpv.conf")
	require.NoError(t, err)
	out := map[string]string{}
	for _, line := range strings.Split(string(conf), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "[") {
			continue
		}
		name, value, _ := strings.Cut(line, "=")
		name = strings.TrimPrefix(strings.TrimSpace(name), "--")
		for _, suffix := range []string{"-append", "-add", "-set", "-pre", "-clr", "-remove", "-toggle"} {
			name = strings.TrimSuffix(name, suffix)
		}
		out[name] = strings.TrimSpace(value)
	}
	require.NotEmpty(t, out, "内置 mpv.conf 一条选项都没读到")
	return out
}

// libmpv 下 mpv.conf 在 mpv_initialize 里才解析，会盖过之前设的 Init 选项；命令行 mpv 正好相反（argv
// 盖过 mpv.conf）。界面配置里要是有与 Init 同名而取值不同的选项，那一项就只在 macOS 上变样 ——
// 比如往 mpv.conf 里写一条 script-opts=…，会整表换掉 nagare 传的 thumbfast-mpv_path。
// BeforeLoad 是 mpv_initialize 之后以属性设的，照旧盖过 mpv.conf，与命令行一致，不用查。
func TestUIConfigDoesNotOverrideLaunchOptions(t *testing.T) {
	conf := uiConfigOptions(t)
	volume := 70
	for name, opts := range map[string]LaunchOptions{
		"全部选项": kitchenSinkLaunch(&volume),
		"空闲":   {},
		"不读配置": {MediaPath: "/v/a.mkv", NoConfig: true},
		"本地文件": {MediaPath: "/v/a.mkv", ConfigDir: "/cfg/mpv-config"},
	} {
		spec, err := NewLaunchSpec(opts, "/run/nagare.sock").Libmpv()
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
			assert.Equalf(t, o.String, value, "%s：mpv.conf 里的 %s 与启动选项取值不同（libmpv 下 mpv.conf 会盖过启动选项，命令行 mpv 下正好相反）", name, o.Name)
		}
	}
}
