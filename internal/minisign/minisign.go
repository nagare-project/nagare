// Package minisign 只做 minisign 签名校验，不做签名生成。
//
// # 为什么自己写
//
// nagare 是零证书发布：安装包不做代码签名，更新包的完整性完全压在 minisign 签名上
// （CI 用 minisign 签 checksums.txt，客户端内嵌公钥校验它，再用其中的 sha256 校验归档）。
// 这段代码保护的正是「供应链」这条路，在这里再引一个第三方依赖等于把风险搬了个位置。
// 而 minisign 的校验只是「解析一个文本格式 + 调 crypto/ed25519.Verify」，
// 不涉及自己实现任何密码学原语 —— 标准库做完了全部危险的部分。
//
// # 格式（jedisct1/minisign）
//
// 公钥文件（minisign -G 产出的 .pub）两行：
//
//	untrusted comment: <任意文本>
//	<base64>
//
// base64 解出 42 字节：[2]签名算法("Ed") + [8]key id + [32]ed25519 公钥。
//
// 签名文件（.minisig）四行：
//
//	untrusted comment: <任意文本>
//	<base64 A>
//	trusted comment: <任意文本>
//	<base64 B>
//
// 各行的含义：
//
//   - base64 A 解出 74 字节：[2]算法 + [8]key id + [64]签名。
//   - 算法 "Ed"：签名是对【原始消息】做的 ed25519 签名。
//   - 算法 "ED"：签名是对【BLAKE2b-512(原始消息)】做的，即 minisign 的预哈希模式；
//     大文件默认走这条，所以它是常态而不是特例。
//   - base64 B 解出 64 字节的【全局签名】，覆盖「那 64 字节签名 ‖ trusted comment 文本」，
//     直接拼接，中间没有换行。
//
// # 两段签名都必须验
//
// 只验第一段的话，trusted comment 可以被任意篡改而校验仍然通过 —— 那正是 minisign
// 设计双签名的原因。trusted comment 里放的是版本号、文件名、时间戳这类会被拿去做决策的
// 元数据，它必须和内容一样是被签过的。
//
// 三条命名上容易搞混的事，记在这里：
//   - untrusted comment（第一行）任何签名都没覆盖，它可以被随便改，改了也照样验得过。
//   - trusted comment（第三行）由第二段全局签名覆盖，Verify 通过后才可信。
//   - key id 不是秘密，它只是「这个签名声称出自哪把钥匙」的提示，不构成任何安全边界。
package minisign

import (
	"crypto/ed25519"
	"crypto/subtle"
	"fmt"
	"strings"

	"golang.org/x/crypto/blake2b"
)

const (
	// maxInputSize 是公钥/签名文本的长度上限。两者都是四行以内的小文本，8 KiB 已经
	// 宽裕得离谱；上限的意义是让「把 300MB 的归档当签名喂进来」这类输入在解析前就被拒，
	// 而不是先按行切一遍再失败。
	maxInputSize = 8 << 10

	// algPure 表示签名直接对原始消息做；algPrehashed 表示对 BLAKE2b-512(消息) 做。
	algPure      = "Ed"
	algPrehashed = "ED"

	algLen   = 2
	keyIDLen = 8

	rawPublicKeyLen = algLen + keyIDLen + ed25519.PublicKeySize // 42
	rawSignatureLen = algLen + keyIDLen + ed25519.SignatureSize // 74

	// 前缀必须带上结尾那个空格：minisign 签的是【前缀之后】的原文，
	// 少剥或多剥一个空格算出来的待验内容就和 minisign 不是同一串了。
	untrustedPrefix = "untrusted comment: "
	trustedPrefix   = "trusted comment: "

	sigFileLines = 4
)

// PublicKey 是解析后的 minisign 公钥。
//
// 零值不可用，必须经 ParsePublicKey 取得。字段用定长数组而不是切片，
// 这样 PublicKey 传值时不与调用方共享底层内存。
type PublicKey struct {
	keyID [keyIDLen]byte
	key   [ed25519.PublicKeySize]byte
}

