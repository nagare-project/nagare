//go:build windows

package mpv

import (
	"bufio"
	"strings"
	"testing"

	winio "github.com/Microsoft/go-winio"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 管道名形状：固定前缀 + pid + 随机段，两次生成不相同。
func TestIPCEndpointIsUniqueNamedPipe(t *testing.T) {
	a, err := ipcEndpoint("C:\\ignored")
	require.NoError(t, err)
	b, err := ipcEndpoint("")
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(a, pipePrefix), a)
	assert.NotEqual(t, a, b, "随机段必须让两次生成不同名")
}

// 真实拨号：用 go-winio 当管道服务端，dialIPC 连上后按 mpv IPC 的「一行一条 JSON」往返一次。
func TestDialIPCRoundTrip(t *testing.T) {
	name, err := ipcEndpoint("")
	require.NoError(t, err)
	ln, err := winio.ListenPipe(name, nil)
	require.NoError(t, err)
	defer ln.Close()

	served := make(chan error, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			served <- err
			return
		}
		defer conn.Close()
		line, err := bufio.NewReader(conn).ReadString('\n')
		if err != nil {
			served <- err
			return
		}
		_, err = conn.Write([]byte(`{"event":"echo","data":` + strings.TrimSpace(line) + "}\n"))
		served <- err
	}()

	conn, err := dialIPC(name)
	require.NoError(t, err)
	defer conn.Close()
	_, err = conn.Write([]byte(`{"command":["get_property","pause"]}` + "\n"))
	require.NoError(t, err)
	reply, err := bufio.NewReader(conn).ReadString('\n')
	require.NoError(t, err)
	assert.Contains(t, reply, `"event":"echo"`)
	require.NoError(t, <-served)
}

// 管道还没被 mpv 建出来时要快速失败，交给上层轮询，而不是挂住启动。
func TestDialIPCFailsFastWhenPipeAbsent(t *testing.T) {
	name, err := ipcEndpoint("")
	require.NoError(t, err)
	_, err = dialIPC(name)
	require.Error(t, err)
}
