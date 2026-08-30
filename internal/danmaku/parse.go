package danmaku

import (
	"math"
	"strconv"
	"strings"
)

// 弹幕模式（p 串第 2 段）。dandanplay 约定 4=底部固定、5=顶部固定，
// 其余一切取值（含 2、3、6 等历史值）都按 1（右→左滚动）处理。
const (
	modeScroll = 1
	modeBottom = 4
	modeTop    = 5
)

// parsed 是一条通过校验、完成清洗、等待排轨的弹幕。
type parsed struct {
	start float64 // 出现时间（秒），已保证非负且有限
	mode  int     // modeScroll / modeBottom / modeTop 之一
	color int     // 0xRRGGBB
	text  string  // 清洗后的正文，非空
}

// parseComment 解析一条 dandanplay 评论并清洗正文。
// ok=false 表示坏评论，调用方应计入 Stats.Skipped：
//   - p 逗号分段不足 3 段；
//   - 时间解析失败、为负、NaN 或 ±Inf；
//   - 模式不是整数；
//   - 颜色不是整数，或超出 0..0xFFFFFF；
//   - 正文清洗后为空。
//
// p 第 4 段起（uid 等）一律忽略。
func parseComment(c Comment) (parsed, bool) {
	parts := strings.Split(c.P, ",")
	if len(parts) < 3 {
		return parsed{}, false
	}
	start, err := strconv.ParseFloat(parts[0], 64)
	if err != nil || math.IsNaN(start) || math.IsInf(start, 0) || start < 0 {
		return parsed{}, false
	}
	mode, err := strconv.Atoi(parts[1])
	if err != nil {
		return parsed{}, false
	}
	if mode != modeBottom && mode != modeTop {
		mode = modeScroll
	}
	color, err := strconv.Atoi(parts[2])
	if err != nil || color < 0 || color > 0xFFFFFF {
		return parsed{}, false
	}
	text := sanitizeText(c.Text)
	if text == "" {
		return parsed{}, false
	}
	return parsed{start: start, mode: mode, color: color, text: text}, true
}

// assTextSanitizer 做两类替换：
//   - 压成单行：CRLF 折成一个空格，落单的 CR / LF 也各换成一个空格；
//   - ASS 特殊字符换成全角形近字符，防止正文被 libass 当成 override tag 解析
//     （反斜杠注入 \move、花括号闭合样式块都是真实攻击面）：
//     '{' → '｛'，'}' → '｝'，'\' → '＼'
var assTextSanitizer = strings.NewReplacer(
	"\r\n", " ",
	"\r", " ",
	"\n", " ",
	"{", "｛",
	"}", "｝",
	`\`, "＼",
)

// sanitizeText 清洗弹幕正文：先替换特殊字符，再去首尾空白。
// 返回空串表示这条弹幕没有可显示内容，调用方应跳过。
func sanitizeText(s string) string {
	return strings.TrimSpace(assTextSanitizer.Replace(s))
}
