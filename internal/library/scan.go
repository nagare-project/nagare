// 本地目录扫描 —— 复刻 animego enumerator.js 的 v3.1 Stage 0 规则集（决议 P1：
// 扫描只 stat + 解析文件名，不读内容；16MB hash 延到首次播放前）。
package library

import (
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/text/unicode/norm"
)

// minVideoSize：小于 1MB 的视频文件按噪声跳过（字幕不受限）。
const minVideoSize = 1 * 1024 * 1024

// maxScanDepth：目录递归深度上限。
const maxScanDepth = 3

var scanVideoExts = map[string]struct{}{
	"mkv": {}, "mp4": {}, "avi": {}, "webm": {}, "mov": {},
	"m4v": {}, "flv": {}, "wmv": {}, "ts": {}, "rmvb": {},
}

var scanSubtitleExts = map[string]struct{}{
	"srt": {}, "ass": {}, "ssa": {}, "vtt": {}, "sup": {},
}

var noiseNames = map[string]struct{}{
	".DS_Store": {}, "Thumbs.db": {}, "desktop.ini": {},
}

// ScannedFile 是扫描产物：视频或字幕文件的元信息（不含内容读取）。
type ScannedFile struct {
	RelPath string // 相对库根，`/` 分隔，NFC 归一化
	AbsPath string
	Size    int64
	MTimeMs int64
	Kind    string // "video" | "subtitle"
}

func fileExt(name string) string {
	idx := strings.LastIndex(name, ".")
	if idx < 0 {
		return ""
	}
	return strings.ToLower(name[idx+1:])
}

// isNoiseName：macOS AppleDouble（._*）与已知系统残留文件。
func isNoiseName(name string) bool {
	if strings.HasPrefix(name, "._") {
		return true
	}
	_, ok := noiseNames[name]
	return ok
}

// ScanResult 是一次扫描的完整产物：进库的东西，和没进库的东西。
//
// Dropped 与 Files 同等重要。只返回 Files 的话，「我那一集怎么不在库里」
// 在界面上就永远无解——用户看得到的只有一个短了几条的列表。
type ScanResult struct {
	Files   []ScannedFile
	Dropped DropSummary
}

// ScanDir 枚举 root 下的视频与字幕文件：
//   - 跳过 ._* / .DS_Store / Thumbs.db / desktop.ini
//   - 视频小于 1MB 跳过
//   - 深度 0/1 处「目录名带视频扩展名」按 macOS ExFAT 包目录处理：
//     内部恰有一个同扩展名的大文件 → 以目录路径产出该文件；多个 → 当普通目录递归
//   - 深度上限 3；relPath 统一 NFC（macOS 文件系统给的是 NFD，CJK 必须归一）
//
// 被跳过的东西按原因汇总进 ScanResult.Dropped（噪声名与非媒体扩展名除外，
// 理由见 drops.go）。
//
// 条目按目录项字典序遍历（os.ReadDir 保证），产出确定性。
func ScanDir(root string) (ScanResult, error) {
	info, err := os.Stat(root)
	if err != nil {
		return ScanResult{}, fmt.Errorf("访问目录 %s: %w", root, err)
	}
	if !info.IsDir() {
		return ScanResult{}, fmt.Errorf("%s 不是目录", root)
	}
	var out []ScannedFile
	// 根目录读失败是硬错误（整个库目录没了/没权限）；子目录读失败只跳过该子树。
	entries, err := os.ReadDir(root)
	if err != nil {
		return ScanResult{}, fmt.Errorf("读取目录 %s: %w", root, err)
	}
	drops := newDropCollector()
	walkEntries(root, "", 0, entries, &out, drops)
	return ScanResult{Files: out, Dropped: drops.summary()}, nil
}

