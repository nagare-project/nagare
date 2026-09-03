package minisign

// 本文件是 .minisig 文本 → 结构体的解析，以及公用的行拆分/base64 解码。
// 解析层只管「格式对不对」，一个密码学判断都不做；判断在 minisign.go 的 Verify 里。

import (
	"crypto/ed25519"
	"encoding/base64"
	"fmt"
	"strings"
)

// parsedSignature 是 .minisig 四行拆开后的结果。
type parsedSignature struct {
	alg            string                      // "Ed" 或 "ED"
	keyID          [keyIDLen]byte              // 签名声明自己出自哪把钥匙
	sig            [ed25519.SignatureSize]byte // 第一段：对消息（或其 BLAKE2b-512）的签名
	trustedComment string                      // 已剥掉 "trusted comment: " 前缀，未经认证
	globalSig      [ed25519.SignatureSize]byte // 第二段：对 sig ‖ trustedComment 的签名
}

// splitLines 把文本按行拆开，只做两件事：
//  1. 剥掉行尾的 \r（Windows 上 \r\n 很常见，CI 里从 artifact 取回的文件也可能是）
//  2. 丢掉末尾的空行（编辑器和 shell 重定向都爱多留一个换行）
//
// 刻意【不】trim 每行两侧的空白：trusted comment 的内容是第二段签名逐字节覆盖的原文，
// 多剥一个字符校验就对不上了。base64 那两行由调用处单独 TrimSpace —— 它们内容定长，
// 宽容一点不会引入歧义。
func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	lines := strings.Split(s, "\n")
	for i := range lines {
		lines[i] = strings.TrimSuffix(lines[i], "\r")
	}
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// decodeBase64 用严格模式解码：拒绝末尾 padding 位里塞了垃圾的输入，
// 免得同一份签名存在多种等价编码（那会让「文件有没有被动过」这个问题变得含糊）。
func decodeBase64(s string) ([]byte, error) {
	return base64.StdEncoding.Strict().DecodeString(strings.TrimSpace(s))
}

// parseSignature 解析 .minisig 的完整内容。
//
// 四行的形状是固定的，行数不对就直接拒 —— 下到 HTML 错误页、半截文件、
// 或者把 checksums.txt 本身当签名喂进来，都会在这里被挡住。
func parseSignature(sig []byte) (parsedSignature, error) {
	const op = "minisign.parse_signature"
	var out parsedSignature

	if len(sig) > maxInputSize {
		return out, sigErr(op, "签名文件异常地大，已拒绝解析", recoveryRedownload,
			fmt.Errorf("%w: %d bytes > %d", ErrTooLarge, len(sig), maxInputSize))
	}

	lines := splitLines(string(sig))
	if len(lines) != sigFileLines {
		return out, sigErr(op, "签名文件格式不合法（应为四行）", recoveryRedownload,
			fmt.Errorf("%w: expected %d lines, got %d", ErrSignatureFormat, sigFileLines, len(lines)))
	}
	if !strings.HasPrefix(lines[0], untrustedPrefix) {
		return out, sigErr(op, "签名文件格式不合法（第一行不是 untrusted comment）", recoveryRedownload,
			fmt.Errorf("%w: line 1 missing %q prefix", ErrSignatureFormat, untrustedPrefix))
	}
	if !strings.HasPrefix(lines[2], trustedPrefix) {
		// 这里必须按整个前缀（含结尾那个空格）比：minisign 签的是前缀之后的原文，
		// 少剥或多剥一个空格，算出来的待验内容就和 minisign 不是同一串。
		return out, sigErr(op, "签名文件格式不合法（第三行不是 trusted comment）", recoveryRedownload,
			fmt.Errorf("%w: line 3 missing %q prefix", ErrSignatureFormat, trustedPrefix))
	}
	out.trustedComment = lines[2][len(trustedPrefix):]

	raw, err := decodeBase64(lines[1])
	if err != nil {
		return out, sigErr(op, "签名文件损坏（第二行不是合法的 base64）", recoveryRedownload,
			fmt.Errorf("%w: line 2: %v", ErrSignatureFormat, err))
	}
	if len(raw) != rawSignatureLen {
		return out, sigErr(op, "签名文件损坏（签名长度不对）", recoveryRedownload,
			fmt.Errorf("%w: line 2 decoded to %d bytes, want %d", ErrSignatureFormat, len(raw), rawSignatureLen))
	}
	out.alg = string(raw[:algLen])
	if out.alg != algPure && out.alg != algPrehashed {
		return out, sigErr(op, "签名用了 nagare 不认识的算法", recoveryUpgrade,
			fmt.Errorf("%w: %q", ErrUnsupportedAlgorithm, out.alg))
	}
	copy(out.keyID[:], raw[algLen:algLen+keyIDLen])
	copy(out.sig[:], raw[algLen+keyIDLen:])

	global, err := decodeBase64(lines[3])
	if err != nil {
		return out, sigErr(op, "签名文件损坏（第四行不是合法的 base64）", recoveryRedownload,
			fmt.Errorf("%w: line 4: %v", ErrSignatureFormat, err))
	}
	if len(global) != ed25519.SignatureSize {
		return out, sigErr(op, "签名文件损坏（全局签名长度不对）", recoveryRedownload,
			fmt.Errorf("%w: line 4 decoded to %d bytes, want %d", ErrSignatureFormat, len(global), ed25519.SignatureSize))
	}
	copy(out.globalSig[:], global)

	return out, nil
}
