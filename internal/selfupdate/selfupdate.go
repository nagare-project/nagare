// Package selfupdate 实现「下载新版本 → 验签 → 校验哈希 → 解包 → 原子替换 → 重启」这条链。
//
// 零证书发布（D1，A6 作废）意味着安装包本身没有任何证书背书，更新的完整性【全部】
// 压在 minisign 签名上。信任链只有一条，中间没有任何可以绕过的环节：
//
//	checksums.txt.minisig --(ed25519，内嵌公钥)--> checksums.txt --(sha256)--> 归档文件
//
// 一份签名覆盖一次发布的全部产物（dmg / setup.exe / tar.gz / zip / .app.zip 都在清单里）。
// 内嵌公钥为空（构建时没配）时 Apply 一律拒绝 —— 失败关闭：宁可不能自更新，
// 也不能装一个没法验证来源的二进制。
//
// 与 internal/update 的分工：那边只查「有没有新版本」并提示，从不落盘任何可执行内容；
// 这边负责真的装上去。两者互不依赖。
//
// 下载前缀可用环境变量 NAGARE_UPDATE_BASE_URL 覆盖（必须是 https）。这不是调试便利，
// 是分发风险的缓解措施：同类开源播放器的 GitHub Release 资产被投诉下架是真实发生过的事，
// 届时需要能立刻指向备用分发源，而不是等一个新版本。
//
// 约定：本包不把任何 URL 写进日志或错误信息（GET 的 query 可携带凭证，仓库红线）。
package selfupdate

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"

	errs "github.com/nagare-project/nagare/internal/errors"
	"golang.org/x/mod/semver"
)

const (
	// DefaultRepo 是官方仓库（owner/name），与 internal/update 保持一致。
	DefaultRepo = "nagare-project/nagare"
	// EnvBaseURL 覆盖发布资产的下载前缀（必须 https）。
	EnvBaseURL = "NAGARE_UPDATE_BASE_URL"

	// oldSuffix 是被替换掉的旧版本的备份后缀。Windows 允许把正在运行的 exe 改名，
	// 但不允许删除它 —— 所以旧文件只能留到下次启动由 SweepOldFiles 清理。
	oldSuffix = ".old"
	// probePrefix 是可写性探测文件的前缀（正常情况下建完立刻就删，
	// 只有进程恰好在这两步之间被杀才会留下）。
	probePrefix = ".nagare-write-probe-"
	// writableTTL 是「安装目录可写」这个结论的缓存时长。
	writableTTL = 30 * time.Second
	// stagePrefix 是暂存目录名的前缀。故意以点开头：Finder 与 ls 默认不显示，
	// 万一进程被杀留下残留也不会吓到用户；SweepOldFiles 按这个前缀清理。
	stagePrefix = ".nagare-update-"
)

// repoRE 与 internal/update 同一套校验：仓库名要拼进 URL，不能含路径分隔以外的花样。
var repoRE = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)

// versionRE 是可接受的版本号（不带 v 前缀）。版本号会被拼进下载路径，
// 这里同时承担「挡路径穿越」的职责 —— 只有这一个形状能通过。
var versionRE = regexp.MustCompile(`^\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?$`)

// Options 是构造更新器的输入。
type Options struct {
	CurrentVersion string       // 形如 "0.2.0"，或 "0.1.0-dev"
	Repo           string       // "nagare-project/nagare"；空取 DefaultRepo
	PublicKey      string       // 内嵌的 minisign 公钥；空串 = 未配置，自更新一律拒绝
	HTTPClient     *http.Client // 空用默认；注入的客户端会被复制并套上重定向策略
	BaseURL        string       // 空则取 EnvBaseURL，再空取按 Repo 推出的 GitHub 地址
}

// Updater 是更新器。Apply 由内部信号量串行化，其余方法只读，可并发调用。
type Updater struct {
	current string
	repo    string
	baseURL string
	pub     string
	client  *http.Client
	env     environment

	// applySem 保证同一时刻只有一次 Apply 在跑。用容量 1 的 channel 而不是 Mutex：
	// 第二个调用方要的是「立刻知道有更新在跑」，不是排队等十分钟。
	applySem chan struct{}

	// 可写性探测的结果缓存，见 probeWritableCached。
	probeMu  sync.Mutex
	probeDir string
	probeAt  time.Time
}

// New 构造更新器。BaseURL / 环境变量不合法（非 https）时在这里就报错，
// 不留到用户点「更新」的那一刻。
func New(opts Options) (*Updater, error) {
	const op = "selfupdate.new"
	repo := strings.TrimSpace(opts.Repo)
	if repo == "" {
		repo = DefaultRepo
	}
	if !repoRE.MatchString(repo) {
		return nil, errs.New(errs.CategoryInput, op, "更新仓库名不合法（应为 owner/name）", "")
	}
	base, err := resolveBaseURL(opts.BaseURL, repo)
	if err != nil {
		return nil, err
	}
	return &Updater{
		current:  strings.TrimSpace(opts.CurrentVersion),
		repo:     repo,
		baseURL:  base,
		pub:      strings.TrimSpace(opts.PublicKey),
		client:   newHTTPClient(opts.HTTPClient),
		env:      defaultEnvironment(),
		applySem: make(chan struct{}, 1),
	}, nil
}

