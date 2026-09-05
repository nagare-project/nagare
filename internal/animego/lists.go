package animego

import (
	"context"
	"fmt"
	"net/http"
)

// ListEntry 使用现有账号的收藏接口，与 AniList 公开目录分离。
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
}

func (c *Client) ListEntries(ctx context.Context) ([]ListEntry, error) {
	res, err := c.authedSend(ctx, "list", http.MethodGet, "/api/subscriptions", nil)
	if err != nil {
		return nil, err
	}
	if err = classifyStatus("list", res, true); err != nil {
		return nil, err
	}
	var env struct{ Data []ListEntry }
	if err = decodeJSON("list", res.body, &env); err != nil {
		return nil, err
	}
	if env.Data == nil {
		env.Data = []ListEntry{}
	}
	return env.Data, nil
}
func (c *Client) SaveListEntry(ctx context.Context, id int, status string, progress int, score *int) error {
	const op = "save-list"
	if id < 1 || progress < 0 || progress > 5000 || (score != nil && (*score < 1 || *score > 10)) {
		return &Error{Kind: ErrBadRequest, Op: op, Err: fmt.Errorf("无效的作品、进度或评分")}
	}
	switch status {
	case "watching", "completed", "plan_to_watch", "dropped":
	default:
		return &Error{Kind: ErrBadRequest, Op: op, Err: fmt.Errorf("无效的收藏状态")}
	}
	path := fmt.Sprintf("/api/subscriptions/%d", id)
	get, err := c.authedSend(ctx, op, http.MethodGet, path, nil)
	if err != nil {
		return err
	}
	if err = classifyStatus(op, get, true); err != nil {
		return err
	}
	var current struct{ Data *ListEntry }
	if err = decodeJSON(op, get.body, &current); err != nil {
		return err
	}
	if current.Data == nil {
		body, _ := marshalJSON(op, map[string]any{"anilistId": id, "status": status, "ifAbsent": true})
		res, e := c.authedSend(ctx, op, http.MethodPost, "/api/subscriptions", body)
		if e != nil {
			return e
		}
		if e = classifyStatus(op, res, true); e != nil {
			return e
		}
	}
	// 账号服务的 PATCH 只增加高水位，减少进度必须明确撤销后面的逐集标记。
	if current.Data != nil && progress < current.Data.CurrentEpisode {
		for _, episode := range current.Data.WatchedEpisodes {
			if episode > progress {
				res, e := c.authedSend(ctx, op, http.MethodDelete, fmt.Sprintf("%s/episodes/%d", path, episode), nil)
				if e != nil {
					return e
				}
				if e = classifyStatus(op, res, true); e != nil {
					return e
				}
			}
		}
	}
	body, _ := marshalJSON(op, map[string]any{"status": status, "currentEpisode": progress, "score": score})
	res, err := c.authedSend(ctx, op, http.MethodPatch, path, body)
	if err != nil {
		return err
	}
	return classifyStatus(op, res, true)
}
func (c *Client) DeleteListEntry(ctx context.Context, id int) error {
	if id < 1 {
		return &Error{Kind: ErrBadRequest, Op: "delete-list", Err: fmt.Errorf("无效的作品 ID")}
	}
	res, err := c.authedSend(ctx, "delete-list", http.MethodDelete, fmt.Sprintf("/api/subscriptions/%d", id), nil)
	if err != nil {
		return err
	}
	return classifyStatus("delete-list", res, true)
}
