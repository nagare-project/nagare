package library

import (
	"bytes"
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

// dropCounts 把丢弃汇总压成 原因 → 数量。断言的是稳定码而不是中文串：
// 中文串会被顺手改掉，而断言改文案就红的测试没人愿意留。
func dropCounts(s DropSummary) map[DropReason]int {
	out := map[DropReason]int{}
	for _, g := range s.Groups {
		out[g.Reason] = g.Count
	}
	return out
}

// dropGroup 取某个原因那一组，没有则测试失败。
func dropGroup(t *testing.T, s DropSummary, reason DropReason) DropGroup {
	t.Helper()
	for _, g := range s.Groups {
		if g.Reason == reason {
			return g
		}
	}
	require.Failf(t, "缺少丢弃分组", "没有 %s，实际：%v", reason, dropCounts(s))
	return DropGroup{}
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

	res, err := ScanDir(root)
	require.NoError(t, err)

	got := relPaths(res.Files)
	assert.ElementsMatch(t, []string{
		"video:Frieren/[Sub] Frieren - 01.mkv",
		"subtitle:Frieren/[Sub] Frieren - 01.ass",
		"video:a/b/c/deep.mkv",
	}, got)

	// 谁活下来只讲了一半。另一半是「没活下来的那些去哪了」——
	// 它们必须能被数出来，否则用户只能看到一个短了几条的列表。
	assert.Equal(t, map[DropReason]int{
		DropTooSmall: 1, // Frieren/sample.mkv
		DropTooDeep:  1, // a/b/c/d 整棵子树，按目录计一笔
	}, dropCounts(res.Dropped))
	assert.Equal(t, 2, res.Dropped.Total)
	assert.Equal(t, []string{"Frieren/sample.mkv"}, dropGroup(t, res.Dropped, DropTooSmall).Samples)
	assert.Equal(t, []string{"a/b/c/d"}, dropGroup(t, res.Dropped, DropTooDeep).Samples)
}

// 噪声名与非媒体扩展名【不算】丢弃：报出来只会训练用户忽略这条横幅，
// 而横幅一旦被忽略，真正的软链丢弃也跟着被忽略。
func TestScanDirDoesNotReportNoiseAsDropped(t *testing.T) {
	root := t.TempDir()
	writeFileSized(t, filepath.Join(root, "01.mkv"), minVideoSize)
	writeFileSized(t, filepath.Join(root, ".DS_Store"), 10)
	writeFileSized(t, filepath.Join(root, "Thumbs.db"), 10)
	writeFileSized(t, filepath.Join(root, "._01.mkv"), minVideoSize)
	writeFileSized(t, filepath.Join(root, "notes.txt"), 10)
	writeFileSized(t, filepath.Join(root, "cover.jpg"), 10)

	res, err := ScanDir(root)
	require.NoError(t, err)
	assert.Equal(t, []string{"video:01.mkv"}, relPaths(res.Files))
	assert.Equal(t, 0, res.Dropped.Total, "实际丢弃分组：%v", res.Dropped.Groups)
	assert.Empty(t, res.Dropped.Groups)
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

	res, err := ScanDir(root)
	require.NoError(t, err)

	byRel := map[string]ScannedFile{}
	for _, f := range res.Files {
		byRel[f.RelPath] = f
	}

	bundle, ok := byRel["episode01.mp4"]
	require.True(t, ok, "包目录应以目录路径产出，实际: %v", relPaths(res.Files))
	assert.Equal(t, inner, bundle.AbsPath)
	assert.Equal(t, "video", bundle.Kind)

	assert.Contains(t, byRel, "[29-38].mp4/29.mp4")
	assert.Contains(t, byRel, "[29-38].mp4/30.mp4")

	// 包没被采纳时走的是普通递归，每个条目都会被重新判断一遍。
	// 若采纳判断里也记丢弃，这两个文件会既进库又被记成「跳过」。
	assert.Equal(t, 0, res.Dropped.Total, "实际丢弃分组：%v", res.Dropped.Groups)
}

// 包目录【被采纳】时，它内部被跳过的条目是真的丢了 —— 那时才记。
func TestScanDirExfatBundleReportsSkippedInnerFiles(t *testing.T) {
	root := t.TempDir()
	writeFileSized(t, filepath.Join(root, "episode01.mp4", "stream.mp4"), minVideoSize)
	writeFileSized(t, filepath.Join(root, "episode01.mp4", "preview.mp4"), 1024) // 太小，不算候选

	res, err := ScanDir(root)
	require.NoError(t, err)
	require.Equal(t, []string{"video:episode01.mp4"}, relPaths(res.Files))
	assert.Equal(t, map[DropReason]int{DropTooSmall: 1}, dropCounts(res.Dropped))
	assert.Equal(t, []string{"episode01.mp4/preview.mp4"},
		dropGroup(t, res.Dropped, DropTooSmall).Samples)
}

// macOS 文件系统给出的 NFD 文件名必须归一成 NFC，否则同一部番的
// CJK 目录名会因编码形态不同而分裂成两个组。
func TestScanDirNormalizesNFC(t *testing.T) {
	root := t.TempDir()
	nfdName := norm.NFD.String("ポケモン")
	writeFileSized(t, filepath.Join(root, nfdName, "01.mkv"), minVideoSize)

	res, err := ScanDir(root)
	require.NoError(t, err)
	require.Len(t, res.Files, 1)
	assert.Equal(t, norm.NFC.String("ポケモン")+"/01.mkv", res.Files[0].RelPath)
	assert.True(t, strings.HasPrefix(res.Files[0].RelPath, "ポケモン/"))
	assert.Equal(t, 0, res.Dropped.Total)
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

	res, err := ScanDir(root)
	require.NoError(t, err)
	got := relPaths(res.Files)
	assert.Equal(t, []string{"video:Show/[Sub] Show - 01.mkv"}, got, "软链字幕与软链目录都应被跳过")

	// 跳过是对的，静默跳过不是。两条都要能报到用户面前。
	g := dropGroup(t, res.Dropped, DropSymlink)
	assert.Equal(t, 2, g.Count)
	assert.ElementsMatch(t, []string{"Show/[Sub] Show - 01.ass", "linkdir"}, g.Samples)
	assert.NotEmpty(t, g.UserMsg)
	assert.NotEmpty(t, g.Recovery, "只说「跳过了」没用，必须给一个用户能做的动作")
}

// 树内的软链子目录是真实场景里最贵的一种丢弃：
// `~/Anime/Season1 -> /Volumes/外置盘/Season1` 会让一整季静默消失。
//
// 顺带钉住一条【曾经被当成事实、实测不成立】的说法：「库根本身是软链会扫成空库」。
// scan.go 用的是 os.Stat（跟随软链），软链库根照常工作 —— 别再照着那个错前提改代码。
func TestScanDirInTreeSymlinkedDirIsReportedNotSilent(t *testing.T) {
	real := t.TempDir()
	writeFileSized(t, filepath.Join(real, "Season1", "01.mkv"), minVideoSize)

	root := t.TempDir()
	writeFileSized(t, filepath.Join(root, "Show", "01.mkv"), minVideoSize)
	require.NoError(t, os.Symlink(filepath.Join(real, "Season1"), filepath.Join(root, "Season1")))

	res, err := ScanDir(root)
	require.NoError(t, err)
	assert.Equal(t, []string{"video:Show/01.mkv"}, relPaths(res.Files))

	g := dropGroup(t, res.Dropped, DropSymlink)
	assert.Equal(t, 1, g.Count)
	assert.Equal(t, []string{"Season1"}, g.Samples)
	assert.Contains(t, g.Recovery, "真实路径", "恢复动作要说清用户该怎么把这一季找回来")

	// 场景 A：库根【本身】是软链 —— 正常扫描，不是丢弃。
	linkedRoot := filepath.Join(t.TempDir(), "link-to-lib")
	require.NoError(t, os.Symlink(root, linkedRoot))
	viaLink, err := ScanDir(linkedRoot)
	require.NoError(t, err)
	assert.Equal(t, []string{"video:Show/01.mkv"}, relPaths(viaLink.Files))
}

// 子目录读不了：跳过该子树是对的（一个坏目录不该让整个库空掉），
// 但必须报出来 —— 原来这里只有一条 log.Printf，用户永远看不到。
func TestScanDirReportsUnreadableDir(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root 不受目录权限限制")
	}
	root := t.TempDir()
	writeFileSized(t, filepath.Join(root, "Show", "01.mkv"), minVideoSize)
	locked := filepath.Join(root, "Locked")
	writeFileSized(t, filepath.Join(locked, "02.mkv"), minVideoSize)
	require.NoError(t, os.Chmod(locked, 0o000))
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })

	res, err := ScanDir(root)
	require.NoError(t, err)
	assert.Equal(t, []string{"video:Show/01.mkv"}, relPaths(res.Files))

	g := dropGroup(t, res.Dropped, DropUnreadableDir)
	assert.Equal(t, 1, g.Count)
	assert.Equal(t, []string{"Locked"}, g.Samples)
}

