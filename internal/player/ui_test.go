package player

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nagare-project/nagare/internal/library"
	"github.com/nagare-project/nagare/internal/mpv"
	"github.com/nagare-project/nagare/internal/store"
)

// uiStreamSource 是一条磁力流的最小替身：只有 MPVPath 是 HTTP 地址这一点要紧。
type uiStreamSource struct{ item library.Item }

func (s uiStreamSource) Item() library.Item                      { return s.item }
func (s uiStreamSource) MPVPath() string                         { return "http://127.0.0.1:7777/stream/x/t/abc/0" }
func (s uiStreamSource) Probe(context.Context) error             { return nil }
func (s uiStreamSource) Hash16M(context.Context) (string, error) { return "", nil }

func strPtr(s string) *string { return &s }
func intPtr(v int) *int       { return &v }

func TestBuildUIStateTitleAndInfo(t *testing.T) {
	item := library.Item{FileName: "[Sub] Frieren - 11 [1080p].mkv", ParsedTitle: strPtr("Frieren"), Episode: intPtr(11)}
	tests := []struct {
		name    string
		src     MediaSource
		binding store.Binding
		cfg     store.PlayerConfig
		want    uiState
	}{
		{
			name: "本地文件，还没匹配到作品：用文件名解析出的标题",
			src:  NewLocalSource(item),
			want: uiState{Title: "Frieren", Info: "第 11 集 · 本地媒体库", Anime4K: "off"},
		},
		{
			name:    "匹配到作品之后：用作品名与作品里的集号",
			src:     NewLocalSource(item),
			binding: store.Binding{Title: "葬送的芙莉莲", Episode: 11},
			cfg:     store.PlayerConfig{Anime4K: mpv.Anime4KHQ},
			want:    uiState{Title: "葬送的芙莉莲", Info: "第 11 集 · 本地媒体库", Anime4K: "hq"},
		},
		{
			name: "磁力流",
			src:  uiStreamSource{item: item},
			want: uiState{Title: "Frieren", Info: "第 11 集 · 磁力边下边播", Anime4K: "off"},
		},
		{
			name: "解析不出集号：只写来源",
			src:  NewLocalSource(library.Item{FileName: "movie.mkv"}),
			want: uiState{Title: "movie.mkv", Info: "本地媒体库", Anime4K: "off"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sess := &session{src: tc.src, item: tc.src.Item(), binding: tc.binding}
			assert.Equal(t, tc.want, buildUIState(sess, tc.cfg))
		})
	}
}

func TestHandleUIMessageUpdatesPlayerConfig(t *testing.T) {
	m, st, _ := newTestManager(t, nil)
	sess := &session{src: NewLocalSource(library.Item{FileName: "a.mkv"}), item: library.Item{FileName: "a.mkv"}}

	m.handleUIMessage(sess, []string{"anime4k", "hq"})
	assert.Equal(t, mpv.Anime4KHQ, st.PlayerConfig().Anime4K)
	m.handleUIMessage(sess, []string{"anime4k", "off"})
	assert.Equal(t, mpv.Anime4KOff, st.PlayerConfig().Anime4K)
	m.handleUIMessage(sess, []string{"anime4k", "ultra"}) // 不认识的档位不动设置
	assert.Equal(t, mpv.Anime4KOff, st.PlayerConfig().Anime4K)

	m.handleUIMessage(sess, []string{"danmaku", "hidden"})
	assert.True(t, st.PlayerConfig().DanmakuHidden)
	m.handleUIMessage(sess, []string{"danmaku", "shown"})
	assert.False(t, st.PlayerConfig().DanmakuHidden)

	m.handleUIMessage(sess, []string{"volume", "70"})
	require.NotNil(t, st.PlayerConfig().Volume)
	assert.Equal(t, 70, *st.PlayerConfig().Volume)
	m.handleUIMessage(sess, []string{"volume", "999"}) // 夹到上限
	assert.Equal(t, uiVolumeMax, *st.PlayerConfig().Volume)
	m.handleUIMessage(sess, []string{"volume", "loud"}) // 不是数字就不动
	assert.Equal(t, uiVolumeMax, *st.PlayerConfig().Volume)

	// 没有播放器的会话（测试里）、空消息、不认识的消息：都不该崩
	m.handleUIMessage(sess, nil)
	m.handleUIMessage(sess, []string{"ready"})
	m.handleUIMessage(sess, []string{"screenshot"})
	m.handleUIMessage(sess, []string{"whatever"})
}

func TestUILaunchOptionsCarryConfigDirAndVolume(t *testing.T) {
	m, st, dir := newTestManager(t, nil)
	configDir, noConfig, volume := m.uiLaunchOptions()
	assert.Empty(t, configDir, "没配界面目录时不带 --config-dir")
	assert.False(t, noConfig)
	assert.Nil(t, volume, "从没调过音量时用 mpv 默认")

	m.opts.UIConfigDir = func() (string, error) { return mpv.PrepareUIConfig(dir) }
	_, err := st.UpdatePlayerConfig(func(c *store.PlayerConfig) { v := 55; c.Volume = &v })
	require.NoError(t, err)
	configDir, noConfig, volume = m.uiLaunchOptions()
	assert.Equal(t, filepath.Join(dir, "mpv-config"), configDir)
	assert.False(t, noConfig)
	require.NotNil(t, volume)
	assert.Equal(t, 55, *volume)

	// 界面配置写不出来：退回 mpv 自带的控制条，但也不读用户自己的配置
	m.opts.UIConfigDir = func() (string, error) { return "", errors.New("disk full") }
	configDir, noConfig, _ = m.uiLaunchOptions()
	assert.Empty(t, configDir)
	assert.True(t, noConfig)
}

func TestScreenshotName(t *testing.T) {
	at := time.Date(2026, 10, 22, 21, 5, 9, 123_000_000, time.Local)
	assert.Equal(t, "葬送的芙莉莲 00-12-03 20261022-210509.123.jpg", screenshotName("葬送的芙莉莲", 723.9, at))
	assert.Equal(t, "a_b_c_ 01-00-00 20261022-210509.123.jpg", screenshotName(`a/b:c?`, 3600, at), "文件名里不能有路径分隔符等字符")
	assert.Equal(t, "nagare 00-00-00 20261022-210509.123.jpg", screenshotName("  ", -5, at))
	long := screenshotName(strings.Repeat("长", 200), 0, at)
	assert.LessOrEqual(t, len(long), 255, "文件名不能超过 255 字节")
}
