package api

import (
	"context"
	"fmt"
	"math"
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
	err := s.Candidates(requestContext, request, func(event sourceplugin.Event) error {
		switch event.Event {
		case "candidate":
			if event.Candidate == nil || event.Candidate.Transport.Type != "torrent" {
				return nil
			}
			if item, ok := pluginTorrentItem(*event.Candidate, names); ok {
				view.Items = append(view.Items, item)
				counts[event.Candidate.SourceID]++
			}
		case "source_error":
			failed[event.SourceID] = event.Category
		}
		return nil
	})
	latency := time.Since(started).Milliseconds()
	for id, count := range counts {
		view.Sources = append(view.Sources, rules.Outcome{Source: pluginSourcePrefix + id, State: rules.StateOK, Count: count, RawCount: count, LatencyMs: latency})
	}
	for id, category := range failed {
		if _, ok := counts[id]; ok {
			continue
		}
		view.Sources = append(view.Sources, rules.Outcome{Source: pluginSourcePrefix + id, State: rules.StateFailed, Reason: "来源插件报告失败", Detail: category, LatencyMs: latency})
	}
	if err != nil && ctx.Err() == nil && len(counts) == 0 && len(failed) == 0 {
		view.Sources = append(view.Sources, rules.Outcome{Source: pluginSourcePrefix + "bt", State: rules.StateFailed, Reason: "来源插件候选流中断", LatencyMs: latency})
	}
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
	title := strings.TrimSpace(candidate.Match.SubjectTitle)
	if candidate.Match.EpisodeNumber > 0 && title != "" {
		title = fmt.Sprintf("%s - %s", title, formatEpisode(candidate.Match.EpisodeNumber))
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
	if magnet == "" {
		out.TorrentURL = torrentURL
	}
	// 插件已经按目标集匹配过；它给的集号比从合成标题里再解一次可靠。
	if candidate.Metadata.Episode > 0 {
		episode := int(math.Round(candidate.Metadata.Episode))
		out.Episode = &episode
	} else if candidate.Match.EpisodeNumber > 0 {
		episode := int(math.Round(candidate.Match.EpisodeNumber))
		out.Episode = &episode
	}
	if candidate.Metadata.Resolution != "" {
		out.Resolution = strings.ToLower(candidate.Metadata.Resolution)
	}
	if out.Group == "" && candidate.Metadata.Fansub != "" {
		out.Group = candidate.Metadata.Fansub
	}
	out.Kind = "main"
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
