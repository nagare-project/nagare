package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// tokens.json 的结构。用数组而不是对象存每一组：数组有顺序，生成物才是确定的；
// 解码时拒绝未知字段，字段名写错不会被悄悄忽略。

type tokenFile struct {
	Comment     string       `json:"$comment"`
	Version     int          `json:"version"`
	Colors      []colorToken `json:"colors"`
	PosterTones []posterTone `json:"posterTones"`
	Fonts       []fontToken  `json:"fonts"`
	Type        []typeStyle  `json:"type"`
	Spacing     []numToken   `json:"spacing"`
	Radii       []numToken   `json:"radii"`
	Durations   []numToken   `json:"durations"`
	Easings     []easing     `json:"easings"`
	Shadows     []shadow     `json:"shadows"`
	Layout      []numToken   `json:"layout"`
	Contrast    contrastSpec `json:"contrast"`
}

type colorToken struct {
	Name    string   `json:"name"`
	Value   string   `json:"value"`
	IOS     string   `json:"ios"`
	Use     string   `json:"use"`
	Targets []string `json:"targets"`

	rgba rgba // 解码后填
}

// posterTone 是海报色阶的一档：OKLCH 的明度与彩度固定，色相运行时取自封面。
type posterTone struct {
	Name string  `json:"name"`
	L    float64 `json:"l"`
	C    float64 `json:"c"`
	Text bool    `json:"text"`
	Use  string  `json:"use"`
}

type fontToken struct {
	Name     string   `json:"name"`
	Families []string `json:"families"`
	Use      string   `json:"use"`
}

type typeStyle struct {
	Name         string   `json:"name"`
	Size         float64  `json:"size"`
	Weight       int      `json:"weight"`
	Tracking     float64  `json:"tracking"` // em
	Family       string   `json:"family"`   // 引用 fonts 里的一项；空 = 全局主字体
	IOSTextStyle string   `json:"iosTextStyle"`
	IOSFont      string   `json:"iosFont"` // 打包的自定义字体，固定字号
	Use          string   `json:"use"`
	Targets      []string `json:"targets"`
}

// numToken 是一个带单位的数：间距、圆角、布局是像素（iOS 是 pt），时长是毫秒。
type numToken struct {
	Name    string   `json:"name"`
	Value   float64  `json:"value"`
	Use     string   `json:"use"`
	Targets []string `json:"targets"`
}

// easing 只输出到 CSS 与 Swift：播放器界面的动画是逐帧手算的，用不上贝塞尔曲线。
type easing struct {
	Name  string    `json:"name"`
	Value []float64 `json:"value"` // cubic-bezier 的四个控制点
	Use   string    `json:"use"`
}

type shadow struct {
	Name  string `json:"name"`
	Value string `json:"value"`
	Use   string `json:"use"`
}

type contrastSpec struct {
	Foregrounds []string       `json:"foregrounds"`
	Backgrounds []string       `json:"backgrounds"`
	Pairs       []contrastPair `json:"pairs"`
}

type contrastPair struct {
	FG  string `json:"fg"`
	BG  string `json:"bg"`
	Use string `json:"use"`
}

// 输出目标
const (
	targetCSS   = "css"
	targetSwift = "swift"
	targetLua   = "lua"
)

// 带 targets 字段的组（颜色、字号、间距、圆角、时长、布局）省略时三端都输出。
// 不带的组输出范围固定：字体栈与阴影只进 CSS（iOS 走系统字体与系统做法，播放器的 ASS 绘图画不了阴影），
// 海报色阶与缓动进 CSS 与 Swift。
var allTargets = []string{targetCSS, targetSwift, targetLua}

