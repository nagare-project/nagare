package artcache

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nagare-project/nagare/internal/colorspace"
)

// 色阶的明度与彩度只有一个来源：design/tokens.json。这里的副本与它逐档核对。
func TestLadderMatchesDesignTokens(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "design", "tokens.json"))
	require.NoError(t, err)
	var f struct {
		PosterTones []struct {
			Name string  `json:"name"`
			L    float64 `json:"l"`
			C    float64 `json:"c"`
		} `json:"posterTones"`
	}
	require.NoError(t, json.Unmarshal(raw, &f))
	require.Len(t, f.PosterTones, len(posterLadder), "tokens.json 的海报色阶档数变了：PosterTones 的字段也要跟着改")
	for i, want := range f.PosterTones {
		got := posterLadder[i]
		assert.Equal(t, want.Name, got.name, "第 %d 档", i)
		assert.Equal(t, want.L, got.l, "%s 的明度", want.Name)
		assert.Equal(t, want.C, got.c, "%s 的彩度", want.Name)
	}
}

var hexRE = regexp.MustCompile(`^#[0-9a-f]{6}$`)

// 对比度地板按【最终下发的 sRGB 值】（色域映射 + 取整之后）在全部色相上核对，
// 色相取接口实际会发的精度（0.1°），所以这 3600 个值就是全部可能的输出。
//
//   - Tone、Quiet 是要读的字：在三层底色上都 ≥ 4.5:1
//   - Mid 是非文字界面元素：在 --bg 上 ≥ 3:1
//   - OnTone 写在 Tone 上：≥ 4.5:1
func TestLadderContrastAllHues(t *testing.T) {
	bgs := map[string]colorspace.RGB{
		"--bg":          colorspace.RGBFrom8(0x00, 0x00, 0x00),
		"--bg-card":     colorspace.RGBFrom8(0x1c, 0x1c, 0x1e),
		"--bg-elevated": colorspace.RGBFrom8(0x2c, 0x2c, 0x2e),
	}
	worst := map[string]float64{}
	note := func(key string, ratio float64) {
		if v, ok := worst[key]; !ok || ratio < v {
			worst[key] = ratio
		}
	}
	for i := 0; i < 3600; i++ {
		hue := float64(i) / 10
		lad := LadderFor(hue)
		for _, h := range []string{lad.Tone, lad.Quiet, lad.Mid, lad.Low, lad.OnTone} {
			require.Regexp(t, hexRE, h)
		}
		tone, quiet, mid, onTone := mustHex(t, lad.Tone), mustHex(t, lad.Quiet), mustHex(t, lad.Mid), mustHex(t, lad.OnTone)
		for name, bg := range bgs {
			r := colorspace.Contrast(tone, bg)
			note("tone on "+name, r)
			if r < 4.5 {
				t.Errorf("h=%.1f: tone %s 在 %s 上只有 %.3f:1", hue, lad.Tone, name, r)
			}
			r = colorspace.Contrast(quiet, bg)
			note("quiet on "+name, r)
			if r < 4.5 {
				t.Errorf("h=%.1f: quiet %s 在 %s 上只有 %.3f:1", hue, lad.Quiet, name, r)
			}
		}
		if r := colorspace.Contrast(mid, bgs["--bg"]); r < 3 {
			t.Errorf("h=%.1f: mid %s 在 --bg 上只有 %.3f:1", hue, lad.Mid, r)
		} else {
			note("mid on --bg", r)
		}
		if r := colorspace.Contrast(onTone, tone); r < 4.5 {
			t.Errorf("h=%.1f: onTone %s 在 tone %s 上只有 %.3f:1", hue, lad.OnTone, lad.Tone, r)
		} else {
			note("onTone on tone", r)
		}
	}
	keys := make([]string, 0, len(worst))
	for k := range worst {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		t.Logf("最差 %-22s %.3f:1", k, worst[k])
	}
}

func TestLadderForIsDeterministic(t *testing.T) {
	assert.Equal(t, LadderFor(292.66), LadderFor(292.7))
	assert.Equal(t, 0.0, LadderFor(359.97).Hue, "359.97 进位成 0，不写成 360")
	assert.Equal(t, 10.0, LadderFor(370).Hue)
	assert.Equal(t, 350.0, LadderFor(-10).Hue)
}

func mustHex(t *testing.T, s string) colorspace.RGB {
	t.Helper()
	var r, g, b uint8
	_, err := fmt.Sscanf(s, "#%02x%02x%02x", &r, &g, &b)
	require.NoError(t, err)
	return colorspace.RGBFrom8(r, g, b)
}
