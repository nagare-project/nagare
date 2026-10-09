package api

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nagare-project/nagare/internal/animego"
	"github.com/nagare-project/nagare/internal/library"
	"github.com/nagare-project/nagare/internal/store"
)

// fakeMatcher 按文件名回答匹配请求，并记下每一次请求。results / errs 的键是文件名里的片段。
type fakeMatcher struct {
	mu      sync.Mutex
	calls   []animego.MatchInput
	results map[string]animego.MatchResult
	errs    map[string]error
	err     error // 所有请求都失败
}

func newFakeMatcher() *fakeMatcher {
	return &fakeMatcher{results: map[string]animego.MatchResult{}, errs: map[string]error{}}
}

func (f *fakeMatcher) Match(_ context.Context, in animego.MatchInput) (animego.MatchResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, in)
	if f.err != nil {
		return animego.MatchResult{}, f.err
	}
	for key, err := range f.errs {
		if strings.Contains(in.FileName, key) {
			return animego.MatchResult{}, err
		}
	}
	for key, res := range f.results {
		if strings.Contains(in.FileName, key) {
			return res, nil
		}
	}
	return animego.MatchResult{}, nil
}

func (f *fakeMatcher) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func (f *fakeMatcher) fileNames() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, c := range f.calls {
		out = append(out, c.FileName)
	}
	return out
}

func workMatch(anilistID int, title, cover string, episodes ...int) animego.MatchResult {
	m := map[int]animego.EpisodeRef{}
	for _, ep := range episodes {
		m[ep] = animego.EpisodeRef{DandanEpisodeID: int64(anilistID*1000 + ep), Title: fmt.Sprintf("第%d话", ep)}
	}
	return animego.MatchResult{Matched: true, Source: "dandanplay", AnilistID: anilistID, TitleChinese: title, CoverImageURL: cover, EpisodeMap: m}
}

func frierenMatch(episodes ...int) animego.MatchResult {
	return workMatch(154587, "葬送的芙莉莲", frierenCover, episodes...)
}

// makeShowDir 在库目录 root 下建一部番的文件夹（episodes 集，文件名带集号）。
func makeShowDir(t *testing.T, root, title string, episodes int) {
	t.Helper()
	sub := filepath.Join(root, title)
	require.NoError(t, os.MkdirAll(sub, 0o755))
	for ep := 1; ep <= episodes; ep++ {
		name := fmt.Sprintf("[Sub] %s - %02d [1080p].mkv", title, ep)
		require.NoError(t, os.WriteFile(filepath.Join(sub, name), make([]byte, 1<<20+ep), 0o644))
	}
}

type identifyEnv struct {
	store   *store.Store
	lib     *LibraryService
	matcher *fakeMatcher
	ident   *Identifier
	now     time.Time
}

// newIdentifyEnv 扫描 dirs（默认一份两集的芙莉莲），识别器的「现在」放在一小时之后 ——
// 测试刚写出来的文件都还在「正在写入」的静默期里。
func newIdentifyEnv(t *testing.T, dirs ...string) *identifyEnv {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "state.json"))
	require.NoError(t, err)
	lib := NewLibraryService(st)
	lib.SetArtPrefix("/art/cap")
	if len(dirs) == 0 {
		dirs = []string{makeMediaDir(t)}
	}
	for _, dir := range dirs {
		_, _, err = lib.AddFolder(dir)
		require.NoError(t, err)
	}
	env := &identifyEnv{store: st, lib: lib, matcher: newFakeMatcher(), now: time.Now().Add(time.Hour)}
	env.ident = NewIdentifier(lib, st, env.matcher)
	env.ident.pace = 0
	env.ident.now = func() time.Time { return env.now }
	lib.SetIdentify(env.ident.Kick, env.ident.Status)
	return env
}

func (e *identifyEnv) fileID(t *testing.T, name string) string {
	t.Helper()
	for _, c := range e.lib.View().Clusters {
		for _, g := range c.Groups {
			for _, it := range g.Items {
				if strings.Contains(it.FileName, name) {
					return it.FileID
				}
			}
		}
	}
	t.Fatalf("媒体库里没有 %s", name)
	return ""
}

func (e *identifyEnv) pass() time.Duration { return e.ident.pass(context.Background()) }

