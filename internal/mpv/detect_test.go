package mpv

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// detectFakes 是一组注入到探测链路的假环境；零值表示「什么都不存在」。
type detectFakes struct {
	goos     string                       // 视为的平台；空则沿用宿主
	exeDir   string                       // 可执行文件目录；空表示取不到
	files    map[string]bool              // 存在的普通文件集合
	pathHit  string                       // PATH 中命中的路径；空表示 PATH 找不到
	version  func(string) (string, error) // --version 的输出；nil 表示一律成功 0.41.0
	lookPath func(string) (string, error) // 覆盖 pathHit 的自定义 PATH 查找
}

// injectDetect 把假环境装进包级注入点，测试结束后还原。
func injectDetect(t *testing.T, f detectFakes) {
	t.Helper()
	origLook, origRun := lookMPVPath, runMPVVersion
	origExists, origExeDir, origGOOS := fileExists, executableDir, detectGOOS
	t.Cleanup(func() {
		lookMPVPath, runMPVVersion = origLook, origRun
		fileExists, executableDir, detectGOOS = origExists, origExeDir, origGOOS
	})

	if f.goos != "" {
		detectGOOS = f.goos
	}
	fileExists = func(p string) bool { return f.files[p] }
	executableDir = func() (string, error) {
		if f.exeDir == "" {
			return "", errors.New("no executable")
		}
		return f.exeDir, nil
	}
	switch {
	case f.lookPath != nil:
		lookMPVPath = f.lookPath
	case f.pathHit != "":
		lookMPVPath = func(string) (string, error) { return f.pathHit, nil }
	default:
		lookMPVPath = func(string) (string, error) {
			return "", errors.New("executable file not found in $PATH")
		}
	}
	if f.version != nil {
		runMPVVersion = f.version
	} else {
		runMPVVersion = func(string) (string, error) { return "mpv v0.41.0 Copyright", nil }
	}
}

// 验收：版本解析表测试 —— 覆盖实测输出形态、下限拒绝、乱格式。
func TestDetect_VersionParsing(t *testing.T) {
	cases := []struct {
		name        string
		output      string
		wantVersion string
		wantErr     []string // 期望错误信息包含的片段；空表示成功
	}{
		{
			name:        "homebrew 形态（v 前缀）",
			output:      "mpv v0.41.0 Copyright © 2000-2025 mpv/MPlayer/mplayer2 projects\nlibplacebo version: v7.360.1",
			wantVersion: "0.41.0",
		},
		{
			name:        "开发版后缀",
			output:      "mpv 0.38.0-641-g1234abcd Copyright © 2000-2024 mpv/MPlayer/mplayer2 projects",
			wantVersion: "0.38.0-641-g1234abcd",
		},
		{
			name:        "恰好等于下限",
			output:      "mpv 0.32.0 Copyright © 2000-2020 mpv/MPlayer/mplayer2 projects",
			wantVersion: "0.32.0",
		},
		{
			name:        "两段式版本号",
			output:      "mpv v0.40 Copyright © 2000-2025 mpv/MPlayer/mplayer2 projects",
			wantVersion: "0.40",
		},
		{
			name:    "低于下限应拒绝并含升级指引",
			output:  "mpv 0.31.0 Copyright © 2000-2019 mpv/MPlayer/mplayer2 projects",
			wantErr: []string{"版本过低", "0.31.0", MinVersion, "升级"},
		},
		{
			name:    "乱格式",
			output:  "definitely not mpv output",
			wantErr: []string{"无法识别"},
		},
		{
			name:    "空输出",
			output:  "",
			wantErr: []string{"无法识别"},
		},
		{
			name:    "缺版本号时不得误抓 Copyright 年份",
			output:  "mpv Copyright © 2000-2025 mpv/MPlayer/mplayer2 projects",
			wantErr: []string{"无法识别"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			injectDetect(t, detectFakes{version: func(path string) (string, error) {
				require.Equal(t, "/fake/mpv", path, "应对显式路径执行 --version")
				return tc.output, nil
			}})

			info, err := Detect("/fake/mpv")
			if len(tc.wantErr) > 0 {
				require.Error(t, err)
				for _, frag := range tc.wantErr {
					require.ErrorContains(t, err, frag)
				}
				return
			}
			require.NoError(t, err)
			assert.Equal(t, "/fake/mpv", info.Path)
			assert.Equal(t, tc.wantVersion, info.Version)
			assert.Equal(t, SourceExplicit, info.Source)
		})
	}
}

