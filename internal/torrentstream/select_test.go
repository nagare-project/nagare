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

// 合集标题不派生集号：解析链会把「[01-03]」读成第 1 集、「全3集」读成第 3 集，
// 不拦的话点了合集就自动开播一个用户从没选过的文件（还会被记成看过）。
func TestSelectFileNeverDerivesHintFromBatchTitle(t *testing.T) {
	list := entries(yuruCamp(1), yuruCamp(2), yuruCamp(3))
	for _, title := range []string{
		"[DBD-Raws][摇曳露营△][01-03][1080P][BDRip][HEVC-10bit][FLAC]",
		"[DBD-Raws][摇曳露营△][全3集][1080P][BDRip]",
		"[DBD-Raws][摇曳露营△][合集][1080P]",
	} {
		got, err := selectFile(list, req(0, -1, title))
		require.NoError(t, err)
		assert.True(t, got.Need, "合集标题 %q 不能替用户选文件", title)
		assert.Len(t, got.Files, 3)
	}
}

// 用户在作品页选了第 3 集再点合集：集号由调用方给，合集照样能自动选中那一集。
func TestSelectFileBatchWithExplicitHint(t *testing.T) {
	list := entries(yuruCamp(1), yuruCamp(2), yuruCamp(3))
	got, err := selectFile(list, req(3, -1, "[DBD-Raws][摇曳露营△][01-03][1080P][BDRip]"))
	require.NoError(t, err)
	require.False(t, got.Need)
	assert.Equal(t, 2, got.Index)
}

// 来源插件指明的文件只是建议：在候选里就直接用（哪怕集号提示指向别的文件）；
// 指向花絮或越界时当没给，照常按集号选或交给用户，而不是报「选中的文件不在这条资源里」。
func TestSelectFileSuggestedIndexIsSoft(t *testing.T) {
	list := entries(yuruCamp(1), yuruCampNCOP, yuruCamp(2), yuruCamp(3))
	at := func(i int) *int { return &i }

	got, err := selectFile(list, PrepareRequest{EpisodeHint: 3, SuggestedFileIndex: at(2), FileIndex: -1})
	require.NoError(t, err)
	require.False(t, got.Need)
	assert.Equal(t, 2, got.Index, "插件指明的文件优先于集号提示")

	for _, bad := range []int{1, 9} {
		got, err = selectFile(list, PrepareRequest{EpisodeHint: 3, SuggestedFileIndex: at(bad), FileIndex: -1})
		require.NoError(t, err, "下标 %d 只是建议，不该报错", bad)
		require.False(t, got.Need)
		assert.Equal(t, 3, got.Index, "退回按集号选")

		got, err = selectFile(list, PrepareRequest{SuggestedFileIndex: at(bad), FileIndex: -1})
		require.NoError(t, err)
		assert.True(t, got.Need, "没有集号提示就交给用户")
	}

	// 用户手选的仍然最优先
	got, err = selectFile(list, PrepareRequest{SuggestedFileIndex: at(2), FileIndex: 0})
	require.NoError(t, err)
	assert.Equal(t, 0, got.Index)
}

// 跨季连续编号：第二季第 3 集在合集里叫 15。两种编号都给时，种子里只有其中一种就自动选；
// 两种都在（前作与本季装在一个合集里）就交给用户。
func TestSelectFileAltEpisodeHintForContinuousNumbering(t *testing.T) {
	continuous := entries(yuruCamp(13), yuruCamp(14), yuruCamp(15))
	r := PrepareRequest{EpisodeHint: 3, AltEpisodeHint: 15, FileIndex: -1}
	got, err := selectFile(continuous, r)
	require.NoError(t, err)
	require.False(t, got.Need)
	assert.Equal(t, 2, got.Index)
	require.NotNil(t, got.Item.Episode)
	assert.Equal(t, 15, *got.Item.Episode)

	seasonal := entries(yuruCamp(1), yuruCamp(2), yuruCamp(3))
	got, err = selectFile(seasonal, r)
	require.NoError(t, err)
	require.False(t, got.Need)
	assert.Equal(t, 2, got.Index, "按季编号的合集照样命中第 3 集")

	both := entries(yuruCamp(3), yuruCamp(15))
	got, err = selectFile(both, r)
	require.NoError(t, err)
	assert.True(t, got.Need, "两种编号同时命中时分不清哪个才是这一集")

	// 另一种编号只是 EpisodeHint 的补充：单独给它不起作用
	got, err = selectFile(continuous, PrepareRequest{AltEpisodeHint: 15, FileIndex: -1})
	require.NoError(t, err)
	assert.True(t, got.Need)
}
