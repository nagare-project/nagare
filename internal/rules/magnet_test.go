package rules

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseInfohash(t *testing.T) {
	hex40 := strings.Repeat("ab", 20)
	cases := map[string]string{
		"magnet:?xt=urn:btih:" + strings.ToUpper(hex40) + "&dn=x": hex40,
		"magnet:?dn=x&XT=URN:BTIH:" + hex40:                       hex40, // 键与前缀大小写不敏感
		"magnet:?xt=urn:btih:" + hex40 + ";dn=y":                  hex40, // ; 分隔
		"magnet:?xt=urn:btih:" + strings.Repeat("c", 64):          strings.Repeat("c", 64),
		"magnet:?xt=urn:btih:ABCDEFGHIJKLMNOPQRSTUVWXYZ234567":    "00443214c74254b635cf84653a56d7c675be77df", // base32 → hex
		"magnet:?xt=urn:btih:zz" + strings.Repeat("0", 38):        "",                                         // 非 hex
		"magnet:?dn=only":     "",
		"magnet:noquery":      "",
		"https://x/y.torrent": "",
	}
	for in, want := range cases {
		assert.Equal(t, want, ParseInfohash(in), in)
	}
}

func TestBuildMagnet(t *testing.T) {
	got := buildMagnet(" "+strings.Repeat("ab", 20)+" ", "Show 第1集 [1080p]", []string{"http://t1/announce", "udp://t2:1337"})
	assert.True(t, strings.HasPrefix(got, "magnet:?xt=urn:btih:"+strings.Repeat("ab", 20)+"&dn="))
	assert.Contains(t, got, "&tr=http%3A%2F%2Ft1%2Fannounce")
	assert.Contains(t, got, "&tr=udp%3A%2F%2Ft2%3A1337")
	assert.Contains(t, got, "%5B1080p%5D", "标题应 QueryEscape")
	assert.Equal(t, "", buildMagnet("", "t", nil), "无 hash → 空串（随后按非 magnet 丢弃）")
	assert.Equal(t, "", buildMagnet("abc123", "t", nil), "形状不合法的 hash 也丢弃")
	upper := strings.ToUpper(strings.Repeat("ab", 20))
	assert.Contains(t, buildMagnet(upper, "t", nil), "urn:btih:"+upper, "合法 hash 原样保留大小写")
}
