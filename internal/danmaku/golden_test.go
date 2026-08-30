package danmaku

import (
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 黄金文件再生方法：
//
//	go test ./internal/danmaku -run TestGoldenFile -update
//
// 再生后必须人工检查 testdata/golden.ass 的坐标、时间与轨道分配是否合理，
// 确认无误再提交。日常运行不带 -update，逐字节比对。
var updateGolden = flag.Bool("update", false, "重新生成 testdata/golden.ass")

// goldenComments 固定输入：覆盖三种模式、七种颜色、同刻并轨、追尾换轨、
// 未知模式回落、坏评论与空文本跳过、精确到界的轨道复用、1 小时以上时间戳。
// 改动它必须同步再生黄金文件。
func goldenComments() []Comment {
	return []Comment{
		{CID: 1, P: "0.00,1,16777215", Text: "前方高能预警！"},
		{CID: 2, P: "0.00,1,16711680", Text: "红色弹幕来了"}, // 同刻 → 滚动 1 号轨
		{CID: 3, P: "1.20,5,16777215", Text: "顶部白字"},
		{CID: 4, P: "1.20,5,16776960", Text: "顶部黄字"}, // 同刻 → 顶部 1 号轨
		{CID: 5, P: "2.00,4,65280", Text: "底部绿字"},
		{CID: 6, P: "2.50,4,65535", Text: "底部青字 cyan"}, // 底轨忙 → 上抬一轨
		{CID: 7, P: "3.00,1,255", Text: "Blue scrolling comment in ASCII"},
		{CID: 8, P: "3.00,2,16777215", Text: "mode=2 也按滚动处理"},
		{CID: 9, P: "4.75,1,16777215", Text: "很长很长很长很长很长很长很长的一条弹幕追上前车"},
		{CID: 10, P: "6.00,5,16777215", Text: "第二批顶部弹幕"},       // 顶 0/1 号轨 6.2 才释放 → 2 号轨
		{CID: 11, P: "7.00,4,16777215", Text: "第二批底部弹幕"},       // 底 0 号轨恰于 7.0 释放 → 复用
		{CID: 12, P: "59.99,1,9868950", Text: "灰色弹幕 0x969696"}, // 时间边界 59.99
		{CID: 13, P: "3600.00,1,16777215", Text: "一小时后的滚动弹幕"},
		{CID: 14, P: "3600.00,4,16777215", Text: "Bottom at exactly one hour"},
		{CID: 15, P: "bad,1,123", Text: "坏评论不会出现在输出里"}, // → Skipped
		{CID: 16, P: "8.00,1,16777215", Text: "   "},   // 清洗后为空 → Skipped
	}
}

func TestGoldenFile(t *testing.T) {
	out, st := Convert(goldenComments(), Options{})
	assert.Equal(t, Stats{Total: 16, Converted: 14, Skipped: 2}, st)

	goldenPath := filepath.Join("testdata", "golden.ass")
	if *updateGolden {
		require.NoError(t, os.MkdirAll("testdata", 0o755))
		require.NoError(t, os.WriteFile(goldenPath, []byte(out), 0o644))
		t.Log("已重新生成", goldenPath)
	}
	want, err := os.ReadFile(goldenPath)
	require.NoError(t, err, "黄金文件缺失时先用 -update 生成并人工检查")
	assert.Equal(t, string(want), out, "输出必须与黄金文件逐字节一致")
}

func TestGoldenCoversAllModes(t *testing.T) {
	out, _ := Convert(goldenComments(), Options{})
	assert.Contains(t, out, `\move(`, "含滚动弹幕")
	assert.Contains(t, out, `\an8\pos(`, "含顶部固定弹幕")
	assert.Contains(t, out, `\an2\pos(`, "含底部固定弹幕")
	assert.Len(t, dialogueLines(t, out), 14)
}
