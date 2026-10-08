package player

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nagare-project/nagare/internal/animego"
	"github.com/nagare-project/nagare/internal/library"
	"github.com/nagare-project/nagare/internal/mpv"
	"github.com/nagare-project/nagare/internal/store"
)

// streamSource 模拟磁力边下边播的来源：经本机 HTTP 交给 mpv，有文件指纹，
// 从作品页播放时带着目录作品身份。播放层不认识 torrentstream（那个包在下游），
// 只认 MediaSource 与 MatchHinted 两个接缝 —— 这里按同样的形状造一个。
type streamSource struct {
	item      library.Item
	url       string
	hash      string
	anilistID int
	titles    []string
}

func (s *streamSource) Item() library.Item                      { return s.item }
func (s *streamSource) Probe(context.Context) error             { return nil }
func (s *streamSource) MPVPath() string                         { return s.url }
func (s *streamSource) Hash16M(context.Context) (string, error) { return s.hash, nil }
func (s *streamSource) MatchHints() (int, []string) {
	return s.anilistID, append([]string(nil), s.titles...)
}

// torrentLike 造一个磁力来源：fileId 的形状与 torrentstream 一致（t:<infohash>/<下标>）。
func torrentLike(episode int, title string, anilistID int, titles []string) *streamSource {
	item := library.Item{
		FileID:     "t:0123456789abcdef0123456789abcdef01234567/2",
		FileName:   "[Group] Sousou no Frieren - 03 [1080p].mkv",
		Size:       1 << 30,
		Episode:    &episode,
		ParsedKind: "main",
	}
	if title != "" {
		item.ParsedTitle = &title
	}
	return &streamSource{
		item: item, url: "http://127.0.0.1:8590/stream/cap/t/0123456789abcdef0123456789abcdef01234567/2",
		hash: "d41d8cd98f00b204e9800998ecf8427e", anilistID: anilistID, titles: titles,
	}
}

// 以前从搜索页（没有作品身份）播过、匹配错到了别的作品：那条匹配被缓存下来，此后从作品页
// 再播也沿用 —— 弹幕是别的番的，看完还记到别的番上。带着身份来的播放必须重新匹配。
func TestEnsureBindingHintedStreamRematchesStaleBinding(t *testing.T) {
	client := &fakeClient{matchRes: animego.MatchResult{
		Matched: true, AnilistID: 154587, TitleChinese: "葬送的芙莉莲",
		EpisodeMap: map[int]animego.EpisodeRef{3: {DandanEpisodeID: 1545870003, Title: "第3话"}},
	}}
	m, st, _ := newTestManager(t, client)
	src := torrentLike(3, "Sousou no Frieren", 154587, []string{"葬送的芙莉莲", "葬送のフリーレン"})
	require.NoError(t, st.SetBinding(src.item.FileID, store.Binding{AnilistID: 999, DandanEpisodeID: 9990003, Episode: 3, Title: "别的番"}))

	b, dan := m.ensureBinding(context.Background(), src, src.Item())
	assert.Equal(t, "ok", dan.State)
	assert.Equal(t, 154587, b.AnilistID)
	assert.Equal(t, int64(1545870003), b.DandanEpisodeID)
	require.Equal(t, int32(1), client.matchCalls.Load(), "身份对不上的缓存不能沿用")
	assert.Equal(t, "d41d8cd98f00b204e9800998ecf8427e", client.matchIn[0].FileHash, "磁力照样带文件指纹去匹配")
	saved, ok := st.Binding(src.item.FileID)
	require.True(t, ok)
	assert.Equal(t, 154587, saved.AnilistID, "重新匹配的结果覆盖旧缓存")

	// 同一部作品的缓存照常沿用
	_, dan = m.ensureBinding(context.Background(), src, src.Item())
	assert.Equal(t, "ok", dan.State)
	assert.Equal(t, int32(1), client.matchCalls.Load())
}

// 文件名里的标题命中了另一部作品：拒绝，换目录标题再试；所有关键词都只命中别的作品时
// 宁可没有弹幕，也不缓存一条错的匹配。
func TestEnsureBindingHintedStreamRejectsOtherAnime(t *testing.T) {
	stray := animego.MatchResult{Matched: true, AnilistID: 999, EpisodeMap: map[int]animego.EpisodeRef{3: {DandanEpisodeID: 9990003}}}
	right := animego.MatchResult{Matched: true, AnilistID: 154587, EpisodeMap: map[int]animego.EpisodeRef{3: {DandanEpisodeID: 1545870003}}}
	client := &fakeClient{matchByKw: map[string]animego.MatchResult{"Sousou no Frieren": stray, "葬送的芙莉莲": right}}
	m, _, _ := newTestManager(t, client)
	src := torrentLike(3, "Sousou no Frieren", 154587, []string{"葬送的芙莉莲"})

	b, dan := m.ensureBinding(context.Background(), src, src.Item())
	assert.Equal(t, "ok", dan.State)
	assert.Equal(t, 154587, b.AnilistID)
	require.Len(t, client.matchIn, 2)
	assert.Equal(t, "Sousou no Frieren", client.matchIn[0].Keyword)
	assert.Equal(t, "葬送的芙莉莲", client.matchIn[1].Keyword)

	onlyStray := &fakeClient{matchRes: stray}
	m2, st2, _ := newTestManager(t, onlyStray)
	_, dan = m2.ensureBinding(context.Background(), src, src.Item())
	assert.Equal(t, "unmatched", dan.State)
	assert.Contains(t, dan.Reason, "999")
	_, ok := st2.Binding(src.item.FileID)
	assert.False(t, ok, "认错的匹配不能落盘")
}

