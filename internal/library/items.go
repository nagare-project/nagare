// EpisodeItem 构造 —— 复刻 animego useVideoFiles.processFiles 的字段派生规则。
package library

import "sort"

// Item 对应 animego 的 EpisodeItem（内存态剧集条目）。指针字段 nil = 未识别。
type Item struct {
	// FileID 是软 id：`name|size|mtime`（决议 P1：hash 懒算，首播前才补 16MB hash）。
	FileID       string
	FileName     string
	RelativePath string
	// AbsPath 是 nagare 扩展：本地绝对路径。语料/纯解析场景为空。
	AbsPath string
	Size    int64
	MTimeMs int64

	Episode          *int
	ParsedTitle      *string
	ParsedNumber     *int
	ParsedKind       string
	ParsedGroup      *string
	ParsedResolution *string
	ParsedSeason     *int
	ParsedEpisodeAlt *int
}

// SourceFile 是扫描层交给解析层的原始文件描述。
type SourceFile struct {
	RelPath string // 相对库根，`/` 分隔
	AbsPath string
	Size    int64
	MTimeMs int64
}

// episodeSortKey 复刻 JS 的 `(a.episode ?? 999)` 排序哨兵。
const episodeSortSentinel = 999

// BuildItems 把源文件转成已解析、按集号排序的 Item 列表（非视频文件被过滤）。
//
// 字段派生与 useVideoFiles 逐行对齐：
//   - episode 取 ParseEpisodeNumber(文件名)
//   - parsedTitle：路径多于一段时【目录首段的标题优先】于文件名标题
//   - 软 id = name|size|mtime
func BuildItems(files []SourceFile) []Item {
	items := make([]Item, 0, len(files))
	for _, f := range files {
		name := baseName(f.RelPath)
		if !IsVideoFile(name) {
			continue
		}
		meta := ParseEpisodeMeta(name)
		title := meta.Title
		if segs := splitPath(f.RelPath); len(segs) > 1 {
			if ft := ParseAnimeKeyword(segs[0]); ft != "" {
				title = &ft
			}
		}
		items = append(items, Item{
			FileID:           softID(name, f.Size, f.MTimeMs),
			FileName:         name,
			RelativePath:     f.RelPath,
			AbsPath:          f.AbsPath,
			Size:             f.Size,
			MTimeMs:          f.MTimeMs,
			Episode:          ParseEpisodeNumber(name),
			ParsedTitle:      title,
			ParsedNumber:     meta.Number,
			ParsedKind:       meta.Kind,
			ParsedGroup:      meta.Group,
			ParsedResolution: meta.Resolution,
			ParsedSeason:     meta.Season,
			ParsedEpisodeAlt: meta.EpisodeAlt,
		})
	}
	sort.SliceStable(items, func(i, j int) bool {
		return epOrSentinel(items[i].Episode) < epOrSentinel(items[j].Episode)
	})
	return items
}

func epOrSentinel(ep *int) int {
	if ep == nil {
		return episodeSortSentinel
	}
	return *ep
}

func softID(name string, size, mtimeMs int64) string {
	return name + "|" + itoa64(size) + "|" + itoa64(mtimeMs)
}

func itoa64(n int64) string {
	// 手写避免到处 strconv.FormatInt 的样板；语义等价。
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// splitPath 按 `/` 切路径并去掉空段（与 JS split('/').filter(Boolean) 对齐）。
func splitPath(p string) []string {
	var segs []string
	start := 0
	for i := 0; i <= len(p); i++ {
		if i == len(p) || p[i] == '/' {
			if i > start {
				segs = append(segs, p[start:i])
			}
			start = i + 1
		}
	}
	return segs
}

func baseName(p string) string {
	segs := splitPath(p)
	if len(segs) == 0 {
		return ""
	}
	return segs[len(segs)-1]
}
