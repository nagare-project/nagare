package episodenav

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/nagare-project/nagare/internal/library"
)

type item = library.Item

func str(s string) *string { return &s }
func intp(n int) *int      { return &n }

// ep 造一个正片条目：abs 是绝对路径，group / res 可空。
func ep(id string, n int, abs string, group, res *string) item {
	return item{FileID: id, FileName: filepath.Base(abs), AbsPath: abs, Episode: &n, ParsedKind: "main", ParsedGroup: group, ParsedResolution: res}
}

func TestPickNeighbours(t *testing.T) {
	cur := ep("c", 5, "/lib/show/Show - 05.mkv", str("Sub"), str("1080p"))
	tests := []struct {
		name   string
		cur    item
		cands  []item
		dir    int
		wantID string
	}{
		{
			name:   "下一集：比当前大的里面最小的",
			cur:    cur,
			cands:  []item{ep("e7", 7, "/lib/show/Show - 07.mkv", nil, nil), ep("e6", 6, "/lib/show/Show - 06.mkv", nil, nil), ep("e4", 4, "/lib/show/Show - 04.mkv", nil, nil)},
			dir:    1,
			wantID: "e6",
		},
		{
			name:   "上一集：比当前小的里面最大的",
			cur:    cur,
			cands:  []item{ep("e2", 2, "/lib/show/Show - 02.mkv", nil, nil), ep("e4", 4, "/lib/show/Show - 04.mkv", nil, nil), ep("e6", 6, "/lib/show/Show - 06.mkv", nil, nil)},
			dir:    -1,
			wantID: "e4",
		},
		{
			name:   "有空缺：第 5 集后面直接是第 8 集",
			cur:    cur,
			cands:  []item{ep("e8", 8, "/lib/show/Show - 08.mkv", nil, nil), ep("e9", 9, "/lib/show/Show - 09.mkv", nil, nil)},
			dir:    1,
			wantID: "e8",
		},
		{
			name: "同一集好几个版本：同目录优先于字幕组",
			cur:  cur,
			cands: []item{
				ep("other-dir", 6, "/disk2/show/Show - 06.mkv", str("Sub"), str("1080p")),
				ep("same-dir", 6, "/lib/show/Show - 06 other.mkv", str("Other"), str("720p")),
			},
			dir:    1,
			wantID: "same-dir",
		},
		{
			name: "同目录都不是：同字幕组优先于分辨率",
			cur:  cur,
			cands: []item{
				ep("res", 6, "/a/Show - 06.mkv", str("Other"), str("1080p")),
				ep("group", 6, "/b/Show - 06.mkv", str("Sub"), str("720p")),
			},
			dir:    1,
			wantID: "group",
		},
		{
			name: "字幕组也分不出：同分辨率优先",
			cur:  cur,
			cands: []item{
				ep("720", 6, "/a/Show - 06.mkv", nil, str("720p")),
				ep("1080", 6, "/b/Show - 06.mkv", nil, str("1080p")),
			},
			dir:    1,
			wantID: "1080",
		},
		{
			name: "全都一样：按文件名自然序",
			cur:  cur,
			cands: []item{
				ep("b", 6, "/x/[B] Show - 06.mkv", nil, nil),
				ep("a", 6, "/y/[A] Show - 06.mkv", nil, nil),
			},
			dir:    1,
			wantID: "a",
		},
		{
			name: "非正片与认不出集号的不算",
			cur:  cur,
			cands: []item{
				{FileID: "sp", FileName: "SP 06.mkv", Episode: intp(6), ParsedKind: "sp"},
				{FileID: "noep", FileName: "x.mkv", ParsedKind: "main"},
				{FileID: "zero", FileName: "00.mkv", Episode: intp(0), ParsedKind: "main"},
				ep("e7", 7, "/lib/show/Show - 07.mkv", nil, nil),
			},
			dir:    1,
			wantID: "e7",
		},
		{
			name:   "当前这一集的另一份副本（同 FileID）不算邻居",
			cur:    cur,
			cands:  []item{ep("c", 5, "/disk2/show/Show - 05.mkv", nil, nil), ep("e6", 6, "/lib/show/Show - 06.mkv", nil, nil)},
			dir:    -1,
			wantID: "",
		},
		{
			name:   "最后一集没有下一集",
			cur:    cur,
			cands:  []item{ep("e4", 4, "/lib/show/Show - 04.mkv", nil, nil), cur},
			dir:    1,
			wantID: "",
		},
		{
			name:   "第一集没有上一集",
			cur:    ep("e1", 1, "/lib/show/Show - 01.mkv", nil, nil),
			cands:  []item{ep("e2", 2, "/lib/show/Show - 02.mkv", nil, nil)},
			dir:    -1,
			wantID: "",
		},
		{
			name:   "当前不是正片：没有邻居",
			cur:    item{FileID: "ova", FileName: "OVA 05.mkv", Episode: intp(5), ParsedKind: "ova"},
			cands:  []item{ep("e6", 6, "/lib/show/Show - 06.mkv", nil, nil)},
			dir:    1,
			wantID: "",
		},
		{
			name:   "当前认不出集号：没有邻居",
			cur:    item{FileID: "x", FileName: "x.mkv", ParsedKind: "main"},
			cands:  []item{ep("e6", 6, "/lib/show/Show - 06.mkv", nil, nil)},
			dir:    1,
			wantID: "",
		},
		{
			name:   "方向为 0：什么都不挑",
			cur:    cur,
			cands:  []item{ep("e6", 6, "/lib/show/Show - 06.mkv", nil, nil)},
			dir:    0,
			wantID: "",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			i, ok := Pick(tc.cur, tc.cands, tc.dir)
			if tc.wantID == "" {
				assert.False(t, ok)
				assert.Equal(t, -1, i)
				return
			}
			if assert.True(t, ok) {
				assert.Equal(t, tc.wantID, tc.cands[i].FileID)
			}
		})
	}
}

