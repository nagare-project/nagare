package torrentstream

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// feed 模拟一个手上一直挂着请求的 peer：开头发一次请求，之后按固定间隔送来 count 块、
// 每块 chunk 字节，返回最后一次的结果。
func feed(s *paceState, start time.Time, every time.Duration, count, chunk int) (limit int, ok bool, end time.Time) {
	if s.lastSent.IsZero() {
		s.sent(start)
	}
	now := start
	for range count {
		now = now.Add(every)
		limit, ok = s.observe(now, chunk)
	}
	return limit, ok, now
}

func TestPaceStateLimitFollowsRate(t *testing.T) {
	start := time.Unix(1_700_000_000, 0)
	tests := []struct {
		name    string
		ceiling int
		every   time.Duration // 每块 16KB 之间的间隔
		want    int
	}{
		// 16KB / 50ms = 320KB/s，3 秒 = 960KB = 60 块
		{"每秒 320KB 的 peer 挂 3 秒的量", 0, 50 * time.Millisecond, 60},
		// 16KB / 400ms = 40KB/s，3 秒只有 7.5 块，抬到下限
		{"很慢的 peer 也至少给下限", 0, 400 * time.Millisecond, minPacedRequests},
		// 16KB / 2ms = 8MB/s，3 秒 1536 块，被对端声明的 500 压住
		{"不超过对端声明的上限", 500, 2 * time.Millisecond, 500},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := &paceState{ceiling: tc.ceiling}
			// 喂够两秒：前后几段速度一致，平滑之后就是它本身
			count := int(2 * time.Second / tc.every)
			limit, ok, _ := feed(s, start, tc.every, count, paceChunkBytes)
			assert.True(t, ok)
			assert.InDelta(t, tc.want, limit, 1)
		})
	}
}

func TestPaceStateNeedsAFullSampleFirst(t *testing.T) {
	s := &paceState{}
	start := time.Unix(1_700_000_000, 0)
	// 不到一个采样窗口：还没测出速度，不该动 anacrolix 给的上限
	_, ok, _ := feed(s, start, 50*time.Millisecond, 5, paceChunkBytes)
	assert.False(t, ok)
}

func TestPaceStateIgnoresIdleGaps(t *testing.T) {
	s := &paceState{}
	start := time.Unix(1_700_000_000, 0)
	limit, ok, end := feed(s, start, 50*time.Millisecond, 40, paceChunkBytes)
	assert.True(t, ok)
	assert.InDelta(t, 60, limit, 1)

	// 窗口里的都下完了，它闲了 30 秒，然后我们才又给它发请求；这 30 秒不能算进速度里
	resumed := end.Add(30 * time.Second)
	s.sent(resumed)
	limit, ok, _ = feed(s, resumed, 50*time.Millisecond, 20, paceChunkBytes)
	assert.True(t, ok)
	assert.InDelta(t, 60, limit, 1, "空闲不是慢：上限应保持原来的量")
}

// 每块之间隔一秒多、中间也没有新请求：它是一直欠着请求的慢 peer，不是闲着。
// 这种 peer 最会卡住播放，必须测得出来、压得下去。
func TestPaceStateCatchesTricklingPeers(t *testing.T) {
	start := time.Unix(1_700_000_000, 0)
	trickle := 1200 * time.Millisecond // 16KB / 1.2s ≈ 13KB/s

	t.Run("一开始就很慢", func(t *testing.T) {
		s := &paceState{}
		limit, ok, _ := feed(s, start, trickle, 5, paceChunkBytes)
		assert.True(t, ok, "慢到一秒一块也该测出速度")
		assert.Equal(t, minPacedRequests, limit)
	})
	t.Run("先快后慢", func(t *testing.T) {
		s := &paceState{}
		_, _, end := feed(s, start, 50*time.Millisecond, 40, paceChunkBytes)
		limit, ok, _ := feed(s, end, trickle, 6, paceChunkBytes)
		assert.True(t, ok)
		assert.Less(t, limit, 15, "慢下来之后不能一直沿用快的时候的上限")
	})
}

// 第一段从发请求算起：只有一块、还不到一个采样窗口时不算数；等够了之后不偏快。
func TestPaceStateFirstSampleCountsWaitForFirstChunk(t *testing.T) {
	s := &paceState{}
	start := time.Unix(1_700_000_000, 0)
	s.sent(start)
	// 40KB/s：16KB 每 400ms。第一块 400ms 后才到，这段等待要算进去
	limit, ok, _ := feed(s, start, 400*time.Millisecond, 2, paceChunkBytes)
	assert.True(t, ok)
	assert.Equal(t, minPacedRequests, limit, "40KB/s 的 3 秒只有 7.5 块，应落在下限，而不是被第一块测快")
}

func TestPaceStateTracksSlowdown(t *testing.T) {
	s := &paceState{}
	start := time.Unix(1_700_000_000, 0)
	_, _, end := feed(s, start, 50*time.Millisecond, 40, paceChunkBytes)
	// 降到每秒 80KB（16KB / 200ms）：几个采样窗口之后上限跟着降下来
	limit, ok, _ := feed(s, end, 200*time.Millisecond, 20, paceChunkBytes)
	assert.True(t, ok)
	assert.Less(t, limit, 25, "速度掉到四分之一后，上限也该大幅降低")
}
