package danmaku

import (
	"fmt"
	"math"
	"strings"
)

// 画布分辨率固定 1920×1080：所有坐标按此计算，libass 会随实际窗口等比缩放。
const (
	playResX = 1920
	playResY = 1080
)

// colorWhite 是 dandanplay 的默认弹幕色，等于基础样式主色，无需行内覆写。
const colorWhite = 0xFFFFFF

// assHeader 是 Script Info、样式段与事件表头。
// 基础样式 Danmaku：白色主色、黑色 1px 描边（Outline=1）、无阴影（Shadow=0）、
// 不加粗（Bold=0），Alignment=7（左上锚点）——\move/\pos 坐标即文本框左上角，
// 排轨换算最直接。个别弹幕的颜色/透明度用行内 override 叠加。
const assHeader = `[Script Info]
; 由 nagare 从 dandanplay 弹幕生成，请勿手工编辑
ScriptType: v4.00+
PlayResX: %d
PlayResY: %d
WrapStyle: 2
ScaledBorderAndShadow: yes

[V4+ Styles]
Format: Name, Fontname, Fontsize, PrimaryColour, SecondaryColour, OutlineColour, BackColour, Bold, Italic, Underline, StrikeOut, ScaleX, ScaleY, Spacing, Angle, BorderStyle, Outline, Shadow, Alignment, MarginL, MarginR, MarginV, Encoding
Style: Danmaku,%s,%d,&H00FFFFFF,&H00FFFFFF,&H00000000,&H00000000,0,0,0,0,100,100,0,0,1,1,0,7,0,0,0,1

[Events]
Format: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text
`

// writeHeader 写入 ASS 头。字体名与字号来自 Options（已填默认值）。
func writeHeader(b *strings.Builder, o Options) {
	fmt.Fprintf(b, assHeader, playResX, playResY, o.FontName, o.FontSize)
}

// writeDialogue 把一条事件渲染成 Dialogue 行：
//   - 滚动：{\move(1920,y,-width,y)}，从右缘外平移到左缘外（x2=-文本宽 → 尾部恰好离屏）；
//   - 顶部：{\an8\pos(960,y)}（\an8 覆盖行锚点为上中，y 是文本上缘）；
//   - 底部：{\an2\pos(960,y)}（锚点改为下中，y 是文本下缘）；
//   - 颜色非白追加 \c&HBBGGRR&（ASS 行内色是 BGR 序）；Opacity<1 追加 \alpha&HXX&。
func writeDialogue(b *strings.Builder, ev event, o Options) {
	var tags strings.Builder
	switch ev.mode {
	case modeTop:
		fmt.Fprintf(&tags, `\an8\pos(%d,%d)`, playResX/2, ev.y)
	case modeBottom:
		fmt.Fprintf(&tags, `\an2\pos(%d,%d)`, playResX/2, ev.y)
	default:
		fmt.Fprintf(&tags, `\move(%d,%d,%d,%d)`, playResX, ev.y, -ev.width, ev.y)
	}
	if ev.color != colorWhite {
		// dandanplay 颜色是十进制 RGB；ASS 写作 &HBBGGRR&，字节序要反过来。
		fmt.Fprintf(&tags, `\c&H%02X%02X%02X&`,
			ev.color&0xFF, (ev.color>>8)&0xFF, (ev.color>>16)&0xFF)
	}
	if a := alphaByte(o.Opacity); a > 0 {
		fmt.Fprintf(&tags, `\alpha&H%02X&`, a)
	}
	fmt.Fprintf(b, "Dialogue: 0,%s,%s,Danmaku,,0,0,0,,{%s}%s\n",
		formatTime(ev.start), formatTime(ev.end), tags.String(), ev.text)
}

// alphaByte 把不透明度换算成 ASS 的 alpha 字节：0x00 全不透明、0xFF 全透明。
// 例：Opacity=0.75 → round((1-0.75)×255) = 0x40。
func alphaByte(opacity float64) int {
	a := int(math.Round((1 - opacity) * 255))
	if a < 0 {
		return 0
	}
	if a > 255 {
		return 255
	}
	return a
}

// formatTime 把秒格式化成 ASS 的 H:MM:SS.cc（cc=厘秒）。
// 先整体换算成厘秒再拆分进位，保证 59.999 这类边界四舍五入后正确进到 0:01:00.00。
func formatTime(seconds float64) string {
	cs := int64(math.Round(seconds * 100))
	if cs < 0 {
		cs = 0 // 负时间在解析层已拒绝，这里仅作防御
	}
	return fmt.Sprintf("%d:%02d:%02d.%02d",
		cs/360000, cs%360000/6000, cs%6000/100, cs%100)
}
