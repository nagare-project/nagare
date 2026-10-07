package api

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nagare-project/nagare/internal/animego"
	"github.com/nagare-project/nagare/internal/player"
)

type spaceStub struct {
	mu          sync.Mutex
	episodes    *int
	offset      animego.EpisodeOffset
	offsetErr   error
	detailErr   error
	detailCalls int
	offsetCalls int
}

func (f *spaceStub) Detail(_ context.Context, id int) (animego.CatalogMedia, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.detailCalls++
	return animego.CatalogMedia{AnilistID: id, Episodes: f.episodes}, f.detailErr
}

func (f *spaceStub) EpisodeOffset(context.Context, int) (animego.EpisodeOffset, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.offsetCalls++
	return f.offset, f.offsetErr
}

func TestEpisodeSpacesCombinesTotalAndOffset(t *testing.T) {
	ten := 10
	f := &spaceStub{episodes: &ten, offset: animego.EpisodeOffset{Known: true, Offset: 28}}
	s := NewEpisodeSpaces(f)

	got, err := s.Lookup(context.Background(), 182255)
	require.NoError(t, err)
	assert.Equal(t, player.EpisodeSpace{Total: 10, Offset: 28, OffsetKnown: true}, got)

	_, err = s.Lookup(context.Background(), 182255)
	require.NoError(t, err)
	assert.Equal(t, 1, f.detailCalls, "十分钟内命中缓存")
	assert.Equal(t, 1, f.offsetCalls)
}

// known=false 不能变成「偏移 0」；连载中没有总集数时总集数为 0（未知）；负数一律当未知。
func TestEpisodeSpacesKeepsUnknownsUnknown(t *testing.T) {
	for name, f := range map[string]*spaceStub{
		"算不出偏移":   {offset: animego.EpisodeOffset{Known: false}},
		"负的偏移与集数": {episodes: ptr(-1), offset: animego.EpisodeOffset{Known: true, Offset: -3}},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := NewEpisodeSpaces(f).Lookup(context.Background(), 1)
			require.NoError(t, err)
			assert.Equal(t, player.EpisodeSpace{}, got)
		})
	}
}

func ptr(n int) *int { return &n }

// 偏移一时取不到：这次按未知处理，但不缓存 —— 下次还会再查。
func TestEpisodeSpacesDoesNotCacheOffsetFailures(t *testing.T) {
	ten := 10
	f := &spaceStub{episodes: &ten, offsetErr: errors.New("timeout")}
	s := NewEpisodeSpaces(f)

	got, err := s.Lookup(context.Background(), 1)
	require.NoError(t, err)
	assert.Equal(t, player.EpisodeSpace{Total: 10, OffsetUnavailable: true}, got, "一时没查到 ≠ animego 说算不出来")

	f.offsetErr, f.offset = nil, animego.EpisodeOffset{Known: true, Offset: 28}
	got, err = s.Lookup(context.Background(), 1)
	require.NoError(t, err)
	assert.True(t, got.OffsetKnown)
	assert.Equal(t, 2, f.offsetCalls)
	assert.Equal(t, 1, f.detailCalls)
}

func TestEpisodeSpacesFailsWithoutTotal(t *testing.T) {
	_, err := NewEpisodeSpaces(&spaceStub{detailErr: errors.New("502")}).Lookup(context.Background(), 1)
	assert.Error(t, err)
	_, err = NewEpisodeSpaces(nil).Lookup(context.Background(), 1)
	assert.Error(t, err)
}
