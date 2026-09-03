package selfupdate

// 本文件解析 checksums.txt。格式是 sha256sum 的输出：每行 `<64 位十六进制><空白><文件名>`
// （coreutils 用两个空格，二进制模式的第二个是 `*`）。清单本身已经过签名校验，
// 但仍然按不可信输入解析：签名只保证「是发布方给的」，不保证「格式没写错」。

import (
	"bufio"
	"bytes"
	"fmt"
	"regexp"
	"strings"

	errs "github.com/nagare-project/nagare/internal/errors"
)

// sha256HexLen 是十六进制 sha256 的长度。
const sha256HexLen = 64

// hexRE 限定校验和的形状；统一转小写后比较。
var hexRE = regexp.MustCompile(`^[0-9a-f]{64}$`)

// versionTokenRE 从签名的可信注释里挑出版本号形状的 token。
var versionTokenRE = regexp.MustCompile(`\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?`)

// lookupChecksum 从清单里取出 name 的 sha256（小写十六进制）。
//
// 查不到不是「哈希不匹配」而是「这个版本没发这个平台的包」—— 两者的恢复动作不同，
// 必须给出不同的提示，否则用户会以为下载坏了而一直重试。
func lookupChecksum(manifest []byte, name string) (string, error) {
	const op = "selfupdate.checksums"
	found := ""
	sc := bufio.NewScanner(bytes.NewReader(manifest))
	sc.Buffer(make([]byte, 0, 4096), maxMetaBytes)
	for sc.Scan() {
		sum, entry, ok := parseChecksumLine(sc.Text())
		if !ok || entry != name {
			continue
		}
		if found != "" && found != sum {
			// 同一个文件名两条不同的哈希：清单本身是坏的，无法判断该信哪条。
			return "", errs.New(errs.CategoryUpstream, op,
				"校验清单里有互相矛盾的记录，已中止更新", "请到项目发布页手动下载")
		}
		found = sum
	}
	if err := sc.Err(); err != nil {
		return "", errs.Wrap(errs.CategoryUpstream, op, "校验清单格式异常，已中止更新",
			"请到项目发布页手动下载", err)
	}
	if found == "" {
		return "", errs.Wrap(errs.CategoryUpstream, op,
			"这个版本没有适用于当前平台的更新包", "请到项目发布页确认后手动下载",
			fmt.Errorf("校验清单里没有 %s", name))
	}
	return found, nil
}

// parseChecksumLine 解析一行；不是有效记录时返回 ok=false（空行、注释、格式不符都属此类）。
func parseChecksumLine(line string) (sum, name string, ok bool) {
	line = strings.TrimSpace(line)
	if len(line) <= sha256HexLen {
		return "", "", false
	}
	sum = strings.ToLower(line[:sha256HexLen])
	if !hexRE.MatchString(sum) {
		return "", "", false
	}
	rest := strings.TrimLeft(line[sha256HexLen:], " \t")
	if rest == line[sha256HexLen:] {
		// 哈希与文件名之间必须有空白，否则是别的东西恰好以 64 位十六进制开头。
		return "", "", false
	}
	// 二进制模式的 `*` 前缀不是文件名的一部分。
	name = strings.TrimPrefix(rest, "*")
	if name == "" {
		return "", "", false
	}
	return sum, name, true
}
