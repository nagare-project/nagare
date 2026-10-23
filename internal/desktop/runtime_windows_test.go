//go:build windows

package desktop

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

// WebView2 运行时的检测（真 Windows）：GitHub 的 Windows runner 装着 Edge 与 WebView2 运行时，装载器找得到它、
// 注册表的 pv 判断（安装包用的那条）也说装了。在没有运行时的机器上跑设 NAGARE_TEST_NO_WEBVIEW2=1，只验另一边。
func TestWebView2RuntimeDetection(t *testing.T) {
	machine, user := registryRuntimeVersions()
	t.Logf("装载器：%q；注册表 pv：按机器 %q，按用户 %q", RuntimeVersion(), machine, user)
	if os.Getenv("NAGARE_TEST_NO_WEBVIEW2") == "1" {
		assert.Error(t, Ready())
		return
	}
	if RuntimeVersion() == "" {
		assert.Error(t, Ready(), "没有运行时时 Ready 必须报错（退回浏览器模式）")
		t.Skip("这台机器上没有 WebView2 运行时：只验了「没有就报错」这一边")
	}
	assert.NoError(t, Ready())
	assert.NotEmpty(t, RuntimeVersion())
	assert.True(t, runtimeInstalled(machine, user), "安装包的判断与装载器对不上：会白跑一次 bootstrapper")
}
