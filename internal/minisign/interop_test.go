package minisign

// 已知向量（known-answer）互通测试。
//
// minisign_test.go 里的向量是本包自己按规范拼的 —— 它能证明实现自洽，证明不了
// 「和真的 minisign 互通」。下面这几组是【真 minisign 产出、公开发布】的签名，
// 逐字节抄自各自仓库，用来钉住互通性：这几条过了，才说明我们对格式的理解和
// 上游一致，而不是自己跟自己对得上。
//
// 来源（2026-09-03 取回）：
//   - jedisct1/go-minisign（minisign 格式与 C 参考实现的作者本人的 Go 绑定）
//     https://raw.githubusercontent.com/jedisct1/go-minisign/master/minisign_test.go
//     TestLegacy（Ed，非预哈希）与 TestPrehashed（ED，预哈希），同一把钥匙
//   - aead/minisign 的 testdata 目录（第三方 Go 实现，提交在仓库里的真实 fixture 文件）
//     https://raw.githubusercontent.com/aead/minisign/main/internal/testdata/
//     minisign.pub / message.txt / message.txt.minisig
//   - aead/minisign 的可执行文档示例 example_test.go 的 ExampleVerify
//
// 注意：trusted comment 行里 timestamp 与 file 之间是【制表符】不是空格。
// 这里一律写成 \t 转义，免得被编辑器或 CI 的空白清理工具悄悄改掉 ——
// 改掉一个字节，第二段全局签名就验不过了。

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// joinSig 把四行拼成 .minisig 的完整内容。
func joinSig(lines ...string) []byte {
	return []byte(strings.Join(lines, "\n") + "\n")
}

