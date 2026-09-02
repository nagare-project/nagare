package rules

import (
	"math"
	"regexp"
	"strconv"
	"strings"
)

// 本文件是 animego 旧适配器数值/文本工具的逐行移植；行为由黄金文件钉死。
// 它们刻意保留 JS parseInt / toFixed 的怪癖 —— 改动会让六源输出与网站端漂移。

// parseIntJSLike 模拟 JS 的 parseInt(s, 10)：跳过前导空白、可选正负号、
// 贪婪吃数字、遇非数字截断（"1234abc" → 1234）；无数字或溢出 → (0,false)。
func parseIntJSLike(raw string) (int, bool) {
	s := strings.TrimLeft(raw, " \t\n\r\f\v")
	if s == "" {
		return 0, false
	}
	neg := false
	switch s[0] {
	case '+':
		s = s[1:]
	case '-':
		neg = true
		s = s[1:]
	}
	end := 0
	for end < len(s) && s[end] >= '0' && s[end] <= '9' {
		end++
	}
	if end == 0 {
		return 0, false
	}
	n, err := strconv.Atoi(s[:end])
	if err != nil {
		return 0, false
	}
	if neg {
		n = -n
	}
	return n, true
}

// formatBytes：输入原始字节数字符串，输出人类可读串。
//   - ≥1e9 → "X.X GB"（toFixed(1)）；≥1e6 → "X MB"（四舍五入）；否则 "X KB"（四舍五入）
//   - 空 / 0 / 负 / 非数字前缀 → ""
func formatBytes(raw string) string {
	n, ok := parseIntJSLike(raw)
	if !ok || n <= 0 {
		return ""
	}
	f := float64(n)
	switch {
	case f >= 1e9:
		return strconv.FormatFloat(f/1e9, 'f', 1, 64) + " GB"
	case f >= 1e6:
		return strconv.Itoa(int(math.Round(f/1e6))) + " MB"
	default:
		return strconv.Itoa(int(math.Round(f/1e3))) + " KB"
	}
}

// formatKb：输入单位已是 KB 的数字串（animes.garden 的 size），阈值比 formatBytes 低 1000 倍。
func formatKb(raw string) string {
	n, ok := parseIntJSLike(raw)
	if !ok || n <= 0 {
		return ""
	}
	f := float64(n)
	switch {
	case f >= 1e6:
		return strconv.FormatFloat(f/1e6, 'f', 1, 64) + " GB"
	case f >= 1e3:
		return strconv.Itoa(int(math.Round(f/1e3))) + " MB"
	default:
		return strconv.Itoa(n) + " KB"
	}
}

// fansubBracketRE 匹配标题开头的字幕组括号：支持 ASCII [..] 与全角【..】，
// 且刻意允许两种括号错配（"[Foo】rest" 也解出 Foo）—— 与网站端 JS 正则逐字一致。
var fansubBracketRE = regexp.MustCompile(`^[\[【]([^\]】]+)[\]】]`)

// parseFansub 取标题开头的括号内容；无括号返回空串（映射层转成 nil）。
func parseFansub(title string) string {
	m := fansubBracketRE.FindStringSubmatch(title)
	if m == nil {
		return ""
	}
	return m[1]
}
