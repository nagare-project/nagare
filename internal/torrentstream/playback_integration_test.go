// 做种的人少时边下边播不卡的端到端测试（决议 T1：同进程做种，不出网）。
//
// 与 integration_test.go 共用脚手架，因此同样【不能】并行：要替换包级的 newClient。
package torrentstream

import (
	"context"
	"io"
	"path/filepath"
	"testing"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/time/rate"
)

// 几个慢做种方合起来的速度够播，按顺序交给播放器的数据也不能一卡几十秒。
//
// mpv 的读法就是这样：起播后按顺序一路往前读，能读多快读多快（--demuxer-readahead-secs=60），
// 播放只在它预读的那 60 秒耗光时才会断。所以要紧的是「按顺序交出来的速度」够不够，几秒的
// 单次阻塞它扛得住。这里用一个按顺序尽快读的 reader 代替 mpv，量读完 24MB 要多久。
//
// 改动前（anacrolix 给每个 peer 一批与速度无关、最多约 15MB 的请求，整批收完才重排），
// 本场景六次：48–75 秒，单次阻塞最长 18–41 秒 —— 要的那一块挂在某个 peer 那一大批请求中间。
// 请求队列按速度封顶（pacer.go）之后二十多次：16–38 秒，最长阻塞 1–10 秒。
// 波动来自 anacrolix 做种方按 map 的随机顺序出货（真实 swarm 里的 libtorrent 系大多按请求顺序），
// 两条阈值都离改动后的最差值留了余量，宁可偶尔放过一次退化，也不让 CI 误报。
func TestPlaybackNotStalledBySlowPeersQueues(t *testing.T) {
	const (
		fileName   = "[Nekomoe kissaten][Some Show][11][1080p][JPSC].mkv"
		pieceLen   = 1 << 20 // 老番常见的分片大小
		fileBytes  = 96<<20 + 137
		readBytes  = 24 << 20
		seeders    = 3
		seederRate = 400 << 10 // 每个 400KB/s，合计 1.2MB/s：够播 1080p，但谁都不快
		maxElapsed = 55 * time.Second
		maxStall   = 25 * time.Second
	)

	seedDir := t.TempDir()
	magnet, mi, _ := buildTorrentFiles(t, seedDir, "", pieceLen, testFile{name: fileName, size: fileBytes})
	engine := newIntegrationEngine(t, filepath.Join(t.TempDir(), "torrent"))
	for range seeders {
		seedTor := startSeeder(t, seedDir, mi, func(cfg *torrent.ClientConfig) {
			cfg.UploadRateLimiter = rate.NewLimiter(rate.Limit(seederRate), 64<<10)
		})
		linkSeeder(t, seedTor, engine)
	}

	_, err := engine.Prepare(prepareCtx(t), PrepareRequest{Magnet: magnet, FileIndex: -1})
	require.NoError(t, err)
	engine.mu.Lock()
	sess := engine.sess
	engine.mu.Unlock()
	require.NotNil(t, sess)
	target := sess.target(mi.HashInfoBytes().HexString(), 0)
	require.NotNil(t, target)

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
	defer cancel()
	r, err := target.open(ctx, 0)
	require.NoError(t, err)
	defer r.Close()

	buf := make([]byte, 64<<10)
	var read int64
	var longest time.Duration
	start := time.Now()
	for read < readBytes {
		before := time.Now()
		n, err := r.Read(buf[:min(int64(len(buf)), readBytes-read)])
		longest = max(longest, time.Since(before))
		read += int64(n)
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
	}
	elapsed := time.Since(start)
	t.Logf("按顺序读 %dMB 用了 %v（%.0f KB/s），单次 Read 最长阻塞 %v",
		read>>20, elapsed.Round(100*time.Millisecond), float64(read)/1024/elapsed.Seconds(), longest.Round(10*time.Millisecond))
	assert.Equal(t, int64(readBytes), read)
	assert.Less(t, elapsed, maxElapsed, "按顺序交出来的速度远低于做种方合起来的速度：带宽花在了播放器暂时用不上的块上")
	assert.Less(t, longest, maxStall, "按顺序读的数据被某个 peer 手上排着的一大批请求卡住了")

	// 封顶的回调确实接上了：三个做种方都测出了速度
	pacer := currentTestPacer()
	require.NotNil(t, pacer)
	peers, measured := pacer.tracked()
	assert.GreaterOrEqual(t, measured, seeders, "每个送过数据的做种方都该测出速度（在测 %d 个）", peers)
	// 停播即断开这些连接，测速状态要跟着清掉，否则 map 只增不减
	require.NoError(t, r.Close())
	engine.Stop()
	assert.Eventually(t, func() bool {
		n, _ := pacer.tracked()
		return n == 0
	}, 10*time.Second, 50*time.Millisecond, "停播之后测速状态没有清掉")
}
