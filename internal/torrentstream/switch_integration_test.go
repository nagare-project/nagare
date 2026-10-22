// 合集里换集的端到端测试（决议 T1：同进程做种，不出网）。
//
// 与 integration_test.go 共用脚手架，因此同样【不能】并行：要替换包级的 newClient。
package torrentstream

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/time/rate"

	errs "github.com/nagare-project/nagare/internal/errors"
)

const (
	switchPackRoot = "[Grp] Show [1080p]"
	// switchEpisodeBytes：比 8MB 门槛大得多，换集那一刻被换走的那一集还有一截没下完，
	// 「旧窗口停用」才看得出来；尾巴不对齐分片，边界分片是两集共用的。
	switchEpisodeBytes = 20<<20 + 137
	// switchSeederRate：两个做种方各限这么多，门槛要一秒多才到齐，换集前后谁在下什么看得清。
	switchSeederRate = 3 << 20
)

// switchPackFiles 是测试合集。种子内按路径排序，下标是：0=.nfo（'S' < '['）、1/2/3=第 1/2/3 集、4=NCOP。
var switchPackFiles = []testFile{
	{name: "Show.nfo", size: 2048},
	{name: "[Grp] Show - 01 [1080p].mkv", size: switchEpisodeBytes},
	{name: "[Grp] Show - 02 [1080p].mkv", size: switchEpisodeBytes + 11},
	{name: "[Grp] Show - 03 [1080p].mkv", size: switchEpisodeBytes + 22},
	{name: "[Grp] Show - NCOP [1080p].mkv", size: 512 << 10},
}

type switchPack struct {
	engine   *Engine
	magnet   string
	ih       string
	data     map[string][]byte
	limiters []*rate.Limiter
}

// seedSwitchPack 造合集、起两个限速的做种方并接到引擎上。限速器留着：用例可以中途调慢。
func seedSwitchPack(t *testing.T) *switchPack {
	t.Helper()
	seedDir := t.TempDir()
	magnet, mi, data := buildTestTorrent(t, seedDir, switchPackRoot, switchPackFiles...)
	p := &switchPack{magnet: magnet, ih: mi.HashInfoBytes().HexString(), data: data}
	p.engine = newIntegrationEngine(t, filepath.Join(t.TempDir(), "torrent"))
	for range 2 {
		limiter := rate.NewLimiter(rate.Limit(switchSeederRate), 256<<10)
		p.limiters = append(p.limiters, limiter)
		// 做种方只读不写，共用同一份数据目录。
		seedTor := startSeeder(t, seedDir, mi, func(cfg *torrent.ClientConfig) {
			cfg.UploadRateLimiter = limiter
		})
		linkSeeder(t, seedTor, p.engine)
	}
	return p
}

func (p *switchPack) setRate(limit rate.Limit) {
	for _, l := range p.limiters {
		l.SetLimit(limit)
	}
}

// fileData 是第 index 个文件的原始内容。
func (p *switchPack) fileData(index int) []byte {
	return p.data[switchPackRoot+"/"+switchPackFiles[index].name]
}

// assertServes 断言第 index 个文件的头 4KB 能经流端点拿到，且逐字节一致。
func (p *switchPack) assertServes(t *testing.T, index int, msg string) {
	t.Helper()
	rec := doGet(p.engine.Handler(), "/t/"+p.ih+"/"+strconv.Itoa(index), "bytes=0-4095")
	require.Equal(t, http.StatusPartialContent, rec.Code, msg)
	assert.True(t, bytes.Equal(p.fileData(index)[:4096], rec.Body.Bytes()), msg)
}

func (p *switchPack) assertNotServed(t *testing.T, index int, msg string) {
	t.Helper()
	rec := doGet(p.engine.Handler(), "/t/"+p.ih+"/"+strconv.Itoa(index), "bytes=0-4095")
	assert.Equal(t, http.StatusNotFound, rec.Code, msg)
}