var (
	nameRE     = regexp.MustCompile(`^[a-z][a-z0-9]*(-[a-z0-9]+)*$`)
	fontNameRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9 -]*$`)
)

func decodeTokens(data []byte) (*tokenFile, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var f tokenFile
	if err := dec.Decode(&f); err != nil {
		return nil, fmt.Errorf("解析 tokens.json: %w", err)
	}
	if dec.More() {
		return nil, errors.New("解析 tokens.json: 顶层对象之后还有内容")
	}
	if err := f.validate(); err != nil {
		return nil, err
	}
	return &f, nil
}

func (f *tokenFile) validate() error {
	if f.Version != 1 {
		return fmt.Errorf("tokens.json: 不认识的 version %d（只认 1）", f.Version)
	}
	v := validator{seen: map[string]string{}}
	for i := range f.Colors {
		v.color(&f.Colors[i])
	}
	for _, t := range f.PosterTones {
		v.posterTone(t)
	}
	fonts := map[string]bool{}
	for _, t := range f.Fonts {
		v.font(t)
		fonts[t.Name] = true
	}
	for _, t := range f.Type {
		v.typeStyle(t, fonts)
	}
	v.nums("spacing", "sp-", f.Spacing)
	v.nums("radii", "radius-", f.Radii)
	v.nums("durations", "dur-", f.Durations)
	v.nums("layout", "", f.Layout)
	for _, e := range f.Easings {
		v.easing(e)
	}
	for _, s := range f.Shadows {
		v.common("shadows", s.Name, s.Use, "shadow-"+s.Name)
		// 原样写进 CSS 声明：分号、花括号、注释结束符会把样式表截断
		if strings.TrimSpace(s.Value) == "" || strings.ContainsAny(s.Value, ";{}\n") || strings.Contains(s.Value, "*/") {
			v.fail("shadows.%s: value 不能为空，也不能含 ; { } */ 或换行", s.Name)
		}
	}
	v.contrast(f)
	return errors.Join(v.errs...)
}

// validator 收集全部错误再一次报出来，改 tokens.json 时不用一条一条地试。
type validator struct {
	errs   []error
	seen   map[string]string // CSS 变量名 → 来自哪一组，防止两组生成同名变量
	colors map[string]rgba
}

func (v *validator) fail(format string, args ...any) {
	v.errs = append(v.errs, fmt.Errorf("tokens.json: "+format, args...))
}

// common 检查每个 token 都有的东西：名字形状、用途说明、CSS 变量名不撞车、
// 转成 Swift / Lua 标识符后不是关键字。
func (v *validator) common(group, name, use, cssName string) {
	if !nameRE.MatchString(name) {
		v.fail("%s: 名字 %q 要写成小写短横线形式（如 bg-card）", group, name)
		return
	}
	if strings.TrimSpace(use) == "" {
		v.fail("%s.%s: use（用途）不能为空", group, name)
	}
	// 用途会原样写进三种语言的注释与文档表格
	if strings.ContainsAny(use, "\n\r|") || strings.Contains(use, "*/") {
		v.fail("%s.%s: use 不能换行，也不能含 | 或 */", group, name)
	}
	if prev, ok := v.seen[cssName]; ok {
		v.fail("%s.%s: CSS 变量 --%s 与 %s 重名", group, name, cssName, prev)
	}
	v.seen[cssName] = group + "." + name
	if swiftKeywords[camelCase(name)] {
		v.fail("%s.%s: 转成 Swift 标识符 %q 是关键字", group, name, camelCase(name))
	}
	if luaKeywords[snakeCase(name)] {
		v.fail("%s.%s: 转成 Lua 标识符 %q 是关键字", group, name, snakeCase(name))
	}
}

func (v *validator) targets(group, name string, ts []string) {
	for _, t := range ts {
		if t != targetCSS && t != targetSwift && t != targetLua {
			v.fail("%s.%s: 不认识的输出目标 %q（只有 css / swift / lua）", group, name, t)
		}
	}
}

func (v *validator) color(c *colorToken) {
	v.common("colors", c.Name, c.Use, c.Name)
	v.targets("colors", c.Name, c.Targets)
	parsed, err := parseColor(c.Value)
	if err != nil {
		v.fail("colors.%s: %v", c.Name, err)
		return
	}
	if canon := parsed.css(); canon != c.Value {
		v.fail("colors.%s: 请写成规范形式 %q（文档与生成物照原样显示这个字符串）", c.Name, canon)
	}
	c.rgba = parsed
	if v.colors == nil {
		v.colors = map[string]rgba{}
	}
	v.colors[c.Name] = parsed
	if c.IOS == "" {
		return
	}
	sys, ok := iosDarkColors[c.IOS]
	if !ok {
		v.fail("colors.%s: 不认识的 iOS 语义色 %q（可用：%s）", c.Name, c.IOS, iosColorNames())
		return
	}
	if !sys.close(parsed) {
		v.fail("colors.%s: iOS %s 在深色外观下是 %s，与 %s 不一致；不一致就不要映射，留空写固定值",
			c.Name, c.IOS, sys.css(), c.Value)
	}
}

func (v *validator) posterTone(t posterTone) {
	v.common("posterTones", t.Name, t.Use, t.Name)
	if t.L <= 0 || t.L >= 1 {
		v.fail("posterTones.%s: l 要在 0 与 1 之间（OKLCH 明度）", t.Name)
	}
	if t.C < 0 || t.C > 0.4 {
		v.fail("posterTones.%s: c 要在 0 与 0.4 之间（OKLCH 彩度）", t.Name)
	}
}

func (v *validator) font(t fontToken) {
	v.common("fonts", t.Name, t.Use, t.Name)
	if len(t.Families) == 0 {
		v.fail("fonts.%s: families 不能为空", t.Name)
	}
	for _, fam := range t.Families {
		if strings.TrimSpace(fam) == "" || strings.ContainsAny(fam, `"';,`) {
			v.fail("fonts.%s: 字体名 %q 不能为空，也不能带引号、逗号或分号", t.Name, fam)
		}
	}
}

