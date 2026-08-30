package library

import (
	"crypto/md5"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"golang.org/x/text/unicode/norm"
)

// writeFileSized 写一个指定大小的占位文件。
func writeFileSized(t *testing.T, path string, size int) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, make([]byte, size), 0o644))
}

func relPaths(files []ScannedFile) []string {
	out := make([]string, 0, len(files))
	for _, f := range files {
		out = append(out, f.Kind+":"+f.RelPath)
	}
	return out
}

// 扫描应用 v3.1 Stage 0 规则集：噪声名、1MB 下限、深度上限。
func TestScanDirRules(t *testing.T) {
	root := t.TempDir()
	writeFileSized(t, filepath.Join(root, "Frieren", "[Sub] Frieren - 01.mkv"), minVideoSize)
	writeFileSized(t, filepath.Join(root, "Frieren", "[Sub] Frieren - 01.ass"), 10) // 字幕不受 1MB 限制
	writeFileSized(t, filepath.Join(root, "Frieren", "sample.mkv"), 1024)           // 小于 1MB 的视频跳过
	writeFileSized(t, filepath.Join(root, "Frieren", "._[Sub] Frieren - 01.mkv"), minVideoSize)
	writeFileSized(t, filepath.Join(root, ".DS_Store"), 10)
	writeFileSized(t, filepath.Join(root, "notes.txt"), 10)
	// 深度 3 可达（root=0 → a=1 → b=2 → c=3 的文件）
	writeFileSized(t, filepath.Join(root, "a", "b", "c", "deep.mkv"), minVideoSize)
	// 深度 4 不可达
	writeFileSized(t, filepath.Join(root, "a", "b", "c", "d", "toodeep.mkv"), minVideoSize)

	files, err := ScanDir(root)
	require.NoError(t, err)

	got := relPaths(files)
	assert.ElementsMatch(t, []string{
		"video:Frieren/[Sub] Frieren - 01.mkv",
		"subtitle:Frieren/[Sub] Frieren - 01.ass",
		"video:a/b/c/deep.mkv",
	}, got)
}

// macOS ExFAT 包目录：目录名带视频扩展名且内部恰有一个同扩展名大文件时，
// 以目录路径示人、AbsPath 指向内部真实文件；多个大文件则按普通目录递归。
func TestScanDirExfatBundle(t *testing.T) {
	root := t.TempDir()
	inner := filepath.Join(root, "episode01.mp4", "stream.mp4")
	writeFileSized(t, inner, minVideoSize)
	// 多子文件的「假包目录」：正常递归产出每个子文件
	writeFileSized(t, filepath.Join(root, "[29-38].mp4", "29.mp4"), minVideoSize)
	writeFileSized(t, filepath.Join(root, "[29-38].mp4", "30.mp4"), minVideoSize)

	files, err := ScanDir(root)
	require.NoError(t, err)

	byRel := map[string]ScannedFile{}
	for _, f := range files {
		byRel[f.RelPath] = f
	}

	bundle, ok := byRel["episode01.mp4"]
	require.True(t, ok, "包目录应以目录路径产出，实际: %v", relPaths(files))
	assert.Equal(t, inner, bundle.AbsPath)
	assert.Equal(t, "video", bundle.Kind)

	assert.Contains(t, byRel, "[29-38].mp4/29.mp4")
	assert.Contains(t, byRel, "[29-38].mp4/30.mp4")
}

// macOS 文件系统给出的 NFD 文件名必须归一成 NFC，否则同一部番的
// CJK 目录名会因编码形态不同而分裂成两个组。
func TestScanDirNormalizesNFC(t *testing.T) {
	root := t.TempDir()
	nfdName := norm.NFD.String("ポケモン")
	writeFileSized(t, filepath.Join(root, nfdName, "01.mkv"), minVideoSize)

	files, err := ScanDir(root)
	require.NoError(t, err)
	require.Len(t, files, 1)
	assert.Equal(t, norm.NFC.String("ポケモン")+"/01.mkv", files[0].RelPath)
	assert.True(t, strings.HasPrefix(files[0].RelPath, "ポケモン/"))
}

