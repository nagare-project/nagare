package player

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/nagare-project/nagare/internal/animego"
	errs "github.com/nagare-project/nagare/internal/errors"
	"github.com/nagare-project/nagare/internal/library"
	"github.com/nagare-project/nagare/internal/mpv/mpvtest"
)

// 窗口里换集的测试脚手架：假 Navigator、线程安全的 animego 替身、按集号造的本地文件。

// navClient 的「看完」回写加了锁：回写在后台 goroutine 里，测试要在窗口还开着时读。
type navClient struct {
	fakeClient
	mu      sync.Mutex
	watched []int
}

func newNavClient() *navClient {
	refs := map[int]animego.EpisodeRef{}
	for ep := 1; ep <= 3; ep++ {
		refs[ep] = animego.EpisodeRef{DandanEpisodeID: int64(420000 + ep), Title: fmt.Sprintf("第%d话", ep)}
	}
	return &navClient{fakeClient: fakeClient{loggedIn: true, matchRes: animego.MatchResult{
		Matched: true, AnilistID: 42, TitleChinese: "测试番剧", EpisodeMap: refs,
	}}}
}

func (c *navClient) EnsureSubscription(context.Context, int) error { return nil }

func (c *navClient) MarkWatched(_ context.Context, _ int, ep int) error {
	c.mu.Lock()
	c.watched = append(c.watched, ep)
	c.mu.Unlock()
	return nil
}

func (c *navClient) marks() []int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]int(nil), c.watched...)
}

// fakeNav 按 eps 的顺序给邻居（第 i 个的上一集是第 i-1 个）。
type fakeNav struct {
	mu   sync.Mutex
	eps  []library.Item
	subs map[string]string
	// wrap 把条目包成媒体源；nil 时是本地文件源。
	wrap func(library.Item) MediaSource
	// openErr 非 nil 时 Open 一律失败；gate 非 nil 时 Open 等它关闭（或 ctx 取消）。
	openErr error
	gate    chan struct{}

	neighborCalls int
	opens         []Direction
	openCtxErr    error
}

func (n *fakeNav) source(it library.Item) MediaSource {
	if n.wrap != nil {
		return n.wrap(it)
	}
	return NewLocalSource(it)
}

func (n *fakeNav) indexOf(src MediaSource) int {
	id := src.Item().FileID
	for i, it := range n.eps {
		if it.FileID == id {
			return i
		}
	}
	return -1
}

func navLabel(it library.Item) *Neighbor {
	return &Neighbor{Episode: *it.Episode, Label: fmt.Sprintf("第 %d 集", *it.Episode)}
}

func (n *fakeNav) Neighbors(src MediaSource) Neighbors {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.neighborCalls++
	i := n.indexOf(src)
	if i < 0 {
		return Neighbors{}
	}
	var out Neighbors
	if i > 0 {
		out.Prev = navLabel(n.eps[i-1])
	}
	if i+1 < len(n.eps) {
		out.Next = navLabel(n.eps[i+1])
	}
	return out
}

func (n *fakeNav) Open(ctx context.Context, src MediaSource, dir Direction) (Opened, error) {
	n.mu.Lock()
	n.opens = append(n.opens, dir)
	gate, openErr := n.gate, n.openErr
	n.mu.Unlock()
	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			n.mu.Lock()
			n.openCtxErr = ctx.Err()
			n.mu.Unlock()
			return Opened{}, ctx.Err()
		}
	}
	if openErr != nil {
		return Opened{}, openErr
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	i := n.indexOf(src) + int(dir)
	if i < 0 || i >= len(n.eps) {
		return Opened{}, errs.New(errs.CategoryFS, "player.nav", "没有下一集了", "媒体库里已经找不到它")
	}
	it := n.eps[i]
	return Opened{Source: n.source(it), SubPath: n.subs[it.FileID]}, nil
}

func (n *fakeNav) setGate(ch chan struct{}) {
	n.mu.Lock()
	n.gate = ch
	n.mu.Unlock()
}

func (n *fakeNav) setOpenErr(err error) {
	n.mu.Lock()
	n.openErr = err
	n.mu.Unlock()
}

func (n *fakeNav) ctxErr() error {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.openCtxErr
}

func (n *fakeNav) openCount() int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return len(n.opens)
}

func (n *fakeNav) calls() int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.neighborCalls
}

// navEpisodes 造 n 集指向真实临时文件的正片（内容各不相同，文件指纹也就不同）。
func navEpisodes(t *testing.T, n int) []library.Item {
	t.Helper()
	dir := t.TempDir()
	title := "测试番剧"
	items := make([]library.Item, 0, n)
	for i := 1; i <= n; i++ {
		name := fmt.Sprintf("[Test] Show - %02d [1080p].mkv", i)
		content := fmt.Sprintf("fake video content %d", i)
		abs := filepath.Join(dir, name)
		require.NoError(t, os.WriteFile(abs, []byte(content), 0o644))
		ep := i
		items = append(items, library.Item{
			FileID: fmt.Sprintf("%s|%d|1", name, len(content)), FileName: name, AbsPath: abs,
			Size: int64(len(content)), Episode: &ep, ParsedTitle: &title, ParsedKind: "main",
		})
	}
	return items
}

