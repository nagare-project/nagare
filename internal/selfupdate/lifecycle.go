package selfupdate

// 更新装好之后的两件收尾：以新版本重启，以及清掉上一次留下的残留。

import (
	"log"
	"os"
	"path/filepath"
	"strings"

	errs "github.com/nagare-project/nagare/internal/errors"
)

// Restart 用新装好的版本替换当前进程。正常情况下不返回。
//
// 调用方须先停掉 HTTP 监听（或确保能接受端口短暂被占）：unix 走 exec 时 Go 创建的
// socket 都带 CLOEXEC 会自动关闭，Windows 是另起进程，端口要靠当前进程尽快退出释放。
func (u *Updater) Restart() error {
	exe, argv, err := u.restartTarget()
	if err != nil {
		return err
	}
	log.Printf("selfupdate: 正在以新版本重启")
	return restartProcess(exe, argv, os.Environ())
}

// restartTarget 组装重启用的可执行文件路径与 argv。
// 替换之后原路径上就是新版本，所以 app bundle 与普通二进制是同一条路径。
func (u *Updater) restartTarget() (string, []string, error) {
	inst, err := u.detect()
	if err != nil {
		return "", nil, errs.Wrap(errs.CategoryInternal, "selfupdate.restart",
			"找不到要重启的可执行文件", "请手动退出后重新打开 nagare", err)
	}
	argv := append([]string{inst.exePath}, os.Args[1:]...)
	return inst.exePath, argv, nil
}

// SweepOldFiles 清掉上次更新留下的残留：<可执行文件>.old、<app>.app.old、
// Windows 的 mpv.old，以及中途失败留下的暂存目录。启动时调一次，失败只记日志
// —— Windows 上旧 exe 可能还被占用（杀毒软件扫描中），下次启动再删就是了。
func (u *Updater) SweepOldFiles() {
	inst, err := u.detect()
	if err != nil {
		return
	}
	// 包管理器管着的目录（/usr/bin 之类）里不该有我们的残留，也绝不在那里删东西。
	if inst.channel == ChannelPackage || inst.channel == ChannelUnknown {
		return
	}
	for _, path := range u.sweepTargets(inst) {
		if err := os.RemoveAll(path); err != nil {
			log.Printf("selfupdate: 清理旧版本残留失败（下次启动会重试）：%v", err)
		}
	}
}

// sweepTargets 列出要清理的路径。用 os.ReadDir 逐个比前缀而不是 filepath.Glob：
// 安装目录名里的 [ 或 * 会被 Glob 当成模式，静默漏掉该清的东西。
func (u *Updater) sweepTargets(inst install) []string {
	targets := []string{inst.target + oldSuffix}
	if inst.channel == ChannelDirect && u.env.goos == "windows" {
		targets = append(targets, filepath.Join(inst.dir, "mpv"+oldSuffix))
	}
	entries, err := os.ReadDir(inst.dir)
	if err != nil {
		return targets
	}
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), stagePrefix) {
			targets = append(targets, filepath.Join(inst.dir, e.Name()))
		}
		// 探测文件本该建完立刻就删，只有进程恰好死在那两步之间才会剩下。
		if !e.IsDir() && strings.HasPrefix(e.Name(), probePrefix) {
			targets = append(targets, filepath.Join(inst.dir, e.Name()))
		}
	}
	return targets
}
