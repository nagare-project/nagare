package httpserver

import (
	"fmt"
	"net"
)

// maxPortProbes 是端口向上探测的最大次数。
const maxPortProbes = 100

// maxTCPPort 是合法 TCP 端口上限。
const maxTCPPort = 65535

// Listen 在 127.0.0.1 上监听：preferred 被占时向上顺延（最多 maxPortProbes 个）。
// 刻意不监听 0.0.0.0 —— 本机服务不上局域网，这是决议 A4 的边界。
func Listen(preferred int) (net.Listener, int, error) {
	for i := 0; i < maxPortProbes; i++ {
		port := preferred + i
		if port > maxTCPPort {
			break
		}
		ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err == nil {
			return ln, port, nil
		}
	}
	return nil, 0, fmt.Errorf("从 %d 起连续 %d 个端口都不可用", preferred, maxPortProbes)
}