// 验收：候选顺序 内置 → PATH → 已知位置，每条路径都能被找到并标对 Source。
func TestDetect_CandidateOrder(t *testing.T) {
	exeDir := filepath.Join("/", "app")
	bundledUnix := filepath.Join(exeDir, "mpv", "mpv")
	bundledWin := filepath.Join(exeDir, "mpv", "mpv.exe")

	cases := []struct {
		name       string
		fakes      detectFakes
		wantPath   string
		wantSource string
	}{
		{
			name:       "windows 内置 mpv.exe",
			fakes:      detectFakes{goos: "windows", exeDir: exeDir, files: map[string]bool{bundledWin: true}, pathHit: "/elsewhere/mpv"},
			wantPath:   bundledWin,
			wantSource: SourceBundled,
		},
		{
			name:       "darwin 内置目录也认（同目录 mpv/mpv）",
			fakes:      detectFakes{goos: "darwin", exeDir: exeDir, files: map[string]bool{bundledUnix: true}, pathHit: "/elsewhere/mpv"},
			wantPath:   bundledUnix,
			wantSource: SourceBundled,
		},
		{
			name:       "内置缺失时走 PATH，PATH 优先于已知位置",
			fakes:      detectFakes{goos: "darwin", exeDir: exeDir, files: map[string]bool{"/opt/homebrew/bin/mpv": true}, pathHit: "/somewhere/bin/mpv"},
			wantPath:   "/somewhere/bin/mpv",
			wantSource: SourcePath,
		},
		{
			name:       "取不到可执行文件目录也不影响 PATH 查找",
			fakes:      detectFakes{goos: "linux", pathHit: "/usr/games/mpv"},
			wantPath:   "/usr/games/mpv",
			wantSource: SourcePath,
		},
	}
	// 每个平台的已知位置逐条覆盖：只有该条存在时必须命中它。
	for _, goos := range []string{"darwin", "linux"} {
		for _, known := range knownLocations(goos) {
			cases = append(cases, struct {
				name       string
				fakes      detectFakes
				wantPath   string
				wantSource string
			}{
				name:       goos + " 已知位置 " + known,
				fakes:      detectFakes{goos: goos, exeDir: exeDir, files: map[string]bool{known: true}},
				wantPath:   known,
				wantSource: SourceKnown,
			})
		}
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			injectDetect(t, tc.fakes)
			info, err := Detect("")
			require.NoError(t, err)
			assert.Equal(t, tc.wantPath, info.Path)
			assert.Equal(t, tc.wantSource, info.Source)
			assert.Equal(t, "0.41.0", info.Version)
		})
	}
}

// 已知位置按顺序取第一个存在的：Apple Silicon Homebrew 优先于 Intel Homebrew。
func TestDetect_KnownLocationOrder(t *testing.T) {
	injectDetect(t, detectFakes{goos: "darwin", files: map[string]bool{
		"/usr/local/bin/mpv":    true,
		"/opt/homebrew/bin/mpv": true,
	}})
	info, err := Detect("")
	require.NoError(t, err)
	assert.Equal(t, "/opt/homebrew/bin/mpv", info.Path)
}

// 什么都找不到：错误信息带平台安装指引；windows 没有已知位置表。
func TestDetect_NotFound(t *testing.T) {
	cases := []struct {
		goos     string
		wantFrag []string
	}{
		{"darwin", []string{"未找到 mpv", "brew install mpv", "https://brew.sh"}},
		{"linux", []string{"未找到 mpv", "apt install mpv", "dnf"}},
		{"windows", []string{"未找到 mpv", "重新安装 nagare"}},
	}
	for _, tc := range cases {
		t.Run(tc.goos, func(t *testing.T) {
			injectDetect(t, detectFakes{goos: tc.goos, exeDir: "/app"})
			_, err := Detect("")
			require.Error(t, err)
			for _, frag := range tc.wantFrag {
				assert.ErrorContains(t, err, frag)
			}
		})
	}
	assert.Nil(t, knownLocations("windows"))
	assert.Equal(t, "mpv.exe", binaryName("windows"))
	assert.Equal(t, "mpv", binaryName("darwin"))
}