// 代表集（集号最小的正片）认出来之后：它落成匹配缓存，分组有了封面和自动匹配的作品；
// 再跑一轮不再发请求。
func TestIdentifyBindsTheRepresentativeEpisode(t *testing.T) {
	env := newIdentifyEnv(t)
	env.matcher.results["Frieren - 01"] = frierenMatch(1)

	assert.Zero(t, env.pass())

	require.Equal(t, []string{"[Sub] Frieren - 01 [1080p].mkv"}, env.matcher.fileNames())
	call := env.matcher.calls[0]
	assert.Equal(t, 1, call.Episode)
	assert.Equal(t, "葬送的芙莉莲", call.Keyword, "关键词与播放前的匹配一致：解析出的标题（目录名优先）")
	assert.Len(t, call.FileHash, 32, "带首 16MB 指纹")

	ep1 := env.fileID(t, "Frieren - 01")
	b, ok := env.store.Binding(ep1)
	require.True(t, ok)
	assert.Equal(t, 154587, b.AnilistID)
	assert.Equal(t, int64(154587001), b.DandanEpisodeID)
	assert.Equal(t, frierenCover, b.CoverURL)
	assert.Equal(t, "葬送的芙莉莲", b.Title)
	assert.NotEmpty(t, env.store.Hash(ep1), "指纹缓存下来，播放时不用再算")

	view := env.lib.View()
	require.Len(t, view.Clusters, 1)
	require.NotNil(t, view.Clusters[0].Matched)
	assert.Equal(t, 154587, view.Clusters[0].Matched.AnilistID)
	assert.NotEmpty(t, view.Clusters[0].Cover)
	assert.Nil(t, view.Identify, "识别完了视图里不带进度")

	env.pass()
	assert.Equal(t, 1, env.matcher.callCount(), "认出来的分组不再请求")
}

// 代表集没认出来就换下一集；问过的记下来。
func TestIdentifyTriesTheNextEpisodeAndRemembersAttempts(t *testing.T) {
	env := newIdentifyEnv(t)
	env.matcher.results["Frieren - 02"] = frierenMatch(2)

	env.pass()
	assert.Equal(t, []string{"[Sub] Frieren - 01 [1080p].mkv", "[Sub] Frieren - 02 [1080p].mkv"}, env.matcher.fileNames())
	assert.NotZero(t, env.store.IdentifyAttempt(env.fileID(t, "Frieren - 01")))
	b, _ := env.store.Binding(env.fileID(t, "Frieren - 02"))
	assert.Equal(t, 154587, b.AnilistID)
}

// 预算按分组算、不按每一轮算：上游没有的番，一周之内一共只问两个文件 ——
// 而不是每次重扫再问两集、直到把整季都读一遍。过了一周预算才恢复。
func TestIdentifyBudgetIsPerClusterNotPerPass(t *testing.T) {
	dir := t.TempDir()
	makeShowDir(t, dir, "Unknown Show", 6)
	env := newIdentifyEnv(t, dir)

	for range 4 {
		env.pass()
	}
	assert.Equal(t, 2, env.matcher.callCount())

	env.now = env.now.Add(identifyRetry + time.Hour)
	env.pass()
	env.pass()
	assert.Equal(t, 4, env.matcher.callCount(), "过了一周再问两个")
}

// 认出了番剧却没有作品 ID（上游没对应上）：封面照存，但也算问过 —— 预算用完就不再问。
func TestIdentifyWithoutAnilistIDKeepsTheCoverButStopsAsking(t *testing.T) {
	env := newIdentifyEnv(t)
	res := frierenMatch(1, 2)
	res.AnilistID = 0
	env.matcher.results["Frieren"] = res

	env.pass()
	b, ok := env.store.Binding(env.fileID(t, "Frieren - 01"))
	require.True(t, ok)
	assert.Equal(t, frierenCover, b.CoverURL)
	calls := env.matcher.callCount()
	assert.LessOrEqual(t, calls, identifyPerCluster)

	env.pass()
	env.pass()
	assert.Equal(t, calls, env.matcher.callCount())
}

// 认出了作品但上游没有封面：再问答案也一样，不再问。
func TestIdentifyMatchWithoutCoverIsSettled(t *testing.T) {
	env := newIdentifyEnv(t)
	env.matcher.results["Frieren"] = workMatch(154587, "葬送的芙莉莲", "", 1, 2)

	env.pass()
	env.pass()
	env.pass()
	assert.Equal(t, 1, env.matcher.callCount())
	b, _ := env.store.Binding(env.fileID(t, "Frieren - 01"))
	assert.Equal(t, 154587, b.AnilistID)
}