// currentPM 在锁内取当前会话的优先级管理器。
func currentPM(e *Engine) *priorityManager {
	e.mu.Lock()
	sess := e.sess
	e.mu.Unlock()
	if sess == nil {
		return nil
	}
	sess.mu.Lock()
	defer sess.mu.Unlock()
	return sess.pm
}

func pmClosed(pm *priorityManager) bool {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	return pm.closed
}

// assertIdle 断言种子里 [first, end) 这些还没下完的分片都不再可请求，返回其中没下完的片数。
func assertIdle(t *testing.T, tor *torrent.Torrent, first, end int, msg string) int {
	t.Helper()
	incomplete := 0
	for idx := first; idx < end; idx++ {
		ps := tor.PieceState(idx)
		if ps.Complete {
			continue
		}
		incomplete++
		assert.Equal(t, torrent.PiecePriorityNone, ps.Priority, "%s：片 %d", msg, idx)
	}
	return incomplete
}

// switchSeen 是换集期间轮询状态看到的东西。
type switchSeen struct {
	inactive, buffering bool
}

// watchSwitch 在后台按 5ms 轮询引擎状态，直到返回的函数被调用。
func watchSwitch(e *Engine) func() switchSeen {
	stop := make(chan struct{})
	out := make(chan switchSeen, 1)
	go func() {
		var seen switchSeen
		tick := time.NewTicker(5 * time.Millisecond)
		defer tick.Stop()
		for {
			st := e.Status()
			seen.inactive = seen.inactive || !st.Active
			seen.buffering = seen.buffering || st.Phase == PhaseBuffering
			select {
			case <-stop:
				out <- seen
				return
			case <-tick.C:
			}
		}
	}()
	return func() switchSeen {
		close(stop)
		return <-out
	}
}

