// 评审（Go / 安全两轮）打回的问题的回归测试。
//
// 这些用例复用 integration_test.go 里的脚手架，因此同样【不能】并行：
// 它们都要替换包级的 newClient / 超时变量。
package torrentstream

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	errs "github.com/nagare-project/nagare/internal/errors"
)

// asCategorized 取出分类错误，顺带断言它确实是一个分类错误而不是裸 error。
func asCategorized(t *testing.T, err error) *errs.E {
	t.Helper()
	require.Error(t, err)
	var ce *errs.E
	require.ErrorAs(t, err, &ce, "失败必须带分类与中文提示，不能是裸 error：%v", err)
	return ce
}

// ─────────────────────── 私有种子（安全评审 H2） ───────────────────────

// buildPrivateTorrent 造一个带 BEP27 private 标记的单文件种子。
func buildPrivateTorrent(t *testing.T, dir string) (magnet string, mi *metainfo.MetaInfo) {
	t.Helper()
	name := "[PT] Private Release - 01 [1080p].mkv"
	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, deterministicBytes(512*1024, 7), 0o600))

	info := metainfo.Info{PieceLength: testPieceLength}
	require.NoError(t, info.BuildFromFilePath(path))
	private := true
	info.Private = &private

	mi = &metainfo.MetaInfo{}
	mi.SetDefaults()
	infoBytes, err := bencode.Marshal(info)
	require.NoError(t, err)
	mi.InfoBytes = infoBytes

	magnet = metainfo.Magnet{InfoHash: mi.HashInfoBytes(), DisplayName: info.BestName()}.String()
	return magnet, mi
}

// 私有站种子必须在拿到 info 的那一刻就停下，而不是继续播。
//
// 依据：anacrolix v1.61.0 没有「只对某个种子关掉 DHT/PEX」的能力，而 private
// 标记只在 info 字典里 —— 磁力得先靠 DHT 找到 peer 才拿得到 info。等我们知道
// 它是私有种子时 infohash 已经进过公共 DHT 了，那一步无法避免；能做的是立刻
// 停手并说清楚，而不是让用户的私有站账号一直暴露下去。
func TestPrivateTorrentIsRefused(t *testing.T) {
	seedDir, cacheDir := t.TempDir(), t.TempDir()
	magnet, mi := buildPrivateTorrent(t, seedDir)

	engine := newIntegrationEngine(t, cacheDir)
	seedTor := startSeeder(t, seedDir, mi)
	linkSeeder(t, seedTor, engine)

	_, err := engine.Prepare(prepareCtx(t), PrepareRequest{Magnet: magnet, FileIndex: -1})
	ce := asCategorized(t, err)
	assert.Equal(t, errs.CategoryInput, ce.Category, "这是「用户给的输入不受支持」，不是 swarm 出了问题")
	assert.Contains(t, ce.UserMsg, "私有站")
	assert.True(t, containsHan(ce.Recovery), "恢复动作要说清楚为什么，且必须是中文")

	// 会话必须收干净：种子不留、缓存不留。
	assert.Empty(t, currentClient(engine).Torrents(), "被拒的私有种子不能留在 client 里")
}

// ─────────────────────── 磁盘写失败（安全评审 M1） ───────────────────────

// 分片写盘失败必须变成一条分类错误，而不是让进度悄悄停住。
//
// anacrolix 的默认处理是记一条 CRITICAL 日志再永久停掉该种子的下载，而那条日志
// 正好被引擎丢弃了 —— 不接住它，用户看到的只有「进度不动了」，与「分享者太慢」
// 完全无法区分。
func TestWriteFailureBecomesStorageError(t *testing.T) {
	s := &session{}
	require.NoError(t, s.storageFailure(), "没出错时不该凭空造一个错误")

	s.noteWriteError(errors.New("no space left on device"))
	ce := asCategorized(t, s.storageFailure())
	assert.Equal(t, errs.CategoryStorage, ce.Category, "磁盘写不进去是本机问题，不能借 CategoryFS 的 404")
	assert.Contains(t, ce.UserMsg, "磁盘")
	assert.NotEmpty(t, ce.Recovery)

	// 后面的错误都是同一个原因的回声，只留第一条。
	s.noteWriteError(errors.New("another one"))
	assert.ErrorContains(t, s.storageFailure(), "no space left on device")
}

// 播放【开始之后】才发生的写失败没有请求在等着接它，只能从状态里透出来。
func TestWriteFailureSurfacesInStatus(t *testing.T) {
	s := &session{}
	var st Status
	s.fill(&st)
	assert.Empty(t, st.Error, "一切正常时状态里不该有错误")

	s.noteWriteError(errors.New("permission denied"))
	s.fill(&st)
	assert.Contains(t, st.Error, "磁盘写入失败")
	assert.True(t, containsHan(st.Error))
}

// ─────────────────────── 文件数上限（安全评审 M2） ───────────────────────

// 解析每个文件名都要跑一遍解析链，而这一步是握着 prepMu 同步做的。
// 不设上限，一份畸形种子就能让「点了播放没反应」持续可观的时间。
func TestSelectFileRejectsAbsurdFileCount(t *testing.T) {
	entries := make([]fileEntry, maxTorrentFiles+1)
	for i := range entries {
		entries[i] = fileEntry{Index: i, Path: "ep.mkv", Size: 1}
	}
	ce := asCategorized(t, func() error {
		_, err := selectFile(entries, PrepareRequest{FileIndex: -1})
		return err
	}())
	assert.Equal(t, errs.CategoryInput, ce.Category)
	assert.Contains(t, ce.UserMsg, "文件数异常")
}

