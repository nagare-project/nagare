package api

import "github.com/nagare-project/nagare/internal/artcache"

// SummaryMedia 是 API 投影；组件通过适配器维持自己的 MediaSummary 契约。
type SummaryMedia struct {
	AnilistID       int                `json:"anilistId"`
	Title           string             `json:"title"`
	TitleNative     string             `json:"titleNative,omitempty"`
	TitleRomaji     string             `json:"titleRomaji,omitempty"` // 只给磁力搜索：nyaa 与不少字幕组只用罗马音登记发布
	TitleEnglish    string             `json:"titleEnglish,omitempty"`
	Cover           string             `json:"cover,omitempty"`
	Banner          string             `json:"banner,omitempty"`
	TrailerID       string             `json:"trailerId,omitempty"`
	Year            int                `json:"year,omitempty"`
	Season          string             `json:"season,omitempty"`
	Episodes        *int               `json:"episodes"`
	Score           int                `json:"score,omitempty"`
	Genres          []string           `json:"genres"`
	Description     string             `json:"description,omitempty"`
	Status          string             `json:"status,omitempty"`
	Format          string             `json:"format,omitempty"`
	Duration        int                `json:"duration,omitempty"`
	Source          string             `json:"source,omitempty"`
	Studios         []string           `json:"studios,omitempty"`
	StartDate       string             `json:"startDate,omitempty"`
	NextAiring      *Airing            `json:"nextAiring,omitempty"`
	RecentAiring    *Airing            `json:"recentAiring,omitempty"`
	Relations       []Relation         `json:"relations,omitempty"`
	Recommendations []SummaryMedia     `json:"recommendations,omitempty"`
	Characters      []Character        `json:"characters,omitempty"`
	EpisodeTitles   []EpisodeTitleView `json:"episodeTitles,omitempty"`
	// Freshness 只在作品详情（GET /api/anime/{id}）的顶层出现，嵌套的关联 / 推荐不带
	Freshness *Freshness `json:"freshness,omitempty"`

	// PosterTones 是按封面色相算好的 sRGB 色阶；null = 界面用中性色（见 poster_tones.go）。
	PosterTones *artcache.PosterTones `json:"posterTones"`
}

// Freshness 只在目录数据不是刚从上游取回时出现：来自磁盘（上次运行留下的）、已过缓存时限、或上游连不上。
// 界面据此显示「离线 · 更新于 …」。
type Freshness struct {
	FetchedAt string `json:"fetchedAt"` // RFC 3339：这份数据真正从上游取回的时间
	Stale     bool   `json:"stale"`     // 超过缓存时限，后台正在刷新（稍后重新请求可拿到新的）
	Offline   bool   `json:"offline"`   // 最近一次向上游刷新失败，仍在显示上一份
}
type Airing struct {
	Episode int   `json:"episode"`
	At      int64 `json:"at"`
}
type Relation struct {
	Type  string       `json:"type"`
	Media SummaryMedia `json:"media"`
}
type Character struct {
	Name       string `json:"name"`
	Image      string `json:"image"`
	Role       string `json:"role"`
	Actor      string `json:"actor"`
	ActorImage string `json:"actorImage"`
}
type EpisodeTitleView struct {
	Episode int    `json:"episode"`
	Title   string `json:"title"`
}

// DiscoverView 是 GET /api/discover：发现页要的全部原料。卡片上的文字（「TV · 连载中」「周日 19:00
// 更新第 14 集」）与分组（横幅取前五、本季高分取前十二、按色调分组、今日更新）都由前端拼
// （frontend/src/lib/home/）：其中一半随时钟变（哪集已播、「现在」在哪），服务端算了也得重算。
type DiscoverView struct {
	Season string `json:"season"` // 本季：WINTER / SPRING / SUMMER / FALL（按本机月份）
	Year   int    `json:"year"`   // 本季的年份，也是年度榜的年份
	// Seasonal 是本季全部作品，按 AniList 评分从高到低（上游的顺序）；SeasonTotal 是这一季一共几部
	// （与 Seasonal.Items 同数；上游一页最多 200 部，一季通常一百部上下）。
	Seasonal    HomeSection[HomeMedia]   `json:"seasonal"`
	SeasonTotal int                      `json:"seasonTotal"`
	Trending    HomeSection[RankedMedia] `json:"trending"` // 追番人数最多的十部
	Gems        HomeSection[HomeMedia]   `json:"gems"`     // 已完结的高分作品，随机一批（换一批见 GET /api/discover/gems）
	Yearly      HomeSection[HomeMedia]   `json:"yearly"`   // 本年度按 AniList 评分的前十
	Schedule    HomeSection[HomeAiring]  `json:"schedule"` // 近七日的放送安排，按时间先后
	FetchedAt   int64                    `json:"fetchedAt"`
	Freshness   *Freshness               `json:"freshness,omitempty"`
}

