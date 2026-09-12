// Package sourceplugin 管理用户安装的 Nagare Source 子进程，并在不信任
// 子进程及其输出的前提下消费 Plugin API v1。
package sourceplugin

import "context"

const (
	LaunchProtocol = "nagare-plugin-launch/v1"
	ProtocolV1     = 1
)

type LaunchConfig struct {
	Executable string
	Root       string
	Version    string
	// Arguments 只供进程级测试使用。生产环境保持 nil，由 Manager 添加固定且
	// 不经过 shell 的 serve 参数。
	Arguments []string
}

type ReadyEvent struct {
	Event    string `json:"event"`
	Protocol string `json:"protocol"`
	URL      string `json:"url"`
}

type Manifest struct {
	ID                   string   `json:"id"`
	Name                 string   `json:"name"`
	Version              string   `json:"version"`
	ProtocolVersions     []int    `json:"protocolVersions"`
	SourceSchemaVersions []int    `json:"sourceSchemaVersions"`
	Capabilities         []string `json:"capabilities"`
}

type Source struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Kind         string   `json:"kind"`
	Tier         int      `json:"tier"`
	Version      string   `json:"version"`
	Enabled      bool     `json:"enabled"`
	Status       string   `json:"status"`
	Capabilities []string `json:"capabilities"`
}

type Subject struct {
	IDs    map[string]string `json:"ids"`
	Titles []string          `json:"titles"`
	Season *int              `json:"season,omitempty"`
	Year   *int              `json:"year,omitempty"`
}

type Episode struct {
	ID       string  `json:"id,omitempty"`
	Number   string  `json:"number"`
	Absolute float64 `json:"absolute,omitempty"`
	AirDate  string  `json:"airDate,omitempty"`
	Title    string  `json:"title,omitempty"`
}

type Preferences struct {
	SubtitleLanguages   []string `json:"subtitleLanguages,omitempty"`
	MaxResolution       string   `json:"maxResolution,omitempty"`
	PreferredTransports []string `json:"preferredTransports,omitempty"`
	// Transports 是允许列表：非空时插件只运行能产出这些 transport 的来源
	// （磁力选集只要 torrent，跳过整队浏览器嗅探）。
	Transports []string `json:"transports,omitempty"`
}

type ResolveRequest struct {
	Schema      string      `json:"schema"`
	Subject     Subject     `json:"subject"`
	Episode     Episode     `json:"episode"`
	Preferences Preferences `json:"preferences,omitempty"`
}

type Match struct {
	Basis         []string `json:"basis"`
	SubjectTitle  string   `json:"subjectTitle,omitempty"`
	EpisodeNumber float64  `json:"episodeNumber,omitempty"`
	Evidence      string   `json:"evidence,omitempty"`
}

type Transport struct {
	Type       string            `json:"type"`
	URL        string            `json:"url,omitempty"`
	Headers    map[string]string `json:"headers,omitempty"`
	ExpiresAt  int64             `json:"expiresAt,omitempty"`
	Magnet     string            `json:"magnet,omitempty"`
	InfoHash   string            `json:"infoHash,omitempty"`
	TorrentURL string            `json:"torrentUrl,omitempty"`
	FileIndex  *int              `json:"fileIndex,omitempty"`
	Trackers   []string          `json:"trackers,omitempty"`
}

type Metadata struct {
	Resolution        string   `json:"resolution,omitempty"`
	SubtitleLanguages []string `json:"subtitleLanguages,omitempty"`
	Channel           string   `json:"channel,omitempty"`
	ChannelTier       *int     `json:"channelTier,omitempty"`
	Episode           float64  `json:"episode,omitempty"`
	Fansub            string   `json:"fansub,omitempty"`
	SizeBytes         int64    `json:"sizeBytes,omitempty"`
	Seeders           *int     `json:"seeders,omitempty"`
	PublishedAt       string   `json:"publishedAt,omitempty"`
	// Title 是来源上的原始发布标题（BT 条目）；字幕组、季数、清晰度都在里面。
	Title string `json:"title,omitempty"`
}

type Candidate struct {
	Schema          string    `json:"schema"`
	ID              string    `json:"id"`
	SourceID        string    `json:"sourceId"`
	Tier            int       `json:"tier"`
	MatchConfidence float64   `json:"matchConfidence"`
	Match           Match     `json:"match"`
	Transport       Transport `json:"transport"`
	Metadata        Metadata  `json:"metadata"`
}

type Event struct {
	Event      string     `json:"event"`
	Candidate  *Candidate `json:"candidate,omitempty"`
	SourceID   string     `json:"sourceId,omitempty"`
	Category   string     `json:"category,omitempty"`
	Message    string     `json:"message,omitempty"`
	Retryable  bool       `json:"retryable,omitempty"`
	Queried    int        `json:"queried,omitempty"`
	Succeeded  int        `json:"succeeded,omitempty"`
	Failed     int        `json:"failed,omitempty"`
	DurationMS int64      `json:"durationMs,omitempty"`
}

type Status struct {
	Phase     string    `json:"phase"`
	URL       string    `json:"url,omitempty"`
	Manifest  *Manifest `json:"manifest,omitempty"`
	Error     string    `json:"error,omitempty"`
	StartedAt *int64    `json:"startedAt,omitempty"`
}

type Runtime interface {
	Status() Status
	Sources(context.Context) ([]Source, error)
	Candidates(context.Context, ResolveRequest, func(Event) error) error
}
