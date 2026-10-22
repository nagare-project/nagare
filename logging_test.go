package main

import (
	"bytes"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type brokenWriter struct{}

func (brokenWriter) Write([]byte) (int, error) { return 0, errors.New("the handle is invalid") }

// 没有控制台（Windows 双击启动的 GUI 程序）时终端写不了，日志文件照样要写进去。
func TestLogOutputWritesFileEvenWhenTerminalIsBroken(t *testing.T) {
	var file bytes.Buffer
	n, err := logOutput(&file, brokenWriter{}).Write([]byte("nagare 启动\n"))
	require.NoError(t, err)
	assert.Equal(t, len("nagare 启动\n"), n)
	assert.Equal(t, "nagare 启动\n", file.String())
}

// 有终端时两边都有。
func TestLogOutputTeesToTerminal(t *testing.T) {
	var file, term bytes.Buffer
	_, err := logOutput(&file, &term).Write([]byte("x\n"))
	require.NoError(t, err)
	assert.Equal(t, "x\n", file.String())
	assert.Equal(t, "x\n", term.String())
}
