package update

// 本文件是更新检查的 HTTP 端点，由 main 挂进 /api/* 的鉴权链（Host 白名单 + token + CSRF 头）。
// 三个端点都返回 View，走 httpserver 的统一信封。

import (
	"context"
	"encoding/json"
	"io"
	"net/http"

	"github.com/nagare-project/nagare/internal/httpserver"
)

// maxRequestBytes 是请求体上限（只有一个布尔字段，给足余量即可）。
const maxRequestBytes = 4 << 10

// Register 注册三个端点：
//
//	GET  /api/update          当前快照（不联网）
//	POST /api/update/check    强制联网检查一次
//	POST /api/update/config   {"enabled": bool} 开关后台检查
func (c *Checker) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/update", c.handleGet)
	mux.HandleFunc("POST /api/update/check", c.handleCheck)
	mux.HandleFunc("POST /api/update/config", c.handleConfig)
}

func (c *Checker) handleGet(w http.ResponseWriter, _ *http.Request) {
	httpserver.WriteJSON(w, http.StatusOK, c.View())
}

// handleCheck 是用户手动触发的检查：总是联网（force），且不受 enabled 开关限制。
// 检查失败也返回 200 —— 失败信息在 View.Error 里，界面按 View 渲染即可。
// 用 WithoutCancel 脱离请求生命周期：页面关掉不该把一次检查记成「网络失败」，
// 时长由 fetchTimeout 兜底。
func (c *Checker) handleCheck(w http.ResponseWriter, r *http.Request) {
	httpserver.WriteJSON(w, http.StatusOK, c.Check(context.WithoutCancel(r.Context()), true))
}

func (c *Checker) handleConfig(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Enabled *bool `json:"enabled"`
	}
	// 多读一字节以区分「正好到上限」与「超限被截断」：截断后的 JSON 解析失败会
	// 误报成「不是合法 JSON」，明确拒绝比让用户猜好。
	body, err := io.ReadAll(io.LimitReader(r.Body, maxRequestBytes+1))
	if err != nil {
		httpserver.WriteError(w, http.StatusBadRequest, "读取请求体失败")
		return
	}
	if len(body) > maxRequestBytes {
		httpserver.WriteError(w, http.StatusRequestEntityTooLarge, "请求体过大")
		return
	}
	if err := json.Unmarshal(body, &req); err != nil {
		httpserver.WriteError(w, http.StatusBadRequest, "请求体不是合法的 JSON")
		return
	}
	if req.Enabled == nil {
		httpserver.WriteError(w, http.StatusBadRequest, "缺少 enabled 字段")
		return
	}
	v, err := c.SetEnabled(*req.Enabled)
	if err != nil {
		httpserver.WriteError(w, http.StatusInternalServerError, userMessage(err))
		return
	}
	httpserver.WriteJSON(w, http.StatusOK, v)
}
