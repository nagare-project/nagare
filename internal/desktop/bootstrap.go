package desktop

import (
	"strconv"
	"strings"
)

// WebView2 运行时（Evergreen）装没装：微软文档给的判断（distribution 一文的 "Detect if a WebView2 Runtime is
// already installed"）是读这两处注册表的 pv（REG_SZ），存在且大于 0.0.0.0 就算装了：
//
//	HKLM\SOFTWARE\WOW6432Node\Microsoft\EdgeUpdate\Clients\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}   按机器装
//	HKCU\Software\Microsoft\EdgeUpdate\Clients\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}               按用户装
//
// 安装包（scripts/windows/nagare.nsi）用同一条规则决定要不要跑内嵌的 bootstrapper；两处都没有才跑。
// 只查 HKLM 会把按用户装的当成没装（stremio 的 Inno 脚本就这样）；Wails 的 NSIS 宏 HKCU 那条路径少了一个
// 反斜杠，等于永远查不到（windows.md §2.4）—— 测试里核对 nsi 里的两条路径与这里逐字一致。
const (
	webView2ClientGUID = "{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}"
	// webView2MachineKey 在 HKLM 下（32 位视图；64 位系统上就是 WOW6432Node 这一条）。
	webView2MachineKey = `SOFTWARE\WOW6432Node\Microsoft\EdgeUpdate\Clients\` + webView2ClientGUID
	// webView2UserKey 在 HKCU 下。
	webView2UserKey = `Software\Microsoft\EdgeUpdate\Clients\` + webView2ClientGUID
	// webView2DownloadURL 是微软的 Evergreen 运行时下载页（便携版没有运行时时，界面与日志里给这个链接）。
	webView2DownloadURL = "https://developer.microsoft.com/microsoft-edge/webview2/"
)

// runtimeInstalled：两处 pv 有一处是比 0.0.0.0 大的版本号，就算装了。
func runtimeInstalled(pvMachine, pvUser string) bool {
	return validRuntimeVersion(pvMachine) || validRuntimeVersion(pvUser)
}

// validRuntimeVersion：形如 a.b.c.d 的版本号，且不全是 0。卸载之后 pv 可能留着空串或 0.0.0.0。
func validRuntimeVersion(pv string) bool {
	parts := strings.Split(strings.TrimSpace(pv), ".")
	if len(parts) != 4 {
		return false
	}
	nonZero := false
	for _, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return false
		}
		if n > 0 {
			nonZero = true
		}
	}
	return nonZero
}
