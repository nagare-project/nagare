package sources

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/nagare-project/nagare/internal/api"
	"github.com/nagare-project/nagare/internal/apigolden"
	"github.com/nagare-project/nagare/internal/mpv"
	"github.com/nagare-project/nagare/internal/player"
	"github.com/nagare-project/nagare/internal/sourceplugin"
	"github.com/nagare-project/nagare/internal/store"
	"github.com/nagare-project/nagare/internal/torrentstream"
)

// TestGolden 把桌面版专属端点（磁力 / 来源插件 / 规则）的真实响应与 testdata/api/desktop-*.json
// 逐字节比对（-update 时重写），见 docs/api-goldens.md。移动端客户端跳过 desktop-* 这一类。
func TestGolden(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("黄金响应在 linux 上比对（与 internal/api 一致）")
	}
	cases := []struct {
		name string
		run  func(t *testing.T) []byte
	}{
		{"desktop-settings", goldenDesktopSettings},
		{"desktop-torrent-status-idle", func(t *testing.T) []byte {
			env := newEnv(t)
			env.torrent.status = torrentstream.Status{Phase: "idle"}
			return goldenCall(t, env, http.StatusOK, http.MethodGet, "/api/torrent/status", "")
		}},
		{"desktop-torrent-status-active", func(t *testing.T) []byte {
			env := newEnv(t)
			env.torrent.status = torrentstream.Status{
				Active: true, Phase: "buffering", Name: "[Sub] Frieren - 01-28 [1080p]", FileName: "[Sub] Frieren - 02 [1080p].mkv",
				Infohash: "0123456789abcdef0123456789abcdef01234567", Peers: 12, Seeders: 9, DownRate: 2_400_000, UpRate: 48_000,
				Buffered: 0.625, Progress: 0.031, CacheBytes: 104_857_600, Seeding: true, Error: "分片写盘失败：磁盘已满",
			}
			return goldenCall(t, env, http.StatusOK, http.MethodGet, "/api/torrent/status", "")
		}},
		{"desktop-torrent-play-select", func(t *testing.T) []byte {
			env := newEnv(t)
			env.torrent.prepareRes = torrentstream.PrepareResult{NeedSelection: true, Files: []torrentstream.FileChoice{
				{Index: 0, Name: "[Sub] Frieren - 01 [1080p].mkv", Path: "Frieren/01.mkv", Size: 1 << 30, Episode: intPtr(1)},
				{Index: 1, Name: "NCOP.mkv", Path: "Frieren/Extras/NCOP.mkv", Size: 1 << 26},
			}}
			return goldenCall(t, env, http.StatusOK, http.MethodPost, "/api/torrent/play", `{"magnet":"magnet:?xt=urn:btih:abc","title":"葬送的芙莉莲"}`)
		}},
		{"desktop-torrent-play-started", func(t *testing.T) []byte {
			env := newEnv(t)
			env.torrent.prepareRes = torrentstream.PrepareResult{Source: &torrentstream.Source{}}
			env.player.playRes = player.PlayResult{
				FileID: "torrent:0123456789abcdef0123456789abcdef01234567:1", Title: "葬送的芙莉莲 第 2 集",
				Danmaku: player.DanmakuInfo{State: "loading"}, PlaybackID: "pb-0002",
			}
			return goldenCall(t, env, http.StatusOK, http.MethodPost, "/api/torrent/play", `{"magnet":"magnet:?xt=urn:btih:abc","title":"葬送的芙莉莲","fileIndex":1}`)
		}},
		{"desktop-torrent-config", func(t *testing.T) []byte {
			env := newEnv(t)
			return goldenCall(t, env, http.StatusOK, http.MethodPost, "/api/torrent/config", `{"seeding":true,"trackers":["udp://tracker.example.test:6969/announce"]}`)
		}},
		{"desktop-torrent-unavailable", func(t *testing.T) []byte {
			return goldenCall(t, newEnvNoTorrent(t), http.StatusServiceUnavailable, http.MethodGet, "/api/torrent/status", "")
		}},
		{"desktop-downloads", func(t *testing.T) []byte {
			env := newEnv(t)
			env.downloads.list = goldenDownloads()
			return goldenCall(t, env, http.StatusOK, http.MethodGet, "/api/downloads", "")
		}},
		{"desktop-source-plugin", func(t *testing.T) []byte {
			return goldenCall(t, newEnv(t), http.StatusOK, http.MethodGet, "/api/source-plugin", "")
		}},
		{"desktop-source-plugin-ready", func(t *testing.T) []byte {
			env := goldenPluginEnv(t)
			return goldenCall(t, env, http.StatusOK, http.MethodGet, "/api/source-plugin", "")
		}},
		{"desktop-source-plugin-sources-error", func(t *testing.T) []byte {
			env := goldenPluginEnv(t)
			env.plugin.sourcesErr = errors.New("connection reset")
			return goldenCall(t, env, http.StatusOK, http.MethodGet, "/api/source-plugin", "")
		}},
		{"desktop-source-plugin-failed", func(t *testing.T) []byte {
			env := newEnv(t)
			env.plugin.status = sourceplugin.Status{Phase: "failed", Error: "插件进程启动后 10 秒内没有报告监听地址"}
			return goldenCall(t, env, http.StatusOK, http.MethodGet, "/api/source-plugin", "")
		}},
		{"desktop-sources-empty", func(t *testing.T) []byte {
			env := newEnv(t)
			body := goldenCall(t, env, http.StatusOK, http.MethodGet, "/api/sources", "")
			return apigolden.Scrub(body, env.sources.activeDir(), "/home/viewer/.config/nagare/rules")
		}},
		{"desktop-sources", goldenSourcesLoaded},
	}
	names := make([]string, 0, len(cases))
	for _, c := range cases {
		names = append(names, c.name)
		t.Run(c.name, func(t *testing.T) { apigolden.Assert(t, c.name, c.run(t)) })
	}
	apigolden.AssertOwned(t, func(name string) bool { return strings.HasPrefix(name, apigolden.PrefixDesktop) }, names)
}

