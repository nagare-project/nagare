package sourceplugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

// 按作品搜全部 BT 发布（POST /v1/releases）。与 /v1/candidates 的区别：不带集号、不按集号筛，
// 一部作品搜一次，选集在 nagare 这边做 —— 与 animego 网站的磁力搜索同一个模型。

// CapabilityReleases 是插件 manifest 里宣告支持 /v1/releases 的能力名。
const CapabilityReleases = "bt_releases"

// ReleaseSearchSchema 是 /v1/releases 请求的 schema 常量。
const ReleaseSearchSchema = "nagare-release-search/v1"

const (
	maximumReleaseTitles = 4
	maximumReleases      = 5000
	maximumReleaseTitle  = 512
	// maximumReleaseEpisode 与 nagare 的集号上限一致（长篇连载过千集，再大就不是集号了）
	maximumReleaseEpisode = 9999
)

// releaseEpisodePattern 与插件的 release-v1 schema 同一条：十进制，可带小数（总集篇 12.5）。
// 不能直接信 ParseFloat：它认 Inf、NaN、1e300、0x1p4，换成整数集号会溢出。
var releaseEpisodePattern = regexp.MustCompile(`^[0-9]+(?:\.[0-9]+)?$`)

// Supports 报告 manifest 是否宣告了某项能力。
func (m Manifest) Supports(capability string) bool {
	for _, value := range m.Capabilities {
		if value == capability {
			return true
		}
	}
	return false
}

type ReleaseSubject struct {
	IDs    map[string]string `json:"ids,omitempty"`
	Titles []string          `json:"titles"`
}

type ReleaseSearchRequest struct {
	Schema  string         `json:"schema"`
	Subject ReleaseSubject `json:"subject"`
}

// ReleaseTransport 只有 release-v1 schema 允许的四个字段：严格解码时多出来的（url、headers、
// trackers、fileIndex…）一律拒收，不让它们混进磁力或文件建议。
type ReleaseTransport struct {
	Type       string `json:"type"`
	Magnet     string `json:"magnet,omitempty"`
	InfoHash   string `json:"infoHash,omitempty"`
	TorrentURL string `json:"torrentUrl,omitempty"`
}

// Release 是来源上的一条 BT 发布。
type Release struct {
	ID          string           `json:"id"`
	SourceID    string           `json:"sourceId"`
	Title       string           `json:"title"`
	Transport   ReleaseTransport `json:"transport"`
	Fansub      string           `json:"fansub,omitempty"`
	SizeBytes   int64            `json:"sizeBytes,omitempty"`
	Seeders     *int             `json:"seeders,omitempty"`
	PublishedAt string           `json:"publishedAt,omitempty"`
	Episode     string           `json:"episode,omitempty"`
}

// SourceResult 是一个来源这次搜索的结论：ok（有发布，partial 表示部分请求失败或超时）、
// zero（来源正常答复、没有发布）、failed（一条都没拿到且出了错）。
type SourceResult struct {
	SourceID   string `json:"sourceId"`
	State      string `json:"state"`
	Count      int    `json:"count"`
	Partial    bool   `json:"partial,omitempty"`
	Cached     bool   `json:"cached,omitempty"`
	Category   string `json:"category,omitempty"`
	Message    string `json:"message,omitempty"`
	Retryable  bool   `json:"retryable,omitempty"`
	DurationMS int64  `json:"durationMs"`
}

// ReleaseEvent 是 /v1/releases 流里的一行：release、source_result、done；
// 或 invalid —— 一条不合规的发布（跳过，不中断整个流；Skipped 是它所属的来源，认不出时为空）。
type ReleaseEvent struct {
	Event   string
	Release *Release
	Result  *SourceResult
	Skipped string
}

func (c *Client) Releases(ctx context.Context, input ReleaseSearchRequest, emit func(ReleaseEvent) error) error {
	if input.Schema != ReleaseSearchSchema || len(input.Subject.Titles) == 0 || len(input.Subject.Titles) > maximumReleaseTitles {
		return errors.New("release search request is incomplete")
	}
	body, err := json.Marshal(input)
	if err != nil {
		return err
	}
	if len(body) > maximumJSONBytes {
		return errors.New("release search request exceeds 1 MiB")
	}
	releases := 0
	return c.postNDJSON(ctx, "/v1/releases", body, func(line []byte) (bool, error) {
		event, err := decodeReleaseEvent(line)
		if err != nil {
			return false, err
		}
		if event.Event == "release" {
			releases++
			if releases > maximumReleases {
				return false, errors.New("plugin release limit exceeded")
			}
		}
		return event.Event == "done", emit(event)
	})
}

