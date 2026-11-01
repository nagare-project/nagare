package api

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
type SectionView struct {
	Key   string         `json:"key"`
	Title string         `json:"title"`
	Items []SummaryMedia `json:"items"`
	Error string         `json:"error,omitempty"`
}
type DiscoverView struct {
	Sections  []SectionView `json:"sections"`
	FetchedAt int64         `json:"fetchedAt"`
	Freshness *Freshness    `json:"freshness,omitempty"`
}
type AiringView struct {
	AnilistID int    `json:"anilistId"`
	Episode   int    `json:"episode"`
	AiringAt  int64  `json:"airingAt"`
	Title     string `json:"title"`
	Cover     string `json:"cover,omitempty"`
	Format    string `json:"format,omitempty"`
	InLibrary bool   `json:"inLibrary"`
}
type ScheduleView struct {
	Airings   []AiringView `json:"airings"`
	FetchedAt int64        `json:"fetchedAt"`
	Freshness *Freshness   `json:"freshness,omitempty"`
}
