package rules

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const sampleRSS = `<?xml version="1.0"?>
<rss version="2.0" xmlns:nyaa="https://nyaa.si/xmlns/nyaa">
<channel><title>feed</title>
<item>
  <title>[Grp] Show - 01</title>
  <link>https://x/1</link>
  <enclosure url="magnet:?xt=urn:btih:abc&amp;dn=a" length="123" type="application/x-bittorrent"/>
  <nyaa:infoHash>DEADBEEF</nyaa:infoHash>
  <torrent xmlns="https://mikanani.me/0.1/"><contentLength>1500000000</contentLength><pubDate>2026-01-01T12:00:00</pubDate></torrent>
</item>
<item><title>second</title></item>
</channel></rss>`

func TestXMLPathSelection(t *testing.T) {
	doc, err := parseXMLDoc([]byte(sampleRSS))
	require.NoError(t, err)
	ns := map[string]string{"nyaa": "https://nyaa.si/xmlns/nyaa", "mikan": "https://mikanani.me/0.1/"}

	segs, err := compileXMLPath("rss/channel/item", ns)
	require.NoError(t, err)
	items := selectXML(doc, segs)
	require.Len(t, items, 2)

	get := func(path string) string {
		s, err := compileXMLPath(path, ns)
		require.NoError(t, err)
		v, _ := xmlValue(items[0], s)
		return v
	}
	assert.Equal(t, "[Grp] Show - 01", get("title"))
	assert.Equal(t, "magnet:?xt=urn:btih:abc&dn=a", get("enclosure@url"), "实体应已解码")
	assert.Equal(t, "123", get("enclosure@length"))
	assert.Equal(t, "DEADBEEF", get("nyaa:infoHash"), "命名空间前缀按 URI 解析")
	assert.Equal(t, "1500000000", get("mikan:torrent/mikan:contentLength"), "默认命名空间元素也按 URI 匹配")
	assert.Equal(t, "", get("infoHash"), "无前缀只匹配无命名空间的元素")

	_, err = compileXMLPath("bogus:x", ns)
	assert.Error(t, err, "未声明前缀应报错")
	_, err = compileXMLPath("a@b/c", ns)
	assert.Error(t, err, "属性只能在最后一段")
}

func TestXMLMalformed(t *testing.T) {
	_, err := parseXMLDoc([]byte("<not-xml"))
	assert.Error(t, err)
}

func TestJSONLookup(t *testing.T) {
	root, err := parseJSONDoc([]byte(`{"resources":[{"title":"t","size":3460300,"fansub":{"name":"G"},"n":null,"ok":true}]}`))
	require.NoError(t, err)
	arr, err := jsonItems(root, "resources")
	require.NoError(t, err)
	require.Len(t, arr, 1)
	item := jsonItem{v: arr[0]}

	str := func(path string) string {
		v, _ := item.lookup(path)
		s, _ := jsonScalar(v)
		return s
	}
	assert.Equal(t, "t", str("title"))
	assert.Equal(t, "3460300", str("size"), "数字保留字面量")
	assert.Equal(t, "G", str("fansub.name"))
	assert.Equal(t, "", str("n"))
	assert.Equal(t, "true", str("ok"))
	_, ok := item.lookup("missing.deep")
	assert.False(t, ok)

	_, err = jsonItems(root, "resources.0")
	assert.Error(t, err, "非数组应报错")
	rootArr, err := parseJSONDoc([]byte(`[{"a":1}]`))
	require.NoError(t, err)
	arr, err = jsonItems(rootArr, "$")
	require.NoError(t, err)
	assert.Len(t, arr, 1)
}
