package rules

import (
	"context"
	"fmt"
	"strings"
	"sync"
)

// Registry 持有已加载的规则（顺序即合并顺序）与用户的启用状态，负责并发搜索。
type Registry struct {
	fetcher *Fetcher

	mu       sync.RWMutex
	rules    []*Rule
	disabled map[string]bool
}

// SearchResult 是一次聚合搜索：已去重排序的条目 + 每个源的健康结论。
type SearchResult struct {
	Query   string    `json:"query"`
	Items   []Item    `json:"items"`
	Sources []Outcome `json:"sources"`
}

// NewRegistry 构造注册表。
func NewRegistry(fetcher *Fetcher, rules ...*Rule) *Registry {
	if fetcher == nil {
		fetcher = &Fetcher{}
	}
	return &Registry{fetcher: fetcher, rules: rules, disabled: map[string]bool{}}
}

// Replace 整体替换规则集（重新加载规则目录后调用）；启用状态按 id 保留。
func (g *Registry) Replace(rules []*Rule) {
	g.mu.Lock()
	g.rules = rules
	g.mu.Unlock()
}

// Rules 返回规则快照（顺序即合并顺序）。
func (g *Registry) Rules() []*Rule {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return append([]*Rule(nil), g.rules...)
}

// SetEnabled 设置某源启用状态。
func (g *Registry) SetEnabled(id string, on bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if on {
		delete(g.disabled, id)
	} else {
		g.disabled[id] = true
	}
}

// Enabled 报告某源是否启用。
func (g *Registry) Enabled(id string) bool {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return !g.disabled[id]
}

// Search 并发查询全部启用的源，按注册顺序合并，再去重、排序。
// 空关键词直接返回空结果，不打任何上游。
func (g *Registry) Search(ctx context.Context, query string) SearchResult {
	query = strings.TrimSpace(query)
	res := SearchResult{Query: query, Items: []Item{}, Sources: []Outcome{}}
	if query == "" {
		return res
	}
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
			outcomes[i] = g.fetcher.Run(ctx, r, query)
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

// SelfCheck 用规则自带的自检关键词探测该源是否活着（零结果时按需触发，CQ3）。
func (g *Registry) SelfCheck(ctx context.Context, id string) (Outcome, error) {
	for _, r := range g.Rules() {
		if r.ID != id {
			continue
		}
		if r.SelfTest.Query == "" {
			return Outcome{}, fmt.Errorf("规则 %s 未声明 selftest.query，无法自检", id)
		}
		return g.fetcher.Run(ctx, r, r.SelfTest.Query), nil
	}
	return Outcome{}, fmt.Errorf("未知的源 %q", id)
}
