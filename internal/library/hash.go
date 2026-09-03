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

// Hash16MBytes 是 dandanplay 匹配约定的取样长度。
//
// 导出是因为磁力边下边播要靠它决定「头部至少得先把多少字节钉下来」才可能算出
// 这个哈希 —— 取样长度只能有一个来源，两边各写死一份必然漂移。
const Hash16MBytes = 16 * 1024 * 1024

// Hash16MFrom 对 r 的前 16MB 计算 MD5（小写 hex），与 dandanplay / animego 网页端
// （SparkMD5 worker）的取样方式一致。可读内容不足 16MB 时对全部内容计算。
//
// 取 io.Reader 而不是路径：磁力播放的字节来自种子 reader，没有本地路径可读，
// 而两条来源必须共用同一份取样长度与算法，否则同一个文件两边算出两个哈希。
func Hash16MFrom(r io.Reader) (string, error) {
	h := md5.New()
	if _, err := io.Copy(h, io.LimitReader(r, Hash16MBytes)); err != nil {
		return "", fmt.Errorf("读取内容计算 hash: %w", err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// Hash16M 计算本地文件首 16MB 的 MD5，是 Hash16MFrom 的路径版薄包装。
func Hash16M(absPath string) (string, error) {
	f, err := os.Open(absPath)
	if err != nil {
		return "", fmt.Errorf("打开文件计算 hash %s: %w", absPath, err)
	}
	defer f.Close()

	sum, err := Hash16MFrom(f)
	if err != nil {
		// 不再套一层「计算文件 hash」：Hash16MFrom 的错误里已经有「读取内容计算 hash」，
		// 两层拼起来读着像出了两次错。这里只补它缺的那一项 —— 是哪个文件。
		return "", fmt.Errorf("%w（文件 %s）", err, absPath)
	}
	return sum, nil
}
