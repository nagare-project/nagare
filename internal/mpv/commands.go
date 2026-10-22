package mpv

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"time"

	errs "github.com/nagare-project/nagare/internal/errors"
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

// SendUIState 把界面要显示的状态（标题、副标题、画质增强档位……）推给播放器界面脚本。
// 脚本可能还没加载完：那样这条会丢，脚本加载后会发 ready 再要一次。
func (p *Player) SendUIState(state any) error {
	b, err := json.Marshal(state)
	if err != nil {
		return err
	}
	_, err = p.Command("script-message-to", "nagare", "state", string(b))
	return err
}

// SendUIToast 让播放器界面弹一条屏幕提示（sub 可空）。
func (p *Player) SendUIToast(text, sub string) error {
	_, err := p.Command("script-message-to", "nagare", "toast", text, sub)
	return err
}

// SendUIUpNext 让播放器界面显示「下一集」卡片（倒计时 / 正在准备），card 序列化成 JSON 交给界面。
func (p *Player) SendUIUpNext(card any) error {
	b, err := json.Marshal(card)
	if err != nil {
		return err
	}
	_, err = p.Command("script-message-to", "nagare", "upnext", string(b))
	return err
}

// ClearUIUpNext 收起「下一集」卡片。
func (p *Player) ClearUIUpNext() error {
	_, err := p.Command("script-message-to", "nagare", "upnext-clear")
	return err
}

// SetKeepOpen 切换 keep-open：开着时播到尾 mpv 停在最后一帧（暂停）而不是关窗，
// 留出放「下一集」卡片的时间；关着就是播完即退出（启动参数里的默认）。
func (p *Player) SetKeepOpen(on bool) error {
	v := "no"
	if on {
		v = "yes"
	}
	return p.SetProperty("keep-open", v)
}

// NextFile 是同一个窗口里接着播的下一个文件。
type NextFile struct {
	// Path 是交给 mpv 的路径或 URL（与 LaunchOptions.MediaPath 同义）。
	Path string
	// Title 是窗口标题与界面顶栏的 media-title；空串时只清掉上一集强制的 media-title（顶栏显示文件名），
	// 窗口标题不动。
	Title string
	// StartAt 是起播秒数（>0 生效），否则从头播。
	StartAt float64
}

// propReset 是换文件时要设的一个属性。
type propReset struct {
	name  string
	value any
}

// 换文件时要复位的选项，分两批（真 mpv 0.41 上逐条验过，见 integration_test.go）。
//
// 这些都是全局选项：启动参数或上一集运行时设的属性（轨道编排设过 sid / secondary-sid /
// 可见性）会原样带到下一个文件。复位不了的后果是字幕选错、弹幕开关沿用，播放本身不受影响，
// 所以都只记日志。speed（倍速）刻意不复位：用户调过的倍速跟着走。
var (
	// beforeLoadResets 在 loadfile 之前设：设成 auto 只改「下一个文件怎么选轨」，正在播的文件
	// 选中的轨道不动。必须在 loadfile 之前 —— auto 晚到（新文件已经选完轨）就不会再选一次。
	beforeLoadResets = []propReset{
		{"sid", "auto"},
		{"aid", "auto"},
	}
	// afterLoadResets 在 loadfile 排上之后设：它们会立刻改动正在播的文件（secondary-sid=no 会
	// 摘掉上一集的弹幕轨），loadfile 被拒时上一集得原样留着。都是明确的值，晚到也照样生效。
	afterLoadResets = []propReset{
		{"secondary-sid", "no"},
		{"sub-visibility", true},
		{"secondary-sub-visibility", true},
	}
)

// LoadNext 在同一个 mpv 进程里换到下一个文件：窗口、全屏、音量、着色器、倍速都保住。
//
// 顺序：起播位置与外挂字幕（必须成功：--start 是全局选项，不复位的话下一集从上一集的续播点
// 起播；--sub-file 对之后每个文件都生效，上一集的对白字幕会挂到下一集上）→ 轨道选择回到 auto →
// loadfile → 标题与弹幕轨 / 可见性。loadfile 之前任何一步因 mpv 已退出或不应答而失败，都不发
// loadfile，原文件照常播；loadfile 排上之后的失败只记日志，换集照样发生。
//
// loadfile 只是排队：返回 nil 不代表新文件能播，打不开会以 end-file(error) 出现在事件里。
// 返回的错误里不带命令参数（流端点地址含能力段）。
func (p *Player) LoadNext(f NextFile) error {
	start := "none"
	if f.StartAt > 0 {
		start = fmt.Sprintf("+%.3f", f.StartAt)
	}
	// sub-files 必须是空数组：nil 切片会序列化成 null
	for _, r := range []propReset{{"start", start}, {"sub-files", []string{}}} {
		if err := p.setWhileAlive(r); err != nil {
			return loadNextErr(err)
		}
	}
	if err := p.resetBestEffort(beforeLoadResets); err != nil {
		return loadNextErr(err)
	}
	if err := p.aliveErr(); err != nil {
		return loadNextErr(err)
	}
	if _, err := p.ipc.callRedacted("loadfile", f.Path, "replace"); err != nil {
		return loadNextErr(err)
	}

	after := []propReset{{"force-media-title", f.Title}}
	if f.Title != "" {
		// 没给标题时窗口标题不复位：mpv 默认的窗口标题是一串属性展开式，与其照抄过来，
		// 不如留着上一集的（调用方总会给标题，这里只是兜底）。media-title 清空即可显示文件名。
		after = append(after, propReset{"title", f.Title})
	}
	if err := p.resetBestEffort(append(after, afterLoadResets...)); err != nil {
		log.Printf("mpv: 下一集已经排上，后面的复位没做完：%v", err)
	}
	return nil
}

// setWhileAlive 设一个属性；mpv 已退出时不再发命令。
func (p *Player) setWhileAlive(r propReset) error {
	if err := p.aliveErr(); err != nil {
		return err
	}
	return p.SetProperty(r.name, r.value)
}

// resetBestEffort 逐个设属性，单个失败只记日志；mpv 已退出或不应答时停下并返回错误 ——
// 卡死的 mpv 对后面的命令也不会应答，别为每一条都等满超时。
func (p *Player) resetBestEffort(resets []propReset) error {
	for _, r := range resets {
		err := p.setWhileAlive(r)
		if err == nil {
			continue
		}
		if p.aliveErr() != nil || isNoResponse(err) {
			return err
		}
		log.Printf("mpv: 换下一集时复位 %s 失败（照样换）：%v", r.name, err)
	}
	return nil
}

// aliveErr 在 mpv 已经退出（或连接已断）时返回错误。
func (p *Player) aliveErr() error {
	select {
	case <-p.done:
		return errors.New("mpv 已经退出")
	default:
		return nil
	}
}

// loadNextErr 是换集失败给用户看的说法：原文件还在播，恢复动作是从网页上重新开始。
func loadNextErr(err error) error {
	return errs.Wrap(errs.CategoryPlayback, "player.nav", "没能切到下一集", "关掉播放器后从网页上重新播放", err)
}
