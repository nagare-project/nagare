package mpv

import (
	"io/fs"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 缩略图的基准尺寸读自内嵌的 thumbfast.conf；兜底值与它一致，改了设置文件这里会提醒同步兜底。
func TestThumbfastMaxSizeFromConf(t *testing.T) {
	content, err := uiFiles.ReadFile(thumbfastConf)
	require.NoError(t, err)
	w, h, err := parseThumbfastMax(content)
	require.NoError(t, err)
	assert.Equal(t, thumbfastDefaultMax, w, "thumbfast.conf 改了 max_width，同步 thumbfastDefaultMax")
	assert.Equal(t, thumbfastDefaultMax, h, "thumbfast.conf 改了 max_height，同步 thumbfastDefaultMax")

	gw, gh := ThumbfastMaxSize()
	assert.Equal(t, w, gw)
	assert.Equal(t, h, gh)
}

func TestParseThumbfastMax(t *testing.T) {
	w, h, err := parseThumbfastMax([]byte("# 注释\nmax_width = 160\n\nnetwork=no\nmax_height=90\n"))
	require.NoError(t, err)
	assert.Equal(t, 160, w)
	assert.Equal(t, 90, h)

	_, _, err = parseThumbfastMax([]byte("max_width=160\n"))
	assert.Error(t, err, "缺 max_height")
	_, _, err = parseThumbfastMax([]byte("max_width=abc\nmax_height=90\n"))
	assert.Error(t, err)
	_, _, err = parseThumbfastMax([]byte("max_width=0\nmax_height=90\n"))
	assert.Error(t, err)
}

// render API 下 thumbfast 读不到 display-hidpi-scale，宿主按屏幕缩放把最大尺寸放大后传进去。
func TestThumbfastScaledSize(t *testing.T) {
	assert.Equal(t, []string{"thumbfast-max_width=192", "thumbfast-max_height=192"}, ThumbfastScaledSize(1))
	assert.Equal(t, []string{"thumbfast-max_width=384", "thumbfast-max_height=384"}, ThumbfastScaledSize(2))
	assert.Equal(t, []string{"thumbfast-max_width=288", "thumbfast-max_height=288"}, ThumbfastScaledSize(1.5))
	for _, bad := range []float64{0, -2, math.NaN(), math.Inf(1)} {
		assert.Equal(t, ThumbfastScaledSize(1), ThumbfastScaledSize(bad), "scale=%v 按 1 算", bad)
	}
}

func TestThumbfastMPVPath(t *testing.T) {
	assert.Empty(t, ThumbfastMPVPath(nil))
	assert.Empty(t, ThumbfastMPVPath([]string{"other=1"}))
	assert.Equal(t, "/a/mpv", ThumbfastMPVPath([]string{"thumbfast-mpv_path=/a/mpv"}))
	assert.Equal(t, "/b/mpv=x", ThumbfastMPVPath([]string{"thumbfast-mpv_path=/a/mpv", "x=1", "thumbfast-mpv_path=/b/mpv=x"}),
		"最后一条为准；值里的 = 原样保留")
}

// 嵌入模式点名加载的路径要真在界面配置里。
func TestThumbfastScriptIsEmbedded(t *testing.T) {
	info, err := fs.Stat(uiFiles, "ui/"+ThumbfastScript)
	require.NoError(t, err)
	assert.False(t, info.IsDir())
}