// 单个文件 stat 失败：0400 = 可读不可搜索，ReadDir 列得出名字，
// 对子项 lstat 才 EACCES。用 0000 走的是另一条分支（ReadDir 直接失败）。
func TestScanDirReportsStatFailure(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root 不受目录权限限制")
	}
	root := t.TempDir()
	locked := filepath.Join(root, "Locked")
	writeFileSized(t, filepath.Join(locked, "01.mkv"), minVideoSize)
	require.NoError(t, os.Chmod(locked, 0o400))
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })

	// 不同文件系统对 d_type 的支持不一样：拿不到 d_type 时 ReadDir 自己会去
	// lstat 并整个失败，那条路径由上面的 unreadable-dir 用例覆盖。
	entries, err := os.ReadDir(locked)
	if err != nil || len(entries) == 0 {
		t.Skip("这个文件系统上 0400 目录连 ReadDir 都过不去")
	}
	if _, err := entries[0].Info(); err == nil {
		t.Skip("这个环境下 0400 目录仍能 stat 子项")
	}

	res, err := ScanDir(root)
	require.NoError(t, err)
	assert.Empty(t, res.Files)

	g := dropGroup(t, res.Dropped, DropStatFailed)
	assert.Equal(t, 1, g.Count)
	assert.Equal(t, []string{"Locked/01.mkv"}, g.Samples)
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
	head := make([]byte, Hash16MBytes)
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

