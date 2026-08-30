package library

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 差分语料测试（决议 CQ2）：testdata/parse.jsonl 由 animego 的 JS 实现生成
// （tools/gen-corpus/gen.mjs），这里逐条断言 Go 移植产出完全一致。
// 任何一条红了 = 与网站端行为漂移，症状是「同一个番两边分成两个剧集」——
// 修 Go 或修 JS 后【必须】再生语料并两边同步。

type corpusLine struct {
	Kind string          `json:"kind"`
	Name string          `json:"name"`
	In   json.RawMessage `json:"in"`
	Out  json.RawMessage `json:"out"`
}

type pipelineIn struct {
	Files []string `json:"files"`
}

// roundtrip 把 Go 值经 JSON 往返成 any，消除 int/float64 与 nil 切片的形状差异。
func roundtrip(t *testing.T, v any) any {
	t.Helper()
	raw, err := json.Marshal(v)
	require.NoError(t, err)
	var out any
	require.NoError(t, json.Unmarshal(raw, &out))
	return out
}

func decodeAny(t *testing.T, raw json.RawMessage) any {
	t.Helper()
	var out any
	require.NoError(t, json.Unmarshal(raw, &out))
	return out
}

func strOrNil(s *string) any {
	if s == nil {
		return nil
	}
	return *s
}

func intOrNil(n *int) any {
	if n == nil {
		return nil
	}
	return *n
}

func TestCorpus(t *testing.T) {
	f, err := os.Open("testdata/parse.jsonl")
	require.NoError(t, err, "语料缺失：先跑 bun tools/gen-corpus/gen.mjs")
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)

	lineNo := 0
	for scanner.Scan() {
		lineNo++
		var line corpusLine
		require.NoError(t, json.Unmarshal(scanner.Bytes(), &line), "第 %d 行不是合法 JSON", lineNo)

		switch line.Kind {
		case "meta":
			var in string
			require.NoError(t, json.Unmarshal(line.In, &in))
			t.Run(fmt.Sprintf("meta/%03d", lineNo), func(t *testing.T) {
				m := ParseEpisodeMeta(in)
				got := map[string]any{
					"title":      strOrNil(m.Title),
					"number":     intOrNil(m.Number),
					"kind":       m.Kind,
					"group":      strOrNil(m.Group),
					"resolution": strOrNil(m.Resolution),
					"season":     intOrNil(m.Season),
					"episodeAlt": intOrNil(m.EpisodeAlt),
				}
				assert.Equal(t, decodeAny(t, line.Out), roundtrip(t, got), "文件名: %s", in)
			})

		case "tokens":
			var in string
			require.NoError(t, json.Unmarshal(line.In, &in))
			t.Run(fmt.Sprintf("tokens/%03d", lineNo), func(t *testing.T) {
				assert.Equal(t, decodeAny(t, line.Out), roundtrip(t, NormalizeTokens(in)), "标题: %s", in)
			})

		case "video":
			var in string
			require.NoError(t, json.Unmarshal(line.In, &in))
			t.Run(fmt.Sprintf("video/%03d", lineNo), func(t *testing.T) {
				assert.Equal(t, decodeAny(t, line.Out), roundtrip(t, IsVideoFile(in)), "文件名: %s", in)
			})

		case "pipeline":
			var in pipelineIn
			require.NoError(t, json.Unmarshal(line.In, &in))
			t.Run("pipeline/"+line.Name, func(t *testing.T) {
				runPipelineCase(t, in.Files, line.Out)
			})

		default:
			t.Fatalf("第 %d 行未知语料类型 %q", lineNo, line.Kind)
		}
	}
	require.NoError(t, scanner.Err())
	require.Greater(t, lineNo, 0, "语料为空")
}

// runPipelineCase 复刻生成器的 buildItems 输入约定：
// size=1000+i、mtime=1700000000000+i，i 按【全部文件】（含非视频）的原始序号计。
func runPipelineCase(t *testing.T, files []string, expected json.RawMessage) {
	t.Helper()
	src := make([]SourceFile, len(files))
	for i, rel := range files {
		src[i] = SourceFile{RelPath: rel, Size: int64(1000 + i), MTimeMs: int64(1700000000000 + i)}
	}
	items := BuildItems(src)
	groups := GroupByFolder(items)
	clusters := Clusterize(groups, nil)

	itemsProj := make([]any, 0, len(items))
	for _, it := range items {
		itemsProj = append(itemsProj, map[string]any{
			"fileId":       it.FileID,
			"episode":      intOrNil(it.Episode),
			"parsedTitle":  strOrNil(it.ParsedTitle),
			"parsedSeason": intOrNil(it.ParsedSeason),
			"parsedKind":   it.ParsedKind,
		})
	}
	groupsProj := make([]any, 0, len(groups))
	for _, g := range groups {
		ids := make([]string, 0, len(g.Items))
		for _, it := range g.Items {
			ids = append(ids, it.FileID)
		}
		groupsProj = append(groupsProj, map[string]any{
			"groupKey":     g.GroupKey,
			"label":        g.Label,
			"sortMode":     g.SortMode,
			"hasAmbiguity": g.HasAmbiguity,
			"items":        ids,
		})
	}
	clustersProj := make([]any, 0, len(clusters))
	for _, c := range clusters {
		ids := make([]string, 0, len(c.Items))
		for _, it := range c.Items {
			ids = append(ids, it.FileID)
		}
		var rep any
		if c.Representative != nil {
			rep = c.Representative.FileID
		}
		verdict := MatchCluster(c, nil)
		var confidence any
		if verdict.Kind == VerdictNew {
			confidence = verdict.Confidence
		}
		clustersProj = append(clustersProj, map[string]any{
			"clusterKey":     c.ClusterKey,
			"tokens":         c.Tokens,
			"representative": rep,
			"items":          ids,
			"verdictKind":    string(verdict.Kind),
			"confidence":     confidence,
		})
	}

	var exp struct {
		Items    json.RawMessage `json:"items"`
		Groups   json.RawMessage `json:"groups"`
		Clusters json.RawMessage `json:"clusters"`
	}
	require.NoError(t, json.Unmarshal(expected, &exp))
	assert.Equal(t, decodeAny(t, exp.Items), roundtrip(t, itemsProj), "items 漂移")
	assert.Equal(t, decodeAny(t, exp.Groups), roundtrip(t, groupsProj), "groups 漂移")
	assert.Equal(t, decodeAny(t, exp.Clusters), roundtrip(t, clustersProj), "clusters 漂移")
}
