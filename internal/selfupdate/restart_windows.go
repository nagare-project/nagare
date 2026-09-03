//go:build windows

package selfupdate

import (
	"os/exec"
	"path/filepath"
	"syscall"

	errs "github.com/nagare-project/nagare/internal/errors"
)

// Windows 没有 execve，只能另起一个进程再让当前进程退出。这几个创建标志
// stdlib 的 syscall 包没有导出（只有 x/sys/windows 有），按 Win32 头文件的值写死。
const (
	// detachedProcess：不继承父进程的控制台。少了它，从终端里启动的 nagare
	// 更新之后会留下一个绑在已退出终端上的孤儿进程。
	detachedProcess = 0x00000008
	// createNewProcessGroup：新进程不在父进程的 Ctrl+C 进程组里，
	// 父进程退出/被中断不会连坐把刚起来的新版本一起干掉。
	createNewProcessGroup = 0x00000200
)

// restartProcess 起一个脱离当前控制台的新进程，然后返回 —— 调用方必须紧接着退出，
// 否则端口会被自己占着，新进程只能退而求其次换端口监听。
func restartProcess(exePath string, argv []string, env []string) error {
	const op = "selfupdate.restart"
	cmd := exec.Command(exePath, argv[1:]...)
	cmd.Env = env
	cmd.Dir = filepath.Dir(exePath)
	// 不接管道：父进程马上就退出了，留着句柄只会让新进程写到一个已经没人读的地方。
	cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, nil, nil
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: detachedProcess | createNewProcessGroup,
	}
	if err := cmd.Start(); err != nil {
		return errs.Wrap(errs.CategoryInternal, op,
			"新版本已装好，但重启失败", "请手动退出后重新打开 nagare", err)
	}
	// Release 之后不再等这个子进程；父进程退出时它继续活着。
	if err := cmd.Process.Release(); err != nil {
		return errs.Wrap(errs.CategoryInternal, op,
			"新版本已启动，但与它脱离关联时出错", "如果界面没有恢复，请手动重新打开 nagare", err)
	}
	return nil
}
