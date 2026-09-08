// 磁力边下边播的端到端集成测试（决议 T1）。
//
// anacrolix/torrent 能在同一个进程里既做种又下载，所以「一条磁力从头播到尾」
// 这条路可以跑得确定、秒级、且完全不出网。只测优先级窗口的算法会漏掉
// 「窗口算对了但 reader 仍然阻塞」这一类 bug —— 而那正是真会出的 bug。
//
// ⚠️ 本文件的用例【绝不能】用 t.Parallel()：它们要替换包级的 newClient /
// metadataTimeout / bufferTimeout，并行跑会互相把对方的注入改掉。
package torrentstream

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	errs "github.com/nagare-project/nagare/internal/errors"
	"github.com/nagare-project/nagare/internal/library"
)

const (
	// testPieceLength 取 256KB：20MB 的样片因此有 80 多个分片，四档优先级窗口
	// 才有实际意义；再小就只是让做种方建种子时多算一会儿。
	testPieceLength = 256 * 1024

	// testEpisodeBytes 是主用例的样片大小。必须 ≥ 20MB 才能同时压到
	// 「首 16MB 弹幕哈希」与「8MB 起播缓冲之外还剩东西可下」两条路；
	// 尾巴上多出的 137 字节是故意的 —— 文件长度不对齐分片边界才是常态，
	// 边界算错要在这里暴露，而不是在用户的最后一集上暴露。
	testEpisodeBytes = 20*1024*1024 + 137

	// testStreamBase 模拟 httpserver 给出的能力 URL 前缀。
	testStreamBase = "http://127.0.0.1:8590/stream/cap0123456789abcdef"

	// linkRetryInterval 是把做种方接到引擎上的重试间隔，见 linkSeeder。
	linkRetryInterval = 50 * time.Millisecond
)

// ─────────────────────────── 脚手架 ───────────────────────────

// testFile 是要放进测试种子里的一个文件。
type testFile struct {
	name string
	size int
}

// deterministicBytes 生成可复现的伪随机字节。
//
// 不用真视频（仓库里不该躺二进制），也不用全零：内容必须确定，infohash 才稳定、
// 失败才能原样复现；又必须够「随机」，否则各分片内容雷同，分片错位这类 bug
// 会被逐字节比对悄悄放过。
func deterministicBytes(n int, seed uint64) []byte {
	rng := rand.New(rand.NewPCG(seed, 0x9e3779b97f4a7c15))
	buf := make([]byte, n)
	var word [8]byte
	for i := 0; i < n; i += len(word) {
		binary.LittleEndian.PutUint64(word[:], rng.Uint64())
		copy(buf[i:], word[:])
	}
	return buf
}

// buildTestTorrent 在 dir 下造出测试数据并建成一份种子。
//
// root 非空时数据落在 dir/root 子目录里（多文件合集），为空时是单文件种子
// （此时只接受一个文件）。返回的 data 以【种子内路径】为键，供逐字节比对。
func buildTestTorrent(t *testing.T, dir, root string, files ...testFile) (magnet string, mi *metainfo.MetaInfo, data map[string][]byte) {
	t.Helper()
	require.NotEmpty(t, files)
	if root == "" {
		require.Len(t, files, 1, "单文件种子只能有一个文件")
	}

	base := dir
	if root != "" {
		base = filepath.Join(dir, root)
		require.NoError(t, os.MkdirAll(base, 0o700))
	}
	data = make(map[string][]byte, len(files))
	for i, f := range files {
		content := deterministicBytes(f.size, uint64(i)+1)
		require.NoError(t, os.WriteFile(filepath.Join(base, f.name), content, 0o600))
		key := f.name
		if root != "" {
			key = root + "/" + f.name
		}
		data[key] = content
	}

	// 先给 PieceLength 再 BuildFromFilePath：它只在字段为零时才自己挑长度。
	info := metainfo.Info{PieceLength: testPieceLength}
	buildRoot := base
	if root == "" {
		buildRoot = filepath.Join(dir, files[0].name)
	}
	require.NoError(t, info.BuildFromFilePath(buildRoot))

	mi = &metainfo.MetaInfo{}
	mi.SetDefaults()
	infoBytes, err := bencode.Marshal(info)
	require.NoError(t, err)
	mi.InfoBytes = infoBytes

	// 磁力里不带 tr：本体不内置 tracker，测试也不该给它任何出网的理由。
	magnet = metainfo.Magnet{InfoHash: mi.HashInfoBytes(), DisplayName: info.BestName()}.String()
	return magnet, mi, data
}

