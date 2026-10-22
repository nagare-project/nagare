//go:build desktoptest && (windows || (darwin && cgo && libmpv))

package desktop

// 测试构建的遥控（testctl_darwin.go / testctl_windows.go）共用的门：Host 头必须正好是监听地址（挡 DNS rebinding），
// 每个请求还要带每次运行随机生成的密钥（挡开发机浏览器里的网页随手发 GET）。正式构建没有这个文件。

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
)

// testSecretHeader 是遥控要求的请求头：值是每次运行随机生成、写进 NAGARE_DESKTOP_TEST_SECRET_FILE 的密钥。
const testSecretHeader = "X-Nagare-Test-Secret"

// writeTestSecret 生成这次运行的密钥写进 path（0600，先删掉旧的），返回密钥。
func writeTestSecret(path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("没有设 NAGARE_DESKTOP_TEST_SECRET_FILE（遥控的密钥写在那里）")
	}
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("生成密钥失败：%w", err)
	}
	secret := hex.EncodeToString(buf)
	_ = os.Remove(path)
	if err := os.WriteFile(path, []byte(secret), 0o600); err != nil {
		return "", fmt.Errorf("写不了密钥文件：%w", err)
	}
	return secret, nil
}

// guardTestControl 只放行 Host 正好是 addr、且带着正确密钥的请求。
func guardTestControl(addr, secret string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != addr {
			http.Error(w, "forbidden host", http.StatusForbidden)
			return
		}
		got := r.Header.Get(testSecretHeader)
		if subtle.ConstantTimeCompare([]byte(got), []byte(secret)) != 1 {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}
