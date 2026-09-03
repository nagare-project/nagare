package minisign

// 本机没有 minisign 命令行工具，所以测试向量在测试里自己造：用 crypto/ed25519
// 造密钥对，按格式规范【手工拼】出公钥串与 .minisig 内容，再喂给 Verify。
// 这反而是更好的测试方式 —— 它把格式规范本身编码进了测试：实现要是和规范对不上，
// 这里就会先炸，而不是等到线上验不过某个真实的发布签名。
//
// 与真 minisign 的互通性由 interop_test.go 里的已知向量负责。

import (
	"crypto/ed25519"
	"encoding/base64"
	"strings"
	"testing"

	errs "github.com/nagare-project/nagare/internal/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/blake2b"
)

// ---------------------------------------------------------------- 测试脚手架

type testKey struct {
	pub  ed25519.PublicKey
	priv ed25519.PrivateKey
	id   [keyIDLen]byte
}

func newTestKey(t testing.TB, idSeed byte) testKey {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	var id [keyIDLen]byte
	for i := range id {
		id[i] = idSeed + byte(i)
	}
	return testKey{pub: pub, priv: priv, id: id}
}

// pubLine 拼出 .pub 里的 base64 那一行：[2]"Ed" + [8]key id + [32]公钥。
func (k testKey) pubLine() string {
	raw := make([]byte, 0, rawPublicKeyLen)
	raw = append(raw, algPure...)
	raw = append(raw, k.id[:]...)
	raw = append(raw, k.pub...)
	return base64.StdEncoding.EncodeToString(raw)
}

// pubFile 拼出完整的两行 .pub 文件。
func (k testKey) pubFile() string {
	return "untrusted comment: minisign public key TEST\n" + k.pubLine() + "\n"
}

func (k testKey) parsed(t testing.TB) PublicKey {
	t.Helper()
	pk, err := ParsePublicKey(k.pubLine())
	require.NoError(t, err)
	return pk
}

// sigFile 是 .minisig 的四行，拆开放着，测试可以逐行改动再拼回去。
type sigFile struct {
	untrusted string // 不含 "untrusted comment: " 前缀
	sigB64    string
	trusted   string // 不含 "trusted comment: " 前缀
	globalB64 string
}

func (f sigFile) String() string {
	return untrustedPrefix + f.untrusted + "\n" +
		f.sigB64 + "\n" +
		trustedPrefix + f.trusted + "\n" +
		f.globalB64 + "\n"
}

func (f sigFile) bytes() []byte { return []byte(f.String()) }

// makeSig 按规范手工签一份 .minisig。alg 传 algPure("Ed") 或 algPrehashed("ED")。
func makeSig(t testing.TB, k testKey, alg string, msg []byte, trusted string) sigFile {
	t.Helper()
	require.Contains(t, []string{algPure, algPrehashed}, alg)

	signed := msg
	if alg == algPrehashed {
		sum := blake2b.Sum512(msg)
		signed = sum[:]
	}
	sig := ed25519.Sign(k.priv, signed)

	raw := make([]byte, 0, rawSignatureLen)
	raw = append(raw, alg...)
	raw = append(raw, k.id[:]...)
	raw = append(raw, sig...)

	// 全局签名覆盖「那 64 字节签名 ‖ trusted comment 文本」，直接拼接、无换行。
	global := ed25519.Sign(k.priv, append(append([]byte{}, sig...), trusted...))

	return sigFile{
		untrusted: "signature from test key",
		sigB64:    base64.StdEncoding.EncodeToString(raw),
		trusted:   trusted,
		globalB64: base64.StdEncoding.EncodeToString(global),
	}
}

// requireNagareError 断言错误是 *errs.E，落在指定分类，且带齐用户提示与恢复动作。
// 「错误不静默」这条约定在这里体面地落地：每一条失败路径都要能对用户说人话。
func requireNagareError(t testing.TB, err error, want errs.Category, sentinel error) {
	t.Helper()
	require.Error(t, err)
	assert.ErrorIs(t, err, sentinel)

	var e *errs.E
	require.ErrorAs(t, err, &e, "错误必须是 *errs.E 才能进界面")
	assert.Equal(t, want, e.Category)
	assert.NotEmpty(t, e.UserMsg, "每条失败都要有中文 UserMsg")
	assert.NotEmpty(t, e.Recovery, "每条失败都要告诉用户能做什么")
	assert.NotEmpty(t, e.Op)
}

// ---------------------------------------------------------------- 正常路径

