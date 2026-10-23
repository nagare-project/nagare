package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nagare-project/nagare/internal/mpv"
)

// settingsView 是 GET /api/settings 契约的测试侧映射（前端按同一契约渲染）。
type settingsView struct {
	Version  string `json:"version"`
	Platform string `json:"platform"`
	Arch     string `json:"arch"`
	DataDir  string `json:"dataDir"`
	LogPath  string `json:"logPath"`
	MPV      struct {
		Found    bool   `json:"found"`
		Embedded bool   `json:"embedded"`
		Path     string `json:"path"`
		Version  string `json:"version"`
		Source   string `json:"source"`
		Hint     string `json:"hint"`
		CLI      *struct {
			Path    string `json:"path"`
			Version string `json:"version"`
			Source  string `json:"source"`
		} `json:"cli"`
		Install *struct {
			Command string `json:"command"`
			URL     string `json:"url"`
			Note    string `json:"note"`
		} `json:"install"`
	} `json:"mpv"`
	Animego struct {
		LoggedIn bool   `json:"loggedIn"`
		BaseURL  string `json:"baseUrl"`
	} `json:"animego"`
	Background struct {
		Mode string `json:"mode"`
	} `json:"background"`
	Desktop DesktopView `json:"desktop"`
}

func getSettings(t *testing.T, env *testEnv) settingsView {
	t.Helper()
	rec := env.do(t, http.MethodGet, "/api/settings", "")
	require.Equal(t, http.StatusOK, rec.Code)
	var s settingsView
	require.NoError(t, json.Unmarshal(decode(t, rec).Data, &s))
	return s
}

// settings 形状：平台信息 + 路径 + mpv（找到时无 install）+ animego 登录态。
func TestSettings(t *testing.T) {
	env := newEnv(t)
	s := getSettings(t, env)
	assert.Equal(t, "test", s.Version)
	assert.Equal(t, runtime.GOOS, s.Platform)
	assert.Equal(t, runtime.GOARCH, s.Arch)
	assert.Equal(t, "/data/nagare", s.DataDir)
	assert.Equal(t, "/data/nagare/logs/nagare.log", s.LogPath)
	assert.True(t, s.MPV.Found)
	assert.Equal(t, "/usr/bin/mpv", s.MPV.Path)
	assert.Equal(t, "0.41.0", s.MPV.Version)
	assert.Equal(t, mpv.SourcePath, s.MPV.Source)
	assert.Empty(t, s.MPV.Hint)
	assert.Nil(t, s.MPV.Install, "找到 mpv 时不该带安装指引")
	assert.False(t, s.Animego.LoggedIn)
	assert.Equal(t, "https://example.test", s.Animego.BaseURL)
	assert.Equal(t, "dock", s.Background.Mode, "界面按后台形态写「去哪里找它」的提示")
}

// 旧调用方没填 BackgroundMode 时归一成 none，界面只认五个枚举。
func TestBackgroundModeViewDefaultsToNone(t *testing.T) {
	assert.Equal(t, "none", backgroundModeView(""))
	assert.Equal(t, "tray", backgroundModeView("tray"))
}

// 桌面窗口：background.mode 是 window（界面在窗口里说「关闭窗口」），其余字段与以前一样。
func TestSettingsBackgroundModeForDesktopWindow(t *testing.T) {
	env := newEnvDeps(t, false, func(d *Deps) {
		d.BackgroundMode = "window"
		d.EmbeddedMPV = true
	})
	s := getSettings(t, env)
	assert.Equal(t, "window", s.Background.Mode)
	assert.True(t, s.MPV.Embedded)
}

// mpv 缺失：settings 带 hint + install；用户装好后 POST /api/mpv/detect 翻转共享状态，
// 之后 settings 立刻看到 found=true，不需要重启。
func TestMPVDetectUpdatesSharedState(t *testing.T) {
	env := newEnv(t)
	env.mpvDetect = func(string) (mpv.Info, error) {
		return mpv.Info{}, assert.AnError
	}

	rec := env.do(t, http.MethodPost, "/api/mpv/detect", "")
	require.Equal(t, http.StatusOK, rec.Code, "没找到是状态不是失败")
	var view struct {
		Found   bool `json:"found"`
		Install *struct {
			URL string `json:"url"`
		} `json:"install"`
		Hint string `json:"hint"`
	}
	require.NoError(t, json.Unmarshal(decode(t, rec).Data, &view))
	assert.False(t, view.Found)
	assert.NotEmpty(t, view.Hint)
	require.NotNil(t, view.Install)
	assert.Equal(t, "https://mpv.io/installation/", view.Install.URL)

	s := getSettings(t, env)
	assert.False(t, s.MPV.Found)
	assert.Empty(t, s.MPV.Path)
	require.NotNil(t, s.MPV.Install)
	assert.Equal(t, mpv.InstallGuide().Command, s.MPV.Install.Command)
	assert.Equal(t, mpv.InstallGuide().Note, s.MPV.Install.Note)

	env.mpvDetect = func(string) (mpv.Info, error) {
		return mpv.Info{Path: "/opt/homebrew/bin/mpv", Version: "0.41.0", Source: mpv.SourceKnown}, nil
	}
	rec = env.do(t, http.MethodPost, "/api/mpv/detect", "")
	require.Equal(t, http.StatusOK, rec.Code)
	s = getSettings(t, env)
	assert.True(t, s.MPV.Found)
	assert.Equal(t, "/opt/homebrew/bin/mpv", s.MPV.Path)
	assert.Equal(t, mpv.SourceKnown, s.MPV.Source)
	assert.Nil(t, s.MPV.Install)
}

