//go:build windows

package mpv

import (
	"os/exec"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/windows"
)

// 起来的子进程（真机上是 mpv）进了 KILL_ON_JOB_CLOSE 的作业：nagare 被硬杀时系统会把它一起结束。
func TestChildJoinsKillOnCloseJob(t *testing.T) {
	cmd := exec.Command("cmd.exe", "/c", "ping -n 30 127.0.0.1 >nul")
	require.NoError(t, cmd.Start())
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	})
	require.NoError(t, assignToJob(cmd.Process.Pid))

	j, err := killOnExitJob()
	require.NoError(t, err)
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(cmd.Process.Pid))
	require.NoError(t, err)
	defer windows.CloseHandle(h)
	var in int32
	r, _, callErr := windows.NewLazySystemDLL("kernel32.dll").NewProc("IsProcessInJob").Call(
		uintptr(h), uintptr(j), uintptr(unsafe.Pointer(&in)))
	require.NotZero(t, r, "IsProcessInJob：%v", callErr)
	assert.NotZero(t, in, "子进程不在作业里")

	var info windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION
	require.NoError(t, windows.QueryInformationJobObject(j, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info)), nil))
	assert.NotZero(t, info.BasicLimitInformation.LimitFlags&windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE)
}
