package api

import (
	"encoding/json"
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
		Found   bool   `json:"found"`
		Path    string `json:"path"`
		Version string `json:"version"`
		Source  string `json:"source"`
		Hint    string `json:"hint"`
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

	view := mpvView(nil)
	assert.Equal(t, false, view["found"])
	assert.NotNil(t, view["install"])
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
