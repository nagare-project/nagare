package library

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 新增一个 DropReason 却忘了配文案 —— 界面上会出现一条「3 项：」后面什么都没有
// 的空提示，而所有既有测试仍然全绿。这条把三张表钉在一起。
func TestEveryDropReasonHasCopyAndOrder(t *testing.T) {
	all := []DropReason{
		DropSymlink, DropTooSmall, DropStatFailed, DropTooDeep, DropUnreadableDir,
	}
	assert.ElementsMatch(t, all, dropReasonOrder, "dropReasonOrder 与常量列表不同步")
	require.Len(t, dropCopy, len(all), "dropCopy 与常量列表不同步")

	for _, r := range all {
		text, ok := dropCopy[r]
		require.Truef(t, ok, "%s 没有文案", r)
		assert.NotEmptyf(t, text.UserMsg, "%s 的 UserMsg 为空", r)
		// 恢复动作是这套东西存在的理由：只告诉用户「跳过了」而不告诉他
		// 怎么把文件找回来，等于把静默失败换成了嘈杂失败。
		assert.NotEmptyf(t, text.Recovery, "%s 没有恢复动作", r)
	}
}

// 汇总按固定序输出，且计数与样本对得上。
func TestDropCollectorSummary(t *testing.T) {
	c := newDropCollector()
	c.add(DropTooSmall, "a/sample.mkv")
	c.add(DropSymlink, "link-1")
	c.add(DropSymlink, "link-2")
	c.add(DropTooDeep, "a/b/c/d")

	got := c.summary()
	assert.Equal(t, 4, got.Total)
	// 顺序按 dropReasonOrder：越靠前越「用户改得动」，不随 map 迭代乱跳
	assert.Equal(t, []DropReason{DropSymlink, DropTooDeep, DropTooSmall},
		[]DropReason{got.Groups[0].Reason, got.Groups[1].Reason, got.Groups[2].Reason})
	assert.Equal(t, []string{"link-1", "link-2"}, got.Groups[0].Samples)
	assert.Equal(t, 2, got.Groups[0].Count)
}

// 样本封顶但计数不封顶：一棵两千文件的软链树要显示 2000，而不是 5。
func TestDropCollectorCapsSamplesNotCounts(t *testing.T) {
	c := newDropCollector()
	for i := 0; i < 200; i++ {
		c.add(DropSymlink, "link")
	}
	g := c.summary().Groups[0]
	assert.Equal(t, 200, g.Count)
	assert.Len(t, g.Samples, maxDropSamples)
}

func TestDropCollectorMergeRespectsCap(t *testing.T) {
	dst := newDropCollector()
	dst.add(DropSymlink, "a")
	src := newDropCollector()
	for i := 0; i < 10; i++ {
		src.add(DropSymlink, "b")
	}
	src.add(DropTooSmall, "c")

	dst.merge(src)
	got := dst.summary()
	assert.Equal(t, 12, got.Total)
	assert.Equal(t, 11, dropCounts(got)[DropSymlink])
	assert.Len(t, dropGroup(t, got, DropSymlink).Samples, maxDropSamples)
	assert.Equal(t, []string{"c"}, dropGroup(t, got, DropTooSmall).Samples)
}

// 什么都没丢时是干干净净的零值：界面据此决定【完全不出】提示。
func TestDropCollectorEmptySummary(t *testing.T) {
	got := newDropCollector().summary()
	assert.Equal(t, 0, got.Total)
	assert.Empty(t, got.Groups)
}
