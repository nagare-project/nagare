package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nagare-project/nagare/internal/animego"
	errs "github.com/nagare-project/nagare/internal/errors"
	"github.com/nagare-project/nagare/internal/library"
	"github.com/nagare-project/nagare/internal/mpv"
	"github.com/nagare-project/nagare/internal/player"
	"github.com/nagare-project/nagare/internal/rules"
	"github.com/nagare-project/nagare/internal/rulesync"
	"github.com/nagare-project/nagare/internal/store"
)

// fakePlayer 是 PlayerAPI 替身。
type fakePlayer struct {
	playRes  player.PlayResult
	playErr  error
	lastItem library.Item
	lastSub  string
	stopped  bool
	status   player.Status
}

func (f *fakePlayer) Play(_ context.Context, item library.Item, sub string) (player.PlayResult, error) {
	f.lastItem, f.lastSub = item, sub
	return f.playRes, f.playErr
}
func (f *fakePlayer) Stop()                 { f.stopped = true }
func (f *fakePlayer) SetPause(bool) error   { return nil }
func (f *fakePlayer) Seek(float64) error    { return nil }
func (f *fakePlayer) Status() player.Status { return f.status }

// fakeAuth 是 AnimegoAuth 替身。
type fakeAuth struct {
	user     animego.User
	loginErr error
	session  animego.Session
	loggedIn bool
}

func (f *fakeAuth) Login(_ context.Context, email, _ string) (animego.User, error) {
	if f.loginErr != nil {
		return animego.User{}, f.loginErr
	}
	f.loggedIn = true
	u := f.user
	if u.Email == "" {
		u.Email = email
	}
	return u, nil
}
func (f *fakeAuth) RestoreSession(s animego.Session) { f.session = s; f.loggedIn = false }
func (f *fakeAuth) Session() animego.Session         { return f.session }
func (f *fakeAuth) LoggedIn() bool                   { return f.loggedIn }

type testEnv struct {
	mux     *http.ServeMux
	store   *store.Store
	lib     *LibraryService
	player  *fakePlayer
	auth    *fakeAuth
	sources *SourcesService
	// mpvDetect 是注入给 mpv.Runtime 的探测函数，测试改它再打 /api/mpv/detect 翻转状态。
	mpvDetect func(string) (mpv.Info, error)
	// shutdown 在 Deps.Shutdown 被调用时收到一个信号。
	shutdown chan struct{}
}

func newEnv(t *testing.T) *testEnv {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "state.json"))
	require.NoError(t, err)
	env := &testEnv{
		store:    st,
		lib:      NewLibraryService(st),
		player:   &fakePlayer{},
		auth:     &fakeAuth{session: animego.Session{AccessToken: "at", RefreshCookie: "rc"}},
		shutdown: make(chan struct{}, 1),
	}
	env.mpvDetect = func(string) (mpv.Info, error) {
		return mpv.Info{Path: "/usr/bin/mpv", Version: "0.41.0", Source: mpv.SourcePath}, nil
	}
	env.sources = NewSourcesService(st, &rules.Fetcher{}, &rulesync.Syncer{}, filepath.Join(t.TempDir(), "rules"))
	h := New(Deps{
		Store:          st,
		Lib:            env.lib,
		Player:         env.player,
		Auth:           env.auth,
		AnimegoBaseURL: "https://example.test",
		MPV:            mpv.NewRuntimeWith(func(e string) (mpv.Info, error) { return env.mpvDetect(e) }, ""),
		Version:        "test",
		Sources:        env.sources,
		Shutdown:       func() { env.shutdown <- struct{}{} },
		DataDir:        "/data/nagare",
		LogPath:        "/data/nagare/logs/nagare.log",
	})
	env.mux = http.NewServeMux()
	h.Register(env.mux)
	return env
}

func (e *testEnv) do(t *testing.T, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, target, reader)
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