// 没有绝对路径（纯解析场景）时按库内相对路径比目录。
func TestPickSameDirectoryByRelativePath(t *testing.T) {
	n5, n6 := 5, 6
	cur := item{FileID: "c", FileName: "05.mkv", RelativePath: "show/05.mkv", Episode: &n5, ParsedKind: "main"}
	cands := []item{
		{FileID: "a", FileName: "06.mkv", RelativePath: "elsewhere/06.mkv", Episode: &n6, ParsedKind: "main"},
		{FileID: "b", FileName: "06.mkv", RelativePath: "show/06.mkv", Episode: &n6, ParsedKind: "main"},
	}
	i, ok := Pick(cur, cands, 1)
	assert.True(t, ok)
	assert.Equal(t, "b", cands[i].FileID)
}

// 结果不随候选顺序变。
func TestPickIsOrderIndependent(t *testing.T) {
	cur := ep("c", 5, "/lib/show/Show - 05.mkv", str("Sub"), nil)
	a := ep("a", 6, "/x/Show - 06.mkv", str("Sub"), nil)
	b := ep("b", 6, "/y/Show - 06.mkv", str("Sub"), nil)
	i1, _ := Pick(cur, []item{a, b}, 1)
	i2, _ := Pick(cur, []item{b, a}, 1)
	assert.Equal(t, []item{a, b}[i1].FileID, []item{b, a}[i2].FileID)
}

