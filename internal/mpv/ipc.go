package mpv

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"
)

const (
	// commandTimeout 是单条 IPC 命令等待响应的上限。
	commandTimeout = 5 * time.Second
	// maxIPCLine 是单行 JSON 的读取上限（track-list 等属性可能很大）。
	maxIPCLine = 2 * 1024 * 1024
)

// ipcRequest 是发往 mpv 的一条命令。
type ipcRequest struct {
	Command   []any `json:"command"`
	RequestID int64 `json:"request_id"`
}

// ipcMessage 是 mpv 发来的一行 JSON 的统一形状：
// 带 request_id 的是命令响应；带 event 的是事件推送（无 request_id）。
type ipcMessage struct {
	RequestID int64           `json:"request_id"`
	Error     string          `json:"error"`
	Data      json.RawMessage `json:"data"`
	Event     string          `json:"event"`
	ID        int64           `json:"id"`     // observe_property 注册时给的观察 id
	Name      string          `json:"name"`   // property-change 的属性名
	Reason    string          `json:"reason"` // end-file 的结束原因
}

// ipcConn 管一条到 mpv 的 IPC 连接：请求/响应配对、事件回调、断开收敛。
type ipcConn struct {
	conn    net.Conn
	writeMu sync.Mutex // 序列化对 socket 的写

	mu      sync.Mutex
	nextID  int64
	pending map[int64]chan ipcMessage // request_id → 等待响应的调用方
	closed  bool

	// onEvent 在读循环 goroutine 内同步调用，实现不得阻塞。
	onEvent func(ipcMessage)
	// onDisconnect 在读循环退出（EOF / 读错误 / 主动关闭）后恰好调用一次；
	// err 为 nil 表示对端正常关闭（EOF）。
	onDisconnect func(error)
}

func newIPCConn(conn net.Conn, onEvent func(ipcMessage), onDisconnect func(error)) *ipcConn {
	return &ipcConn{
		conn:         conn,
		pending:      make(map[int64]chan ipcMessage),
		onEvent:      onEvent,
		onDisconnect: onDisconnect,
	}
}

// call 发送命令并等待配对的响应，超时上限 commandTimeout。返回响应的 data 字段。
func (c *ipcConn) call(cmd ...any) (json.RawMessage, error) {
	return c.callTimeout(commandTimeout, cmd...)
}

func (c *ipcConn) callTimeout(timeout time.Duration, cmd ...any) (json.RawMessage, error) {
	id, ch, err := c.register()
	if err != nil {
		return nil, fmt.Errorf("mpv 连接已断开，无法发送命令 %v：%w", cmd, err)
	}
	defer c.unregister(id)

	payload, err := json.Marshal(ipcRequest{Command: cmd, RequestID: id})
	if err != nil {
		return nil, fmt.Errorf("序列化 mpv 命令 %v 失败：%w", cmd, err)
	}
	if err := c.writeLine(payload); err != nil {
		return nil, fmt.Errorf("向 mpv 发送命令 %v 失败（连接可能已断开）：%w", cmd, err)
	}

	select {
	case msg, ok := <-ch:
		if !ok {
			return nil, errors.New("mpv 连接在等待响应时断开，命令未完成；请重新发起播放")
		}
		if msg.Error != "" && msg.Error != "success" {
			return nil, fmt.Errorf("mpv 拒绝命令 %v：%s", cmd, msg.Error)
		}
		return msg.Data, nil
	case <-time.After(timeout):
		return nil, fmt.Errorf("mpv 在 %s 内未响应命令 %v，播放器可能已卡死；可关闭播放器后重试", timeout, cmd)
	}
}

// register 分配 request_id 并登记等待通道；连接已关闭时返回错误。
func (c *ipcConn) register() (int64, chan ipcMessage, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return 0, nil, errors.New("IPC 连接已关闭")
	}
	c.nextID++
	id := c.nextID
	ch := make(chan ipcMessage, 1) // 带缓冲：读循环投递时永不阻塞
	c.pending[id] = ch
	return id, ch, nil
}

func (c *ipcConn) unregister(id int64) {
	c.mu.Lock()
	delete(c.pending, id)
	c.mu.Unlock()
}

func (c *ipcConn) writeLine(payload []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_, err := c.conn.Write(append(payload, '\n'))
	return err
}

// readLoop 逐行读取 mpv 输出：事件交给 onEvent，响应按 request_id 派发。
// 循环退出（EOF、读错误、shutdown 关闭连接）即触发断开收敛。
func (c *ipcConn) readLoop() {
	sc := bufio.NewScanner(c.conn)
	sc.Buffer(make([]byte, 0, 64*1024), maxIPCLine)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var msg ipcMessage
		if err := json.Unmarshal(line, &msg); err != nil {
			// mpv 的 IPC 协议保证逐行 JSON，实际只有超长被截断才会走到这里，
			// 而截断会让 Scanner 直接报 ErrTooLong 退出循环。单行解析失败不足以
			// 判定整条连接失效，跳过该行；连接级错误统一由循环退出路径上抛。
			continue
		}
		if msg.Event != "" {
			c.onEvent(msg)
			continue
		}
		c.dispatch(msg)
	}

	var reason error
	if err := sc.Err(); err != nil {
		reason = fmt.Errorf("读取 mpv IPC 数据失败：%w", err)
	}
	c.shutdown(reason)
}

// dispatch 把命令响应投递给等待的调用方；无人等待（如已超时放弃）则丢弃。
func (c *ipcConn) dispatch(msg ipcMessage) {
	c.mu.Lock()
	ch, ok := c.pending[msg.RequestID]
	if ok {
		delete(c.pending, msg.RequestID)
	}
	c.mu.Unlock()
	if ok {
		ch <- msg // 缓冲为 1 且登记后只投递一次，不会阻塞
	}
}

// shutdown 收敛连接：标记关闭、唤醒所有挂起的调用、关闭底层连接，
// 然后恰好一次地通知 onDisconnect。可被读循环与外部并发调用，幂等。
func (c *ipcConn) shutdown(reason error) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.closed = true
	for id, ch := range c.pending {
		delete(c.pending, id)
		close(ch) // 挂起的 call 立即返回错误，不用干等超时
	}
	c.mu.Unlock()

	_ = c.conn.Close() // 促使读循环退出；重复关闭的错误无意义，忽略
	if c.onDisconnect != nil {
		c.onDisconnect(reason)
	}
}
