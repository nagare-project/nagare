package rules

import (
	"sort"
	"time"
)

// 去重与排序 —— 移植自 animego rank.go，语义逐条保留。

// sourceRanks 给每个源打分：priority 高者优先，同分按注册顺序；最优得最高分。
func sourceRanks(rules []*Rule) map[string]int {
	type ranked struct {
		id       string
		priority int
		index    int
	}
	ordered := make([]ranked, 0, len(rules))
	for i, r := range rules {
		ordered = append(ordered, ranked{id: r.ID, priority: r.Capabilities.Priority, index: i})
	}
	sort.SliceStable(ordered, func(a, b int) bool {
		if ordered[a].priority != ordered[b].priority {
			return ordered[a].priority > ordered[b].priority
		}
		return ordered[a].index < ordered[b].index
	})
	ranks := make(map[string]int, len(ordered))
	for i, r := range ordered {
		ranks[r.id] = len(ordered) - i
	}
	return ranks
}

// dedupByInfohash 按 magnet 里解析出的 infohash 去重：保留首次出现的位置，
// 同 hash 取「更好」的一份（做种数多者胜，未知最差；再比源分数）。
// 解析不出 hash 的条目原样透传。存活条目会被盖上归一化的 Infohash。
func dedupByInfohash(items []Item, ranks map[string]int) []Item {
	out := make([]Item, 0, len(items))
	indexByHash := make(map[string]int, len(items))
	for _, it := range items {
		hash := ParseInfohash(it.Magnet)
		if hash == "" {
			out = append(out, it)
			continue
		}
		candidate := it
		candidate.Infohash = hash
		pos, seen := indexByHash[hash]
		if !seen {
			indexByHash[hash] = len(out)
			out = append(out, candidate)
			continue
		}
		if betterItem(candidate, out[pos], ranks) {
			out[pos] = candidate
		}
	}
	return out
}

func betterItem(a, b Item, ranks map[string]int) bool {
	if c := compareSeeders(a.Seeders, b.Seeders); c != 0 {
		return c > 0
	}
	return ranks[a.Source] > ranks[b.Source]
}

// compareSeeders：已知数永远大于 nil（未知沉底，含 0 > nil）；两个已知比大小。
func compareSeeders(a, b *int) int {
	switch {
	case a == nil && b == nil:
		return 0
	case a == nil:
		return -1
	case b == nil:
		return 1
	case *a != *b:
		if *a > *b {
			return 1
		}
		return -1
	default:
		return 0
	}
}

// rankItems 稳定排序：做种数降序（nil 最后）→ 日期降序（解析失败视为最旧）→ 源分数降序。
func rankItems(items []Item, ranks map[string]int) []Item {
	out := make([]Item, len(items))
	copy(out, items)
	sort.SliceStable(out, func(i, j int) bool {
		if c := compareSeeders(out[i].Seeders, out[j].Seeders); c != 0 {
			return c > 0
		}
		di, dj := parseItemDate(out[i].Date), parseItemDate(out[j].Date)
		if !di.Equal(dj) {
			return di.After(dj)
		}
		return ranks[out[i].Source] > ranks[out[j].Source]
	})
	return out
}

var itemDateLayouts = []string{time.RFC1123Z, time.RFC1123, time.RFC3339}

// parseItemDate 宽容解析日期；nil / 空 / 未知格式 → 零值（排最旧），绝不报错。
func parseItemDate(d *string) time.Time {
	if d == nil || *d == "" {
		return time.Time{}
	}
	for _, layout := range itemDateLayouts {
		if t, err := time.Parse(layout, *d); err == nil {
			return t
		}
	}
	return time.Time{}
}
