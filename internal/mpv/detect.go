// Package mpv 管理外部 mpv 进程：探测二进制与版本、带 IPC socket 启动、
// JSON IPC 双向通信（属性观察 + 命令），以及进程死亡后的状态收敛。
//
// 决议 A5：mpv 按平台分发 —— Windows 安装包内置（放在 nagare 同目录的 mpv/ 下）；
// macOS/Linux 检测系统安装，缺失或过旧时给出可操作的引导。
package mpv

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// MinVersion 是允许的最低 mpv 版本。
// 0.32.0 之前 observe_property / JSON IPC 行为不够稳定，取保守下限。
const MinVersion = "0.32.0"

// Info.Source 的取值：说明 mpv 是从哪条候选路径找到的（设置页展示、日志诊断）。
const (
	SourceExplicit = "explicit" // 调用方显式指定的路径
	SourceBundled  = "bundled"  // nagare 可执行文件同目录下的 mpv/（Windows 安装包内置）
	SourcePath     = "path"     // PATH 环境变量
	SourceKnown    = "known"    // 平台已知安装位置（Homebrew / MacPorts / 发行版包）
)

// installURL 是 mpv 官方安装页。
const installURL = "https://mpv.io/installation/"

// versionTimeout 是 `mpv --version` 的执行上限。没有它，一个卡死的候选二进制
// 会挂死整个启动流程（探测在 HTTP 服务起来之前跑），并把 /api/mpv/detect
// 连同 Runtime 的探测锁一起永久堵住。
const versionTimeout = 5 * time.Second

// versionKillGrace 是 ctx 超时后强行断开管道前的宽限期。
// 必须有：CommandContext 只杀直接子进程，若它又派生了孙进程（坏安装里的
// 包装脚本就是这样），孙进程会继续攥着 stdout/stderr 管道，
// CombinedOutput 便一直读不到 EOF —— 杀了进程照样挂死。WaitDelay 到点强断管道。
const versionKillGrace = time.Second

// versionTimeoutForTest 让测试把上限调小；生产恒为 versionTimeout。
var versionTimeoutForTest = versionTimeout

// Info 是 Detect 的探测结果。
type Info struct {
	Path    string // mpv 可执行文件的绝对路径
	Version string // 解析出的版本号，如 "0.41.0" 或 "0.38.0-641-g1234abcd"
	Source  string // 见 Source* 常量
}

// Guide 是按平台给用户的 mpv 安装指引（设置页在 mpv 缺失时展示）。
type Guide struct {
	Command string `json:"command"` // 一行安装命令；Windows 为空（安装包已内置）
	URL     string `json:"url"`     // 官方安装页
	Note    string `json:"note"`    // 补充说明（其他发行版命令 / 没有 Homebrew 怎么办）
}

// 以下函数变量把「触碰外部环境」的动作抽出来，单元测试注入假实现，
// 不需要真实 mpv 或真实文件系统就能覆盖每条候选路径与错误分支。
var (
	// lookMPVPath 在 PATH 中查找可执行文件。
	lookMPVPath = exec.LookPath

	// runMPVVersion 执行 `<path> --version` 并返回合并输出（限时 versionTimeout）。
	runMPVVersion = realRunMPVVersion

	// fileExists 判断路径是否为存在的普通文件（候选位置探测用）。
	fileExists = func(path string) bool {
		st, err := os.Stat(path)
		return err == nil && !st.IsDir()
	}

	// executableDir 返回 nagare 可执行文件所在目录（内置 mpv 的锚点）。
	// 解析符号链接：Homebrew 的 bin/ 里是指向 Cellar 的链接。
	executableDir = func() (string, error) {
		exe, err := os.Executable()
		if err != nil {
			return "", err
		}
		if resolved, err := filepath.EvalSymlinks(exe); err == nil {
			exe = resolved
		}
		return filepath.Dir(exe), nil
	}

	// detectGOOS 是探测时视为的平台，测试用它覆盖各平台的候选表与指引。
	detectGOOS = runtime.GOOS
)

