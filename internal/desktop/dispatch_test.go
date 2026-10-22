package desktop

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeUI 是一个假的界面线程：wake 往通道里投一条，loop 取到就 drain（与 Windows 的消息循环同形）。
type fakeUI struct {
	q     *uiQueue
	msgs  chan struct{}
	wakes atomic.Int32
	fail  atomic.Bool // 模拟 PostMessage 失败
	stop  chan struct{}
	done  chan struct{}
}

func newFakeUI(q *uiQueue) *fakeUI {
	return &fakeUI{q: q, msgs: make(chan struct{}, 16), stop: make(chan struct{}), done: make(chan struct{})}
}

func (u *fakeUI) wake() bool {
	if u.fail.Load() {
		return false
	}
	u.wakes.Add(1)
	u.msgs <- struct{}{}
	return true
}

func (u *fakeUI) run() {
	defer close(u.done)
	for {
		select {
		case <-u.msgs:
			u.q.drain()
		case <-u.stop:
			return
		}
	}
}

// 界面线程起来之前排的活先攒着，start 时一起做，顺序不变。
func TestUIQueueRunsWorkQueuedBeforeStart(t *testing.T) {
	q := &uiQueue{}
	var got []int
	for i := 0; i < 3; i++ {
		i := i
		require.True(t, q.post(func() { got = append(got, i) }))
	}
	ui := newFakeUI(q)
	go ui.run()
	defer func() { close(ui.stop); <-ui.done }()
	q.start(ui.wake)
	require.NoError(t, q.call(time.Second, func() {}))
	assert.Equal(t, []int{0, 1, 2}, got)
}

// 连着排很多件活只叫醒一次（界面线程取一次就全做掉），不会把消息队列塞满。
func TestUIQueueCoalescesWakes(t *testing.T) {
	q := &uiQueue{}
	ui := newFakeUI(q)
	q.start(ui.wake)
	for i := 0; i < 100; i++ {
		q.post(func() {})
	}
	assert.Equal(t, int32(1), ui.wakes.Load())
	q.drain()
	q.post(func() {})
	assert.Equal(t, int32(2), ui.wakes.Load(), "取过之后再排要再叫")
}

// 投递失败（窗口没了、队列满了）不能让后面的活永远没人叫。
func TestUIQueueRetriesWakeAfterFailure(t *testing.T) {
	q := &uiQueue{}
	ui := newFakeUI(q)
	q.start(ui.wake)
	ui.fail.Store(true)
	q.post(func() {})
	ui.fail.Store(false)
	ran := false
	q.post(func() { ran = true })
	assert.Equal(t, int32(1), ui.wakes.Load())
	q.drain()
	assert.True(t, ran)
}

// call 等界面线程做完；界面线程卡住时按时返回错误，之后那件活也不再做。
func TestUIQueueCallTimesOutAndAbandons(t *testing.T) {
	q := &uiQueue{}
	q.start(func() bool { return true }) // 投了，但没有人取：界面线程卡住
	var ran atomic.Bool
	err := q.call(50*time.Millisecond, func() { ran.Store(true) })
	require.Error(t, err)
	q.drain() // 界面线程终于醒了
	assert.False(t, ran.Load(), "超时放弃的活不能在之后偷偷做掉")
}

// 在界面线程上调 call 直接做（否则会等自己等到超时）。
func TestUIQueueCallOnUIThreadRunsInline(t *testing.T) {
	q := &uiQueue{onUI: func() bool { return true }}
	ran := false
	require.NoError(t, q.call(time.Second, func() { ran = true }))
	assert.True(t, ran)
}

// close 之后 post / call 都失败，排着的活丢掉。
func TestUIQueueClose(t *testing.T) {
	q := &uiQueue{}
	ran := false
	q.post(func() { ran = true })
	q.close()
	q.drain()
	assert.False(t, ran)
	assert.False(t, q.post(func() {}))
	assert.ErrorIs(t, q.call(time.Second, func() {}), errUIClosed)
}

// 并发排活不丢、不重（-race 下跑）。
func TestUIQueueConcurrentPosts(t *testing.T) {
	q := &uiQueue{}
	ui := newFakeUI(q)
	go ui.run()
	defer func() { close(ui.stop); <-ui.done }()
	q.start(ui.wake)
	var n atomic.Int32
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				q.post(func() { n.Add(1) })
			}
		}()
	}
	wg.Wait()
	require.NoError(t, q.call(2*time.Second, func() {}))
	assert.Equal(t, int32(1600), n.Load())
}
