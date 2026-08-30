// 同目录分组 —— 从 animego grouping.js 逐行移植（决议 CQ2）。
package library

import (
	"sort"
	"strings"
)

// Group 是同一目录下条目的自动分组。
type Group struct {
	ID           string
	GroupKey     string // 目录键（relativePath 的 dirname，根目录为 __root__）
	Label        string
	Items        []Item
	SortMode     string // "episode" | "alpha"（alpha 表示触发了歧义回退）
	HasAmbiguity bool
}

// rootGroupKey 是根目录的哨兵键。
const rootGroupKey = "__root__"

// deriveGroupKey 取 relativePath 的目录部分。
func deriveGroupKey(relativePath string) string {
	idx := strings.LastIndex(relativePath, "/")
	if idx == -1 {
		return rootGroupKey
	}
	return relativePath[:idx]
}

// detectAmbiguity 检测集号歧义：
//  1. 同一非空集号出现了不同 parsedKind；
//  2. kind 混合 ⊆ {main,sp,ova} 且主线集号有空洞被 sp/ova 补位。
func detectAmbiguity(items []Item) bool {
	epKinds := map[int]map[string]struct{}{}
	for _, it := range items {
		if it.Episode == nil {
			continue
		}
		if epKinds[*it.Episode] == nil {
			epKinds[*it.Episode] = map[string]struct{}{}
		}
		epKinds[*it.Episode][it.ParsedKind] = struct{}{}
	}
	for _, kinds := range epKinds {
		if len(kinds) > 1 {
			return true
		}
	}

	ambiguousKinds := map[string]struct{}{"main": {}, "sp": {}, "ova": {}}
	present := map[string]struct{}{}
	for _, it := range items {
		present[it.ParsedKind] = struct{}{}
	}
	_, hasMain := present["main"]
	_, hasSp := present["sp"]
	_, hasOva := present["ova"]
	allInSubset := true
	for k := range present {
		if _, ok := ambiguousKinds[k]; !ok {
			allInSubset = false
			break
		}
	}
	if hasMain && (hasSp || hasOva) && allInSubset {
		mainEps := map[int]struct{}{}
		minEp, maxEp := 0, 0
		first := true
		for _, it := range items {
			if it.ParsedKind == "main" && it.Episode != nil {
				mainEps[*it.Episode] = struct{}{}
				if first || *it.Episode < minEp {
					minEp = *it.Episode
				}
				if first || *it.Episode > maxEp {
					maxEp = *it.Episode
				}
				first = false
			}
		}
		if len(mainEps) >= 2 {
			for ep := minEp; ep <= maxEp; ep++ {
				if _, ok := mainEps[ep]; !ok {
					return true
				}
			}
		}
	}
	return false
}

// sortByEpisodeThenName：集号升序（无集号在最后），再按文件名自然序。
func sortByEpisodeThenName(a, b Item) bool {
	ea, eb := epOrInf(a.Episode), epOrInf(b.Episode)
	if ea != eb {
		return ea < eb
	}
	return localeCompare(a.FileName, b.FileName, true) < 0
}

// epOrInf 复刻 JS 的 `episode ?? Infinity`（分组内排序哨兵与 BuildItems 的 999 不同，原样保留）。
func epOrInf(ep *int) float64 {
	if ep == nil {
		return float64(1 << 62)
	}
	return float64(*ep)
}

// GroupByFolder 按目录分组，返回按 (items 数降序, groupKey 升序) 排序的 Group 列表。
//
// 根目录特例（v3.1 §4 Stage 2）：根下多个文件时逐文件独立成组、禁用同目录簇，
// 避免不同番被错合；单一根文件保留 __root__ 哨兵键。
func GroupByFolder(items []Item) []Group {
	if len(items) == 0 {
		return []Group{}
	}

	buckets := map[string][]Item{}
	var order []string
	for _, it := range items {
		key := deriveGroupKey(it.RelativePath)
		if _, ok := buckets[key]; !ok {
			order = append(order, key)
		}
		buckets[key] = append(buckets[key], it)
	}

	groups := []Group{}
	for _, groupKey := range order {
		raw := buckets[groupKey]

		if groupKey == rootGroupKey && len(raw) > 1 {
			for _, it := range raw {
				groups = append(groups, Group{
					ID:           "g:__root__/" + it.FileID,
					GroupKey:     rootGroupKey + "/" + it.FileName,
					Label:        it.FileName,
					Items:        []Item{it},
					SortMode:     "episode",
					HasAmbiguity: false,
				})
			}
			continue
		}

		hasAmbiguity := detectAmbiguity(raw)
		sortMode := "episode"
		if hasAmbiguity {
			sortMode = "alpha"
		}
		sorted := make([]Item, len(raw))
		copy(sorted, raw)
		if sortMode == "alpha" {
			sort.SliceStable(sorted, func(i, j int) bool {
				return localeCompare(sorted[i].FileName, sorted[j].FileName, true) < 0
			})
		} else {
			sort.SliceStable(sorted, func(i, j int) bool {
				return sortByEpisodeThenName(sorted[i], sorted[j])
			})
		}
		segs := strings.Split(groupKey, "/")
		groups = append(groups, Group{
			ID:           "g:" + groupKey,
			GroupKey:     groupKey,
			Label:        segs[len(segs)-1],
			Items:        sorted,
			SortMode:     sortMode,
			HasAmbiguity: hasAmbiguity,
		})
	}

	sort.SliceStable(groups, func(i, j int) bool {
		if len(groups[i].Items) != len(groups[j].Items) {
			return len(groups[i].Items) > len(groups[j].Items)
		}
		return localeCompare(groups[i].GroupKey, groups[j].GroupKey, false) < 0
	})
	return groups
}
