package api

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// 期望值是用参照实现的同一套算法（OKLab → atan2，取一位小数）对真实目录里的颜色算出来的：
// 两边对同一个颜色给出同一个色相，发现页的染色才与参照的版面一致。
func TestAccentHueMatchesReferenceAlgorithm(t *testing.T) {
	cases := map[string]float64{
		"#31add6":   225.2,
		"#d88852":   53.5,
		"#ae5de4":   309.4,
		"#b0a200":   103.3,
		"#fe286b":   10.6,
		"#d98a26":   66.4,
		"#ff0000":   29.2,
		"D98A26":    66.4, // 没有 # 也认
		" #e47843 ": 45.2,
	}
	for hex, want := range cases {
		got := accentHue(hex)
		require.NotNil(t, got, hex)
		require.InDelta(t, want, *got, 1e-9, hex)
	}
}

func TestAccentHueRejectsPlaceholderGreyAndGarbage(t *testing.T) {
	for _, hex := range []string{"", "#8b5cf6", "#8B5CF6", "#808080", "#000000", "#ffffff", "#12345", "#gggggg", "red", "#1234567"} {
		require.Nil(t, accentHue(hex), hex)
	}
}

// 色相落在 [0, 360)：359.96 取一位小数是 360.0，要写成 0。
func TestAccentHueStaysBelow360(t *testing.T) {
	for _, hex := range []string{"#ff0088", "#ff0188"} { // 原始色相 359.976 / 359.994
		h := accentHue(hex)
		require.NotNil(t, h, hex)
		require.Equal(t, 0.0, *h, hex)
	}
	for r := 0; r < 256; r += 5 {
		for b := 0; b < 256; b += 5 {
			hex := "#" + hex2(r) + "00" + hex2(b)
			if h := accentHue(hex); h != nil {
				require.GreaterOrEqual(t, *h, 0.0, hex)
				require.Less(t, *h, 360.0, hex)
			}
		}
	}
}

func hex2(v int) string {
	const digits = "0123456789abcdef"
	return string([]byte{digits[v>>4], digits[v&15]})
}