// 搜索页播放的磁力没有作品身份：与改动前逐字节一致 —— 缓存照用，标题解析不出来也照发
// 空关键词（服务端靠文件指纹命中）。
func TestEnsureBindingUnhintedStreamKeepsOldBehaviour(t *testing.T) {
	client := &fakeClient{matchRes: animego.MatchResult{
		Matched: true, AnilistID: 5, EpisodeMap: map[int]animego.EpisodeRef{3: {DandanEpisodeID: 50003}},
	}}
	m, st, _ := newTestManager(t, client)
	src := torrentLike(3, "", 0, nil)

	_, dan := m.ensureBinding(context.Background(), src, src.Item())
	assert.Equal(t, "ok", dan.State)
	require.Len(t, client.matchIn, 1)
	assert.Empty(t, client.matchIn[0].Keyword, "解析不出标题也要发一次（空关键词）")

	require.NoError(t, st.SetBinding(src.item.FileID, store.Binding{AnilistID: 999, DandanEpisodeID: 9990003, Episode: 3}))
	b, _ := m.ensureBinding(context.Background(), src, src.Item())
	assert.Equal(t, 999, b.AnilistID, "没有身份就无从判断缓存对不对，照旧沿用")
	assert.Equal(t, int32(1), client.matchCalls.Load())
}

// 带着作品 ID 却一个能用的标题都没有（文件名解析不出、目录标题也没给）：照发一次空关键词，
// 靠文件指纹匹配，结果仍要对上作品 ID。
func TestEnsureBindingHintedWithoutTitlesStillMatchesByFingerprint(t *testing.T) {
	client := &fakeClient{matchRes: animego.MatchResult{
		Matched: true, AnilistID: 154587, EpisodeMap: map[int]animego.EpisodeRef{3: {DandanEpisodeID: 1545870003}},
	}}
	m, _, _ := newTestManager(t, client)
	src := torrentLike(3, "", 154587, nil)

	b, dan := m.ensureBinding(context.Background(), src, src.Item())
	assert.Equal(t, "ok", dan.State)
	assert.Equal(t, 154587, b.AnilistID)
	require.Len(t, client.matchIn, 1)
	assert.Empty(t, client.matchIn[0].Keyword)
	assert.NotEmpty(t, client.matchIn[0].FileHash)
}

// 跨季连续编号的磁力文件（前作 12 集，第二季第 3 集叫 15）：分季条目里没有第 15 集，
// 偏移已知时换成作品集号再匹配一次，弹幕与「看完」都落在第 3 集上。
func TestEnsureBindingContinuousNumberingRetriesSeasonEpisode(t *testing.T) {
	client := &fakeClient{matchRes: animego.MatchResult{
		Matched: true, AnilistID: 182255,
		EpisodeMap: map[int]animego.EpisodeRef{3: {DandanEpisodeID: 1822550003, Title: "第3话"}},
	}}
	m, st, _ := newTestManager(t, client)
	m.opts.EpisodeSpace = func(context.Context, int) (EpisodeSpace, error) {
		return EpisodeSpace{Total: 12, Offset: 12, OffsetKnown: true}, nil
	}
	src := torrentLike(15, "Some Show S2", 182255, []string{"某作品 第二季"})

	b, dan := m.ensureBinding(context.Background(), src, src.Item())
	assert.Equal(t, "ok", dan.State)
	assert.Equal(t, 15, b.Episode, "绑定记文件里的集号：回写前 placeEpisode 会统一换算，这里换过就会被换两次")
	assert.Equal(t, int64(1822550003), b.DandanEpisodeID, "弹幕是作品第 3 集的")
	require.Len(t, client.matchIn, 2)
	assert.Equal(t, 15, client.matchIn[0].Episode)
	assert.Equal(t, 3, client.matchIn[1].Episode)
	saved, ok := st.Binding(src.item.FileID)
	require.True(t, ok)
	assert.Equal(t, 15, saved.Episode)

	// 偏移未知：不拿没人确认过的起点去换算，老实报「没有这一集」
	unknown := &fakeClient{matchRes: client.matchRes}
	m2, _, _ := newTestManager(t, unknown)
	m2.opts.EpisodeSpace = func(context.Context, int) (EpisodeSpace, error) { return EpisodeSpace{Total: 12}, nil }
	_, dan = m2.ensureBinding(context.Background(), src, src.Item())
	assert.Equal(t, "unmatched", dan.State)
	assert.Contains(t, dan.Reason, "第 15 集")
	assert.Equal(t, int32(1), unknown.matchCalls.Load())
}