func TestVerifyAcceptsValidSignature(t *testing.T) {
	msg := []byte("nagare-1.2.3_MacOS_arm64.tar.gz  d0c5...\n")

	for _, alg := range []string{algPure, algPrehashed} {
		t.Run(alg, func(t *testing.T) {
			k := newTestKey(t, 0x10)
			sig := makeSig(t, k, alg, msg, "timestamp:1756800000\tfile:checksums.txt")
			require.NoError(t, Verify(k.parsed(t), msg, sig.bytes()))
		})
	}
}

func TestVerifyAcceptsEmptyMessageAndEmptyComment(t *testing.T) {
	k := newTestKey(t, 0x20)
	// trusted comment 为空时，minisign 写出来的第三行就是 "trusted comment: " 本身。
	sig := makeSig(t, k, algPure, nil, "")
	require.NoError(t, Verify(k.parsed(t), nil, sig.bytes()))

	got, err := TrustedComment(sig.bytes())
	require.NoError(t, err)
	assert.Equal(t, "", got)
}

func TestVerifyIgnoresUntrustedComment(t *testing.T) {
	// untrusted comment 不被任何一段签名覆盖 —— 这正是它叫「untrusted」的原因。
	// 改了它校验照样通过，这条测试是把这个事实钉住，免得有人日后拿它当可信信息用。
	k := newTestKey(t, 0x30)
	msg := []byte("payload")
	sig := makeSig(t, k, algPure, msg, "timestamp:1")

	sig.untrusted = "totally different text written by anyone"
	require.NoError(t, Verify(k.parsed(t), msg, sig.bytes()))
}

func TestTrustedCommentReturnsContent(t *testing.T) {
	k := newTestKey(t, 0x40)
	msg := []byte("payload")
	const comment = "timestamp:1756800000\tfile:checksums.txt\thashed"
	sig := makeSig(t, k, algPrehashed, msg, comment)

	require.NoError(t, Verify(k.parsed(t), msg, sig.bytes()))
	got, err := TrustedComment(sig.bytes())
	require.NoError(t, err)
	assert.Equal(t, comment, got)
}

func TestTrustedCommentRejectsMalformedSignature(t *testing.T) {
	_, err := TrustedComment([]byte("not a signature"))
	requireNagareError(t, err, errs.CategoryUpstream, ErrSignatureFormat)
}

// ---------------------------------------------------------------- 篡改路径

func TestVerifyRejectsModifiedMessage(t *testing.T) {
	for _, alg := range []string{algPure, algPrehashed} {
		t.Run(alg, func(t *testing.T) {
			k := newTestKey(t, 0x50)
			msg := []byte("hash: 0123456789abcdef")
			sig := makeSig(t, k, alg, msg, "timestamp:1")

			tampered := append([]byte(nil), msg...)
			tampered[len(tampered)-1] ^= 0x01 // 只改一个 bit

			err := Verify(k.parsed(t), tampered, sig.bytes())
			requireNagareError(t, err, errs.CategoryUpstream, ErrSignatureMismatch)
		})
	}
}

// TestVerifyRejectsModifiedTrustedComment 是这个包里最重要的一条测试，
// 它就是双签名存在的理由。
//
// trusted comment 里放的是版本号、文件名、时间戳 —— 会被拿去做决策的元数据。
// 它【不】参与第一段签名，所以只验第一段的实现会在这里悄悄放行：文件内容没动，
// 第一段签名当然还对得上；被改掉的是附注信息，只有第二段全局签名能发现。
//
// 断言必须是 ErrGlobalSignature 而不是笼统的「有错就行」：如果实现哪天把两段
// 验反了或漏了一段，只断言「有错」的测试会被 ErrSignatureMismatch 蒙混过去。
func TestVerifyRejectsModifiedTrustedComment(t *testing.T) {
	k := newTestKey(t, 0x60)
	msg := []byte("nagare-1.0.0.tar.gz  aaaa\n")
	sig := makeSig(t, k, algPure, msg, "timestamp:1756800000\tfile:checksums.txt")

	// 文件内容一个字节都没动，只改附注 —— 第一段签名此刻仍然是对的。
	sig.trusted = "timestamp:1756800000\tfile:evil.tar.gz"

	err := Verify(k.parsed(t), msg, sig.bytes())
	requireNagareError(t, err, errs.CategoryUpstream, ErrGlobalSignature)
}