// startSeeder 在同一个进程里起一个只做种的 client（决议 T1）。
//
// 它同样不出网：DHT / tracker / PEX / 端口映射全关，唯一的对端是测试亲手
// 喂进去的那一个（见 linkSeeder）。
func startSeeder(t *testing.T, dataDir string, mi *metainfo.MetaInfo) *torrent.Torrent {
	t.Helper()
	cfg := torrent.NewDefaultClientConfig()
	cfg.DataDir = dataDir
	cfg.Seed = true
	cfg.NoDHT = true
	cfg.DisableTrackers = true
	cfg.DisablePEX = true
	cfg.NoDefaultPortForwarding = true
	cfg.ListenPort = 0
	cfg.Slogger = slog.New(slog.DiscardHandler)
	cfg.DefaultStorage = storage.NewFileOpts(storage.NewFileClientOpts{
		ClientBaseDir:   dataDir,
		PieceCompletion: storage.NewMapPieceCompletion(),
	})

	client, err := torrent.NewClient(cfg)
	require.NoError(t, err)
	t.Cleanup(func() { client.Close() })

	tor, err := client.AddTorrent(mi)
	require.NoError(t, err)
	<-tor.GotInfo()
	// 分片完成状态在内存里，新起的 client 并不知道盘上的数据是全的。
	// 不先校验一遍，做种方会认为自己什么都没有，下载方只能空等 ——
	// 那种失败会被误读成引擎的 bug。
	require.NoError(t, tor.VerifyData())
	require.Zero(t, tor.BytesMissing(), "做种方本地数据必须完整")
	return tor
}

// linkSeeder 把做种方接到引擎的 BT 监听端口上。
//
// 两边都关了 DHT 与 tracker，谁也发现不了谁 —— 这个端口号是它们之间唯一的
// 连接来源，因此这条链路一旦跑通，就同时证明了整组测试不依赖网络。
//
// 反复喂而不是喂一次：引擎要到 Prepare 里 AddMagnet 之后才认得这个 infohash，
// 在那之前握手会被拒；client 重建（改监听端口/做种开关）后端口也会变。
// anacrolix 对已连上的地址会跳过重连，重复喂是安全的。
func linkSeeder(t *testing.T, seedTor *torrent.Torrent, engine *Engine) {
	t.Helper()
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			if port := engine.listenPort(); port > 0 {
				seedTor.AddPeers([]torrent.PeerInfo{{
					Addr:    torrent.StringAddr(net.JoinHostPort("127.0.0.1", strconv.Itoa(port))),
					Source:  torrent.PeerSourceDirect,
					Trusted: true,
				}})
			}
			select {
			case <-stop:
				return
			case <-time.After(linkRetryInterval):
			}
		}
	}()
	t.Cleanup(func() {
		close(stop)
		<-done
	})
}

// offlineClient 是注入给引擎的 client 构造点：字段与生产版 newTorrentClient
// 一致，只额外关掉 DHT / tracker / PEX —— 生产默认配置带公共 DHT bootstrap
// 节点，跑测试绝不能真的往外发包。
func offlineClient(cacheDir string, cfg Config) (*torrent.Client, error) {
	tc := torrent.NewDefaultClientConfig()
	tc.DataDir = cacheDir
	tc.DefaultStorage = storage.NewFileOpts(storage.NewFileClientOpts{
		ClientBaseDir:   cacheDir,
		PieceCompletion: storage.NewMapPieceCompletion(),
	})
	tc.Seed = cfg.Seeding
	tc.ListenPort = cfg.ListenPort
	tc.NoDefaultPortForwarding = true
	tc.NoDHT = true
	tc.DisableTrackers = true
	tc.DisablePEX = true
	tc.Slogger = slog.New(slog.DiscardHandler)
	return torrent.NewClient(tc)
}

// newIntegrationEngine 起一个用「不出网 client」的引擎。
//
// ListenPort 固定给 0 由系统分配，再用 engine.listenPort() 读回来：写死端口
// 会在并发跑测试或本机端口被占时随机失败，而失败原因看起来会像引擎的问题。
// 注入是替换包级变量，恢复挂在 t.Cleanup 上 —— 这也是本文件不能并行的原因。
func newIntegrationEngine(t *testing.T, cacheDir string) *Engine {
	t.Helper()
	prev := newClient
	newClient = offlineClient
	t.Cleanup(func() { newClient = prev })

	engine, err := New(Options{
		CacheDir:   cacheDir,
		StreamBase: func() string { return testStreamBase },
		Config:     Config{ListenPort: 0},
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, engine.Close()) })
	return engine
}

