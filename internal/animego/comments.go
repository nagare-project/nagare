// 弹幕拉取：GET /api/dandanplay/comments/{episodeId}（公开端点，无需登录）。
// episodeId 来自 Match 结果的 EpisodeRef.DandanEpisodeID。
// Comment 类型直接复用 internal/danmaku 的定义（cid/p/m 的 JSON 形状由它约定），
// 拉回来的切片可以原样交给 danmaku 包转 ASS。
package animego

import (
	"context"
	"fmt"
	"net/http"

	"github.com/nagare-project/nagare/internal/danmaku"
)

// Comments 拉取一集的全部弹幕。
// 上游 miss 时服务端降级为 200 的 {"count":0,"comments":[]} —— 返回空切片、
// nil 错误，这是正常结果；「这集没弹幕」和「弹幕服务坏了」必须分得开。
func (c *Client) Comments(ctx context.Context, episodeID int64) ([]danmaku.Comment, error) {
	const op = "comments"
	if episodeID <= 0 {
		return nil, &Error{Kind: ErrBadRequest, Op: op, Err: fmt.Errorf("episodeId 必须为正数，收到 %d", episodeID)}
	}

	path := fmt.Sprintf("/api/dandanplay/comments/%d", episodeID)
	res, err := c.send(ctx, op, http.MethodGet, path, nil, nil)
	if err != nil {
		return nil, err
	}
	if err := classifyStatus(op, res, false); err != nil {
		return nil, err
	}

	// 响应是裸结构（没有 data 信封）：{"count":123,"comments":[...]}。
	var wire struct {
		Count    int               `json:"count"`
		Comments []danmaku.Comment `json:"comments"`
	}
	if err := decodeJSON(op, res.body, &wire); err != nil {
		return nil, err
	}
	if wire.Comments == nil {
		// comments 为 null/缺席等价于空列表，给调用方一个可直接 range 的切片。
		return []danmaku.Comment{}, nil
	}
	return wire.Comments, nil
}
