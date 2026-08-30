// 观看进度回写（三条允许连线中唯一的「写」）。全部需要登录，
// 401 自动刷新由 authedSend 兜底。
package animego

import (
	"context"
	"errors"
	"fmt"
	"net/http"
)

// EnsureSubscription 确保番剧存在于用户的订阅列表（幂等）。
// ifAbsent 必须恒为 true：否则会把「已弃番/已看完」的状态覆盖回 watching ——
// 播放器只该在没订阅时补一条，绝不该改写用户手动设置的状态。
func (c *Client) EnsureSubscription(ctx context.Context, anilistID int) error {
	const op = "subscribe"
	if anilistID <= 0 {
		return &Error{Kind: ErrBadRequest, Op: op, Err: fmt.Errorf("anilistId 必须为正数，收到 %d", anilistID)}
	}
	body, err := marshalJSON(op, struct {
		AnilistID int    `json:"anilistId"`
		Status    string `json:"status"`
		IfAbsent  bool   `json:"ifAbsent"`
	}{AnilistID: anilistID, Status: "watching", IfAbsent: true})
	if err != nil {
		return err
	}

	res, err := c.authedSend(ctx, op, http.MethodPost, "/api/subscriptions", body)
	if err != nil {
		return err
	}
	return classifyStatus(op, res, true)
}

// MarkWatched 标记单集看过。服务端按单调递增合并，重复调用安全 ——
// 播放器可以在每次「看完一集」时无脑调用，不必先查状态。
func (c *Client) MarkWatched(ctx context.Context, anilistID, episode int) error {
	const op = "mark-watched"
	if anilistID <= 0 {
		return &Error{Kind: ErrBadRequest, Op: op, Err: fmt.Errorf("anilistId 必须为正数，收到 %d", anilistID)}
	}
	if episode <= 0 {
		return &Error{Kind: ErrBadRequest, Op: op, Err: errors.New("episode 必须为正数")}
	}

	path := fmt.Sprintf("/api/subscriptions/%d/episodes/%d", anilistID, episode)
	res, err := c.authedSend(ctx, op, http.MethodPut, path, nil)
	if err != nil {
		return err
	}
	return classifyStatus(op, res, true)
}