func TestVerifyRejectsSignatureFromAnotherKey(t *testing.T) {
	victim := newTestKey(t, 0x70)
	attacker := newTestKey(t, 0x90) // key id 与 victim 不重叠
	msg := []byte("payload")

	sig := makeSig(t, attacker, algPure, msg, "timestamp:1")

	err := Verify(victim.parsed(t), msg, sig.bytes())
	// 报的必须是「这个签名不是这把公钥签的」，而不是笼统的校验失败 ——
	// 两者指向的排查方向完全相反。
	requireNagareError(t, err, errs.CategoryUpstream, ErrKeyIDMismatch)
}

func TestVerifyRejectsForgedSignatureWithMatchingKeyID(t *testing.T) {
	// key id 只是「这个签名声称出自哪把钥匙」的提示，不构成任何安全边界：
	// 攻击者当然可以把它改成受害者的 id。改完以后就轮到 ed25519 说话了。
	victim := newTestKey(t, 0x70)
	attacker := newTestKey(t, 0x90)
	msg := []byte("payload")

	sig := makeSig(t, attacker, algPure, msg, "timestamp:1")
	raw, err := base64.StdEncoding.DecodeString(sig.sigB64)
	require.NoError(t, err)
	copy(raw[algLen:algLen+keyIDLen], victim.id[:]) // 冒充 key id
	sig.sigB64 = base64.StdEncoding.EncodeToString(raw)

	err = Verify(victim.parsed(t), msg, sig.bytes())
	requireNagareError(t, err, errs.CategoryUpstream, ErrSignatureMismatch)
}

func TestVerifyRejectsUnsupportedAlgorithm(t *testing.T) {
	k := newTestKey(t, 0x80)
	msg := []byte("payload")
	sig := makeSig(t, k, algPure, msg, "timestamp:1")

	raw, err := base64.StdEncoding.DecodeString(sig.sigB64)
	require.NoError(t, err)
	copy(raw[:algLen], "Xx")
	sig.sigB64 = base64.StdEncoding.EncodeToString(raw)

	err = Verify(k.parsed(t), msg, sig.bytes())
	requireNagareError(t, err, errs.CategoryUpstream, ErrUnsupportedAlgorithm)
}

func TestVerifyRejectsTamperedGlobalSignature(t *testing.T) {
	k := newTestKey(t, 0x11)
	msg := []byte("payload")
	sig := makeSig(t, k, algPure, msg, "timestamp:1")

	raw, err := base64.StdEncoding.DecodeString(sig.globalB64)
	require.NoError(t, err)
	raw[0] ^= 0x01
	sig.globalB64 = base64.StdEncoding.EncodeToString(raw)

	err = Verify(k.parsed(t), msg, sig.bytes())
	requireNagareError(t, err, errs.CategoryUpstream, ErrGlobalSignature)
}

// ---------------------------------------------------------------- 行尾与空行

func TestVerifyHandlesCRLFAndTrailingBlankLines(t *testing.T) {
	k := newTestKey(t, 0xA0)
	msg := []byte("payload")
	sig := makeSig(t, k, algPure, msg, "timestamp:1756800000\tfile:checksums.txt")

	cases := map[string]string{
		"lf":               sig.String(),
		"crlf":             strings.ReplaceAll(sig.String(), "\n", "\r\n"),
		"no trailing lf":   strings.TrimSuffix(sig.String(), "\n"),
		"extra blank line": sig.String() + "\n\n",
		"crlf blank line":  strings.ReplaceAll(sig.String(), "\n", "\r\n") + "\r\n",
	}
	for name, text := range cases {
		t.Run(name, func(t *testing.T) {
			require.NoError(t, Verify(k.parsed(t), msg, []byte(text)))
		})
	}
}

func TestParsePublicKeyAcceptsBothShapes(t *testing.T) {
	k := newTestKey(t, 0xB0)
	msg := []byte("payload")
	sig := makeSig(t, k, algPure, msg, "timestamp:1")

	cases := map[string]string{
		"bare base64 line": k.pubLine(),
		"full .pub file":   k.pubFile(),
		"crlf .pub file":   strings.ReplaceAll(k.pubFile(), "\n", "\r\n"),
		// 内嵌公钥常写成 Go 的反引号字面量，开头很自然会多一个换行。
		"leading newline": "\n" + k.pubFile(),
		"no trailing lf":  strings.TrimSuffix(k.pubFile(), "\n"),
	}
	for name, text := range cases {
		t.Run(name, func(t *testing.T) {
			pk, err := ParsePublicKey(text)
			require.NoError(t, err)
			require.NoError(t, Verify(pk, msg, sig.bytes()))
		})
	}
}

