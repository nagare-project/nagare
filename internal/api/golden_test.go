package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/nagare-project/nagare/internal/apigolden"
	"github.com/nagare-project/nagare/internal/mpv"
	"github.com/nagare-project/nagare/internal/player"
	"github.com/nagare-project/nagare/internal/store"
)

// TestGolden 把各端点的真实响应与仓库根 testdata/api/*.json 逐字节比对（-update 时重写）。
// 每个客户端都严格解码这些文件，见 docs/api-goldens.md。
func TestGolden(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("黄金响应按 POSIX 路径与报错措辞生成（CI 在 linux 上比对）")
	}
	var cases []goldenCase
	cases = append(cases, libraryGoldenCases()...)
	cases = append(cases, systemGoldenCases()...)
	cases = append(cases, catalogGoldenCases()...)
	names := make([]string, 0, len(cases))
	for _, c := range cases {
		require.NotContains(t, names, c.name, "黄金用例重名")
		names = append(names, c.name)
		t.Run(c.name, func(t *testing.T) {
			body := c.run(t)
			// 替身里的会话凭证是 placeholder-*：哪天某个处理器开始回显凭证，-update 不能把它烤进黄金文件
			require.NotContains(t, string(body), "placeholder-", "响应里出现了会话凭证")
			apigolden.Assert(t, c.name, body)
		})
	}
	apigolden.AssertOwned(t, ownedByAPI, names)
}

// ownedByAPI：testdata/api 里除了桌面版专属与自更新两类，其余都归 internal/api。
func ownedByAPI(name string) bool {
	return !strings.HasPrefix(name, apigolden.PrefixDesktop) && !strings.HasPrefix(name, apigolden.PrefixUpdate)
}

type goldenCase struct {
	name string
	run  func(t *testing.T) []byte
}

// 设置、mpv、播放器、账号的黄金用例。
func systemGoldenCases() []goldenCase {
	return []goldenCase{
		{"settings", func(t *testing.T) []byte {
			return newGoldenEnv(t, goldenOptions{}).call(t, http.StatusOK, http.MethodGet, "/api/settings", "")
		}},
		{"settings-desktop-window", func(t *testing.T) []byte {
			env := newGoldenEnv(t, goldenOptions{deps: func(d *Deps) {
				d.EmbeddedMPV = true
				d.Desktop = func() DesktopView { return DesktopView{Window: true} }
				d.BackgroundMode = "window"
				d.DataDir = "/Users/viewer/Library/Application Support/nagare"
				d.LogPath = "/Users/viewer/Library/Application Support/nagare/logs/nagare.log"
				d.MPV = mpv.NewRuntimeWith(func(string) (mpv.Info, error) {
					return mpv.Info{Path: "/Applications/Nagare.app/Contents/MacOS/mpv", Version: "0.41.0", Source: mpv.SourceBundled}, nil
				}, "")
			}})
			setGoldenPlatform(t, "darwin", "arm64")
			env.auth.loggedIn = false
			require.NoError(t, env.store.SetAnimegoSession(store.AnimegoSession{}))
			_, err := env.store.UpdatePlayerConfig(func(c *store.PlayerConfig) { c.Anime4K = mpv.Anime4KFast })
			require.NoError(t, err)
			return env.call(t, http.StatusOK, http.MethodGet, "/api/settings", "")
		}},
		{"settings-desktop-fallback", func(t *testing.T) []byte {
			env := newGoldenEnv(t, goldenOptions{deps: func(d *Deps) {
				d.Desktop = func() DesktopView {
					return DesktopView{Fallback: "没有找到 WebView2 运行时，界面改在浏览器里打开；装好运行时后重启 nagare 即可回到桌面窗口"}
				}
				d.BackgroundMode = ""
			}})
			setGoldenPlatform(t, "windows", "amd64")
			return env.call(t, http.StatusOK, http.MethodGet, "/api/settings", "")
		}},
		{"mpv-detect-found", func(t *testing.T) []byte {
			return newGoldenEnv(t, goldenOptions{}).call(t, http.StatusOK, http.MethodPost, "/api/mpv/detect", "")
		}},
		{"mpv-detect-missing", func(t *testing.T) []byte {
			env := newGoldenEnv(t, goldenOptions{deps: func(d *Deps) {
				d.MPV = mpv.NewRuntimeWith(func(string) (mpv.Info, error) {
					return mpv.Info{}, errors.New("没有找到 mpv：PATH 与常见安装位置里都没有")
				}, "")
			}})
			return env.call(t, http.StatusOK, http.MethodPost, "/api/mpv/detect", "")
		}},
		{"play", func(t *testing.T) []byte {
			env := newGoldenLibrary(t)
			env.player.playRes = player.PlayResult{
				Title:      "葬送的芙莉莲 第 2 集",
				Danmaku:    player.DanmakuInfo{State: "unmatched", Reason: "没有在弹幕库里认出这一集，可以在播放器里手动选集"},
				PlaybackID: "pb-0001",
			}
			id := env.firstFile(t, "Frieren - 02")
			return env.call(t, http.StatusOK, http.MethodPost, "/api/play", `{"fileId":`+jsonString(t, id)+`}`)
		}},
		{"player-status-idle", func(t *testing.T) []byte {
			return newGoldenEnv(t, goldenOptions{}).call(t, http.StatusOK, http.MethodGet, "/api/player/status", "")
		}},
		{"player-status-playing", func(t *testing.T) []byte {
			env := newGoldenEnv(t, goldenOptions{})
			env.player.status = player.Status{
				Playing: true, FileID: "[Sub] Frieren - 02 [1080p].mkv|3145728|1788249600000", Title: "葬送的芙莉莲 第 2 集",
				Position: 312.5, Duration: 1420.04, Danmaku: &player.DanmakuInfo{State: "ok", Count: 1834},
			}
			return env.call(t, http.StatusOK, http.MethodGet, "/api/player/status", "")
		}},
		{"player-status-paused-with-failures", func(t *testing.T) []byte {
			env := newGoldenEnv(t, goldenOptions{})
			env.player.status = player.Status{
				Playing: true, FileID: "[Sub] Frieren - 02 [1080p].mkv|3145728|1788249600000", Title: "葬送的芙莉莲 第 2 集",
				Position: 1200, Duration: 1420.04, Paused: true,
				Danmaku:         &player.DanmakuInfo{State: "degraded", Count: 120, Reason: "弹幕只取回了一部分，稍后重新打开这一集会再试"},
				PlaybackFailure: goldenPlaybackFailure(),
				Sync:            goldenSyncFailure(),
			}
			return env.call(t, http.StatusOK, http.MethodGet, "/api/player/status", "")
		}},
		{"player-status-idle-with-failures", func(t *testing.T) []byte {
			env := newGoldenEnv(t, goldenOptions{})
			env.player.status = player.Status{PlaybackFailure: goldenPlaybackFailure(), Sync: goldenSyncFailure()}
			return env.call(t, http.StatusOK, http.MethodGet, "/api/player/status", "")
		}},
		{"player-stop", func(t *testing.T) []byte {
			return newGoldenEnv(t, goldenOptions{}).call(t, http.StatusOK, http.MethodPost, "/api/player/stop", "")
		}},
		{"player-config", func(t *testing.T) []byte {
			env := newGoldenEnv(t, goldenOptions{})
			env.player.playing = true
			return env.call(t, http.StatusOK, http.MethodPost, "/api/player/config", `{"anime4k":"hq"}`)
		}},
		{"player-config-apply-error", func(t *testing.T) []byte {
			env := newGoldenEnv(t, goldenOptions{})
			env.player.playing, env.player.refreshErr = true, errors.New("set glsl-shaders: property unavailable")
			return env.call(t, http.StatusOK, http.MethodPost, "/api/player/config", `{"anime4k":"off"}`)
		}},
		{"auth-login", func(t *testing.T) []byte {
			env := newGoldenEnv(t, goldenOptions{})
			env.auth.loggedIn = false
			return env.call(t, http.StatusOK, http.MethodPost, "/api/animego/login", `{"email":"viewer@example.test","password":"placeholder"}`)
		}},
		{"auth-logout", func(t *testing.T) []byte {
			return newGoldenEnv(t, goldenOptions{}).call(t, http.StatusOK, http.MethodPost, "/api/animego/logout", "")
		}},
	}
}

