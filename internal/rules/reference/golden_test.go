// 临时参照组：复制自 animego go-api/internal/torrents（AGPL-3.0，同一作者）。M2 差分测试跑通后整包删除，勿在此新增功能。

// Package reference — golden_test.go
//
// 黄金输出生成与比对（决议 CQ1）。这是参照组里唯一一个 nagare 自己写的
// 文件：它读 ../testdata/<source>/cases.json，用 httptest 把 fixture 按清单
// 里的 HTTP 状态喂给旧适配器，再把 []TorrentItem 以稳定缩进 JSON 落成
// <case>.golden.json。
//
//   - 默认：逐字节比对 golden（缺失即失败，提示用 -update 生成）
//   - go test ./internal/rules/reference/ -update：重写全部 golden
//
// 规则引擎（M2）的差分测试只吃 testdata（fixture + cases.json + golden），
// 不依赖本包；参照组跑通后整包删除时 golden 仍然是真值。
//
// 清单里 expect == "error" 的用例不产 golden，但会断言旧适配器确实返回
// error（而不是空切片）——规则引擎侧据此把它们分类为「源失败」而非「无结果」。
package reference

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// updateGolden 为 true 时重写 golden 而非比对。只在本包定义，所以要用
// `go test ./internal/rules/reference/ -update`，不能从 ./... 透传。
var updateGolden = flag.Bool("update", false, "重写 testdata/<source>/<case>.golden.json")

// testdataRoot 相对于测试进程的工作目录（即本包目录）。
const testdataRoot = "../testdata"

// goldenCase 是 cases.json 里一条用例的形状。字段语义见 testdata/README.md。
type goldenCase struct {
	Name    string `json:"name"`
	File    string `json:"file"`
	Query   string `json:"query"`
	Status  int    `json:"status"`
	Kind    string `json:"kind"`
	AnidbID int    `json:"anidbId,omitempty"`
	Expect  string `json:"expect"`
}

// goldenSource 把 testdata 子目录绑定到旧适配器入口。
//
//   - param  ：承载 query 的 URL 参数名（anidb 用例走 aid，不看此字段）
//   - errTag ：旧适配器错误信息里的源前缀，用来确认 error 出自该源
type goldenSource struct {
	dir    string
	param  string
	errTag string
	fetch  func(ctx context.Context, c *http.Client, tc goldenCase) ([]TorrentItem, error)
}

var goldenSources = []goldenSource{
	{dir: "garden", param: "search", errTag: "garden",
		fetch: func(ctx context.Context, c *http.Client, tc goldenCase) ([]TorrentItem, error) {
			return FetchGarden(ctx, c, nil, tc.Query)
		}},
	{dir: "acgrip", param: "term", errTag: "acgrip",
		fetch: func(ctx context.Context, c *http.Client, tc goldenCase) ([]TorrentItem, error) {
			return FetchAcgRip(ctx, c, tc.Query)
		}},
	{dir: "nyaa", param: "q", errTag: "nyaa",
		fetch: func(ctx context.Context, c *http.Client, tc goldenCase) ([]TorrentItem, error) {
			return FetchNyaa(ctx, c, tc.Query)
		}},
	{dir: "dmhy", param: "keyword", errTag: "dmhy",
		fetch: func(ctx context.Context, c *http.Client, tc goldenCase) ([]TorrentItem, error) {
			return FetchDmhy(ctx, c, tc.Query)
		}},
	{dir: "mikan", param: "searchstr", errTag: "mikan",
		fetch: func(ctx context.Context, c *http.Client, tc goldenCase) ([]TorrentItem, error) {
			return FetchMikan(ctx, c, tc.Query)
		}},
	{dir: "animetosho", param: "q", errTag: "tosho",
		fetch: func(ctx context.Context, c *http.Client, tc goldenCase) ([]TorrentItem, error) {
			if tc.Kind == "anidb" {
				return FetchAnimeToshoByAniDB(ctx, c, tc.AnidbID)
			}
			return FetchAnimeTosho(ctx, c, nil, tc.Query)
		}},
}

// ---------------------------------------------------------------------------
// 主测试：每源 × 每用例
// ---------------------------------------------------------------------------

