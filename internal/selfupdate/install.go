package selfupdate

// 替换是整条链里唯一不可撤销的一步，所以规矩只有两条：
//   1. 走到这里时，要装的东西已经全部验过、解好、就在同一个卷上 —— 剩下的只有 rename。
//   2. 每一步都留一个 restore，任何一步失败就把前面的都撤回去，绝不留下半个装好的版本。

import (
	"errors"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"

	errs "github.com/nagare-project/nagare/internal/errors"
)

// mpvDirName 是 Windows 便携版/安装包里内置 mpv 的目录名（与 internal/mpv 的探测约定一致）。
const mpvDirName = "mpv"

func (u *Updater) install(inst install, unpacked string) error {
	const op = "selfupdate.install"
	switch inst.channel {
	case ChannelAppBundle:
		return installAppBundle(inst, unpacked)
	case ChannelDirect:
		return installDirect(inst, unpacked, u.env.goos)
	case ChannelPackage:
		return errs.New(errs.CategoryInput, op, "nagare 由系统包管理器安装，不能自动更新",
			"请用 apt / dnf / brew 等原渠道升级")
	default:
		return errs.New(errs.CategoryInput, op, "无法确定 nagare 的安装方式，已中止更新",
			"请到项目发布页手动下载新版本覆盖安装")
	}
}

// installDirect 就地换掉可执行文件（Windows 安装包/便携版、Linux tar.gz）。
// Windows 允许把正在运行的 exe 改名（不允许删），备份出来的 .old 留到下次启动清理。
func installDirect(inst install, unpacked, goos string) error {
	const op = "selfupdate.install"
	newBin, err := findFile(unpacked, binaryName(goos))
	if err != nil {
		return err
	}

	var undo []func()
	rollback := func() {
		for i := len(undo) - 1; i >= 0; i-- {
			undo[i]()
		}
	}

	// 内置 mpv 先换：它与二进制是一起发的，版本要配套。放在前面是为了让回滚
	// 只有一层 —— 二进制那步失败时把 mpv 也撤回去，用户回到完全一致的旧状态。
	stagedMPV := filepath.Join(unpacked, mpvDirName)
	if isDir(stagedMPV) {
		restore, err := swap(stagedMPV, filepath.Join(inst.dir, mpvDirName))
		if err != nil {
			return err
		}
		undo = append(undo, restore)
	}

	restore, err := swap(newBin, inst.target)
	if err != nil {
		rollback()
		return err
	}
	undo = append(undo, restore)

	// 解包时已经按可执行位归一化过，这里再钉一次：装上去的东西必须能跑，
	// 不能因为归档里权限位丢了就留一个启动不了的 nagare。
	if err := os.Chmod(inst.target, execMode); err != nil {
		rollback()
		return errs.Wrap(errs.CategoryStorage, op, "设置新版本可执行权限失败，已回滚",
			"请到项目发布页手动下载新版本", err)
	}
	return nil
}

// installAppBundle 整包替换 macOS 的 .app。
//
// ⚠️ 不要只替换 .app 里的那个可执行文件：bundle 是 ad-hoc 签名的，
// 签名覆盖 Contents 下的全部内容（_CodeSignature/CodeResources 里记着每个资源的哈希）。
// 换掉其中任何一个文件都会让签名失效，macOS 会直接拒绝启动 —— 而且失败发生在
// 用户双击的那一刻，自更新这边看不到任何错误。所以更新包是整个 .app 的 zip，
// 这里也只做整个目录的 rename。
func installAppBundle(inst install, unpacked string) error {
	app, err := findAppBundle(unpacked)
	if err != nil {
		return err
	}
	if err := verifyBundle(app); err != nil {
		return err
	}
	// 在搬过去【之前】清 quarantine：安装位置上永远不该出现一个带 quarantine 的 bundle。
	clearQuarantine(app)
	if _, err := swap(app, inst.target); err != nil {
		return err
	}
	return nil
}

