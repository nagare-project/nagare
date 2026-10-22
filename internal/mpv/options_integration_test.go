//go:build unix

package mpv

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 验收：在线来源的请求头经命令行交给真 mpv，到对端时每条还是一条 —— 值里的逗号不能把它拆开。
// 以前用的 --http-header-fields-add 按 -set 的语法在逗号处拆项，「(KHTML, like Gecko)」那种
// User-Agent 到对端就成了两行（第二行以空格开头，被当成续行折回去，逗号丢了）。
func TestIntegration_HTTPHeadersArriveWhole(t *testing.T) {
	if testing.Short() {
		t.Skip("-short 模式跳过真 mpv 集成测试")
	}
	mpvPath, err := exec.LookPath("mpv")
	if err != nil {
		t.Skip("未安装 mpv，跳过集成测试")
	}
	info, err := Detect(mpvPath)
	require.NoError(t, err)

	got := make(chan http.Header, 1)
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case got <- r.Header.Clone():
		default:
		}
		// 不应答：mpv 一直等着这条流，测试读完请求头再收掉它
		select {
		case <-release:
		case <-r.Context().Done():
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(func() { close(release) })

	dir, err := os.MkdirTemp("", "naghdr")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	const ua = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0 Safari/537.36"
	const probe = "a=1, b=2, c=3"
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	p, err := Launch(ctx, LaunchOptions{
		MPVPath:                info.Path,
		MediaPath:              srv.URL + "/ep01.mkv",
		SocketDir:              dir,
		NetworkStream:          true,
		RedactMediaDiagnostics: true,
		HTTPHeaders:            map[string]string{"User-Agent": ua, "X-Nagare-Probe": probe},
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = p.Close() })

	select {
	case h := <-got:
		assert.Equal(t, []string{ua}, h.Values("User-Agent"), "User-Agent 要原样一条到达")
		assert.Equal(t, []string{probe}, h.Values("X-Nagare-Probe"), "值里带逗号的请求头要原样一条到达")
	case <-time.After(10 * time.Second):
		t.Fatal("10 秒内 mpv 没有来请求")
	}
}

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
