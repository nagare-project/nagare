package torrentstream

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	errs "github.com/nagare-project/nagare/internal/errors"
)

// newTestEngine 起一个真实引擎：ListenPort 0 交给系统分配，避免并行测试抢端口；
// 不加任何种子，因此不会产生网络请求。
func newTestEngine(t *testing.T, cacheDir string) *Engine {
	t.Helper()
	engine, err := New(Options{
		CacheDir:   cacheDir,
		StreamBase: func() string { return "http://127.0.0.1:8590/stream/deadbeef" },
		Config:     Config{PortForwarding: false, ListenPort: 0},
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, engine.Close()) })
	return engine
}

func TestNewPurgesLeftoverCache(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "torrent")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "旧种子"), 0o700))
	junk := filepath.Join(dir, "旧种子", "残留分片.mkv")
	require.NoError(t, os.WriteFile(junk, make([]byte, 4096), 0o600))

	engine := newTestEngine(t, dir)

	_, err := os.Stat(junk)
	assert.True(t, os.IsNotExist(err), "启动时应清空残留分片")
	info, err := os.Stat(dir)
	require.NoError(t, err, "缓存目录应被重建")
	assert.True(t, info.IsDir())
	assert.Zero(t, engine.CacheBytes())
}

func TestNewRejectsEmptyCacheDir(t *testing.T) {
	_, err := New(Options{CacheDir: ""})
	require.Error(t, err)
	var e *errs.E
	require.ErrorAs(t, err, &e)
	assert.Equal(t, errs.CategoryInternal, e.Category)
}

func TestNewReportsStorageFailure(t *testing.T) {
	// 缓存目录的父级是一个普通文件：建目录必然失败，且必须归到 CategoryStorage
	// （「写不进去」不是「文件不存在」，用户要做的是清空间/改目录，不是重新扫描）。
	parent := filepath.Join(t.TempDir(), "占位文件")
	require.NoError(t, os.WriteFile(parent, []byte("x"), 0o600))

	_, err := New(Options{CacheDir: filepath.Join(parent, "torrent")})
	require.Error(t, err)
	var e *errs.E
	require.ErrorAs(t, err, &e)
	assert.Equal(t, errs.CategoryStorage, e.Category)
	assert.NotEmpty(t, e.Recovery, "失败必须给出恢复动作")
}

func TestSetConfigTrackersOnlyDoesNotRebuild(t *testing.T) {
	engine := newTestEngine(t, filepath.Join(t.TempDir(), "torrent"))
	before := engine.client

	restart, err := engine.SetConfig(Config{
		PortForwarding: false,
		ListenPort:     0,
		Trackers:       []string{"udp://tracker.example:1337/announce", "不是地址"},
	})
	require.NoError(t, err)
	assert.False(t, restart, "只改 tracker 不需要重建")
	assert.Same(t, before, engine.client, "client 不该被换掉")
	assert.False(t, engine.RestartRequired())
	assert.Equal(t, []string{"udp://tracker.example:1337/announce"}, engine.trackers(),
		"非法地址在存入前就被过滤掉")
}

func TestSetConfigRebuildsClientWhenIdle(t *testing.T) {
	engine := newTestEngine(t, filepath.Join(t.TempDir(), "torrent"))
	before := engine.client

	restart, err := engine.SetConfig(Config{Seeding: true, PortForwarding: false, ListenPort: 0})
	require.NoError(t, err)
	assert.False(t, restart, "没有活动会话时就地重建，不需要重启 nagare")
	assert.NotSame(t, before, engine.client, "client 应被换成新配置的那个")
	assert.True(t, engine.seedingEnabled())
	assert.False(t, engine.RestartRequired(), "已经就地生效，不该再提示重启")
}

func TestSetConfigRequiresRestartWhilePlaying(t *testing.T) {
	engine := newTestEngine(t, filepath.Join(t.TempDir(), "torrent"))
	before := engine.client
	attachFakeSession(engine)

	restart, err := engine.SetConfig(Config{Seeding: true, PortForwarding: false, ListenPort: 0})
	require.NoError(t, err)
	assert.True(t, restart, "正在播时不重建，改为提示重启")
	assert.Same(t, before, engine.client, "不能把正在播的这一集掐掉")
	// 提示必须活过一次页面刷新：只在这一次响应里出现等于没提示。
	assert.True(t, engine.RestartRequired())
	assert.True(t, engine.RestartRequired())

	engine.Stop()
}

