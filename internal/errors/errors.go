// Package errs 是 nagare 的失败分类（决议 CQ3）。
//
// 目录名按仓库形状约定叫 internal/errors，包名用 errs 以免与标准库 errors 相互遮蔽。
//
// 每个用户可见的失败都要回答两个问题：用户看到什么（UserMsg）、能做什么（Recovery）。
// 「成功返回空结果」类失败（规则解出 0 字段 ≠ 无结果）不走 error 通道，
// 由各业务层显式建模 —— 这里只管真 error 的分类与展示。
package errs

import "fmt"

// Category 是失败大类，决定 HTTP 状态码与界面处理方式。
type Category string

const (
	// CategoryNetwork：网络不可达/超时，稍后重试即可，不影响本地功能。
	CategoryNetwork Category = "network"
	// CategoryAuth：会话过期或未登录，需要用户重新登录。
	CategoryAuth Category = "auth"
	// CategoryUpstream：上游服务（animego/dandanplay）报错或限流。
	CategoryUpstream Category = "upstream"
	// CategoryPlayback：mpv 进程或 IPC 层失败。
	CategoryPlayback Category = "playback"
	// CategoryFS：本地文件问题（被移走/改名/无权限）。
	CategoryFS Category = "fs"
	// CategoryInput：调用方输入不合法。
	CategoryInput Category = "input"
	// CategoryInternal：nagare 自身缺陷，兜底类。
	CategoryInternal Category = "internal"
)

// E 是带分类与用户提示的错误。
type E struct {
	Category Category
	Op       string // 出错的操作（英文标识，日志用）
	UserMsg  string // 用户看到什么（中文）
	Recovery string // 用户能做什么（中文，可空）
	Err      error  // 底层错误（可空）
}

func (e *E) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s [%s]: %s: %v", e.Op, e.Category, e.UserMsg, e.Err)
	}
	return fmt.Sprintf("%s [%s]: %s", e.Op, e.Category, e.UserMsg)
}

func (e *E) Unwrap() error { return e.Err }

// UserFacing 拼出给界面的完整中文提示（UserMsg + Recovery）。
func (e *E) UserFacing() string {
	if e.Recovery == "" {
		return e.UserMsg
	}
	return e.UserMsg + "。" + e.Recovery
}

// New 构造一个不带底层错误的分类错误。
func New(cat Category, op, userMsg, recovery string) *E {
	return &E{Category: cat, Op: op, UserMsg: userMsg, Recovery: recovery}
}

// Wrap 包装底层错误并分类。
func Wrap(cat Category, op, userMsg, recovery string, err error) *E {
	return &E{Category: cat, Op: op, UserMsg: userMsg, Recovery: recovery, Err: err}
}
