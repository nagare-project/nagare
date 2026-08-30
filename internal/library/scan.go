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

// ScanDir 枚举 root 下的视频与字幕文件：
//   - 跳过 ._* / .DS_Store / Thumbs.db / desktop.ini
//   - 视频小于 1MB 跳过
//   - 深度 0/1 处「目录名带视频扩展名」按 macOS ExFAT 包目录处理：
//     内部恰有一个同扩展名的大文件 → 以目录路径产出该文件；多个 → 当普通目录递归
//   - 深度上限 3；relPath 统一 NFC（macOS 文件系统给的是 NFD，CJK 必须归一）
//
// 条目按目录项字典序遍历（os.ReadDir 保证），产出确定性。
func ScanDir(root string) ([]ScannedFile, error) {
	info, err := os.Stat(root)
	if err != nil {
		return nil, fmt.Errorf("访问目录 %s: %w", root, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%s 不是目录", root)
	}
	var out []ScannedFile
	// 根目录读失败是硬错误（整个库目录没了/没权限）；子目录读失败只跳过该子树。
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("读取目录 %s: %w", root, err)
	}
	walkEntries(root, "", 0, entries, &out)
	return out, nil
}

// walkScan 递归子目录：读失败只跳过该子树并记日志，不毁掉整次扫描
// （与下面单文件 stat 失败的处理一致——一个坏子目录不该让整个库目录空掉）。
func walkScan(dir, prefix string, depth int, out *[]ScannedFile) {
	if depth > maxScanDepth {
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		log.Printf("library: 跳过无法读取的子目录 %s：%v", dir, err)
		return
	}
	walkEntries(dir, prefix, depth, entries, out)
}

// walkEntries 处理一个已读出的目录项列表（根与子目录共用）。
func walkEntries(dir, prefix string, depth int, entries []os.DirEntry, out *[]ScannedFile) {
	for _, entry := range entries {
		name := entry.Name()
		if isNoiseName(name) {
			continue
		}
		// 跳过符号链接：恶意压缩包可放一个指向 ~/.ssh/id_rsa 的 .ass 软链，
		// 播放时 mpv 会顺着链接打开目标当字幕渲染。字幕没有 1MB 下限兜底，
		// 只能在扫描处一刀切掉（正规发布包内部不用软链）。
		if entry.Type()&fs.ModeSymlink != 0 {
			continue
		}
		entryRel := name
		if prefix != "" {
			entryRel = prefix + "/" + name
		}
		entryRel = norm.NFC.String(entryRel)
		entryAbs := filepath.Join(dir, name)

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
				continue
			}
			if isVid && fi.Size() < minVideoSize {
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
			if picked := pickLargestSameExt(entryAbs, fileExt(name)); picked != nil {
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
			walkScan(entryAbs, entryRel, depth+1, out)
		}
	}
}

type pickedBundle struct {
	abs     string
	size    int64
	mtimeMs int64
}

// pickLargestSameExt：包目录内恰有一个 ≥1MB 的同扩展名子文件时返回之，否则 nil。
func pickLargestSameExt(dirAbs, targetExt string) *pickedBundle {
	entries, err := os.ReadDir(dirAbs)
	if err != nil {
		return nil
	}
	var candidates []pickedBundle
	for _, e := range entries {
		if e.IsDir() || e.Type()&fs.ModeSymlink != 0 || isNoiseName(e.Name()) || fileExt(e.Name()) != targetExt {
			continue
		}
		if e.Type()&os.ModeSymlink != 0 {
			continue // 与主扫描同一条软链红线
		}
		fi, err := e.Info()
		if err != nil || fi.Size() < minVideoSize {
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