// Capability 是「这个安装能不能自更新」的判定结果，供界面显示。
type Capability struct {
	Supported bool    `json:"supported"`
	Channel   Channel `json:"channel"`
	// Reason 在 Supported 为 false 时给出中文原因与用户该做什么。
	Reason string `json:"reason,omitempty"`
	// Target 是将被替换的东西（二进制路径或 .app 路径），给界面显示。
	Target string `json:"target"`
}

// Capability 判定当前安装能否自更新。故意做成「每次现算」而不是构造时算一次：
// 用户完全可能在运行期间把 nagare 从下载目录拖进 /Applications。
//
// 可写性是真的往安装目录建一个临时文件再删掉试出来的 —— 只看 os.Stat 的权限位
// 判断不了只读挂载、SIP、ACL、Windows 的继承权限，而这些恰恰是最常见的失败原因。
// 与其在替换到一半时失败，不如在按钮变灰之前就知道。这一项的成功结论会缓存
// writableTTL（见 probeWritableCached），免得界面每拉一次状态就建删一个文件。
func (u *Updater) Capability() Capability {
	inst, err := u.detect()
	if err != nil {
		return Capability{Channel: ChannelUnknown,
			Reason: "无法确定 nagare 的安装位置，请到项目发布页手动下载新版本覆盖安装"}
	}
	c := Capability{Channel: inst.channel, Target: inst.target}
	switch {
	case u.pub == "":
		// 发布流水线没配签名密钥时构建出来的二进制会走到这里。能跑，但不能自更新。
		c.Reason = "这个构建没有内嵌更新公钥，校验不了更新包的完整性，请到项目发布页手动下载"
	case inst.channel == ChannelPackage:
		c.Reason = "nagare 由系统包管理器安装，请用 apt / dnf / brew 等原渠道升级"
	case inst.channel == ChannelUnknown:
		c.Reason = "无法确定 nagare 的安装方式，请到项目发布页手动下载新版本覆盖安装"
	default:
		if _, err := assetTemplate(inst.channel, u.env.goos, u.env.goarch); err != nil {
			c.Reason = fmt.Sprintf("当前平台（%s/%s）没有发布更新包，请到项目发布页确认",
				u.env.goos, u.env.goarch)
			break
		}
		if err := u.probeWritableCached(inst.dir); err != nil {
			c.Reason = fmt.Sprintf("安装目录 %s 不可写，请把 nagare 装到有写权限的位置，或手动下载新版本覆盖安装", inst.dir)
			break
		}
		c.Supported = true
	}
	return c
}

// Apply 走完整条链并把新版本装好。成功返回时【尚未重启】，由调用方决定何时调 Restart。
// 全程受 ctx 约束；任何一步失败都不会留下半个装好的版本 —— 校验与解包全在暂存目录里
// 完成，只有全部通过之后才动真正的安装位置，且替换本身可回滚。
func (u *Updater) Apply(ctx context.Context, version string) error {
	const op = "selfupdate.apply"
	select {
	case u.applySem <- struct{}{}:
	default:
		return errs.New(errs.CategoryInput, op, "已经有一次更新在进行中", "请等它结束后再试")
	}
	defer func() { <-u.applySem }()

	ver, err := normalizeVersion(version)
	if err != nil {
		return err
	}
	inst, err := u.applyTarget(ver)
	if err != nil {
		return err
	}
	asset, err := assetName(inst.channel, u.env.goos, u.env.goarch, ver)
	if err != nil {
		return err
	}

	// 暂存目录开在【安装目录旁边】而不是系统临时目录：跨文件系统的 os.Rename 会失败
	// （Linux 的 /tmp 常是 tmpfs），而替换要的正是同卷 rename 的原子性。
	// 代价是下载期间安装目录里会多出一个约 120MB 的隐藏目录，用完即删。
	stage, err := os.MkdirTemp(inst.dir, stagePrefix+"*")
	if err != nil {
		return errs.Wrap(errs.CategoryStorage, op, "无法在安装目录创建临时目录",
			"请确认安装目录可写且磁盘空间充足", err)
	}
	defer func() {
		if err := os.RemoveAll(stage); err != nil {
			log.Printf("selfupdate: 清理暂存目录失败（下次启动会重试）：%v", err)
		}
	}()

	sum, err := u.verifiedChecksum(ctx, ver, asset)
	if err != nil {
		return err
	}
	archive := filepath.Join(stage, "archive")
	if err := u.downloadArchive(ctx, u.assetURL(ver, asset), archive, sum); err != nil {
		return err
	}
	unpacked := filepath.Join(stage, "unpacked")
	if err := extractArchive(archive, unpacked, asset); err != nil {
		return err
	}
	// 解包完了才动安装位置。这中间可能过了几分钟，权限可能已经变了，再探一次。
	if err := probeWritable(inst.dir); err != nil {
		return errs.Wrap(errs.CategoryStorage, op, "安装目录已不可写，更新中止",
			"请确认对安装目录有写权限后重试", err)
	}
	return u.install(inst, unpacked)
}

