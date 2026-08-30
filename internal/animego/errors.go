// 错误分类（决议 CQ3）：调用方必须能区分「稍后重试」「重新登录」「等限速」
// 「别再试了」「接口变了」五种局面，才能做出正确的降级 —— 尤其是
// 「API 不可达 → 本地库全功能、弹幕标为不可用」这条失败模式，绝不能让播放跟着失败。
package animego

import "fmt"

// ErrKind 是本包所有错误的分类维度。
type ErrKind int

const (
	// ErrUnavailable 网络失败或服务端 5xx —— 暂时性问题，稍后重试可能恢复。
	ErrUnavailable ErrKind = iota + 1
	// ErrAuthExpired 会话过期或未登录 —— 需要用户重新登录才能继续。
	ErrAuthExpired
	// ErrRateLimited 撞上服务端限速（429）—— 等一等再试，不要立刻重试。
	ErrRateLimited
	// ErrBadRequest 服务端明确拒绝的 4xx —— 请求本身有问题，重试无意义。
	ErrBadRequest
	// ErrDecode 响应形状与契约不符 —— 多半是服务端接口变更，提示用户检查更新。
	ErrDecode
)

// String 给日志与测试失败信息用。
func (k ErrKind) String() string {
	switch k {
	case ErrUnavailable:
		return "unavailable"
	case ErrAuthExpired:
		return "auth-expired"
	case ErrRateLimited:
		return "rate-limited"
	case ErrBadRequest:
		return "bad-request"
	case ErrDecode:
		return "decode"
	default:
		return fmt.Sprintf("unknown(%d)", int(k))
	}
}

// Error 是本包所有失败的统一载体。调用方用 errors.As 取出后按 Kind 分流；
// Err 保留完整底层链路，errors.Is 依然可以命中 context.Canceled 之类的哨兵。
type Error struct {
	Kind ErrKind
	Op   string // 出错的操作，如 "login" / "match" / "comments" / "mark-watched"
	Err  error  // 底层原因；含服务端错误信封里的信息（若有）
}

// Error 输出中文描述，并告诉用户能做什么 —— 「错误不静默」的另一半是
// 错误信息必须可行动，而不是一串状态码。
func (e *Error) Error() string {
	var hint string
	switch e.Kind {
	case ErrUnavailable:
		hint = "animego 暂时不可达，稍后会自动恢复；本地播放不受影响"
	case ErrAuthExpired:
		hint = "登录已过期，请重新登录 animego 账号"
	case ErrRateLimited:
		hint = "请求过于频繁，请稍等一两分钟再试"
	case ErrBadRequest:
		hint = "服务端拒绝了这次请求；若持续出现，请检查 nagare 是否需要更新"
	case ErrDecode:
		hint = "响应格式与预期不符，可能服务端接口已变更，请检查 nagare 更新"
	default:
		hint = "未知错误"
	}
	if e.Err != nil {
		return fmt.Sprintf("animego %s: %s（%v）", e.Op, hint, e.Err)
	}
	return fmt.Sprintf("animego %s: %s", e.Op, hint)
}

// Unwrap 让 errors.Is/As 能继续沿链路向下找。
func (e *Error) Unwrap() error { return e.Err }
