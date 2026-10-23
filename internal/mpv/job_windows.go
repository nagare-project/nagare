//go:build windows

package mpv

import (
	"fmt"
	"log"
	"os"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Windows 上 mpv（以及 thumbfast 另起的那个缩略图用的 mpv.exe，它是 mpv 的子进程，自动跟着进同一个作业）
// 放进一个「作业对象」，设 KILL_ON_JOB_CLOSE：nagare 不管怎么结束 —— 正常退出、被任务管理器结束、崩溃 —— 作业的
// 最后一个句柄随进程关掉，系统就把里面的进程全部结束。否则 nagare 被硬杀之后 mpv 会留在桌面上接着播
// （桌面窗口里还会是一个没有父窗口的孤儿窗口），也没有人回写进度。
//
// 作业句柄整个进程只建一个、永远不关（关它就是结束 mpv）。建不起来只记日志：mpv 照常播，只是没有这层保险。

var (
	jobOnce sync.Once
	job     windows.Handle
	jobErr  error
)

func killOnExitJob() (windows.Handle, error) {
	jobOnce.Do(func() {
		h, err := windows.CreateJobObject(nil, nil)
		if err != nil {
			jobErr = fmt.Errorf("建不了作业对象：%w", err)
			return
		}
		info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
		info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
		if _, err := windows.SetInformationJobObject(h, windows.JobObjectExtendedLimitInformation,
			uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
			windows.CloseHandle(h)
			jobErr = fmt.Errorf("作业对象设不上 KILL_ON_JOB_CLOSE：%w", err)
			return
		}
		job = h
	})
	return job, jobErr
}

// bindToNagare 把刚起来的 mpv 放进作业（见上）。失败只记日志。
func bindToNagare(p *os.Process) {
	if err := assignToJob(p.Pid); err != nil {
		log.Printf("mpv: 没能把 mpv 绑到 nagare 上（nagare 被强行结束时 mpv 会留下来）：%v", err)
	}
}

func assignToJob(pid int) error {
	j, err := killOnExitJob()
	if err != nil {
		return err
	}
	h, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(pid))
	if err != nil {
		return fmt.Errorf("打不开 mpv 进程：%w", err)
	}
	defer windows.CloseHandle(h)
	if err := windows.AssignProcessToJobObject(j, h); err != nil {
		return fmt.Errorf("放不进作业对象：%w", err)
	}
	return nil
}
