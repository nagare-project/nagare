//go:build unix

package mpv

import (
	"context"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 验收：libmpv 形态里整表设的值，交给真 mpv 之后原样读得回来 —— 逗号、冒号、分号、等号都不拆。
//
// 这台机器上编不了 libmpv（cgo 坏了），这里经 JSON IPC 的 set_property 设：JSON 数组 / 对象在 mpv
// 里就是 NODE_ARRAY / NODE_MAP，与桌面窗口那层将来调的 mpv_set_option(MPV_FORMAT_NODE) 走同一条
// 节点 → 选项的转换（m_option 的 set 回调）。没装 mpv 跳过；-short 跳过。
func TestIntegration_LibmpvListValuesSurviveRealMPV(t *testing.T) {
	if testing.Short() {
		t.Skip("-short 模式跳过真 mpv 集成测试")
	}
	mpvPath, err := exec.LookPath("mpv")
	if err != nil {
		t.Skip("未安装 mpv，跳过集成测试")
	}
	info, err := Detect(mpvPath)
	require.NoError(t, err)
	dir, err := os.MkdirTemp("", "nagopt")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	p, err := Launch(ctx, LaunchOptions{MPVPath: info.Path, SocketDir: dir})
	require.NoError(t, err)
	t.Cleanup(func() { _ = p.Close() })

	volume := 70
	spec, err := NewLaunchSpec(kitchenSinkLaunch(&volume), "/s").Libmpv()
	require.NoError(t, err)
	checked := 0
	for _, o := range append(spec.Init, spec.BeforeLoad...) {
		switch o.Name {
		case "script-opts", "glsl-shaders", "sub-files", "http-header-fields", "title", "force-media-title":
		default:
			// 其余要么只能在启动前设（input-ipc-server、config），要么 mpv 读回来会规范化写法（start 的 +12.500 读回 +12.5）
			continue
		}
		require.NoError(t, p.SetProperty(o.Name, o.Value()), o.Name)
		switch want := o.Value().(type) {
		case []string:
			var got []string
			require.NoError(t, p.GetProperty(o.Name, &got))
			assert.Equal(t, want, got, o.Name)
		case map[string]string:
			var got map[string]string
			require.NoError(t, p.GetProperty(o.Name, &got))
			assert.Equal(t, want, got, o.Name)
		default:
			var got string
			require.NoError(t, p.GetProperty(o.Name, &got))
			assert.Equal(t, want, got, o.Name)
		}
		checked++
	}
	assert.Equal(t, 6, checked, "六条都要验到")
	// 不存在的着色器与字幕别留到之后（空闲窗口不渲染视频，但收干净）
	require.NoError(t, p.SetProperty("glsl-shaders", []string{}))
	require.NoError(t, p.SetProperty("sub-files", []string{}))
}
