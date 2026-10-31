package main

import (
	"encoding/json"
	"flag"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 改了生成器的输出格式后：go test ./tools/gentokens -run TestGolden -update
var update = flag.Bool("update", false, "重写 testdata/golden 里的黄金文件")

const repoRoot = "../.."

// 黄金测试：小样本 tokens.json + 样本文档 → 四份生成物逐字节对得上。
func TestGolden(t *testing.T) {
	tokens := readFile(t, "testdata/tokens.json")
	design := readFile(t, "testdata/design.md")

	outs, err := render(tokens, design)
	require.NoError(t, err)

	for _, rel := range outputPaths {
		golden := filepath.Join("testdata", "golden", path.Base(rel))
		if *update {
			require.NoError(t, os.WriteFile(golden, outs[rel], 0o644))
			continue
		}
		assert.Equal(t, string(readFile(t, golden)), string(outs[rel]), "%s 与黄金文件不一致（有意改动就加 -update 重写）", golden)
	}
}

// 生成是幂等的：对已经生成过的文档再跑一遍，结果不变。
func TestRenderIsIdempotent(t *testing.T) {
	tokens := readFile(t, "testdata/tokens.json")
	first, err := render(tokens, readFile(t, "testdata/design.md"))
	require.NoError(t, err)
	second, err := render(tokens, first[designPath])
	require.NoError(t, err)
	assert.Equal(t, string(first[designPath]), string(second[designPath]))
}

// 仓库里的生成物与 design/tokens.json 一致。CI 另有一步 go generate + git diff，
// 这条让本地 go test 也能发现「改了 tokens.json 忘了生成」。
func TestRepoOutputsUpToDate(t *testing.T) {
	outs, err := render(readFile(t, filepath.Join(repoRoot, tokensPath)), readFile(t, filepath.Join(repoRoot, designPath)))
	require.NoError(t, err)
	for _, rel := range outputPaths {
		onDisk := readFile(t, filepath.Join(repoRoot, filepath.FromSlash(rel)))
		assert.Equal(t, string(outs[rel]), string(onDisk), "%s 过期了：在仓库根目录运行 go generate ./...", rel)
	}
}

// tokens.json 里声明的每一条对比度配对都要达标：要读的文字 ≥ 4.5:1，非文字界面元素 ≥ 3:1；
// restricted（占位符 / 禁用 / 换底色就该换 token）本来就不达标，达标了说明限制过时。
// 生成器的校验也会拦，这里不经过校验、直接从原始 JSON 再算一遍，校验被改坏时这条仍然会红。
func TestDeclaredContrastPairs(t *testing.T) {
	var f tokenFile
	require.NoError(t, json.Unmarshal(readFile(t, filepath.Join(repoRoot, tokensPath)), &f))
	colors := map[string]rgba{}
	for _, c := range f.Colors {
		parsed, err := parseColor(c.Value)
		require.NoError(t, err, c.Name)
		colors[c.Name] = parsed
	}
	ratio := func(fg, bg string) float64 {
		require.Contains(t, colors, fg)
		require.Contains(t, colors, bg)
		return contrastRatio(colors[fg], colors[bg])
	}
	declared := map[string]bool{} // "kind fg bg"
	for _, r := range f.Contrast.Text {
		for _, bg := range r.On {
			declared["text "+r.FG+" "+bg] = true
			assert.GreaterOrEqual(t, ratio(r.FG, bg), 4.5, "文字 %s 在 %s 上", r.FG, bg)
		}
	}
	for _, r := range f.Contrast.UI {
		for _, bg := range r.On {
			declared["ui "+r.FG+" "+bg] = true
			assert.GreaterOrEqual(t, ratio(r.FG, bg), 3.0, "界面元素 %s 在 %s 上", r.FG, bg)
		}
	}
	for _, r := range f.Contrast.Restricted {
		for _, bg := range r.On {
			declared["restricted "+r.FG+" "+bg] = true
			assert.Less(t, ratio(r.FG, bg), 4.5, "%s 在 %s 上已经达标：限制过时，改 DESIGN.md", r.FG, bg)
		}
	}

	// DESIGN.md 颜色规则依赖的配对必须声明着（删掉一条，上面的检查就管不到它了）
	for _, bg := range []string{"bg", "bg-card", "bg-elevated"} {
		assert.True(t, declared["text text-secondary "+bg], "text-secondary 在 %s 上", bg)
		assert.True(t, declared["restricted text-tertiary "+bg], "text-tertiary 在 %s 上", bg)
	}
	for _, bg := range []string{"accent-fill", "accent-fill-hover", "accent-fill-active", "error-fill", "error-fill-hover", "error-fill-active"} {
		assert.True(t, declared["text text "+bg], "实心按钮的白字在 %s 上", bg)
	}
	assert.True(t, declared["text accent-on-elevated bg-elevated"])
	assert.True(t, declared["text error-on-elevated bg-elevated"])
}

// 公开仓库里的设计文档与 token 不出现其他产品的品牌名。
func TestNoForeignBrandInDesignFiles(t *testing.T) {
	for _, rel := range append([]string{tokensPath}, outputPaths...) {
		content := strings.ToLower(string(readFile(t, filepath.Join(repoRoot, filepath.FromSlash(rel)))))
		assert.NotContains(t, content, "animego", rel)
	}
}

func TestColorMath(t *testing.T) {
	white := rgba{R: 255, G: 255, B: 255, A: 1}
	black := rgba{A: 1}
	blue := rgba{R: 0x0a, G: 0x84, B: 0xff, A: 1}

	assert.InDelta(t, 21, contrastRatio(white, black), 1e-9)
	assert.InDelta(t, 21, contrastRatio(black, white), 1e-9, "前景比底色暗也一样算")
	assert.InDelta(t, 3.65, contrastRatio(white, blue), 0.005)
	// 半透明文字先叠到底色上：30% 白在黑底上只有 2.25:1
	assert.InDelta(t, 2.25, contrastRatio(rgba{R: 235, G: 235, B: 245, A: 0.3}, black), 0.01)

	assert.Equal(t, "#b1b1b1", neutralTone(0.76).hex())
	assert.Equal(t, "#000000", neutralTone(0).hex())
	assert.Equal(t, "#ffffff", neutralTone(1).hex())
}

func TestParseColor(t *testing.T) {
	c, err := parseColor("rgba(235, 235, 245, 0.6)")
	require.NoError(t, err)
	assert.Equal(t, rgba{R: 235, G: 235, B: 245, A: 0.6}, c)
	assert.Equal(t, "rgba(235, 235, 245, 0.6)", c.css())

	c, err = parseColor("#0a84ff")
	require.NoError(t, err)
	assert.Equal(t, "#0a84ff", c.css())

	for _, bad := range []string{"#FFF", "#FFFFFF", "rgb(1, 2, 3)", "rgba(256, 0, 0, 1)", "rgba(0, 0, 0, 1.5)", "red"} {
		_, err := parseColor(bad)
		assert.Error(t, err, bad)
	}
}

func TestHasTarget(t *testing.T) {
	assert.True(t, hasTarget(nil, allTargets, targetLua), "省略 = 该组默认")
	assert.False(t, hasTarget([]string{}, allTargets, targetCSS), "空数组 = 只进文档")
	assert.True(t, hasTarget([]string{targetCSS}, allTargets, targetCSS))
	assert.False(t, hasTarget([]string{targetCSS}, allTargets, targetSwift))
}

func TestNameConversion(t *testing.T) {
	assert.Equal(t, "bgElevated", camelCase("bg-elevated"))
	assert.Equal(t, "toastIn", camelCase("toast-in"))
	assert.Equal(t, "bg_elevated", snakeCase("bg-elevated"))
}

// minimalTokens 是能通过校验的最小 tokens.json；校验测试在它上面做一处改动。
const minimalTokens = `{
  "version": 1,
  "colors": [
    { "name": "bg", "value": "#000000", "ios": "systemBackground", "use": "底色" },
    { "name": "text", "value": "#ffffff", "use": "文字" }
  ],
  "type": [{ "name": "body", "size": 16, "weight": 400, "tracking": 0, "iosTextStyle": "body", "use": "正文" }],
  "spacing": [{ "name": "sm", "value": 8, "use": "间距" }],
  "contrast": {
    "text": [{ "fg": "text", "on": ["bg"], "use": "正文" }],
    "restricted": [{ "fg": "bg", "on": ["bg"], "use": "黑在黑上" }]
  }
}`

func TestValidation(t *testing.T) {
	_, err := decodeTokens([]byte(minimalTokens))
	require.NoError(t, err, "基准样本本身要合法")

	cases := []struct {
		name, old, new, want string
	}{
		{"未知字段", `"version": 1,`, `"version": 1, "colour": [],`, "unknown field"},
		{"版本", `"version": 1`, `"version": 2`, "version"},
		{"大写十六进制", `"#ffffff"`, `"#FFFFFF"`, "小写"},
		{"非规范写法", `"#ffffff"`, `"rgba(255,255,255,1)"`, "规范形式"},
		{"不认识的 iOS 名", `"systemBackground"`, `"systemBackgroundColor"`, "不认识的 iOS 语义色"},
		{"iOS 值不一致", `"systemBackground"`, `"systemBlue"`, "不一致"},
		{"名字形状", `"name": "text"`, `"name": "Text"`, "小写短横线"},
		{"用途为空", `"use": "文字"`, `"use": ""`, "use"},
		{"用途含竖线", `"use": "文字"`, `"use": "a | b"`, "不能换行"},
		{"CSS 变量重名", `"name": "text"`, `"name": "sp-sm"`, "重名"},
		{"Lua 关键字", `"name": "sm"`, `"name": "end"`, "Lua 标识符"},
		{"Swift 关键字", `"name": "sm"`, `"name": "default"`, "Swift 标识符"},
		{"输出目标", `"value": 8,`, `"value": 8, "targets": ["web"],`, "输出目标"},
		{"缺 iOS 文字样式", `, "iosTextStyle": "body"`, ``, "iosTextStyle"},
		{"不认识的文字样式", `"iosTextStyle": "body"`, `"iosTextStyle": "huge"`, "文字样式"},
		{"字重", `"weight": 400`, `"weight": 450`, "整百"},
		{"对比度引用", `"fg": "text", "on": ["bg"]`, `"fg": "missing", "on": ["bg"]`, "不在 colors 里"},
		{"对比度底色缺失", `"fg": "text", "on": ["bg"]`, `"fg": "text", "on": ["nope"]`, "不在 colors 里"},
		{"对比度没有底色", `"fg": "text", "on": ["bg"]`, `"fg": "text", "on": []`, "至少要有一种底色"},
		{"文字不到 4.5", `"fg": "text", "on": ["bg"]`, `"fg": "bg", "on": ["bg"]`, "要 ≥ 4.5:1"},
		{"界面元素不到 3", `"restricted": [`, `"ui": [{ "fg": "bg", "on": ["bg"], "use": "色块" }], "restricted": [`, "要 ≥ 3:1"},
		{"限制已过时", `"fg": "bg", "on": ["bg"], "use": "黑在黑上"`, `"fg": "text", "on": ["bg"], "use": "白在黑上"`, "限制过时了"},
		{"use 里的对比度与实算不符", `"use": "文字" }`, `"use": "文字（20.00:1）" }`, "实算出这个值"},
		{"阴影截断样式表", `"version": 1,`, `"version": 1, "shadows": [{ "name": "x", "value": "0 0 1px red; } body {", "use": "坏" }],`, "不能含"},
		{"多余内容", `"use": "黑在黑上" }]
  }
}`, `"use": "黑在黑上" }]
  }
} {}`, "之后还有内容"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Contains(t, minimalTokens, tc.old, "用例的 old 要能在基准样本里找到")
			_, err := decodeTokens([]byte(strings.Replace(minimalTokens, tc.old, tc.new, 1)))
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

func TestDesignBlocks(t *testing.T) {
	blocks := map[string]string{"a": "表 A", "b": "表 B"}
	begin := func(n string) string { return "<!-- gentokens:begin " + n + " -->" }
	end := func(n string) string { return "<!-- gentokens:end " + n + " -->" }

	got, err := renderDesign([]byte("前\n"+begin("a")+"\n旧\n"+end("a")+"\n中\n"+begin("b")+end("b")+"\n后\n"), blocks)
	require.NoError(t, err)
	assert.Equal(t, "前\n"+begin("a")+"\n\n表 A\n"+end("a")+"\n中\n"+begin("b")+"\n\n表 B\n"+end("b")+"\n后\n", string(got))

	bad := map[string]string{
		"缺块":             begin("a") + end("a"),
		"重复":             begin("a") + end("a") + begin("a") + end("a") + begin("b") + end("b"),
		"不认识的块":          begin("a") + end("a") + begin("b") + end("b") + begin("c") + end("c"),
		"只有 end":         end("a"),
		"begin 后接别的 end": begin("a") + end("b"),
		"没有 end":         begin("a"),
	}
	for name, doc := range bad {
		t.Run(name, func(t *testing.T) {
			_, err := renderDesign([]byte(doc), blocks)
			assert.Error(t, err)
		})
	}
}

func readFile(t *testing.T, p string) []byte {
	t.Helper()
	b, err := os.ReadFile(p)
	require.NoError(t, err)
	return b
}
