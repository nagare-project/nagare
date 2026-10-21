package player

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/nagare-project/nagare/internal/mpv"
	"github.com/nagare-project/nagare/internal/store"
)

// 播放器界面（mpv 里的 nagare 控制条，见 internal/mpv/ui）与播放管理器之间的消息。
//
// 界面 → nagare：ready（脚本加载好了，要一次状态）、anime4k <off|fast|hq>、danmaku <shown|hidden>、
// volume <0–130>、screenshot。nagare → 界面：uiState（起播、弹幕编排完、画质增强改了的时候各推一次）
// 与屏幕提示（截图的结果）。

// uiVolumeMax 是界面能记下的最大音量（mpv 默认 volume-max）。
const uiVolumeMax = 130

// uiState 是推给界面的状态，字段名与 ui/scripts/nagare/main.lua 的 apply_state 对应。
type uiState struct {
	Title   string `json:"title"`
	Info    string `json:"info"`
	Anime4K string `json:"anime4k"`
}

// sourceLabel 是副标题里「从哪来的」那一段。
func sourceLabel(src MediaSource) string {
	if _, online := src.(*remoteSource); online {
		return "在线播放"
	}
	if isNetworkStream(src.MPVPath()) {
		return "磁力边下边播"
	}
	return "本地媒体库"
}

// buildUIState 拼出界面顶栏的标题与副标题：匹配到作品后用作品名，否则用文件名解析出的标题。
func buildUIState(sess *session, cfg store.PlayerConfig) uiState {
	binding := sess.bindingSnapshot()
	item := sess.item
	title := binding.Title
	if title == "" && item.ParsedTitle != nil {
		title = *item.ParsedTitle
	}
	if title == "" {
		title = item.FileName
	}
	episode := binding.Episode
	if episode <= 0 && item.Episode != nil {
		episode = *item.Episode
	}
	info := sourceLabel(sess.src)
	if episode > 0 {
		info = fmt.Sprintf("第 %d 集 · %s", episode, info)
	}
	preset := cfg.Anime4K
	if preset == mpv.Anime4KOff {
		preset = "off"
	}
	return uiState{Title: title, Info: info, Anime4K: preset}
}

// pushUIState 把当前状态推给这个会话的界面。界面脚本还没加载好时这条会丢，它加载好会发 ready 再要。
func (m *Manager) pushUIState(sess *session) {
	if sess == nil || sess.player == nil {
		return
	}
	if err := sess.player.SendUIState(buildUIState(sess, m.opts.Store.PlayerConfig())); err != nil {
		log.Printf("player: 推送播放器界面状态失败：%v", err)
	}
}

// handleUIMessage 处理界面发来的一条消息。在会话的事件循环里同步调用（可以发 IPC 命令）。
func (m *Manager) handleUIMessage(sess *session, args []string) {
	if len(args) == 0 {
		return
	}
	switch args[0] {
	case "ready":
		m.pushUIState(sess)
	case "anime4k":
		if len(args) < 2 {
			return
		}
		preset := args[1]
		if preset == "off" {
			preset = mpv.Anime4KOff
		}
		if !mpv.ValidAnime4K(preset) {
			return
		}
		if _, err := m.opts.Store.UpdatePlayerConfig(func(c *store.PlayerConfig) { c.Anime4K = preset }); err != nil {
			log.Printf("player: 保存画质增强设置失败：%v", err)
			return
		}
		if sess != nil && sess.player != nil {
			if err := sess.player.SetShaders(m.shaders()); err != nil {
				log.Printf("player: 画质增强没能套到正在播放的窗口上：%v", err)
			}
		}
		m.pushUIState(sess)
	case "screenshot":
		m.takeScreenshot(sess)
	case "danmaku":
		if len(args) < 2 {
			return
		}
		hidden := args[1] == "hidden"
		if _, err := m.opts.Store.UpdatePlayerConfig(func(c *store.PlayerConfig) { c.DanmakuHidden = hidden }); err != nil {
			log.Printf("player: 保存弹幕开关失败：%v", err)
		}
	case "volume":
		if len(args) < 2 {
			return
		}
		v, err := strconv.Atoi(args[1])
		if err != nil {
			return
		}
		v = max(0, min(uiVolumeMax, v))
		if _, err := m.opts.Store.UpdatePlayerConfig(func(c *store.PlayerConfig) { c.Volume = &v }); err != nil {
			log.Printf("player: 保存音量失败：%v", err)
		}
	}
}

// uiLaunchOptions 是起播时与界面有关的两个参数：配置目录、上次的音量。
// 界面配置准备失败不阻断播放：mpv 退回它自带的控制条。
func (m *Manager) uiLaunchOptions() (configDir string, volume *int) {
	if m.opts.UIConfigDir != nil {
		dir, err := m.opts.UIConfigDir()
		if err != nil {
			log.Printf("player: 播放器界面没准备好（这次用 mpv 自带的控制条）：%v", err)
		} else {
			configDir = dir
		}
	}
	return configDir, m.opts.Store.PlayerConfig().Volume
}

// screenshotName 是截图的文件名：作品名 + 播放位置 + 截下的时刻（带到毫秒，连按也不会互相覆盖）。
func screenshotName(title string, pos float64, now time.Time) string {
	clean := strings.Map(func(r rune) rune {
		if strings.ContainsRune(`\/:*?"<>|`, r) || r < 0x20 {
			return '_'
		}
		return r
	}, strings.TrimSpace(title))
	if clean == "" {
		clean = "nagare"
	}
	t := max(0, int(pos))
	return fmt.Sprintf("%s %02d-%02d-%02d %s.png", clean, t/3600, t%3600/60, t%60, now.Format("20060102-150405.000"))
}

// takeScreenshot 截一张带字幕与弹幕的图放进截图目录，结果作为屏幕提示推回界面。
func (m *Manager) takeScreenshot(sess *session) {
	if sess == nil || sess.player == nil {
		return
	}
	dir := m.opts.ScreenshotDir
	toast := func(text, sub string) {
		if err := sess.player.SendUIToast(text, sub); err != nil {
			log.Printf("player: 推送屏幕提示失败：%v", err)
		}
	}
	if dir == "" {
		if _, err := sess.player.Command("screenshot", "subtitles"); err != nil {
			toast("截图失败", err.Error())
			return
		}
		toast("已截图", "")
		return
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		toast("截图失败", "建不了截图目录："+dir)
		return
	}
	path := filepath.Join(dir, screenshotName(buildUIState(sess, store.PlayerConfig{}).Title, sess.player.State().TimePos, time.Now()))
	if _, err := sess.player.Command("screenshot-to-file", path, "subtitles"); err != nil {
		toast("截图失败", err.Error())
		return
	}
	toast("已截图", dir)
}