// goldenPluginEnv 是插件已就绪、用的是安装包捆绑那一份的环境。
func goldenPluginEnv(t *testing.T) *testEnv {
	t.Helper()
	env := newEnv(t)
	const exe, root = "/opt/nagare/plugin/nagare-source", "/opt/nagare/plugin/repo"
	require.NoError(t, env.store.SetSourcePluginConfig(store.SourcePluginConfig{Enabled: true, Executable: exe, Root: root}))
	env.pluginSvc.SetBundled(sourceplugin.Bundled{Executable: exe, Root: root}, true)
	started := int64(1792065000000)
	env.plugin.status = sourceplugin.Status{
		Phase: "ready", URL: "http://127.0.0.1:43123", StartedAt: &started,
		Manifest: &sourceplugin.Manifest{
			ID: "org.example.source", Name: "Example Source", Version: "0.2.0",
			ProtocolVersions: []int{1}, SourceSchemaVersions: []int{1}, Capabilities: []string{"candidates", "bt_releases"},
		},
	}
	env.plugin.sources = []sourceplugin.Source{{
		ID: "example-bt", Name: "Example BT", Kind: "bt", Tier: 2, Version: "1", Enabled: true,
		Status: "healthy", Capabilities: []string{"search", "torrent"},
	}}
	return env
}