func decodeReleaseEvent(line []byte) (ReleaseEvent, error) {
	var envelope struct {
		Event string `json:"event"`
	}
	if err := json.Unmarshal(line, &envelope); err != nil {
		return ReleaseEvent{}, errors.New("plugin emitted invalid NDJSON")
	}
	switch envelope.Event {
	case "release":
		// 单条发布不合规只跳过这一条：插件一次交出几百条，不能因为某个站的一行脏数据
		// 把其余来源的结果全丢掉。整行不是 JSON、未知事件这类结构性错误仍然中断。
		var wire struct {
			Event   string   `json:"event"`
			Release *Release `json:"release"`
		}
		if err := decodeStrict(line, &wire); err != nil || wire.Release == nil || validateRelease(*wire.Release) != nil {
			return ReleaseEvent{Event: "invalid", Skipped: releaseSourceID(line)}, nil
		}
		return ReleaseEvent{Event: "release", Release: wire.Release}, nil
	case "source_result":
		var wire struct {
			Event string `json:"event"`
			SourceResult
		}
		if err := decodeStrict(line, &wire); err != nil {
			return ReleaseEvent{}, errors.New("plugin emitted an invalid source_result event")
		}
		result := wire.SourceResult
		if err := validateSourceResult(result); err != nil {
			return ReleaseEvent{}, err
		}
		return ReleaseEvent{Event: "source_result", Result: &result}, nil
	case "done":
		// done 与候选流同一个形状，沿用那边的校验
		if _, err := decodeEvent(line); err != nil {
			return ReleaseEvent{}, err
		}
		return ReleaseEvent{Event: "done"}, nil
	default:
		return ReleaseEvent{}, fmt.Errorf("plugin emitted unknown event %q", envelope.Event)
	}
}

// releaseSourceID 尽量从一条不合规的发布里读出来源 id（读不出或不合法时为空）。
func releaseSourceID(line []byte) string {
	var loose struct {
		Release struct {
			SourceID string `json:"sourceId"`
		} `json:"release"`
	}
	if json.Unmarshal(line, &loose) != nil || !validSourceID(loose.Release.SourceID) {
		return ""
	}
	return loose.Release.SourceID
}

// Locator 把发布的定位换成候选的 Transport（沿用候选那一套条目映射）。
func (t ReleaseTransport) Locator() Transport {
	return Transport{Type: t.Type, Magnet: t.Magnet, InfoHash: t.InfoHash, TorrentURL: t.TorrentURL}
}

func validateRelease(release Release) error {
	if release.ID == "" || len(release.ID) > 256 || !validSourceID(release.SourceID) {
		return errors.New("plugin emitted an invalid release")
	}
	title := strings.TrimSpace(release.Title)
	if title == "" || utf8.RuneCountInString(title) > maximumReleaseTitle {
		return errors.New("plugin release has an invalid title")
	}
	if release.Transport.Type != "torrent" {
		return errors.New("plugin release is not a torrent")
	}
	if err := validateTorrentTransport(release.Transport.Locator()); err != nil {
		return err
	}
	if release.SizeBytes < 0 || (release.Seeders != nil && *release.Seeders < 0) {
		return errors.New("plugin release has negative counters")
	}
	if release.Episode != "" {
		n, err := strconv.ParseFloat(release.Episode, 64)
		if !releaseEpisodePattern.MatchString(release.Episode) || err != nil || n <= 0 || n > maximumReleaseEpisode {
			return errors.New("plugin release has an invalid episode")
		}
	}
	return nil
}

func validateSourceResult(result SourceResult) error {
	if !validSourceID(result.SourceID) || result.Count < 0 || result.DurationMS < 0 {
		return errors.New("plugin emitted an invalid source_result event")
	}
	// 状态与条数要对得上：ok 至少一条，zero 与 failed 一条没有
	switch result.State {
	case "ok":
		if result.Count < 1 {
			return errors.New("plugin reported ok without releases")
		}
		return nil
	case "zero":
		if result.Count != 0 {
			return errors.New("plugin reported zero with releases")
		}
		return nil
	case "failed":
		if result.Category == "" || result.Count != 0 {
			return errors.New("plugin failure is malformed")
		}
		return nil
	default:
		return fmt.Errorf("plugin emitted unknown source state %q", result.State)
	}
}
