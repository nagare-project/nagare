package torrentstream

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	errs "github.com/nagare-project/nagare/internal/errors"
	"github.com/nagare-project/nagare/internal/library"
)

func newDetachedSource(t *testing.T, base string) *Source {
	t.Helper()
	engine := newTestEngine(t, filepath.Join(t.TempDir(), "torrent"))
	engine.streamBase = func() string { return base }
	attachFakeSession(engine)

	engine.mu.Lock()
	sess := engine.sess
	engine.mu.Unlock()

	sess.mu.Lock()
	sess.infohash = testInfohash
	sess.index = 3
	sess.item = library.Item{
		FileID:   "t:" + testInfohash + "/3",
		FileName: "[DBD-Raws][摇曳露营△][01][1080P][BDRip].mkv",
		Size:     1 << 30,
	}
	sess.mu.Unlock()
	return sess.newSource()
}

func TestSourceMPVPath(t *testing.T) {
	tests := []struct {
		name string
		base string
		want string
	}{
		{
			name: "常规能力 URL",
			base: "http://127.0.0.1:8590/stream/deadbeef",
			want: "http://127.0.0.1:8590/stream/deadbeef/t/" + testInfohash + "/3",
		},
		{
			name: "调用方多给了一个尾斜杠",
			base: "http://127.0.0.1:8590/stream/deadbeef/",
			want: "http://127.0.0.1:8590/stream/deadbeef/t/" + testInfohash + "/3",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, newDetachedSource(t, tc.base).MPVPath())
		})
	}
}

func TestSourceMPVPathFollowsCapabilityRotation(t *testing.T) {
	// 能力段每进程轮换，MPVPath 必须现算而不是构造时定死。
	engine := newTestEngine(t, filepath.Join(t.TempDir(), "torrent"))
	current := "http://127.0.0.1:8590/stream/aaaa"
	engine.streamBase = func() string { return current }
	attachFakeSession(engine)
	engine.mu.Lock()
	sess := engine.sess
	engine.mu.Unlock()
	sess.mu.Lock()
	sess.infohash, sess.index = testInfohash, 0
	sess.mu.Unlock()

	src := sess.newSource()
	require.Contains(t, src.MPVPath(), "/stream/aaaa/")
	current = "http://127.0.0.1:8590/stream/bbbb"
	assert.Contains(t, src.MPVPath(), "/stream/bbbb/")
}

func TestSourceItemCarriesParsedFields(t *testing.T) {
	src := newDetachedSource(t, "http://127.0.0.1:8590/stream/deadbeef")
	item := src.Item()
	assert.Equal(t, "t:"+testInfohash+"/3", item.FileID, "FileID 稳定且跨会话可续播")
	assert.Empty(t, item.AbsPath, "磁力来源没有本地路径")
	assert.Zero(t, item.MTimeMs)
}

func TestSourceProbeFailsWhenSessionGone(t *testing.T) {
	src := newDetachedSource(t, "http://127.0.0.1:8590/stream/deadbeef")
	err := src.Probe(context.Background())
	require.Error(t, err, "会话没有选定文件时不该让 mpv 起来")
	var e *errs.E
	require.ErrorAs(t, err, &e)
	assert.Equal(t, errs.CategoryTorrent, e.Category)
	assert.NotEmpty(t, e.Recovery)
}

func TestSourceHash16MFailsSoftlyWhenSessionGone(t *testing.T) {
	src := newDetachedSource(t, "http://127.0.0.1:8590/stream/deadbeef")
	_, err := src.Hash16M(context.Background())
	require.Error(t, err)
	var e *errs.E
	require.ErrorAs(t, err, &e)
	assert.Equal(t, errs.CategoryTorrent, e.Category)
	assert.Contains(t, e.Recovery, "弹幕", "哈希失败只降级弹幕，不该影响播放")
}
