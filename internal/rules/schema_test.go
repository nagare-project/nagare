package rules

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const minimalRule = `
schema: 1
id: demo
name: Demo
request: { url: "https://x/rss?q={{query}}" }
format: xml
namespaces: { nyaa: "https://nyaa.si/xmlns/nyaa" }
items: rss/channel/item
fields:
  title: title
  magnet:
    any:
      - { path: "enclosure@url" }
      - { path: link }
  size: { path: "enclosure@length", transforms: [format_bytes] }
  fansub: { path: "$title", transforms: [parse_fansub] }
  infohash: { path: "nyaa:infoHash", transforms: [trim, lower] }
`

func TestParseRuleShorthands(t *testing.T) {
	r, err := Parse([]byte(minimalRule))
	require.NoError(t, err)
	assert.Equal(t, "title", r.Fields.Title.Path, "标量简写 = path")
	assert.Len(t, r.Fields.Magnet.Any, 2)
	assert.Equal(t, "format_bytes", r.Fields.Size.Transforms[0].Name)
	assert.Equal(t, "$title", r.Fields.Fansub.Path)
}

func TestParseRuleRejects(t *testing.T) {
	cases := map[string]string{
		"缺 {{query}}": strings.Replace(minimalRule, "{{query}}", "q", 1),
		"未知转换":        strings.Replace(minimalRule, "format_bytes", "explode", 1),
		"未知 schema":   strings.Replace(minimalRule, "schema: 1", "schema: 9", 1),
		"坏 id":        strings.Replace(minimalRule, "id: demo", "id: Bad Id", 1),
		"未声明命名空间前缀":   strings.Replace(minimalRule, "nyaa:infoHash", "foo:infoHash", 1),
		"拼错的键":        strings.Replace(minimalRule, "format: xml", "format: xml\nfromat: y", 1),
		"坏正则":         strings.Replace(minimalRule, "[trim, lower]", "[{regex: \"(\"}]", 1),
	}
	for name, src := range cases {
		_, err := Parse([]byte(src))
		assert.Error(t, err, name)
	}
}

func TestTransformMappingForms(t *testing.T) {
	src := strings.Replace(minimalRule, "[trim, lower]",
		`[{regex: "[0-9a-fA-F]{40}"}, {magnet: {dn: $title, trackers: [http://t/announce]}}]`, 1)
	r, err := Parse([]byte(src))
	require.NoError(t, err)
	ts := r.Fields.Infohash.Transforms
	require.Len(t, ts, 2)
	assert.Equal(t, "regex", ts[0].Name)
	assert.NotNil(t, ts[0].re, "regex 应在校验时预编译")
	assert.Equal(t, "magnet", ts[1].Name)
	assert.Equal(t, []string{"http://t/announce"}, ts[1].trackers())
}

// 超时上限：规则不能声明离谱的超时（HTTP 客户端另有 20s 硬顶，这里是自文档化的校验）。
func TestParseRuleRejectsHugeTimeout(t *testing.T) {
	src := strings.Replace(minimalRule,
		`request: { url: "https://x/rss?q={{query}}" }`,
		`request: { url: "https://x/rss?q={{query}}", timeout_seconds: 999 }`, 1)
	_, err := Parse([]byte(src))
	assert.Error(t, err)
}

// H2 回归：属性必须写成 elem@attr；"elem/@attr" 与孤立的 "@attr" 在加载期就拒绝，
// 否则会静默读到当前节点自己的属性（错得很安静）。
func TestParseRuleRejectsDetachedAttributeSegment(t *testing.T) {
	for name, path := range map[string]string{
		"斜杠后单独 @attr": `"enclosure/@url"`,
		"孤立 @attr":     `"@url"`,
		"items 指向属性":  `"rss/channel/item@id"`,
	} {
		src := strings.Replace(minimalRule, `- { path: "enclosure@url" }`, `- { path: `+path+` }`, 1)
		if name == "items 指向属性" {
			src = strings.Replace(minimalRule, "items: rss/channel/item", "items: rss/channel/item@id", 1)
		}
		_, err := Parse([]byte(src))
		assert.Error(t, err, name)
	}
}
