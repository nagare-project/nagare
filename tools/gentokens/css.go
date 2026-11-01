package main

import (
	"fmt"
	"math"
	"strings"
)

const cssHeader = `/* 由 tools/gentokens 从 design/tokens.json 生成，不要手改。
   改 token：编辑 design/tokens.json，再在仓库根目录运行 go generate ./...（CI 检查生成文件是最新的）。
   规则与用法见仓库根目录的 DESIGN.md。

   本文件还没有接入页面：现有界面仍用 global.css 里的旧 token，界面按 DESIGN.md 重写时再引入，
   届时删掉旧 token。在那之前不要在组件里引用这里的变量（--bg 等与 global.css 同名、取值不同）。 */
`

// renderCSS 把 token 写成 :root 上的 CSS 自定义属性。
func renderCSS(f *tokenFile) []byte {
	w := &cssWriter{}
	w.raw(cssHeader + "\n:root {\n")

	w.section("颜色")
	for _, c := range f.Colors {
		if hasTarget(c.Targets, allTargets, targetCSS) {
			w.prop(c.Name, c.Value, c.Use)
		}
	}

	w.section("海报色阶：没有色相时的中性值。有色相时由服务端算好 sRGB，直接写在使用它的元素上")
	for _, t := range f.PosterTones {
		w.prop(t.Name, neutralTone(t.L).hex(), fmt.Sprintf("%s；%s", oklchLabel(t), t.Use))
	}

	w.section("字体")
	for _, t := range f.Fonts {
		w.prop(t.Name, fontStack(t.Families), t.Use)
	}

	w.section("字号阶梯")
	for _, t := range f.Type {
		if !hasTarget(t.Targets, allTargets, targetCSS) {
			continue
		}
		p := "type-" + t.Name
		w.prop(p+"-size", fmtNum(t.Size)+"px", t.Use)
		w.prop(p+"-weight", fmt.Sprint(t.Weight), "")
		w.prop(p+"-tracking", em(t.Tracking), "")
		if t.Family != "" {
			w.prop(p+"-family", "var(--"+t.Family+")", "")
		}
	}

	w.section("间距")
	w.nums("sp-", "px", f.Spacing)
	w.section("圆角")
	w.nums("radius-", "px", f.Radii)

	w.section("动效")
	w.nums("dur-", "ms", f.Durations)
	for _, e := range f.Easings {
		w.prop("ease-"+e.Name, cubicBezier(e.Value), e.Use)
	}

	w.section("阴影")
	for _, s := range f.Shadows {
		w.prop("shadow-"+s.Name, s.Value, s.Use)
	}

	w.section("布局（断点用不了自定义属性，只写在 DESIGN.md 里）")
	w.nums("", "px", f.Layout)

	w.raw("}\n")
	return []byte(w.b.String())
}

type cssWriter struct {
	b       strings.Builder
	started bool
}

func (w *cssWriter) raw(s string) { w.b.WriteString(s) }

func (w *cssWriter) section(title string) {
	if w.started {
		w.b.WriteString("\n")
	}
	w.started = true
	fmt.Fprintf(&w.b, "  /* ---- %s ---- */\n", title)
}

func (w *cssWriter) prop(name, value, use string) {
	if use == "" {
		fmt.Fprintf(&w.b, "  --%s: %s;\n", name, value)
		return
	}
	fmt.Fprintf(&w.b, "  --%s: %s; /* %s */\n", name, value, use)
}

func (w *cssWriter) nums(prefix, unit string, ts []numToken) {
	for _, t := range ts {
		if hasTarget(t.Targets, allTargets, targetCSS) {
			w.prop(prefix+t.Name, fmtNum(t.Value)+unit, t.Use)
		}
	}
}

// fontStack：带空格的字体名加引号，通用族名与 var() 原样写。
func fontStack(families []string) string {
	out := make([]string, len(families))
	for i, f := range families {
		if strings.Contains(f, " ") && !strings.HasPrefix(f, "var(") {
			out[i] = `"` + f + `"`
		} else {
			out[i] = f
		}
	}
	return strings.Join(out, ", ")
}

// oklchLabel：oklch(76% 0.085 h)，h 是运行时的色相角。乘 100 先取整，免得写出 68.00000000000001。
func oklchLabel(t posterTone) string {
	return fmt.Sprintf("oklch(%s%% %s h)", fmtNum(math.Round(t.L*10000)/100), fmtNum(t.C))
}

func em(v float64) string {
	if v == 0 {
		return "0"
	}
	return fmtNum(v) + "em"
}

func cubicBezier(v []float64) string {
	return fmt.Sprintf("cubic-bezier(%s, %s, %s, %s)", fmtNum(v[0]), fmtNum(v[1]), fmtNum(v[2]), fmtNum(v[3]))
}