// 候选存在但跑不起来：错误里带路径，不静默换下一个（用户要知道是哪个坏了）。
func TestDetect_CandidateBroken(t *testing.T) {
	injectDetect(t, detectFakes{
		goos:    "darwin",
		files:   map[string]bool{"/opt/homebrew/bin/mpv": true},
		version: func(string) (string, error) { return "", errors.New("permission denied") },
	})
	_, err := Detect("")
	require.ErrorContains(t, err, "无法执行")
	require.ErrorContains(t, err, "/opt/homebrew/bin/mpv")

	_, err = Detect("/broken/mpv")
	require.ErrorContains(t, err, "/broken/mpv")
}

// 显式路径不走候选查找：候选全部存在也只验证给定路径。
func TestDetect_ExplicitSkipsCandidates(t *testing.T) {
	injectDetect(t, detectFakes{
		goos:    "darwin",
		exeDir:  "/app",
		files:   map[string]bool{"/app/mpv/mpv": true, "/opt/homebrew/bin/mpv": true},
		pathHit: "/usr/bin/mpv",
		lookPath: func(string) (string, error) {
			t.Fatal("显式路径不应查 PATH")
			return "", nil
		},
	})
	info, err := Detect("/custom/mpv")
	require.NoError(t, err)
	assert.Equal(t, "/custom/mpv", info.Path)
	assert.Equal(t, SourceExplicit, info.Source)
}

// 安装指引：三平台形状固定（前端按此契约渲染）。
func TestInstallGuide(t *testing.T) {
	darwin := installGuide("darwin")
	assert.Equal(t, "brew install mpv", darwin.Command)
	assert.Equal(t, installURL, darwin.URL)
	assert.Contains(t, darwin.Note, "brew.sh")

	linux := installGuide("linux")
	assert.Equal(t, "sudo apt install mpv", linux.Command)
	assert.Contains(t, linux.Note, "dnf")
	assert.Contains(t, linux.Note, "pacman")

	windows := installGuide("windows")
	assert.Empty(t, windows.Command)
	assert.Contains(t, windows.Note, "内置")

	injectDetect(t, detectFakes{goos: "linux"})
	assert.Equal(t, linux, InstallGuide())
}

func TestCompareVersion(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"0.32.0", "0.32.0", 0},
		{"0.31.9", "0.32.0", -1},
		{"0.41.0", "0.32.0", 1},
		{"1.0.0", "0.99.9", 1},
		{"0.32.1", "0.32.0", 1},
	}
	for _, tc := range cases {
		pa, ok := parseVersionParts(tc.a)
		require.True(t, ok)
		pb, ok := parseVersionParts(tc.b)
		require.True(t, ok)
		require.Equal(t, tc.want, compareVersion(pa, pb), "%s vs %s", tc.a, tc.b)
	}
}

// H2 回归（评审 2026-09-02）：候选二进制卡住不返回时，探测必须超时收场
// —— 否则启动流程（在 HTTP 服务之前跑）和 /api/mpv/detect 都会被永久挂住。
func TestDetect_VersionCommandTimesOut(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("用 shell 脚本造卡死候选，仅在类 Unix 上跑")
	}
	// 一个永远不返回的「mpv」：正是坏安装/挂在网络盘上的二进制的表现。
	hung := filepath.Join(t.TempDir(), "mpv")
	require.NoError(t, os.WriteFile(hung, []byte("#!/bin/sh\nsleep 300\n"), 0o755))

	origRun, origTimeout := runMPVVersion, versionTimeoutForTest
	t.Cleanup(func() { runMPVVersion, versionTimeoutForTest = origRun, origTimeout })
	runMPVVersion = realRunMPVVersion
	versionTimeoutForTest = 300 * time.Millisecond

	start := time.Now()
	_, err := Detect(hung)
	elapsed := time.Since(start)

	require.Error(t, err)
	assert.ErrorContains(t, err, "无法执行")
	assert.ErrorContains(t, err, "超时")
	// 上限 = 超时 + WaitDelay 宽限 + 余量：孙进程攥着管道也必须断开返回。
	assert.Less(t, elapsed, 5*time.Second, "探测必须在超时后返回，不得挂死")
	assert.GreaterOrEqual(t, elapsed, 300*time.Millisecond, "应当真的等满了超时才放弃")
}
