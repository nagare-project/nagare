package rules

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func ip(n int) *int       { return &n }
func sp(s string) *string { return &s }

func TestSourceRanks(t *testing.T) {
	rules := []*Rule{{ID: "garden"}, {ID: "acg"}, {ID: "tosho", Capabilities: Capabilities{Priority: 10}}}
	ranks := sourceRanks(rules)
	assert.Equal(t, 3, ranks["tosho"], "priority 高者最高分")
	assert.Equal(t, 2, ranks["garden"], "同分按注册顺序")
	assert.Equal(t, 1, ranks["acg"])
}

func TestDedupByInfohash(t *testing.T) {
	m := "magnet:?xt=urn:btih:" + "aa" + "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	ranks := map[string]int{"a": 2, "b": 1}
	items := []Item{
		{Title: "first", Magnet: m, Source: "b"},
		{Title: "nohash", Magnet: "magnet:?dn=x", Source: "a"},
		{Title: "better", Magnet: m, Source: "a", Seeders: ip(3)},
		{Title: "worse", Magnet: m, Source: "a"},
	}
	out := dedupByInfohash(items, ranks)
	require.Len(t, out, 2)
	assert.Equal(t, "better", out[0].Title, "同 hash 取做种数多者，且占据首次出现的位置")
	assert.Equal(t, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", out[0].Infohash, "存活条目盖上归一化 hash")
	assert.Equal(t, "nohash", out[1].Title, "无 hash 条目原样透传")
	assert.Equal(t, "", out[1].Infohash)
}

func TestRankItems(t *testing.T) {
	ranks := map[string]int{"hi": 2, "lo": 1}
	items := []Item{
		{Title: "old-lo", Date: sp("Sat, 01 Jan 2000 00:00:00 +0000"), Source: "lo"},
		{Title: "seed0", Seeders: ip(0), Source: "lo"},
		{Title: "new-hi", Date: sp("2026-01-01T00:00:00Z"), Source: "hi"},
		{Title: "new-lo", Date: sp("2026-01-01T00:00:00Z"), Source: "lo"},
		{Title: "seed9", Seeders: ip(9), Source: "lo"},
		{Title: "nodate", Source: "hi"},
	}
	out := rankItems(items, ranks)
	titles := make([]string, len(out))
	for i, it := range out {
		titles[i] = it.Title
	}
	assert.Equal(t, []string{"seed9", "seed0", "new-hi", "new-lo", "old-lo", "nodate"}, titles,
		"做种数降序（已知 0 仍在未知之上）→ 日期降序 → 源分数 → 无日期最末")
}
