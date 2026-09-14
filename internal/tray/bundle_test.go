package tray

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsBundlePath(t *testing.T) {
	assert.True(t, isBundlePath("/Applications/Nagare.app/Contents/MacOS/nagare"))
	assert.True(t, isBundlePath("/Users/a/Downloads/Nagare.app/Contents/MacOS/nagare-source-arm64"))
	assert.False(t, isBundlePath("/opt/homebrew/bin/nagare"), "Homebrew 裸二进制")
	assert.False(t, isBundlePath("/Users/a/nagare.app-notes/nagare"), "只是路径里带 .app 字样")
	assert.False(t, isBundlePath(""))
}