// shortenWaits 把两段等待的上限调短，让失败路径的用例秒级返回而不是等满一分钟。
func shortenWaits(t *testing.T, metadata, buffer time.Duration) {
	t.Helper()
	prevMeta, prevBuf := metadataTimeout, bufferTimeout
	metadataTimeout, bufferTimeout = metadata, buffer
	t.Cleanup(func() { metadataTimeout, bufferTimeout = prevMeta, prevBuf })
}

// currentClient 在锁内读一眼引擎当前的 torrent client。
//
// 集成测试里有后台 goroutine（linkSeeder）在读同一个字段，裸读能过 -race 纯属
// 「同时只有读」的巧合；换个断言顺序就可能变成真竞争。测试自己先守规矩。
func currentClient(e *Engine) *torrent.Client {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.client
}

// containsHan 判断串里有没有汉字。用户可见提示必须是中文（不能漏成底层库的
// 英文错误），但具体措辞将来会调，所以只查「是不是中文」加关键词，不断整句。
func containsHan(s string) bool {
	for _, r := range s {
		if unicode.Is(unicode.Han, r) {
			return true
		}
	}
	return false
}

// prepareCtx 给【一次】准备用的超时上下文。给得比实测耗时宽得多：它是防挂死的
// 兜底，不是被测的时限；真正的时限断言写在各自的用例里。
//
// 每次 Prepare 都单独要一个，不要一个用例共用一份：共用等于把整份预算摊给
// 好几次准备，最后一次失败时看起来像「它卡住了」，其实是前面几次花光了预算。
//
// 为什么是 90 秒而不是 30：正常路径实测 1.5 秒左右，但 `-race` 叠上机器负载时，
// 这条「只有一个手工喂进去的 peer」的链路偶尔会显著变慢（实测 15 次里有 2 次
// 撞破 30 秒）。这个数字是挂死兜底，调宽它不会放过任何真的挂死，却能免掉一类
// 只在忙碌机器上出现、追起来毫无收获的 CI 假红。
func prepareCtx(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(cancel)
	return ctx
}

// ─────────────────────── 场景 1 / 2：单集端到端 + 流服务 ───────────────────────

