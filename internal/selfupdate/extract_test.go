package selfupdate

// 解包层是这个包的攻击面：归档来自网络，即使验过签也按不可信输入处理。
// 这一组测试把每条拒绝规则都撞一遍。

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func writeArchive(t *testing.T, name string, body []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	require.NoError(t, os.WriteFile(p, body, 0o600))
	return p
}

func TestExtractRejectsUnsafeEntries(t *testing.T) {
	tests := []struct {
		name    string
		file    string
		archive func(t *testing.T) []byte
		wantMsg string
	}{
		{
			name: "tar 路径穿越",
			file: "x.tar.gz",
			archive: func(t *testing.T) []byte {
				return buildTarGz(t, []entry{{name: "../../etc/passwd", body: "pwned"}})
			},
			wantMsg: "内容不合法",
		},
		{
			name: "tar 绕一圈的路径穿越",
			file: "x.tar.gz",
			archive: func(t *testing.T) []byte {
				return buildTarGz(t, []entry{{name: "a/b/../../../../evil", body: "pwned"}})
			},
			wantMsg: "内容不合法",
		},
		{
			name: "tar 绝对路径",
			file: "x.tar.gz",
			archive: func(t *testing.T) []byte {
				return buildTarGz(t, []entry{{name: "/etc/passwd", body: "pwned"}})
			},
			wantMsg: "内容不合法",
		},
		{
			name: "tar 软链接",
			file: "x.tar.gz",
			archive: func(t *testing.T) []byte {
				return buildTarGz(t, []entry{{name: "nagare", link: "/etc/passwd"}})
			},
			wantMsg: "内容不合法",
		},
		{
			name: "tar 硬链接",
			file: "x.tar.gz",
			archive: func(t *testing.T) []byte {
				return buildTarGz(t, []entry{
					{name: "real", body: "x"},
					{name: "nagare", hard: "real"},
				})
			},
			wantMsg: "内容不合法",
		},
		{
			name: "tar 条目名含反斜杠",
			file: "x.tar.gz",
			archive: func(t *testing.T) []byte {
				return buildTarGz(t, []entry{{name: `..\..\evil`, body: "pwned"}})
			},
			wantMsg: "内容不合法",
		},
		{
			name: "zip 路径穿越",
			file: "x.zip",
			archive: func(t *testing.T) []byte {
				return buildZip(t, []entry{{name: "../../etc/passwd", body: "pwned"}})
			},
			wantMsg: "内容不合法",
		},
		{
			name: "zip 绝对路径",
			file: "x.zip",
			archive: func(t *testing.T) []byte {
				return buildZip(t, []entry{{name: "/etc/passwd", body: "pwned"}})
			},
			wantMsg: "内容不合法",
		},
		{
			name: "zip 软链接",
			file: "x.zip",
			archive: func(t *testing.T) []byte {
				return buildZip(t, []entry{{name: "nagare", link: "/etc/passwd"}})
			},
			wantMsg: "内容不合法",
		},
		{
			name: "zip 同名条目出现两次",
			file: "x.zip",
			archive: func(t *testing.T) []byte {
				return buildZip(t, []entry{
					{name: "nagare", body: "first"},
					{name: "nagare", body: "second"},
				})
			},
			wantMsg: "写文件失败",
		},
		{
			name:    "gzip 数据损坏",
			file:    "x.tar.gz",
			archive: func(t *testing.T) []byte { return []byte("this is not gzip") },
			wantMsg: "已损坏",
		},
		{
			name:    "认不出的格式",
			file:    "x.rar",
			archive: func(t *testing.T) []byte { return []byte("whatever") },
			wantMsg: "无法识别",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			src := writeArchive(t, tc.file, tc.archive(t))
			dst := filepath.Join(t.TempDir(), "out")

			err := extractArchive(src, dst, tc.file)
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.wantMsg)

			// 越界的条目一个都不能落在解包目录之外。
			require.NoFileExists(t, filepath.Join(filepath.Dir(dst), "evil"))
			require.NoFileExists(t, filepath.Join(filepath.Dir(dst), "..", "evil"))
		})
	}
}

func TestExtractEnforcesSizeLimits(t *testing.T) {
	tests := []struct {
		name    string
		limits  func(e *extractor)
		entries []entry
	}{
		{
			name:    "单条超过上限",
			limits:  func(e *extractor) { e.maxEntry = 4 },
			entries: []entry{{name: "nagare", body: "0123456789"}},
		},
		{
			name:   "总量超过上限",
			limits: func(e *extractor) { e.maxEntry = 8; e.maxTotal = 10 },
			entries: []entry{
				{name: "a", body: "12345678"},
				{name: "b", body: "12345678"},
			},
		},
		{
			name:   "条目数超过上限",
			limits: func(e *extractor) { e.maxCount = 2 },
			entries: []entry{
				{name: "a", body: "1"}, {name: "b", body: "2"}, {name: "c", body: "3"},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name+"（tar）", func(t *testing.T) {
			src := writeArchive(t, "x.tar.gz", buildTarGz(t, tc.entries))
			dst := t.TempDir()
			e := newExtractor(dst)
			tc.limits(e)
			err := e.tarGz(src)
			require.Error(t, err)
			require.Contains(t, err.Error(), "内容不合法")
		})
		t.Run(tc.name+"（zip）", func(t *testing.T) {
			src := writeArchive(t, "x.zip", buildZip(t, tc.entries))
			dst := t.TempDir()
			e := newExtractor(dst)
			tc.limits(e)
			err := e.zip(src)
			require.Error(t, err)
			require.Contains(t, err.Error(), "内容不合法")
		})
	}
}

// TestExtractNormalizesModes：权限只保留可执行位，其余归一化。
// 归档里的权限是构建机的产物，原样照搬会把 0777 之类的东西带进用户机器。
func TestExtractNormalizesModes(t *testing.T) {
	entries := []entry{
		{name: "dir", dir: true},
		{name: "dir/nagare", body: "bin", exec: true},
		{name: "dir/README.md", body: "doc"},
	}
	for _, tc := range []struct {
		name  string
		file  string
		build func(t *testing.T) []byte
	}{
		{"tar", "x.tar.gz", func(t *testing.T) []byte { return buildTarGz(t, entries) }},
		{"zip", "x.zip", func(t *testing.T) []byte { return buildZip(t, entries) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := writeArchive(t, tc.file, tc.build(t))
			dst := filepath.Join(t.TempDir(), "out")
			require.NoError(t, extractArchive(src, dst, tc.file))

			bin, err := os.Stat(filepath.Join(dst, "dir", "nagare"))
			require.NoError(t, err)
			doc, err := os.Stat(filepath.Join(dst, "dir", "README.md"))
			require.NoError(t, err)
			require.Equal(t, execMode, bin.Mode().Perm())
			require.Equal(t, fileMode, doc.Mode().Perm())
			require.Equal(t, "bin", readFile(t, filepath.Join(dst, "dir", "nagare")))
		})
	}
}

// TestExtractCreatesMissingParents：zip 里不一定有目录条目，父目录要自己补。
func TestExtractCreatesMissingParents(t *testing.T) {
	src := writeArchive(t, "x.zip", buildZip(t, []entry{
		{name: "Nagare.app/Contents/MacOS/nagare", body: "bin", exec: true},
	}))
	dst := filepath.Join(t.TempDir(), "out")
	require.NoError(t, extractArchive(src, dst, "x.zip"))
	require.Equal(t, "bin", readFile(t, filepath.Join(dst, "Nagare.app", "Contents", "MacOS", "nagare")))
}
