package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nagare-project/nagare/internal/tray"
)

// 首次启动弹一次，之后不再弹（标记文件落在配置目录）。
func TestBackgroundNoticeOnlyOnce(t *testing.T) {
	dir := t.TempDir()

	first := backgroundNotice(dir, tray.ModeTray)
	require.NotNil(t, first, "首次启动应有通知")
	assert.Contains(t, first.Body, "托盘")
	_, err := os.Stat(filepath.Join(dir, noticeMarker))
	require.NoError(t, err, "应写下标记")

	assert.Nil(t, backgroundNotice(dir, tray.ModeTray), "第二次启动不应再弹")
}

// 没有图标可指（--no-tray / API-only / 桩）时不弹，也不写标记：
// 用户以后换成有图标的形态时仍应得到那一次提示。
func TestBackgroundNoticeSkipsWhenNoIcon(t *testing.T) {
	dir := t.TempDir()
	assert.Nil(t, backgroundNotice(dir, tray.ModeNone))
	_, err := os.Stat(filepath.Join(dir, noticeMarker))
	assert.True(t, os.IsNotExist(err), "无图标形态不应写标记")
}

// 每种形态的文案都要告诉用户「去哪里找、怎么退出」。
func TestNoticeBodyMentionsWhereToFindIt(t *testing.T) {
	cases := map[tray.Mode]string{
		tray.ModeDock:    "Dock",
		tray.ModeMenuBar: "菜单栏",
		tray.ModeTray:    "托盘",
		tray.ModeNone:    "设置页",
	}
	for mode, want := range cases {
		assert.Contains(t, noticeBody(mode), want, "mode=%s", mode)
		assert.Contains(t, noticeBody(mode), "不会退出", "mode=%s", mode)
	}
}

func TestBackgroundModeFlags(t *testing.T) {
	assert.Equal(t, tray.ModeNone, backgroundMode(true, false), "--no-tray")
	assert.Equal(t, tray.ModeNone, backgroundMode(false, true), "API-only 没有界面可开")
	assert.Equal(t, tray.CurrentMode(), backgroundMode(false, false))
}
