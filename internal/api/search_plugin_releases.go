package api

import (
	"context"
	"errors"
	"log"
	"strconv"
	"strings"

	"github.com/nagare-project/nagare/internal/rules"
	"github.com/nagare-project/nagare/internal/sourceplugin"
)

// 按作品搜全部发布（插件的 /v1/releases）。与 animego 网站的磁力搜索同一个模型：一部作品
// 只搜一次，不带集号、不按集号筛，选集在界面上做 —— 换集不再重新抓站，也不会因为插件认不出
// 「[01v2]」这类集号把发布丢掉。插件不支持时退回按集问（streamPluginTorrents）。

// 搜索范围：all 是整部作品的全部发布（换集不必重问），episode 是只含请求那一集的结果。
const (
	pluginScopeAll     = "all"
	pluginScopeEpisode = "episode"
)

// supportsReleases 报告当前插件能不能按作品搜全部发布。
func (s *SourcePluginService) supportsReleases() bool {
	if s == nil {
		return false
	}
	status := s.Status()
	return status.Phase == "ready" && status.Manifest != nil && status.Manifest.Supports(sourceplugin.CapabilityReleases)
}

func (s *SourcePluginService) Releases(ctx context.Context, request sourceplugin.ReleaseSearchRequest, emit func(sourceplugin.ReleaseEvent) error) error {
	return s.runtime.Releases(ctx, request, emit)
}

// streamPluginReleases 逐条交出插件按作品搜到的发布；每个来源有了结论就交出它的结果状态。
// emit 不会被并发调用（插件的流是逐行读的）。
func (s *SourcePluginService) streamPluginReleases(ctx context.Context, titles []string, anilistID int, emit func(*SearchItemView, *rules.Outcome) error) {
	// 与本机规则同一套整理（去空白、不分大小写去重、最多四种写法）
	titles = rules.QueryVariants(titles)
	if len(titles) == 0 {
		return
	}
	request := sourceplugin.ReleaseSearchRequest{Schema: sourceplugin.ReleaseSearchSchema, Subject: sourceplugin.ReleaseSubject{Titles: titles}}
	if anilistID > 0 {
		request.Subject.IDs = map[string]string{"anilist": strconv.Itoa(anilistID)}
	}
	// 取来源名也算在超时里：插件挂住时不能把整个请求拖住
	requestContext, cancel := context.WithTimeout(ctx, pluginTorrentTimeout)
	defer cancel()
	stream := newReleaseStream(s.sourceNames(requestContext), emit)
	err := s.Releases(requestContext, request, stream.handle)
	stream.finish()
	// 浏览器走了（ctx 取消、写不出去）不算故障；插件的流自己断了要说出来：结果可能不全
	if err != nil && ctx.Err() == nil && stream.emitErr == nil {
		log.Printf("api: 向来源插件按作品搜发布失败：%v", err)
		reason := "来源插件的发布流中断，结果可能不全"
		if errors.Is(err, context.DeadlineExceeded) {
			reason = "来源插件超时，部分来源没有返回"
		}
		_ = stream.send(nil, &rules.Outcome{Source: pluginSourcePrefix + "bt", State: rules.StateFailed, Reason: reason})
	}
}

// sourceNames 是插件来源 id → 显示名；取不到时为空（条目只是少了来源名）。
func (s *SourcePluginService) sourceNames(ctx context.Context) map[string]string {
	names := map[string]string{}
	sources, err := s.runtime.Sources(ctx)
	if err != nil {
		log.Printf("api: 读取来源插件的来源列表失败：%v", err)
		return names
	}
	for _, source := range sources {
		names[source.ID] = source.Name
	}
	return names
}

// releaseStream 把插件的发布流换成界面的条目与来源状态：同一来源的重复发布只交一次；
// 条数按实际交出的算（插件报的数只作对照），跳过的条数写进来源状态，不静默。
type releaseStream struct {
	names   map[string]string
	emit    func(*SearchItemView, *rules.Outcome) error
	emitErr error
	seen    map[string]bool
	sent    map[string]int // 每个来源交出的条目数
	skipped map[string]int // 每个来源不合规、拼不出定位而跳过的条数（"" 是认不出来源的）
	settled map[string]bool
}

