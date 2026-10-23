package desktop

import (
	"net/url"
	"strings"
)

// navAction 是网页层一次导航的处理办法（Windows 的 WebView2 用；macOS 的同一套规则写在 nav_darwin.m 里，
// 两边的测试表是同一份用例）。
type navAction int

const (
	actAllow    navAction = iota // 放行
	actLoadHere                  // target=_blank / window.open 指向我们自己：就在这个窗口里开
	actExternal                  // 交给系统默认浏览器 / 邮件应用
	actCancel                    // 拦下
)

// decideNav 决定一次导航怎么处理：
//
//   - 窗口里只开我们自己的源（与 Init 给的 origin 的协议、主机、端口完全一致；不认 localhost 之类的别名，
//     首个地址用的就是 127.0.0.1）；target=_blank / window.open 指向它时在这个窗口里开，不开第二个窗口。
//   - http / https / mailto 交给系统默认浏览器 / 邮件应用。
//   - 其余一律拦下：data:、javascript:、blob:、about:、file:、各种应用协议……
//   - 内嵌框架（发现页的预告片 iframe）照网页自己的规矩走。
//
// 为什么这么严：target=_blank 要在窗口里开，只能由宿主自己 Navigate —— 那是「嵌入方发起」的导航，会绕开浏览器
// 对「页面发起的顶层 data: / javascript: 导航」的拦截（macOS 评审实测旧规则下 data: 换掉了主框架）。
func decideNav(raw string, newWindow, subframe bool, origin string) navAction {
	if subframe {
		return actAllow
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" {
		return actCancel
	}
	if isOurOrigin(u, origin) {
		if newWindow {
			return actLoadHere
		}
		return actAllow
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https", "mailto":
		return actExternal
	}
	return actCancel
}

// desktopUAMarker 是桌面窗口追加在 User-Agent 末尾的记号（frontend/src/lib/desktopWindow.ts 认它）。
const desktopUAMarker = "NagareDesktop"

// withDesktopMarker 在 User-Agent 末尾追加记号；已经有了就不再加（网页层重建时会再设一次）。
func withDesktopMarker(ua string) string {
	for _, tok := range strings.Fields(ua) {
		if tok == desktopUAMarker || strings.HasPrefix(tok, desktopUAMarker+"/") {
			return ua
		}
	}
	return strings.TrimSpace(ua) + " " + desktopUAMarker
}

// isOurOrigin：协议、主机（不分大小写）与端口都与 origin 一致，且两边都写明了端口。
func isOurOrigin(u *url.URL, origin string) bool {
	o, err := url.Parse(origin)
	if err != nil || o.Port() == "" || u.Port() == "" {
		return false
	}
	return strings.EqualFold(u.Scheme, o.Scheme) && strings.EqualFold(u.Hostname(), o.Hostname()) && u.Port() == o.Port()
}

// keepContextMenu：网页层里右键时留不留浏览器自己的菜单。输入框（往里贴磁力链接、地址）和选中的文字（复制）上留着；
// 别处不弹 —— 返回、刷新、另存为、打印……在应用里没有意义。all：开发者工具开着时全留（排查界面用）。
func keepContextMenu(editable, hasSelection, all bool) bool {
	return all || editable || hasSelection
}