// swap 把 staged 移到 live，原来的 live 备份成 live+".old"。
// 返回的 restore 撤销这一步，供上层做整体回滚。
//
// 全靠同卷 os.Rename：它是原子的，不存在「拷到一半断电」这种中间态。
// 暂存目录开在安装目录旁边就是为了保证同卷（见 Apply）。
func swap(staged, live string) (restore func(), err error) {
	const op = "selfupdate.install"
	backup := live + oldSuffix
	// Windows 的 rename 不允许覆盖已存在的目标，先清掉上次留下的备份。
	if err := os.RemoveAll(backup); err != nil {
		return nil, errs.Wrap(errs.CategoryStorage, op, "清理上一次的备份失败",
			"请重启 nagare 后重试更新", err)
	}
	hadLive := true
	if err := os.Rename(live, backup); err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			return nil, errs.Wrap(errs.CategoryStorage, op, "备份当前版本失败，更新已中止",
				"请确认安装目录可写，或到项目发布页手动下载", err)
		}
		hadLive = false
	}
	if err := os.Rename(staged, live); err != nil {
		if hadLive {
			if rerr := os.Rename(backup, live); rerr != nil {
				// 这是最坏的一格：备份改不回去，安装位置上什么都没有了。
				log.Printf("selfupdate: 回滚失败，旧版本仍在 %s%s，请手动改回原名：%v", live, oldSuffix, rerr)
			}
		}
		return nil, errs.Wrap(errs.CategoryStorage, op, "安装新版本失败，已回滚",
			"请确认安装目录可写，或到项目发布页手动下载", err)
	}
	return func() {
		if err := os.RemoveAll(live); err != nil {
			log.Printf("selfupdate: 回滚时移除新版本失败：%v", err)
			return
		}
		if hadLive {
			if err := os.Rename(backup, live); err != nil {
				log.Printf("selfupdate: 回滚失败，旧版本仍在 %s%s，请手动改回原名：%v", live, oldSuffix, err)
			}
		}
	}, nil
}

// binaryName 是归档里可执行文件的名字（与 .goreleaser.yaml 的 builds.binary 一致）。
func binaryName(goos string) string {
	if goos == "windows" {
		return "nagare.exe"
	}
	return "nagare"
}

// findFile 在解包目录里找 name。goreleaser 的归档把二进制放在根，
// 但仍然往下找一层以上 —— 归档布局是发布侧的细节，不值得为它卡死更新。
func findFile(root, name string) (string, error) {
	if direct := filepath.Join(root, name); isRegular(direct) {
		return direct, nil
	}
	found := ""
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || found != "" {
			return nil //nolint:nilerr // 遍历失败不该让整次更新失败，下面按「没找到」报错
		}
		if !d.IsDir() && d.Name() == name {
			found = p
			return fs.SkipAll
		}
		return nil
	})
	if found == "" {
		return "", errs.Wrap(errs.CategoryUpstream, "selfupdate.install",
			"更新包里没有 nagare 可执行文件，已中止更新",
			"请到项目发布页手动下载", errors.New("缺少 "+name))
	}
	return found, nil
}

// findAppBundle 在解包目录的根上找唯一的 .app。
// ditto --keepParent 压出来的 zip 顶层就是 Nagare.app/（__MACOSX/ 已在解包时跳过）。
func findAppBundle(root string) (string, error) {
	const op = "selfupdate.install"
	entries, err := os.ReadDir(root)
	if err != nil {
		return "", errs.Wrap(errs.CategoryStorage, op, "读取解包目录失败", "请重试更新", err)
	}
	var apps []string
	for _, e := range entries {
		if e.IsDir() && strings.HasSuffix(e.Name(), ".app") {
			apps = append(apps, filepath.Join(root, e.Name()))
		}
	}
	if len(apps) != 1 {
		return "", errs.Wrap(errs.CategoryUpstream, op,
			"更新包里不是一个完整的 Nagare.app，已中止更新", "请到项目发布页手动下载",
			errors.New("解包后根目录下的 .app 数量为 "+itoa(len(apps))))
	}
	return apps[0], nil
}

// verifyBundle 检查 bundle 的最小完整性。少了这两样，装上去就是一个双击没反应的图标
// —— 而那时旧版本已经被改名了，用户手上什么都不剩。
func verifyBundle(app string) error {
	// Contents/MacOS/nagare 这个名字来自 scripts/macos/Info.plist.tmpl 的 CFBundleExecutable，
	// 与 .goreleaser.yaml 的 builds.binary 一致；改名要同时改这三处。
	required := []string{
		filepath.Join(app, "Contents", "Info.plist"),
		filepath.Join(app, "Contents", "MacOS", "nagare"),
	}
	for _, p := range required {
		if !isRegular(p) {
			return errs.Wrap(errs.CategoryUpstream, "selfupdate.install",
				"更新包里的 Nagare.app 不完整，已中止更新", "请到项目发布页手动下载",
				errors.New("缺少 "+filepath.Base(p)))
		}
	}
	return nil
}

func isRegular(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.Mode().IsRegular()
}

func isDir(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.IsDir()
}

// itoa 避免为一行数字引 strconv。
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
