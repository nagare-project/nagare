package mpv

import "encoding/json"

// EventKind 标识 Player 事件的类别。
type EventKind int

const (
	// EventTimePos：播放位置变化，Event.TimePos 为新位置（秒）。
	EventTimePos EventKind = iota + 1
	// EventPause：暂停状态变化，Event.Paused 为新状态。
	EventPause
	// EventFileLoaded：文件加载完成，开始播放。
	EventFileLoaded
	// EventEnded：播放结束，Event.Reason 为 mpv 的结束原因（eof/stop/quit/error）。
	EventEnded
	// EventDied：进程退出或 IPC 断开后的终态事件；详情见 Err()。
	EventDied
	// EventUIMessage：nagare 的播放器界面（ui/scripts/nagare）发来一条消息，Event.Args 是名字和参数。
	EventUIMessage
	// EventFileStarted：mpv 开始加载一个新文件（start-file）。此时 State 的 TimePos/Duration 已清零，
	// 下一个文件的 Ended 重新可发 —— 同一个窗口里 loadfile 换文件时，靠它分清新旧文件的事件。
	EventFileStarted
	// EventEOF：eof-reached 由假变真（播到了文件尾）。每次上升沿都发、不去重：keep-open 下
	// 用户拖回去再播到尾，会再来一次。与 Ended 不同，它不代表文件已卸载。
	EventEOF
	// EventEOFCleared：eof-reached 在 EOF 之后又落下（用户拖回去了，或文件被卸载）。
	EventEOFCleared
)

// uiMessagePrefix 是界面脚本发消息时的第一个参数（script-message nagare-event <名字> ...）。
// mpv 把所有脚本的 script-message 都广播给 IPC 客户端，只认这个前缀的。
const uiMessagePrefix = "nagare-event"

// String 便于日志与测试失败信息可读。
func (k EventKind) String() string {
	switch k {
	case EventTimePos:
		return "TimePos"
	case EventPause:
		return "Pause"
	case EventFileLoaded:
		return "FileLoaded"
	case EventEnded:
		return "Ended"
	case EventDied:
		return "Died"
	case EventUIMessage:
		return "UIMessage"
	case EventFileStarted:
		return "FileStarted"
	case EventEOF:
		return "EOF"
	case EventEOFCleared:
		return "EOFCleared"
	default:
		return "Unknown"
	}
}

// Event 是 Player 推送的一条事件。仅与 Kind 对应的字段有意义。
type Event struct {
	Kind    EventKind
	TimePos float64  // EventTimePos
	Paused  bool     // EventPause
	Reason  string   // EventEnded
	Args    []string // EventUIMessage：消息名在前，参数在后
}

// handleIPCEvent 在读循环 goroutine 内同步处理 mpv 事件，因此绝不能阻塞，
// 也不能在这里同步发 IPC 命令（响应要靠同一个读循环派发，会死锁）。
func (p *Player) handleIPCEvent(msg ipcMessage) {
	switch msg.Event {
	case "property-change":
		p.handlePropertyChange(msg)
	case "start-file":
		// 新文件：上一个文件的结束标记、到尾标记与位置都不再适用。位置清零是为了
		// 不让上一个文件最后的 23:40 被当成新文件的进度。
		p.endedOnce.Store(false)
		p.eofSeen.Store(false)
		p.setState(func(s *State) { s.TimePos, s.Duration = 0, 0 })
		p.emit(Event{Kind: EventFileStarted})
	case "file-loaded":
		// path 属性此时才有值，异步取一次刷新快照（见上：不能同步 call）
		go p.refreshPath()
		p.emit(Event{Kind: EventFileLoaded})
	case "end-file":
		p.emitEnded(msg.Reason)
	case "client-message":
		if len(msg.Args) >= 2 && msg.Args[0] == uiMessagePrefix {
			p.emit(Event{Kind: EventUIMessage, Args: append([]string(nil), msg.Args[1:]...)})
		}
	case "shutdown":
		// mpv 正常退出前必广播 shutdown；据此把随后的连接断开判为正常
		p.sawQuit.Store(true)
	}
}