// ---------------------------------------------------------------- 畸形输入

func TestParsePublicKeyRejectsMalformed(t *testing.T) {
	k := newTestKey(t, 0xC0)

	b64OfLen := func(n int) string {
		raw := make([]byte, n)
		copy(raw, algPure)
		return base64.StdEncoding.EncodeToString(raw)
	}

	cases := []struct {
		name     string
		input    string
		sentinel error
	}{
		{"空串", "", ErrPublicKeyFormat},
		{"只有空行", "\n\n\n", ErrPublicKeyFormat},
		{"只有注释行", "untrusted comment: only\n", ErrPublicKeyFormat},
		{"三行", k.pubFile() + "extra line\n", ErrPublicKeyFormat},
		{"base64 非法", "untrusted comment: x\n!!!not base64!!!\n", ErrPublicKeyFormat},
		{"base64 少一个字符（长度不是 4 的倍数）", k.pubLine()[:len(k.pubLine())-1], ErrPublicKeyFormat},
		{"base64 少一组（解出 39 字节）", k.pubLine()[:len(k.pubLine())-4], ErrPublicKeyFormat},
		{"长度 41", b64OfLen(rawPublicKeyLen - 1), ErrPublicKeyFormat},
		{"长度 43", b64OfLen(rawPublicKeyLen + 1), ErrPublicKeyFormat},
		{"零字节", string(make([]byte, 64)), ErrPublicKeyFormat},
		{"二进制垃圾", "\x00\xff\xfe\x01\x02\x03", ErrPublicKeyFormat},
		{"算法字段是 ED", func() string {
			raw, err := base64.StdEncoding.DecodeString(k.pubLine())
			require.NoError(t, err)
			copy(raw[:algLen], algPrehashed) // 预哈希是签名的属性，不是钥匙的
			return base64.StdEncoding.EncodeToString(raw)
		}(), ErrUnsupportedAlgorithm},
		{"算法字段是 Xx", func() string {
			raw, err := base64.StdEncoding.DecodeString(k.pubLine())
			require.NoError(t, err)
			copy(raw[:algLen], "Xx")
			return base64.StdEncoding.EncodeToString(raw)
		}(), ErrUnsupportedAlgorithm},
		{"超长输入", strings.Repeat("A", maxInputSize+1), ErrTooLarge},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParsePublicKey(tc.input)
			// 公钥是编译进二进制的常量，它不合法说明是构建问题，不是用户能修的。
			requireNagareError(t, err, errs.CategoryInternal, tc.sentinel)
		})
	}
}

