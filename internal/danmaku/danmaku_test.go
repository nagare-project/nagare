package danmaku

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOptionsWithDefaults(t *testing.T) {
	d := Options{}.withDefaults()
	assert.Equal(t, "PingFang SC", d.FontName)
	assert.Equal(t, 48, d.FontSize)
	assert.Equal(t, 12.0, d.ScrollDuration)
	assert.Equal(t, 5.0, d.StillDuration)
	assert.Equal(t, 0.75, d.Opacity)
	assert.Equal(t, 0.72, d.DisplayRatio)

	odd := Options{FontName: "My,Font", FontSize: -1, Opacity: 3, DisplayRatio: -2}.withDefaults()
	assert.Equal(t, "My Font", odd.FontName, "字体名里的逗号会破坏 Style 行，替换成空格")
	assert.Equal(t, 48, odd.FontSize)
	assert.Equal(t, 1.0, odd.Opacity)
	assert.Equal(t, 0.72, odd.DisplayRatio)
}

func TestConvertEmptyInput(t *testing.T) {
	out, st := Convert(nil, Options{})
	assert.Equal(t, Stats{}, st)
	assert.Contains(t, out, "[Script Info]")
	assert.Contains(t, out, "PlayResX: 1920")
	assert.Contains(t, out, "PlayResY: 1080")
	assert.Contains(t, out, "WrapStyle: 2")
	assert.Contains(t, out, "ScaledBorderAndShadow: yes")
	assert.Contains(t, out, "Style: Danmaku,PingFang SC,48,")
	assert.Contains(t, out, "[Events]")
	assert.Empty(t, dialogueLines(t, out))
}

func TestConvertDeterministicAndSorted(t *testing.T) {
	comments := []Comment{
		{CID: 3, P: "5,1,255", Text: "later-first"}, // 与下一条同刻，但输入序在前
		{CID: 1, P: "1,1,16777215", Text: "early"},
		{CID: 2, P: "5,1,65280", Text: "later-second"},
	}
	out1, st1 := Convert(comments, Options{})
	out2, st2 := Convert(comments, Options{})
	assert.Equal(t, out1, out2, "同输入必须逐字节一致")
	assert.Equal(t, st1, st2)

	lines := dialogueLines(t, out1)
	require.Len(t, lines, 3)
	assert.Contains(t, lines[0], "early", "事件按出现时间升序")
	assert.Contains(t, lines[1], "later-first", "同一时刻保持输入顺序")
	assert.Contains(t, lines[2], "later-second")
}

func TestStatsInvariant(t *testing.T) {
	comments := []Comment{
		{P: "bad"},
		{P: "1,1,255", Text: "ok"},
		{P: "1,1,255", Text: " "},
		{P: "2,5,0", Text: "顶"},
		{P: "x,y,z", Text: "?"},
	}
	_, st := Convert(comments, Options{})
	assert.Equal(t, st.Total, st.Converted+st.Skipped, "恒有 Total = Converted + Skipped")
	assert.GreaterOrEqual(t, st.Converted, st.Overlapped, "Overlapped 含于 Converted")
}

func TestWriteFileAtomic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ep01.ass")
	comments := []Comment{{CID: 1, P: "1,1,16777215", Text: "写盘"}}

	st, err := WriteFile(path, comments, Options{})
	require.NoError(t, err)
	assert.Equal(t, Stats{Total: 1, Converted: 1}, st)

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	expect, _ := Convert(comments, Options{})
	assert.Equal(t, expect, string(data), "落盘内容与 Convert 输出一致")

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, entries, 1, "目录里不残留临时文件")
	assert.Equal(t, "ep01.ass", entries[0].Name())
}

func TestWriteFileCreatesParentDirAndOverwrites(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cache", "danmaku", "ep02.ass")

	_, err := WriteFile(path, []Comment{{CID: 1, P: "1,1,255", Text: "第一版"}}, Options{})
	require.NoError(t, err, "父目录不存在时自动创建")

	// 原子覆盖：第二次写同一路径直接替换，无残留临时文件。
	_, err = WriteFile(path, []Comment{{CID: 2, P: "2,1,255", Text: "第二版"}}, Options{})
	require.NoError(t, err)

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(data), "第二版")
	assert.NotContains(t, string(data), "第一版")

	entries, err := os.ReadDir(filepath.Dir(path))
	require.NoError(t, err)
	require.Len(t, entries, 1, "覆盖写后同样不残留临时文件")
}