func TestSingleEpisodeEndToEnd(t *testing.T) {
	const fileName = "[Nekomoe kissaten][Some Show][01][1080p][JPSC].mkv"

	seedDir := t.TempDir()
	cacheDir := filepath.Join(t.TempDir(), "torrent")
	magnet, mi, data := buildTestTorrent(t, seedDir, "", testFile{name: fileName, size: testEpisodeBytes})
	seedTor := startSeeder(t, seedDir, mi)

	engine := newIntegrationEngine(t, cacheDir)
	linkSeeder(t, seedTor, engine)

	ctx := prepareCtx(t)
	res, err := engine.Prepare(ctx, PrepareRequest{Magnet: magnet, FileIndex: -1})
	require.NoError(t, err)
	require.False(t, res.NeedSelection, "单集种子里只有一个候选，不该弹选集")
	require.NotNil(t, res.Source)

	infohash := mi.HashInfoBytes().HexString()
	item := res.Source.Item()
	assert.Equal(t, fileName, item.FileName)
	assert.Equal(t, fileName, item.RelativePath)
	require.NotNil(t, item.Episode, "解析链必须认出集号，否则弹幕与进度都会错位")
	assert.Equal(t, 1, *item.Episode)
	assert.Equal(t, int64(testEpisodeBytes), item.Size)
	assert.Equal(t, "t:"+infohash+"/0", item.FileID, "FileID 跨会话稳定，续播靠它对齐")
	assert.Equal(t, testStreamBase+"/t/"+infohash+"/0", res.Source.MPVPath())
	require.NoError(t, res.Source.Probe(ctx))

	// 状态在这里查：此刻刚缓冲完，phase 还停在 ready，尚未被后面的整文件读改写。
	st := engine.Status()
	assert.True(t, st.Active)
	assert.Equal(t, PhaseReady, st.Phase)
	assert.Equal(t, infohash, st.Infohash)
	assert.Equal(t, fileName, st.FileName)
	assert.Equal(t, float64(1), st.Buffered)
	assert.Positive(t, st.Progress)
	// 证明字节真的是从同进程的做种方那里换来的，用做种方的上传计数而不是
	// 快照里的 peer 数：anacrolix 默认开 DropMutuallyCompletePeers，双方都拿全
	// 之后连接会被主动断掉，那一刻 Status().Peers 归零是【对的】，拿它做断言必然抖。
	seedStats := seedTor.Stats()
	assert.Positive(t, seedStats.BytesWrittenData.Int64(),
		"字节只可能来自同进程的做种方（两边都不出网）")

	// Range 放在整文件读【之前】：整文件读一跑，全部分片就都在本地了，
	// 之后再拖进度条也碰不到「读一段还没下下来的数据」这条真正会阻塞的路。
	t.Run("Range 请求", func(t *testing.T) {
		// 这组覆盖「拖进度条」的真实路径：优先级窗口挪过去、reader seek、
		// 由 ServeContent 出 206。
		tests := []struct {
			name string
			from int64
			to   int64
		}{
			{"文件中段", 5 * 1024 * 1024, 6*1024*1024 - 1},
			// 尾部是 MKV 的 Cues（seek 索引）所在，顺序下载永远拿不到，
			// 靠的是启动期钉住尾 4MB 那条修正。
			{"文件尾部", testEpisodeBytes - 1024*1024, testEpisodeBytes - 1},
		}
		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				rec := doGet(engine.Handler(), "/t/"+infohash+"/0",
					fmt.Sprintf("bytes=%d-%d", tc.from, tc.to))
				require.Equal(t, http.StatusPartialContent, rec.Code)
				assert.Equal(t, fmt.Sprintf("bytes %d-%d/%d", tc.from, tc.to, testEpisodeBytes),
					rec.Header().Get("Content-Range"))
				require.Equal(t, int(tc.to-tc.from+1), rec.Body.Len())
				assert.True(t, bytes.Equal(data[fileName][tc.from:tc.to+1], rec.Body.Bytes()),
					"区间必须逐字节一致")
			})
		}
	})

	t.Run("弹幕哈希与本地文件完全相等", func(t *testing.T) {
		// 本组最值钱的一条：它同时证明了「头部 16MB 真的下下来了」与
		// 「本地文件和磁力两条来源共用同一份哈希实现」（决议 CQ2）。
		want, err := library.Hash16M(filepath.Join(seedDir, fileName))
		require.NoError(t, err)
		got, err := res.Source.Hash16M(ctx)
		require.NoError(t, err)
		assert.Equal(t, want, got)
	})

	t.Run("无 Range 时整文件逐字节一致", func(t *testing.T) {
		rec := doGet(engine.Handler(), "/t/"+infohash+"/0", "")
		require.Equal(t, http.StatusOK, rec.Code)
		require.Equal(t, len(data[fileName]), rec.Body.Len())
		// 不用 assert.Equal：断言失败时它会把 20MB 二进制打进测试输出。
		assert.True(t, bytes.Equal(data[fileName], rec.Body.Bytes()), "整文件必须逐字节一致")
	})

	t.Run("infohash 与当前会话不符时 404", func(t *testing.T) {
		rec := doGet(engine.Handler(), "/t/"+testInfohash+"/0", "")
		assert.Equal(t, http.StatusNotFound, rec.Code)
	})
}

func TestTorrentURLCandidateUsesDownloadedMetaInfo(t *testing.T) {
	const fileName = "[Nekomoe kissaten][Some Show][02][1080p][JPSC].mkv"

	seedDir := t.TempDir()
	cacheDir := filepath.Join(t.TempDir(), "torrent")
	_, metadata, _ := buildTestTorrent(t, seedDir, "", testFile{name: fileName, size: testEpisodeBytes})
	seedTorrent := startSeeder(t, seedDir, metadata)

	engine := newIntegrationEngine(t, cacheDir)
	fetches := 0
	engine.fetchMetaInfo = func(context.Context, string) (*metainfo.MetaInfo, error) {
		fetches++
		return metadata, nil
	}
	linkSeeder(t, seedTorrent, engine)

	result, err := engine.Prepare(prepareCtx(t), PrepareRequest{
		TorrentURL: "https://tracker.example/release.torrent", EpisodeHint: 2, FileIndex: -1,
	})
	require.NoError(t, err)
	require.NotNil(t, result.Source)
	assert.Equal(t, 1, fetches)
	assert.Equal(t, "t:"+metadata.HashInfoBytes().HexString()+"/0", result.Source.Item().FileID)
}

