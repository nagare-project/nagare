// 16MB 懒哈希（决议 P1）：扫描阶段绝不读文件内容，
// 首次播放前才算 dandanplay 匹配要用的「首 16MB MD5」。
package library

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"os"
)

// hash16MBytes 是 dandanplay 匹配约定的取样长度。
const hash16MBytes = 16 * 1024 * 1024

// Hash16M 计算文件首 16MB 的 MD5（小写 hex），与 dandanplay / animego 网页端
// （SparkMD5 worker）的取样方式一致。文件不足 16MB 时对全文件计算。
func Hash16M(absPath string) (string, error) {
	f, err := os.Open(absPath)
	if err != nil {
		return "", fmt.Errorf("打开文件计算 hash %s: %w", absPath, err)
	}
	defer f.Close()

	h := md5.New()
	if _, err := io.Copy(h, io.LimitReader(f, hash16MBytes)); err != nil {
		return "", fmt.Errorf("读取文件计算 hash %s: %w", absPath, err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