// realRunMPVVersion 执行 `<path> --version` 并返回合并输出（限时 versionTimeoutForTest）。
func realRunMPVVersion(path string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), versionTimeoutForTest)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, "--version")
	cmd.WaitDelay = versionKillGrace
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		return string(out), fmt.Errorf("执行超时（超过 %s 未返回）：%w", versionTimeoutForTest, ctx.Err())
	}
	return string(out), err
}

// versionLineRe 匹配 `mpv --version` 首行的版本 token。
// 实测形态：`mpv v0.41.0 Copyright © 2000-2025 ...`、`mpv 0.38.0-641-g1234 ...`。
// 必须锚定在行首的 "mpv " 之后，否则会误抓 Copyright 里的年份数字。
var versionLineRe = regexp.MustCompile(`^mpv[ \t]+v?([0-9][0-9A-Za-z.+~-]*)`)

// candidate 是一条候选路径及其来源标记。
type candidate struct {
	path   string
	source string
}

// Detect 定位并校验 mpv：explicitPath 非空则只验证它；否则按固定顺序找第一个
// 存在的候选 —— 内置目录 → PATH → 平台已知位置（Finder 启动的 .app 拿到的 PATH
// 只有 /usr/bin:/bin:/usr/sbin:/sbin，brew 装在 /opt/homebrew/bin 的 mpv 必须靠
// 已知位置兜住）。通过 `mpv --version` 解析版本，低于 MinVersion 时返回带升级指引的错误。
func Detect(explicitPath string) (Info, error) {
	if explicitPath != "" {
		return verify(explicitPath, SourceExplicit)
	}
	c, ok := locate()
	if !ok {
		return Info{}, fmt.Errorf("未找到 mpv（已检查 PATH 与常见安装位置）：%s", installHint())
	}
	return verify(c.path, c.source)
}

// locate 按 内置 → PATH → 已知位置 的顺序返回第一个存在的候选。
func locate() (candidate, bool) {
	if dir, err := executableDir(); err == nil {
		bundled := filepath.Join(dir, "mpv", binaryName(detectGOOS))
		if fileExists(bundled) {
			return candidate{path: bundled, source: SourceBundled}, true
		}
	}
	if found, err := lookMPVPath("mpv"); err == nil {
		return candidate{path: found, source: SourcePath}, true
	}
	for _, p := range knownLocations(detectGOOS) {
		if fileExists(p) {
			return candidate{path: p, source: SourceKnown}, true
		}
	}
	return candidate{}, false
}

// binaryName 返回平台上的 mpv 可执行文件名。
func binaryName(goos string) string {
	if goos == "windows" {
		return "mpv.exe"
	}
	return "mpv"
}

// knownLocations 返回平台上不在默认 PATH 里、但常见的 mpv 安装位置。
// Windows 没有：安装包内置的走 SourceBundled。
func knownLocations(goos string) []string {
	switch goos {
	case "darwin":
		return []string{
			"/opt/homebrew/bin/mpv",                    // Apple Silicon Homebrew
			"/usr/local/bin/mpv",                       // Intel Homebrew
			"/opt/local/bin/mpv",                       // MacPorts
			"/Applications/mpv.app/Contents/MacOS/mpv", // 官方 .app
		}
	case "linux":
		return []string{"/usr/bin/mpv", "/usr/local/bin/mpv"}
	}
	return nil
}

// verify 对一条候选执行 --version 并校验版本下限。
func verify(path, source string) (Info, error) {
	out, err := runMPVVersion(path)
	if err != nil {
		return Info{}, fmt.Errorf(
			"无法执行 %q 获取 mpv 版本（%v）：请确认该路径指向可运行的 mpv，或在设置中留空以自动查找",
			path, err)
	}

	version, parts, err := parseVersionOutput(out)
	if err != nil {
		return Info{}, fmt.Errorf("无法识别 %q 的版本输出（首行应形如 \"mpv v0.41.0\"）：%w", path, err)
	}

	if compareVersion(parts, mustParseVersion(MinVersion)) < 0 {
		return Info{}, fmt.Errorf("mpv 版本过低：当前 %s，最低要求 %s。%s", version, MinVersion, upgradeHint())
	}
	return Info{Path: path, Version: version, Source: source}, nil
}

