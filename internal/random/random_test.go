package random

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// Hex 的契约：长度 = 2n、只含小写十六进制字符、两次调用不同。
// 鉴权 token 与流端点能力 URL 都建在这个函数上，契约必须钉死。
func TestHexContract(t *testing.T) {
	for _, n := range []int{1, 16, 32} {
		got := Hex(n)
		assert.Len(t, got, 2*n)
		assert.Regexp(t, `^[0-9a-f]+$`, got)
	}
	assert.NotEqual(t, Hex(16), Hex(16), "两次生成不应相同")
}

// 非正的字节数是编程错误，必须立刻暴露。
func TestHexRejectsNonPositive(t *testing.T) {
	assert.Panics(t, func() { Hex(0) })
	assert.Panics(t, func() { Hex(-1) })
}