// walkScan 递归子目录：读失败只跳过该子树并记日志，不毁掉整次扫描
// （与下面单文件 stat 失败的处理一致——一个坏子目录不该让整个库目录空掉）。
//
// drops 与 out 一样按指针往下传：丢弃发生在递归的每一层，收集器只穿到
// walkEntries 会漏掉这个函数里的两条早退。
func walkScan(dir, prefix string, depth int, out *[]ScannedFile, drops *dropCollector) {
	// 防御性上限。调用方已经只在 depth < maxScanDepth 时才递归，所以正常走不到
	// （深度丢弃实际发生在 walkEntries 末尾那个 else）。真走到了说明有人加了一条
	// 绕过那道闸的递归路径——那时也要记一笔，别让新路径变成新的静默丢弃。
	if depth > maxScanDepth {
		drops.add(DropTooDeep, prefix)
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		log.Printf("library: 跳过无法读取的子目录 %s：%v", dir, err)
		drops.add(DropUnreadableDir, prefix)
		return
	}
	walkEntries(dir, prefix, depth, entries, out, drops)
}

// walkEntries 处理一个已读出的目录项列表（根与子目录共用）。
func walkEntries(dir, prefix string, depth int, entries []os.DirEntry, out *[]ScannedFile, drops *dropCollector) {
	for _, entry := range entries {
		name := entry.Name()
		if isNoiseName(name) {
			continue
		}
		// relPath 统一 NFC：macOS 文件系统给的是 NFD，CJK 必须归一，
		// 否则同一部番的目录名会因编码形态不同而分裂成两个组。
		entryRel := name
		if prefix != "" {
			entryRel = prefix + "/" + name
		}
		entryRel = norm.NFC.String(entryRel)
		entryAbs := filepath.Join(dir, name)

		// 跳过符号链接：恶意压缩包可放一个指向 ~/.ssh/id_rsa 的 .ass 软链，
		// 播放时 mpv 会顺着链接打开目标当字幕渲染。字幕没有 1MB 下限兜底，
		// 只能在扫描处一刀切掉（正规发布包内部不用软链）。
		//
		// 不按扩展名筛选要不要记这一笔：不跟随就意味着不知道它指向文件还是目录，
		// 而「指向目录」正是一条软链吃掉整季的那种情况。
		if entry.Type()&fs.ModeSymlink != 0 {
			drops.add(DropSymlink, entryRel)
			continue
		}

		if !entry.IsDir() {
			ext := fileExt(name)
			_, isVid := scanVideoExts[ext]
			_, isSub := scanSubtitleExts[ext]
			if !isVid && !isSub {
				continue
			}
			fi, err := entry.Info()
			if err != nil {
				// 单个文件 stat 失败（竞态删除等）不该毁掉整次扫描，跳过即可。
				drops.add(DropStatFailed, entryRel)
				continue
			}
			if isVid && fi.Size() < minVideoSize {
				drops.add(DropTooSmall, entryRel)
				continue
			}
			kind := "video"
			if isSub {
				kind = "subtitle"
			}
			*out = append(*out, ScannedFile{
				RelPath: entryRel,
				AbsPath: entryAbs,
				Size:    fi.Size(),
				MTimeMs: fi.ModTime().UnixMilli(),
				Kind:    kind,
			})
			continue
		}

		// 目录：先看 ExFAT 包目录（目录名本身带视频扩展名）。
		if _, isVidDir := scanVideoExts[fileExt(name)]; isVidDir && depth <= 1 {
			// 包内被跳过的条目先记在一个局部收集器里，只有这个包【被采纳】
			// 才并进正式结果：没采纳会走下面的普通递归把每个条目重新判断一遍，
			// 那时再记就是重复计数。
			inner := newDropCollector()
			if picked := pickLargestSameExt(entryAbs, entryRel, fileExt(name), inner); picked != nil {
				drops.merge(inner)
				*out = append(*out, ScannedFile{
					RelPath: entryRel, // 以包目录的路径示人，内部真实文件走 AbsPath
					AbsPath: picked.abs,
					Size:    picked.size,
					MTimeMs: picked.mtimeMs,
					Kind:    "video",
				})
				continue
			}
			// 零个或多个同扩展名大文件 → 用户自建目录，走普通递归。
		}

		if depth < maxScanDepth {
			walkScan(entryAbs, entryRel, depth+1, out, drops)
			continue
		}
		// 深度上限。丢的是一整棵子树而不是一个文件，所以按【目录】计一笔，
		// 界面文案也按目录说。这一条才是深度丢弃的真实发生点——
		// walkScan 开头那个 depth > maxScanDepth 有这道闸在前面，永远走不到。
		drops.add(DropTooDeep, entryRel)
	}
}