// ─────────────────────────── 场景 3：合集选集 ───────────────────────────

func TestPackSelection(t *testing.T) {
	const root = "[Nekomoe kissaten] Some Show [1080p][BDRip]"
	files := []testFile{
		// 种子内文件按路径字典序排列，因此下标是：0=.nfo（'S' < '['）、
		// 1/2/3=正片、4=NCOP。用例里的下标断言都建立在这个顺序上。
		{name: "Some Show.nfo", size: 2048},
		{name: "[Nekomoe kissaten][Some Show][01][1080p][JPSC].mkv", size: 1 << 20},
		{name: "[Nekomoe kissaten][Some Show][02][1080p][JPSC].mkv", size: 1<<20 + 11},
		{name: "[Nekomoe kissaten][Some Show][03][1080p][JPSC].mkv", size: 1<<20 + 22},
		{name: "[Nekomoe kissaten][Some Show][NCOP][1080p].mkv", size: 512 << 10},
	}

	seedDir := t.TempDir()
	magnet, mi, _ := buildTestTorrent(t, seedDir, root, files...)
	seedTor := startSeeder(t, seedDir, mi)

	packCacheDir := filepath.Join(t.TempDir(), "torrent")
	engine := newIntegrationEngine(t, packCacheDir)
	linkSeeder(t, seedTor, engine)

	infohash := mi.HashInfoBytes().HexString()

	res, err := engine.Prepare(prepareCtx(t), PrepareRequest{Magnet: magnet, FileIndex: -1})
	require.NoError(t, err)
	require.True(t, res.NeedSelection, "多集合集且没有集号提示时必须交给用户选")
	require.Nil(t, res.Source)
	require.Len(t, res.Files, 3, "非视频（.nfo）与花絮（NCOP）都不该出现在候选里")

	for i, choice := range res.Files {
		wantEpisode := i + 1
		// 第 N 集正好落在种子内下标 N 上（下标 0 是被滤掉的 .nfo）：
		// 候选按集号排序，Index 却必须是【种子内】的原始下标，
		// 用户选完回传的就是它，错位一格就会播成隔壁那一集。
		assert.Equal(t, wantEpisode, choice.Index)
		require.NotNil(t, choice.Episode)
		assert.Equal(t, wantEpisode, *choice.Episode)
		assert.Equal(t, files[wantEpisode].name, choice.Name)
		assert.Equal(t, root+"/"+files[wantEpisode].name, choice.Path)
		assert.Equal(t, int64(files[wantEpisode].size), choice.Size)
	}

	t.Run("用户在弹窗里选完重发", func(t *testing.T) {
		// 同一条磁力重发时会话被复用（种子不 drop），元数据不必再等一遍。
		res, err := engine.Prepare(prepareCtx(t), PrepareRequest{Magnet: magnet, FileIndex: 3})
		require.NoError(t, err)
		require.False(t, res.NeedSelection)
		require.NotNil(t, res.Source)
		item := res.Source.Item()
		assert.Equal(t, files[3].name, item.FileName)
		require.NotNil(t, item.Episode)
		assert.Equal(t, 3, *item.Episode)
		assert.Equal(t, "t:"+infohash+"/3", item.FileID)

		rec := doGet(engine.Handler(), "/t/"+infohash+"/3", "bytes=0-1023")
		require.Equal(t, http.StatusPartialContent, rec.Code)

		// 只下要播的这一集：其余文件保持 PiecePriorityNone。
		// 上界给得宽是有原因的 —— 与相邻文件共享的边界分片必须整片下下来，
		// 而 dirSize 统计的是文件【逻辑】长度，稀疏写会把相邻文件撑到它的
		// 名义大小；真正要挡住的是「把整个合集都拖下来」。
		assert.Less(t, dirSize(packCacheDir), int64(3<<20), "不该把整个合集都拖下来")
	})

	t.Run("带集号提示时直接选中", func(t *testing.T) {
		res, err := engine.Prepare(prepareCtx(t), PrepareRequest{Magnet: magnet, EpisodeHint: 2, FileIndex: -1})
		require.NoError(t, err)
		require.False(t, res.NeedSelection, "集号唯一命中就不该再打扰用户")
		require.NotNil(t, res.Source)
		item := res.Source.Item()
		assert.Equal(t, files[2].name, item.FileName)
		require.NotNil(t, item.Episode)
		assert.Equal(t, 2, *item.Episode)
		assert.Equal(t, "t:"+infohash+"/2", item.FileID)
	})
}

