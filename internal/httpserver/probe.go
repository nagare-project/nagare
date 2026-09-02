package httpserver

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

const (
	// probeTimeout 是单实例探测的总时限：本机回环，1 秒足够。
	probeTimeout = time.Second
	// probeMaxBody 限制读取的响应体：健康探针的信封只有几十字节。
	probeMaxBody = 4096
	// instanceFileName 是「我方实例正在运行」的标记文件（内含 PID），
	// 落在 0700 的配置目录里 —— 别的 OS 用户建不出来，这正是它的价值所在。
	instanceFileName = "instance.pid"
)

// ⚠️ 身份判定的边界（安全评审 2026-09-02 的结论，改前先读）：
//
// /api/health 的成功信封是【公开且固定】的形状，任何本地进程都能照抄一份返回，
// 无须知道 token。所以「响应长得像我们」证明不了对端就是我们 —— 它只能否证。
// 一旦把 token 发给冒充者，鉴权就整个失效（token 在手，Host 白名单与 CSRF 头都拦不住）。
//
// 因此 token 只在 OS 层面已经给出「同用户实例确实起过」的信号时才发出：
// 调用方必须先用 HasInstance（标记文件在 0700 目录内，跨用户伪造不了）把门，
// 再调 Probe。没有标记却发现端口被占 —— 那一定不是我们，直接按外来处理。

// ProbeResult 是 Probe 的三态结论。
type ProbeResult int

const (
	// ProbeNone：端口上没有任何进程在监听。
	ProbeNone ProbeResult = iota
	// ProbeRunning：端口上是持相同 token 的 nagare 实例（用户双击了两次）。
	ProbeRunning
	// ProbeForeign：端口被别的进程占着（或是另一份配置的 nagare）。
	// 探测请求已把 token 发给了它 —— 调用方应当视 token 为已泄露并轮换。
	ProbeForeign
)

// ProbeExisting 报告 127.0.0.1:port 上是否已经跑着一个持相同 token 的 nagare。
// 只有拿到我们自己的信封且 data.status=="ok" 才算命中。
// 同 Probe：会把 token 发给应答方，调用前必须先过 HasInstance。
func ProbeExisting(port int, token string) bool {
	return Probe(port, token) == ProbeRunning
}

// InstancePath 返回实例标记文件路径。
func InstancePath(configDir string) string {
	return filepath.Join(configDir, instanceFileName)
}

// MarkInstance 在成功监听后写下本进程 PID（0600）：下次启动据此判断该不该做带 token 的探测。
func MarkInstance(configDir string) error {
	path := InstancePath(configDir)
	if err := os.WriteFile(path, []byte(strconv.Itoa(os.Getpid())), 0o600); err != nil {
		return fmt.Errorf("写入实例标记 %s: %w", path, err)
	}
	return nil
}

// ClearInstance 退出时清掉标记。删不掉不致命：下次启动最多多做一次探测。
func ClearInstance(configDir string) { _ = os.Remove(InstancePath(configDir)) }

// HasInstance 报告标记文件是否存在 —— 即「本机同一用户下曾有实例起来过且未干净退出」。
// 它不证明进程还活着（崩溃会留下陈旧标记），只用来决定「能不能把 token 发出去」。
func HasInstance(configDir string) bool {
	_, err := os.Stat(InstancePath(configDir))
	return !errors.Is(err, os.ErrNotExist)
}

// PortOccupied 只做一次裸连：探测端口上有没有人，全程不发送任何凭据。
func PortOccupied(port int) bool {
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), probeTimeout)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// Probe 先裸连一次分辨「没人」与「有人」，再对有人的端口打带 token 的 /api/health：
// 裸连失败不发 token（无泄露）；有人但答不出我们的信封 → ProbeForeign。
//
// 为什么要区分 Foreign：同一台机器上另一个用户抢占了这个回环端口，就会收到我们的
// token，而 nagare 接着会在相邻端口起来 —— 那个 token 对新实例依然有效。
// 调用方在 Foreign 时轮换 token 即可把这条路堵死。
func Probe(port int, token string) ProbeResult {
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	conn, err := net.DialTimeout("tcp", addr, probeTimeout)
	if err != nil {
		return ProbeNone
	}
	_ = conn.Close()

	if healthOK(addr, token) {
		return ProbeRunning
	}
	return ProbeForeign
}

// healthOK 带 token 请求 /api/health，只认成功信封里的 status=="ok"。
func healthOK(addr, token string) bool {
	client := &http.Client{Timeout: probeTimeout}
	req, err := http.NewRequest(http.MethodGet, "http://"+addr+"/api/health", nil)
	if err != nil {
		return false
	}
	req.Header.Set(TokenHeader, token)
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false
	}

	var env struct {
		Success bool `json:"success"`
		Data    struct {
			Status string `json:"status"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, probeMaxBody)).Decode(&env); err != nil {
		return false
	}
	return env.Success && env.Data.Status == "ok"
}