// ParsePublicKey 解析 minisign 公钥。既接受完整的 .pub 文件内容（两行，第一行是
// untrusted comment），也接受单独的 base64 那一行 —— 公钥是要内嵌进源码常量的，
// 写成一行更方便。
//
// 公钥文件里的 untrusted comment 没有任何签名覆盖，这里也就不校验它的内容，
// 只按行数认位置（与 minisign 本身的做法一致）。
//
// 失败一律归 errs.CategoryInternal：内嵌公钥不合法说明构建时就配错了，
// 跟用户的网络、跟下载到的文件都没关系。
func ParsePublicKey(s string) (PublicKey, error) {
	const op = "minisign.parse_public_key"

	if len(s) > maxInputSize {
		return PublicKey{}, keyErr(op, "内置的发布公钥异常地大，已拒绝解析",
			fmt.Errorf("%w: %d bytes > %d", ErrTooLarge, len(s), maxInputSize))
	}

	lines := splitLines(s)
	// 公钥常写成 Go 的反引号字面量，开头很容易多一个换行。这一段空行没有任何
	// 签名覆盖，丢掉它是安全的（签名文件那边就没有这份宽容，那里严格要求四行）。
	for len(lines) > 0 && strings.TrimSpace(lines[0]) == "" {
		lines = lines[1:]
	}

	var b64 string
	switch len(lines) {
	case 1:
		b64 = lines[0]
	case 2: // 完整 .pub 文件：第一行是注释，第二行才是本体
		b64 = lines[1]
	default:
		return PublicKey{}, keyErr(op, "内置的发布公钥格式不合法",
			fmt.Errorf("%w: expected 1 or 2 lines, got %d", ErrPublicKeyFormat, len(lines)))
	}

	raw, err := decodeBase64(b64)
	if err != nil {
		return PublicKey{}, keyErr(op, "内置的发布公钥不是合法的 base64",
			fmt.Errorf("%w: %v", ErrPublicKeyFormat, err))
	}
	if len(raw) != rawPublicKeyLen {
		return PublicKey{}, keyErr(op, "内置的发布公钥长度不对",
			fmt.Errorf("%w: decoded to %d bytes, want %d", ErrPublicKeyFormat, len(raw), rawPublicKeyLen))
	}
	// 公钥里的算法字段恒为 "Ed"：预哈希与否是【签名】的属性，不是钥匙的属性。
	if alg := string(raw[:algLen]); alg != algPure {
		return PublicKey{}, keyErr(op, "内置的发布公钥用了未知的算法",
			fmt.Errorf("%w: %q, want %q", ErrUnsupportedAlgorithm, alg, algPure))
	}

	var pub PublicKey
	copy(pub.keyID[:], raw[algLen:algLen+keyIDLen])
	copy(pub.key[:], raw[algLen+keyIDLen:])
	return pub, nil
}

// Verify 用 pub 校验 message 的 minisign 签名。sig 是 .minisig 文件的完整内容。
// 校验通过返回 nil；任何一步不通过都返回错误。
//
// 返回的错误可以用 errors.Is 分辨具体原因：ErrSignatureFormat / ErrUnsupportedAlgorithm /
// ErrKeyIDMismatch / ErrSignatureMismatch / ErrGlobalSignature / ErrTooLarge。
func Verify(pub PublicKey, message, sig []byte) error {
	const op = "minisign.verify"

	parsed, err := parseSignature(sig)
	if err != nil {
		return err
	}

	// key id 先比。不一致要明确报「这个签名不是这把公钥签的」，而不是笼统地报
	// 「校验失败」—— 前者指向发布流程配错了钥匙或用户下错了来源，后者指向文件被改过，
	// 排查方向完全相反。
	//
	// key id 不是秘密，但按位早退的比较是个坏习惯，不在密码学代码里留这种示范。
	if subtle.ConstantTimeCompare(parsed.keyID[:], pub.keyID[:]) != 1 {
		return sigErr(op, "这个签名不是用 nagare 的发布公钥签的", recoverySource,
			fmt.Errorf("%w: signature key id %X, public key id %X",
				ErrKeyIDMismatch, parsed.keyID, pub.keyID))
	}

	// "ED" 是预哈希模式：签名覆盖的是 BLAKE2b-512(消息) 而不是消息本身。
	signed := message
	if parsed.alg == algPrehashed {
		sum := blake2b.Sum512(message)
		signed = sum[:]
	}

	key := ed25519.PublicKey(pub.key[:])
	if !ed25519.Verify(key, signed, parsed.sig[:]) {
		return sigErr(op, "签名与文件内容对不上，文件可能已损坏或被篡改", recoveryRedownload,
			ErrSignatureMismatch)
	}

	// 第二段：全局签名覆盖「那 64 字节签名 ‖ trusted comment 文本」，直接拼接、无换行。
	// 少了这一步，trusted comment 里的版本号/文件名可以被任意改写而校验照样通过。
	global := make([]byte, 0, len(parsed.sig)+len(parsed.trustedComment))
	global = append(global, parsed.sig[:]...)
	global = append(global, parsed.trustedComment...)
	if !ed25519.Verify(key, global, parsed.globalSig[:]) {
		return sigErr(op, "签名里的附注信息（trusted comment）被改过", recoveryRedownload,
			ErrGlobalSignature)
	}

	return nil
}

// TrustedComment 取出签名里的 trusted comment（已剥掉 "trusted comment: " 前缀）。
//
// 【只应在 Verify 成功之后调用】—— 在那之前它是未经认证的、攻击者完全可控的文本：
// 它就在签名文件里明文放着，谁都能改。只有 Verify 通过（第二段全局签名验过）之后，
// 这段文本才是可信的。把它拿去比对版本号、拼文件路径、写日志之前，先确认 Verify 过了。
func TrustedComment(sig []byte) (string, error) {
	parsed, err := parseSignature(sig)
	if err != nil {
		return "", err
	}
	return parsed.trustedComment, nil
}
