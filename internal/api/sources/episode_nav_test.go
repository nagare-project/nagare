package sources

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nagare-project/nagare/internal/api"
	errs "github.com/nagare-project/nagare/internal/errors"
	"github.com/nagare-project/nagare/internal/library"
	"github.com/nagare-project/nagare/internal/player"
	"github.com/nagare-project/nagare/internal/store"
	"github.com/nagare-project/nagare/internal/torrentstream"
)

// emptyLibraryNavigator 是桌面版的内层导航（本地媒体库），库里什么都没有。
func emptyLibraryNavigator(t *testing.T) *api.EpisodeNavigator {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "state.json"))
	require.NoError(t, err)
	return api.NewEpisodeNavigator(api.NewLibraryService(st))
}

// 磁力来源交给引擎：会话已经不在（零值 Source）时没有邻居，Open 原样交出引擎的分类错误，
// 屏幕提示与错误卡片直接用它的说法与恢复动作。有会话时的邻居与换集见 torrentstream 的集成测试。
func TestEpisodeNavigatorTorrentDelegatesToEngine(t *testing.T) {
	nav := NewTorrentNavigator(emptyLibraryNavigator(t))
	src := &torrentstream.Source{}
	assert.Equal(t, player.Neighbors{}, nav.Neighbors(src))
	for dir, word := range map[player.Direction]string{player.DirectionNext: "下一集", player.DirectionPrev: "上一集"} {
		opened, err := nav.Open(context.Background(), src, dir)
		assert.Nil(t, opened.Source)
		var ce *errs.E
		require.ErrorAs(t, err, &ce)
		assert.Equal(t, "torrentstream.switch", ce.Op)
		assert.Equal(t, errs.CategoryTorrent, ce.Category)
		assert.Contains(t, ce.UserMsg, word)
		assert.NotEmpty(t, ce.Recovery)
	}
}

// 种子里的候选翻成界面上的邻居：「第 N 集」；没有集号的不出按钮。
func TestChoiceNeighbor(t *testing.T) {
	ep := 7
	assert.Equal(t, &player.Neighbor{Episode: 7, Label: "第 7 集"}, choiceNeighbor(&torrentstream.FileChoice{Index: 3, Episode: &ep}))
	assert.Nil(t, choiceNeighbor(&torrentstream.FileChoice{Index: 3}))
	assert.Nil(t, choiceNeighbor(nil))
}

// recordingNavigator 是内层导航替身：记下被问到的来源与方向，原样交回预设结果。
type recordingNavigator struct {
	neighbors player.Neighbors
	opened    player.Opened
	openErr   error
	asked     []player.MediaSource
	dirs      []player.Direction
}

func (r *recordingNavigator) Neighbors(src player.MediaSource) player.Neighbors {
	r.asked = append(r.asked, src)
	return r.neighbors
}

func (r *recordingNavigator) Open(_ context.Context, src player.MediaSource, dir player.Direction) (player.Opened, error) {
	r.asked = append(r.asked, src)
	r.dirs = append(r.dirs, dir)
	return r.opened, r.openErr
}

// 不是磁力的来源（本地文件、在线候选）一律交给内层导航，结果原样交回 —— 与拆包前同一个导航里的行为一致。
func TestTorrentNavigatorPassesOtherSourcesThrough(t *testing.T) {
	ep := 2
	local := player.NewLocalSource(library.Item{FileID: "x|1|1", FileName: "x.mkv", Episode: &ep, ParsedKind: "main"})
	remote := player.NewRemoteSource(player.RemoteSourceOptions{SourceID: "s", CandidateID: "c", URL: "https://example.test/a.m3u8", Title: "t", Episode: 1})
	for name, src := range map[string]player.MediaSource{"本地文件": local, "在线候选": remote} {
		t.Run(name, func(t *testing.T) {
			inner := &recordingNavigator{
				neighbors: player.Neighbors{Next: api.NeighborForEpisode(3)},
				opened:    player.Opened{SubPath: "/subs/03.ass"},
				openErr:   errors.New("inner"),
			}
			nav := NewTorrentNavigator(inner)

			assert.Equal(t, inner.neighbors, nav.Neighbors(src))
			opened, err := nav.Open(context.Background(), src, player.DirectionNext)
			assert.Equal(t, inner.opened, opened)
			assert.Same(t, inner.openErr, err)
			assert.Equal(t, []player.MediaSource{src, src}, inner.asked)
			assert.Equal(t, []player.Direction{player.DirectionNext}, inner.dirs)
		})
	}
}
