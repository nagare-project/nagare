package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/nagare-project/nagare/internal/animego"
	"github.com/nagare-project/nagare/internal/apigolden"
	"github.com/nagare-project/nagare/internal/mpv"
	"github.com/nagare-project/nagare/internal/store"
)

// API 黄金响应（testdata/api/*.json，见 docs/api-goldens.md）的测试环境：真实处理器 + 确定性的替身。
// 固定时钟、固定能力段、不联网、不碰用户的配置目录；会话凭证只是占位串，响应里本来也不带它们。

// goldenNow 是黄金环境里的「现在」：2026 年秋季（Discover 的本季 = FALL 2026，下季 = WINTER 2027）。
var goldenNow = time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)

// goldenMTime 是媒体文件的修改时间：fileId 里含它（名字|大小|修改时间）。
var goldenMTime = time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC)

const (
	goldenArtPrefix   = "/art/cap-art"
	goldenMediaPrefix = "/media/cap-media"
	goldenBaseURL     = "https://catalog.example.test"
	// goldenMediaRoot 是黄金文件里媒体库目录的样子（真实路径是每次不同的临时目录，比对前换掉）。
	goldenMediaRoot = "/media/anime"
)

func goldenClock() time.Time { return goldenNow }

type goldenEnv struct {
	mux     *http.ServeMux
	h       *Handler
	store   *store.Store
	lib     *LibraryService
	player  *fakePlayer
	auth    *fakeAuth
	catalog *goldenCatalog
	lists   *goldenLists
	// scrub 是比对前要换掉的字面值（成对：真实值 → 黄金文件里的值）。
	scrub []string
}

// goldenOptions 调整黄金环境；零值是「已登录、mpv 已就绪、浏览器模式」。
type goldenOptions struct {
	deps func(*Deps)
	// statePath 非空时用这个状态文件（调用方预先写好）。
	statePath string
}

func newGoldenEnv(t *testing.T, opts goldenOptions) *goldenEnv {
	t.Helper()
	setGoldenPlatform(t, "linux", "amd64")
	path := opts.statePath
	if path == "" {
		path = filepath.Join(t.TempDir(), "state.json")
	}
	st, err := store.Open(path)
	require.NoError(t, err)
	lib := NewLibraryService(st)
	lib.now = goldenClock
	lib.SetArtPrefix(goldenArtPrefix)
	lib.SetMediaPrefix(goldenMediaPrefix)
	env := &goldenEnv{
		store:   st,
		lib:     lib,
		player:  &fakePlayer{},
		auth:    &fakeAuth{loggedIn: true, session: animego.Session{AccessToken: "placeholder-access", RefreshCookie: "placeholder-refresh"}},
		catalog: &goldenCatalog{},
		lists:   &goldenLists{},
	}
	env.lists.epoch = 1
	require.NoError(t, st.SetAnimegoSession(store.AnimegoSession{Email: "viewer@example.test"}))
	deps := Deps{
		Store:          st,
		Lib:            lib,
		Player:         env.player,
		Auth:           env.auth,
		Lists:          env.lists,
		Catalog:        env.catalog,
		AnimegoBaseURL: goldenBaseURL,
		MPV: mpv.NewRuntimeWith(func(string) (mpv.Info, error) {
			return mpv.Info{Path: "/usr/bin/mpv", Version: "0.41.0", Source: mpv.SourcePath}, nil
		}, ""),
		Version:        "0.7.0",
		EpisodeSpaces:  NewEpisodeSpaces(env.catalog),
		Shutdown:       func() {},
		DataDir:        "/home/viewer/.config/nagare",
		LogPath:        "/home/viewer/.config/nagare/logs/nagare.log",
		BackgroundMode: "tray",
	}
	if opts.deps != nil {
		opts.deps(&deps)
	}
	env.h = New(deps)
	env.h.SetCatalogArtPrefix(goldenArtPrefix)
	env.h.catalog.cache.now = goldenClock
	env.h.catalog.sleep = func(context.Context, time.Duration) error { return nil }
	env.h.lists.cache.now = goldenClock
	env.h.episodes.cache.now = goldenClock
	if deps.EpisodeSpaces != nil {
		deps.EpisodeSpaces.cache.now = goldenClock
	}
	env.mux = http.NewServeMux()
	env.h.Register(env.mux)
	return env
}

// setGoldenPlatform 把设置载荷报的平台钉住（黄金响应不随跑测试的机器变）。
func setGoldenPlatform(t *testing.T, goos, goarch string) {
	t.Helper()
	prevOS, prevArch := hostGOOS, hostGOARCH
	hostGOOS, hostGOARCH = goos, goarch
	t.Cleanup(func() { hostGOOS, hostGOARCH = prevOS, prevArch })
}

// call 打一次请求，返回已换掉临时路径等字面值的响应体，并核对状态码。
func (e *goldenEnv) call(t *testing.T, wantStatus int, method, target, body string) []byte {
	t.Helper()
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	e.mux.ServeHTTP(rec, req)
	require.Equal(t, wantStatus, rec.Code, rec.Body.String())
	return apigolden.Scrub(rec.Body.Bytes(), e.scrub...)
}

// writeGoldenMedia 在 root 下造一个文件（稀疏文件，大小 size），修改时间固定为 goldenMTime。
func writeGoldenMedia(t *testing.T, root, rel string, size int64) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	f, err := os.Create(path)
	require.NoError(t, err)
	require.NoError(t, f.Truncate(size))
	require.NoError(t, f.Close())
	require.NoError(t, os.Chtimes(path, goldenMTime, goldenMTime))
}