// 一个文件夹里混放两部番（媒体库按文件夹分组，它们是同一个作品分组）：邻居只在同一部番里找。
// 子目录里的文件 ParsedTitle 取的是目录名（两部番一样），所以要看文件名里的标题。
func TestPickMixedFolderStaysInTheSameShow(t *testing.T) {
	season := "2026-01"
	files := map[string]item{}
	var all []item
	for _, f := range []struct {
		id   string
		n    int
		name string
	}{
		{"f1", 1, "[Sub] Frieren - 01 [1080p].mkv"}, {"f2", 2, "[Sub] Frieren - 02 [1080p].mkv"}, {"f3", 3, "[Sub] Frieren - 03 [1080p].mkv"},
		{"d1", 1, "[Sub] Dandadan - 01 [1080p].mkv"}, {"d2", 2, "[Sub] Dandadan - 02 [1080p].mkv"},
	} {
		it := ep(f.id, f.n, "/lib/"+season+"/"+f.name, str("Sub"), str("1080p"))
		it.ParsedTitle = &season
		files[f.id] = it
		all = append(all, it)
	}
	tests := []struct {
		cur    string
		dir    int
		wantID string
	}{
		{"f1", 1, "f2"}, // 只看集号会挑《胆大党》第 2 集（文件名自然序 D 在 F 前面）
		{"f2", 1, "f3"},
		{"f3", 1, ""},
		{"f3", -1, "f2"},
		{"d1", 1, "d2"},
		{"d2", 1, ""}, // 不能接到《芙莉莲》第 3 集
		{"d2", -1, "d1"},
	}
	for _, tc := range tests {
		i, ok := Pick(files[tc.cur], all, tc.dir)
		if tc.wantID == "" {
			assert.False(t, ok, "%s 方向 %d 不该有邻居，却挑了下标 %d", tc.cur, tc.dir, i)
			continue
		}
		if assert.True(t, ok, "%s 方向 %d 应该有邻居", tc.cur, tc.dir) {
			assert.Equal(t, tc.wantID, all[i].FileID, "%s 方向 %d", tc.cur, tc.dir)
		}
	}
}

// 认不出作品标题（「05.mkv」）的一边：只认同一个目录里的，不跨目录冒险；标题认得出时跨目录照常找。
func TestPickUnknownTitleOnlyWithinTheSameDirectory(t *testing.T) {
	bare := ep("c", 5, "/lib/show/05.mkv", nil, nil)
	assert.Empty(t, workKey(bare), "前提：这个文件名认不出作品标题")
	_, ok := Pick(bare, []item{ep("far", 6, "/disk2/show/Show - 06.mkv", nil, nil)}, 1)
	assert.False(t, ok, "当前认不出标题：别的目录里的不算")
	cands := []item{ep("far", 6, "/disk2/show/Show - 06.mkv", nil, nil), ep("near", 7, "/lib/show/Show - 07.mkv", nil, nil)}
	i, ok := Pick(bare, cands, 1)
	if assert.True(t, ok) {
		assert.Equal(t, "near", cands[i].FileID, "同目录的第 7 集可以接（集号有空缺照接）")
	}

	titled := ep("c", 5, "/lib/show/Show - 05.mkv", nil, nil)
	_, ok = Pick(titled, []item{ep("bare", 6, "/disk2/show/06.mkv", nil, nil)}, 1)
	assert.False(t, ok, "候选认不出标题：别的目录里的也不算")
	_, ok = Pick(titled, []item{ep("bare", 6, "/lib/show/06.mkv", nil, nil)}, 1)
	assert.True(t, ok, "候选认不出标题但在同一个目录：算")
}

// 两边都认出了季号就必须相同：同一个文件夹里第一季第 3 集后面不能接第二季第 4 集。
func TestPickRequiresTheSameSeason(t *testing.T) {
	s1, s2 := 1, 2
	cur := ep("c", 3, "/lib/show/Show S1 - 03.mkv", nil, nil)
	cur.ParsedSeason = &s1
	other := ep("s2", 4, "/lib/show/Show S2 - 04.mkv", nil, nil)
	other.ParsedSeason = &s2
	same := ep("s1", 5, "/lib/show/Show S1 - 05.mkv", nil, nil)
	same.ParsedSeason = &s1
	unknown := ep("nil", 6, "/lib/show/Show - 06.mkv", nil, nil)

	cands := []item{other, same}
	i, ok := Pick(cur, cands, 1)
	if assert.True(t, ok) {
		assert.Equal(t, "s1", cands[i].FileID)
	}
	_, ok = Pick(cur, []item{other}, 1)
	assert.False(t, ok)
	_, ok = Pick(cur, []item{unknown}, 1)
	assert.True(t, ok, "有一边没认出季号：不按季号排除")
}
