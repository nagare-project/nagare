package main

import (
	"fmt"
	"strings"
)

const swiftHeader = `// 由 tools/gentokens 从 design/tokens.json 生成，不要手改。
// 改 token：编辑 design/tokens.json，再在仓库根目录运行 go generate ./...（CI 检查生成文件是最新的）。
// 规则与用法见仓库根目录的 DESIGN.md。
//
// 颜色：有系统语义色对应的引用系统色（「增强对比度」由系统接管），没有的写固定 sRGB 值。
// App 固定暗色（.preferredColorScheme(.dark)），生成器只在系统色的深色值与 token 一致时才映射。
// 字号：引用系统文字样式（跟随动态字号），只有字标用打包的 Sora、固定字号。

import SwiftUI
import UIKit
`

// renderSwift 写 iOS 用的常量：UIColor / Color、文字样式、间距、圆角、时长、缓动。
func renderSwift(f *tokenFile) []byte {
	w := &swiftWriter{}
	w.b.WriteString(swiftHeader)

	colors := filterColors(f.Colors, targetSwift)
	// 计算属性而不是存储属性：UIColor 在旧 SDK 上不是 Sendable，全局存储的 UIColor 会被
	// Swift 6 的严格并发检查拦下；每次取一个新实例很便宜。
	w.open("NagareUIColor", "颜色（UIKit）")
	for _, c := range colors {
		w.doc(fmt.Sprintf("%s · %s", c.Use, c.Value))
		if c.IOS != "" {
			w.line("static var %s: UIColor { .%s }", camelCase(c.Name), c.IOS)
		} else {
			w.line("static var %s: UIColor { %s }", camelCase(c.Name), uiColorLiteral(c.rgba))
		}
	}
	w.close()

	w.open("NagareColor", "颜色（SwiftUI），与 NagareUIColor 一一对应")
	for _, c := range colors {
		w.line("static let %s = Color(NagareUIColor.%s)", camelCase(c.Name), camelCase(c.Name))
	}
	w.close()

	w.open("NagarePosterTone", "海报色阶没有色相时的中性值。有色相时用服务端按 OKLCH 算好的 sRGB 值")
	for _, t := range f.PosterTones {
		w.doc(fmt.Sprintf("%s；%s", oklchLabel(t), t.Use))
		w.line("static let %s = Color(%s)", camelCase(t.Name), uiColorLiteral(neutralTone(t.L)))
	}
	w.close()

	w.b.WriteString(`
/// 一档文字样式。font 跟随动态字号；webSize 是网页上的像素字号（对照用）；
/// trackingEm 是字距（em），SwiftUI 的 .tracking() 要的是 pt：trackingEm × 实际字号。
struct NagareTextStyle {
    let font: Font
    let webSize: CGFloat
    let trackingEm: CGFloat
}
`)
	w.open("NagareType", "字号阶梯")
	for _, t := range f.Type {
		if !hasTarget(t.Targets, allTargets, targetSwift) {
			continue
		}
		w.doc(fmt.Sprintf("%s · %spx / %d", t.Use, fmtNum(t.Size), t.Weight))
		w.line("static let %s = NagareTextStyle(font: %s, webSize: %s, trackingEm: %s)",
			camelCase(t.Name), swiftFont(t), fmtNum(t.Size), fmtNum(t.Tracking))
	}
	w.close()

	w.nums("NagareSpacing", "间距（pt）", "CGFloat", 1, f.Spacing)
	w.nums("NagareRadius", "圆角（pt）", "CGFloat", 1, f.Radii)
	w.nums("NagareLayout", "布局（pt）", "CGFloat", 1, f.Layout)
	w.nums("NagareDuration", "时长（秒）", "TimeInterval", 0.001, f.Durations)

	w.open("NagareEasing", "缓动：cubic-bezier 的四个控制点，用法 Animation.timingCurve(e.0, e.1, e.2, e.3, duration:)")
	for _, e := range f.Easings {
		w.doc(e.Use)
		w.line("static let %s: (Double, Double, Double, Double) = (%s, %s, %s, %s)",
			camelCase(e.Name), fmtNum(e.Value[0]), fmtNum(e.Value[1]), fmtNum(e.Value[2]), fmtNum(e.Value[3]))
	}
	w.close()
	return []byte(w.b.String())
}

type swiftWriter struct{ b strings.Builder }

func (w *swiftWriter) open(name, doc string) {
	fmt.Fprintf(&w.b, "\n/// %s\nenum %s {\n", doc, name)
}

func (w *swiftWriter) close() { w.b.WriteString("}\n") }

func (w *swiftWriter) doc(s string) { fmt.Fprintf(&w.b, "    /// %s\n", s) }

func (w *swiftWriter) line(format string, args ...any) {
	fmt.Fprintf(&w.b, "    "+format+"\n", args...)
}

// nums 写一组数值常量；scale 用来把毫秒换成秒。整组都不输出到 Swift 时不写空 enum。
func (w *swiftWriter) nums(name, doc, typ string, scale float64, ts []numToken) {
	var kept []numToken
	for _, t := range ts {
		if hasTarget(t.Targets, allTargets, targetSwift) {
			kept = append(kept, t)
		}
	}
	if len(kept) == 0 {
		return
	}
	w.open(name, doc)
	for _, t := range kept {
		w.doc(t.Use)
		w.line("static let %s: %s = %s", camelCase(t.Name), typ, fmtNum(roundTo(t.Value*scale, 6)))
	}
	w.close()
}

func filterColors(cs []colorToken, target string) []colorToken {
	var out []colorToken
	for _, c := range cs {
		if hasTarget(c.Targets, allTargets, target) {
			out = append(out, c)
		}
	}
	return out
}

func uiColorLiteral(c rgba) string {
	return fmt.Sprintf("UIColor(red: %d.0 / 255, green: %d.0 / 255, blue: %d.0 / 255, alpha: %s)",
		c.R, c.G, c.B, fmtNum(c.A))
}

func swiftFont(t typeStyle) string {
	weight := swiftWeights[t.Weight]
	if t.IOSFont != "" {
		return fmt.Sprintf("Font.custom(%q, fixedSize: %s).weight(.%s)", t.IOSFont, fmtNum(t.Size), weight)
	}
	return fmt.Sprintf("Font.system(.%s).weight(.%s)", t.IOSTextStyle, weight)
}