// 合集里换到下一集：新的一集缓冲到能起播才返回，FileID 与流地址指向它，目录作品身份照抄；
// 换集期间会话一直是活的；被换走那一集的窗口停用、却仍能服务。预先取消的换集回到原来那一集；
// 再往回切到刚被换走的那一集也行。
func TestPackSwitchToSiblingKeepsOldFileServable(t *testing.T) {
	p := seedSwitchPack(t)
	res, err := p.engine.Prepare(prepareCtx(t), PrepareRequest{Magnet: p.magnet, FileIndex: 1})
	require.NoError(t, err)
	require.NotNil(t, res.Source)
	src1 := res.Source.WithMatchHints(42, []string{"测试番剧"})

	prev, next := src1.Siblings()
	assert.Nil(t, prev, "第 1 集没有上一集")
	require.NotNil(t, next)
	assert.Equal(t, 2, next.Index, "NCOP 与 .nfo 不算邻居")
	require.NotNil(t, next.Episode)
	assert.Equal(t, 2, *next.Episode)

	oldPM := currentPM(p.engine)
	require.NotNil(t, oldPM)
	stopWatch := watchSwitch(p.engine)
	src2, err := src1.OpenSibling(prepareCtx(t), 1)
	seen := stopWatch()
	require.NoError(t, err)

	assert.Equal(t, "t:"+p.ih+"/2", src2.Item().FileID)
	assert.Equal(t, testStreamBase+"/t/"+p.ih+"/2", src2.MPVPath())
	id, titles := src2.MatchHints()
	assert.Equal(t, 42, id, "目录作品身份照抄：还是同一部作品")
	assert.Equal(t, []string{"测试番剧"}, titles)
	assert.False(t, seen.inactive, "换集期间会话一直是活的：网页上的状态条不能收摊")
	assert.True(t, seen.buffering, "换集期间要报缓冲中")
	st := p.engine.Status()
	assert.True(t, st.Active)
	assert.Equal(t, PhaseReady, st.Phase)
	assert.Equal(t, switchPackFiles[2].name, st.FileName)

	// 被换走那一集的窗口停用：没下完的分片不再可请求（与第 2 集共用的边界分片除外）
	assert.True(t, pmClosed(oldPM))
	tor, file2 := currentFile(p.engine)
	require.NotNil(t, file2)
	ep1 := tor.Files()[1]
	left := assertIdle(t, tor, ep1.BeginPieceIndex(), min(ep1.EndPieceIndex(), file2.BeginPieceIndex()), "被换走那一集的分片不该再下")
	t.Logf("换集之后第 1 集还有 %d 片没下完（都已不可请求）", left)

	p.assertServes(t, 2, "新的一集能播")
	p.assertServes(t, 1, "被换走那一集仍能服务：换集准备期间拖进度条不能 404")
	require.NoError(t, src1.Probe(context.Background()))
	p.assertNotServed(t, 3, "没切过去的那一集不服务")

	prev, next = src2.Siblings()
	require.NotNil(t, prev)
	require.NotNil(t, next)
	assert.Equal(t, 1, prev.Index)
	assert.Equal(t, 3, next.Index)

	// 下面几个子用例是依次走下去的同一个会话（每一步建立在上一步的状态上），不能单独跑。
	var back, third *Source
	var pmEp1 *priorityManager

	t.Run("预先取消的换集回到原来那一集", func(t *testing.T) {
		pm2 := currentPM(p.engine)
		canceled, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := src2.OpenSibling(canceled, 1)
		var ce *errs.E
		require.ErrorAs(t, err, &ce)
		assert.Equal(t, errs.CategoryTorrent, ce.Category)
		assert.True(t, errors.Is(err, context.Canceled), "底层原因要能被 errors.Is 追到")
		assert.True(t, containsHan(ce.UserMsg), ce.UserMsg)

		st := p.engine.Status()
		assert.True(t, st.Active)
		assert.Equal(t, PhaseReady, st.Phase)
		assert.Equal(t, switchPackFiles[2].name, st.FileName)
		assert.Same(t, pm2, currentPM(p.engine), "回到原来那一集的窗口，不从起播状态重来")
		assert.False(t, pmClosed(pm2))
		p.assertServes(t, 2, "原来那一集照常服务")
		p.assertServes(t, 1, "换集之前还能服务的也照旧")
		p.assertNotServed(t, 3, "没切成的那一集不服务")
		tor, file2 := currentFile(p.engine)
		ep3 := tor.Files()[3]
		assertIdle(t, tor, max(ep3.BeginPieceIndex(), file2.EndPieceIndex()), ep3.EndPieceIndex()-1, "没切成的那一集不该再下")
	})

	t.Run("窗口没跟上时再切：要切去的那一集已经是活的", func(t *testing.T) {
		pm2 := currentPM(p.engine)
		again, err := src1.OpenSibling(prepareCtx(t), 1)
		require.NoError(t, err)
		assert.Equal(t, "t:"+p.ih+"/2", again.Item().FileID)
		assert.Same(t, pm2, currentPM(p.engine), "不用重来")
		p.assertServes(t, 1, "窗口还在播的那一集照常服务")
	})

	t.Run("往回切到刚被换走的那一集", func(t *testing.T) {
		var err error
		back, err = src2.OpenSibling(prepareCtx(t), -1)
		require.NoError(t, err)
		pmEp1 = currentPM(p.engine)
		assert.Equal(t, "t:"+p.ih+"/1", back.Item().FileID)
		p.assertServes(t, 1, "切回来的那一集")
		p.assertServes(t, 2, "刚被换走的第 2 集仍能服务")
		prev, next := back.Siblings()
		assert.Nil(t, prev)
		require.NotNil(t, next)
		assert.Equal(t, 2, next.Index)
	})

	t.Run("从被换走的那一集再切：留着能服务的是窗口还在播的那一集", func(t *testing.T) {
		// 此刻活着的是第 1 集、第 2 集被换走了；假装窗口没跟上，还在播第 2 集，从它切下一集
		var err error
		third, err = src2.OpenSibling(prepareCtx(t), 1)
		require.NoError(t, err)
		assert.Equal(t, "t:"+p.ih+"/3", third.Item().FileID)
		p.assertServes(t, 3, "新的一集")
		p.assertServes(t, 2, "窗口还在播的第 2 集不能 404")
		p.assertNotServed(t, 1, "谁都不播的第 1 集不再服务")
	})

	t.Run("窗口最终没换上：Discard 把种子换回去，迟到的 Discard 不撤后来的换集", func(t *testing.T) {
		back.Discard() // 之后又换过文件：过时了，什么都不做
		assert.Equal(t, switchPackFiles[3].name, p.engine.Status().FileName)

		third.Discard()
		st := p.engine.Status()
		assert.True(t, st.Active)
		assert.Equal(t, PhaseReady, st.Phase)
		assert.Equal(t, switchPackFiles[1].name, st.FileName, "回到换集之前活着的第 1 集")
		assert.Same(t, pmEp1, currentPM(p.engine), "它的窗口原样重新启用")
		assert.False(t, pmClosed(pmEp1))
		p.assertServes(t, 1, "换回来的那一集")
		p.assertServes(t, 2, "换集之前还能服务的也照旧")
		p.assertNotServed(t, 3, "没换上去的那一集不服务")

		third.Discard() // 重复调用无副作用
		assert.Same(t, pmEp1, currentPM(p.engine))
	})
}

