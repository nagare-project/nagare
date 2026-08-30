// 簇裁决与置信度 —— 移植自 animego seriesMatcher.js 的纯逻辑部分（决议 CQ2）。
// 原版还负责构造 IndexedDB 记录（ulid、Series/Episode/FileRef 行），那部分是
// 存储专属，nagare 的存储层自建记录 —— 这里只保留跨端必须一致的裁决与置信度。
package library

// VerdictKind 是簇裁决结果类型。
type VerdictKind string

const (
	VerdictReuse  VerdictKind = "reuse"  // 命中已有季，复用
	VerdictNew    VerdictKind = "new"    // 新系列
	VerdictFailed VerdictKind = "failed" // 空簇等异常
)

// Verdict 是单个簇对既有库的匹配裁决。
type Verdict struct {
	Kind       VerdictKind
	Confidence float64 // kind==new 时有效，0..1，<0.7 需用户确认
	// kind==reuse 时有效：
	ReuseSeriesID string
	ReuseSeasonID string
	ReuseAnimeID  int
	// kind==failed 时有效：
	Reason string
}

// hasConsecutiveEpisodes：≥3 条非空集号且排序后连续无空洞。
func hasConsecutiveEpisodes(items []Item) bool {
	var eps []int
	for _, it := range items {
		if it.Episode != nil {
			eps = append(eps, *it.Episode)
		}
	}
	if len(eps) < 3 {
		return false
	}
	// 插入排序足矣（簇内条目量级很小）。
	for i := 1; i < len(eps); i++ {
		for j := i; j > 0 && eps[j] < eps[j-1]; j-- {
			eps[j], eps[j-1] = eps[j-1], eps[j]
		}
	}
	for i := 1; i < len(eps); i++ {
		if eps[i] != eps[i-1]+1 {
			return false
		}
	}
	return true
}

// computeConfidence：
//   - 0.9：有标题且 ≥3 集连续
//   - 0.7：有标题且 ≥2 条带集号
//   - 0.5：兜底
func computeConfidence(c Cluster) float64 {
	hasTitle := c.Representative != nil &&
		c.Representative.ParsedTitle != nil && *c.Representative.ParsedTitle != ""

	if hasTitle && hasConsecutiveEpisodes(c.Items) {
		return 0.9
	}
	withEp := 0
	for _, it := range c.Items {
		if it.Episode != nil {
			withEp++
		}
	}
	if hasTitle && withEp >= 2 {
		return 0.7
	}
	return 0.5
}

// MatchCluster 对单个簇做裁决：先看 AnimeIDHint 能否复用既有季，否则判为新系列并给出置信度。
func MatchCluster(c Cluster, priors []PriorSeason) Verdict {
	if len(c.Items) == 0 {
		return Verdict{Kind: VerdictFailed, Reason: "empty cluster"}
	}

	if c.AnimeIDHint != nil {
		for _, s := range priors {
			if s.AnimeID == *c.AnimeIDHint {
				return Verdict{
					Kind:          VerdictReuse,
					ReuseSeriesID: s.SeriesID,
					ReuseSeasonID: s.SeasonID,
					ReuseAnimeID:  s.AnimeID,
				}
			}
		}
		// 有提示但找不到对应季 → 走 new。
	}

	return Verdict{Kind: VerdictNew, Confidence: computeConfidence(c)}
}
