package mpv

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

// injectDetect 注入假的「查 PATH」与「跑 --version」实现，测试结束后还原。
func injectDetect(t *testing.T, look func(string) (string, error), run func(string) (string, error)) {
	t.Helper()
	origLook, origRun := lookMPVPath, runMPVVersion
	if look != nil {
		lookMPVPath = look
	}
	if run != nil {
		runMPVVersion = run
	}
	t.Cleanup(func() { lookMPVPath, runMPVVersion = origLook, origRun })
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
			injectDetect(t, nil, func(path string) (string, error) {
				require.Equal(t, "/fake/mpv", path, "应对显式路径执行 --version")
				return tc.output, nil
			})

			info, err := Detect("/fake/mpv")
			if len(tc.wantErr) > 0 {
				require.Error(t, err)
				for _, frag := range tc.wantErr {
					require.ErrorContains(t, err, frag)
				}
				return
			}
			require.NoError(t, err)
			require.Equal(t, "/fake/mpv", info.Path)
			require.Equal(t, tc.wantVersion, info.Version)
		})
	}
}

// 验收：explicitPath 为空时走 PATH 查找；找不到给安装指引。
func TestDetect_PathLookup(t *testing.T) {
	t.Run("PATH 中找到", func(t *testing.T) {
		injectDetect(t,
			func(name string) (string, error) {
				require.Equal(t, "mpv", name)
				return "/somewhere/bin/mpv", nil
			},
			func(path string) (string, error) {
				require.Equal(t, "/somewhere/bin/mpv", path)
				return "mpv v0.41.0 Copyright", nil
			})

		info, err := Detect("")
		require.NoError(t, err)
		require.Equal(t, "/somewhere/bin/mpv", info.Path)
		require.Equal(t, "0.41.0", info.Version)
	})

	t.Run("PATH 中找不到", func(t *testing.T) {
		injectDetect(t, func(string) (string, error) {
			return "", errors.New("executable file not found in $PATH")
		}, nil)

		_, err := Detect("")
		require.ErrorContains(t, err, "未在 PATH 中找到 mpv")
		require.ErrorContains(t, err, "安装")
	})

	t.Run("显式路径无法执行", func(t *testing.T) {
		injectDetect(t, nil, func(string) (string, error) {
			return "", errors.New("permission denied")
		})

		_, err := Detect("/broken/mpv")
		require.ErrorContains(t, err, "无法执行")
		require.ErrorContains(t, err, "/broken/mpv")
	})
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
