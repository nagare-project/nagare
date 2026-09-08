package api

// SummaryMedia 是 API 投影；组件通过适配器维持自己的 MediaSummary 契约。
type SummaryMedia struct {
	AnilistID       int                `json:"anilistId"`
	Title           string             `json:"title"`
	TitleNative     string             `json:"titleNative,omitempty"`
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
}
