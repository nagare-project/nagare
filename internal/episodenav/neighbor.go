// Package episodenav 在同一部作品的文件里挑「上一集 / 下一集」。
//
// 只读 library.Item 的解析结果，不改解析链本身（internal/library 是与网页端共享语料的
// 移植，决议 CQ2：不在那里加东西）。媒体库与磁力包都用这一条规则，两边挑出来的邻居才一致。
package episodenav

import (
	"path"
	"path/filepath"

	"github.com/nagare-project/nagare/internal/library"
)

// mainKind 是正片的 ParsedKind；SP / OVA / 剧场版 / 特典不参与上一集下一集。
const mainKind = "main"

// Pick 在 cands 里为 cur 挑 dir 方向的邻居集，返回它在 cands 里的下标。
// dir < 0 是上一集，dir > 0 是下一集，0 什么都不挑。
//
// 规则：
//   - cur 自己必须是认出集号的正片，否则没有邻居（不知道自己是第几集，就无从谈「下一集」）；
//   - 候选只看认出集号的正片，跳过 cur 本身（按 FileID：另一个库目录里的同一份副本也跳过）；
//   - 下一集 = 集号比 cur 大的里面最小的，上一集 = 比 cur 小的里面最大的；集号可以有空缺
//     （第 4 集后面直接是第 6 集），界面上照实显示真实集号；
//   - 同一集有好几个版本时，依次偏好：与 cur 同一个目录 > 同一个字幕组 > 同一个分辨率 >
//     文件名自然序（library.CompareFileNames）。
func Pick(cur library.Item, cands []library.Item, dir int) (int, bool) {
	curEp, ok := mainEpisode(cur)
	if !ok || dir == 0 {
		return -1, false
	}
	best := -1
	bestEp := 0
	for i, c := range cands {
		ep, ok := mainEpisode(c)
		if !ok || c.FileID == cur.FileID || !beyond(ep, curEp, dir) {
			continue
		}
		switch {
		case best < 0 || closer(ep, bestEp, dir):
			best, bestEp = i, ep
		case ep == bestEp && preferred(cur, c, cands[best]):
			best = i
		}
	}
	return best, best >= 0
}

// mainEpisode 返回正片的集号；不是正片或认不出集号时 ok 为 false。
func mainEpisode(it library.Item) (int, bool) {
	if it.ParsedKind != mainKind || it.Episode == nil || *it.Episode <= 0 {
		return 0, false
	}
	return *it.Episode, true
}

// beyond：ep 在 cur 的 dir 方向上。
func beyond(ep, cur, dir int) bool {
	if dir > 0 {
		return ep > cur
	}
	return ep < cur
}

// closer：ep 比 best 更贴近当前这一集（下一集要更小、上一集要更大）。
func closer(ep, best, dir int) bool {
	if dir > 0 {
		return ep < best
	}
	return ep > best
}

// preferred 报告同一集的两个版本里 a 是否比 b 更合适接在 cur 后面播。
func preferred(cur, a, b library.Item) bool {
	if sa, sb := dirOf(a) == dirOf(cur), dirOf(b) == dirOf(cur); sa != sb {
		return sa
	}
	if sa, sb := samePtr(a.ParsedGroup, cur.ParsedGroup), samePtr(b.ParsedGroup, cur.ParsedGroup); sa != sb {
		return sa
	}
	if sa, sb := samePtr(a.ParsedResolution, cur.ParsedResolution), samePtr(b.ParsedResolution, cur.ParsedResolution); sa != sb {
		return sa
	}
	if c := library.CompareFileNames(a.FileName, b.FileName); c != 0 {
		return c < 0
	}
	// 文件名也一样（两个库目录里的同名文件）：按路径定个先后，结果不随候选顺序变
	return a.AbsPath+"\x00"+a.RelativePath < b.AbsPath+"\x00"+b.RelativePath
}

// dirOf 是文件所在的目录：有绝对路径用它，否则用库内相对路径（`/` 分隔）。
func dirOf(it library.Item) string {
	if it.AbsPath != "" {
		return filepath.Dir(it.AbsPath)
	}
	return path.Dir(it.RelativePath)
}

// samePtr：两边都认出来了而且相同（都认不出来不算「相同」）。
func samePtr(a, b *string) bool {
	return a != nil && b != nil && *a == *b
}
