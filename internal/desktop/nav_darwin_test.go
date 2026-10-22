//go:build darwin && cgo && libmpv

package desktop

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

const (
	navAllow    = 0
	navLoadHere = 1
	navExternal = 2
	navCancel   = 3
)

// 网页层的导航规则：窗口里只开我们自己的源；http / https / mailto 交给默认浏览器；其余拦下。
// 尤其 target=_blank 的 data: / javascript: —— 宿主替页面 loadRequest 会绕开 WebKit 对页面发起的
// 顶层 data: / javascript: 导航的拦截（评审实测旧规则下 data: 换掉了主框架、javascript: 在应用的源里跑了）。
func TestNavDecision(t *testing.T) {
	const origin = "http://127.0.0.1:8591"
	for _, tc := range []struct {
		name      string
		url       string
		newWindow bool
		subframe  bool
		want      int
	}{
		{"本机界面", "http://127.0.0.1:8591/settings#about", false, false, navAllow},
		{"本机界面 target=_blank", "http://127.0.0.1:8591/entry?id=1", true, false, navLoadHere},
		{"主机名大小写不敏感", "HTTP://127.0.0.1:8591/", false, false, navAllow},
		{"别的端口不是我们", "http://127.0.0.1:8592/", false, false, navExternal},
		{"localhost 不是我们（首个地址用的是 127.0.0.1）", "http://localhost:8591/", true, false, navExternal},
		{"外链", "https://anilist.co/anime/1", false, false, navExternal},
		{"外链 target=_blank", "https://github.com/nagare-project/Nagare_Source", true, false, navExternal},
		{"邮件", "mailto:someone@example.com", true, false, navExternal},
		{"data: target=_blank", "data:text/html,<h1>x</h1>", true, false, navCancel},
		{"data: 顶层", "data:text/html,<h1>x</h1>", false, false, navCancel},
		{"javascript: target=_blank", "javascript:document.title='x'", true, false, navCancel},
		{"blob:", "blob:http://127.0.0.1:8591/abc", true, false, navCancel},
		{"about:blank", "about:blank", true, false, navCancel},
		{"file:", "file:///etc/passwd", false, false, navCancel},
		{"应用协议", "x-apple.systempreferences:com.apple.preference.security", true, false, navCancel},
		{"ftp", "ftp://127.0.0.1/x", true, false, navCancel},
		{"内嵌框架照常（预告片）", "https://www.youtube-nocookie.com/embed/x", false, true, navAllow},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, navDecision(tc.url, tc.newWindow, tc.subframe, origin))
		})
	}
}
