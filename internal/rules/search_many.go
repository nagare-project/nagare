package rules

import (
	"context"
	"strings"
	"sync"
)

// MaxQueryVariants 是一次搜索最多用几种写法（中文名、原名、英文名、罗马音）。
// 每种写法每个源都要打一次上游，上限挡住「写法 × 源」的扇出。
const MaxQueryVariants = 4

// QueryVariants 整理同一部作品的几种写法：去首尾空白、跳过空串、不分大小写去重（保留先出现的
// 那种写法）、最多 MaxQueryVariants 个。规则移植自 animego 的 buildTorrentVariants。
func QueryVariants(titles []string) []string {
	out := make([]string, 0, MaxQueryVariants)
	seen := make(map[string]bool, MaxQueryVariants)
	for _, title := range titles {
		title = strings.TrimSpace(title)
		key := strings.ToLower(title)
		if title == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, title)
		if len(out) == MaxQueryVariants {
			break
		}
	}
	return out
}

// SearchMany 用同一部作品的几种写法各搜一次再合并：字幕组按各自习惯的名字登记发布
// （nyaa 多是罗马音与英文名，国内站多是中文名），只用一种写法会漏掉另一半。
// 合并后跨写法、跨源按 infohash 去重，沿用同一套排序（做种数 → 日期 → 源优先级）；
// 每个源只给一个结论。同一个源的几种写法并发查询（与来源插件同一做法），整体耗时
// 仍是最慢的那一次，选集窗口的「本机规则先回」不被拖慢。只有一种写法时与 Search 完全一致。
func (g *Registry) SearchMany(ctx context.Context, queries []string) SearchResult {
	queries = QueryVariants(queries)
	res := SearchResult{Query: "", Items: []Item{}, Sources: []Outcome{}}
	if len(queries) == 0 {
		return res
	}
	res.Query = queries[0]
	rules := g.Rules()
	outcomes := make([]Outcome, len(rules))
	var wg sync.WaitGroup
	for i, r := range rules {
		if !g.Enabled(r.ID) {
			outcomes[i] = Outcome{Source: r.ID, State: StateDisabled}
			continue
		}
		wg.Add(1)
		go func(i int, r *Rule) {
			defer wg.Done()
			outcomes[i] = g.searchRule(ctx, r, queries)
		}(i, r)
	}
	wg.Wait()

	var enabled []*Rule
	var merged []Item
	for i, r := range rules {
		if outcomes[i].State != StateDisabled {
			enabled = append(enabled, r)
		}
		merged = append(merged, outcomes[i].Items...)
	}
	ranks := sourceRanks(enabled)
	res.Items = rankItems(dedupByInfohash(merged, ranks), ranks)
	res.Sources = outcomes
	return res
}

// searchRule 用每种写法查一个源，收成这个源的一个结论。
func (g *Registry) searchRule(ctx context.Context, r *Rule, queries []string) Outcome {
	if len(queries) == 1 {
		return g.fetcher.Run(ctx, r, queries[0])
	}
	per := make([]Outcome, len(queries))
	var wg sync.WaitGroup
	for j, query := range queries {
		wg.Add(1)
		go func() {
			defer wg.Done()
			per[j] = g.fetcher.Run(ctx, r, query)
		}()
	}
	wg.Wait()
	return mergeOutcomes(r.ID, per)
}

// stateWeight 决定几种写法的结论怎么合：有一种写法有结果就算有结果；都没有结果时，
// 规则解不出（dead）比连不上（failed）更该被看见，二者都比零结果优先 —— 一种写法零结果、
// 另一种连不上，说不清这个源到底有没有，不能报成「确实没有」（CQ3）。
var stateWeight = map[State]int{StateZero: 0, StateFailed: 1, StateDead: 2, StateOK: 3}

// mergeOutcomes 把一个源在几种写法下的结果收成一个结论。
func mergeOutcomes(source string, outcomes []Outcome) Outcome {
	merged := Outcome{Source: source, State: StateZero}
	partial := "" // 第一种失败写法的原因：整体有结果时留在 Detail 里
	for _, o := range outcomes {
		merged.Items = append(merged.Items, o.Items...)
		merged.Count += o.Count
		merged.RawCount += o.RawCount
		merged.Dropped += o.Dropped
		merged.LatencyMs = max(merged.LatencyMs, o.LatencyMs) // 并发查询：耗时取最慢的一次
		if (o.State == StateFailed || o.State == StateDead) && partial == "" {
			partial = o.Reason
		}
		if stateWeight[o.State] > stateWeight[merged.State] {
			merged.State, merged.Reason, merged.Detail = o.State, o.Reason, o.Detail
		}
	}
	if merged.State == StateOK {
		// 有结果时界面不报故障，但另一种写法失败了要留下痕迹（CQ3：失败不静默）
		merged.Reason, merged.Detail = "", ""
		if partial != "" {
			merged.Detail = "部分写法查询失败：" + partial
		}
		// 「全部条目上都为空」要对全部写法的条目一起判断
		merged.FieldGaps = fieldGaps(merged.Items)
	}
	return merged
}
