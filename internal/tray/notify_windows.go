//go:build windows

package tray

import (
	"errors"
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Windows 平台钩子：托盘本身由 systray 提供，这里只补「首次启动气泡」。
//
// Windows 10/11 默认把新程序的托盘图标折进任务栏的「^」溢出区，用户根本看不到；
// 气泡通知是系统给托盘程序留的标准提示手段（Shell_NotifyIcon NIF_INFO），会以
// 系统通知的形式弹出并进入通知中心。fyne systray 没有暴露气泡接口，所以这里
// 直接对它已经注册的那个图标（窗口类 SystrayClass、ID 100）发一次 NIM_MODIFY。

func platformReady(Options) (<-chan struct{}, func()) { return nil, func() {} }

// Available 在 Windows 总是可用。
func Available() bool { return true }

// CurrentMode 见 Mode。
func CurrentMode() Mode { return ModeTray }

var (
	shell32           = windows.NewLazySystemDLL("shell32.dll")
	procShellNotify   = shell32.NewProc("Shell_NotifyIconW")
	user32            = windows.NewLazySystemDLL("user32.dll")
	procEnumWindows   = user32.NewProc("EnumWindows")
	procGetClassNameW = user32.NewProc("GetClassNameW")
)

// 与 systray 内部的 notifyIconData / 注册参数保持一致（fyne.io/systray v1.12.2 systray_windows.go）。
const (
	systrayWindowClass = "SystrayClass"
	systrayIconID      = 100
	nimModify          = 0x00000001
	nifInfo            = 0x00000010
	niifInfo           = 0x00000001
)

// notifyIconData 是 NOTIFYICONDATAW 的 Go 镜像；字段顺序与大小必须与 Win32 定义逐字节相同。
type notifyIconData struct {
	Size                       uint32
	Wnd                        windows.Handle
	ID, Flags, CallbackMessage uint32
	Icon                       windows.Handle
	Tip                        [128]uint16
	State, StateMask           uint32
	Info                       [256]uint16
	Timeout, Version           uint32
	InfoTitle                  [64]uint16
	InfoFlags                  uint32
	GuidItem                   windows.GUID
	BalloonIcon                windows.Handle
}

// showNotice 在 systray 的托盘图标上弹一条气泡。
func showNotice(n Notice) error {
	hwnd, err := findSystrayWindow()
	if err != nil {
		return err
	}
	nid := notifyIconData{
		Wnd:       hwnd,
		ID:        systrayIconID,
		Flags:     nifInfo,
		InfoFlags: niifInfo,
	}
	nid.Size = uint32(unsafe.Sizeof(nid))
	copyUTF16(nid.InfoTitle[:], n.Title)
	copyUTF16(nid.Info[:], n.Body)
	res, _, callErr := procShellNotify.Call(nimModify, uintptr(unsafe.Pointer(&nid)))
	if res == 0 {
		return fmt.Errorf("Shell_NotifyIcon(NIM_MODIFY)：%w", callErr)
	}
	return nil
}

// findSystrayWindow 找本进程里 systray 建的那个隐藏窗口。
// 窗口类名是进程内注册的，但 FindWindow 按名字全局匹配，别的进程用同一个库也叫
// SystrayClass；所以枚举顶层窗口并核对进程号，只认自己的。
func findSystrayWindow() (windows.Handle, error) {
	var found windows.Handle
	self := windows.GetCurrentProcessId()
	cb := windows.NewCallback(func(hwnd windows.Handle, _ uintptr) uintptr {
		var pid uint32
		windows.GetWindowThreadProcessId(windows.HWND(hwnd), &pid)
		if pid != self {
			return 1 // 继续
		}
		var buf [64]uint16
		n, _, _ := procGetClassNameW.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
		if n == 0 || windows.UTF16ToString(buf[:n]) != systrayWindowClass {
			return 1
		}
		found = hwnd
		return 0 // 停止
	})
	procEnumWindows.Call(cb, 0)
	if found == 0 {
		return 0, errors.New("没找到 systray 的托盘窗口（托盘尚未就绪或库内部改了窗口类名）")
	}
	return found, nil
}

// copyUTF16 把 s 写进定长的 UTF-16 缓冲，超长截断并保证以 NUL 结尾。
func copyUTF16(dst []uint16, s string) {
	src, err := windows.UTF16FromString(s)
	if err != nil {
		return
	}
	n := copy(dst, src)
	if n == len(dst) {
		dst[len(dst)-1] = 0
	}
}
