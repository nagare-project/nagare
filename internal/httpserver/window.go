package httpserver

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// 第二次启动：用户在 nagare 已经跑着的时候又启动了一次（终端里再敲一遍、双击了另一份二进制）。
// 新进程探到已有实例（Probe）之后，请它把桌面窗口提到前面；它没有窗口（浏览器模式、没有窗口的平台、
// 不认识这个端点的旧版本）就由新进程照旧开浏览器。新进程随后退出，界面始终只有已有实例的那一个。
//
// 端点挂在 /api/* 的鉴权链之内：Host 白名单 + token + 变更操作的自定义头（A4），与界面里的「退出」同等对待
// —— 恶意网页拿不到 token，就连「把窗口弹到前面」这种骚扰也做不到。新进程的 token 从同一个配置目录读。
//
// 平台无关：窗口怎么提到前面由 Options.ShowWindow 决定（macOS / Windows 的 internal/desktop.Show），
// 这里只管协议。macOS 上从访达 / 启动台再开一次 .app 根本不会起第二个进程，系统把「重新打开」交给已有
// 实例（applicationShouldHandleReopen，见 internal/tray 的 dock_darwin.m），不经过这里。

// ShowWindowPath 是「把桌面窗口提到前面」的端点（POST，无请求体）。
// 成功信封的 data 是 {"shown": bool}：true 表示窗口已提到前面；false 表示这个实例没有桌面窗口。
const ShowWindowPath = "/api/window/show"

// showWindowMaxBody 限制读取的响应体：信封只有几十字节。
const showWindowMaxBody = 4096

// ShowWindowFunc 把桌面窗口提到前面（第一次调用时建窗口）；返回假表示这个实例没有桌面窗口。
// 在 HTTP 处理器的 goroutine 上调，实现自己切到界面线程，且不能阻塞到窗口真的显示出来。
type ShowWindowFunc func() bool

// handleShowWindow 处理第二次启动的请求。没有窗口是一种状态而不是失败：200 + shown=false。
func (s *Server) handleShowWindow(w http.ResponseWriter, _ *http.Request) {
	shown := false
	if s.opts.ShowWindow != nil {
		shown = s.opts.ShowWindow()
	}
	writeJSON(w, http.StatusOK, map[string]bool{"shown": shown})
}

// RequestShowWindow 请 127.0.0.1:port 上已在运行的实例把桌面窗口提到前面。
// shown 为假且 err 为空：对端没有窗口，或是不认识这个端点的旧版本（404）—— 调用方退回开浏览器。
// err 非空：请求本身出了问题（超时、被拒、回的不是我们的信封），调用方同样退回开浏览器，并记下原因。
//
// 同 Probe：会把 token 发给应答方，调用前必须先过 HasInstance 与 Probe（ProbeRunning）。
func RequestShowWindow(port int, token string) (shown bool, err error) {
	client := &http.Client{Timeout: probeTimeout}
	req, err := http.NewRequest(http.MethodPost, fmt.Sprintf("http://127.0.0.1:%d%s", port, ShowWindowPath), nil)
	if err != nil {
		return false, fmt.Errorf("构造请求失败：%w", err)
	}
	req.Header.Set(TokenHeader, token)
	resp, err := client.Do(req)
	if err != nil {
		return false, fmt.Errorf("请求已有实例失败：%w", err)
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		return false, nil // 旧版本：没有这个端点，也就没有桌面窗口
	default:
		return false, fmt.Errorf("已有实例返回 %d", resp.StatusCode)
	}
	var env struct {
		Success bool `json:"success"`
		Data    struct {
			Shown bool `json:"shown"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, showWindowMaxBody)).Decode(&env); err != nil {
		return false, fmt.Errorf("已有实例的响应读不懂：%w", err)
	}
	if !env.Success {
		return false, fmt.Errorf("已有实例拒绝了请求")
	}
	return env.Data.Shown, nil
}