func TestGolden(t *testing.T) {
	t.Parallel()

	for _, src := range goldenSources {
		src := src
		t.Run(src.dir, func(t *testing.T) {
			t.Parallel()
			for _, tc := range loadCases(t, src.dir) {
				tc := tc
				t.Run(tc.Name, func(t *testing.T) {
					t.Parallel()
					runGoldenCase(t, src, tc)
				})
			}
		})
	}
}

// runGoldenCase 起一个只回 fixture 的 httptest 服务，经 rewriteTransport 让旧
// 适配器的生产 URL 常量打过来，然后按 expect 分三路断言。
func runGoldenCase(t *testing.T, src goldenSource, tc goldenCase) {
	t.Helper()

	body := readFixture(t, src.dir, tc.File)

	// 记录到达测试服务器的查询串，确认 query 落在了旧适配器约定的参数上。
	var (
		mu       sync.Mutex
		seenReqs []url.Values
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seenReqs = append(seenReqs, r.URL.Query())
		mu.Unlock()
		if tc.Status == http.StatusOK {
			w.Header().Set("Content-Type", contentTypeFor(tc.File))
		}
		w.WriteHeader(tc.Status)
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	items, err := src.fetch(context.Background(), newRewriteClient(srv.URL), tc)

	mu.Lock()
	reqs := append([]url.Values(nil), seenReqs...)
	mu.Unlock()
	require.Len(t, reqs, 1, "旧适配器对一次 Fetch 应恰好发一个请求")
	assertRequestShape(t, src, tc, reqs[0])

	goldenPath := filepath.Join(testdataRoot, src.dir, tc.Name+".golden.json")

	switch tc.Expect {
	case "error":
		require.Error(t, err, "expect=error 的用例旧适配器必须返回 error，而非空切片")
		assert.Empty(t, items)
		assert.Contains(t, err.Error(), src.errTag, "错误应带源前缀，便于归因")
		_, statErr := os.Stat(goldenPath)
		assert.True(t, os.IsNotExist(statErr), "error 用例不该有 golden：%s", goldenPath)
		return
	case "empty":
		require.NoError(t, err)
		require.NotNil(t, items, "空结果必须是非 nil 空切片（序列化为 []）")
		require.Empty(t, items)
	case "items":
		require.NoError(t, err)
		require.NotEmpty(t, items)
	default:
		t.Fatalf("cases.json 里未知的 expect=%q", tc.Expect)
	}

	got := marshalGolden(t, items)
	if *updateGolden {
		require.NoError(t, os.WriteFile(goldenPath, got, 0o644))
		return
	}
	want, readErr := os.ReadFile(goldenPath)
	require.NoError(t, readErr, "缺少 golden；运行 `go test ./internal/rules/reference/ -update` 生成")
	assert.Equal(t, string(want), string(got),
		"与 golden 不一致（%s）。确认是预期变更后用 -update 重写", goldenPath)
}

// assertRequestShape 校验查询参数：search 用例的 query 必须原样落在源约定的
// 参数上；anidb 用例必须带 aid 且不带 q。
func assertRequestShape(t *testing.T, src goldenSource, tc goldenCase, q url.Values) {
	t.Helper()
	if tc.Kind == "anidb" {
		assert.Equal(t, strconv.Itoa(tc.AnidbID), q.Get("aid"), "anidb 用例走 ?aid=")
		assert.Empty(t, q.Get("q"), "anidb 用例不得带关键词")
		return
	}
	assert.Equal(t, tc.Query, q.Get(src.param), "query 应落在 ?%s=", src.param)
}

// ---------------------------------------------------------------------------
// 清单一致性：没有孤儿 fixture / 孤儿 golden
// ---------------------------------------------------------------------------

// TestGoldenManifestCoversDir 保证每个源目录里除 cases.json 外的文件都被清单
// 引用（fixture）或对应一条非 error 用例（golden），避免 testdata 悄悄长出没人
// 用的文件。
func TestGoldenManifestCoversDir(t *testing.T) {
	t.Parallel()

	for _, src := range goldenSources {
		src := src
		t.Run(src.dir, func(t *testing.T) {
			t.Parallel()
			cases := loadCases(t, src.dir)

			fixtures := map[string]bool{}
			goldens := map[string]bool{}
			for _, tc := range cases {
				fixtures[tc.File] = true
				if tc.Expect != "error" {
					goldens[tc.Name+".golden.json"] = true
				}
			}

			entries, err := os.ReadDir(filepath.Join(testdataRoot, src.dir))
			require.NoError(t, err)
			for _, e := range entries {
				name := e.Name()
				if name == "cases.json" {
					continue
				}
				if strings.HasSuffix(name, ".golden.json") {
					assert.True(t, goldens[name], "孤儿 golden（没有对应的非 error 用例）：%s/%s", src.dir, name)
					continue
				}
				assert.True(t, fixtures[name], "孤儿 fixture（cases.json 未引用）：%s/%s", src.dir, name)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// loadCases 读取并校验一个源的 cases.json。校验规则同时是清单的书面契约：
//   - name 非空且唯一；file 非空且存在
//   - kind ∈ {search, anidb}；anidb 只允许 animetosho，且 anidbId > 0
//   - expect ∈ {items, empty, error}
//   - status != 200 ⇒ expect 必须是 error（旧适配器对非 2xx 一律报错）
func loadCases(t *testing.T, dir string) []goldenCase {
	t.Helper()

	raw, err := os.ReadFile(filepath.Join(testdataRoot, dir, "cases.json"))
	require.NoError(t, err)
	var cases []goldenCase
	require.NoError(t, json.Unmarshal(raw, &cases))
	require.NotEmpty(t, cases, "%s/cases.json 为空", dir)

	seen := map[string]bool{}
	for _, tc := range cases {
		require.NotEmpty(t, tc.Name, "%s: 用例缺 name", dir)
		require.False(t, seen[tc.Name], "%s: 重复用例名 %q", dir, tc.Name)
		seen[tc.Name] = true

		require.NotEmpty(t, tc.File, "%s/%s: 缺 file", dir, tc.Name)
		_, statErr := os.Stat(filepath.Join(testdataRoot, dir, tc.File))
		require.NoError(t, statErr, "%s/%s: fixture 不存在", dir, tc.Name)

		switch tc.Kind {
		case "search":
		case "anidb":
			require.Equal(t, "animetosho", dir, "%s/%s: 只有 animetosho 有 anidb 用例", dir, tc.Name)
			require.Greater(t, tc.AnidbID, 0, "%s/%s: anidb 用例需要 anidbId", dir, tc.Name)
		default:
			t.Fatalf("%s/%s: 未知 kind=%q", dir, tc.Name, tc.Kind)
		}

		switch tc.Expect {
		case "items", "empty", "error":
		default:
			t.Fatalf("%s/%s: 未知 expect=%q", dir, tc.Name, tc.Expect)
		}
		require.Greater(t, tc.Status, 0, "%s/%s: status 缺失", dir, tc.Name)
		if tc.Status != http.StatusOK {
			require.Equal(t, "error", tc.Expect, "%s/%s: 非 200 必须 expect=error", dir, tc.Name)
		}
	}
	return cases
}

func readFixture(t *testing.T, dir, file string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(testdataRoot, dir, file))
	require.NoError(t, err)
	return b
}

// contentTypeFor 按后缀给 200 响应挂 Content-Type。旧适配器不看这个头，
// 挂上只是让 fixture 更像真实上游。
func contentTypeFor(file string) string {
	if strings.HasSuffix(file, ".json") {
		return "application/json"
	}
	return "application/xml"
}

// marshalGolden 以稳定缩进 JSON 序列化：字段序即 TorrentItem 结构体序，
// nil 指针按各自的 json tag 输出（Fansub/Date → null；Provider/Seeders 带
// omitempty → 键省略）。关掉 HTML 转义，否则 magnet 里的每个 & 都会变成
// &，golden 不可读。除此之外与 json.MarshalIndent 逐字节一致（多一个
// 结尾换行）。
func marshalGolden(t *testing.T, items []TorrentItem) []byte {
	t.Helper()
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	require.NoError(t, enc.Encode(items))
	return buf.Bytes()
}