// 播放时已经匹配到【别的】作品的文件不改，而且不会每一轮都再问一遍。
func TestIdentifyNeverOverridesAPlaybackMatchToAnotherWork(t *testing.T) {
	env := newIdentifyEnv(t)
	ep1 := env.fileID(t, "Frieren - 01")
	played := store.Binding{AnilistID: 999, DandanEpisodeID: 7, Episode: 1, Title: "别的番", MatchedAt: 1}
	require.NoError(t, env.store.SetBinding(ep1, played))
	env.matcher.results["Frieren"] = frierenMatch(1, 2)

	env.pass()
	b, _ := env.store.Binding(ep1)
	assert.Equal(t, played, b)
	calls := env.matcher.callCount()

	env.pass()
	env.pass()
	assert.Equal(t, calls, env.matcher.callCount())
}

// 同一部作品的旧匹配（早期版本没存封面）：先试它，只补缺的封面。
func TestIdentifyFillsTheMissingCoverOfAnOldMatch(t *testing.T) {
	env := newIdentifyEnv(t)
	ep1 := env.fileID(t, "Frieren - 01")
	old := store.Binding{AnilistID: 154587, DandanEpisodeID: 1001, Episode: 1, Title: "葬送的芙莉莲", MatchedAt: 1}
	require.NoError(t, env.store.SetBinding(ep1, old))
	env.matcher.results["Frieren"] = frierenMatch(1, 2)

	env.pass()
	require.Equal(t, []string{"[Sub] Frieren - 01 [1080p].mkv"}, env.matcher.fileNames(), "先试已经匹配过作品的那一集")
	b, _ := env.store.Binding(ep1)
	assert.Equal(t, frierenCover, b.CoverURL)
	assert.Equal(t, int64(1001), b.DandanEpisodeID, "弹幕那一集不动")
	assert.Greater(t, b.MatchedAt, old.MatchedAt, "封面地址带匹配时间：补了封面要换地址")
}

// 用户认定过作品（或标为不是目录作品）的分组以用户为准，不去识别。
func TestIdentifySkipsAssociatedClusters(t *testing.T) {
	env := newIdentifyEnv(t)
	key := env.lib.View().Clusters[0].ClusterKey
	_, err := env.store.ApplyAssociation(key, &store.Association{Mode: store.AssociationNone}, nil)
	require.NoError(t, err)

	env.pass()
	assert.Zero(t, env.matcher.callCount())
}

// 刚写入的文件（BT 还在下载）先不读：首 16MB 可能还是空洞。静默期过了再来一轮。
func TestIdentifyWaitsForFilesStillBeingWritten(t *testing.T) {
	env := newIdentifyEnv(t)
	env.now = time.Now()

	wait := env.pass()
	assert.Zero(t, env.matcher.callCount())
	assert.Positive(t, wait)
	assert.LessOrEqual(t, wait, identifyQuiet)
}

// 扫描之后文件又被改写了（还在下载）：读之前核对一次，不对就这一轮跳过，不缓存错的指纹。
func TestIdentifySkipsFilesChangedSinceTheScan(t *testing.T) {
	env := newIdentifyEnv(t)
	ep1 := env.fileID(t, "Frieren - 01")
	it, ok := env.lib.Item(ep1)
	require.True(t, ok)
	require.NoError(t, os.WriteFile(it.AbsPath, make([]byte, 3<<20), 0o644))

	env.pass()
	assert.Equal(t, []string{"[Sub] Frieren - 02 [1080p].mkv"}, env.matcher.fileNames())
	assert.Empty(t, env.store.Hash(ep1))
	assert.Zero(t, env.store.IdentifyAttempt(ep1))
}

// 修改时间在未来（exFAT 时区、时钟漂移）的文件不会一直卡在「正在写入」。
func TestIdentifyFutureMtimeIsNotStuck(t *testing.T) {
	dir := makeMediaDir(t)
	future := time.Now().Add(10 * time.Hour)
	files, err := filepath.Glob(filepath.Join(dir, "葬送的芙莉莲", "*.mkv"))
	require.NoError(t, err)
	for _, f := range files {
		require.NoError(t, os.Chtimes(f, future, future))
	}
	env := newIdentifyEnv(t, dir)
	env.matcher.results["Frieren"] = frierenMatch(1, 2)

	assert.Zero(t, env.pass())
	assert.Equal(t, 1, env.matcher.callCount())
}

