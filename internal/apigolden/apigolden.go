// Package apigolden 管仓库根 testdata/api/ 下的 API 黄金响应：Go 处理器测试把真实响应写成规范化的 JSON，
// 每个客户端（现在是前端 TypeScript，以后是 Swift / Kotlin）都严格解码每一份。Go 改了字段、
// 客户端没跟上，两边总有一边的测试红 —— 而不是客户端静默解出一堆零值。
//
// 只给测试用（只有 _test.go 引用它）：导入本包的测试二进制多出一个 -update 开关，
// 重新生成的写法见 docs/api-goldens.md。
//
// 文件名约定（客户端据此挑自己要的那部分）：
//
//	desktop-*  只有桌面版注册的端点（磁力 / 来源插件 / 规则，internal/api/sources）
//	update-*   自更新检查（internal/update）
//	其余       internal/api 的端点，所有客户端共用
package apigolden

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// update 为真时重写黄金文件而不是比对。
var update = flag.Bool("update", false, "重写仓库根 testdata/api/ 下的 API 黄金响应")

// 文件名前缀：按归属分开，见包注释。
const (
	PrefixDesktop = "desktop-"
	PrefixUpdate  = "update-"
)

var nameRE = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// Dir 返回仓库根的 testdata/api（从当前目录往上找 go.mod）。
func Dir(t testing.TB) string {
	t.Helper()
	dir, err := os.Getwd()
	require.NoError(t, err)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return filepath.Join(dir, "testdata", "api")
		}
		parent := filepath.Dir(dir)
		require.NotEqual(t, dir, parent, "往上找不到 go.mod，没法定位 testdata/api")
		dir = parent
	}
}

// Canonical 把响应体排成黄金文件的样子：两格缩进、末尾一个换行。
// 键的顺序沿用 encoding/json 的输出（结构体按字段顺序、map 按键排序），本身就是稳定的。
// 不是合法 JSON、或者 JSON 后面还跟着别的东西，都算错。
func Canonical(body []byte) ([]byte, error) {
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	var probe any
	if err := dec.Decode(&probe); err != nil {
		return nil, err
	}
	if dec.More() {
		return nil, errors.New("JSON 之后还有多余的内容")
	}
	var out bytes.Buffer
	if err := json.Indent(&out, bytes.TrimSpace(body), "", "  "); err != nil {
		return nil, err
	}
	out.WriteByte('\n')
	return out.Bytes(), nil
}

// Scrub 把响应体里随机器 / 随运行变化、又没有注入点的字面值（临时目录的路径、store 生成的
// 目录 ID 与添加时间）换成固定值。按 JSON 字符串的转义形式匹配，old 为空的对子跳过。
func Scrub(body []byte, pairs ...string) []byte {
	if len(pairs)%2 != 0 {
		panic("apigolden.Scrub: 参数要成对给出")
	}
	out := body
	for i := 0; i < len(pairs); i += 2 {
		if pairs[i] == "" {
			continue
		}
		out = bytes.ReplaceAll(out, jsonInner(pairs[i]), jsonInner(pairs[i+1]))
	}
	return out
}

// jsonInner 是 s 编成 JSON 字符串后去掉两头引号的样子（路径里的反斜杠等按转义后的形式匹配）。
func jsonInner(s string) []byte {
	b, err := json.Marshal(s)
	if err != nil {
		panic(err)
	}
	return b[1 : len(b)-1]
}

// Assert 比对（或在 -update 时重写）testdata/api/<name>.json。
func Assert(t testing.TB, name string, body []byte) {
	t.Helper()
	require.Regexp(t, nameRE, name, "黄金文件名只用小写字母、数字和连字符")
	got, err := Canonical(body)
	require.NoError(t, err, "响应体不是一份完整的 JSON：%s", body)
	path := filepath.Join(Dir(t), name+".json")
	if *update {
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, got, 0o644))
		return
	}
	want, err := os.ReadFile(path)
	require.NoError(t, err, "缺少黄金文件 %s：确认改动无误后照 docs/api-goldens.md 重新生成", path)
	require.Equal(t, string(want), string(got),
		"%s 与处理器的实际输出不一致：确认改动无误后照 docs/api-goldens.md 重新生成，并同步更新各客户端的类型", path)
}

// AssertOwned 检查 testdata/api 里归本包管的文件（owns 判定）正好是这次产出的那些：
// 删掉或改名一个用例时，旧的黄金文件不能留在目录里继续冒充契约。
// -update 时直接删掉多出来的文件。
func AssertOwned(t testing.TB, owns func(name string) bool, produced []string) {
	t.Helper()
	entries, err := os.ReadDir(Dir(t))
	require.NoError(t, err)
	var orphans []string
	for _, e := range entries {
		name, ok := strings.CutSuffix(e.Name(), ".json")
		if !ok || e.IsDir() || !owns(name) || slices.Contains(produced, name) {
			continue
		}
		if *update {
			require.NoError(t, os.Remove(filepath.Join(Dir(t), e.Name())))
			continue
		}
		orphans = append(orphans, e.Name())
	}
	require.Empty(t, orphans, "这些黄金文件没有对应的用例（用例删了或改名了？）：照 docs/api-goldens.md 重新生成会删掉它们")
}
