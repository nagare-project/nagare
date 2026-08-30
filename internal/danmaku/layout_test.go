package danmaku

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTextWidth(t *testing.T) {
	assert.Equal(t, 24, textWidth("a", 48), "ASCII 半宽 = 0.5 × 字号")
	assert.Equal(t, 96, textWidth("字字", 48), "CJK 全宽 = 1.0 × 字号")
	assert.Equal(t, 72, textWidth("a字", 48), "混排逐 rune 累加")
	assert.Equal(t, 25, textWidth("a", 49), "奇数乘积向上取整")
	assert.Equal(t, 24, textWidth("ｱ", 48), "半角片假名按半宽")
	assert.Equal(t, 48, textWidth("！", 48), "全角标点按全宽")
}

func TestScrollSameTimeDifferentTracks(t *testing.T) {
	out, st := Convert([]Comment{
		{CID: 1, P: "0,1,16777215", Text: "同时出现一"},
		{CID: 2, P: "0,1,16777215", Text: "同时出现二"},
	}, Options{})
	require.Equal(t, 2, st.Converted)
	assert.Zero(t, st.Overlapped)
	lines := dialogueLines(t, out)
	// 默认字号 48 → 行高 round(48×1.2)=58：0 号轨 y=0，1 号轨 y=58。
	assert.Contains(t, lines[0], `\move(1920,0,`)
	assert.Contains(t, lines[1], `\move(1920,58,`)
}

func TestScrollChasePushedToNextTrack(t *testing.T) {
	// 0 号轨先放一条短弹幕（慢车，宽 48px）。0.5s 后来一条 20 个汉字的长弹幕
	//（快车，宽 960px，速度 (1920+960)/12=240px/s）。离场判据要求
	// tB ≥ tA + D·LB/(W+LB) = 0 + 12×960/2880 = 4.0s；0.5 < 4.0 → 0 号轨
	// 对它不空闲，必须推到 1 号轨（换轨解决，不算重叠）。
	long := strings.Repeat("追", 20)
	out, st := Convert([]Comment{
		{CID: 1, P: "0,1,16777215", Text: "ab"},
		{CID: 2, P: "0.5,1,16777215", Text: long},
	}, Options{})
	require.Equal(t, 2, st.Converted)
	assert.Zero(t, st.Overlapped, "换轨解决追尾，不计入 Overlapped")
	lines := dialogueLines(t, out)
	assert.Contains(t, lines[0], `\move(1920,0,`)
	assert.Contains(t, lines[1], `\move(1920,58,`, "快车会追尾慢车 → 被推到下一条轨")

	// 对照组：同样的长弹幕晚到 4.0s 之后 → 两个判据都满足，留在 0 号轨。
	out2, _ := Convert([]Comment{
		{CID: 1, P: "0,1,16777215", Text: "ab"},
		{CID: 2, P: "4.1,1,16777215", Text: long},
	}, Options{})
	lines2 := dialogueLines(t, out2)
	assert.Contains(t, lines2[1], `\move(1920,0,`, "安全间隔后可复用 0 号轨")
}

func TestScrollOverflowForcesPlacement(t *testing.T) {
	// 默认几何：可用高 floor(1080×0.72)=777，行高 58 → 13 条滚动轨道。
	// 同一时刻塞 15 条 → 前 13 条各占一轨，后 2 条强制放置并计入 Overlapped，
	// 但输出一条不少。
	comments := make([]Comment, 0, 15)
	for i := 0; i < 15; i++ {
		comments = append(comments, Comment{CID: int64(i), P: "0,1,16777215", Text: "满屏弹幕"})
	}
	out, st := Convert(comments, Options{})
	assert.Equal(t, 15, st.Converted, "强制放置不丢弃任何弹幕")
	assert.Equal(t, 2, st.Overlapped)
	assert.Len(t, dialogueLines(t, out), 15)
}

func TestStillTracksAllocation(t *testing.T) {
	out, st := Convert([]Comment{
		{CID: 1, P: "0,5,16777215", Text: "顶一"},
		{CID: 2, P: "0,5,16777215", Text: "顶二"}, // 同刻 → 顶部第二轨
		{CID: 3, P: "6,5,16777215", Text: "顶三"}, // 0+5=5 ≤ 6 → 复用 0 号顶轨
		{CID: 4, P: "0,4,16777215", Text: "底一"},
		{CID: 5, P: "1,4,16777215", Text: "底二"}, // 0 号底轨占用到 5 → 上抬一轨
	}, Options{})
	require.Equal(t, 5, st.Converted)
	lines := dialogueLines(t, out)
	require.Len(t, lines, 5)
	// 排序后顺序：顶一(0) 顶二(0) 底一(0) 底二(1) 顶三(6)。
	assert.Contains(t, lines[0], `\an8\pos(960,0)`, "顶部自上而下：0 号轨 y=0")
	assert.Contains(t, lines[1], `\an8\pos(960,58)`, "顶部 1 号轨 y=58")
	assert.Contains(t, lines[2], `\an2\pos(960,777)`, "底部 0 号轨下缘贴可用区域底边 777")
	assert.Contains(t, lines[3], `\an2\pos(960,719)`, "底部 1 号轨上抬一个行高")
	assert.Contains(t, lines[4], `\an8\pos(960,0)`, "占用期满后复用 0 号顶轨")
}

func TestStillOverflowForcesPlacement(t *testing.T) {
	// 13 条顶轨全占后再来一条 → 无空轨，强制放置并计数。
	comments := make([]Comment, 0, 14)
	for i := 0; i < 14; i++ {
		comments = append(comments, Comment{CID: int64(i), P: "0,5,16777215", Text: "顶部"})
	}
	_, st := Convert(comments, Options{})
	assert.Equal(t, 14, st.Converted)
	assert.Equal(t, 1, st.Overlapped)
}

func TestForcedPlacementPicksEarliestFreeTrack(t *testing.T) {
	// FontSize 300 → 行高 360 → floor(777/360)=2 条轨。
	// t=0 占 0 号轨（空闲于 5），t=1 占 1 号轨（空闲于 6），t=2 全忙 →
	// 强制放置应选最早空闲的 0 号轨（y=0），而不是随便挑一条。
	out, st := Convert([]Comment{
		{CID: 1, P: "0,5,16777215", Text: "一"},
		{CID: 2, P: "1,5,16777215", Text: "二"},
		{CID: 3, P: "2,5,16777215", Text: "三"},
	}, Options{FontSize: 300})
	assert.Equal(t, 1, st.Overlapped)
	lines := dialogueLines(t, out)
	require.Len(t, lines, 3)
	assert.Contains(t, lines[2], `\an8\pos(960,0)`, "强制放置选最早空闲的轨道")
}
