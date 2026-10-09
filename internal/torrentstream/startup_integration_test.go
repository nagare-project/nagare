// 起播门槛先到的端到端测试（决议 T1：同进程做种，不出网）。
//
// 与 integration_test.go 共用脚手架，因此同样【不能】并行：要替换包级的 newClient。
package torrentstream

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/time/rate"
)

// headMarks 是头部三段各自到齐的时刻（从开始准备算起）。
type headMarks struct {
	gate, head, tail time.Duration
	// restAtGate 是门槛到齐那一刻，头部余下 [门槛, 16MB) 已经到齐的分片比例。
	restAtGate float64
	// receivedAtGate 是看到门槛到齐那一刻一共收到的有效字节（只用于日志）。
	receivedAtGate int64
	// receivedBuffering 是门槛放行之前收到的有效字节：超出门槛的部分都是和门槛抢了带宽的。
	receivedBuffering int64
}

// pieceSpan 是文件内 [lo, hi) 覆盖的分片区间 [first, last]。
func pieceSpan(file *torrent.File, pieceLen, lo, hi int64) (first, last int) {
	return int((file.Offset() + lo) / pieceLen), int((file.Offset() + hi - 1) / pieceLen)
}

// countDone 数 [first, last] 里没有缺块的分片。
func countDone(tor *torrent.Torrent, first, last int) int {
	n := 0
	for idx := first; idx <= last; idx++ {
		if tor.PieceBytesMissing(idx) == 0 {
			n++
		}
	}
	return n
}

// currentFile 在锁内取出当前会话正在播的种子与文件；还没选定文件时返回 nil。
func currentFile(e *Engine) (*torrent.Torrent, *torrent.File) {
	e.mu.Lock()
	sess := e.sess
	e.mu.Unlock()
	if sess == nil {
		return nil, nil
	}
	sess.mu.Lock()
	defer sess.mu.Unlock()
	return sess.tor, sess.file
}

// gateOpened 报告当前会话的起播门槛是否已经放行。
func gateOpened(e *Engine) bool {
	e.mu.Lock()
	sess := e.sess
	e.mu.Unlock()
	if sess == nil {
		return false
	}
	sess.mu.Lock()
	pm := sess.pm
	sess.mu.Unlock()
	if pm == nil {
		return false
	}
	pm.mu.Lock()
	defer pm.mu.Unlock()
	return !pm.gateOpenedAt.IsZero()
}

// watchHead 在后台按 5ms 轮询，记下门槛、16MB 头部、尾部各自到齐的时刻。
//
// 不拿 Prepare 返回的时刻当门槛时刻：waitBuffered 每 250ms 才看一次，那点量化误差
// 在限速下就是一两片分片，足以把「门槛到齐时头部余下部分到了多少」量歪。
func watchHead(ctx context.Context, engine *Engine, start time.Time) <-chan headMarks {
	out := make(chan headMarks, 1)
	go func() {
		var m headMarks
		tick := time.NewTicker(5 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
			}
			tor, file := currentFile(engine)
			if tor == nil || file == nil || tor.Info() == nil {
				continue
			}
			pieceLen := tor.Info().PieceLength
			length := file.Length()
			gateFirst, gateLast := pieceSpan(file, pieceLen, 0, bufferStartBytes)
			restFirst, headLast := pieceSpan(file, pieceLen, bufferStartBytes, startupHeadBytes)
			tailFirst, tailLast := pieceSpan(file, pieceLen, length-startupTailBytes, length)
			now := time.Since(start)
			// 先读字节、再确认门槛还没放行：字节只增不减，读完之后才放行的话，
			// 读到的这个数一定是放行之前收到的。
			if received := usefulBytesRead(tor); !gateOpened(engine) {
				m.receivedBuffering = max(m.receivedBuffering, received)
			}
			if m.gate == 0 && countDone(tor, gateFirst, gateLast) == gateLast-gateFirst+1 {
				m.gate = now
				m.restAtGate = float64(countDone(tor, restFirst, headLast)) / float64(headLast-restFirst+1)
				m.receivedAtGate = usefulBytesRead(tor)
			}
			if m.head == 0 && countDone(tor, gateFirst, headLast) == headLast-gateFirst+1 {
				m.head = now
			}
			if m.tail == 0 && countDone(tor, tailFirst, tailLast) == tailLast-tailFirst+1 {
				m.tail = now
			}
			if m.gate > 0 && m.head > 0 && m.tail > 0 {
				out <- m
				return
			}
		}
	}()
	return out
}

