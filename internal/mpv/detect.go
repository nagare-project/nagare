// Package mpv 管理外部 mpv 进程：探测二进制与版本、带 IPC socket 启动、
// JSON IPC 双向通信（属性观察 + 命令），以及进程死亡后的状态收敛。
//
// 决议 A5：mpv 按平台分发 —— macOS/Linux 检测系统安装，缺失或过旧时给出
// 可操作的引导；Windows 的命名管道 IPC 在打包里程碑（M4）接入。
package mpv

import (
	"fmt"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"
)

// MinVersion 是允许的最低 mpv 版本。
// 0.32.0 之前 observe_property / JSON IPC 行为不够稳定，取保守下限。
const MinVersion = "0.32.0"

// Info 是 Detect 的探测结果。
type Info struct {
	Path    string // mpv 可执行文件的绝对路径
	Version string // 解析出的版本号，如 "0.41.0" 或 "0.38.0-641-g1234abcd"
}

// 以下两个函数变量把「执行外部命令拿输出」抽出来，单元测试注入假实现，
// 不需要真实 mpv 就能覆盖版本解析与错误分支。
var (
	// lookMPVPath 在 PATH 中查找可执行文件。
	lookMPVPath = exec.LookPath

	// runMPVVersion 执行 `<path> --version` 并返回合并输出。
	runMPVVersion = func(path string) (string, error) {
		out, err := exec.Command(path, "--version").CombinedOutput()
		return string(out), err
	}
)

// versionLineRe 匹配 `mpv --version` 首行的版本 token。
// 实测形态：`mpv v0.41.0 Copyright © 2000-2025 ...`、`mpv 0.38.0-641-g1234 ...`。
// 必须锚定在行首的 "mpv " 之后，否则会误抓 Copyright 里的年份数字。
var versionLineRe = regexp.MustCompile(`^mpv[ \t]+v?([0-9][0-9A-Za-z.+~-]*)`)

// Detect 定位并校验 mpv：explicitPath 非空则只验证它；否则在 PATH 中查找。
// 通过 `mpv --version` 解析版本，低于 MinVersion 时返回带升级指引的错误。
func Detect(explicitPath string) (Info, error) {
	path := explicitPath
	if path == "" {
		found, err := lookMPVPath("mpv")
		if err != nil {
			return Info{}, fmt.Errorf("未在 PATH 中找到 mpv：%s", installHint())
		}
		path = found
	}

	out, err := runMPVVersion(path)
	if err != nil {
		return Info{}, fmt.Errorf(
			"无法执行 %q 获取 mpv 版本（%v）：请确认该路径指向可运行的 mpv，或在设置中留空以自动从 PATH 查找",
			path, err)
	}

	version, parts, err := parseVersionOutput(out)
	if err != nil {
		return Info{}, fmt.Errorf("无法识别 %q 的版本输出（首行应形如 \"mpv v0.41.0\"）：%w", path, err)
	}

	if compareVersion(parts, mustParseVersion(MinVersion)) < 0 {
		return Info{}, fmt.Errorf("mpv 版本过低：当前 %s，最低要求 %s。%s", version, MinVersion, upgradeHint())
	}
	return Info{Path: path, Version: version}, nil
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

// installHint 返回按当前平台的 mpv 安装指引。
func installHint() string {
	switch runtime.GOOS {
	case "darwin":
		return "请先安装：brew install mpv（或从 https://mpv.io/installation/ 下载）"
	case "windows":
		return "请从 https://mpv.io/installation/ 下载安装 mpv"
	default:
		return "请用发行版包管理器安装（如 apt install mpv / dnf install mpv / pacman -S mpv）"
	}
}

// upgradeHint 返回按当前平台的 mpv 升级指引。
func upgradeHint() string {
	switch runtime.GOOS {
	case "darwin":
		return "请升级：brew upgrade mpv"
	case "windows":
		return "请从 https://mpv.io/installation/ 下载最新版本"
	default:
		return "请用发行版包管理器升级 mpv（如 apt / dnf / pacman）"
	}
}