// 换集号匹配之后看完回写：前作 12 集、本作 24 集，文件「26」是作品第 14 集。
// 绑定若记成换算后的 14，回写时会被再换一次、写成第 2 集。
func TestContinuousNumberingRetryWritesTheRightEpisode(t *testing.T) {
	for name, space := range map[string]EpisodeSpace{
		"总集数已知":    {Total: 24, Offset: 12, OffsetKnown: true},
		"连载中没有总集数": {Offset: 12, OffsetKnown: true},
	} {
		t.Run(name, func(t *testing.T) {
			client := &fakeClient{loggedIn: true, matchRes: animego.MatchResult{
				Matched: true, AnilistID: 182255,
				EpisodeMap: map[int]animego.EpisodeRef{14: {DandanEpisodeID: 1822550014}},
			}}
			m, st, _ := newTestManager(t, client)
			m.opts.EpisodeSpace = func(context.Context, int) (EpisodeSpace, error) { return space, nil }
			src := torrentLike(26, "Some Show S2", 182255, []string{"某作品 第二季"})

			b, dan := m.ensureBinding(context.Background(), src, src.Item())
			require.Equal(t, "ok", dan.State)
			require.NoError(t, st.SetProgress(src.item.FileID, store.Progress{Completed: true}))
			m.syncWatched(&session{item: src.item, finished: make(chan struct{}), binding: b})
			assert.Equal(t, []int{14}, client.marked)
		})
	}
}

// 磁力流断了供给（分享者走光、会话被收掉）时 mpv 照样报 eof：远没播完就不能算看完。
func TestPrematureEOFCoversTorrentStreams(t *testing.T) {
	torrent := torrentLike(3, "t", 0, nil)
	assert.True(t, prematureEOF(torrent, mpv.State{TimePos: 300, Duration: 1440}, true))
	assert.False(t, prematureEOF(torrent, mpv.State{TimePos: 1400, Duration: 1440}, true), "播到尾")
	assert.False(t, prematureEOF(torrent, mpv.State{TimePos: 300, Duration: 0}, true), "时长未知无从判断")
	assert.False(t, prematureEOF(torrent, mpv.State{TimePos: 300, Duration: 1440}, false), "不是 eof")

	failure := playbackFailureFor(torrent, torrent.item.FileID, "eof", nil, true, 7)
	require.NotNil(t, failure)
	assert.Equal(t, torrent.item.FileID, failure.FileID)
	assert.Contains(t, failure.Reason, "没有记为看完", "要说清楚这一集没记上，用户才知道要重看")
}

// 端到端：磁力流只给出前一小段就断（mpv 报 eof），这一集不能被记成看完，也不能回写账号。
func TestTorrentTruncatedStreamIsNotCompletion(t *testing.T) {
	info, err := mpv.Detect("")
	if err != nil {
		t.Skipf("本机无可用 mpv：%v", err)
	}
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("本机无 ffmpeg，跳过")
	}
	dir := t.TempDir()
	full := filepath.Join(dir, "full.mp4")
	out, err := exec.Command(ffmpeg, "-y", "-f", "lavfi", "-i", "testsrc2=duration=20:size=320x180:rate=10",
		"-c:v", "libx264", "-preset", "ultrafast", "-movflags", "+faststart", full).CombinedOutput()
	require.NoError(t, err, "ffmpeg 生成测试视频失败：%s", out)
	data, err := os.ReadFile(full)
	require.NoError(t, err)
	truncated := data[:len(data)*2/5]
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "ep.mp4", time.Time{}, bytes.NewReader(truncated))
	}))
	defer server.Close()

	client := &fakeClient{loggedIn: true, matchRes: animego.MatchResult{
		Matched: true, AnilistID: 154587, EpisodeMap: map[int]animego.EpisodeRef{3: {DandanEpisodeID: 1545870003}},
	}}
	st, err := store.Open(filepath.Join(dir, "state.json"))
	require.NoError(t, err)
	m := New(Options{Store: st, Client: client, RuntimeDir: dir, MPV: mpv.NewRuntimeWith(func(string) (mpv.Info, error) { return info, nil }, "")})
	defer m.Stop()
	src := torrentLike(3, "Sousou no Frieren", 154587, []string{"葬送的芙莉莲"})
	src.url = server.URL + "/ep.mp4"
	result, err := m.Play(context.Background(), src, "")
	require.NoError(t, err)

	require.Eventually(t, func() bool { return !m.Status().Playing }, 30*time.Second, 100*time.Millisecond)
	m.Stop()
	status := m.Status()
	require.NotNil(t, status.PlaybackFailure, "提前 eof 要记为播放失败")
	assert.Equal(t, result.FileID, status.PlaybackFailure.FileID)
	assert.Contains(t, status.PlaybackFailure.Reason, "没有记为看完")
	if p, ok := st.Progress(result.FileID); ok {
		assert.False(t, p.Completed, "只播了一小段不能算看完")
	}
	assert.Empty(t, client.marked, "没看完不能回写账号")
}