// 换集正在等缓冲时网页上又点了播放：Prepare 在抢 prepMu 之前先收掉会话，换集立刻以「种子已经停了」
// 返回，Prepare 不被晾在锁上；旧的 Source 随即不可服务。
func TestWebPrepareDuringSwitchPreemptsIt(t *testing.T) {
	p := seedSwitchPack(t)
	res, err := p.engine.Prepare(prepareCtx(t), PrepareRequest{Magnet: p.magnet, FileIndex: 1})
	require.NoError(t, err)

	// 调慢到第 2 集的门槛几分钟都到不齐：换集一定还卡在等缓冲
	p.setRate(rate.Limit(64 << 10))
	switched := make(chan error, 1)
	go func() {
		_, err := res.Source.OpenSibling(context.Background(), 1)
		switched <- err
	}()
	require.Eventually(t, func() bool {
		st := p.engine.Status()
		return st.Phase == PhaseBuffering && st.FileName == switchPackFiles[2].name
	}, 10*time.Second, 5*time.Millisecond, "换集要进到等缓冲")

	type prepared struct {
		res PrepareResult
		err error
	}
	ctx := prepareCtx(t)
	done := make(chan prepared, 1)
	start := time.Now()
	go func() {
		r, err := p.engine.Prepare(ctx, PrepareRequest{Magnet: p.magnet, FileIndex: 3})
		done <- prepared{r, err}
	}()
	select {
	case err := <-switched:
		var ce *errs.E
		require.ErrorAs(t, err, &ce)
		assert.Equal(t, errs.CategoryTorrent, ce.Category)
		assert.Contains(t, ce.UserMsg, "种子已经停了")
		assert.NotEmpty(t, ce.Recovery)
		assert.Less(t, time.Since(start), 10*time.Second)
	case <-time.After(30 * time.Second):
		t.Fatal("网页上再点播放要立刻掐掉换集")
	}

	p.setRate(rate.Inf)
	got := <-done
	require.NoError(t, got.err)
	require.NotNil(t, got.res.Source)
	assert.Equal(t, "t:"+p.ih+"/3", got.res.Source.Item().FileID)
	assert.Error(t, res.Source.Probe(context.Background()), "旧会话已经结束")
	p.assertServes(t, 3, "新会话照常服务")
	p.assertNotServed(t, 1, "旧会话的文件不再服务")
}