// ─────────────────────── 场景 4：生命周期（决议 M3-4 几乎不留） ───────────────────────

func TestPlaybackLifecycleLeavesNothingBehind(t *testing.T) {
	const fileName = "[Nekomoe kissaten][Some Show][05][1080p][JPSC].mkv"

	seedDir := t.TempDir()
	cacheDir := filepath.Join(t.TempDir(), "torrent")

	// 启动即清空：New 之前先在缓存目录里放上一次留下的垃圾。
	require.NoError(t, os.MkdirAll(filepath.Join(cacheDir, "上一次的种子"), 0o700))
	junk := filepath.Join(cacheDir, "上一次的种子", "残留分片.mkv")
	require.NoError(t, os.WriteFile(junk, make([]byte, 1<<20), 0o600))

	magnet, mi, _ := buildTestTorrent(t, seedDir, "", testFile{name: fileName, size: 3 << 20})
	seedTor := startSeeder(t, seedDir, mi)

	engine := newIntegrationEngine(t, cacheDir)
	_, statErr := os.Stat(junk)
	assert.True(t, os.IsNotExist(statErr), "启动即清空：残留分片对下一次播放没有价值")

	linkSeeder(t, seedTor, engine)
	res, err := engine.Prepare(prepareCtx(t), PrepareRequest{Magnet: magnet, FileIndex: -1})
	require.NoError(t, err)
	require.NotNil(t, res.Source)
	require.Positive(t, dirSize(cacheDir), "播起来之后缓存目录里应该有下载下来的分片")

	engine.Stop()
	// 按目录累计大小判断，不硬编码文件名：储存布局是 anacrolix 的实现细节。
	assert.Zero(t, dirSize(cacheDir), "停播即删（决议 M3-4）")
	assert.False(t, engine.Status().Active)
	assert.Equal(t, PhaseIdle, engine.Status().Phase)
	engine.Stop() // 没有会话时再停一次不能 panic

	require.NoError(t, engine.Close())
	_, statErr = os.Stat(cacheDir)
	assert.True(t, os.IsNotExist(statErr), "退出即清：整个缓存目录都不该留下")
	require.NoError(t, engine.Close(), "Close 幂等")
}

// ─────────────────────── 场景 5：失败可见性（决议 CQ3 错误不静默） ───────────────────────

func TestPrepareReportsMissingSeeders(t *testing.T) {
	// 把等元数据的上限调到 1 秒：这条路本来就永远等不到，没必要真等一分钟。
	shortenWaits(t, time.Second, 2*time.Second)

	cacheDir := filepath.Join(t.TempDir(), "torrent")
	engine := newIntegrationEngine(t, cacheDir)

	// 一条谁也没在做种的随机 infohash：引擎不出网、没有任何对端，
	// 元数据永远不会来 —— 正是「没人分享」在用户那边的样子。
	magnet := "magnet:?xt=urn:btih:" + strings.Repeat("ab", 20) + "&dn=nobody"
	start := time.Now()
	_, err := engine.Prepare(context.Background(), PrepareRequest{Magnet: magnet, FileIndex: -1})
	require.Error(t, err)
	assert.Less(t, time.Since(start), 10*time.Second, "必须在放宽后的超时内收敛")

	var e *errs.E
	require.ErrorAs(t, err, &e, "失败必须落进 internal/errors 的分类里")
	assert.Equal(t, errs.CategoryTorrent, e.Category, "这是 swarm 侧的问题，不是本机的")
	assert.True(t, containsHan(e.UserFacing()), "用户看到的必须是中文：%q", e.UserFacing())
	assert.Contains(t, e.UserFacing(), "分享者", "提示要说清是「没人在分享」而不是泛泛的失败")
	assert.NotEmpty(t, e.Recovery, "失败必须给出可执行的下一步")

	// 失败之后不能把种子和分片留在后台。
	assert.Empty(t, currentClient(engine).Torrents(), "失败后不该留下孤儿种子")
	assert.Zero(t, dirSize(cacheDir))
}