// 造一个真实媒体目录（两集 + 字幕）。
func makeMediaDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	sub := filepath.Join(dir, "葬送的芙莉莲")
	require.NoError(t, os.MkdirAll(sub, 0o755))
	for _, name := range []string{"[Sub] Frieren - 01 [1080p].mkv", "[Sub] Frieren - 02 [1080p].mkv"} {
		require.NoError(t, os.WriteFile(filepath.Join(sub, name), make([]byte, 1<<20), 0o644))
	}
	require.NoError(t, os.WriteFile(filepath.Join(sub, "[Sub] Frieren - 01 [1080p].ass"), []byte("x"), 0o644))
	return dir
}

// 添加目录 → 扫描 → 视图形状正确（含字幕配对与进度合并）。
func TestAddFolderAndGetLibrary(t *testing.T) {
	env := newEnv(t)
	dir := makeMediaDir(t)

	rec := env.do(t, http.MethodPost, "/api/library/folders", `{"path":`+string(mustJSON(t, dir))+`}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var addRes struct {
		Folder store.Folder `json:"folder"`
		Stats  Stats        `json:"stats"`
	}
	require.NoError(t, json.Unmarshal(decode(t, rec).Data, &addRes))
	assert.Equal(t, 2, addRes.Stats.Videos)
	assert.Equal(t, 1, addRes.Stats.Clusters)

	rec = env.do(t, http.MethodGet, "/api/library", "")
	require.Equal(t, http.StatusOK, rec.Code)
	var view LibraryView
	require.NoError(t, json.Unmarshal(decode(t, rec).Data, &view))
	require.Len(t, view.Clusters, 1)
	assert.Equal(t, "葬送的芙莉莲", view.Clusters[0].Title)
	assert.Equal(t, 2, view.Clusters[0].EpisodeCount)
	require.Len(t, view.Clusters[0].Groups, 1)
	require.Len(t, view.Clusters[0].Groups[0].Items, 2)
	assert.NotNil(t, view.ScannedAt)

	// 写入进度后视图应带出。
	fileID := view.Clusters[0].Groups[0].Items[0].FileID
	require.NoError(t, env.store.SetProgress(fileID, store.Progress{PositionSec: 100, DurationSec: 1400}))
	rec = env.do(t, http.MethodGet, "/api/library", "")
	require.NoError(t, json.Unmarshal(decode(t, rec).Data, &view))
	require.NotNil(t, view.Clusters[0].Groups[0].Items[0].Progress)
	assert.InDelta(t, 100, view.Clusters[0].Groups[0].Items[0].Progress.PositionSec, 0.01)
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return b
}

// 路径校验：相对路径 400、不存在 404、重复 400。
func TestAddFolderValidation(t *testing.T) {
	env := newEnv(t)

	rec := env.do(t, http.MethodPost, "/api/library/folders", `{"path":"relative/path"}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, decode(t, rec).Error, "绝对路径")

	rec = env.do(t, http.MethodPost, "/api/library/folders", `{"path":"/不存在的路径xyz"}`)
	assert.Equal(t, http.StatusNotFound, rec.Code)

	dir := makeMediaDir(t)
	body := `{"path":` + string(mustJSON(t, dir)) + `}`
	require.Equal(t, http.StatusOK, env.do(t, http.MethodPost, "/api/library/folders", body).Code)
	rec = env.do(t, http.MethodPost, "/api/library/folders", body)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, decode(t, rec).Error, "已在库中")
}

