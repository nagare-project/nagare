// Package random 提供加密安全的随机十六进制串，供鉴权 token 与流端点能力 URL 共用。
package random

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

// Hex 返回 n 个随机字节的十六进制表示（长度 2n）。n 必须为正。
func Hex(n int) string {
	if n <= 0 {
		panic("random: 字节数必须为正")
	}
	b := make([]byte, n)
	// 按当前标准库契约 crypto/rand.Read 永不返回错误（内部失败会直接终止进程），
	// 这里的检查是对未来契约变化的保险，而不是可达路径。
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("random: 读取系统熵源失败: %v", err))
	}
	return hex.EncodeToString(b)
}