func (v *validator) typeStyle(t typeStyle, fonts map[string]bool) {
	v.common("type", t.Name, t.Use, "type-"+t.Name)
	v.targets("type", t.Name, t.Targets)
	if t.Size <= 0 {
		v.fail("type.%s: size 要大于 0", t.Name)
	}
	if t.Weight < 100 || t.Weight > 900 || t.Weight%100 != 0 {
		v.fail("type.%s: weight 要是 100–900 之间的整百", t.Name)
	}
	if t.Family != "" && !fonts[t.Family] {
		v.fail("type.%s: family %q 不在 fonts 里", t.Name, t.Family)
	}
	if !hasTarget(t.Targets, allTargets, targetSwift) {
		return
	}
	switch {
	case t.IOSTextStyle != "" && t.IOSFont != "":
		v.fail("type.%s: iosTextStyle 与 iosFont 只能填一个", t.Name)
	case t.IOSTextStyle == "" && t.IOSFont == "":
		v.fail("type.%s: 要输出到 Swift 就得填 iosTextStyle（系统动态字号）或 iosFont（打包字体）", t.Name)
	case t.IOSTextStyle != "" && !iosTextStyles[t.IOSTextStyle]:
		v.fail("type.%s: 不认识的 iOS 文字样式 %q", t.Name, t.IOSTextStyle)
	case t.IOSFont != "" && !fontNameRE.MatchString(t.IOSFont):
		v.fail("type.%s: iosFont %q 只能含字母、数字、空格与短横线", t.Name, t.IOSFont)
	}
}

func (v *validator) nums(group, prefix string, ts []numToken) {
	for _, t := range ts {
		v.common(group, t.Name, t.Use, prefix+t.Name)
		v.targets(group, t.Name, t.Targets)
		if t.Value < 0 {
			v.fail("%s.%s: value 不能是负数", group, t.Name)
		}
	}
}

func (v *validator) easing(e easing) {
	v.common("easings", e.Name, e.Use, "ease-"+e.Name)
	if len(e.Value) != 4 {
		v.fail("easings.%s: value 要是 cubic-bezier 的四个数", e.Name)
		return
	}
	if e.Value[0] < 0 || e.Value[0] > 1 || e.Value[2] < 0 || e.Value[2] > 1 {
		v.fail("easings.%s: 两个控制点的 x 要在 0 与 1 之间", e.Name)
	}
}

func (v *validator) contrast(f *tokenFile) {
	check := func(where, name string) {
		if _, ok := v.colors[name]; !ok {
			v.fail("contrast.%s: 颜色 %q 不在 colors 里", where, name)
		}
	}
	for _, n := range f.Contrast.Foregrounds {
		check("foregrounds", n)
	}
	for _, n := range f.Contrast.Backgrounds {
		check("backgrounds", n)
		if c, ok := v.colors[n]; ok && c.A != 1 {
			v.fail("contrast.backgrounds: %q 是半透明的，算不出对比度", n)
		}
	}
	for _, p := range f.Contrast.Pairs {
		check("pairs", p.FG)
		check("pairs", p.BG)
		if c, ok := v.colors[p.BG]; ok && c.A != 1 {
			v.fail("contrast.pairs: 底色 %q 是半透明的，算不出对比度", p.BG)
		}
	}
}

// hasTarget 报告 token 会不会输出到 want。ts 为 nil 时用该组默认；空数组表示只进文档。
func hasTarget(ts, groupDefault []string, want string) bool {
	if ts == nil {
		ts = groupDefault
	}
	for _, t := range ts {
		if t == want {
			return true
		}
	}
	return false
}

// camelCase：bg-card → bgCard（Swift 标识符）。
func camelCase(name string) string {
	parts := strings.Split(name, "-")
	for i := 1; i < len(parts); i++ {
		if parts[i] != "" {
			parts[i] = strings.ToUpper(parts[i][:1]) + parts[i][1:]
		}
	}
	return strings.Join(parts, "")
}

// snakeCase：bg-card → bg_card（Lua 标识符）。
func snakeCase(name string) string {
	return strings.ReplaceAll(name, "-", "_")
}

var swiftKeywords = setOf("as", "break", "case", "catch", "class", "continue", "default", "defer", "do",
	"else", "enum", "extension", "fallthrough", "false", "for", "func", "guard", "if", "import", "in",
	"init", "inout", "internal", "is", "let", "nil", "operator", "private", "protocol", "public",
	"repeat", "return", "self", "static", "struct", "subscript", "super", "switch", "throw", "throws",
	"true", "try", "typealias", "var", "where", "while")

var luaKeywords = setOf("and", "break", "do", "else", "elseif", "end", "false", "for", "function",
	"goto", "if", "in", "local", "nil", "not", "or", "repeat", "return", "then", "true", "until", "while")

// iosTextStyles 是 SwiftUI Font.TextStyle 的全部取值：用它们才跟随系统的动态字号。
var iosTextStyles = setOf("largeTitle", "title", "title2", "title3", "headline", "subheadline",
	"body", "callout", "footnote", "caption", "caption2")

var swiftWeights = map[int]string{
	100: "ultraLight", 200: "thin", 300: "light", 400: "regular", 500: "medium",
	600: "semibold", 700: "bold", 800: "heavy", 900: "black",
}

func setOf(items ...string) map[string]bool {
	m := make(map[string]bool, len(items))
	for _, it := range items {
		m[it] = true
	}
	return m
}
