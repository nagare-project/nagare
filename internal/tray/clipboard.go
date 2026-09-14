package tray

import (
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

// copyToClipboard 把文本放进系统剪贴板。三平台都借系统自带 / 桌面标配的命令行工具，
// 不为这一个菜单项引入 cgo 或第三方依赖。Linux 上 Wayland 与 X11 各试一个，
// 都没有就明确报错（用户在设置页仍能看到地址）。
func copyToClipboard(text string) error {
	for _, argv := range clipboardCommands(runtime.GOOS) {
		cmd := exec.Command(argv[0], argv[1:]...)
		cmd.Stdin = strings.NewReader(text)
		hideConsole(cmd)
		if err := cmd.Run(); err == nil {
			return nil
		} else if !errors.Is(err, exec.ErrNotFound) {
			return fmt.Errorf("%s：%w", argv[0], err)
		}
	}
	return errors.New("没有可用的剪贴板工具（Linux 需要 wl-copy / xclip / xsel 之一）")
}

// clipboardCommands 按平台给出候选命令，前面的优先。
func clipboardCommands(goos string) [][]string {
	switch goos {
	case "darwin":
		return [][]string{{"pbcopy"}}
	case "windows":
		return [][]string{{"clip"}}
	default:
		return [][]string{{"wl-copy"}, {"xclip", "-selection", "clipboard"}, {"xsel", "--clipboard", "--input"}}
	}
}
