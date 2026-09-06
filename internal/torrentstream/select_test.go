package torrentstream

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	errs "github.com/nagare-project/nagare/internal/errors"
)

// 样本取自 internal/library/testdata/parse.jsonl（M1 的 97 条共享语料）。
// 合集用同一命名模板只改集号 —— 真实 BD 合集正是这个形状。
const (
	yuruCampTmpl = "[DBD-Raws][摇曳露营△][%02d][1080P][BDRip][HEVC-10bit][FLAC].mkv"
	yuruCampMenu = "[DBD-Raws][摇曳露营△][BD Menu][1080P][BDRip][HEVC-10bit][FLAC].mkv"
	yuruCampNCOP = "[DBD-Raws][摇曳露营△][NCOP1][1080P][BDRip][HEVC-10bit][FLAC].mkv"
	yuruCampNCED = "[DBD-Raws][摇曳露营△][NCED1][1080P][BDRip][HEVC-10bit][FLAC].mkv"
	showCM       = "[foo] Show CM 01 [1080p].mkv"
	oshiNoKo11   = "[ANi] 我推的孩子 - 11 [1080P][Baha][WEB-DL][AAC AVC][CHT].mp4"
	shikanoko05  = "[SweetSub][鹿乃子乃子虚乌有][05][WebRip][1080P][AVC 8bit][简日双语].mp4"
	bocchiSP01   = "[Moozzi2] Bocchi the Rock! SP01 (BD 1920x1080 x265-10Bit FLAC).mkv"
)

func yuruCamp(ep int) string { return fmt.Sprintf(yuruCampTmpl, ep) }

// entries 按传入顺序造种子内文件列表（下标即种子内下标）。
func entries(names ...string) []fileEntry {
	out := make([]fileEntry, 0, len(names))
	for i, name := range names {
		out = append(out, fileEntry{Index: i, Path: name, Size: int64(i+1) << 20})
	}
	return out
}

func req(hint, fileIndex int, title string) PrepareRequest {
	return PrepareRequest{Title: title, EpisodeHint: hint, FileIndex: fileIndex}
}

func TestSelectFileAutoSelectsSingleVideo(t *testing.T) {
	// 单集种子里除了正片还有字幕与说明文件，非视频先被过滤掉。
	got, err := selectFile(entries(shikanoko05, "x.ass", "z.txt"), req(0, -1, ""))
	require.NoError(t, err)
	require.False(t, got.Need, "只有一个候选就不该弹选集")
	assert.Equal(t, 0, got.Index)
	require.NotNil(t, got.Item.Episode)
	assert.Equal(t, 5, *got.Item.Episode, "集号走解析链派生")
	assert.Equal(t, shikanoko05, got.Item.FileName)
}

func TestSelectFileHintHitsExactlyOne(t *testing.T) {
	list := entries(yuruCamp(1), yuruCamp(2), yuruCamp(3))
	got, err := selectFile(list, req(2, -1, ""))
	require.NoError(t, err)
	require.False(t, got.Need)
	assert.Equal(t, 1, got.Index)
	require.NotNil(t, got.Item.Episode)
	assert.Equal(t, 2, *got.Item.Episode)
}

func TestSelectFileHintDerivedFromTitle(t *testing.T) {
	// 调用方没给 EpisodeHint，用搜索结果标题派生。
	list := entries(yuruCamp(1), yuruCamp(2), yuruCamp(3))
	got, err := selectFile(list, req(0, -1, yuruCamp(3)))
	require.NoError(t, err)
	require.False(t, got.Need)
	assert.Equal(t, 2, got.Index)
}

func TestSelectFileHintMissesNeedsSelection(t *testing.T) {
	list := entries(yuruCamp(1), yuruCamp(2), yuruCamp(3))
	got, err := selectFile(list, req(99, -1, ""))
	require.NoError(t, err)
	require.True(t, got.Need, "集号一个都没命中就交给用户选")
	assert.Len(t, got.Files, 3)
}

func TestSelectFileHintMatchesMultipleNeedsSelection(t *testing.T) {
	// SP01 也解析成第 1 集：两个候选同时命中，不替用户猜。
	list := entries(yuruCamp(1), bocchiSP01)
	got, err := selectFile(list, req(1, -1, ""))
	require.NoError(t, err)
	assert.True(t, got.Need, "同一集号有多个候选时必须交给用户选")
}

func TestSelectFileNoHintNeedsSelectionSortedByEpisode(t *testing.T) {
	// 种子内顺序打乱，候选按集号升序给出。
	list := entries(yuruCamp(3), oshiNoKo11, yuruCamp(1))
	got, err := selectFile(list, req(0, -1, ""))
	require.NoError(t, err)
	require.True(t, got.Need)
	require.Len(t, got.Files, 3)

	wantEpisodes := []int{1, 3, 11}
	wantIndexes := []int{2, 0, 1}
	for i, choice := range got.Files {
		require.NotNil(t, choice.Episode, "第 %d 个候选应有集号", i)
		assert.Equal(t, wantEpisodes[i], *choice.Episode)
		assert.Equal(t, wantIndexes[i], choice.Index, "下标必须仍是种子内下标")
	}
}

func TestSelectFileFiltersExtras(t *testing.T) {
	list := entries(yuruCamp(1), yuruCampMenu, yuruCampNCOP, yuruCampNCED, showCM, yuruCamp(2))
	got, err := selectFile(list, req(0, -1, ""))
	require.NoError(t, err)
	require.True(t, got.Need)
	require.Len(t, got.Files, 2, "菜单/NCOP/NCED/CM 应被过滤掉")
	assert.Equal(t, yuruCamp(1), got.Files[0].Name)
	assert.Equal(t, yuruCamp(2), got.Files[1].Name)
}

