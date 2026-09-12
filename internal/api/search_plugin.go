package api

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/nagare-project/nagare/internal/httpserver"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/nagare-project/nagare/internal/rules"
	"github.com/nagare-project/nagare/internal/sourceplugin"
)

// pluginTorrentTimeout 是磁力选集向插件要 BT 候选的上限。BT 来源是直接抓取，
// 正常几秒就回；这里只挡住某个来源挂死把整个选集拖住。
const pluginTorrentTimeout = 25 * time.Second

// pluginSourcePrefix 标记来自插件的条目，与本机规则的 id 空间分开。
const pluginSourcePrefix = "plugin:"

// pluginTorrentQuery 是磁力选集附带的作品身份；有集号才会去问插件。
type pluginTorrentQuery struct {
	Titles    []string
	Episode   int
	AnilistID int
	Year      int
}

// appendPluginTorrents 用插件的 BT 来源补充磁力选集。只要 torrent（transports 允许列表），
// 插件不会为此启动任何浏览器嗅探；候选映射成与本机规则同形的条目，来源 id 加前缀区分。
// 任何失败只落到 Sources 状态里，不影响本机规则的结果。
func (s *SourcePluginService) appendPluginTorrents(ctx context.Context, view *SearchView, query pluginTorrentQuery) {
	s.streamPluginTorrents(ctx, query, func(item *SearchItemView, outcome *rules.Outcome) error {
		if item != nil {
			view.Items = append(view.Items, *item)
		}
		if outcome != nil {
			view.Sources = append(view.Sources, *outcome)
		}
		return nil
	})
}

// PluginSearchEvent 是 GET /api/search/plugin 的 NDJSON 事件：条目、来源结果、结束。
// 选集窗口先拿本机规则的结果（不到一秒），插件来源一条条追加——最慢的站点（Mikan 十几秒）
// 不再拖住整个列表。
type PluginSearchEvent struct {
	Event   string          `json:"event"`
	Item    *SearchItemView `json:"item,omitempty"`
	Outcome *rules.Outcome  `json:"outcome,omitempty"`
}

// streamPluginTorrents 逐条交出插件的 BT 候选；每个来源结束时交出它的结果状态。
// emit 返回错误（客户端断开）即停止。
func (s *SourcePluginService) streamPluginTorrents(ctx context.Context, query pluginTorrentQuery, emit func(item *SearchItemView, outcome *rules.Outcome) error) {
	if s == nil || query.Episode < 1 || s.Status().Phase != "ready" {
		return
	}
	titles := make([]string, 0, len(query.Titles))
	for _, title := range query.Titles {
		if title = strings.TrimSpace(title); title != "" {
			titles = append(titles, title)
		}
	}
	if len(titles) == 0 {
		return
	}
	request := sourceplugin.ResolveRequest{
		Schema:      "nagare-resolve-request/v1",
		Subject:     sourceplugin.Subject{IDs: map[string]string{}, Titles: titles},
		Episode:     sourceplugin.Episode{Number: strconv.Itoa(query.Episode), Absolute: float64(query.Episode)},
		Preferences: sourceplugin.Preferences{Transports: []string{"torrent"}},
	}
	if query.AnilistID > 0 {
		request.Subject.IDs["anilist"] = strconv.Itoa(query.AnilistID)
	}
	if query.Year > 0 {
		year := query.Year
		request.Subject.Year = &year
	}
	names := map[string]string{}
	if sources, err := s.runtime.Sources(ctx); err == nil {
		for _, source := range sources {
			names[source.ID] = source.Name
		}
	}

	requestContext, cancel := context.WithTimeout(ctx, pluginTorrentTimeout)
	defer cancel()
	started := time.Now()
	counts := map[string]int{}
	failed := map[string]string{}
	var emitErr error
	err := s.Candidates(requestContext, request, func(event sourceplugin.Event) error {
		latency := time.Since(started).Milliseconds()
		switch event.Event {
		case "candidate":
			if event.Candidate == nil || event.Candidate.Transport.Type != "torrent" {
				return nil
			}
			if item, ok := pluginTorrentItem(*event.Candidate, names); ok {
				counts[event.Candidate.SourceID]++
				if emitErr = emit(&item, nil); emitErr != nil {
					return emitErr
				}
			}
		case "source_error":
			failed[event.SourceID] = event.Category
			if _, ok := counts[event.SourceID]; !ok {
				emitErr = emit(nil, &rules.Outcome{Source: pluginSourcePrefix + event.SourceID, State: rules.StateFailed, Reason: "来源插件报告失败", Detail: event.Category, LatencyMs: latency})
				return emitErr
			}
		}
		return nil
	})
	if emitErr != nil {
		return
	}
	latency := time.Since(started).Milliseconds()
	for id, count := range counts {
		if emit(nil, &rules.Outcome{Source: pluginSourcePrefix + id, State: rules.StateOK, Count: count, RawCount: count, LatencyMs: latency}) != nil {
			return
		}
	}
	if err != nil && ctx.Err() == nil && len(counts) == 0 && len(failed) == 0 {
		_ = emit(nil, &rules.Outcome{Source: pluginSourcePrefix + "bt", State: rules.StateFailed, Reason: "来源插件候选流中断", LatencyMs: latency})
	}
}