type pickedBundle struct {
	abs     string
	size    int64
	mtimeMs int64
}

// pickLargestSameExt：包目录内恰有一个 ≥1MB 的同扩展名子文件时返回之，否则 nil。
//
// dirRel 是包目录相对库根的路径，只用来给 drops 里的样本拼路径。
// 目录本身读不了时直接返回 nil：调用方会退回普通递归，walkScan 会在那里
// 再读一次同一个目录、失败并记 unreadable-dir，这里记等于记两遍。
func pickLargestSameExt(dirAbs, dirRel, targetExt string, drops *dropCollector) *pickedBundle {
	entries, err := os.ReadDir(dirAbs)
	if err != nil {
		return nil
	}
	var candidates []pickedBundle
	for _, e := range entries {
		if e.IsDir() || isNoiseName(e.Name()) || fileExt(e.Name()) != targetExt {
			continue
		}
		rel := norm.NFC.String(dirRel + "/" + e.Name())
		// 与主扫描同一条软链红线（原先这两行判断写了两遍，
		// fs.ModeSymlink 与 os.ModeSymlink 是同一个常量）。
		if e.Type()&fs.ModeSymlink != 0 {
			drops.add(DropSymlink, rel)
			continue
		}
		fi, err := e.Info()
		if err != nil {
			drops.add(DropStatFailed, rel)
			continue
		}
		if fi.Size() < minVideoSize {
			drops.add(DropTooSmall, rel)
			continue
		}
		candidates = append(candidates, pickedBundle{
			abs:     filepath.Join(dirAbs, e.Name()),
			size:    fi.Size(),
			mtimeMs: fi.ModTime().UnixMilli(),
		})
	}
	if len(candidates) != 1 {
		return nil
	}
	return &candidates[0]
}

// SubtitleRef 是与视频条目配对的字幕文件。
type SubtitleRef struct {
	FileName string
	RelPath  string
	AbsPath  string
	Episode  *int
	Type     string // ass/ssa/srt/vtt（sup 等扫描得到但不参与配对）
}

// subTypePriority 与 useVideoFiles 的 SUB_PRIORITY 对齐：ass 优先。
func subTypePriority(t string) int {
	switch t {
	case "ass":
		return 0
	case "ssa":
		return 1
	case "srt":
		return 2
	case "vtt":
		return 3
	default:
		return 9
	}
}

// PairSubtitles 为每个视频条目配字幕（与 useVideoFiles 的规则一致）：
// 有集号 → 同集号字幕按类型优先级取第一；无集号 → 同 basename 匹配。
// 返回 fileID → SubtitleRef。
func PairSubtitles(items []Item, subs []ScannedFile) map[string]SubtitleRef {
	refs := make([]SubtitleRef, 0, len(subs))
	for _, s := range subs {
		name := baseName(s.RelPath)
		t := SubtitleType(name)
		if t == "" {
			continue // sup 等不在配对范围（与网页端行为一致）
		}
		refs = append(refs, SubtitleRef{
			FileName: name,
			RelPath:  s.RelPath,
			AbsPath:  s.AbsPath,
			Episode:  ParseEpisodeNumber(name),
			Type:     t,
		})
	}

	out := map[string]SubtitleRef{}
	for _, it := range items {
		if it.Episode != nil {
			var matched []SubtitleRef
			for _, r := range refs {
				if r.Episode != nil && *r.Episode == *it.Episode {
					matched = append(matched, r)
				}
			}
			if len(matched) > 0 {
				sort.SliceStable(matched, func(i, j int) bool {
					return subTypePriority(matched[i].Type) < subTypePriority(matched[j].Type)
				})
				out[it.FileID] = matched[0]
			}
			continue
		}
		base := stripLastExt(it.FileName)
		for _, r := range refs {
			if stripLastExt(r.FileName) == base {
				out[it.FileID] = r
				break
			}
		}
	}
	return out
}

func stripLastExt(name string) string {
	idx := strings.LastIndex(name, ".")
	if idx < 0 {
		return name
	}
	return name[:idx]
}