// 符号链接一律跳过：恶意压缩包塞一个指向敏感文件的 .ass 软链，
// 不得进入扫描结果（否则 mpv 会顺着它读目标文件当字幕）。
func TestScanDirSkipsSymlinks(t *testing.T) {
	root := t.TempDir()
	writeFileSized(t, filepath.Join(root, "Show", "[Sub] Show - 01.mkv"), minVideoSize)
	// 指向 root 外某敏感文件的字幕软链。
	secret := filepath.Join(t.TempDir(), "id_rsa")
	require.NoError(t, os.WriteFile(secret, []byte("PRIVATE KEY"), 0o600))
	require.NoError(t, os.Symlink(secret, filepath.Join(root, "Show", "[Sub] Show - 01.ass")))
	// 指向目录的软链也不该被递归。
	realDir := t.TempDir()
	writeFileSized(t, filepath.Join(realDir, "outside.mkv"), minVideoSize)
	require.NoError(t, os.Symlink(realDir, filepath.Join(root, "linkdir")))

	files, err := ScanDir(root)
	require.NoError(t, err)
	got := relPaths(files)
	assert.Equal(t, []string{"video:Show/[Sub] Show - 01.mkv"}, got, "软链字幕与软链目录都应被跳过")
}

func TestScanDirRejectsNonDir(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "x.mkv")
	writeFileSized(t, file, minVideoSize)

	_, err := ScanDir(file)
	assert.Error(t, err)
	_, err = ScanDir(filepath.Join(root, "不存在"))
	assert.Error(t, err)
}

// 字幕配对：有集号按集号 + 类型优先级（ass 赢 srt），无集号按 basename。
func TestPairSubtitles(t *testing.T) {
	items := BuildItems([]SourceFile{
		{RelPath: "F/[Sub] Frieren - 01.mkv", Size: 1 << 21, MTimeMs: 1},
		{RelPath: "F/OP Creditless.mkv", Size: 1 << 21, MTimeMs: 2},
	})
	require.Len(t, items, 2)

	subs := []ScannedFile{
		{RelPath: "F/[Sub] Frieren - 01.srt", AbsPath: "/abs/01.srt", Kind: "subtitle"},
		{RelPath: "F/[Sub] Frieren - 01.ass", AbsPath: "/abs/01.ass", Kind: "subtitle"},
		{RelPath: "F/OP Creditless.ass", AbsPath: "/abs/op.ass", Kind: "subtitle"},
		{RelPath: "F/extra.sup", AbsPath: "/abs/extra.sup", Kind: "subtitle"}, // sup 不参与配对
	}

	pairs := PairSubtitles(items, subs)

	var ep1, noEp Item
	for _, it := range items {
		if it.Episode != nil && *it.Episode == 1 {
			ep1 = it
		} else {
			noEp = it
		}
	}
	require.Contains(t, pairs, ep1.FileID)
	assert.Equal(t, "ass", pairs[ep1.FileID].Type, "同集号多字幕应按类型优先级取 ass")

	require.Contains(t, pairs, noEp.FileID)
	assert.Equal(t, "/abs/op.ass", pairs[noEp.FileID].AbsPath, "无集号条目按 basename 配对")
}

// Hash16M：小文件全量、大文件只取首 16MB。
func TestHash16M(t *testing.T) {
	dir := t.TempDir()

	small := filepath.Join(dir, "small.bin")
	require.NoError(t, os.WriteFile(small, []byte("nagare"), 0o644))
	sum := md5.Sum([]byte("nagare"))
	got, err := Hash16M(small)
	require.NoError(t, err)
	assert.Equal(t, hex.EncodeToString(sum[:]), got)

	// 16MB + 尾巴：尾巴不参与哈希。
	big := filepath.Join(dir, "big.bin")
	head := make([]byte, hash16MBytes)
	for i := range head {
		head[i] = byte(i % 251)
	}
	require.NoError(t, os.WriteFile(big, append(append([]byte{}, head...), []byte("tail-ignored")...), 0o644))
	headSum := md5.Sum(head)
	got, err = Hash16M(big)
	require.NoError(t, err)
	assert.Equal(t, hex.EncodeToString(headSum[:]), got)

	_, err = Hash16M(filepath.Join(dir, "missing.bin"))
	assert.Error(t, err)
}