// animego 连不上 / 限速：整轮推迟、界面看得到原因，什么都不记（不是上游没有这部番）。
func TestIdentifyBacksOffWhenAnimegoIsDown(t *testing.T) {
	env := newIdentifyEnv(t)
	env.matcher.err = &animego.Error{Kind: animego.ErrUnavailable, Op: "match", Err: errors.New("dial tcp: refused")}

	assert.Equal(t, identifyBackoffUnavailable, env.pass())
	assert.Equal(t, 1, env.matcher.callCount(), "不再对着不可用的上游连发")
	assert.Zero(t, env.store.IdentifyAttempt(env.fileID(t, "Frieren - 01")))
	st := env.ident.Status()
	assert.False(t, st.Running)
	assert.NotEmpty(t, st.Error)
	require.NotNil(t, env.lib.View().Identify, "暂停时视图带上原因")

	env.matcher.err = &animego.Error{Kind: animego.ErrRateLimited, Op: "match", Status: 429}
	assert.Equal(t, identifyBackoffRateLimited, env.pass())
}

// 4xx 分两种：400 一类是这一次请求的内容不对（这个文件的问题，记下、换下一个文件）；
// 403 / 404 / 410 一类是接口整体不让用（暂停、什么都不记，否则每个文件都白白一周不再问）。
func TestIdentifyRejectionsByStatus(t *testing.T) {
	for _, tc := range []struct {
		status    int
		wantCalls int
		wantWait  time.Duration
		recorded  bool
	}{
		{status: 400, wantCalls: 2, recorded: true},
		{status: 422, wantCalls: 2, recorded: true},
		{status: 403, wantCalls: 1, wantWait: identifyBackoffUnavailable},
		{status: 404, wantCalls: 1, wantWait: identifyBackoffUnavailable},
	} {
		t.Run(fmt.Sprint(tc.status), func(t *testing.T) {
			env := newIdentifyEnv(t)
			env.matcher.err = &animego.Error{Kind: animego.ErrBadRequest, Op: "match", Status: tc.status}
			assert.Equal(t, tc.wantWait, env.pass())
			assert.Equal(t, tc.wantCalls, env.matcher.callCount())
			assert.Equal(t, tc.recorded, env.store.IdentifyAttempt(env.fileID(t, "Frieren - 01")) > 0)
			if tc.wantWait > 0 {
				assert.Contains(t, env.ident.Status().Error, fmt.Sprintf("HTTP %d", tc.status))
			}
		})
	}
}

// 一部作品的响应解析不了，只推迟它自己的文件，排在后面的分组照常识别；
// 连续好几个文件都这样才整轮暂停（那多半是上游整体出了问题）。
func TestIdentifyDecodeErrorOnlyDefersThatFile(t *testing.T) {
	dir := t.TempDir()
	makeShowDir(t, dir, "Broken", 2)
	makeShowDir(t, dir, "Fine", 2)
	env := newIdentifyEnv(t, dir)
	env.matcher.errs["Broken"] = &animego.Error{Kind: animego.ErrDecode, Op: "match"}
	env.matcher.results["Fine"] = workMatch(7, "好番", frierenCover, 1, 2)

	assert.Zero(t, env.pass())
	b, _ := env.store.Binding(env.fileID(t, "Fine - 01"))
	assert.Equal(t, 7, b.AnilistID, "排在后面的分组照常认出")
	assert.Empty(t, env.ident.Status().Error)
	assert.Zero(t, env.store.IdentifyAttempt(env.fileID(t, "Broken - 01")), "暂时性问题不算问过")

	calls := env.matcher.callCount()
	env.pass()
	assert.Equal(t, calls, env.matcher.callCount(), "推迟期内不再试")
}

func TestIdentifyBacksOffAfterRepeatedDecodeErrors(t *testing.T) {
	dir := t.TempDir()
	for _, title := range []string{"A Show", "B Show"} {
		makeShowDir(t, dir, title, 2)
	}
	env := newIdentifyEnv(t, dir)
	env.matcher.err = &animego.Error{Kind: animego.ErrDecode, Op: "match"}

	assert.Equal(t, identifyBackoffUnavailable, env.pass())
	assert.Equal(t, identifyMaxFileFailures, env.matcher.callCount())
	assert.NotEmpty(t, env.ident.Status().Error)
}

