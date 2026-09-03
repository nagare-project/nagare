package player

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	errs "github.com/nagare-project/nagare/internal/errors"
	"github.com/nagare-project/nagare/internal/library"
)

// 本地实现是接缝的回归基准：包装前后管线看到的东西必须一模一样。
func TestLocalSourceMirrorsItem(t *testing.T) {
	dir := t.TempDir()
	item := testItem(t, dir, 7)

	src := NewLocalSource(item)
	assert.Equal(t, item, src.Item(), "Item 应原样返回构造时的条目")
	assert.Equal(t, item.AbsPath, src.MPVPath(), "本地播放交给 mpv 的就是绝对路径")
	require.NoError(t, src.Probe(context.Background()))
}

// 文件被移走/改名是本地库最常见的失效方式：必须是 CategoryFS 的分类错误，
// 且用户看得见是哪个文件、下一步该做什么。
func TestLocalSourceProbeMissingFile(t *testing.T) {
	item := library.Item{FileID: "x", FileName: "消失的一集.mkv", AbsPath: "/不存在/消失的一集.mkv"}

	err := NewLocalSource(item).Probe(context.Background())
	var ce *errs.E
	require.ErrorAs(t, err, &ce)
	assert.Equal(t, errs.CategoryFS, ce.Category)
	assert.Contains(t, ce.UserMsg, "消失的一集.mkv", "提示里要指明是哪个文件")
	assert.Contains(t, ce.UserFacing(), "重新扫描", "要给出可执行的恢复动作")
}

// 接缝不得改变哈希口径：走 MediaSource 与直接调 library.Hash16M 必须同值，
// 否则同一个文件在两条路径下会匹配到不同的 dandanplay 剧集。
func TestLocalSourceHash16MMatchesLibrary(t *testing.T) {
	dir := t.TempDir()
	abs := filepath.Join(dir, "ep.mkv")
	require.NoError(t, os.WriteFile(abs, []byte("nagare 16m hash 一致性"), 0o644))

	want, err := library.Hash16M(abs)
	require.NoError(t, err)

	got, err := NewLocalSource(library.Item{AbsPath: abs}).Hash16M(context.Background())
	require.NoError(t, err)
	assert.Equal(t, want, got)
}
