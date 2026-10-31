package main

import (
	"fmt"
	"regexp"
	"strings"
)

// 对比度配对：tokens.json 的 contrast 里声明「哪个颜色会写在哪些底色上」，每一条都是要求，
// 生成器逐条实算，不满足就报错（go generate 失败，CI 红）。三类：
//
//   - text：要读的文字，WCAG AA 正文 ≥ 4.5:1
//   - ui：非文字的界面元素（进度条、选中态的色块），WCAG 1.4.11 ≥ 3:1
//   - restricted：只许用于占位符、禁用、装饰，或换了底色就该改用别的 token 的配对。
//     它们本来就不达标；哪天达标了，限制就过时了，生成器同样报错，提醒把它挪进 text、改 DESIGN.md。

const (
	minTextContrast = 4.5
	minUIContrast   = 3
)

type contrastSpec struct {
	Text       []contrastRule `json:"text"`
	UI         []contrastRule `json:"ui"`
	Restricted []contrastRule `json:"restricted"`
}

// contrastRule：前景色 fg 会出现在 on 里的每一种底色上。
type contrastRule struct {
	FG  string   `json:"fg"`
	On  []string `json:"on"`
	Use string   `json:"use"`
}

type contrastKind struct {
	key   string // tokens.json 里的字段名
	label string // 文档里的「要求」列
	rules []contrastRule
	ok    func(ratio float64) bool
	fix   string // 不满足时的提示
}

func contrastKinds(c contrastSpec) []contrastKind {
	return []contrastKind{
		{"text", "文字 ≥ 4.5:1", c.Text, func(r float64) bool { return r >= minTextContrast },
			"要读的文字要 ≥ 4.5:1：换一个颜色，或把这条挪进 restricted 并在 DESIGN.md 写明限制"},
		{"ui", "界面元素 ≥ 3:1", c.UI, func(r float64) bool { return r >= minUIContrast },
			"非文字的界面元素要 ≥ 3:1"},
		{"restricted", "不写要读的字", c.Restricted, func(r float64) bool { return r < minTextContrast },
			"已经达到 4.5:1，限制过时了：挪进 text，并改 DESIGN.md 里对应的规则"},
	}
}

// ratioInUseRE 找 use 里手写的对比度，如「白字 4.90:1」。
var ratioInUseRE = regexp.MustCompile(`(\d+\.\d{2}):1`)

func (v *validator) contrast(f *tokenFile) {
	measured := map[string]map[string]bool{} // 颜色 → 它参与的配对实算出的对比度（两位小数）
	note := func(name, ratio string) {
		if measured[name] == nil {
			measured[name] = map[string]bool{}
		}
		measured[name][ratio] = true
	}
	for _, k := range contrastKinds(f.Contrast) {
		for _, r := range k.rules {
			fg, okFG := v.colors[r.FG]
			if !okFG {
				v.fail("contrast.%s: 颜色 %q 不在 colors 里", k.key, r.FG)
			}
			if strings.TrimSpace(r.Use) == "" || strings.ContainsAny(r.Use, "\n\r|") {
				v.fail("contrast.%s.%s: use 不能为空，也不能换行或含 |", k.key, r.FG)
			}
			if len(r.On) == 0 {
				v.fail("contrast.%s.%s: on 至少要有一种底色", k.key, r.FG)
			}
			for _, name := range r.On {
				bg, okBG := v.colors[name]
				switch {
				case !okBG:
					v.fail("contrast.%s.%s: 底色 %q 不在 colors 里", k.key, r.FG, name)
					continue
				case bg.A != 1:
					v.fail("contrast.%s.%s: 底色 %q 是半透明的，算不出对比度", k.key, r.FG, name)
					continue
				case !okFG:
					continue
				}
				ratio := contrastRatio(fg, bg)
				if !k.ok(ratio) {
					v.fail("contrast.%s: %s 在 %s 上是 %.2f:1 —— %s", k.key, r.FG, name, ratio, k.fix)
				}
				s := fmt.Sprintf("%.2f", ratio)
				note(r.FG, s)
				note(name, s)
			}
		}
	}
	// 写进 use 的对比度（给读注释的人看的）必须与实算一致，改了颜色忘了改说明就报错
	for _, c := range f.Colors {
		for _, m := range ratioInUseRE.FindAllStringSubmatch(c.Use, -1) {
			if !measured[c.Name][m[1]] {
				v.fail("colors.%s: use 里写着 %s:1，但 contrast 里没有哪条配对实算出这个值", c.Name, m[1])
			}
		}
	}
}

// mdContrast：每条配对一行，列出它在每种底色上的实测值。
func mdContrast(f *tokenFile) string {
	colors := map[string]rgba{}
	for _, c := range f.Colors {
		colors[c.Name] = c.rgba
	}
	var rows [][]string
	for _, k := range contrastKinds(f.Contrast) {
		for _, r := range k.rules {
			cells := make([]string, len(r.On))
			for i, bg := range r.On {
				ratio := contrastRatio(colors[r.FG], colors[bg])
				mark := "✓"
				if !k.ok(ratio) {
					mark = "✗"
				}
				if k.key == "restricted" {
					mark = "" // 本来就不达标，不打勾也不打叉
				}
				cells[i] = strings.TrimSpace(fmt.Sprintf("%s %.2f:1 %s", code(bg), ratio, mark))
			}
			rows = append(rows, []string{code(r.FG), k.label, strings.Join(cells, "<br>"), r.Use})
		}
	}
	return table([]string{"前景", "要求", "实测（底色 对比度）", "场景"}, rows)
}
