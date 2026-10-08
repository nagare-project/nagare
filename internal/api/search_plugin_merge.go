package api

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/nagare-project/nagare/internal/rules"
	"github.com/nagare-project/nagare/internal/sourceplugin"
)

// 插件 BT 候选的合并。一次选集可能向插件发两个请求（跨季连续编号的两种集号，见
// pluginTorrentQuery.episodes）：同一条发布可能两次都回来（覆盖两种编号的合集），同一个
// 来源可能一次有结果、一次报失败。这里把它们收成「每条发布一次、每个来源一个结论」——
// 先报「源连接失败」再报成功，界面就会一直挂着一条并不存在的故障。

// zeroResultCategories 是插件「来源正常答复、只是没有匹配的发布」的失败类别。零结果不是
// 故障（CQ3）：用户换着集号点，没发布的集多得是，不能每次都说「源连接失败」。
var zeroResultCategories = map[string]bool{"episode_not_found": true, "no_subject_match": true}

type pluginMerge struct {
	queries int
	names   map[string]string
	emit    func(*SearchItemView, *rules.Outcome) error
	// abort 在客户端断开后叫停其余请求：没人在听了，别再让插件抓站。
	abort   context.CancelFunc
	started time.Time

	mu       sync.Mutex
	emitErr  error
	seen     map[string]bool     // 已交出的发布（来源 + 定位）
	counts   map[string]int      // 每个来源交出的条目数
	failures map[string][]string // 每个来源报过的失败类别（每个请求至多一条）
	settled  map[string]bool     // 已经交出结论的来源
}

func newPluginMerge(queries int, names map[string]string, emit func(*SearchItemView, *rules.Outcome) error, abort context.CancelFunc) *pluginMerge {
	return &pluginMerge{
		queries: queries, names: names, emit: emit, abort: abort, started: time.Now(),
		seen: map[string]bool{}, counts: map[string]int{}, failures: map[string][]string{}, settled: map[string]bool{},
	}
}

// handle 是插件事件回调。几个请求的回调会并发进来，交出（emit）必须串行。
func (m *pluginMerge) handle(event sourceplugin.Event) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.emitErr != nil {
		return m.emitErr
	}
	switch event.Event {
	case "candidate":
		if event.Candidate == nil || event.Candidate.Transport.Type != "torrent" {
			return nil
		}
		item, ok := pluginTorrentItem(*event.Candidate, m.names)
		if !ok {
			return nil
		}
		if m.queries > 1 {
			// 两种编号各问一次时，同一个合集会被两次请求各指一个文件（第 3 集与第 15 集），
			// 先到的那个未必是这一集：不转交插件的文件建议，交给集号提示与选集弹窗
			item.FileIndex = nil
		}
		key := event.Candidate.SourceID + "\x00" + releaseLocator(item)
		if m.seen[key] {
			return nil
		}
		m.seen[key] = true
		m.counts[event.Candidate.SourceID]++
		return m.send(&item, nil)
	case "source_error":
		id := event.SourceID
		m.failures[id] = append(m.failures[id], event.Category)
		// 每个请求都对这个来源报了失败、它又一条没交出过：结论已定，立刻告诉界面
		if len(m.failures[id]) >= m.queries && m.counts[id] == 0 && !m.settled[id] {
			m.settled[id] = true
			return m.send(nil, m.failureOutcome(id))
		}
	}
	return nil
}

// finish 在全部请求结束后交出其余来源的结论。errs 是每个请求的返回值。
func (m *pluginMerge) finish(ctx context.Context, errs []error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.emitErr != nil {
		return
	}
	for _, id := range sortedKeys(m.counts) {
		if !m.settled[id] {
			m.settled[id] = true
			outcome := &rules.Outcome{Source: pluginSourcePrefix + id, State: rules.StateOK, Count: m.counts[id], RawCount: m.counts[id], LatencyMs: m.latency()}
			if failed := m.failures[id]; len(failed) > 0 {
				// 有结果就算有结果，但另一种编号的请求失败了要留下痕迹
				outcome.Detail = "部分请求失败：" + strings.Join(failed, ",")
			}
			if m.send(nil, outcome) != nil {
				return
			}
		}
	}
	// 只有部分请求报了失败的来源（另一个请求的流半路断了）：照它报的说
	for _, id := range sortedKeys(m.failures) {
		if !m.settled[id] {
			m.settled[id] = true
			if m.send(nil, m.failureOutcome(id)) != nil {
				return
			}
		}
	}
	// 插件的候选流自己断了（插件崩溃、超时）：哪怕别的来源有结果也要说出来 —— 结果可能不全，
	// 别让界面以为「没有资源」或「就这些」。浏览器已经走了（ctx 取消）就不必说。
	if err := firstError(errs); err != nil && ctx.Err() == nil {
		reason := "来源插件候选流中断，结果可能不全"
		if errors.Is(err, context.DeadlineExceeded) {
			reason = "来源插件超时，部分来源没有返回"
		}
		_ = m.send(nil, &rules.Outcome{Source: pluginSourcePrefix + "bt", State: rules.StateFailed, Reason: reason, LatencyMs: m.latency()})
	}
}

// failureOutcome 把来源报过的失败收成一个结论：只要有一次是真故障就算失败，
// 全是「没有匹配的发布」才算零结果。调用方持 mu。
func (m *pluginMerge) failureOutcome(id string) *rules.Outcome {
	categories := m.failures[id]
	for _, category := range categories {
		if !zeroResultCategories[category] {
			return &rules.Outcome{Source: pluginSourcePrefix + id, State: rules.StateFailed, Reason: "来源插件报告失败", Detail: category, LatencyMs: m.latency()}
		}
	}
	return &rules.Outcome{Source: pluginSourcePrefix + id, State: rules.StateZero, Reason: "来源里没有这一集的发布", Detail: categories[0], LatencyMs: m.latency()}
}

// send 交出一条事件；客户端断开时记下错误并叫停其余请求。调用方持 mu。
func (m *pluginMerge) send(item *SearchItemView, outcome *rules.Outcome) error {
	if err := m.emit(item, outcome); err != nil {
		m.emitErr = err
		m.abort()
		return err
	}
	return nil
}

func (m *pluginMerge) latency() int64 { return time.Since(m.started).Milliseconds() }

// releaseLocator 是同一条发布在两次请求里的共同身份：infohash 优先，其次磁力、种子地址。
func releaseLocator(item SearchItemView) string {
	switch {
	case item.Infohash != "":
		return item.Infohash
	case item.Magnet != "":
		return item.Magnet
	default:
		return item.TorrentURL
	}
}

func firstError(errs []error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

func sortedKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