// searchPlugin 是 GET /api/search/plugin：只问插件的 BT 来源，NDJSON 逐条返回。
func (h *Handler) searchPlugin(w http.ResponseWriter, r *http.Request) {
	params := r.URL.Query()
	episode, err := strconv.Atoi(params.Get("episode"))
	if err != nil || episode < 1 {
		httpserver.WriteError(w, http.StatusBadRequest, "集号必须大于零")
		return
	}
	anilist, _ := strconv.Atoi(params.Get("anilist"))
	year, _ := strconv.Atoi(params.Get("year"))
	titles := append([]string{params.Get("q")}, params["title"]...)
	w.Header().Set("Content-Type", "application/x-ndjson; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)
	encoder := json.NewEncoder(w)
	write := func(event PluginSearchEvent) error {
		if err := encoder.Encode(event); err != nil {
			return err
		}
		if flusher != nil {
			flusher.Flush()
		}
		return nil
	}
	h.deps.SourcePlugin.streamPluginTorrents(r.Context(), pluginTorrentQuery{Titles: titles, Episode: episode, AnilistID: anilist, Year: year}, func(item *SearchItemView, outcome *rules.Outcome) error {
		return write(PluginSearchEvent{Event: "item", Item: item, Outcome: outcome})
	})
	_ = write(PluginSearchEvent{Event: "done"})
}

// pluginTorrentItem 把一条 torrent 候选压成磁力选集的条目；没有 magnet 但有 infohash 时拼一条。
func pluginTorrentItem(candidate sourceplugin.Candidate, names map[string]string) (SearchItemView, bool) {
	magnet := strings.TrimSpace(candidate.Transport.Magnet)
	infohash := strings.ToLower(strings.TrimSpace(candidate.Transport.InfoHash))
	torrentURL := strings.TrimSpace(candidate.Transport.TorrentURL)
	if magnet == "" && infohash != "" {
		magnet = "magnet:?xt=urn:btih:" + infohash
	}
	if magnet == "" && torrentURL == "" {
		return SearchItemView{}, false
	}
	// 优先用来源的原始发布标题：字幕组、季数、清晰度都在里面，本机解析链也靠它；
	// 老插件不给时才退回「作品名 - 集号」的合成写法。
	title := strings.TrimSpace(candidate.Metadata.Title)
	if title == "" {
		title = strings.TrimSpace(candidate.Match.SubjectTitle)
		if candidate.Match.EpisodeNumber > 0 && title != "" {
			title = fmt.Sprintf("%s - %s", title, formatEpisode(candidate.Match.EpisodeNumber))
		}
	}
	item := rules.Item{Magnet: magnet, Source: pluginSourcePrefix + candidate.SourceID, Infohash: infohash, Title: title}
	if candidate.Metadata.Fansub != "" {
		fansub := candidate.Metadata.Fansub
		item.Fansub = &fansub
	}
	if candidate.Metadata.SizeBytes > 0 {
		item.Size = formatBytesHuman(candidate.Metadata.SizeBytes)
	}
	if candidate.Metadata.PublishedAt != "" {
		date := candidate.Metadata.PublishedAt
		item.Date = &date
	}
	if candidate.Metadata.Seeders != nil {
		seeders := *candidate.Metadata.Seeders
		item.Seeders = &seeders
	}
	if name := names[candidate.SourceID]; name != "" {
		provider := name
		item.Provider = &provider
	}
	out := enrichSearchItem(item)
	// 有种子文件地址就一并带上（不只在没磁力时）：播放端优先下载种子文件，跳过找元数据。
	out.TorrentURL = torrentURL
	// 集号：插件从标题解出来的（metadata.episode）最可靠；没有就用本机解析链从原始标题解的
	// （合集会解成 nil，这正是我们要的——合集不能冒充单集）；只有老插件不给原始标题时，
	// 才退回 match.episodeNumber（那是请求里的目标集，不是条目自己的）。
	if candidate.Metadata.Episode > 0 {
		episode := int(math.Round(candidate.Metadata.Episode))
		out.Episode = &episode
	} else if candidate.Metadata.Title == "" && candidate.Match.EpisodeNumber > 0 {
		episode := int(math.Round(candidate.Match.EpisodeNumber))
		out.Episode = &episode
	}
	if candidate.Metadata.Resolution != "" {
		out.Resolution = strings.ToLower(candidate.Metadata.Resolution)
	}
	if out.Group == "" && candidate.Metadata.Fansub != "" {
		out.Group = candidate.Metadata.Fansub
	}
	if candidate.Metadata.Title == "" {
		out.Kind = "main"
	}
	return out, true
}

func formatEpisode(n float64) string {
	if n == math.Trunc(n) {
		return fmt.Sprintf("%02d", int(n))
	}
	return strconv.FormatFloat(n, 'f', -1, 64)
}

// formatBytesHuman 与规则引擎的 format_bytes 同一口径（GB 一位小数、MB/KB 取整）。
func formatBytesHuman(n int64) string {
	f := float64(n)
	switch {
	case f >= 1e9:
		return strconv.FormatFloat(f/1e9, 'f', 1, 64) + " GB"
	case f >= 1e6:
		return strconv.Itoa(int(math.Round(f/1e6))) + " MB"
	default:
		return strconv.Itoa(int(math.Round(f/1e3))) + " KB"
	}
}
