package player

import (
	"math"
	"time"

	"github.com/nagare-project/nagare/internal/mpv"
)

// 在线服务器可能保持连接却不再送出视频，mpv 此时不退出也不报告错误。
// 通过实际播放进度识别停滞，并复用已有 playbackFailure 自动换源通道。
// 在线来源不在窗口里换集，看开播那一集就够了。
func (m *Manager) watchRemoteProgress(pb *playback) {
	sess := pb.episode()
	if _, online := sess.src.(*remoteSource); !online {
		return
	}
	timeout := m.opts.RemoteStallTimeout
	if timeout <= 0 {
		timeout = 45 * time.Second
	}
	interval := min(time.Second, max(time.Millisecond, timeout/4))
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	progress := remoteProgress{lastChange: time.Now()}
	for {
		select {
		case <-pb.player.Done():
			return
		case <-pb.finished:
			return
		case now := <-ticker.C:
			if !progress.stalled(pb.player.State(), now, timeout) {
				continue
			}
			m.mu.Lock()
			if m.current != pb {
				m.mu.Unlock()
				return
			}
			m.lastPlaybackFailure = &PlaybackFailure{
				FileID: sess.item.FileID, PlaybackID: pb.id, Reason: "在线媒体长时间没有播放进度，已停止当前来源", At: now.UnixMilli(),
			}
			m.mu.Unlock()
			_ = pb.player.Close()
			return
		}
	}
}

type remoteProgress struct {
	lastPosition float64
	lastChange   time.Time
}

func (p *remoteProgress) stalled(state mpv.State, now time.Time, timeout time.Duration) bool {
	// 暂停与跳转均重新计时；不会把用户暂停误判为来源故障。
	if state.Paused || math.Abs(state.TimePos-p.lastPosition) > 0.01 {
		p.lastChange = now
	}
	p.lastPosition = state.TimePos
	return now.Sub(p.lastChange) >= timeout
}
