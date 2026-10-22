// 合集里换集的撤回与收尾（决议 T1：同进程做种，不出网）：撤回跟着 mpv 的真实读位置走、
// 两次接连被放弃的换集回到窗口在播的那一集、换集等缓冲时引擎被停掉。
//
// 与 integration_test.go 共用脚手架，因此同样【不能】并行：要替换包级的 newClient。
package torrentstream

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/time/rate"

	errs "github.com/nagare-project/nagare/internal/errors"
)

// slowSeederRate：调慢到第 2 集的门槛几分钟都到不齐，换集一定卡在等缓冲；没下完的分片在断言时也还没下完。
const slowSeederRate = rate.Limit(64 << 10)

// expireStartup 把 pm 的启动期拨到已经过去：启动期钉住的头尾覆盖了整个 20MB 样片，
// 不拨过去的话，断言看到的是钉住，而不是 reader 的窗口。
func expireStartup(pm *priorityManager) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	pm.gateOpenedAt = pm.gateOpenedAt.Add(-startupWindow - time.Second)
}

// openStream 在后台经流端点从 from 起读第 index 个文件（mpv 带着 Range 重连）。
// 返回的函数断开这个请求并等处理器返回；可重复调用。
func (p *switchPack) openStream(t *testing.T, index int, from int64) func() {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/t/"+p.ih+"/"+strconv.Itoa(index), nil).WithContext(ctx)
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-", from))
	done := make(chan struct{})
	go func() {
		defer close(done)
		p.engine.Handler().ServeHTTP(httptest.NewRecorder(), req)
	}()
	var once sync.Once
	stop := func() { once.Do(func() { cancel(); <-done }) }
	t.Cleanup(stop)
	return stop
}

// playFirst 播第 1 集，返回它的 Source 与优先级管理器。
func (p *switchPack) playFirst(t *testing.T) (*Source, *priorityManager) {
	t.Helper()
	res, err := p.engine.Prepare(prepareCtx(t), PrepareRequest{Magnet: p.magnet, FileIndex: 1})
	require.NoError(t, err)
	require.NotNil(t, res.Source)
	pm := currentPM(p.engine)
	require.NotNil(t, pm)
	return res.Source, pm
}

// startStuckSwitch 调慢做种，然后在后台从 from 往下一集换，等它进到等缓冲。返回换集的结果通道。
func (p *switchPack) startStuckSwitch(t *testing.T, ctx context.Context, from *Source) <-chan error {
	t.Helper()
	p.setRate(slowSeederRate)
	switched := make(chan error, 1)
	go func() {
		_, err := from.OpenSibling(ctx, 1)
		switched <- err
	}()
	require.Eventually(t, func() bool {
		st := p.engine.Status()
		return st.Phase == PhaseBuffering && st.FileName == switchPackFiles[2].name
	}, 10*time.Second, 5*time.Millisecond, "换集要进到等缓冲")
	return switched
}

// awaitSwitchError 等换集返回，断言它是分类错误、说法里有 want。
func awaitSwitchError(t *testing.T, switched <-chan error, want string) {
	t.Helper()
	select {
	case err := <-switched:
		var ce *errs.E
		require.ErrorAs(t, err, &ce)
		assert.Equal(t, errs.CategoryTorrent, ce.Category)
		assert.Contains(t, ce.UserMsg, want)
	case <-time.After(30 * time.Second):
		t.Fatal("换集要立刻返回")
	}
}

