package desktop

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 运行时装没装：两处注册表的 pv 有一处是正经版本号就算；空串、0.0.0.0（卸载后的残留）、乱写的都不算。
func TestRuntimeInstalled(t *testing.T) {
	for _, tc := range []struct {
		machine, user string
		want          bool
	}{
		{"", "", false},
		{"0.0.0.0", "", false},
		{"", "0.0.0.0", false},
		{"130.0.2849.80", "", true},
		{"", "130.0.2849.80", true},
		{"0.0.0.0", "118.0.2088.76", true},
		{"garbage", "", false},
		{"130.0.2849", "", false},
		{"130.0.2849.80.1", "", false},
		{"-1.0.0.0", "", false},
		{" 130.0.2849.80 ", "", true},
	} {
		assert.Equal(t, tc.want, runtimeInstalled(tc.machine, tc.user), "machine=%q user=%q", tc.machine, tc.user)
	}
}

// 安装包（nagare.nsi）用同一条规则决定要不要跑 bootstrapper：两处注册表的路径必须与这里逐字一致（Wails 的宏
// 在 HKCU 那条少了一个反斜杠，按用户装的运行时永远查不到），两处都查，只在都没有时才静默跑。
func TestInstallerBootstrapsWebView2LikeTheApp(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "scripts", "windows", "nagare.nsi"))
	require.NoError(t, err)
	nsi := string(b)
	expand := func(s string) string { return strings.ReplaceAll(s, "${WEBVIEW2_GUID}", webView2ClientGUID) }
	define := func(name string) string {
		for _, line := range strings.Split(nsi, "\n") {
			prefix := "!define " + name + " \""
			if strings.HasPrefix(strings.TrimSpace(line), prefix) {
				return expand(strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(line), prefix), "\""))
			}
		}
		return ""
	}
	assert.Equal(t, webView2ClientGUID, define("WEBVIEW2_GUID"))
	assert.Equal(t, webView2MachineKey, define("WEBVIEW2_MACHINE_KEY"))
	assert.Equal(t, webView2UserKey, define("WEBVIEW2_USER_KEY"))
	assert.Contains(t, nsi, `WebView2Installed HKLM "${WEBVIEW2_MACHINE_KEY}"`)
	assert.Contains(t, nsi, `WebView2Installed HKCU "${WEBVIEW2_USER_KEY}"`)
	assert.Contains(t, nsi, `/silent /install`)
	assert.Contains(t, nsi, `${OrIf} ${_OUT} == "0.0.0.0"`, "卸载后留下的 0.0.0.0 不算装了")
	assert.Contains(t, nsi, "Call EnsureWebView2")
	assert.NotContains(t, nsi, `Clients{`, "Clients 后面少反斜杠（Wails 宏的错）")
}