func goldenPlaybackFailure() *player.PlaybackFailure {
	return &player.PlaybackFailure{
		FileID: "[Sub] Frieren - 01 [1080p].mkv|3145728|1788249600000", PlaybackID: "pb-0001",
		Reason: "mpv 打不开这个文件：文件可能已损坏或不完整", At: goldenNow.UnixMilli(),
	}
}

func goldenSyncFailure() *player.SyncInfo {
	return &player.SyncInfo{
		State: "failed", FileID: "[Sub] Frieren - 01 [1080p].mkv|3145728|1788249600000", Title: "葬送的芙莉莲", Episode: 1,
		Reason: "账号服务暂时连不上，这一集没有记成看完", Recovery: "重看这一集并看完时会再试一次", At: goldenNow.UnixMilli(),
	}
}

// firstFile 按文件名里的一段文字找 fileId。
func (e *goldenEnv) firstFile(t *testing.T, namePart string) string {
	t.Helper()
	for _, c := range e.lib.View().Clusters {
		for _, g := range c.Groups {
			for _, it := range g.Items {
				if strings.Contains(it.FileName, namePart) {
					return it.FileID
				}
			}
		}
	}
	t.Fatalf("媒体库里没有文件名含「%s」的文件", namePart)
	return ""
}

func (e *goldenEnv) scrubBytes(body []byte) []byte { return apigolden.Scrub(body, e.scrub...) }

func jsonString(t *testing.T, s string) string {
	t.Helper()
	b, err := json.Marshal(s)
	require.NoError(t, err)
	return string(b)
}

// replaceNumber 把响应体里 "key":old 换成 "key":new（old 必须正好出现一次）。
func replaceNumber(t *testing.T, body, key string, old, new int64) string {
	t.Helper()
	from := `"` + key + `":` + strconv.FormatInt(old, 10)
	require.Equal(t, 1, strings.Count(body, from), "响应体里应正好有一处 %s：%s", from, body)
	return strings.Replace(body, from, `"`+key+`":`+strconv.FormatInt(new, 10), 1)
}

func containsFold(s, part string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(part))
}