// 换集等缓冲时拖进度条（mpv 带着第 1 集的旧地址重连到新位置），随后换集被取消：换回来的窗口画在 mpv 的
// 真实读位置上。不跟着这个 reader 的话，换回来的管理器上一个 reader 都没有，读位置之后没下完的分片只剩 Normal。
func TestSwitchRevertFollowsReaderOnRetiredFile(t *testing.T) {
	p := seedSwitchPack(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	src, pmEp1 := p.playFirst(t)
	switched := p.startStuckSwitch(t, ctx, src)
	require.Same(t, pmEp1, retiredPM(p.engine))
	require.True(t, pmClosed(pmEp1))

	const pos = 9 << 20
	stop := p.openStream(t, 1, pos)
	require.Eventually(t, func() bool { return readerCount(pmEp1) == 1 }, 10*time.Second, 5*time.Millisecond,
		"被换走那一集的 reader 要登记在它停用了的管理器上")
	expireStartup(pmEp1)

	cancel()
	awaitSwitchError(t, switched, "已取消")
	require.Same(t, pmEp1, currentPM(p.engine), "回到第 1 集原来的窗口")
	require.False(t, pmClosed(pmEp1))

	// 窗口从 pos-1MB 起：Now 5MB，其后 Next 一直画到文件尾。anacrolix 自己的 reader 只把 [pos, pos+5MB)
	// 抬到 Readahead，所以 pos+5MB 之后那一段要是 Next 以上，只可能是这个窗口画的。
	tor, file := currentFile(p.engine)
	require.NotNil(t, file)
	pieceLen := tor.Info().PieceLength
	first := int((file.Offset()+pos+readerReadahead)/pieceLen) + 1
	end := min(file.EndPieceIndex(), tor.Files()[2].BeginPieceIndex()) // 与第 2 集共用的边界分片除外
	windowed := 0
	for idx := first; idx < end; idx++ {
		ps := tor.PieceState(idx)
		if ps.Complete {
			continue
		}
		windowed++
		assert.True(t, ps.Priority >= torrent.PiecePriorityNext, "片 %d 要在 mpv 读位置之后的窗口里，实际 %v", idx, ps.Priority)
	}
	require.Positive(t, windowed, "读位置之后还得有没下完的分片，断言才有意义")

	stop()
	assert.Zero(t, readerCount(pmEp1), "断开就注销，不留 reader")
}

// 两次换集接连被放弃：第一次换到了第 2 集、窗口没跟上，它的 Discard 还没跑，用户又在窗口还在播的第 1 集上
// 按了一次下一集（第 2 集已经是活的，不用重来），这次也没换上。两个 Discard 谁先谁后，种子都回到窗口在播的
// 第 1 集、它原来的窗口重新启用，第 2 集不再下载。
func TestDoubleAbandonedSwitchReturnsToWindowFile(t *testing.T) {
	p := seedSwitchPack(t)
	srcEp1, pmEp1 := p.playFirst(t)

	first, err := srcEp1.OpenSibling(prepareCtx(t), 1)
	require.NoError(t, err)
	pmEp2 := currentPM(p.engine)
	second, err := srcEp1.OpenSibling(prepareCtx(t), 1)
	require.NoError(t, err)
	require.Same(t, pmEp2, currentPM(p.engine), "第 2 集已经是活的：不用重来")
	p.setRate(slowSeederRate) // 断言时两集都还有没下完的分片

	var wg sync.WaitGroup
	for _, src := range []*Source{first, second} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			src.Discard()
		}()
	}
	wg.Wait()

	st := p.engine.Status()
	assert.True(t, st.Active)
	assert.Equal(t, PhaseReady, st.Phase)
	assert.Equal(t, switchPackFiles[1].name, st.FileName, "种子回到窗口在播的第 1 集")
	assert.Same(t, pmEp1, currentPM(p.engine), "第 1 集原来的窗口重新启用")
	assert.False(t, pmClosed(pmEp1))
	assert.True(t, pmClosed(pmEp2))
	p.assertServes(t, 1, "窗口在播的那一集")
	p.assertNotServed(t, 2, "谁都不播的第 2 集不再服务")

	tor, file1 := currentFile(p.engine)
	file2 := tor.Files()[2]
	// 两头与相邻文件共用的边界分片除外
	wanted := 0
	for idx := file1.BeginPieceIndex() + 1; idx < file1.EndPieceIndex()-1; idx++ {
		if ps := tor.PieceState(idx); !ps.Complete {
			wanted++
			assert.NotEqual(t, torrent.PiecePriorityNone, ps.Priority, "第 1 集的片 %d 要重新可请求", idx)
		}
	}
	require.Positive(t, wanted, "第 1 集还得有没下完的分片，断言才有意义")
	idle := assertIdle(t, tor, file2.BeginPieceIndex()+1, file2.EndPieceIndex(), "第 2 集不该再下")
	require.Positive(t, idle, "第 2 集还得有没下完的分片，断言才有意义")
}

// 换集正在等缓冲时引擎停了（网页上点了停止 / 退出 nagare）：换集很快以「种子已经停了」返回，
// 不会把会话复活，种子已经丢掉，什么都不再下载。
func TestEngineStopDuringSwitchEndsIt(t *testing.T) {
	tests := []struct {
		name string
		stop func(t *testing.T, e *Engine)
	}{
		{"网页上停播", func(_ *testing.T, e *Engine) { e.Stop() }},
		{"退出", func(t *testing.T, e *Engine) { require.NoError(t, e.Close()) }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := seedSwitchPack(t)
			src, _ := p.playFirst(t)
			switched := p.startStuckSwitch(t, context.Background(), src)
			tor, _ := currentFile(p.engine)
			require.NotNil(t, tor)

			start := time.Now()
			tc.stop(t, p.engine)
			awaitSwitchError(t, switched, "种子已经停了")
			assert.Less(t, time.Since(start), 10*time.Second, "不能等满缓冲上限")

			st := p.engine.Status()
			assert.False(t, st.Active, "换集不能把会话复活")
			assert.Equal(t, PhaseIdle, st.Phase)
			assert.Nil(t, engineSession(p.engine))
			select {
			case <-tor.Closed():
			default:
				t.Error("种子要被丢掉：不然它还在后台下载")
			}
			if client := currentClient(p.engine); client != nil {
				assert.Empty(t, client.Torrents(), "不留下任何种子")
			}
			assert.Error(t, src.Probe(context.Background()))
			p.assertNotServed(t, 1, "停了之后哪一集都不服务")
			p.assertNotServed(t, 2, "停了之后哪一集都不服务")
		})
	}
}

// retiredPM 在锁内取当前会话里被换走那一集的优先级管理器。
func retiredPM(e *Engine) *priorityManager {
	sess := engineSession(e)
	if sess == nil {
		return nil
	}
	sess.mu.Lock()
	defer sess.mu.Unlock()
	if sess.retired == nil {
		return nil
	}
	return sess.retired.pm
}

// engineSession 在锁内取引擎当前的会话。
func engineSession(e *Engine) *session {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.sess
}