// goldenSourcesLoaded 配好本机规则目录、加载一条规则之后的源列表。
func goldenSourcesLoaded(t *testing.T) []byte {
	env := newEnv(t)
	_, dir := fakeSource(t)
	goldenCall(t, env, http.StatusOK, http.MethodPost, "/api/sources/config", `{"localDir":`+string(mustJSON(t, dir))+`}`)
	rec := env.do(t, http.MethodGet, "/api/sources", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var view SourcesView
	require.NoError(t, json.Unmarshal(decode(t, rec).Data, &view))
	require.NotNil(t, view.Rules.LastLoadedAt)
	// 加载时间是当前时间（规则服务没有时钟注入点）：换成固定值
	loadedAt := []byte(`"lastLoadedAt":` + strconv.FormatInt(*view.Rules.LastLoadedAt, 10))
	require.Equal(t, 1, bytes.Count(rec.Body.Bytes(), loadedAt))
	body := bytes.Replace(rec.Body.Bytes(), loadedAt, []byte(`"lastLoadedAt":1792065600000`), 1)
	return apigolden.Scrub(body, dir, "/home/viewer/anime-rules")
}

func goldenCall(t *testing.T, env *testEnv, wantStatus int, method, target, body string) []byte {
	t.Helper()
	rec := env.do(t, method, target, body)
	require.Equal(t, wantStatus, rec.Code, rec.Body.String())
	return rec.Body.Bytes()
}

// goldenDesktopSettings 是桌面版的设置载荷：多了 torrent 一项（经 api.Deps.TorrentSettings 接进来）。
func goldenDesktopSettings(t *testing.T) []byte {
	env := newEnv(t)
	env.torrent.cacheBytes = 52_428_800
	h := New(Deps{
		Store: env.store, Player: env.player, Sources: env.sources, Torrent: env.torrent, Downloads: env.downloads,
		TorrentCacheDir: "/home/viewer/.config/nagare/cache/torrent", DefaultDownloadDir: "/home/viewer/Downloads/nagare",
	})
	core := api.New(api.Deps{
		Store: env.store, Lib: api.NewLibraryService(env.store), Player: env.player, Auth: fakeAuth{},
		AnimegoBaseURL: "https://catalog.example.test", Version: "0.7.0", TorrentSettings: h.TorrentSettings,
		MPV: mpv.NewRuntimeWith(func(string) (mpv.Info, error) {
			return mpv.Info{Path: "/usr/bin/mpv", Version: "0.41.0", Source: mpv.SourcePath}, nil
		}, ""),
		DataDir: "/home/viewer/.config/nagare", LogPath: "/home/viewer/.config/nagare/logs/nagare.log", BackgroundMode: "tray",
	})
	mux := http.NewServeMux()
	core.Register(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/settings", nil))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	// 平台由 internal/api 按运行的机器报，这个包里没有注入点：换成黄金文件里的 linux/amd64
	body := bytes.Replace(rec.Body.Bytes(), []byte(`"platform":"`+runtime.GOOS+`"`), []byte(`"platform":"linux"`), 1)
	return bytes.Replace(body, []byte(`"arch":"`+runtime.GOARCH+`"`), []byte(`"arch":"amd64"`), 1)
}

func goldenDownloads() []torrentstream.DownloadView {
	const added = 1792051200000
	return []torrentstream.DownloadView{
		{Download: store.Download{
			ID: "0123456789abcdef0123456789abcdef01234567", Magnet: "magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567",
			Title: "[Sub] Frieren - 01-28 [1080p]", Name: "Frieren S01", Root: "/home/viewer/Downloads/nagare",
			State: store.DownloadDownloading, Size: 30 << 30, AddedAt: added,
		}, BytesDone: 3 << 30, Peers: 14, Seeders: 11, DownRate: 3_200_000},
		{Download: store.Download{
			ID: "89abcdef0123456789abcdef0123456789abcdef", TorrentURL: "https://releases.example.test/bocchi.torrent",
			Title: "[ANi] Bocchi the Rock! - 01", Name: "[ANi] Bocchi the Rock! - 01.mp4", Root: "/home/viewer/Downloads/nagare",
			State: store.DownloadDone, Size: 600 << 20, Paths: []string{"/home/viewer/Downloads/nagare/[ANi] Bocchi the Rock! - 01.mp4"},
			AddedAt: added - 3_600_000, CompletedAt: added - 1_800_000,
		}, BytesDone: 600 << 20},
		{Download: store.Download{
			ID: "fedcba9876543210fedcba9876543210fedcba98", Magnet: "magnet:?xt=urn:btih:fedcba9876543210fedcba9876543210fedcba98",
			Title: "没人分享的老番", Root: "/home/viewer/Downloads/nagare", State: store.DownloadFailed,
			Error: "等了 10 分钟仍然没有找到分享者", AddedAt: added - 7_200_000,
		}},
	}
}

func intPtr(v int) *int { return &v }
