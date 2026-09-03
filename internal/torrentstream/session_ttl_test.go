package torrentstream

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 停在选集弹窗的会话有保留时限：用户可能直接关掉标签页而不点取消，
// 没有这个兜底，那条磁力会一直挂在 DHT 里 announce 到进程退出。
func TestAwaitingSessionExpires(t *testing.T) {
	prev := awaitingTTL
	awaitingTTL = 20 * time.Millisecond
	t.Cleanup(func() { awaitingTTL = prev })

	e := &Engine{cacheDir: t.TempDir()}
	sess := &session{engine: e, cancel: func() {}}
	e.sess = sess

	sess.markAwaiting()
	require.NotNil(t, e.sess, "刚标记等待时会话还在")

	assert.Eventually(t, func() bool {
		e.mu.Lock()
		defer e.mu.Unlock()
		return e.sess == nil
	}, time.Second, 5*time.Millisecond, "超过保留时限后应当自己把种子放掉")
}

// 用户选完集回来时计时器必须停掉，否则十分钟后会把正在播的那一集掐了。
func TestResumeCancelsAwaitingExpiry(t *testing.T) {
	prev := awaitingTTL
	awaitingTTL = 20 * time.Millisecond
	t.Cleanup(func() { awaitingTTL = prev })

	e := &Engine{cacheDir: t.TempDir()}
	sess := &session{engine: e, cancel: func() {}}
	e.sess = sess

	sess.markAwaiting()
	sess.resume()

	time.Sleep(60 * time.Millisecond)
	e.mu.Lock()
	still := e.sess
	e.mu.Unlock()
	assert.Same(t, sess, still, "已经选完集的会话不该被保留时限收掉")
}

// 计时器到点时若当前会话已经换成了别人，绝不能误杀 —— 那把停的是别人的种子。
func TestExpiryDoesNotKillReplacedSession(t *testing.T) {
	e := &Engine{cacheDir: t.TempDir()}
	stale := &session{engine: e, cancel: func() {}, awaiting: true}
	fresh := &session{engine: e, cancel: func() {}}
	e.sess = fresh

	stale.expireAwaiting()

	e.mu.Lock()
	defer e.mu.Unlock()
	assert.Same(t, fresh, e.sess, "过期的是旧会话，当前会话不该受影响")
}