func TestSetConfigPortChangeRebuilds(t *testing.T) {
	engine := newTestEngine(t, filepath.Join(t.TempDir(), "torrent"))
	before := engine.client

	restart, err := engine.SetConfig(Config{PortForwarding: true, ListenPort: 0})
	require.NoError(t, err)
	assert.False(t, restart)
	assert.NotSame(t, before, engine.client)
}

func TestClearCacheResetsUsage(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "torrent")
	engine := newTestEngine(t, dir)

	require.NoError(t, os.WriteFile(filepath.Join(dir, "片段.mkv"), make([]byte, 8192), 0o600))
	forceCacheRefresh(engine)
	require.Equal(t, int64(8192), engine.CacheBytes())

	require.NoError(t, engine.ClearCache())
	assert.Zero(t, engine.CacheBytes())
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Empty(t, entries)
}

func TestStopAndCloseAreIdempotent(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "torrent")
	engine, err := New(Options{CacheDir: dir, Config: Config{ListenPort: 0}})
	require.NoError(t, err)

	engine.Stop()
	engine.Stop()
	attachFakeSession(engine)
	engine.Stop()
	engine.Stop()

	require.NoError(t, engine.Close())
	require.NoError(t, engine.Close())

	_, statErr := os.Stat(dir)
	assert.True(t, os.IsNotExist(statErr), "退出时应清空缓存目录")
}

func TestStatusWhenIdle(t *testing.T) {
	engine := newTestEngine(t, filepath.Join(t.TempDir(), "torrent"))
	st := engine.Status()
	assert.False(t, st.Active)
	assert.Equal(t, PhaseIdle, st.Phase)
	assert.Zero(t, st.CacheBytes)
	assert.False(t, st.Seeding)
}

func TestPrepareRejectsNonMagnet(t *testing.T) {
	engine := newTestEngine(t, filepath.Join(t.TempDir(), "torrent"))
	_, err := engine.Prepare(context.Background(), PrepareRequest{Magnet: "https://example.com/a.torrent", FileIndex: -1})
	require.Error(t, err)
	var e *errs.E
	require.ErrorAs(t, err, &e)
	assert.Equal(t, errs.CategoryInput, e.Category)
}

func TestPrepareAfterCloseFails(t *testing.T) {
	engine, err := New(Options{CacheDir: filepath.Join(t.TempDir(), "torrent"), Config: Config{ListenPort: 0}})
	require.NoError(t, err)
	require.NoError(t, engine.Close())

	_, err = engine.Prepare(context.Background(), PrepareRequest{Magnet: "magnet:?xt=urn:btih:" + testInfohash, FileIndex: -1})
	require.Error(t, err)
	var e *errs.E
	require.ErrorAs(t, err, &e)
	assert.Equal(t, errs.CategoryInternal, e.Category)
}

func TestHandlerReturns404WhenNothingPlaying(t *testing.T) {
	engine := newTestEngine(t, filepath.Join(t.TempDir(), "torrent"))
	req := httptest.NewRequest(http.MethodGet, "/t/"+testInfohash+"/0", nil)
	rec := httptest.NewRecorder()
	engine.Handler().ServeHTTP(rec, req)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

// attachFakeSession 挂一个只有生命周期、没有种子的会话，
// 用来验证「正在播」这一条分支。
func attachFakeSession(e *Engine) {
	ctx, cancel := context.WithCancel(e.rootCtx)
	e.mu.Lock()
	e.sess = &session{engine: e, ctx: ctx, cancel: cancel, index: -1, phase: PhaseBuffering}
	e.mu.Unlock()
}

// forceCacheRefresh 抹掉节流时间戳，让下一次 CacheBytes 真的去走一遍目录。
func forceCacheRefresh(e *Engine) {
	e.cacheMu.Lock()
	e.cacheAt = time.Time{}
	e.cacheMu.Unlock()
}
