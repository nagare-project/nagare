package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/nagare-project/nagare/internal/httpserver"
)

// EpisodeMetadata 只包含公开的逐集资料；图片仍经过本机登记与缓存。
type EpisodeMetadata struct {
	Episode     int    `json:"episode"`
	Title       string `json:"title"`
	Image       string `json:"image,omitempty"`
	Description string `json:"description,omitempty"`
	Duration    int    `json:"duration,omitempty"`
	AirDate     string `json:"airDate,omitempty"`
	AiredAt     string `json:"airedAt,omitempty"`
}

type episodeMetadataService struct {
	client   *http.Client
	endpoint string
	art      *RemoteArt
	cache    *readCache
}

func newEpisodeMetadataService(art *RemoteArt) *episodeMetadataService {
	return &episodeMetadataService{
		client:   &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }},
		endpoint: "https://api.ani.zip/v1/episodes?anilist_id=", art: art, cache: newReadCache(),
	}
}

// view 单独加载截图，不阻塞作品详情，也不向元数据服务发送任何账号凭据。
func (s *episodeMetadataService) view(ctx context.Context, id int) ([]EpisodeMetadata, error) {
	rows, err := cachedRead(ctx, s.cache, strconv.Itoa(id), 6*time.Hour, func(ctx context.Context) ([]EpisodeMetadata, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.endpoint+strconv.Itoa(id), nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Accept", "application/json")
		res, err := s.client.Do(req)
		if err != nil {
			return nil, err
		}
		defer res.Body.Close()
		if res.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("episode metadata: HTTP %d", res.StatusCode)
		}
		body, err := io.ReadAll(io.LimitReader(res.Body, 4<<20+1))
		if err != nil {
			return nil, err
		}
		if len(body) > 4<<20 {
			return nil, fmt.Errorf("episode metadata too large")
		}
		var data struct {
			Mappings struct {
				AnilistID int `json:"anilist_id"`
			} `json:"mappings"`
			Episodes map[string]struct {
				Title      map[string]string `json:"title"`
				Image      string            `json:"image"`
				Overview   string            `json:"overview"`
				Runtime    int               `json:"runtime"`
				AirDate    string            `json:"airDate"`
				AirDateUTC string            `json:"airDateUtc"`
			} `json:"episodes"`
		}
		if err := json.Unmarshal(body, &data); err != nil {
			return nil, err
		}
		if data.Mappings.AnilistID != id {
			return nil, fmt.Errorf("episode metadata ID mismatch")
		}
		out := []EpisodeMetadata{}
		for key, ep := range data.Episodes {
			number, err := strconv.Atoi(key)
			// 正片编号来自映射键，不能用 TVDB 季内集号去关联另一季的文件。
			if err != nil || number < 1 {
				continue
			}
			out = append(out, EpisodeMetadata{Episode: number, Title: firstText(ep.Title["zh-Hans"], ep.Title["zh"], ep.Title["en"], ep.Title["ja"]), Image: ep.Image, Description: plainDescription(ep.Overview), Duration: ep.Runtime, AirDate: ep.AirDate, AiredAt: ep.AirDateUTC})
		}
		sort.Slice(out, func(i, j int) bool { return out[i].Episode < out[j].Episode })
		return out, nil
	})
	if err != nil {
		return nil, err
	}
	// 每次响应都重新登记，避免图片 LRU 淘汰后还返回无法使用的键。
	out := append([]EpisodeMetadata{}, rows...)
	for i := range out {
		out[i].Image = s.art.Register(out[i].Image)
	}
	return out, nil
}

func (h *Handler) catalogEpisodes(w http.ResponseWriter, r *http.Request) {
	id, ok := catalogID(w, r)
	if !ok {
		return
	}
	rows, err := h.episodes.view(r.Context(), id)
	if err != nil {
		httpserver.WriteError(w, http.StatusBadGateway, "逐集图片暂时无法加载，请重试")
		return
	}
	httpserver.WriteJSON(w, http.StatusOK, rows)
}
