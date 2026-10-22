//go:build windows

package desktop

import (
	"errors"
	"fmt"

	"golang.org/x/sys/windows/registry"

	webviewloader "github.com/nagare-project/nagare/internal/webview2/webviewloader"
)

// Ready 报告这台机器上窗口能不能用：要有 WebView2 运行时（Evergreen，Win11 自带，绝大多数 Win10 已装；安装包在
// 两处注册表都没有时会静默装一份，便携版装不了）。没有就退回浏览器模式，错误里写着怎么装。
//
// 真正的判断是装载器自己找得到运行时（webviewloader 读 EdgeUpdate\ClientState 下的 EBWebView，与 nagare 实际
// 创建 WebView2 时走的是同一条路）；安装包用的是微软文档的 pv 判断（bootstrap.go），两者对不上时写进错误里，
// 方便看日志的人知道是哪一边的问题。
func Ready() error {
	if testHooks.noRuntime {
		return errors.New("（测试）假装这台电脑上没有 WebView2 运行时")
	}
	v, err := webviewloader.GetAvailableCoreWebView2BrowserVersionString("")
	if err == nil && v != "" {
		return nil
	}
	if err == nil {
		err = errors.New("没找到")
	}
	machine, user := registryRuntimeVersions()
	if runtimeInstalled(machine, user) {
		err = fmt.Errorf("%w；注册表里登记着运行时（按机器 %q，按用户 %q），但装载器用不了它", err, machine, user)
	}
	return fmt.Errorf("这台电脑上没有可用的 WebView2 运行时（%v）。界面改在浏览器里打开、mpv 单独开窗口；"+
		"从 %s 装上「Evergreen 运行时」后重启 nagare 就会用自己的窗口", err, webView2DownloadURL)
}

// RuntimeVersion 是装载器找到的 WebView2 运行时版本（找不到为空）。给测试遥控与日志用。
func RuntimeVersion() string {
	v, _ := webviewloader.GetAvailableCoreWebView2BrowserVersionString("")
	return v
}

// registryRuntimeVersions 读两处注册表的 pv（与安装包的判断同一份数据）。
func registryRuntimeVersions() (machine, user string) {
	return readPV(registry.LOCAL_MACHINE, webView2MachineKey), readPV(registry.CURRENT_USER, webView2UserKey)
}

func readPV(root registry.Key, path string) string {
	k, err := registry.OpenKey(root, path, registry.QUERY_VALUE)
	if err != nil {
		return ""
	}
	defer k.Close()
	v, _, err := k.GetStringValue("pv")
	if err != nil {
		return ""
	}
	return v
}