func TestSelectFileKeepsSpecials(t *testing.T) {
	// SP 与剧场版是用户可能真想看的，不能跟着花絮一起丢。
	got, err := selectFile(entries(bocchiSP01), req(0, -1, ""))
	require.NoError(t, err)
	require.False(t, got.Need)
	assert.Equal(t, "sp", got.Item.ParsedKind)
}

func TestSelectFileNoVideoFiles(t *testing.T) {
	_, err := selectFile(entries("x.ass", "z.txt", "trap.mkv.jpg"), req(0, -1, ""))
	require.Error(t, err)
	var e *errs.E
	require.ErrorAs(t, err, &e)
	assert.Equal(t, errs.CategoryInput, e.Category)
	assert.Contains(t, e.UserMsg, "没有可播放的视频文件")
}

func TestSelectFileOnlyExtras(t *testing.T) {
	// 有视频但全是花絮：与「压根没有视频」是两种失败，提示也不同。
	_, err := selectFile(entries(yuruCampMenu, yuruCampNCOP, yuruCampNCED), req(0, -1, ""))
	require.Error(t, err)
	var e *errs.E
	require.ErrorAs(t, err, &e)
	assert.Equal(t, errs.CategoryInput, e.Category)
	assert.Contains(t, e.UserMsg, "花絮")
}

func TestSelectFileHonoursExplicitFileIndex(t *testing.T) {
	list := entries(yuruCamp(1), yuruCamp(2), yuruCamp(3))
	got, err := selectFile(list, req(0, 2, ""))
	require.NoError(t, err)
	require.False(t, got.Need)
	assert.Equal(t, 2, got.Index)
	require.NotNil(t, got.Item.Episode)
	assert.Equal(t, 3, *got.Item.Episode)
}

func TestSelectFileRejectsFileIndexOutsideCandidates(t *testing.T) {
	// 下标指向被过滤掉的花絮（或压根越界）：只接受候选列表里给过的下标。
	list := entries(yuruCamp(1), yuruCampNCOP)
	for _, idx := range []int{1, 7} {
		_, err := selectFile(list, req(0, idx, ""))
		require.Error(t, err, "下标 %d 应被拒绝", idx)
		var e *errs.E
		require.ErrorAs(t, err, &e)
		assert.Equal(t, errs.CategoryInput, e.Category)
	}
}

func TestSelectFileUsesTorrentFolderPaths(t *testing.T) {
	// 合集种子里文件带目录前缀，解析链按目录首段取标题（与本地库一致）。
	list := []fileEntry{
		{Index: 0, Path: "摇曳露营△ BDRip/" + yuruCamp(1), Size: 1 << 30},
		{Index: 1, Path: "摇曳露营△ BDRip/" + yuruCamp(2), Size: 1 << 30},
	}
	got, err := selectFile(list, req(2, -1, ""))
	require.NoError(t, err)
	require.False(t, got.Need)
	assert.Equal(t, 1, got.Index)
	assert.Equal(t, yuruCamp(2), got.Item.FileName, "FileName 取基名")
	assert.Equal(t, "摇曳露营△ BDRip/"+yuruCamp(2), got.Item.RelativePath)
}

// 同集号的多个文件（合集里常见的简繁/多分辨率双版本）按文件名自然序定序，
// 而不是留着种子内那个毫无意义的原始顺序。复用 library.CompareFileNames ——
// 这条规则只能有一份实现。
func TestSelectFileSortsSameEpisodeByFileName(t *testing.T) {
	const tmpl = "[SweetSub][鹿乃子乃子虚乌有][05][WebRip][%s][AVC 8bit][简日双语].mp4"
	p1080 := fmt.Sprintf(tmpl, "1080P")
	p720 := fmt.Sprintf(tmpl, "720P")
	// 故意把 1080P 放在种子里的第一个：只按集号排的话输出会保持这个顺序。
	// 自然序是【数值】比较，所以 720P 排在 1080P 之前（720 < 1080）——
	// 这与网页端的 localeCompare(numeric:true) 一致，别为了"高清优先"另立一套。
	got, err := selectFile(entries(p1080, p720, yuruCamp(6)), req(0, -1, ""))
	require.NoError(t, err)
	require.True(t, got.Need, "同集号有两个候选，必须让用户选")
	require.Len(t, got.Files, 3)
	assert.Equal(t, p720, got.Files[0].Name, "720P 的数值小，自然序在前")
	assert.Equal(t, p1080, got.Files[1].Name)
	assert.Equal(t, yuruCamp(6), got.Files[2].Name, "第 6 集仍排在第 5 集之后")
	// 下标是种子内的原始下标，排序不能把它算错。
	assert.Equal(t, 1, got.Files[0].Index)
	assert.Equal(t, 0, got.Files[1].Index)
}

func TestSingleVideoMustRespectRequestedEpisode(t *testing.T) {
	for _, filename := range []string{shikanoko05, "Unknown Video.mkv"} {
		got, err := selectFile(entries(filename), req(7, -1, ""))
		require.NoError(t, err)
		require.True(t, got.Need, "不能因只有一个文件而忽略目标集号")
		require.Len(t, got.Files, 1)
		chosen, err := selectFile(entries(filename), req(7, 0, ""))
		require.NoError(t, err)
		require.False(t, chosen.Need)
	}
}