// applyTarget 把 Apply 前置的三道闸（能不能更新 / 装在哪 / 版本是不是往回退）收在一处。
func (u *Updater) applyTarget(ver string) (install, error) {
	const op = "selfupdate.apply"
	if c := u.Capability(); !c.Supported {
		return install{}, errs.New(errs.CategoryInput, op, "当前安装方式不支持自动更新", c.Reason)
	}
	inst, err := u.detect()
	if err != nil {
		return install{}, errs.Wrap(errs.CategoryInternal, op, "无法确定 nagare 的安装位置",
			"请到项目发布页手动下载新版本", err)
	}
	if err := u.guardDowngrade(ver); err != nil {
		return install{}, err
	}
	return inst, nil
}

// guardDowngrade 拒绝装比当前更旧的版本。
//
// 这不是洁癖：攻击者若能左右下载前缀（备用分发源、内网劫持），把一份【签名完全合法】
// 的旧版本喂过来就能把用户降级到一个已知有洞的版本上，签名校验对此毫无察觉。
// 版本号无法比较（本地构建、非 semver）时不拦 —— 那种情况本来就不该走到这里。
func (u *Updater) guardDowngrade(ver string) error {
	cur, next := canon(u.current), canon(ver)
	if !semver.IsValid(cur) || !semver.IsValid(next) {
		return nil
	}
	if semver.Compare(next, cur) < 0 {
		return errs.New(errs.CategoryInput, "selfupdate.apply",
			fmt.Sprintf("%s 比当前版本 %s 更旧，已拒绝安装", ver, u.current),
			"如果确实要回退，请到项目发布页手动下载")
	}
	return nil
}

// environment 把「触碰运行环境」的动作收拢成可注入的字段，
// 让渠道判定能在任意平台上做表驱动测试，而不依赖测试机真的装在哪。
type environment struct {
	goos         string
	goarch       string
	executable   func() (string, error)
	evalSymlinks func(string) (string, error)
}

func defaultEnvironment() environment {
	return environment{
		goos:         runtime.GOOS,
		goarch:       runtime.GOARCH,
		executable:   os.Executable,
		evalSymlinks: filepath.EvalSymlinks,
	}
}

// normalizeVersion 去掉可选的 v 前缀并校验形状。
func normalizeVersion(v string) (string, error) {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if !versionRE.MatchString(v) {
		return "", errs.New(errs.CategoryInput, "selfupdate.version",
			"版本号格式不合法（应形如 0.2.0）", "")
	}
	return v, nil
}

// canon 给版本号补上 semver 包要求的 v 前缀。
func canon(v string) string {
	v = strings.TrimSpace(v)
	if v == "" || strings.HasPrefix(v, "v") {
		return v
	}
	return "v" + v
}

// probeWritableCached 探测安装目录可写，成功的结论缓存 writableTTL。
//
// Capability 是面向 API 的方法，界面每拉一次状态就调它一次；每次都在 /Applications
// 或 Program Files 里建删一个文件，会白白惊动 Spotlight 与杀毒软件。
// 只缓存成功：失败每次都重探，这样用户一把权限改好按钮立刻就亮，不用等缓存过期。
// 真正动手前（Apply 里）走的是不带缓存的 probeWritable。
func (u *Updater) probeWritableCached(dir string) error {
	u.probeMu.Lock()
	defer u.probeMu.Unlock()
	if u.probeDir == dir && time.Since(u.probeAt) < writableTTL {
		return nil
	}
	if err := probeWritable(dir); err != nil {
		return err
	}
	u.probeDir, u.probeAt = dir, time.Now()
	return nil
}

// probeWritable 真的在目录里建一个临时文件再删掉。只看权限位判断不了只读挂载、
// SIP 保护、ACL 与 Windows 的继承权限 —— 而这些正是实际会踩到的。
func probeWritable(dir string) error {
	f, err := os.CreateTemp(dir, probePrefix+"*")
	if err != nil {
		return err
	}
	name := f.Name()
	if err := f.Close(); err != nil {
		_ = os.Remove(name)
		return err
	}
	return os.Remove(name)
}

// trunc 截断要写进日志的外部字段（签名的可信注释来自发布流水线，仍然按外部输入对待）。
func trunc(s string, n int) string {
	s = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' {
			return ' '
		}
		return r
	}, s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
