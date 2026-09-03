// 选集：从种子内的文件里挑出要播的那一集。
//
// 复用 internal/library 的解析链（决议 CQ2）：本地库与磁力用同一条链解析文件名，
// 下游的弹幕匹配、进度回写、看完同步因此完全不必区分来源，也不会因为两套启发式
// 而把同一个番分成两条剧集线。
package torrentstream

import (
	"math"
	"sort"

	errs "github.com/nagare-project/nagare/internal/errors"
	"github.com/nagare-project/nagare/internal/library"
)

// maxTorrentFiles 是选集阶段接受的文件数上限。
//
// 正常发布的合集在几十到几百个文件之间。种子元数据本身有 16MB 上限，所以文件数
// 并非无界，但每个文件名都要跑一遍解析链，而这一步是【握着 prepMu 同步做】的 ——
// 不设上限，一份畸形种子就能让「点了播放没反应」持续可观的时间，而那期间用户
// 连换一条资源都做不到。
const maxTorrentFiles = 4096

// extraKinds 是要从候选里剔除的花絮类型。
//
// 保留 main / sp / ova / movie / unknown：剧场版和 SP 是用户可能真想看的，
// unknown 是「解析不出类型」的兜底 —— 解析不出不等于不是正片，不能替用户丢掉。
var extraKinds = map[string]struct{}{
	"ncop": {}, "nced": {}, "menu": {}, "bonus": {}, "trailer": {},
	"interview": {}, "wp": {}, "cm": {}, "pv": {}, "commentary": {},
}

func isExtraKind(kind string) bool {
	_, ok := extraKinds[kind]
	return ok
}

// fileEntry 是选集逻辑对种子内文件的全部需求。
// 不直接吃 *torrent.File：这样表驱动测试不必构造真实种子。
type fileEntry struct {
	Index int
	Path  string // 种子内路径，'/' 分隔
	Size  int64
}

// candidate 是一个可播放的候选（已过解析链、已排除花絮）。
type candidate struct {
	index int
	item  library.Item
}

// selection 是选集结果。Need 为真时 Index/Item 无意义，改交用户手选。
type selection struct {
	Index int
	Item  library.Item
	Files []FileChoice
	Need  bool
}

// selectFile 按「过滤 → 唯一 → 集号命中 → 交给用户」四级决定播哪个文件。
func selectFile(entries []fileEntry, req PrepareRequest) (selection, error) {
	if len(entries) > maxTorrentFiles {
		return selection{}, errs.New(errs.CategoryInput, "torrentstream.select",
			"这条资源的文件数异常，可能不是正常的影视发布", "换一条资源试试")
	}
	cands, videoCount := buildCandidates(entries)
	if videoCount == 0 {
		return selection{}, errs.New(errs.CategoryInput, "torrentstream.select",
			"这条资源里没有可播放的视频文件", "换一条资源试试")
	}
	if len(cands) == 0 {
		// 有视频但全被判成花絮。与「压根没有视频」是两种失败，
		// 用户的下一步动作也不同，不能合并成一条提示。
		return selection{}, errs.New(errs.CategoryInput, "torrentstream.select",
			"这条资源里只有 OP/ED/特典之类的花絮", "换一条包含正片的资源")
	}

	// 用户已经手选过：只接受候选列表里给出过的下标。
	if req.FileIndex >= 0 {
		for _, c := range cands {
			if c.index == req.FileIndex {
				return selection{Index: c.index, Item: c.item}, nil
			}
		}
		return selection{}, errs.New(errs.CategoryInput, "torrentstream.select",
			"选中的文件不在这条资源里", "重新打开选集列表再选一次")
	}

	if len(cands) == 1 {
		return selection{Index: cands[0].index, Item: cands[0].item}, nil
	}

	// 集号命中且【唯一】才自动选。多个命中说明种子里同一集有多个版本
	// （不同分辨率/字幕组），替用户猜一个不如让用户自己挑。
	if hint := episodeHint(req); hint > 0 {
		var matched []candidate
		for _, c := range cands {
			if c.item.Episode != nil && *c.item.Episode == hint {
				matched = append(matched, c)
			}
		}
		if len(matched) == 1 {
			return selection{Index: matched[0].index, Item: matched[0].item}, nil
		}
	}

	return selection{Need: true, Files: toChoices(cands)}, nil
}

// buildCandidates 走解析链，返回排除花絮后的候选与「视频文件总数」。
// 两个返回值分开是为了区分「没有视频」与「视频全是花絮」这两种失败。
func buildCandidates(entries []fileEntry) (cands []candidate, videoCount int) {
	srcs := make([]library.SourceFile, 0, len(entries))
	indexByPath := make(map[string]int, len(entries))
	for _, e := range entries {
		srcs = append(srcs, library.SourceFile{RelPath: e.Path, Size: e.Size})
		indexByPath[e.Path] = e.Index
	}
	// BuildItems 顺带做掉非视频过滤、集号/标题/字幕组派生，并按集号排序
	// （无集号排最后，同集号保持种子内原有顺序）。
	items := library.BuildItems(srcs)
	videoCount = len(items)

	cands = make([]candidate, 0, len(items))
	for _, it := range items {
		if isExtraKind(it.ParsedKind) {
			continue
		}
		idx, ok := indexByPath[it.RelativePath]
		if !ok {
			continue
		}
		cands = append(cands, candidate{index: idx, item: it})
	}
	// BuildItems 只按集号排序，同集号那一档保持种子内的原始顺序。合集里同集号
	// 有多个文件是常事（多分辨率、多语种、简繁双版本），那时种子内顺序毫无意义，
	// 再按文件名自然序定一次，选集弹窗里的次序才稳定且符合直觉。
	sort.SliceStable(cands, func(i, j int) bool {
		ei, ej := episodeOrLast(cands[i].item.Episode), episodeOrLast(cands[j].item.Episode)
		if ei != ej {
			return ei < ej
		}
		return library.CompareFileNames(cands[i].item.FileName, cands[j].item.FileName) < 0
	})
	return cands, videoCount
}

// episodeOrLast 把「没有集号」排到最后（与 library.BuildItems 的哨兵语义一致）。
func episodeOrLast(ep *int) int {
	if ep == nil {
		return math.MaxInt
	}
	return *ep
}

// episodeHint 取调用方给的集号；没给就从搜索结果标题里派生。
func episodeHint(req PrepareRequest) int {
	if req.EpisodeHint > 0 {
		return req.EpisodeHint
	}
	if req.Title == "" {
		return 0
	}
	if n := library.ParseEpisodeNumber(req.Title); n != nil {
		return *n
	}
	return 0
}

func toChoices(cands []candidate) []FileChoice {
	out := make([]FileChoice, 0, len(cands))
	for _, c := range cands {
		out = append(out, FileChoice{
			Index:   c.index,
			Name:    c.item.FileName,
			Path:    c.item.RelativePath,
			Size:    c.item.Size,
			Episode: c.item.Episode,
		})
	}
	return out
}
