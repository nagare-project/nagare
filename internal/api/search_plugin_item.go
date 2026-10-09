package api

import (
	"fmt"
	"math"
	"net/url"
	"strconv"
	"strings"

	"github.com/nagare-project/nagare/internal/rules"
	"github.com/nagare-project/nagare/internal/sourceplugin"
	"github.com/nagare-project/nagare/internal/torrentstream"
)

// 插件的 BT 候选 → 磁力选集里与本机规则同形的条目。

// pluginTorrentItem 把一条 torrent 候选压成磁力选集的条目；没有 magnet 但有 infohash 时拼一条。
func pluginTorrentItem(candidate sourceplugin.Candidate, names map[string]string) (SearchItemView, bool) {
	magnet, infohash, torrentURL := pluginLocators(candidate.Transport)
	if magnet == "" && torrentURL == "" {
		return SearchItemView{}, false
	}
	out := enrichSearchItem(pluginRuleItem(candidate, names, magnet, infohash))
	// 有种子文件地址就一并带上（不只在没磁力时）：播放端优先下载种子文件，跳过找元数据。
	out.TorrentURL = torrentURL
	applyPluginMetadata(&out, candidate)
	return out, true
}

// pluginLocators 取出候选的三种定位方式。只认合法形状的 infohash（40 位 hex 或 32 位
// base32，后者转成 hex）：它要被拼进磁力，来源给的垃圾值不能带着别的参数混进去。
func pluginLocators(transport sourceplugin.Transport) (magnet, infohash, torrentURL string) {
	magnet = strings.TrimSpace(transport.Magnet)
	infohash = rules.ParseInfohash("magnet:?xt=urn:btih:" + strings.TrimSpace(transport.InfoHash))
	torrentURL = strings.TrimSpace(transport.TorrentURL)
	if magnet == "" && infohash != "" {
		magnet = pluginMagnet(infohash, transport.Trackers)
	}
	return magnet, infohash, torrentURL
}

// pluginRuleItem 填出与本机规则同形的基础字段。标题优先用来源的原始发布标题：字幕组、季数、
// 清晰度都在里面，本机解析链也靠它；老插件不给时才退回「作品名 - 集号」的合成写法。
func pluginRuleItem(candidate sourceplugin.Candidate, names map[string]string, magnet, infohash string) rules.Item {
	title := strings.TrimSpace(candidate.Metadata.Title)
	if title == "" {
		title = strings.TrimSpace(candidate.Match.SubjectTitle)
		if candidate.Match.EpisodeNumber > 0 && title != "" {
			title = fmt.Sprintf("%s - %s", title, formatEpisode(candidate.Match.EpisodeNumber))
		}
	}
	item := rules.Item{Magnet: magnet, Source: pluginSourcePrefix + candidate.SourceID, Infohash: infohash, Title: title}
	meta := candidate.Metadata
	if meta.Fansub != "" {
		fansub := meta.Fansub
		item.Fansub = &fansub
	}
	// 不到 1MB 的「视频发布」不可能是真的大小：多半是插件规则把单位读错了（比如把 KB 当成字节），
	// 显示成「0 KB」只会误导人去挑别的版本，宁可不显示
	if meta.SizeBytes >= minPlausibleReleaseBytes {
		item.Size = formatBytesHuman(meta.SizeBytes)
	}
	if meta.PublishedAt != "" {
		date := meta.PublishedAt
		item.Date = &date
	}
	if meta.Seeders != nil {
		seeders := *meta.Seeders
		item.Seeders = &seeders
	}
	if name := names[candidate.SourceID]; name != "" {
		provider := name
		item.Provider = &provider
	}
	return item
}

// applyPluginMetadata 用插件给的结构化信息补全（或纠正）解析链的结果。
func applyPluginMetadata(out *SearchItemView, candidate sourceplugin.Candidate) {
	meta := candidate.Metadata
	// 集号：插件从标题解出来的（metadata.episode）最可靠；没有就用本机解析链从原始标题解的。
	// 合集不能冒充单集，插件给了数也不行（「[01-12]」被插件读成第 1 集时，它其实是合集）；
	// 只有老插件不给原始标题时，才退回 match.episodeNumber（那是请求里的目标集，不是条目自己的）。
	switch {
	case out.Kind == "batch":
	case meta.Episode > 0 && meta.Episode != math.Trunc(meta.Episode):
		// 12.5 这类总集篇不是第 12 集也不是第 13 集：不给集号，不会被当成哪一集排到前面
		out.Episode = nil
	case meta.Episode > 0:
		episode := int(math.Round(meta.Episode))
		out.Episode = &episode
	case meta.Title == "" && candidate.Match.EpisodeNumber > 0:
		episode := int(math.Round(candidate.Match.EpisodeNumber))
		out.Episode = &episode
	}
	// 插件指明了合集里哪个文件就是这一集：播放时作为建议交给引擎
	if index := candidate.Transport.FileIndex; index != nil && *index >= 0 {
		fileIndex := *index
		out.FileIndex = &fileIndex
	}
	if meta.Resolution != "" {
		out.Resolution = strings.ToLower(meta.Resolution)
	}
	if out.Group == "" && meta.Fansub != "" {
		out.Group = meta.Fansub
	}
	if meta.Title == "" {
		out.Kind = "main"
	}
}

// pluginMagnet 用插件给的 infohash 拼磁力；插件同时给了 tracker 就一并带上（tr=）。
// 只有这条路补 tracker：.torrent 地址可能是私有站的种子，往里补公共 tracker 等于把 passkey
// 种子的 infohash 报到站外（见 torrentstream.trackersFor）。磁力本来就要先经 DHT 公开找
// peer，多带几个 tracker 不多暴露什么，却能把找元数据从几十秒缩到几秒。tracker 的合法性
// 判定沿用引擎那一份（只收 http/https/udp/ws/wss，有条数上限）。
func pluginMagnet(infohash string, trackers []string) string {
	var b strings.Builder
	b.WriteString("magnet:?xt=urn:btih:")
	b.WriteString(infohash)
	kept, _ := torrentstream.NormalizeTrackers(trackers)
	for _, tracker := range kept {
		b.WriteString("&tr=")
		b.WriteString(url.QueryEscape(tracker))
	}
	return b.String()
}

func formatEpisode(n float64) string {
	if n == math.Trunc(n) {
		return fmt.Sprintf("%02d", int(n))
	}
	return strconv.FormatFloat(n, 'f', -1, 64)
}

// formatBytesHuman 与规则引擎的 format_bytes 同一口径（GB 一位小数、MB/KB 取整）。
// minPlausibleReleaseBytes 是一条视频发布可信的最小体积。
const minPlausibleReleaseBytes = 1 << 20

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
