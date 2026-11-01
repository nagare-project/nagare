package main

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"
)

// DESIGN.md 里由生成器维护的表格夹在一对标记之间：
//
//	<!-- gentokens:begin colors -->
//	（生成的表格）
//	<!-- gentokens:end colors -->
//
// 标记之外的文字是手写的，生成器不碰。每个块必须恰好出现一次。

var blockRE = regexp.MustCompile(`<!-- gentokens:(begin|end) ([a-z-]+) -->`)

// markdownBlocks 按块名给出表格内容。
func markdownBlocks(f *tokenFile) map[string]string {
	return map[string]string{
		"colors":       mdColors(f),
		"poster-tones": mdPosterTones(f),
		"contrast":     mdContrast(f),
		"fonts":        mdFonts(f),
		"type":         mdType(f),
		"spacing":      mdNums("sp-", "px", f.Spacing),
		"radii":        mdNums("radius-", "px", f.Radii),
		"motion":       mdMotion(f),
		"elevation":    mdShadows(f),
		"layout":       mdNums("", "px", f.Layout),
	}
}

// renderDesign 把 design 里每个块替换成新生成的表格。
func renderDesign(design []byte, blocks map[string]string) ([]byte, error) {
	matches := blockRE.FindAllSubmatchIndex(design, -1)
	seen := map[string]bool{}
	var out bytes.Buffer
	last := 0
	for i := 0; i < len(matches); i++ {
		m := matches[i]
		kind, name := string(design[m[2]:m[3]]), string(design[m[4]:m[5]])
		if kind != "begin" {
			return nil, fmt.Errorf("DESIGN.md: 块 %s 的 end 标记前没有 begin", name)
		}
		body, ok := blocks[name]
		if !ok {
			return nil, fmt.Errorf("DESIGN.md: 不认识的生成块 %q", name)
		}
		if seen[name] {
			return nil, fmt.Errorf("DESIGN.md: 生成块 %q 出现了不止一次", name)
		}
		seen[name] = true
		if i+1 >= len(matches) {
			return nil, fmt.Errorf("DESIGN.md: 块 %s 没有 end 标记", name)
		}
		e := matches[i+1]
		if string(design[e[2]:e[3]]) != "end" || string(design[e[4]:e[5]]) != name {
			return nil, fmt.Errorf("DESIGN.md: 块 %s 的 begin 后面要紧跟它自己的 end", name)
		}
		out.Write(design[last:m[1]])
		out.WriteString("\n\n" + body + "\n")
		last = e[0]
		i++
	}
	out.Write(design[last:])
	for name := range blocks {
		if !seen[name] {
			return nil, fmt.Errorf("DESIGN.md: 缺少生成块 %q（<!-- gentokens:begin %s --> … <!-- gentokens:end %s -->）", name, name, name)
		}
	}
	return out.Bytes(), nil
}

// table 写一张 Markdown 表格。
func table(header []string, rows [][]string) string {
	var b strings.Builder
	b.WriteString("| " + strings.Join(header, " | ") + " |\n|")
	for range header {
		b.WriteString("---|")
	}
	b.WriteString("\n")
	for _, r := range rows {
		b.WriteString("| " + strings.Join(r, " | ") + " |\n")
	}
	return b.String()
}

func code(s string) string { return "`" + s + "`" }

func mdColors(f *tokenFile) string {
	var rows [][]string
	for _, c := range f.Colors {
		ios := "固定值"
		switch {
		case !hasTarget(c.Targets, allTargets, targetSwift):
			ios = "不输出"
		case c.IOS != "":
			ios = code(c.IOS)
		}
		rows = append(rows, []string{code("--" + c.Name), code(c.Value), ios, c.Use})
	}
	return table([]string{"Token", "值", "iOS", "用途"}, rows)
}

func mdPosterTones(f *tokenFile) string {
	var rows [][]string
	for _, t := range f.PosterTones {
		text := "否"
		if t.Text {
			text = "能"
		}
		rows = append(rows, []string{code("--" + t.Name), code(oklchLabel(t)), code(neutralTone(t.L).hex()), text, t.Use})
	}
	return table([]string{"Token", "OKLCH（h = 封面色相）", "没有色相时", "写字", "用途"}, rows)
}

func mdFonts(f *tokenFile) string {
	var rows [][]string
	for _, t := range f.Fonts {
		rows = append(rows, []string{code("--" + t.Name), code(fontStack(t.Families)), t.Use})
	}
	return table([]string{"Token", "字体栈", "用途"}, rows)
}

func mdType(f *tokenFile) string {
	var rows [][]string
	for _, t := range f.Type {
		ios := "—"
		switch {
		case t.IOSTextStyle != "":
			ios = code("." + t.IOSTextStyle)
		case t.IOSFont != "":
			ios = t.IOSFont + "（固定字号）"
		}
		family := "主字体"
		if t.Family != "" {
			family = code("--" + t.Family)
		}
		rows = append(rows, []string{code(t.Name), fmtNum(t.Size) + "px", fmt.Sprint(t.Weight), em(t.Tracking), family, ios, t.Use})
	}
	return table([]string{"样式", "字号", "字重", "字距", "字体", "iOS", "用途"}, rows)
}

func mdNums(prefix, unit string, ts []numToken) string {
	var rows [][]string
	for _, t := range ts {
		rows = append(rows, []string{code("--" + prefix + t.Name), fmtNum(t.Value) + unit, targetsLabel(t.Targets), t.Use})
	}
	return table([]string{"Token", "值", "输出到", "用途"}, rows)
}

func mdMotion(f *tokenFile) string {
	out := mdNums("dur-", "ms", f.Durations)
	var rows [][]string
	for _, e := range f.Easings {
		rows = append(rows, []string{code("--ease-" + e.Name), code(cubicBezier(e.Value)), "css / swift", e.Use})
	}
	return out + "\n" + table([]string{"Token", "值", "输出到", "用途"}, rows)
}

func mdShadows(f *tokenFile) string {
	var rows [][]string
	for _, s := range f.Shadows {
		rows = append(rows, []string{code("--shadow-" + s.Name), code(s.Value), s.Use})
	}
	return table([]string{"Token", "值", "用途"}, rows)
}

func targetsLabel(ts []string) string {
	if ts == nil {
		return "全部"
	}
	if len(ts) == 0 {
		return "只在文档"
	}
	return strings.Join(ts, " / ")
}