func TestPrepareReturnsPromptlyWhenCallerCancels(t *testing.T) {
	// 两段超时都留长：这样「及时返回」只可能来自 ctx 取消，
	// 不会被超时顺带掩盖过去。
	shortenWaits(t, 30*time.Second, 30*time.Second)

	cacheDir := filepath.Join(t.TempDir(), "torrent")
	engine := newIntegrationEngine(t, cacheDir)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// 等引擎真的进到「等元数据」再取消 —— 这才是前端点「取消」时的状态。
	// 轮询而不是固定 sleep：固定等待要么不够稳，要么白白拖慢整组测试。
	go func() {
		deadline := time.After(10 * time.Second)
		tick := time.NewTicker(time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-tick.C:
				if engine.Status().Phase == PhaseMetadata {
					cancel()
					return
				}
			case <-deadline:
				cancel()
				return
			}
		}
	}()

	magnet := "magnet:?xt=urn:btih:" + strings.Repeat("cd", 20) + "&dn=canceled"
	start := time.Now()
	_, err := engine.Prepare(ctx, PrepareRequest{Magnet: magnet, FileIndex: -1})
	require.Error(t, err)
	assert.Less(t, time.Since(start), 10*time.Second, "取消要立刻返回，而不是等满 30 秒超时")

	var e *errs.E
	require.ErrorAs(t, err, &e)
	assert.Equal(t, errs.CategoryTorrent, e.Category)
	assert.True(t, errors.Is(err, context.Canceled), "底层原因要能被 errors.Is 追到")
	assert.True(t, containsHan(e.UserFacing()), "用户看到的必须是中文：%q", e.UserFacing())

	// 用户取消后还在后台偷偷下载，是这条路上最容易漏掉的泄漏。
	assert.Empty(t, currentClient(engine).Torrents(), "取消后不该留下孤儿种子")
	assert.False(t, engine.Status().Active)
	assert.Equal(t, PhaseIdle, engine.Status().Phase)
	assert.Zero(t, dirSize(cacheDir))
}

// ─────────────────────────── 场景 6：配置生效 ───────────────────────────

func TestSetConfigAroundLivePlayback(t *testing.T) {
	const fileName = "[Nekomoe kissaten][Some Show][07][1080p][JPSC].mkv"

	seedDir := t.TempDir()
	magnet, mi, data := buildTestTorrent(t, seedDir, "", testFile{name: fileName, size: 3 << 20})
	seedTor := startSeeder(t, seedDir, mi)

	engine := newIntegrationEngine(t, filepath.Join(t.TempDir(), "torrent"))
	// 先牵线：linkSeeder 每轮都重读端口，client 被重建换了端口它也跟得上。
	linkSeeder(t, seedTor, engine)

	// 没有会话时改做种开关：就地重建 client，因此立即生效、不必提示重启。
	before := currentClient(engine)
	restart, err := engine.SetConfig(Config{Seeding: true, ListenPort: 0})
	require.NoError(t, err)
	assert.False(t, restart, "没在播的时候能就地重建，不该打扰用户重启")
	assert.NotSame(t, before, currentClient(engine), "client 应换成新配置建的那个")
	assert.False(t, engine.RestartRequired())

	ctx := prepareCtx(t)
	res, err := engine.Prepare(ctx, PrepareRequest{Magnet: magnet, FileIndex: -1})
	require.NoError(t, err)
	require.NotNil(t, res.Source)
	playing := currentClient(engine)

	// 正在播时改监听端口：重建会把这一集掐掉，所以改成提示重启。
	restart, err = engine.SetConfig(Config{Seeding: true, ListenPort: 51413})
	require.NoError(t, err)
	assert.True(t, restart)
	assert.Same(t, playing, currentClient(engine), "不能把正在播的这一集掐掉")
	assert.True(t, engine.RestartRequired())
	assert.True(t, engine.RestartRequired(), "提示必须活过一次页面刷新")

	// 这一集确实还在播，而且流端点仍然出得了字节。
	require.NoError(t, res.Source.Probe(ctx))
	st := engine.Status()
	assert.True(t, st.Active)
	assert.Equal(t, PhaseReady, st.Phase)

	infohash := mi.HashInfoBytes().HexString()
	rec := doGet(engine.Handler(), "/t/"+infohash+"/0", "bytes=0-4095")
	require.Equal(t, http.StatusPartialContent, rec.Code)
	assert.True(t, bytes.Equal(data[fileName][:4096], rec.Body.Bytes()), "改配置不该影响正在播的字节")
}