// Hash16MFrom：与路径版共用同一份取样长度与算法 —— 磁力侧拿种子 reader
// 调它，两条来源不能算出两个哈希。
func TestHash16MFrom(t *testing.T) {
	// 不足 16MB：对读到的全部内容求值。
	small := []byte("nagare")
	sum := md5.Sum(small)
	got, err := Hash16MFrom(bytes.NewReader(small))
	require.NoError(t, err)
	assert.Equal(t, hex.EncodeToString(sum[:]), got)

	// 超过 16MB：只取前 16MB，尾巴不参与。
	head := make([]byte, Hash16MBytes)
	for i := range head {
		head[i] = byte(i % 251)
	}
	headSum := md5.Sum(head)
	got, err = Hash16MFrom(bytes.NewReader(append(append([]byte{}, head...), []byte("tail-ignored")...)))
	require.NoError(t, err)
	assert.Equal(t, hex.EncodeToString(headSum[:]), got)

	// 与路径版对同一份内容结果一致。
	path := filepath.Join(t.TempDir(), "same.bin")
	require.NoError(t, os.WriteFile(path, small, 0o644))
	viaPath, err := Hash16M(path)
	require.NoError(t, err)
	viaReader, err := Hash16MFrom(bytes.NewReader(small))
	require.NoError(t, err)
	assert.Equal(t, viaPath, viaReader)
}