// 播放：未知 fileId 404；已知条目透传给 Player 且带上配对字幕。
func TestPlay(t *testing.T) {
	env := newEnv(t)
	dir := makeMediaDir(t)
	env.do(t, http.MethodPost, "/api/library/folders", `{"path":`+string(mustJSON(t, dir))+`}`)

	rec := env.do(t, http.MethodPost, "/api/play", `{"fileId":"ghost"}`)
	assert.Equal(t, http.StatusNotFound, rec.Code)

	var view LibraryView
	require.NoError(t, json.Unmarshal(decode(t, env.do(t, http.MethodGet, "/api/library", "")).Data, &view))
	var ep1 ViewItem
	for _, it := range view.Clusters[0].Groups[0].Items {
		if it.Episode != nil && *it.Episode == 1 {
			ep1 = it
		}
	}
	require.NotEmpty(t, ep1.FileID)

	env.player.playRes = player.PlayResult{Title: "T", Danmaku: player.DanmakuInfo{State: "ok", Count: 42}}
	rec = env.do(t, http.MethodPost, "/api/play", `{"fileId":`+string(mustJSON(t, ep1.FileID))+`}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, ep1.FileID, env.player.lastItem.FileID)
	assert.True(t, strings.HasSuffix(env.player.lastSub, ".ass"), "第 1 集应带上配对的 ass 字幕，实际：%q", env.player.lastSub)
	var res player.PlayResult
	require.NoError(t, json.Unmarshal(decode(t, rec).Data, &res))
	assert.Equal(t, 42, res.Danmaku.Count)

	// 播放层的分类错误 → 对应状态码与用户话。
	env.player.playErr = errs.New(errs.CategoryFS, "t", "文件不存在或已被移动", "重新扫描后再试")
	rec = env.do(t, http.MethodPost, "/api/play", `{"fileId":`+string(mustJSON(t, ep1.FileID))+`}`)
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Contains(t, decode(t, rec).Error, "重新扫描")
}

// 登录成功后会话落盘（含 refresh cookie）；登出清空。
func TestLoginPersistsSession(t *testing.T) {
	env := newEnv(t)
	rec := env.do(t, http.MethodPost, "/api/animego/login", `{"email":"a@b.c","password":"pw"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	sess := env.store.AnimegoSession()
	assert.Equal(t, "a@b.c", sess.Email)
	assert.Equal(t, "at", sess.AccessToken)
	assert.Equal(t, "rc", sess.RefreshCookie)

	rec = env.do(t, http.MethodPost, "/api/animego/logout", "")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Empty(t, env.store.AnimegoSession().AccessToken)
}

// 登录失败：animego 错误按 Kind 映射状态码。
func TestLoginErrorMapping(t *testing.T) {
	env := newEnv(t)
	env.auth.loginErr = &animego.Error{Kind: animego.ErrRateLimited, Op: "login"}
	rec := env.do(t, http.MethodPost, "/api/animego/login", `{"email":"a@b.c","password":"x"}`)
	assert.Equal(t, http.StatusTooManyRequests, rec.Code)
	assert.Contains(t, decode(t, rec).Error, "频繁")
}

// seek 负数 400；请求体不是 JSON 400。
func TestInputValidation(t *testing.T) {
	env := newEnv(t)
	rec := env.do(t, http.MethodPost, "/api/player/seek", `{"position":-1}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)

	rec = env.do(t, http.MethodPost, "/api/play", `{broken`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, decode(t, rec).Error, "JSON")
}

// 库目录扫描失败（目录被删）→ 视图里带 error 而不是整个接口挂掉（CQ3 降级）。
func TestFolderErrorDegradation(t *testing.T) {
	env := newEnv(t)
	dir := makeMediaDir(t)
	env.do(t, http.MethodPost, "/api/library/folders", `{"path":`+string(mustJSON(t, dir))+`}`)

	require.NoError(t, os.RemoveAll(dir))
	rec := env.do(t, http.MethodPost, "/api/library/rescan", "")
	require.Equal(t, http.StatusOK, rec.Code)

	var view LibraryView
	require.NoError(t, json.Unmarshal(decode(t, env.do(t, http.MethodGet, "/api/library", "")).Data, &view))
	require.Len(t, view.Folders, 1)
	assert.NotEmpty(t, view.Folders[0].Error, "被删目录应标注错误")
	assert.Empty(t, view.Clusters)
}