func newReleaseStream(names map[string]string, emit func(*SearchItemView, *rules.Outcome) error) *releaseStream {
	return &releaseStream{names: names, emit: emit, seen: map[string]bool{}, sent: map[string]int{}, skipped: map[string]int{}, settled: map[string]bool{}}
}

func (r *releaseStream) handle(event sourceplugin.ReleaseEvent) error {
	switch {
	case event.Event == "invalid":
		r.skipped[event.Skipped]++
	case event.Release != nil:
		id := event.Release.SourceID
		item, ok := pluginTorrentItem(releaseCandidate(*event.Release), r.names)
		if !ok {
			r.skipped[id]++
			return nil
		}
		key := id + "\x00" + releaseLocator(item)
		if r.seen[key] {
			return nil
		}
		r.seen[key] = true
		r.sent[id]++
		return r.send(&item, nil)
	case event.Result != nil && !r.settled[event.Result.SourceID]:
		r.settled[event.Result.SourceID] = true
		return r.send(nil, r.outcome(*event.Result))
	}
	return nil
}

// finish 给交了发布、却没报结论的来源补一条结论（插件不该这样，但界面不能少一行）。
func (r *releaseStream) finish() {
	for _, id := range sortedKeys(r.sent) {
		if !r.settled[id] && r.emitErr == nil {
			r.settled[id] = true
			_ = r.send(nil, &rules.Outcome{Source: pluginSourcePrefix + id, State: rules.StateOK, Count: r.sent[id], RawCount: r.sent[id], Detail: "来源插件没有报告这个来源的结论"})
		}
	}
}

func (r *releaseStream) send(item *SearchItemView, outcome *rules.Outcome) error {
	if r.emitErr != nil {
		return r.emitErr
	}
	r.emitErr = r.emit(item, outcome)
	return r.emitErr
}

// outcome 把插件的来源结论换成界面的来源状态。零结果不是故障（CQ3）。
func (r *releaseStream) outcome(result sourceplugin.SourceResult) *rules.Outcome {
	id := result.SourceID
	outcome := releaseOutcome(result)
	outcome.Count = r.sent[id]
	var notes []string
	if outcome.Detail != "" {
		notes = append(notes, outcome.Detail)
	}
	if n := r.skipped[id]; n > 0 {
		notes = append(notes, strconv.Itoa(n)+" 条发布格式不对或缺少定位，已跳过")
	}
	if result.State == "ok" && r.sent[id] == 0 {
		// 插件说有结果、一条都没交到界面：不能显示成一个正常的源
		outcome.State = rules.StateFailed
		outcome.Reason = "来源插件的发布全部无法使用"
	}
	outcome.Detail = strings.Join(notes, "；")
	return outcome
}

// releaseCandidate 把一条发布换成候选的形状，沿用候选那一套条目映射（解析链、磁力拼装、体积兜底）。
func releaseCandidate(release sourceplugin.Release) sourceplugin.Candidate {
	episode, _ := strconv.ParseFloat(release.Episode, 64)
	return sourceplugin.Candidate{
		SourceID:  release.SourceID,
		Transport: release.Transport.Locator(),
		Metadata: sourceplugin.Metadata{
			Title: release.Title, Fansub: release.Fansub, SizeBytes: release.SizeBytes,
			Seeders: release.Seeders, PublishedAt: release.PublishedAt, Episode: episode,
		},
	}
}

// releaseOutcome 把插件的来源结论换成界面的来源状态。零结果不是故障（CQ3）。
func releaseOutcome(result sourceplugin.SourceResult) *rules.Outcome {
	outcome := &rules.Outcome{Source: pluginSourcePrefix + result.SourceID, Count: result.Count, RawCount: result.Count, LatencyMs: result.DurationMS}
	switch result.State {
	case "ok":
		outcome.State = rules.StateOK
		if result.Partial {
			outcome.Detail = "部分请求失败或超时，结果可能不全"
		}
	case "zero":
		outcome.State = rules.StateZero
		outcome.Reason = "来源里没有这部作品的发布"
	default:
		outcome.State = rules.StateFailed
		outcome.Reason = "来源插件报告失败"
		outcome.Detail = result.Category
	}
	return outcome
}