// ─────────────────────── 关闭与重建的竞态（Go 评审 H1） ───────────────────────

// 回归：Close 跑完之后，一个正卡在 newClient 里的 SetConfig 不能把刚建好的
// client 写回引擎 —— 那个 client 再没有人关（main.go 只调一次 Close），
// 监听端口与 DHT 会一直活到进程退出。
func TestCloseDoesNotResurrectClientFromConcurrentRebuild(t *testing.T) {
	cacheDir := t.TempDir()
	engine := newIntegrationEngine(t, cacheDir) // 这一步已经把 newClient 换成 offlineClient

	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	var once sync.Once
	prev := newClient
	newClient = func(dir string, cfg Config) (*torrent.Client, error) {
		// 只卡住重建那一次；引擎自己回滚时还要能正常建。
		once.Do(func() {
			entered <- struct{}{}
			<-release
		})
		return prev(dir, cfg)
	}
	t.Cleanup(func() { newClient = prev })

	rebuilt := make(chan error, 1)
	go func() {
		_, err := engine.SetConfig(Config{ListenPort: 0, Seeding: true})
		rebuilt <- err
	}()

	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		close(release)
		t.Fatal("SetConfig 没有进到 client 重建里")
	}

	closed := make(chan error, 1)
	go func() { closed <- engine.Close() }()
	// Close 会在 prepMu 上等着重建让路，这里放行。
	time.Sleep(20 * time.Millisecond)
	close(release)

	require.NoError(t, <-closed)
	<-rebuilt // 报不报错都行，关键是别把 client 留下
	assert.Nil(t, currentClient(engine), "引擎已关闭，不能还握着一个活着的 client")
}

// ─────────────────────── 按会话身份收（Go 评审 H2） ───────────────────────

// 抢占与保留时限都跑在 prepMu 之外：它们只能收掉自己观察到的那个会话，
// 无条件收「当前那个」会把另一次 Prepare 刚建好的会话误杀。
func TestStopSessionOnlyStopsTheSessionItWasGiven(t *testing.T) {
	e := &Engine{cacheDir: t.TempDir()}
	stale := &session{engine: e, cancel: func() {}}
	fresh := &session{engine: e, cancel: func() {}}
	e.sess = fresh

	e.stopSession(stale)

	e.mu.Lock()
	defer e.mu.Unlock()
	assert.Same(t, fresh, e.sess, "被指名的是旧会话，当前会话不该受影响")
	assert.False(t, fresh.closed, "当前会话不该被连累关掉")
}

func TestStopSessionIsNilSafe(t *testing.T) {
	e := &Engine{cacheDir: t.TempDir()}
	e.stopSession(nil) // 不 panic 即可
	assert.Nil(t, e.sess)
}

// 换了一条磁力时，抢占要把旧会话收掉（否则新的那次 Prepare 会在 prepMu 上
// 白等一个元数据超时）。「同一条磁力停在选集弹窗上就放过它」那一半由
// integration_test.go 的 TestPackSelection 覆盖 —— 那里有真实的 info。
func TestInterruptStopsSessionForDifferentMagnet(t *testing.T) {
	e := &Engine{cacheDir: t.TempDir()}
	sess := &session{
		engine:   e,
		cancel:   func() {},
		magnet:   "magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567",
		awaiting: true,
	}
	e.sess = sess

	e.interrupt("magnet:?xt=urn:btih:ffffffffffffffffffffffffffffffffffffffff")

	e.mu.Lock()
	got := e.sess
	e.mu.Unlock()
	assert.Nil(t, got, "换了一条磁力，旧会话应当被收掉")
	assert.True(t, sess.closed)
}

// 两次并发的 Prepare（用户双击、或没等界面反应就点了另一条）：
// 后来的那次会抢占前一次，前一次必须【立刻】返回一个分类错误，
// 而不是让后来者在 prepMu 上白等一个元数据超时。
func TestConcurrentPrepareInterruptsTheEarlierOne(t *testing.T) {
	engine := newIntegrationEngine(t, t.TempDir())
	shortenWaits(t, 3*time.Second, 3*time.Second)

	const magnetA = "magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567"
	const magnetB = "magnet:?xt=urn:btih:fedcba9876543210fedcba9876543210fedcba98"

	firstDone := make(chan error, 1)
	go func() {
		// 没有任何分享者，它会一直停在「等元数据」上直到被抢占或超时。
		_, err := engine.Prepare(prepareCtx(t), PrepareRequest{Magnet: magnetA, FileIndex: -1})
		firstDone <- err
	}()

	// 等它真的进到等待里，否则抢占会发生在它开始之前，测的就不是这件事了。
	require.Eventually(t, func() bool {
		return engine.Status().Phase == PhaseMetadata
	}, 3*time.Second, 5*time.Millisecond, "第一次准备没有进入等元数据阶段")

	start := time.Now()
	go func() {
		_, _ = engine.Prepare(prepareCtx(t), PrepareRequest{Magnet: magnetB, FileIndex: -1})
	}()

	select {
	case err := <-firstDone:
		ce := asCategorized(t, err)
		assert.Equal(t, errs.CategoryTorrent, ce.Category)
		assert.True(t, containsHan(ce.UserMsg))
		assert.Less(t, time.Since(start), 2*time.Second,
			"被抢占的那次要立刻返回，不能拖到元数据超时")
	case <-time.After(2 * time.Second):
		t.Fatal("第一次准备没有被抢占，后来者会在 prepMu 上白等一个超时")
	}
	engine.Stop()
}