// 推迟期内的重扫 kick 不提前打断推迟：限速窗口里不再去敲上游。
func TestIdentifyKickDuringBackoffDoesNotRequest(t *testing.T) {
	env := newIdentifyEnv(t)
	env.matcher.err = &animego.Error{Kind: animego.ErrRateLimited, Op: "match", Status: 429}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		env.ident.Run(ctx)
		close(done)
	}()
	defer func() {
		cancel()
		<-done
	}()

	env.ident.Kick()
	require.Eventually(t, func() bool { return env.ident.Status().Error != "" }, 5*time.Second, 5*time.Millisecond)
	for range 3 {
		env.ident.Kick()
	}
	time.Sleep(100 * time.Millisecond)
	assert.Equal(t, 1, env.matcher.callCount())
	assert.False(t, env.ident.Status().Running, "推迟期内 kick 不把进度显示成「排上队了」")
}

// 退出时正在等限速间隔：立刻收工，不记任何结果、进度清空。
func TestIdentifyStopsWhenCancelledMidPass(t *testing.T) {
	dir := t.TempDir()
	makeShowDir(t, dir, "A Show", 2)
	makeShowDir(t, dir, "B Show", 2)
	env := newIdentifyEnv(t, dir)
	env.ident.pace = time.Hour
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		for env.matcher.callCount() == 0 {
			time.Sleep(time.Millisecond)
		}
		cancel()
	}()

	start := time.Now()
	assert.Zero(t, env.ident.pass(ctx))
	assert.Less(t, time.Since(start), 5*time.Second)
	assert.Equal(t, 1, env.matcher.callCount())
	assert.Equal(t, IdentifyStatus{}, env.ident.Status())
}

// 外接盘拔掉的那一轮扫不到它的文件：问过的记录不能跟着清掉，否则插回来又从头问一遍。
func TestIdentifyAttemptsSurviveAnUnpluggedDrive(t *testing.T) {
	disk := t.TempDir()
	makeShowDir(t, disk, "Unknown Show", 4)
	env := newIdentifyEnv(t, disk)
	env.pass()
	require.Equal(t, 2, env.matcher.callCount())

	unplugged := disk + ".off"
	require.NoError(t, os.Rename(disk, unplugged))
	env.lib.Rescan()
	env.pass()
	require.NoError(t, os.Rename(unplugged, disk))
	env.lib.Rescan()
	env.pass()
	assert.Equal(t, 2, env.matcher.callCount())
}

// 重扫完成会 kick 识别器：进度立刻显示「排上队了」（界面重读媒体库就开始刷新），Run 收到后跑一轮。
func TestRescanKicksTheIdentifier(t *testing.T) {
	env := newIdentifyEnv(t)
	env.matcher.results["Frieren - 01"] = frierenMatch(1)
	ep1 := env.fileID(t, "Frieren - 01")

	env.lib.Rescan()
	require.NotNil(t, env.lib.View().Identify)
	assert.True(t, env.lib.View().Identify.Running)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		env.ident.Run(ctx)
		close(done)
	}()
	require.Eventually(t, func() bool {
		b, ok := env.store.Binding(ep1)
		return ok && b.AnilistID == 154587
	}, 5*time.Second, 10*time.Millisecond)
	cancel()
	<-done
}

// 同一部番一模一样地拷在两块盘上：同名子目录并成一个分组（键不重复），
// 一模一样的副本只算一份，识别时也不会把同一个文件问两遍。
func TestIdenticalCopiesOnTwoDisks(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	stamp := time.Now().Add(-time.Hour)
	for _, disk := range []string{a, b} {
		makeShowDir(t, disk, "Unknown Show", 2)
		files, err := filepath.Glob(filepath.Join(disk, "Unknown Show", "*.mkv"))
		require.NoError(t, err)
		for _, f := range files {
			require.NoError(t, os.Chtimes(f, stamp, stamp))
		}
	}
	makeShowDir(t, b, "Unknown Show", 0)
	require.NoError(t, os.WriteFile(filepath.Join(b, "Unknown Show", "[Sub] Unknown Show - 03 [1080p].mkv"), make([]byte, 1<<20+3), 0o644))
	env := newIdentifyEnv(t, a, b)

	view := env.lib.View()
	require.Len(t, view.Clusters, 1)
	keys := map[string]bool{}
	var names []string
	for _, g := range view.Clusters[0].Groups {
		assert.False(t, keys[g.GroupKey], "groupKey 重复：%s", g.GroupKey)
		keys[g.GroupKey] = true
		for _, it := range g.Items {
			names = append(names, it.FileName)
		}
	}
	assert.Equal(t, []string{
		"[Sub] Unknown Show - 01 [1080p].mkv", "[Sub] Unknown Show - 02 [1080p].mkv", "[Sub] Unknown Show - 03 [1080p].mkv",
	}, names, "按集号排好、副本只算一份")

	env.pass()
	assert.Equal(t, []string{"[Sub] Unknown Show - 01 [1080p].mkv", "[Sub] Unknown Show - 02 [1080p].mkv"}, env.matcher.fileNames())
}

