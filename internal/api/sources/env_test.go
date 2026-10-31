package sources

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/nagare-project/nagare/internal/animego"
	"github.com/nagare-project/nagare/internal/api"
	"github.com/nagare-project/nagare/internal/library"
	"github.com/nagare-project/nagare/internal/mpv"
	"github.com/nagare-project/nagare/internal/player"
	"github.com/nagare-project/nagare/internal/rules"
	"github.com/nagare-project/nagare/internal/rulesync"
	"github.com/nagare-project/nagare/internal/sourceplugin"
	"github.com/nagare-project/nagare/internal/store"
)

// 测试环境与桌面版 main 的装配一致：api.Handler 与本包的 Handler 挂在同一个 mux 上，
// 设置载荷的 torrent 一项经 api.Deps.TorrentSettings 接到本包。

// fakePlayer 是 api.PlayerAPI 替身。
type fakePlayer struct {
	playRes     player.PlayResult
	playErr     error
	lastItem    library.Item
	lastSub     string
	lastPath    string
	lastHeaders map[string]string
	lastAnilist int
	lastAlts    []string
	stopped     bool
	status      player.Status
	// log 记调用顺序（可为 nil）。磁力播放里「先停播放器再准备种子」的顺序
	// 是正确性的一部分，只能靠调用序来断言。
	log func(string)
}

func (f *fakePlayer) Play(_ context.Context, src player.MediaSource, sub string) (player.PlayResult, error) {
	f.record("player.play")
	f.lastItem, f.lastSub = src.Item(), sub
	if withHeaders, ok := src.(interface{ HTTPHeaders() map[string]string }); ok {
		f.lastPath = src.MPVPath()
		f.lastHeaders = withHeaders.HTTPHeaders()
	}
	if hinted, ok := src.(interface{ MatchHints() (int, []string) }); ok {
		f.lastAnilist, f.lastAlts = hinted.MatchHints()
	}
	result := f.playRes
	if result.FileID == "" {
		result.FileID = f.lastItem.FileID
	}
	return result, f.playErr
}
func (f *fakePlayer) record(op string) {
	if f.log != nil {
		f.log(op)
	}
}
func (f *fakePlayer) Stop()                         { f.record("player.stop"); f.stopped = true }
func (f *fakePlayer) SetPause(bool) error           { return nil }
func (f *fakePlayer) Seek(float64) error            { return nil }
func (f *fakePlayer) Status() player.Status         { return f.status }
func (f *fakePlayer) RefreshShaders() (bool, error) { return false, nil }

// fakeAuth 是 api.AnimegoAuth 替身（设置载荷要读登录态）。
type fakeAuth struct{}

func (fakeAuth) Login(context.Context, string, string) (animego.User, error) {
	return animego.User{}, nil
}
func (fakeAuth) RestoreSession(animego.Session) {}
func (fakeAuth) Session() animego.Session       { return animego.Session{} }
func (fakeAuth) LoggedIn() bool                 { return false }

type testEnv struct {
	mux     *http.ServeMux
	store   *store.Store
	player  *fakePlayer
	sources *SourcesService
	plugin  *fakeSourcePluginRuntime
	torrent *fakeTorrent
	// downloads 是磁力下载的替身（见 downloads_test.go）。
	downloads *fakeDownloads
	// calls 是跨替身的调用顺序记录。
	calls *[]string
}

func newEnv(t *testing.T) *testEnv {
	t.Helper()
	return newEnvWith(t, true)
}

// newEnvNoTorrent 造一个磁力引擎缺席的环境（引擎启动失败时的降级形态）。
func newEnvNoTorrent(t *testing.T) *testEnv {
	t.Helper()
	return newEnvWith(t, false)
}

func newEnvWith(t *testing.T, withTorrent bool) *testEnv {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "state.json"))
	require.NoError(t, err)
	env := &testEnv{store: st, player: &fakePlayer{}}
	env.sources = NewSourcesService(st, &rules.Fetcher{}, &rulesync.Syncer{}, filepath.Join(t.TempDir(), "rules"))
	env.plugin = &fakeSourcePluginRuntime{status: sourceplugin.Status{Phase: "disabled"}}
	calls := []string{}
	env.calls = &calls
	record := func(op string) { calls = append(calls, op) }
	env.player.log = record
	deps := Deps{
		Store:           st,
		Player:          env.player,
		Sources:         env.sources,
		SourcePlugin:    NewSourcePluginService(st, env.plugin, "test"),
		TorrentCacheDir: "/data/nagare/cache/torrent",

		DefaultDownloadDir: "/home/you/Downloads/nagare",
	}
	env.downloads = &fakeDownloads{}
	deps.Downloads = env.downloads
	if withTorrent {
		env.torrent = &fakeTorrent{log: record}
		deps.Torrent = env.torrent
	}
	h := New(deps)
	core := api.New(api.Deps{
		Store:           st,
		Lib:             api.NewLibraryService(st),
		Player:          env.player,
		Auth:            fakeAuth{},
		AnimegoBaseURL:  "https://example.test",
		MPV:             mpv.NewRuntimeWith(func(string) (mpv.Info, error) { return mpv.Info{}, nil }, ""),
		Version:         "test",
		TorrentSettings: h.TorrentSettings,
	})
	env.mux = http.NewServeMux()
	core.Register(env.mux)
	h.Register(env.mux)
	return env
}

func (e *testEnv) do(t *testing.T, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	rec := httptest.NewRecorder()
	e.mux.ServeHTTP(rec, req)
	return rec
}

type envData struct {
	Success bool            `json:"success"`
	Data    json.RawMessage `json:"data"`
	Error   string          `json:"error"`
}

func decode(t *testing.T, rec *httptest.ResponseRecorder) envData {
	t.Helper()
	var e envData
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &e), "响应体：%s", rec.Body.String())
	return e
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return b
}