// HomeSection 是发现页的一个板块。读取失败时 Items 为空、Error 写明原因，界面只藏这一块。
type HomeSection[T any] struct {
	Items []T    `json:"items"`
	Error string `json:"error,omitempty"`
}

// HomeMedia 是发现页一张卡片用到的字段，比 SummaryMedia 少得多。
type HomeMedia struct {
	AnilistID   int    `json:"anilistId"`
	Title       string `json:"title"`
	TitleNative string `json:"titleNative,omitempty"`
	// TitleRomaji / TitleEnglish 不显示，只给磁力搜索（与 SummaryMedia 同一个理由：不少字幕组只用罗马音登记发布）
	TitleRomaji  string `json:"titleRomaji,omitempty"`
	TitleEnglish string `json:"titleEnglish,omitempty"`
	Cover        string `json:"cover,omitempty"`
	Banner       string `json:"banner,omitempty"`
	// Hue 是目录给的封面主色换算出的 OKLCH 色相角（度，一位小数）；null = 没有可信色相，界面用中性色（见 accent_hue.go）
	Hue          *float64 `json:"hue"`
	Score        int      `json:"score,omitempty"`        // AniList 评分，0–100
	BangumiScore float64  `json:"bangumiScore,omitempty"` // Bangumi 评分，0–10（未取整）
	Episodes     *int     `json:"episodes"`
	Season       string   `json:"season,omitempty"` // WINTER / SPRING / SUMMER / FALL
	Year         int      `json:"year,omitempty"`
	Status       string   `json:"status,omitempty"`
	Format       string   `json:"format,omitempty"`
	Genres       []string `json:"genres"`
	Description  string   `json:"description,omitempty"` // 纯文本（去掉了 HTML）
}

// RankedMedia 是热门榜的一项：名次与追番人数。
type RankedMedia struct {
	HomeMedia
	Rank     int `json:"rank"`
	Watchers int `json:"watchers"`
}

// HomeAiring 是一次放送（今日更新的一张卡片，也是横幅与本季高分里「下一集什么时候播」的来源）。
type HomeAiring struct {
	ScheduleID int      `json:"scheduleId"` // 同一部作品一天可能播两集，卡片按它区分
	AnilistID  int      `json:"anilistId"`
	Episode    int      `json:"episode"`
	AiringAt   int64    `json:"airingAt"` // Unix 秒
	Title      string   `json:"title"`
	Cover      string   `json:"cover,omitempty"`
	Hue        *float64 `json:"hue"`
}

// GemsView 是 GET /api/discover/gems：「换一批」要的下一批完结佳作。
type GemsView struct {
	Items []HomeMedia `json:"items"`
}
type AiringView struct {
	AnilistID int    `json:"anilistId"`
	Episode   int    `json:"episode"`
	AiringAt  int64  `json:"airingAt"`
	Title     string `json:"title"`
	Cover     string `json:"cover,omitempty"`
	Format    string `json:"format,omitempty"`
	InLibrary bool   `json:"inLibrary"`

	PosterTones *artcache.PosterTones `json:"posterTones"`
}
type ScheduleView struct {
	Airings   []AiringView `json:"airings"`
	FetchedAt int64        `json:"fetchedAt"`
	Freshness *Freshness   `json:"freshness,omitempty"`
}