// 起播门槛 [0, 8MB) 必须先于头部余下的 [8MB, 16MB) 到齐，而不是和它们平级、
// 按 anacrolix 的随机分片顺序一起下 —— 原先整个 16MB 头部都是 Now，门槛实际要等
// 头部下到九成五以上，起播慢了将近一倍。
//
// 做种方限速，让「谁先到」在时间轴上拉开；起多个做种方，门槛的块分散在几个对端手上
// 才像真实 swarm。anacrolix 做种时按 map 遍历的随机顺序处理手上的请求 —— 这恰好把
// 「同一批请求里混进低档块会拖慢门槛」放大成看得见的差别（见 session.openGate）。
func TestStartGateArrivesBeforeRestOfHead(t *testing.T) {
	const (
		fileName   = "[Nekomoe kissaten][Some Show][09][1080p][JPSC].mkv"
		fileBytes  = 24<<20 + 137 // 头 16MB + 中段 + 尾 4MB，尾巴不对齐分片
		seeders    = 4
		seederRate = 1 << 20 // 每个做种方 1MB/s，合计 4MB/s
	)

	seedDir := t.TempDir()
	magnet, mi, _ := buildTestTorrent(t, seedDir, "", testFile{name: fileName, size: fileBytes})
	engine := newIntegrationEngine(t, filepath.Join(t.TempDir(), "torrent"))
	for range seeders {
		// 做种方只读不写，共用同一份数据目录。
		seedTor := startSeeder(t, seedDir, mi, func(cfg *torrent.ClientConfig) {
			cfg.UploadRateLimiter = rate.NewLimiter(rate.Limit(seederRate), 256<<10)
		})
		linkSeeder(t, seedTor, engine)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
	defer cancel()
	start := time.Now()
	marksCh := watchHead(ctx, engine, start)
	res, err := engine.Prepare(prepareCtx(t), PrepareRequest{Magnet: magnet, FileIndex: -1})
	require.NoError(t, err)
	require.NotNil(t, res.Source)

	var m headMarks
	select {
	case m = <-marksCh:
	case <-ctx.Done():
		t.Fatal("头部与尾部迟迟没有到齐")
	}
	t.Logf("门槛 8MB 到齐 %v（此刻共收到 %.1fMB，头部余下部分到齐 %.0f%%），16MB 头部到齐 %v，尾部到齐 %v",
		m.gate.Round(time.Millisecond), float64(m.receivedAtGate)/(1<<20), m.restAtGate*100,
		m.head.Round(time.Millisecond), m.tail.Round(time.Millisecond))

	// 最硬的一条，与机器快慢无关：缓冲期只有门槛可请求，放行之前收到的就只是门槛。
	// 原先 16MB 头部全是 Now，门槛到齐时已经收了 16MB 上下。
	require.Positive(t, m.receivedBuffering, "缓冲期间至少该采到一次样")
	assert.LessOrEqual(t, m.receivedBuffering, int64(bufferStartBytes),
		"门槛放行之前收到的字节不该比门槛多：多出来的都在和门槛抢带宽")
	assert.LessOrEqual(t, m.restAtGate, 0.5,
		"门槛到齐时头部余下部分不该已经下完：它们本该排在门槛之后")
	// 只断言先后，不断言倍数：计时从连上之前开始，CI 负载高时建链开销会吃掉倍数的余量；
	// 「门槛之前只收门槛」那一条已经把带宽分配钉死了
	assert.Less(t, m.gate, m.head, "门槛应早于整个 16MB 头部到齐（门槛 %v，头部 %v）", m.gate, m.head)
	st := engine.Status()
	assert.Equal(t, float64(1), st.Buffered, "门槛放行后界面进度是 100%")
}

// 大分片（这里 4MB，合集常见 8–16MB）下门槛按整片判定，等待期间界面进度不能
// 一直停在 0%：要把未完成分片里已经到手的块按比例算进去，且放行之前封顶在 99%。
func TestGateProgressCountsPartialPieces(t *testing.T) {
	const fileName = "[Nekomoe kissaten][Some Show][10][1080p][JPSC].mkv"
	seedDir := t.TempDir()
	magnet, mi, _ := buildTestTorrentWithPieceLength(t, seedDir, 4<<20,
		testFile{name: fileName, size: 12<<20 + 99})
	engine := newIntegrationEngine(t, filepath.Join(t.TempDir(), "torrent"))
	seedTor := startSeeder(t, seedDir, mi, func(cfg *torrent.ClientConfig) {
		cfg.UploadRateLimiter = rate.NewLimiter(rate.Limit(2<<20), 256<<10)
	})
	linkSeeder(t, seedTor, engine)

	done := make(chan error, 1)
	go func() {
		_, err := engine.Prepare(prepareCtx(t), PrepareRequest{Magnet: magnet, FileIndex: -1})
		done <- err
	}()

	// 门槛是头两片（8MB）。在两片都还没完整到齐的时候，进度就该已经动了。
	var sawPartial bool
	// 150 秒：单个做种方时 anacrolix 的写入唤醒偶有丢失（peer-conn-msg-writer.go 先填缓冲再取 Signaled），
	// 那条连接要等 60 秒保活才恢复 —— 这是上游的竞态，不是这里要测的东西，期限不能比它短
	deadline := time.After(150 * time.Second)
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for waiting := true; waiting; {
		select {
		case err := <-done:
			require.NoError(t, err)
			waiting = false
			continue
		case <-deadline:
			t.Fatal("起播缓冲迟迟没有完成")
		case <-tick.C:
		}
		st := engine.Status()
		tor, file := currentFile(engine)
		if st.Phase != PhaseBuffering || tor == nil || file == nil {
			continue
		}
		// 先取进度、后数分片：分片只会从缺到齐，后数时还缺，取进度那一刻也一定缺。
		first, last := pieceSpan(file, tor.Info().PieceLength, 0, bufferStartBytes)
		whole := countDone(tor, first, last)
		if whole < last-first+1 {
			require.Less(t, st.Buffered, float64(1), "门槛放行之前界面进度不能到 100%")
		}
		if st.Buffered > 0 && whole == 0 {
			sawPartial = true
		}
	}
	assert.True(t, sawPartial, "门槛内一片都没到齐时，进度就该按已到的块往前走")
	assert.Equal(t, float64(1), engine.Status().Buffered)
}
