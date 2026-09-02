package rules

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// 与旧适配器逐字对齐的格式化表（JS parseInt / toFixed 怪癖）。
func TestFormatBytes(t *testing.T) {
	cases := map[string]string{
		"1234567890": "1.2 GB", "1500000000": "1.5 GB", "500000": "500 KB", "40448000": "40 MB",
		"0": "", "": "", "-5": "", "abc": "", "1234abc": "1 KB", "  999": "1 KB",
		"99999999999999999999": "", // 溢出 → 无法解析 → 空
	}
	for in, want := range cases {
		assert.Equal(t, want, formatBytes(in), "formatBytes(%q)", in)
	}
}

func TestFormatKb(t *testing.T) {
	cases := map[string]string{
		"3460300": "3.5 GB", "40448": "40 MB", "512": "512 KB", "0": "", "x": "", "1500000": "1.5 GB",
	}
	for in, want := range cases {
		assert.Equal(t, want, formatKb(in), "formatKb(%q)", in)
	}
}

func TestParseIntJSLike(t *testing.T) {
	cases := []struct {
		in string
		n  int
		ok bool
	}{
		{"42", 42, true}, {" +7", 7, true}, {"-3x", -3, true}, {"12ab", 12, true},
		{"", 0, false}, {"abc", 0, false}, {"-", 0, false},
	}
	for _, c := range cases {
		n, ok := parseIntJSLike(c.in)
		assert.Equal(t, c.ok, ok, c.in)
		assert.Equal(t, c.n, n, c.in)
	}
}

func TestParseFansub(t *testing.T) {
	assert.Equal(t, "SubsPlease", parseFansub("[SubsPlease] X - 01"))
	assert.Equal(t, "喵萌奶茶屋", parseFansub("【喵萌奶茶屋】葬送的芙莉莲"))
	assert.Equal(t, "FooBar", parseFansub("[FooBar】rest"), "括号错配也要解出（与 JS 正则一致）")
	assert.Equal(t, "", parseFansub("no bracket"))
	assert.Equal(t, "", parseFansub(" [late] not at start"))
}