func TestVerifyKnownVectors(t *testing.T) {
	cases := []struct {
		name    string
		pub     string // 有的来源只给 base64 一行，有的给完整两行 .pub，两种形状都要能吃
		message []byte
		sig     []byte
		alg     string
		comment string
	}{
		{
			name:    "go-minisign/TestLegacy",
			pub:     "RWQf6LRCGA9i53mlYecO4IzT51TGPpvWucNSCh1CBM0QTaLn73Y7GFO3",
			message: []byte("test"),
			sig: joinSig(
				"untrusted comment: signature from minisign secret key",
				"RWQf6LRCGA9i59SLOFxz6NxvASXDJeRtuZykwQepbDEGt87ig1BNpWaVWuNrm73YiIiJbq71Wi+dP9eKL8OC351vwIasSSbXxwA=",
				"trusted comment: timestamp:1635442742\tfile:test",
				"0YteLgV960ia80vnA/fHbvkyjl/IoP/HNOCaZfrF0CdhAlp7ok+Tpkya+VpWPX5C/Is3q8a/kEDSY7fBmmgJCg==",
			),
			alg:     algPure,
			comment: "timestamp:1635442742\tfile:test",
		},
		{
			// 与上一条同一把钥匙、同一段消息，只是走预哈希模式：
			// 签名覆盖的是 BLAKE2b-512("test") 而不是 "test" 本身。
			name:    "go-minisign/TestPrehashed",
			pub:     "RWQf6LRCGA9i53mlYecO4IzT51TGPpvWucNSCh1CBM0QTaLn73Y7GFO3",
			message: []byte("test"),
			sig: joinSig(
				"untrusted comment: signature from minisign secret key",
				"RUQf6LRCGA9i559r3g7V1qNyJDApGip8MfqcadIgT9CuhV3EMhHoN1mGTkUidF/z7SrlQgXdy8ofjb7bNJJylDOocrCo8KLzZwo=",
				"trusted comment: timestamp:1635443258\tfile:test\thashed",
				"/cj37GK60vryibFn+ftOgbCvW9NKhKYgjVpFFQUcWPAnjO23wrvVDTt7cloNC06maoBli9q6qwZDXXoaxweICQ==",
			),
			alg:     algPrehashed,
			comment: "timestamp:1635443258\tfile:test\thashed",
		},
		{
			// 这一条给的是完整的两行 .pub 文件；消息带结尾换行，别丢。
			name: "aead/minisign testdata",
			pub: "untrusted comment: minisign public key C373193807678450\n" +
				"RWRQhGcHOBlzw4CoKyugkk4ioDfoxlXxC9LBx+VNhJ3w9w+cAxgvPsuo\n",
			message: []byte("Hello World!\n"),
			sig: joinSig(
				"untrusted comment: signature from minisign secret key",
				"RWRQhGcHOBlzwxrJCyuC+rJfHSfyRKRxkuwa3JJ0bWEs7RHjL1OUmqnTr+V1B9JzFuJIH/ybR2Eus9oEZKt9RbitpF/L4D3+5wg=",
				"trusted comment: timestamp:1614549543\tfile:message.txt",
				"P/722+ynQ+tIy0qadFHwLx5MsyNz/jDKJkDWQj4dDD2OKnVte8m/M14mwPE/1NMwzShPMSBhMXqZGdbe+UZjDg==",
			),
			alg:     algPure,
			comment: "timestamp:1614549543\tfile:message.txt",
		},
		{
			// trusted comment 里没有制表符，只有 timestamp 一段。
			name:    "aead/minisign ExampleVerify",
			pub:     "RWQGPaMY2ls0CkF/83ls7D+IU25w3jeYczwo3s451zDlnrJJwOdt2ro8",
			message: []byte("Hello Gopher!"),
			sig: joinSig(
				"untrusted comment: signature from private key: A345BDA18A33D06",
				"RWQGPaMY2ls0CmMflCAP5J/MpaXmt+3+UoT1vRSPRjXO6w0KNtpkcQe3TxQ35kAwhjFVB6CEYYrHZmMvWjXRutefRHicRUiAJwQ=",
				"trusted comment: timestamp:1600100266",
				"2x/lxCqL+PHoT4I9Wc8PHmoNBtohgmFdWwPBON55Y2P0ttpBHgr4OFldr/Hq7nDcBGt5SBs2XjtMnxjVs6byBg==",
			),
			alg:     algPure,
			comment: "timestamp:1600100266",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pk, err := ParsePublicKey(tc.pub)
			require.NoError(t, err)

			require.NoError(t, Verify(pk, tc.message, tc.sig), "已知向量必须验得过")

			parsed, err := parseSignature(tc.sig)
			require.NoError(t, err)
			assert.Equal(t, tc.alg, parsed.alg, "算法字段要和向量声明的一致")

			got, err := TrustedComment(tc.sig)
			require.NoError(t, err)
			assert.Equal(t, tc.comment, got)

			// 同一组向量走一遍反向断言：消息改一个字节就必须失败。
			tampered := append([]byte(nil), tc.message...)
			if len(tampered) == 0 {
				tampered = []byte("x")
			} else {
				tampered[0] ^= 0x01
			}
			assert.ErrorIs(t, Verify(pk, tampered, tc.sig), ErrSignatureMismatch)
		})
	}
}

// TestKnownVectorRejectsCommentTampering 用真 minisign 产出的签名再验一次双签名：
// 只改 trusted comment、文件内容一字未动，必须被第二段全局签名逮住。
func TestKnownVectorRejectsCommentTampering(t *testing.T) {
	pk, err := ParsePublicKey("RWQf6LRCGA9i53mlYecO4IzT51TGPpvWucNSCh1CBM0QTaLn73Y7GFO3")
	require.NoError(t, err)

	const sigLine = "RWQf6LRCGA9i59SLOFxz6NxvASXDJeRtuZykwQepbDEGt87ig1BNpWaVWuNrm73YiIiJbq71Wi+dP9eKL8OC351vwIasSSbXxwA="
	const globalLine = "0YteLgV960ia80vnA/fHbvkyjl/IoP/HNOCaZfrF0CdhAlp7ok+Tpkya+VpWPX5C/Is3q8a/kEDSY7fBmmgJCg=="

	tampered := joinSig(
		"untrusted comment: signature from minisign secret key",
		sigLine,
		"trusted comment: timestamp:1635442742\tfile:evil", // 只改了这一行
		globalLine,
	)
	assert.ErrorIs(t, Verify(pk, []byte("test"), tampered), ErrGlobalSignature)

	// untrusted comment 反过来：随便改，照样验得过（它不被任何签名覆盖）。
	untouched := joinSig(
		"untrusted comment: rewritten by anyone at all",
		sigLine,
		"trusted comment: timestamp:1635442742\tfile:test",
		globalLine,
	)
	assert.NoError(t, Verify(pk, []byte("test"), untouched))
}