func TestVerifyRejectsMalformedSignature(t *testing.T) {
	k := newTestKey(t, 0xD0)
	msg := []byte("payload")
	good := makeSig(t, k, algPure, msg, "timestamp:1")
	pk := k.parsed(t)

	b64OfLen := func(n int) string {
		raw := make([]byte, n)
		copy(raw, algPure)
		return base64.StdEncoding.EncodeToString(raw)
	}
	withLines := func(l ...string) string { return strings.Join(l, "\n") + "\n" }

	cases := []struct {
		name     string
		input    string
		sentinel error
	}{
		{"空串", "", ErrSignatureFormat},
		{"只有一行", untrustedPrefix + "x\n", ErrSignatureFormat},
		{"只有三行", withLines(untrustedPrefix+"x", good.sigB64, trustedPrefix+"t"), ErrSignatureFormat},
		{"多出第五行", good.String() + "extra\n", ErrSignatureFormat},
		{"第一行缺前缀", withLines("hello", good.sigB64, trustedPrefix+good.trusted, good.globalB64), ErrSignatureFormat},
		{"第三行缺前缀", withLines(untrustedPrefix+"x", good.sigB64, "trusted: t", good.globalB64), ErrSignatureFormat},
		// 前缀里那个结尾空格是规范的一部分：minisign 签的是前缀之后的原文，
		// 少剥一个空格算出来的待验内容就和 minisign 不是同一串。
		{"第三行前缀少了空格", withLines(untrustedPrefix+"x", good.sigB64, "trusted comment:"+good.trusted, good.globalB64), ErrSignatureFormat},
		{"第二行 base64 非法", withLines(untrustedPrefix+"x", "!!!", trustedPrefix+good.trusted, good.globalB64), ErrSignatureFormat},
		{"第二行 base64 少一个字符", withLines(untrustedPrefix+"x", good.sigB64[:len(good.sigB64)-1], trustedPrefix+good.trusted, good.globalB64), ErrSignatureFormat},
		{"第二行 base64 少一组", withLines(untrustedPrefix+"x", good.sigB64[:len(good.sigB64)-4], trustedPrefix+good.trusted, good.globalB64), ErrSignatureFormat},
		{"第四行 base64 少一组", withLines(untrustedPrefix+"x", good.sigB64, trustedPrefix+good.trusted, good.globalB64[:len(good.globalB64)-4]), ErrSignatureFormat},
		{"第二行长度 73", withLines(untrustedPrefix+"x", b64OfLen(rawSignatureLen-1), trustedPrefix+good.trusted, good.globalB64), ErrSignatureFormat},
		{"第二行长度 75", withLines(untrustedPrefix+"x", b64OfLen(rawSignatureLen+1), trustedPrefix+good.trusted, good.globalB64), ErrSignatureFormat},
		{"第四行 base64 非法", withLines(untrustedPrefix+"x", good.sigB64, trustedPrefix+good.trusted, "!!!"), ErrSignatureFormat},
		{"第四行长度 63", withLines(untrustedPrefix+"x", good.sigB64, trustedPrefix+good.trusted, b64OfLen(ed25519.SignatureSize-1)), ErrSignatureFormat},
		{"第四行长度 65", withLines(untrustedPrefix+"x", good.sigB64, trustedPrefix+good.trusted, b64OfLen(ed25519.SignatureSize+1)), ErrSignatureFormat},
		{"二进制垃圾", "\x00\xff\xfe\x01\x02\x03", ErrSignatureFormat},
		{"零字节四行", string(make([]byte, 4)) + "\n\n\n\n", ErrSignatureFormat},
		{"把 checksums.txt 本身喂进来", "aaaa  nagare.tar.gz\nbbbb  nagare.zip\n", ErrSignatureFormat},
		{"HTML 错误页", "<html><body>404 Not Found</body></html>\n", ErrSignatureFormat},
		{"超长 trusted comment", withLines(untrustedPrefix+"x", good.sigB64,
			trustedPrefix+strings.Repeat("a", maxInputSize), good.globalB64), ErrTooLarge},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := Verify(pk, msg, []byte(tc.input))
			// 签名文件是下载来的，它不对说明「下载到的东西不对」。
			requireNagareError(t, err, errs.CategoryUpstream, tc.sentinel)

			_, cErr := TrustedComment([]byte(tc.input))
			assert.ErrorIs(t, cErr, tc.sentinel, "TrustedComment 走同一条解析路径")
		})
	}
}

// ---------------------------------------------------------------- 内部工具

func TestSplitLines(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{"空串", "", nil},
		{"单行无换行", "a", []string{"a"}},
		{"末尾换行", "a\n", []string{"a"}},
		{"多个末尾空行", "a\n\n\n", []string{"a"}},
		{"crlf", "a\r\nb\r\n", []string{"a", "b"}},
		{"中间空行保留", "a\n\nb\n", []string{"a", "", "b"}},
		// 行内空白不能动：trusted comment 的内容是全局签名逐字节覆盖的原文。
		{"不 trim 行内空白", "  a  \n", []string{"  a  "}},
		{"保留制表符", "a\tb\n", []string{"a\tb"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, splitLines(tc.in))
		})
	}
}

// FuzzVerify 只保证一件事：不管喂进来什么，都不 panic、不越界。
// 普通 go test 会跑一遍种子语料，需要真模糊测试时加 -fuzz=FuzzVerify。
func FuzzVerify(f *testing.F) {
	k := newTestKey(f, 0xE0)
	msg := []byte("payload")
	good := makeSig(f, k, algPure, msg, "timestamp:1\tfile:x")
	pk := k.parsed(f)

	f.Add(good.String(), string(msg))
	f.Add(strings.ReplaceAll(good.String(), "\n", "\r\n"), string(msg))
	f.Add(good.String()[:len(good.String())/2], string(msg))
	f.Add("", "")
	f.Add("untrusted comment: \n\ntrusted comment: \n\n", "x")
	f.Add("\x00\xff\xfe", "\x00")

	f.Fuzz(func(t *testing.T, sig, message string) {
		_ = Verify(pk, []byte(message), []byte(sig))
		_, _ = TrustedComment([]byte(sig))
		_, _ = ParsePublicKey(sig)
	})
}