// 没有集号的单个文件（剧场版）：按第 1 集问，只记作品与封面 —— 不猜集号，
// 免得播放时多出一条把「第 1 集看完」写进账号的路径（没有集号的文件以前从不回写）。
func TestIdentifyUnnumberedFileGetsTheWorkOnly(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "[XKsub][5Hanayome_Movie][CHS_JPN][1080p][WebRip].mp4"), make([]byte, 1<<20), 0o644))
	env := newIdentifyEnv(t, dir)
	movie := animego.MatchResult{Matched: true, AnilistID: 131520, TitleChinese: "电影 五等分的新娘", CoverImageURL: frierenCover, TotalEpisodes: 1,
		EpisodeMap: map[int]animego.EpisodeRef{1: {DandanEpisodeID: 161650001, Title: "剧场版"}}}
	env.matcher.results["5Hanayome"] = movie

	env.pass()
	require.Equal(t, 1, env.matcher.callCount())
	assert.Equal(t, 1, env.matcher.calls[0].Episode)
	b, _ := env.store.Binding(env.fileID(t, "5Hanayome"))
	assert.Equal(t, 131520, b.AnilistID)
	assert.Equal(t, frierenCover, b.CoverURL)
	assert.Zero(t, b.Episode)
	assert.Zero(t, b.DandanEpisodeID)

	env.pass()
	assert.Equal(t, 1, env.matcher.callCount(), "认出来之后不再请求")
}

// 上游给的标题与封面地址落盘前有上限：每次写进度都会整份重写 state.json。
func TestIdentifiedBindingCapsUpstreamStrings(t *testing.T) {
	ep := 1
	title := "标题"
	it := testItem("x.mkv", &ep, &title)
	res := workMatch(1, strings.Repeat("长", 5000), "https://s4.anilist.co/"+strings.Repeat("a", maxIdentifyCoverBytes), 1)
	b := identifiedBinding(res, it, 1, time.Now())
	assert.Equal(t, maxIdentifyTitleRunes, len([]rune(b.Title)))
	assert.Empty(t, b.CoverURL)
}

func testItem(name string, ep *int, title *string) library.Item {
	return library.Item{FileID: name, FileName: name, Episode: ep, ParsedTitle: title, ParsedKind: "main"}
}

// mergeByKey：三个库目录里同键的分组并成一个；同名子目录并进同一个分组；不改动输入。
func TestMergeByKey(t *testing.T) {
	ep := func(n int) *int { return &n }
	title := "番"
	item := func(name string, n int) library.Item { return testItem(name, ep(n), &title) }
	group := func(key string, items ...library.Item) library.Group {
		return library.Group{GroupKey: key, Label: key, Items: items, SortMode: "episode"}
	}
	entry := func(key string, conf float64, rep *library.Item, groups ...library.Group) clusterEntry {
		var items []library.Item
		for _, g := range groups {
			items = append(items, g.Items...)
		}
		return clusterEntry{cluster: library.Cluster{ClusterKey: key, Groups: groups, Items: items, Representative: rep}, confidence: conf}
	}
	e1, e2, e3, other := item("e1", 1), item("e2", 2), item("e3", 3), item("o1", 1)
	in := []clusterEntry{
		entry("k", 0.5, nil, group("Show", e2)),
		entry("x", 0.9, &other, group("Other", other)),
		entry("k", 0.9, &e1, group("Show", e1, e2)),
		entry("k", 0.7, &e3, group("Extra", e3)),
	}
	before := fmt.Sprintf("%+v", in)

	out := mergeByKey(in)
	require.Len(t, out, 2)
	k := out[0].cluster
	assert.Equal(t, "k", k.ClusterKey)
	assert.Equal(t, 0.9, out[0].confidence)
	require.NotNil(t, k.Representative)
	assert.Equal(t, "e1", k.Representative.FileID, "第一个没有代表集时用后面的")
	require.Len(t, k.Groups, 2)
	assert.Equal(t, "Show", k.Groups[0].GroupKey)
	var names []string
	for _, it := range k.Groups[0].Items {
		names = append(names, it.FileID)
	}
	assert.Equal(t, []string{"e1", "e2"}, names, "同名子目录并成一组、按集号排、副本只算一份")
	assert.Len(t, k.Items, 3)
	assert.Equal(t, before, fmt.Sprintf("%+v", in), "不改动输入")
}
