package mpv

import (
	"fmt"
	"net"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// disconnectGrace：IPC 干净断开后等待进程退出结论的宽限期。
const disconnectGrace = 3 * time.Second

// eventBufferSize 是 Events() 通道的缓冲。time-pos 观察推送很频繁，
// 消费慢时按「丢最旧」策略腾位，绝不阻塞读循环。
const eventBufferSize = 64

// State 是播放状态的最近快照。进程死后保留最后已知值 —— 观看进度回写靠它。
type State struct {
	TimePos  float64 // 当前播放位置（秒）
	Duration float64 // 总时长（秒），文件加载前为 0
	Paused   bool    // 是否暂停
	Path     string  // 正在播放的文件路径
}

// observedProps 是 Launch 后自动观察的属性。顺序固定，观察 id 从 1 递增，
// 测试按此断言 observe_property 命令的形状。
var observedProps = []struct {
	id   int
	name string
}{
	{1, "time-pos"},
	{2, "pause"},
	{3, "duration"},
	{4, "eof-reached"},
}

// Player 管一个 mpv 进程与它的 IPC 连接。所有方法并发安全。
//
// 生命周期收敛：进程退出（cmd.Wait）与 IPC 断开（读循环 EOF）两条路径
// 汇入同一个终态 —— Done() 关闭、Err() 返回终态错误、Events() 收到 Died。
// 用户手动关掉 mpv 窗口也会走到这里，调用方不会「以为还在播」。
type Player struct {
	ipc      *ipcConn
	proc     *exec.Cmd // attach 模式（无进程，仅测试）为 nil
	endpoint string    // IPC socket / 管道路径，终态时清理

	events chan Event

	stateMu sync.RWMutex
	state   State

	termMu  sync.Mutex
	termed  bool
	termErr error
	done    chan struct{}

	closeOnce sync.Once
	closeErr  error       // 仅 Close 兜底失败时非 nil，termMu 保护
	closeReq  atomic.Bool // Close() 已请求：随后的退出/断开一律视为正常
	sawQuit   atomic.Bool // 收到 mpv shutdown 事件：随后的断开是正常退出
	endedOnce atomic.Bool // Ended 事件只发一次（eof-reached 与 end-file 会重复报告）

	stderrTail *boundedBuffer // mpv stderr 开头若干字节，异常退出时用于诊断
}

// newPlayer 完成「连接已建立」之后的通用初始化：启动读循环与进程守望、
// 注册属性观察。waitCh 是 cmd.Wait 的结果通道（attach 模式传 nil）。
func newPlayer(conn net.Conn, proc *exec.Cmd, waitCh <-chan error, endpoint, initialPath string, stderrTail *boundedBuffer) (*Player, error) {
	p := &Player{
		proc:       proc,
		endpoint:   endpoint,
		events:     make(chan Event, eventBufferSize),
		state:      State{Path: initialPath},
		done:       make(chan struct{}),
		stderrTail: stderrTail,
	}
	p.ipc = newIPCConn(conn, p.handleIPCEvent, p.onDisconnect)
	go p.ipc.readLoop()
	if waitCh != nil {
		go p.watchProcess(waitCh)
	}

	for _, prop := range observedProps {
		if _, err := p.ipc.call("observe_property", prop.id, prop.name); err != nil {
			_ = p.Close() // 初始化失败不留半死进程
			return nil, fmt.Errorf("注册 mpv 属性观察（%s）失败：%w", prop.name, err)
		}
	}
	return p, nil
}

// attach 基于一条已建立的 IPC 连接构造 Player，不管理任何进程。
// 供测试用 fake socket 驱动完整协议路径；生产入口是 Launch。
func attach(conn net.Conn, initialPath string) (*Player, error) {
	return newPlayer(conn, nil, nil, "", initialPath, nil)
}

// watchProcess 等待 mpv 进程退出并收敛终态。与读循环 EOF 竞争时，
// 先到者定终态（两条路径对每种退出场景给出的结论一致）。
func (p *Player) watchProcess(waitCh <-chan error) {
	werr := <-waitCh
	p.terminate(p.waitVerdict(werr))
	p.ipc.shutdown(nil) // 进程已死，唤醒所有挂起的 IPC 调用
}

// waitVerdict 把 cmd.Wait 的结果翻译成终态错误：正常退出 / 主动关闭为 nil。
func (p *Player) waitVerdict(werr error) error {
	if werr == nil || p.closeReq.Load() {
		return nil
	}
	msg := "mpv 进程异常退出"
	if p.stderrTail != nil {
		if tail := strings.TrimSpace(p.stderrTail.String()); tail != "" {
			msg += fmt.Sprintf("（stderr：%s）", tail)
		}
	}
	return fmt.Errorf("%s：%w。可重新发起播放；若反复出现请检查 mpv 安装（mpv --version）", msg, werr)
}

// onDisconnect 是 IPC 断开路径的终态判定（读循环退出后被调用恰好一次）。
func (p *Player) onDisconnect(readErr error) {
	switch {
	case p.closeReq.Load() || p.sawQuit.Load():
		// 主动 Close，或 mpv 已广播 shutdown 事件（正常退出前必发）
		p.terminate(nil)
	case readErr != nil:
		p.terminate(readErr)
	default:
		// 干净的 EOF 但没收到 shutdown 事件：用户直接关掉 mpv 窗口时最常见——
		// mpv 先关 socket、事件没送到。此时进程往往几毫秒后就以 exit 0 退出，
		// 先把判定权让给 watchProcess（它按 exit code 定终态），别抢先判成异常；
		// 进程在宽限期内仍不退出，才是真正的"socket 死了但进程还活着"。
		if p.proc != nil {
			select {
			case <-p.done:
				return
			case <-time.After(disconnectGrace):
			}
		}
		p.terminate(fmt.Errorf("mpv IPC 连接意外断开（进程可能被强制结束）；最后进度已保留，可重新发起播放"))
	}
}

// terminate 收敛到终态：记录错误、关闭 Done、发出 Died 事件、清理 socket 文件。
// 幂等 —— 进程退出与 IPC 断开谁先到都只生效一次。
func (p *Player) terminate(err error) {
	p.termMu.Lock()
	if p.termed {
		p.termMu.Unlock()
		return
	}
	p.termed = true
	p.termErr = err
	close(p.done)
	p.termMu.Unlock()

	p.emit(Event{Kind: EventDied})
	if p.endpoint != "" {
		removeIPCEndpoint(p.endpoint) // mpv 正常退出会自删；这里兜底强杀等场景
	}
}

// State 返回最近的状态快照（副本）。进程死后仍可读到最后已知值。
func (p *Player) State() State {
	p.stateMu.RLock()
	defer p.stateMu.RUnlock()
	return p.state
}

func (p *Player) setState(mutate func(*State)) {
	p.stateMu.Lock()
	mutate(&p.state)
	p.stateMu.Unlock()
}

// Events 返回事件通道。缓冲有限：消费慢时丢弃最旧事件，通道不会关闭；
// 调用方应同时 select Done() 以感知退出（Died 是终态前的最后一类事件）。
func (p *Player) Events() <-chan Event {
	return p.events
}

// Done 在进程退出或 IPC 断开收敛后关闭。
func (p *Player) Done() <-chan struct{} {
	return p.done
}

// Err 返回终态错误，仅在 Done() 关闭后有意义；正常退出（含主动 Close）为 nil。
func (p *Player) Err() error {
	p.termMu.Lock()
	defer p.termMu.Unlock()
	return p.termErr
}
