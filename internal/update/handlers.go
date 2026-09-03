package update

// 本文件是更新检查的 HTTP 端点，由 main 挂进 /api/* 的鉴权链（Host 白名单 + token + CSRF 头）。
// 三个端点都返回 View，走 httpserver 的统一信封。

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"time"

	errs "github.com/nagare-project/nagare/internal/errors"
	"github.com/nagare-project/nagare/internal/httpserver"
)

// maxRequestBytes 是请求体上限（只有一个布尔字段，给足余量即可）。
const maxRequestBytes = 4 << 10

// Register 注册三个端点：
//
//	GET  /api/update          当前快照（不联网）
//	POST /api/update/check    强制联网检查一次
//	POST /api/update/config   {"enabled": bool} 开关后台检查
//	POST /api/update/apply    下载 → 验签 → 校验哈希 → 解包 → 替换（阻塞，随后进程重启）
func (c *Checker) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/update", c.handleGet)
	mux.HandleFunc("POST /api/update/check", c.handleCheck)
	mux.HandleFunc("POST /api/update/config", c.handleConfig)
	mux.HandleFunc("POST /api/update/apply", c.handleApply)
}

// restartDelay 是「响应写完」到「真的重启」之间的间隔，让响应先送达浏览器
// —— 界面要靠这个响应里的版本号去轮询 /api/health 判断新版本起来没有。
const restartDelay = 300 * time.Millisecond

// handleApply 执行一次自更新。
//
// 这是个【阻塞】请求：要下载 20–120MB 再验签、校验哈希、解包、替换，几十秒到几分钟
// 都正常。成功后写响应，再延迟一小会儿触发重启 —— 重启走 main 装配的那条优雅退出
// 路径（停播放、回写进度、关 HTTP），不是在这里直接 exec。
func (c *Checker) handleApply(w http.ResponseWriter, r *http.Request) {
	if c.selfUpdate == nil {
		httpserver.WriteError(w, http.StatusServiceUnavailable,
			"这个版本不带一键更新，请到下载页手动更新")
		return
	}
	// 变量名避开内建的 cap：遮蔽内建函数在这种短函数里不显眼，但读起来很别扭。
	if capability := c.selfUpdate.Capability(); !capability.Supported {
		// 400 而不是 503：请求本身没毛病，是这个安装方式不支持，
		// 原因（包管理器装的 / 目录不可写 / 没配公钥）在 Reason 里。
		httpserver.WriteError(w, http.StatusBadRequest, capability.Reason)
		return
	}
	v := c.View()
	if !v.Available {
		httpserver.WriteError(w, http.StatusBadRequest, "当前已经是最新版本")
		return
	}

	// 先停播放：Windows 上 mpv 活着时安装目录里的 mpv\ 换不动，整次更新会回滚。
	// 放在下载【之前】而不是替换之前 —— 下载要几分钟，让用户在这几分钟里继续看
	// 一集只会让替换阶段撞上一个刚开始播的 mpv。
	if c.beforeApply != nil {
		c.beforeApply()
	}

	// 脱离请求 ctx：用户关掉页面不该把一次装到一半的更新掐断
	// —— 那正是最容易留下半个版本的时刻。时长由 selfupdate 自己的超时兜底。
	if err := c.selfUpdate.Apply(context.WithoutCancel(r.Context()), v.Latest); err != nil {
		httpserver.WriteError(w, applyStatus(err), userMessage(err))
		return
	}
	httpserver.WriteJSON(w, http.StatusOK, map[string]string{"version": v.Latest})
	log.Printf("update: 已装好 %s，准备重启", v.Latest)
	if c.onRestart != nil {
		time.AfterFunc(restartDelay, c.onRestart)
	}
}

// applyStatus 把自更新的失败分类映射成状态码。
func applyStatus(err error) int {
	var ce *errs.E
	if !errors.As(err, &ce) {
		return http.StatusInternalServerError
	}
	switch ce.Category {
	case errs.CategoryNetwork, errs.CategoryUpstream:
		return http.StatusBadGateway
	case errs.CategoryInput:
		return http.StatusBadRequest
	default:
		return http.StatusInternalServerError
	}
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
