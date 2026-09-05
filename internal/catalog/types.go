// Package catalog 提供 Discover 使用的公开 AniList 元数据，不包含资源或磁力查询。
package catalog

// Media 是前端展示契约；账号进度由独立收藏接口提供。
type Media struct {
	ID              int         `json:"id"`
	Title           string      `json:"title"`
	TitleEnglish    string      `json:"titleEnglish,omitempty"`
	TitleNative     string      `json:"titleNative,omitempty"`
	Cover           string      `json:"cover,omitempty"`
	Banner          string      `json:"banner,omitempty"`
	TrailerID       string      `json:"trailerId,omitempty"`
	Year            int         `json:"year,omitempty"`
	Season          string      `json:"season,omitempty"`
	Episodes        *int        `json:"episodes"`
	Watched         int         `json:"watched"`
	Score           int         `json:"score,omitempty"`
	Genres          []string    `json:"genres"`
	Description     string      `json:"description,omitempty"`
	Status          string      `json:"status,omitempty"`
	Format          string      `json:"format,omitempty"`
	Duration        int         `json:"duration,omitempty"`
	Source          string      `json:"source,omitempty"`
	Studios         []string    `json:"studios,omitempty"`
	StartDate       string      `json:"startDate,omitempty"`
	NextAiring      *Airing     `json:"nextAiring,omitempty"`
	RecentAiring    *Airing     `json:"recentAiring,omitempty"`
	Relations       []Relation  `json:"relations,omitempty"`
	Recommendations []Media     `json:"recommendations,omitempty"`
	Characters      []Character `json:"characters,omitempty"`
	Rankings        []Ranking   `json:"rankings,omitempty"`
}
type Airing struct {
	Episode int   `json:"episode"`
	At      int64 `json:"at"`
}
type Relation struct {
	Type  string `json:"type"`
	Media Media  `json:"media"`
}
type Character struct {
	Name       string `json:"name"`
	Image      string `json:"image"`
	Role       string `json:"role"`
	Actor      string `json:"actor"`
	ActorImage string `json:"actorImage"`
}
type Ranking struct {
	Rank    int    `json:"rank"`
	Type    string `json:"type"`
	Context string `json:"context"`
	Year    int    `json:"year"`
	Season  string `json:"season"`
}

type rawMedia struct {
	Type                   string                                   `json:"type"`
	CountryOfOrigin        string                                   `json:"countryOfOrigin"`
	IsAdult                bool                                     `json:"isAdult"`
	ID                     int                                      `json:"id"`
	Title                  struct{ Romaji, English, Native string } `json:"title"`
	CoverImage             struct{ ExtraLarge, Large string }       `json:"coverImage"`
	BannerImage            string                                   `json:"bannerImage"`
	Trailer                *struct{ ID, Site string }               `json:"trailer"`
	SeasonYear             int                                      `json:"seasonYear"`
	Season                 string                                   `json:"season"`
	Episodes               *int                                     `json:"episodes"`
	MeanScore              int                                      `json:"meanScore"`
	Genres                 []string                                 `json:"genres"`
	Description            string                                   `json:"description"`
	Status, Format, Source string
	Duration               int
	StartDate              struct{ Year, Month, Day int }
	NextAiringEpisode      *struct {
		Episode  int
		AiringAt int64
	}
	Studios   struct{ Nodes []struct{ Name string } }
	Relations struct {
		Edges []struct {
			RelationType string
			Node         *rawMedia
		}
	}
	Recommendations struct {
		Nodes []struct{ MediaRecommendation *rawMedia }
	}
	Characters struct {
		Edges []struct {
			Role string
			Node struct {
				Name  struct{ Full string }
				Image struct{ Large string }
			}
			VoiceActors []struct {
				Name  struct{ Full string }
				Image struct{ Large string }
			}
		}
	}
	Rankings []Ranking
}
