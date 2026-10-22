package desktop

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// 网页层的导航规则（Windows 的 Go 实现）：用例与 nav_darwin_test.go 相同，两边的规则必须一致。
func TestDecideNav(t *testing.T) {
	const origin = "http://127.0.0.1:8591"
	for _, tc := range []struct {
		name      string
		url       string
		newWindow bool
		subframe  bool
		want      navAction
	}{
		{"本机界面", "http://127.0.0.1:8591/settings#about", false, false, actAllow},
		{"本机界面 target=_blank", "http://127.0.0.1:8591/entry?id=1", true, false, actLoadHere},
		{"主机名大小写不敏感", "HTTP://127.0.0.1:8591/", false, false, actAllow},
		{"别的端口不是我们", "http://127.0.0.1:8592/", false, false, actExternal},
		{"没写端口不是我们", "http://127.0.0.1/", false, false, actExternal},
		{"localhost 不是我们（首个地址用的是 127.0.0.1）", "http://localhost:8591/", true, false, actExternal},
		{"外链", "https://anilist.co/anime/1", false, false, actExternal},
		{"外链 target=_blank", "https://github.com/nagare-project/Nagare_Source", true, false, actExternal},
		{"邮件", "mailto:someone@example.com", true, false, actExternal},
		{"data: target=_blank", "data:text/html,<h1>x</h1>", true, false, actCancel},
		{"data: 顶层", "data:text/html,<h1>x</h1>", false, false, actCancel},
		{"javascript: target=_blank", "javascript:document.title='x'", true, false, actCancel},
		{"blob:", "blob:http://127.0.0.1:8591/abc", true, false, actCancel},
		{"about:blank", "about:blank", true, false, actCancel},
		{"file:", "file:///C:/Windows/win.ini", false, false, actCancel},
		{"应用协议", "ms-settings:privacy", true, false, actCancel},
		{"ftp", "ftp://127.0.0.1/x", true, false, actCancel},
		{"解析不了", "http://[::1", false, false, actCancel},
		{"没有协议", "/relative/path", false, false, actCancel},
		{"内嵌框架照常（预告片）", "https://www.youtube-nocookie.com/embed/x", false, true, actAllow},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, decideNav(tc.url, tc.newWindow, tc.subframe, origin))
		})
	}
}
