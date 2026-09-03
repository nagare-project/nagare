//go:build !windows

package selfupdate

import (
	"syscall"

	errs "github.com/nagare-project/nagare/internal/errors"
)

// restartProcess 用新版本【替换掉当前进程本身】（execve），正常情况下不返回。
//
// 为什么 macOS 的 app bundle 也走这条路，而不是 `open -a Nagare.app`：
// open 走 LaunchServices，而 LaunchServices 认 bundle identifier —— 此刻这个
// identifier 对应的进程（就是我们自己）还活着，open 只会把已在运行的那个实例
// 激活一下，根本不会启动新的；随后我们退出，结果是一个都不剩。
// exec 没有这个问题：PID 与 argv 都保留，Go 建的 socket 都带 CLOEXEC 会自动关闭，
// 端口立刻释放；新映像的路径仍在 .app 里，NSBundle 照样能找到 Info.plist，
// LSUIElement（菜单栏应用、不占 Dock）继续生效。
func restartProcess(exePath string, argv []string, env []string) error {
	if err := syscall.Exec(exePath, argv, env); err != nil {
		return errs.Wrap(errs.CategoryInternal, "selfupdate.restart",
			"新版本已装好，但重启失败", "请手动退出后重新打开 nagare", err)
	}
	return nil
}
