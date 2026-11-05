package player

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 登录成功之后的补发（KickPendingSyncs）：在后台跑、不等闸，与别处起的补发过同一道闸。

// waitKicked 等 KickPendingSyncs 起的后台轮次都跑完。
func waitKicked(t *testing.T, m *Manager) {
	t.Helper()
	require.True(t, waitTimeout(&m.kicked, 10*time.Second), "后台补发 10 秒内没有跑完")
}

func (c *retryClient) maxInFlightSnapshot() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.maxInFlight
}

// 登录之后的补发在后台跑：KickPendingSyncs 立刻返回，随后把待补的集写进账号。
func TestKickRetriesInTheBackground(t *testing.T) {
	client := &retryClient{loggedIn: true}
	m, st := retryHarness(t, client)
	addPending(t, st, "a", 42, 3, time.Hour)
	addPending(t, st, "b", 42, 4, time.Minute)

	m.KickPendingSyncs()
	waitKicked(t, m)

	assert.Equal(t, []string{"42/3", "42/4"}, client.markedSnapshot())
	assert.True(t, progressOf(t, st, "a").Synced)
	assert.True(t, progressOf(t, st, "b").Synced)
	assert.Nil(t, m.Status().Sync)
}

// 已经有一轮在跑（启动时那一轮、横幅上的「重试」）：登录的补发等它跑完再跑，两轮不同时写账号；
// 轮上时重新取待补列表，前一轮开跑之后才待补的集也补上。
func TestKickWaitsForTheRoundInFlight(t *testing.T) {
	client := &retryClient{loggedIn: true, entered: make(chan struct{}, 4), release: make(chan struct{})}
	m, st := retryHarness(t, client)
	addPending(t, st, "a", 42, 3, time.Hour)

	first := make(chan SyncRetryResult, 1)
	go func() { first <- m.RetryPendingSyncs(context.Background()) }()
	<-client.entered                           // 前一轮正写着 a
	addPending(t, st, "c", 42, 5, time.Minute) // 前一轮开跑时的列表里没有它

	m.KickPendingSyncs()
	select {
	case <-client.entered:
		t.Fatal("前一轮还在写，登录的补发就开写了")
	case <-time.After(100 * time.Millisecond):
	}
	close(client.release)

	assert.Equal(t, SyncRetryResult{Synced: 1, Remaining: 1}, <-first)
	waitKicked(t, m)
	assert.Equal(t, []string{"42/3", "42/5"}, client.markedSnapshot())
	assert.Equal(t, 1, client.maxInFlightSnapshot(), "两轮补发同时在写账号")
}

// 排着的那一轮还没轮上时再 kick（连点了几次登录）：不多排。它轮上时取的就是最新的列表与登录态；
// 多排的只会紧接着把下一个十集也补掉，单轮上限就形同虚设。kick 本身从不等闸。
func TestKicksWhileOneIsQueuedCoalesce(t *testing.T) {
	client := &retryClient{loggedIn: true}
	m, st := retryHarness(t, client)
	for i := 1; i <= syncRetryMax+5; i++ {
		addPending(t, st, fmt.Sprintf("ep%02d", i), 5, i, time.Duration(100-i)*time.Minute)
	}

	m.retrySem <- struct{}{} // 另一轮正在跑
	returned := make(chan struct{})
	go func() {
		for range 3 {
			m.KickPendingSyncs()
		}
		close(returned)
	}()
	select {
	case <-returned:
	case <-time.After(5 * time.Second):
		t.Fatal("KickPendingSyncs 在等闸：登录的答复会被它拖住")
	}
	<-m.retrySem
	waitKicked(t, m)

	assert.Len(t, client.markedSnapshot(), syncRetryMax, "排着队时的三次 kick 只该补一轮")
}

// 排下的那一轮轮上之后再 kick（比如补到一半退出登录、换了账号）：照常再排一轮，
// 它轮上时取的是那时的登录态与列表；两轮仍然一前一后。
func TestKickAfterTheQueuedRoundStartedQueuesAnother(t *testing.T) {
	client := &retryClient{loggedIn: true, entered: make(chan struct{}, 4), release: make(chan struct{})}
	m, st := retryHarness(t, client)
	addPending(t, st, "a", 42, 3, time.Hour)

	m.KickPendingSyncs()
	<-client.entered // 第一轮轮上了，正写着 a
	addPending(t, st, "c", 42, 5, time.Minute)
	m.KickPendingSyncs()
	close(client.release)
	waitKicked(t, m)

	assert.Equal(t, []string{"42/3", "42/5"}, client.markedSnapshot())
	assert.Equal(t, 1, client.maxInFlightSnapshot(), "两轮补发同时在写账号")
}

// 退出之后（StopRetrying）不再起补发；排着队时退出的那一轮不再轮上，也不卡住。
func TestKickEndsWithStopRetrying(t *testing.T) {
	t.Run("退出之后再 kick", func(t *testing.T) {
		client := &retryClient{loggedIn: true}
		m, st := retryHarness(t, client)
		addPending(t, st, "a", 42, 3, time.Hour)

		m.StopRetrying()
		m.KickPendingSyncs()
		waitKicked(t, m)

		assert.Empty(t, client.markedSnapshot())
	})
	t.Run("排着队时退出", func(t *testing.T) {
		client := &retryClient{loggedIn: true}
		m, st := retryHarness(t, client)
		addPending(t, st, "a", 42, 3, time.Hour)

		m.retrySem <- struct{}{} // 另一轮正在跑
		m.KickPendingSyncs()
		m.StopRetrying()
		waitKicked(t, m)
		<-m.retrySem

		assert.Empty(t, client.markedSnapshot())
	})
}