// shortNavDelays 把倒计时与「正在准备」的延迟改短。要在建 harness 之前调：恢复要排在 m.Stop 之后
// （t.Cleanup 后进先出），窗口的事件循环那时已经退出，不会与恢复赛跑。
func shortNavDelays(t *testing.T) {
	t.Helper()
	prevUp, prevSlow := upNextDelay, preparingCardDelay
	upNextDelay, preparingCardDelay = 50*time.Millisecond, 30*time.Millisecond
	t.Cleanup(func() { upNextDelay, preparingCardDelay = prevUp, prevSlow })
}

type navHarness struct {
	*windowHarness
	nav *fakeNav
	eps []library.Item
}

func newNavHarness(t *testing.T, client AnimegoClient, n int) *navHarness {
	t.Helper()
	shortNavDelays(t)
	h := newWindowHarness(t, client)
	eps := navEpisodes(t, n)
	nav := &fakeNav{eps: eps, subs: map[string]string{}}
	h.m.opts.Navigator = nav
	return &navHarness{windowHarness: h, nav: nav, eps: eps}
}

// switchEvents 是真 mpv 在 loadfile replace 之后发的事件：旧文件 end-file(stop) → start-file → file-loaded。
func switchEvents() []map[string]any {
	return []map[string]any{
		{"event": "end-file", "reason": "stop"},
		{"event": "start-file"},
		{"event": "file-loaded"},
	}
}

// replyLoadfile 让假 mpv 应答 loadfile 之后推 events。
func replyLoadfile(f *mpvtest.Fake, events ...map[string]any) {
	f.OnCommand("loadfile", func([]any) *mpvtest.Reply { return &mpvtest.Reply{Events: events} })
}

func uiMessage(t *testing.T, f *mpvtest.Fake, args ...string) {
	t.Helper()
	f.Push(t, map[string]any{"event": "client-message", "args": append([]string{"nagare-event"}, args...)})
}

// reachEnd 推一次「播到尾、停在最后一帧」（keep-open 开着时 mpv 不退出）。
func reachEnd(t *testing.T, f *mpvtest.Fake, pos, duration float64) {
	t.Helper()
	f.PushProperty(t, "duration", duration)
	f.PushProperty(t, "time-pos", pos)
	f.PushProperty(t, "eof-reached", true)
}

// startPlaying 起播 h.eps[i]：文件加载好、界面 ready。
func (h *navHarness) startPlaying(t *testing.T, i int) (PlayResult, *playback) {
	t.Helper()
	res, pb := h.playSource(t, h.nav.source(h.eps[i]))
	h.fake.Push(t, map[string]any{"event": "file-loaded"})
	uiMessage(t, h.fake, "ready")
	return res, pb
}

func eventually(t *testing.T, cond func() bool, msg string) {
	t.Helper()
	require.Eventually(t, cond, 3*time.Second, 5*time.Millisecond, msg)
}

func (h *navHarness) awaitEpisode(t *testing.T, fileID string) {
	t.Helper()
	eventually(t, func() bool { return h.m.Status().FileID == fileID }, "窗口里没有换到 "+fileID)
}

func (h *navHarness) awaitDanmakuOK(t *testing.T, fileID string) {
	t.Helper()
	eventually(t, func() bool {
		st := h.m.Status()
		return st.FileID == fileID && st.Danmaku != nil && st.Danmaku.State == "ok"
	}, "弹幕没有解析完")
}

func count(f *mpvtest.Fake, prefix ...any) int { return len(f.CommandsWith(prefix...)) }

// upNextCards 是推给界面的「下一集」卡片（按推送的顺序）。
func upNextCards(t *testing.T, f *mpvtest.Fake) []upNextCard {
	t.Helper()
	var cards []upNextCard
	for _, cmd := range f.CommandsWith("script-message-to", "nagare", "upnext") {
		require.Len(t, cmd, 4)
		var c upNextCard
		require.NoError(t, json.Unmarshal([]byte(cmd[3].(string)), &c))
		cards = append(cards, c)
	}
	return cards
}

// lastUIState 是最近一次推给界面的状态（原始 JSON 解成 map，看得到每个字段是否真的发了）。
func lastUIState(t *testing.T, f *mpvtest.Fake) map[string]any {
	t.Helper()
	states := f.CommandsWith("script-message-to", "nagare", "state")
	require.NotEmpty(t, states, "没推过界面状态")
	var out map[string]any
	require.NoError(t, json.Unmarshal([]byte(states[len(states)-1][3].(string)), &out))
	return out
}

// indexOfCommand 是第一条以 prefix 开头的命令在全部命令里的位置（没有为 -1）。
func indexOfCommand(f *mpvtest.Fake, prefix ...any) int {
	for i, cmd := range f.Commands() {
		if mpvtest.HasPrefix(cmd, prefix...) {
			return i
		}
	}
	return -1
}