// parseVersionOutput 从 `mpv --version` 输出中解析版本 token 与数字三段。
func parseVersionOutput(out string) (string, [3]int, error) {
	firstLine, _, _ := strings.Cut(strings.TrimSpace(out), "\n")
	m := versionLineRe.FindStringSubmatch(firstLine)
	if m == nil {
		return "", [3]int{}, fmt.Errorf("首行是 %q", firstLine)
	}
	token := m[1]
	parts, ok := parseVersionParts(token)
	if !ok {
		return "", [3]int{}, fmt.Errorf("版本 token %q 不含可比较的数字段", token)
	}
	return token, parts, nil
}

// parseVersionParts 宽松解析版本号前三段数字：
// "0.38.0-641-g1234" → [0 38 0]；"0.32" → [0 32 0]。每段取前导数字，遇非数字截断。
func parseVersionParts(token string) ([3]int, bool) {
	var parts [3]int
	segs := strings.SplitN(token, ".", 4)
	for i := 0; i < 3 && i < len(segs); i++ {
		digits := leadingDigits(segs[i])
		if digits == "" {
			// 第一段必须有数字；后续段没有数字就当 0 结束（如 "0.38.0-641" 的尾巴）
			if i == 0 {
				return parts, false
			}
			break
		}
		n, err := strconv.Atoi(digits)
		if err != nil {
			return parts, false
		}
		parts[i] = n
	}
	return parts, true
}

func leadingDigits(s string) string {
	end := 0
	for end < len(s) && s[end] >= '0' && s[end] <= '9' {
		end++
	}
	return s[:end]
}

// mustParseVersion 解析格式已知合法的常量版本号（如 MinVersion）。
func mustParseVersion(s string) [3]int {
	parts, ok := parseVersionParts(s)
	if !ok {
		panic("mpv: 内置版本常量格式非法: " + s)
	}
	return parts
}

// compareVersion 按 [major minor patch] 逐段比较，a<b 返回 -1，相等 0，a>b 返回 1。
func compareVersion(a, b [3]int) int {
	for i := range a {
		switch {
		case a[i] < b[i]:
			return -1
		case a[i] > b[i]:
			return 1
		}
	}
	return 0
}

// InstallGuide 返回当前平台的 mpv 安装指引（设置页在 mpv 缺失时展示）。
func InstallGuide() Guide { return installGuide(detectGOOS) }

func installGuide(goos string) Guide {
	switch goos {
	case "darwin":
		return Guide{
			Command: "brew install mpv",
			URL:     installURL,
			Note:    "没有 Homebrew？先到 https://brew.sh 安装",
		}
	case "windows":
		return Guide{
			Command: "",
			URL:     installURL,
			Note:    "安装包已内置 mpv；若仍提示未找到，请重新安装 nagare",
		}
	default:
		return Guide{
			Command: "sudo apt install mpv",
			URL:     installURL,
			Note:    "Fedora：sudo dnf install mpv · Arch：sudo pacman -S mpv",
		}
	}
}

// installHint 把 Guide 压成一行，拼进「未找到 mpv」的错误信息。
func installHint() string {
	g := installGuide(detectGOOS)
	if g.Command == "" {
		return fmt.Sprintf("%s（或从 %s 下载）", g.Note, g.URL)
	}
	return fmt.Sprintf("请先安装：%s（%s；或从 %s 下载）", g.Command, g.Note, g.URL)
}

// upgradeHint 返回按当前平台的 mpv 升级指引。
func upgradeHint() string {
	switch detectGOOS {
	case "darwin":
		return "请升级：brew upgrade mpv"
	case "windows":
		return "请重新安装 nagare 以获得内置的新版 mpv，或从 " + installURL + " 下载最新版本"
	default:
		return "请用发行版包管理器升级 mpv（如 apt / dnf / pacman）"
	}
}
