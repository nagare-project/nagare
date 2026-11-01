package main

import (
	"fmt"
	"math"
	"strings"
)

const luaHeader = `-- 由 tools/gentokens 从 design/tokens.json 生成，不要手改。
-- 改 token：编辑 design/tokens.json，再在仓库根目录运行 go generate ./...（CI 检查生成文件是最新的）。
-- 规则与用法见仓库根目录的 DESIGN.md。
--
-- 用法：local T = require 'tokens'（界面是目录脚本，mpv 把脚本目录放进了 package.path）。
-- 颜色项可以整个交给 draw.lua 的 color 选项，例如 c:rect(x1, y1, x2, y2, { color = T.color.accent })。
-- 尺寸是 CSS 像素，画的时候照常乘界面缩放倍数；时长是秒（mp.add_timeout 用秒）。
-- 字体按平台取（core.lua），ASS 的 \fn 只认一个字体名。
`

// renderLua 写播放器界面（mpv Lua）用的模块。
func renderLua(f *tokenFile) []byte {
	var b strings.Builder
	b.WriteString(luaHeader + "\nlocal M = {}\n")

	b.WriteString("\n-- 颜色：color 是 \"#RRGGBB\"，opacity 0..1\nM.color = {\n")
	for _, c := range filterColors(f.Colors, targetLua) {
		fmt.Fprintf(&b, "\t%s = { color = %q, opacity = %s }, -- %s\n",
			snakeCase(c.Name), strings.ToUpper(c.rgba.hex()), fmtNum(c.rgba.A), c.Use)
	}
	b.WriteString("}\n")

	b.WriteString("\n-- 字号阶梯：size 是像素，weight 是 100–900，tracking_em 是字距（em）\nM.type_scale = {\n")
	for _, t := range f.Type {
		if hasTarget(t.Targets, allTargets, targetLua) {
			fmt.Fprintf(&b, "\t%s = { size = %s, weight = %d, tracking_em = %s }, -- %s\n",
				snakeCase(t.Name), fmtNum(t.Size), t.Weight, fmtNum(t.Tracking), t.Use)
		}
	}
	b.WriteString("}\n")

	luaFonts(&b, f.Fonts)
	luaNums(&b, "space", "间距（像素）", 1, f.Spacing)
	luaNums(&b, "radius", "圆角（像素）", 1, f.Radii)
	luaNums(&b, "layout", "布局（像素）", 1, f.Layout)
	luaNums(&b, "duration", "时长（秒）", 0.001, f.Durations)

	b.WriteString("\nreturn M\n")
	return []byte(b.String())
}

// luaFonts 写播放器按平台用的字体：只有带 player 的字体项，键去掉 font- 前缀（font-cjk → cjk）。
func luaFonts(b *strings.Builder, fonts []fontToken) {
	var kept []fontToken
	for _, t := range fonts {
		if t.Player != nil {
			kept = append(kept, t)
		}
	}
	if len(kept) == 0 {
		return
	}
	b.WriteString("\n-- 字体：按 mpv 的 platform 属性取（darwin / windows，其余按 linux）\nM.font = {\n")
	for _, t := range kept {
		fmt.Fprintf(b, "\t%s = {", snakeCase(strings.TrimPrefix(t.Name, "font-")))
		for i, p := range playerPlatforms {
			if i > 0 {
				b.WriteString(",")
			}
			fmt.Fprintf(b, " %s = %q", p, t.Player[p])
		}
		fmt.Fprintf(b, " }, -- %s\n", t.Use)
	}
	b.WriteString("}\n")
}

func luaNums(b *strings.Builder, field, doc string, scale float64, ts []numToken) {
	var kept []numToken
	for _, t := range ts {
		if hasTarget(t.Targets, allTargets, targetLua) {
			kept = append(kept, t)
		}
	}
	if len(kept) == 0 {
		return
	}
	fmt.Fprintf(b, "\n-- %s\nM.%s = {\n", doc, field)
	for _, t := range kept {
		fmt.Fprintf(b, "\t%s = %s, -- %s\n", snakeCase(t.Name), fmtNum(roundTo(t.Value*scale, 6)), t.Use)
	}
	b.WriteString("}\n")
}

// roundTo 去掉毫秒换算成秒时的浮点尾巴（0.26 而不是 0.26000000000000001）。
func roundTo(v float64, digits int) float64 {
	p := math.Pow(10, float64(digits))
	return math.Round(v*p) / p
}
