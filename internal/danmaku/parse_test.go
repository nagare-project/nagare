package danmaku

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// dialogueLines 从 Convert 输出里抽出全部 Dialogue 行，方便逐行断言。
func dialogueLines(t *testing.T, out string) []string {
	t.Helper()
	var lines []string
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, "Dialogue: ") {
			lines = append(lines, l)
		}
	}
	return lines
}

func TestParseCommentTable(t *testing.T) {
	tests := []struct {
		name  string
		p     string
		text  string
		ok    bool
		mode  int
		color int
		start float64
	}{
		{name: "正常滚动", p: "12.34,1,16777215", text: "hello", ok: true, mode: modeScroll, color: 0xFFFFFF, start: 12.34},
		{name: "底部固定", p: "3,4,255", text: "底部", ok: true, mode: modeBottom, color: 0x0000FF, start: 3},
		{name: "顶部固定", p: "0,5,16711680", text: "顶部", ok: true, mode: modeTop, color: 0xFF0000, start: 0},
		{name: "未知模式按滚动", p: "1,7,123", text: "x", ok: true, mode: modeScroll, color: 123, start: 1},
		{name: "负模式按滚动", p: "1,-2,255", text: "x", ok: true, mode: modeScroll, color: 255, start: 1},
		{name: "多余段忽略", p: "1,1,255,123456789,abcdef", text: "x", ok: true, mode: modeScroll, color: 255, start: 1},
		{name: "缺段", p: "1,1", text: "x", ok: false},
		{name: "空 p", p: "", text: "x", ok: false},
		{name: "时间非数字", p: "abc,1,255", text: "x", ok: false},
		{name: "负时间", p: "-0.5,1,255", text: "x", ok: false},
		{name: "模式非数字", p: "1,x,255", text: "x", ok: false},
		{name: "颜色非数字", p: "1,1,red", text: "x", ok: false},
		{name: "颜色超界", p: "1,1,16777216", text: "x", ok: false},
		{name: "颜色为负", p: "1,1,-1", text: "x", ok: false},
		{name: "清洗后为空", p: "1,1,255", text: "  \r\n ", ok: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parseComment(Comment{CID: 1, P: tt.p, Text: tt.text})
			require.Equal(t, tt.ok, ok)
			if !ok {
				return
			}
			assert.Equal(t, tt.mode, got.mode)
			assert.Equal(t, tt.color, got.color)
			assert.InDelta(t, tt.start, got.start, 1e-9)
		})
	}
}

func TestConvertColorOverride(t *testing.T) {
	out, st := Convert([]Comment{
		{CID: 1, P: "1,1,16777215", Text: "white"},
		{CID: 2, P: "2,1,16711680", Text: "red"},
		{CID: 3, P: "3,1,65280", Text: "green"},
		{CID: 4, P: "4,1,255", Text: "blue"},
	}, Options{})
	require.Equal(t, Stats{Total: 4, Converted: 4}, st)
	lines := dialogueLines(t, out)
	require.Len(t, lines, 4)
	assert.NotContains(t, lines[0], `\c&H`, "白色用基础样式，不加颜色覆写")
	assert.Contains(t, lines[1], `\c&H0000FF&`, "BGR 序：红 0xFF0000 → RR 落在最后一组")
	assert.Contains(t, lines[2], `\c&H00FF00&`, "绿 0x00FF00")
	assert.Contains(t, lines[3], `\c&HFF0000&`, "BGR 序：蓝 0x0000FF → BB 落在第一组")
	for _, l := range lines {
		assert.Contains(t, l, `\alpha&H40&`, "默认不透明度 0.75 → alpha 0x40")
	}
}

func TestConvertSkipsBadComments(t *testing.T) {
	out, st := Convert([]Comment{
		{CID: 1, P: "bad,1,255", Text: "a"},
		{CID: 2, P: "1,1", Text: "b"},
		{CID: 3, P: "-1,1,255", Text: "c"},
		{CID: 4, P: "1,1,255", Text: "   \r\n  "},
		{CID: 5, P: "2,1,255", Text: "ok"},
	}, Options{})
	assert.Equal(t, Stats{Total: 5, Converted: 1, Skipped: 4}, st)
	require.Len(t, dialogueLines(t, out), 1)
}

func TestSanitizeText(t *testing.T) {
	tests := []struct{ name, in, want string }{
		{"去首尾空白", "  hello  ", "hello"},
		{"LF 压成空格", "a\nb", "a b"},
		{"CRLF 压成单个空格", "a\r\nb", "a b"},
		{"CR 压成空格", "a\rb", "a b"},
		{"花括号转全角", "{危}", "｛危｝"},
		{"反斜杠转全角防注入", `\N\move(0,0,1,1)`, "＼N＼move(0,0,1,1)"},
		{"override 注入整体失效", `{\pos(0,0)}x`, "｛＼pos(0,0)｝x"},
		{"清洗后为空", " \r\n \n ", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, sanitizeText(tt.in))
		})
	}
}

func TestFormatTime(t *testing.T) {
	tests := []struct {
		in   float64
		want string
	}{
		{0, "0:00:00.00"},
		{12.34, "0:00:12.34"},
		{59.99, "0:00:59.99"},
		{59.999, "0:01:00.00"}, // 厘秒四舍五入后正确进位
		{3600, "1:00:00.00"},
		{3661.05, "1:01:01.05"},
		{35999.99, "9:59:59.99"},
		{-1, "0:00:00.00"}, // 防御性钳位；负时间实际在解析层已拒绝
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, formatTime(tt.in), "formatTime(%v)", tt.in)
	}
}

func TestOpacityToAlpha(t *testing.T) {
	assert.Equal(t, 0x40, alphaByte(0.75))
	assert.Equal(t, 0x80, alphaByte(0.5)) // round(127.5) 远离零舍入 → 128
	assert.Equal(t, 0, alphaByte(1))

	out, _ := Convert([]Comment{{CID: 1, P: "1,1,255", Text: "x"}}, Options{Opacity: 1})
	assert.NotContains(t, out, `\alpha`, "完全不透明时不写 alpha 标签")
}
