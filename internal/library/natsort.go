// localeCompare 的近似实现。
//
// JS 侧排序用 `a.localeCompare(b, undefined, { numeric: true })`（文件名）与
// 无参 `localeCompare`（groupKey）。完整 ICU 排序无法在 Go 里零依赖复刻，
// 这里实现工程近似：主序按小写逐符比较、numeric 模式下数字串按数值比较、
// 全等时小写优先（ICU en 的表现）。与 ICU 的偏差只影响展示顺序，
// 不影响任何 id/键的生成；差分语料（parse.jsonl）覆盖到的排序场景全部对齐。
package library

import (
	"strings"
	"unicode"
)

// localeCompare 返回 -1/0/1。numeric 为 true 时连续数字按数值比较。
func localeCompare(a, b string, numeric bool) int {
	ra, rb := []rune(a), []rune(b)
	ia, ib := 0, 0
	for ia < len(ra) && ib < len(rb) {
		ca, cb := ra[ia], rb[ib]
		if numeric && unicode.IsDigit(ca) && unicode.IsDigit(cb) {
			// 提取两侧完整数字串按数值比较（等值时位数少的在前——"1" < "01"）。
			sa, ea := ia, ia
			for ea < len(ra) && unicode.IsDigit(ra[ea]) {
				ea++
			}
			sb, eb := ib, ib
			for eb < len(rb) && unicode.IsDigit(rb[eb]) {
				eb++
			}
			na := strings.TrimLeft(string(ra[sa:ea]), "0")
			nb := strings.TrimLeft(string(rb[sb:eb]), "0")
			if len(na) != len(nb) {
				if len(na) < len(nb) {
					return -1
				}
				return 1
			}
			if na != nb {
				if na < nb {
					return -1
				}
				return 1
			}
			if ea-sa != eb-sb {
				if ea-sa < eb-sb {
					return -1
				}
				return 1
			}
			ia, ib = ea, eb
			continue
		}
		la, lb := unicode.ToLower(ca), unicode.ToLower(cb)
		if la != lb {
			if la < lb {
				return -1
			}
			return 1
		}
		if ca != cb {
			// 主序相同、大小写不同：小写在前（对齐 ICU en 默认表现）。
			if unicode.IsLower(ca) && unicode.IsUpper(cb) {
				return -1
			}
			if unicode.IsUpper(ca) && unicode.IsLower(cb) {
				return 1
			}
			if ca < cb {
				return -1
			}
			return 1
		}
		ia++
		ib++
	}
	switch {
	case ia < len(ra):
		return 1
	case ib < len(rb):
		return -1
	default:
		return 0
	}
}
