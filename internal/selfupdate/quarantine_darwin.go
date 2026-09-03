//go:build darwin

package selfupdate

import (
	"context"
	"log"
	"os"
	"os/exec"
	"time"
)

// xattrTool 是系统自带的扩展属性工具。
//
// 为什么不是 syscall.Removexattr：Go 的 syscall 包在 darwin 上根本没有这个函数
// （xattr 系列只在 linux 上导出）。golang.org/x/sys/unix 有，但它在本仓库是间接依赖，
// 为一个降级用的收尾动作把它提成直接依赖不划算。
// /usr/bin/xattr 在 macOS 13+ 是原生 Mach-O（早年那个 Python 脚本版本已经不在了），
// 不会因为没装命令行工具而弹「安装开发者工具」的对话框。
const xattrTool = "/usr/bin/xattr"

// quarantineTimeout 是清理动作的时长上限：它是可有可无的收尾，不能拖住更新。
const quarantineTimeout = 30 * time.Second

// clearQuarantine 递归清掉 com.apple.quarantine。
//
// 多数情况下它是空操作：quarantine 由【下载方】主动打上（浏览器等走 LaunchServices 的
// 程序会打），Go 的 net/http 不打；而且 bundle 是我们用 archive/zip 一个字节一个字节
// 写出来的，扩展属性从来没被还原过。留着这一步是纵深防御 —— 将来若改用 ditto 解包
// （它会还原扩展属性），少了这一步用户双击就会被 Gatekeeper 拦下，而那要等发版之后
// 才会被发现。
//
// 失败一律降级为一行日志：清不掉最坏是弹一次「无法打开」，让整次更新失败要糟得多。
func clearQuarantine(path string) {
	if _, err := os.Stat(xattrTool); err != nil {
		log.Printf("selfupdate: 系统里没有 xattr，跳过清理 quarantine 标记")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), quarantineTimeout)
	defer cancel()
	// -d 只删指定的那一个属性，不用 -c 全清：bundle 上别的扩展属性可能是
	// 代码签名的一部分，不该连坐。整棵树都没有这个属性时 xattr 会非零退出，
	// 属于预期情况。
	cmd := exec.CommandContext(ctx, xattrTool, "-r", "-d", "com.apple.quarantine", path)
	if out, err := cmd.CombinedOutput(); err != nil {
		log.Printf("selfupdate: 清理 quarantine 标记未完成（通常说明本来就没有）：%v %s",
			err, trunc(string(out), 120))
	}
}
