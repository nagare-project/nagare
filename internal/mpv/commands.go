package mpv

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

const (
	// closeGraceTimeout 是 Close 发出 quit 后等待自然退出的时长，超过则强杀。
	closeGraceTimeout = 3 * time.Second
	// closeKillTimeout 是强杀后等待收敛的兜底时长。
	closeKillTimeout = 2 * time.Second
	// quitCallTimeout：quit 的响应经常赶不上进程退出，不值得等满默认超时。
	quitCallTimeout = 1 * time.Second
)

// SetPause 设置暂停状态。
func (p *Player) SetPause(v bool) error {
	if _, err := p.ipc.call("set_property", "pause", v); err != nil {
		return fmt.Errorf("设置 mpv 暂停状态失败：%w", err)
	}
	return nil
}

// Seek 绝对定位到指定秒数。
func (p *Player) Seek(seconds float64) error {
	if _, err := p.ipc.call("seek", seconds, "absolute"); err != nil {
		return fmt.Errorf("mpv 定位到 %.1f 秒失败：%w", seconds, err)
	}
	return nil
}

// LoadSub 外挂并选中一条字幕轨（弹幕 ASS 走这里）。
func (p *Player) LoadSub(path string) error {
	if _, err := p.ipc.call("sub-add", path); err != nil {
		return fmt.Errorf("mpv 加载字幕 %q 失败：%w", path, err)
	}
	return nil
}

// ReloadSub 重载当前外挂字幕轨（弹幕文件原地更新样式后调用）。
func (p *Player) ReloadSub() error {
	if _, err := p.ipc.call("sub-reload"); err != nil {
		return fmt.Errorf("mpv 重载字幕失败：%w", err)
	}
	return nil
}

// GetProperty 读取任意 mpv 属性并反序列化到 out（out 为 nil 时仅探测可读性）。
func (p *Player) GetProperty(name string, out any) error {
	data, err := p.ipc.call("get_property", name)
	if err != nil {
		return fmt.Errorf("读取 mpv 属性 %s 失败：%w", name, err)
	}
	if out == nil || len(data) == 0 {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("解析 mpv 属性 %s 的值 %s 失败：%w", name, data, err)
	}
	return nil
}

// SetProperty 设置任意 mpv 属性（set_property）。
// 供上层做轨道编排（sid / secondary-sid / secondary-sub-ass-override）等通用操作。
func (p *Player) SetProperty(name string, value any) error {
	if _, err := p.ipc.call("set_property", name, value); err != nil {
		return fmt.Errorf("设置 mpv 属性 %s 失败：%w", name, err)
	}
	return nil
}

// Command 发送任意原始 IPC 命令并返回响应的 data 字段。
// 有专用方法（SetPause/Seek/…）时优先用专用方法，这里是逃生舱。
func (p *Player) Command(args ...any) (json.RawMessage, error) {
	data, err := p.ipc.call(args...)
	if err != nil {
		return nil, fmt.Errorf("mpv 命令 %v 失败：%w", args, err)
	}
	return data, nil
}

// Close 关闭播放器：发 quit → 限时等自然退出 → 强杀。幂等，可并发调用；
// 后续调用等待首次收敛完成后返回。关闭引发的退出不计入 Err()（终态为 nil）。
// 返回非 nil 仅代表「无法确认 mpv 已退出」这一种失败。
func (p *Player) Close() error {
	p.closeReq.Store(true) // 先立 flag：之后的任何退出/断开都判为正常
	p.closeOnce.Do(func() {
		// 礼貌退出。连接可能已断，失败属正常，不影响后续收敛
		_, _ = p.ipc.callTimeout(quitCallTimeout, "quit")

		select {
		case <-p.done:
			return
		case <-time.After(closeGraceTimeout):
		}

		// 限期未退：强杀进程并关闭连接，逼两条收敛路径出结果
		if p.proc != nil && p.proc.Process != nil {
			_ = p.proc.Process.Kill() // 进程可能刚好自己退了，错误无意义
		}
		p.ipc.shutdown(nil)

		select {
		case <-p.done:
		case <-time.After(closeKillTimeout):
			// 不应到达：Kill 后 watchProcess / 读循环必有一方收敛。兜底以保幂等
			err := errors.New("mpv 未能在限期内退出，可能需要手动结束进程")
			p.termMu.Lock()
			p.closeErr = err
			p.termMu.Unlock()
			p.terminate(err)
		}
	})

	<-p.done
	p.termMu.Lock()
	defer p.termMu.Unlock()
	return p.closeErr
}
