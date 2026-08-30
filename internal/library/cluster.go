// 跨目录聚簇 —— 从 animego clusterizer.js 逐行移植（决议 CQ2）。
package library

import (
	"fmt"
	"sort"
	"strings"
	"unicode/utf16"
)

// Cluster 是归一化标题相同的若干 Group 的合并簇。
type Cluster struct {
	ClusterKey     string   // tokens 非空时为桶键的 FNV-1a hex，否则退化为 groupKey
	Tokens         []string // 归一化 token（不含 #S 季后缀）
	Groups         []Group
	Items          []Item // 展平并按 (groupKey, episode|fileName) 排序
	Representative *Item  // 首个 episode 非空且 kind==main 的条目，否则第一条
	AnimeIDHint    *int   // 命中 priorSeasons 标题时预填
}

// PriorSeason 是已存在的季记录（用于跨次导入的复用提示）。
type PriorSeason struct {
	SeriesID  string
	SeasonID  string
	AnimeID   int
	TitleHint string
}

// fnv1aUTF16 复刻 JS 版 FNV-1a：按 UTF-16 code unit 迭代（charCodeAt 语义），
// 32 位无符号，输出 8 位小写 hex。按字节算会让所有 CJK 桶键的 hash 与网站端漂移。
func fnv1aUTF16(s string) string {
	h := uint32(0x811c9dc5)
	for _, u := range utf16.Encode([]rune(s)) {
		h ^= uint32(u)
		h *= 0x01000193
	}
	return fmt.Sprintf("%08x", h)
}

// pickRepresentative 选簇代表：首个「有集号且是正片」的条目，否则第一条。
func pickRepresentative(items []Item) *Item {
	if len(items) == 0 {
		return nil
	}
	for i := range items {
		if items[i].Episode != nil && items[i].ParsedKind == "main" {
			return &items[i]
		}
	}
	return &items[0]
}

// sortClusterItems 簇内排序：先按所属 groupKey，再按集号升序（空集号最后），再按文件名自然序。
func sortClusterItems(items []Item, groups []Group) []Item {
	fileGroupKey := map[string]string{}
	for _, g := range groups {
		for _, it := range g.Items {
			fileGroupKey[it.FileID] = g.GroupKey
		}
	}
	sorted := make([]Item, len(items))
	copy(sorted, items)
	sort.SliceStable(sorted, func(i, j int) bool {
		ga, gb := fileGroupKey[sorted[i].FileID], fileGroupKey[sorted[j].FileID]
		if ga != gb {
			return localeCompare(ga, gb, false) < 0
		}
		ea, eb := epOrInf(sorted[i].Episode), epOrInf(sorted[j].Episode)
		if ea != eb {
			return ea < eb
		}
		return localeCompare(sorted[i].FileName, sorted[j].FileName, true) < 0
	})
	return sorted
}

type clusterBucket struct {
	tokens []string
	groups []Group
}

// Clusterize 按归一化 parsedTitle 聚簇。同名不同季（parsedSeason）会因
// `#S<n>` 后缀拆开——Re:Zero 四季的 token 完全相同，全靠它分簇。
// tokens 为空的 Group 退化为以 groupKey 为键的单例簇。
func Clusterize(groups []Group, priors []PriorSeason) []Cluster {
	if len(groups) == 0 {
		return []Cluster{}
	}

	// priorSeasons 标题 → animeId 提示索引（键不含季后缀）。
	seasonIndex := map[string]int{}
	for _, s := range priors {
		if s.TitleHint == "" {
			continue
		}
		tokens := NormalizeTokens(s.TitleHint)
		if len(tokens) > 0 {
			seasonIndex[strings.Join(tokens, "|")] = s.AnimeID
		}
	}

	buckets := map[string]*clusterBucket{}
	var order []string

	for _, g := range groups {
		var source string
		var season *int
		if len(g.Items) > 0 {
			if g.Items[0].ParsedTitle != nil {
				source = *g.Items[0].ParsedTitle
			} else {
				source = g.Label
			}
			season = g.Items[0].ParsedSeason
		} else {
			source = g.Label
		}
		tokens := NormalizeTokens(source)
		seasonSuffix := ""
		if season != nil {
			seasonSuffix = fmt.Sprintf("#S%d", *season)
		}

		if len(tokens) == 0 {
			// 单例桶：groupKey 作唯一键（对齐 JS：无条件 set + push order）。
			key := g.GroupKey + seasonSuffix
			buckets[key] = &clusterBucket{tokens: []string{}, groups: []Group{g}}
			order = append(order, key)
			continue
		}

		bucketKey := strings.Join(tokens, "|") + seasonSuffix
		if _, ok := buckets[bucketKey]; !ok {
			buckets[bucketKey] = &clusterBucket{tokens: tokens}
			order = append(order, bucketKey)
		}
		buckets[bucketKey].groups = append(buckets[bucketKey].groups, g)
	}

	clusters := []Cluster{}
	for _, key := range order {
		b := buckets[key]

		clusterKey := key
		if len(b.tokens) > 0 {
			clusterKey = fnv1aUTF16(key)
		}

		var allItems []Item
		for _, g := range b.groups {
			allItems = append(allItems, g.Items...)
		}
		items := sortClusterItems(allItems, b.groups)

		c := Cluster{
			ClusterKey:     clusterKey,
			Tokens:         b.tokens,
			Groups:         b.groups,
			Items:          items,
			Representative: pickRepresentative(items),
		}
		if len(b.tokens) > 0 {
			if animeID, ok := seasonIndex[strings.Join(b.tokens, "|")]; ok {
				c.AnimeIDHint = &animeID
			}
		}
		clusters = append(clusters, c)
	}
	return clusters
}
