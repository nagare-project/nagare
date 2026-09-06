package animego

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
)

// ListEntry 使用同一账号的收藏接口，保留作品信息以免列表产生逐卡请求。
type ListEntry struct {
	AnilistID       int    `json:"anilistId"`
	Status          string `json:"status"`
	CurrentEpisode  int    `json:"currentEpisode"`
	Score           *int   `json:"score"`
	WatchedEpisodes []int  `json:"watchedEpisodes,omitempty"`
	TitleRomaji     string `json:"titleRomaji"`
	TitleChinese    string `json:"titleChinese"`
	TitleNative     string `json:"titleNative"`
	CoverImageURL   string `json:"coverImageUrl"`
	BannerImageURL  string `json:"bannerImageUrl"`
	Episodes        *int   `json:"episodes"`
	Season          string `json:"season"`
	SeasonYear      int    `json:"seasonYear"`
	AnimeStatus     string `json:"animeStatus"`
	LastWatchedAt   string `json:"lastWatchedAt"`
}

// ListEditError 保留已确认完成的操作；网络错误时不把最后一次请求误报为确定失败。
type ListEditError struct {
	Err           error
	Removed       []int
	Remaining     []int
	LastConfirmed int
	Stage         string
}

func (e *ListEditError) Error() string {
	return fmt.Sprintf("收藏未完全保存：已确认撤销 %v；尚未确认撤销 %v；上次确认进度为第 %d 集。%s未完成，请刷新后核对：%v", e.Removed, e.Remaining, e.LastConfirmed, e.Stage, e.Err)
}
func (e *ListEditError) Unwrap() error { return e.Err }
func (c *Client) listSend(ctx context.Context, op, method, path string, body []byte) (*httpResult, error) {
	if err := c.waitListTurn(ctx); err != nil {
		return nil, err
	}
	return c.authedSend(ctx, op, method, path, body)
}
func (c *Client) ListEntries(ctx context.Context) ([]ListEntry, error) {
	ctx = c.accountContext(ctx)
	res, err := c.listSend(ctx, "list", http.MethodGet, "/api/subscriptions", nil)
	if err != nil {
		return nil, err
	}
	if err = classifyStatus("list", res, true); err != nil {
		return nil, err
	}
	var env struct{ Data *[]ListEntry }
	if err = decodeJSON("list", res.body, &env); err != nil {
		return nil, err
	}
	if env.Data == nil {
		return nil, &Error{Kind: ErrDecode, Op: "list", Err: fmt.Errorf("收藏响应缺少 data 数组")}
	}
	return *env.Data, nil
}
func (c *Client) GetListEntry(ctx context.Context, id int) (*ListEntry, error) {
	if id < 1 || id > 2147483647 {
		return nil, &Error{Kind: ErrBadRequest, Op: "list", Err: fmt.Errorf("无效的作品 ID")}
	}
	ctx = c.accountContext(ctx)
	res, err := c.listSend(ctx, "list", http.MethodGet, fmt.Sprintf("/api/subscriptions/%d", id), nil)
	if err != nil {
		return nil, err
	}
	if err = classifyStatus("list", res, true); err != nil {
		return nil, err
	}
	var env struct{ Data json.RawMessage }
	if err = decodeJSON("list", res.body, &env); err != nil {
		return nil, err
	}
	if len(env.Data) == 0 {
		return nil, &Error{Kind: ErrDecode, Op: "list", Err: fmt.Errorf("收藏响应缺少 data")}
	}
	var entry *ListEntry
	if err := decodeJSON("list", env.Data, &entry); err != nil {
		return nil, err
	}
	return entry, nil
}
func (c *Client) SaveListEntry(ctx context.Context, id int, status string, progress int, score *int) error {
	const op = "save-list"
	ctx = c.accountContext(ctx)
	if id < 1 || id > 2147483647 || progress < 0 || progress > 5000 || (score != nil && (*score < 1 || *score > 10)) {
		return &Error{Kind: ErrBadRequest, Op: op, Err: fmt.Errorf("无效的作品、进度或评分")}
	}
	switch status {
	case "watching", "completed", "plan_to_watch", "dropped":
	default:
		return &Error{Kind: ErrBadRequest, Op: op, Err: fmt.Errorf("无效的收藏状态")}
	}
	current, err := c.GetListEntry(ctx, id)
	if err != nil {
		return err
	}
	path := fmt.Sprintf("/api/subscriptions/%d", id)
	if current != nil && current.Episodes != nil && progress > *current.Episodes {
		return &Error{Kind: ErrBadRequest, Op: op, Err: fmt.Errorf("进度超过已知总集数")}
	}
	if current == nil {
		body, err := marshalJSON(op, map[string]any{"anilistId": id, "status": status, "ifAbsent": true})
		if err != nil {
			return err
		}
		res, err := c.listSend(ctx, op, http.MethodPost, "/api/subscriptions", body)
		if err != nil {
			return err
		}
		if err = classifyStatus(op, res, true); err != nil {
			return err
		}
	}
	removed, pending := []int{}, []int{}
	last := 0
	if current != nil {
		last = current.CurrentEpisode
		if progress < last {
			seen := map[int]bool{}
			for _, ep := range current.WatchedEpisodes {
				if ep > progress && !seen[ep] {
					pending = append(pending, ep)
					seen[ep] = true
				}
			}
			sort.Sort(sort.Reverse(sort.IntSlice(pending)))
			// 缺少逐集集合时不能凭最大集号虚构要撤销的集数。
			if len(pending) == 0 {
				return &Error{Kind: ErrDecode, Op: op, Err: fmt.Errorf("服务未返回可撤销的逐集进度，请刷新后重试")}
			}
		}
	}
	for len(pending) > 0 {
		ep := pending[0]
		res, err := c.listSend(ctx, op, http.MethodDelete, fmt.Sprintf("%s/episodes/%d", path, ep), nil)
		if err == nil {
			err = classifyStatus(op, res, true)
		}
		if err != nil {
			return &ListEditError{err, removed, pending, last, "撤销进度"}
		}
		removed = append(removed, ep)
		pending = pending[1:]
		var result struct {
			Data *struct {
				CurrentEpisode *int `json:"currentEpisode"`
			} `json:"data"`
		}
		if err := decodeJSON(op, res.body, &result); err != nil {
			return &ListEditError{err, removed, pending, last, "确认撤销后进度"}
		}
		if result.Data == nil || result.Data.CurrentEpisode == nil {
			err := &Error{Kind: ErrDecode, Op: op, Err: fmt.Errorf("撤销响应缺少 currentEpisode")}
			return &ListEditError{err, removed, pending, last, "确认撤销后进度"}
		}
		last = *result.Data.CurrentEpisode
	}
	body, err := marshalJSON(op, map[string]any{"status": status, "currentEpisode": progress, "score": score})
	if err != nil {
		return &ListEditError{err, removed, pending, last, "更新状态与评分"}
	}
	res, err := c.listSend(ctx, op, http.MethodPatch, path, body)
	if err == nil {
		err = classifyStatus(op, res, true)
	}
	if err != nil {
		return &ListEditError{err, removed, pending, last, "更新状态与评分"}
	}
	return nil
}
func (c *Client) DeleteListEntry(ctx context.Context, id int) error {
	if id < 1 || id > 2147483647 {
		return &Error{Kind: ErrBadRequest, Op: "delete-list", Err: fmt.Errorf("无效的作品 ID")}
	}
	ctx = c.accountContext(ctx)
	res, err := c.listSend(ctx, "delete-list", http.MethodDelete, fmt.Sprintf("/api/subscriptions/%d", id), nil)
	if err != nil {
		return err
	}
	return classifyStatus("delete-list", res, true)
}
