package rules

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 差分测试（决议 CQ1）：六条 YAML 规则对旧适配器的每一份 fixture 必须产出
// 逐字节相同的输出（黄金文件由 internal/rules/reference 里的旧适配器生成）。
// 参照组删除后，这套 fixture + golden 就是规则引擎的回归套件。

type fixtureCase struct {
	Name   string `json:"name"`
	File   string `json:"file"`
	Query  string `json:"query"`
	Status int    `json:"status"`
	Kind   string `json:"kind"`
	Expect string `json:"expect"` // items | empty | error
}

// sourceRuleFiles：testdata 目录名 → 规则文件。
var sourceRuleFiles = map[string]string{
	"garden": "garden.yaml", "acgrip": "acgrip.yaml", "nyaa": "nyaa.yaml",
	"dmhy": "dmhy.yaml", "mikan": "mikan.yaml", "animetosho": "animetosho.yaml",
}

// encodeGolden 与参照组 golden_test.go 的编码方式完全一致。
func encodeGolden(t *testing.T, items []Item) string {
	t.Helper()
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	require.NoError(t, enc.Encode(items))
	return buf.String()
}

func TestDifferentialAgainstReferenceGoldens(t *testing.T) {
	for dir, ruleFile := range sourceRuleFiles {
		rule, err := LoadFile(filepath.Join("testdata", "sources", ruleFile))
		require.NoError(t, err, dir)

		raw, err := os.ReadFile(filepath.Join("testdata", dir, "cases.json"))
		require.NoError(t, err, dir)
		var cases []fixtureCase
		require.NoError(t, json.Unmarshal(raw, &cases), dir)
		require.NotEmpty(t, cases, dir)

		for _, c := range cases {
			c := c
			t.Run(dir+"/"+c.Name, func(t *testing.T) {
				if c.Status != 200 {
					t.Skip("非 200 由 Fetcher 层分类，见 TestFetcherRun")
				}
				body, err := os.ReadFile(filepath.Join("testdata", dir, c.File))
				require.NoError(t, err)
				out := Evaluate(rule, body)

				if c.Expect == "error" {
					assert.Equal(t, StateFailed, out.State, "旧适配器报错的响应，引擎应判 failed")
					return
				}
				golden, err := os.ReadFile(filepath.Join("testdata", dir, c.Name+".golden.json"))
				require.NoError(t, err, "缺黄金文件")
				assert.Equal(t, string(golden), encodeGolden(t, out.Items), "与旧适配器输出不一致")

				switch c.Expect {
				case "items":
					assert.Equal(t, StateOK, out.State)
				case "empty":
					assert.Empty(t, out.Items)
					assert.Contains(t, []State{StateZero, StateDead}, out.State)
				}
			})
		}
	}
}

// 六条规则本身必须能被引擎完整加载（schema 校验 + 命名空间 + 转换词汇表）。
func TestSourceRulesLoad(t *testing.T) {
	rules, errs := LoadDir(filepath.Join("testdata", "sources"))
	assert.Empty(t, errs)
	assert.Len(t, rules, 6)
	ids := map[string]bool{}
	for _, r := range rules {
		ids[r.ID] = true
		assert.NotEmpty(t, r.SelfTest.Query, "%s 应声明自检关键词", r.ID)
	}
	for _, want := range []string{"garden", "acg", "nyaa", "dmhy", "mikan", "tosho"} {
		assert.True(t, ids[want], "缺少 %s", want)
	}
}