// 没有 Runtime 的 Handler：detect 给 503，settings 仍能渲染（found=false + install）。
func TestMPVDetectWithoutRuntime(t *testing.T) {
	h := New(Deps{})
	mux := http.NewServeMux()
	h.Register(mux)
	rec := (&testEnv{mux: mux}).do(t, http.MethodPost, "/api/mpv/detect", "")
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)

	view := mpvView(nil, false)
	assert.Equal(t, false, view["found"])
	assert.NotNil(t, view["install"])
}

// 桌面窗口（EmbeddedMPV）：播放用进程内 libmpv，命令行 mpv 找不找得到都报已就绪、带 embedded，
// 不给安装指引 —— 引导、媒体库的红点与横幅都只认 found。
func TestSettingsEmbeddedMPV(t *testing.T) {
	env := newEnvDeps(t, true, func(d *Deps) { d.EmbeddedMPV = true })
	env.mpvDetect = func(string) (mpv.Info, error) { return mpv.Info{}, errors.New("未找到 mpv") }
	rec := env.do(t, http.MethodPost, "/api/mpv/detect", "")
	require.Equal(t, http.StatusOK, rec.Code)

	s := getSettings(t, env)
	assert.True(t, s.MPV.Found)
	assert.True(t, s.MPV.Embedded)
	assert.Equal(t, "embedded", s.MPV.Source)
	assert.Nil(t, s.MPV.Install, "桌面窗口里没有要装的 mpv")
	assert.Empty(t, s.MPV.Hint)
	assert.Nil(t, s.MPV.CLI, "没有命令行 mpv：缩略图不出，但不算没就绪")

	var detected struct {
		Found    bool `json:"found"`
		Embedded bool `json:"embedded"`
	}
	require.NoError(t, json.Unmarshal(decode(t, rec).Data, &detected))
	assert.True(t, detected.Found && detected.Embedded, "重新检测也按桌面窗口报")
}

// 桌面窗口 + 探测到 .app 内置的命令行 mpv：照旧报 embedded，另在 cli 里给出缩略图用的那个 mpv。
func TestSettingsEmbeddedMPVWithBundledCLI(t *testing.T) {
	env := newEnvDeps(t, true, func(d *Deps) { d.EmbeddedMPV = true })
	env.mpvDetect = func(string) (mpv.Info, error) {
		return mpv.Info{Path: "/Applications/Nagare.app/Contents/MacOS/mpv", Version: "0.41.0", Source: mpv.SourceBundled}, nil
	}
	require.Equal(t, http.StatusOK, env.do(t, http.MethodPost, "/api/mpv/detect", "").Code)

	s := getSettings(t, env)
	assert.True(t, s.MPV.Found)
	assert.True(t, s.MPV.Embedded)
	assert.Equal(t, "embedded", s.MPV.Source)
	assert.Empty(t, s.MPV.Path, "播放用的不是这个路径上的 mpv")
	require.NotNil(t, s.MPV.CLI)
	assert.Equal(t, "/Applications/Nagare.app/Contents/MacOS/mpv", s.MPV.CLI.Path)
	assert.Equal(t, "0.41.0", s.MPV.CLI.Version)
	assert.Equal(t, "bundled", s.MPV.CLI.Source)
}

// 桌面窗口的状态照原样进设置载荷；没有桌面窗口的运行（Linux、--no-window）是零值。
func TestSettingsDesktopView(t *testing.T) {
	assert.Equal(t, DesktopView{}, getSettings(t, newEnv(t)).Desktop)

	env := newEnvDeps(t, true, func(d *Deps) {
		d.Desktop = func() DesktopView { return DesktopView{Fallback: "这台电脑上没有可用的 WebView2 运行时"} }
	})
	s := getSettings(t, env)
	assert.False(t, s.Desktop.Window)
	assert.Equal(t, "这台电脑上没有可用的 WebView2 运行时", s.Desktop.Fallback)

	on := newEnvDeps(t, true, func(d *Deps) { d.Desktop = func() DesktopView { return DesktopView{Window: true} } })
	assert.True(t, getSettings(t, on).Desktop.Window)
}

// 不是桌面窗口时照旧：没有 embedded 字段，找不到就给安装指引。
func TestSettingsCommandLineMPVUnchanged(t *testing.T) {
	env := newEnv(t)
	s := getSettings(t, env)
	assert.True(t, s.MPV.Found)
	assert.False(t, s.MPV.Embedded)
	assert.Equal(t, "path", s.MPV.Source)
	assert.Nil(t, s.MPV.CLI, "cli 只在桌面窗口里出现")
}

// shutdown：先回 200 空信封，再（异步、约 100ms 后）触发退出回调。
func TestShutdownRespondsBeforeExiting(t *testing.T) {
	env := newEnv(t)
	rec := env.do(t, http.MethodPost, "/api/shutdown", "")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.True(t, decode(t, rec).Success)

	select {
	case <-env.shutdown:
		t.Fatal("退出回调不应在响应写完前同步触发")
	default:
	}
	select {
	case <-env.shutdown:
	case <-time.After(2 * time.Second):
		t.Fatal("退出回调没有被触发")
	}
}

// 没接退出回调的实例：501，且不会崩。
func TestShutdownUnsupported(t *testing.T) {
	h := New(Deps{})
	mux := http.NewServeMux()
	h.Register(mux)
	rec := (&testEnv{mux: mux}).do(t, http.MethodPost, "/api/shutdown", "")
	assert.Equal(t, http.StatusNotImplemented, rec.Code)
	assert.Contains(t, decode(t, rec).Error, "不支持")
}