// handlePropertyChange 把观察属性的推送写入状态快照并转成事件。
func (p *Player) handlePropertyChange(msg ipcMessage) {
	switch msg.Name {
	case "time-pos":
		// 停止/结束时 mpv 会推 null —— 保留最后已知值（进度回写依赖它）
		v, ok := decodeFloat(msg.Data)
		if !ok {
			return
		}
		p.setState(func(s *State) { s.TimePos = v })
		p.emitTimePos(v)
	case "duration":
		v, ok := decodeFloat(msg.Data)
		if !ok {
			return
		}
		p.setState(func(s *State) { s.Duration = v })
	case "pause":
		var paused bool
		if json.Unmarshal(msg.Data, &paused) != nil {
			return
		}
		p.setState(func(s *State) { s.Paused = paused })
		p.emit(Event{Kind: EventPause, Paused: paused})
	case "eof-reached":
		var reached *bool
		if json.Unmarshal(msg.Data, &reached) != nil {
			return
		}
		p.handleEOFReached(reached != nil && *reached)
	}
}

// handleEOFReached 把 eof-reached 的变化转成事件。null（没有文件）按「没到尾」算。
func (p *Player) handleEOFReached(reached bool) {
	if !reached {
		if p.eofSeen.CompareAndSwap(true, false) {
			p.emit(Event{Kind: EventEOFCleared})
		}
		return
	}
	if p.eofSeen.CompareAndSwap(false, true) {
		p.emit(Event{Kind: EventEOF})
	}
	// keep-open 场景下 end-file 可能不来，这里兜底；与 end-file 去重
	p.emitEnded("eof")
}

// emitTimePos 只在事件通道不到半满时投递位置事件：time-pos 每秒推好几次，消费方一慢就会把
// 通道塞满，按「丢最旧」腾位会把 Ended / FileStarted 这类生命周期事件挤掉。位置本身已经写进
// State，丢掉的只是「位置变了」这个提醒，消费方读 State 拿到的永远是最新值。
func (p *Player) emitTimePos(v float64) {
	if len(p.events) >= cap(p.events)/2 {
		return
	}
	p.emit(Event{Kind: EventTimePos, TimePos: v})
}

// decodeFloat 解出可能为 null 的数值属性；null 或解析失败返回 ok=false。
func decodeFloat(raw json.RawMessage) (float64, bool) {
	var v *float64
	if json.Unmarshal(raw, &v) != nil || v == nil {
		return 0, false
	}
	return *v, true
}

// emitEnded 保证每个文件只发一次 Ended：eof-reached 观察与 end-file 事件会先后报告同一次结束。
// 下一个文件的 start-file 会复位。
func (p *Player) emitEnded(reason string) {
	if !p.endedOnce.CompareAndSwap(false, true) {
		return
	}
	if reason == "" {
		reason = "eof"
	}
	p.emit(Event{Kind: EventEnded, Reason: reason})
}

// refreshPath 用 get_property 拿一次 path 刷新快照（file-loaded 后调用）。
// 失败只发生在退出竞态（连接已断），保留启动时的初始路径即可，不视为故障。
func (p *Player) refreshPath() {
	var path string
	if err := p.GetProperty("path", &path); err != nil || path == "" {
		return
	}
	p.setState(func(s *State) { s.Path = path })
}

// emit 非阻塞投递事件：通道满时丢最旧的腾位（保新弃旧），绝不卡住读循环。
func (p *Player) emit(ev Event) {
	// 理论上并发 emit 可能反复抢占，限制尝试次数保证有界；实际一两轮内必成
	for i := 0; i < 8; i++ {
		select {
		case p.events <- ev:
			return
		default:
		}
		select {
		case <-p.events: // 丢一条最旧事件
		default:
		}
	}
}
