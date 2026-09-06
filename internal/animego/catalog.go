package animego

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
)

// CatalogMedia 只接收目录及详情实际使用的字段；空标题、缺图和未知集数仍由投影层处理。
type CatalogMedia struct {
	AnilistID      int      `json:"anilistId"`
	Title          string   `json:"title"`
	TitleChinese   string   `json:"titleChinese"`
	TitleRomaji    string   `json:"titleRomaji"`
	TitleEnglish   string   `json:"titleEnglish"`
	TitleNative    string   `json:"titleNative"`
	CoverImageURL  string   `json:"coverImageUrl"`
	BannerImageURL string   `json:"bannerImageUrl"`
	Description    string   `json:"description"`
	DescriptionCn  string   `json:"descriptionCn"`
	Season         string   `json:"season"`
	SeasonYear     int      `json:"seasonYear"`
	Episodes       *int     `json:"episodes"`
	AverageScore   float64  `json:"averageScore"`
	BangumiScore   float64  `json:"bangumiScore"`
	Genres         []string `json:"genres"`
	Status         string   `json:"status"`
	Format         string   `json:"format"`
	Duration       int      `json:"duration"`
	Source         string   `json:"source"`
	StartDate      string   `json:"startDate"`
	TrailerID      string   `json:"trailerId"`
	TrailerSite    string   `json:"trailerSite"`
	Trailer        *struct {
		ID   string `json:"id"`
		Site string `json:"site"`
	} `json:"trailer"`
	Studios         []string           `json:"studios"`
	Relations       []CatalogRelation  `json:"relations"`
	Recommendations []CatalogMedia     `json:"recommendations"`
	Characters      []CatalogCharacter `json:"characters"`
	EpisodeTitles   []EpisodeTitle     `json:"episodeTitles"`
}

type CatalogRelation struct {
	CatalogMedia
	RelationType string `json:"relationType"`
}
type CatalogCharacter struct {
	NameCn             string `json:"nameCn"`
	NameJa             string `json:"nameJa"`
	NameEn             string `json:"nameEn"`
	ImageURL           string `json:"imageUrl"`
	Role               string `json:"role"`
	VoiceActorCn       string `json:"voiceActorCn"`
	VoiceActorJa       string `json:"voiceActorJa"`
	VoiceActorEn       string `json:"voiceActorEn"`
	VoiceActorImageURL string `json:"voiceActorImageUrl"`
}
type EpisodeTitle struct {
	Episode int    `json:"episode"`
	Name    string `json:"name"`
	NameCn  string `json:"nameCn"`
}

// ScheduleItem 保留 Unix 秒，不将上游服务时区作为用户时区。
type ScheduleItem struct {
	CatalogMedia
	ScheduleID int   `json:"scheduleId"`
	Episode    int   `json:"episode"`
	AiringAt   int64 `json:"airingAt"`
}
type ScheduleData struct {
	Today  string                    `json:"today"`
	Groups map[string][]ScheduleItem `json:"groups"`
}

// catalogGet 只读公开元数据，不能带入账号 token。错误和 data:null 都必须显式传回。
func catalogGet[T any](ctx context.Context, c *Client, path string) (T, error) {
	var zero T
	res, err := c.send(ctx, "catalog", http.MethodGet, path, nil, nil)
	if err != nil {
		return zero, err
	}
	if err = classifyStatus("catalog", res, false); err != nil {
		return zero, err
	}
	var env struct {
		Data *T `json:"data"`
	}
	if err = decodeJSON("catalog", res.body, &env); err != nil {
		return zero, err
	}
	if env.Data == nil {
		return zero, &Error{Kind: ErrDecode, Op: "catalog", Err: fmt.Errorf("目录响应缺少 data")}
	}
	return *env.Data, nil
}
func (c *Client) Trending(ctx context.Context) ([]CatalogMedia, error) {
	return catalogGet[[]CatalogMedia](ctx, c, "/api/anime/trending?limit=20")
}
func (c *Client) Seasonal(ctx context.Context, season string, year int) ([]CatalogMedia, error) {
	switch season {
	case "WINTER", "SPRING", "SUMMER", "FALL":
	default:
		return nil, &Error{Kind: ErrBadRequest, Op: "seasonal", Err: fmt.Errorf("无效季度")}
	}
	if year < 1900 || year > 2200 {
		return nil, &Error{Kind: ErrBadRequest, Op: "seasonal", Err: fmt.Errorf("无效年份")}
	}
	q := url.Values{"season": {season}, "year": {strconv.Itoa(year)}, "perPage": {"200"}, "page": {"1"}}
	return catalogGet[[]CatalogMedia](ctx, c, "/api/anime/seasonal?"+q.Encode())
}
func (c *Client) Gems(ctx context.Context) ([]CatalogMedia, error) {
	return catalogGet[[]CatalogMedia](ctx, c, "/api/anime/completed-gems?limit=20")
}
func (c *Client) YearlyTop(ctx context.Context, year int) ([]CatalogMedia, error) {
	return catalogGet[[]CatalogMedia](ctx, c, fmt.Sprintf("/api/anime/yearly-top?year=%d&limit=20", year))
}
func (c *Client) Schedule(ctx context.Context) (ScheduleData, error) {
	return catalogGet[ScheduleData](ctx, c, "/api/anime/schedule")
}
func (c *Client) Detail(ctx context.Context, id int) (CatalogMedia, error) {
	if id < 1 || id > 2147483647 {
		return CatalogMedia{}, &Error{Kind: ErrBadRequest, Op: "detail", Err: fmt.Errorf("无效作品 ID")}
	}
	return catalogGet[CatalogMedia](ctx, c, fmt.Sprintf("/api/anime/%d", id))
}
