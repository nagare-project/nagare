package torrentstream

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/anacrolix/torrent"
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

// 固定端口上重建 client：anacrolix 的 Client.Close 把 socket 关闭丢进 goroutine 就返回
// （client.go:402 `go s.Close()`），Close 返回那一刻端口多半还被占着。真机复现过：
// 第一次切换做种开关就撞 EADDRINUSE，回滚走同一端口再撞一次，引擎被标成死态。
// 之前的用例全部用 ListenPort 0（每次随机端口），所以从来没撞上。
func TestSetConfigRebuildsOnSamePortAfterClose(t *testing.T) {
	port := freeTCPPort(t)
	engine, err := New(Options{
		CacheDir:   t.TempDir(),
		StreamBase: func() string { return "" },
		Config:     Config{PortForwarding: false, ListenPort: port},
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, engine.Close()) })

	for i, seeding := range []bool{true, false, true} {
		restart, err := engine.SetConfig(Config{PortForwarding: false, ListenPort: port, Seeding: seeding})
		require.NoError(t, err, "第 %d 次在同一端口上重建", i+1)
		assert.False(t, restart)
		assert.False(t, engine.RestartRequired())
		_, err = engine.activeClient()
		require.NoError(t, err, "重建后引擎必须可用")
	}
}

// 端口迟迟不释放时，重建只在 EADDRINUSE 上等，其他错误立刻返回、不白等。
func TestRebuildRetriesOnlyOnAddrInUse(t *testing.T) {
	engine := newTestEngine(t, t.TempDir())
	prev := newClient
	t.Cleanup(func() { newClient = prev })

	var calls int
	newClient = func(dir string, cfg Config) (*torrent.Client, error) {
		calls++
		if calls <= 3 {
			return nil, errs.Wrap(errs.CategoryTorrent, "torrentstream.client", "磁力引擎启动失败", "换端口",
				&net.OpError{Op: "listen", Err: &os.SyscallError{Syscall: "bind", Err: testAddrInUse}})
		}
		return prev(dir, cfg)
	}
	_, err := engine.SetConfig(Config{PortForwarding: false, ListenPort: 0, Seeding: true})
	require.NoError(t, err, "端口释放后应重建成功")
	assert.Equal(t, 4, calls, "前三次 EADDRINUSE 都应重试")

	calls = 0
	newClient = func(dir string, cfg Config) (*torrent.Client, error) {
		calls++
		if calls == 1 {
			return nil, errors.New("别的错误")
		}
		return prev(dir, cfg)
	}
	_, err = engine.SetConfig(Config{PortForwarding: false, ListenPort: 0, Seeding: false})
	require.Error(t, err)
	assert.Equal(t, 2, calls, "非 EADDRINUSE 不重试：一次失败 + 一次回滚")
	_, err = engine.activeClient()
	require.NoError(t, err, "回滚成功，引擎仍可用")
}

// freeTCPPort 向系统要一个当前空闲的端口。
func freeTCPPort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := l.Addr().(*net.TCPAddr).Port
	require.NoError(t, l.Close())
	return port
}

// 缓存目录被占用（Windows 的「being used by another process」）时在宽限期内重试，
// 句柄一放开就删掉；不是第一次失败就放弃。
func TestPurgeCacheRetriesWhileFilesAreBusy(t *testing.T) {
	engine := newTestEngine(t, t.TempDir())
	prev := removeAll
	t.Cleanup(func() { removeAll = prev })

	var calls int
	removeAll = func(dir string) error {
		calls++
		if calls <= 3 {
			return errors.New("remove: The process cannot access the file because it is being used by another process.")
		}
		return prev(dir)
	}
	require.NoError(t, engine.purgeCache())
	assert.Equal(t, 4, calls, "前三次被占用都应重试")
}
