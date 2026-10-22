package torrentstream

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	errs "github.com/nagare-project/nagare/internal/errors"
)

// 合集里挑上一集 / 下一集：与本地媒体库同一条规则（episodenav.Pick）。
func TestSiblingOfWithinPack(t *testing.T) {
	// 种子内下标就是切片下标。一个合集里混着两部番、带一个 NCOP、一个 SP，第 2 集有两个子目录各一份。
	entries := []fileEntry{
		{Index: 0, Path: "Pack/[G] Show - 01 [1080p].mkv", Size: 100},
		{Index: 1, Path: "Pack/[G] Show - 03 [1080p].mkv", Size: 100},
		{Index: 2, Path: "Pack/[G] Show - NCOP [1080p].mkv", Size: 100},
		{Index: 3, Path: "Pack/[G] Other - 04 [1080p].mkv", Size: 100},
		{Index: 4, Path: "Pack/[G] Other - 05 [1080p].mkv", Size: 100},
		{Index: 5, Path: "Pack/[G] Show - SP1 [1080p].mkv", Size: 100},
		{Index: 6, Path: "Pack/A/[G] Show - 02 [1080p].mkv", Size: 100},
		{Index: 7, Path: "Pack/[G] Show - 02 [1080p].mkv", Size: 100},
		{Index: 8, Path: "Pack/readme.txt", Size: 100},
	}
	cands, _ := buildCandidates(entries)
	const ih = "0123456789abcdef0123456789abcdef01234567"

	tests := []struct {
		name  string
		index int
		dir   int
		want  int // -1 表示没有
	}{
		{"第 1 集的下一集：同目录那份第 2 集", 0, 1, 7},
		{"第 1 集没有上一集", 0, -1, -1},
		{"第 3 集的上一集", 1, -1, 7},
		{"第 3 集没有下一集：另一部番的第 4 集不算", 1, 1, -1},
		{"另一部番的第 4 集：下一集是它自己的第 5 集", 3, 1, 4},
		{"另一部番的第 4 集：上一集不跨到这部番", 3, -1, -1},
		{"子目录里那份第 2 集：下一集是第 3 集", 6, 1, 1},
		{"NCOP 被滤掉了，不是候选", 2, 1, -1},
		{"SP 不是正片，没有邻居", 5, 1, -1},
		{"不在候选里的下标", 99, 1, -1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := siblingOf(cands, ih, tc.index, tc.dir)
			if tc.want < 0 {
				assert.False(t, ok, "不该有邻居，却挑到了 %d", got.index)
				return
			}
			require.True(t, ok)
			assert.Equal(t, tc.want, got.index)
		})
	}
}

// 单集种子没有邻居；只有一份候选时上下都是空。
func TestSiblingOfSingleEpisode(t *testing.T) {
	cands, _ := buildCandidates([]fileEntry{{Index: 0, Path: "[G] Show - 07 [1080p].mkv", Size: 1}})
	for _, dir := range []int{-1, 1} {
		_, ok := siblingOf(cands, testInfohash, 0, dir)
		assert.False(t, ok)
	}
}

// 零值 Source（会话不存在）：没有邻居，切集给出分类错误而不是空指针。
func TestZeroSourceHasNoSiblings(t *testing.T) {
	src := &Source{}
	prev, next := src.Siblings()
	assert.Nil(t, prev)
	assert.Nil(t, next)

	_, err := src.OpenSibling(context.Background(), 1)
	var ce *errs.E
	require.ErrorAs(t, err, &ce)
	assert.Equal(t, errs.CategoryTorrent, ce.Category)
	assert.Contains(t, ce.UserMsg, "下一集")
	assert.NotEmpty(t, ce.Recovery)

	_, err = src.OpenSibling(context.Background(), 0)
	require.ErrorAs(t, err, &ce)
	assert.Equal(t, errs.CategoryInternal, ce.Category)
}
