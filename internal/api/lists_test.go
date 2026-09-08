package api

import (
	"context"
	"errors"
	"github.com/nagare-project/nagare/internal/animego"
	"github.com/stretchr/testify/require"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

type listStub struct {
	epoch   atomic.Uint64
	calls   atomic.Int32
	read    func() ([]animego.ListEntry, error)
	saveErr error
}

func (f *listStub) LoggedIn() bool            { return f.epoch.Load() > 0 }
func (f *listStub) SessionGeneration() uint64 { return f.epoch.Load() }
func (f *listStub) ListEntries(context.Context) ([]animego.ListEntry, error) {
	f.calls.Add(1)
	if f.read != nil {
		return f.read()
	}
	return []animego.ListEntry{{AnilistID: 1, TitleChinese: "作品", Status: "watching"}}, nil
}
func (f *listStub) GetListEntry(context.Context, int) (*animego.ListEntry, error) {
	return &animego.ListEntry{AnilistID: 1, TitleChinese: "作品", Status: "watching"}, nil
}
func (f *listStub) SaveListEntry(context.Context, int, string, int, *int) error { return f.saveErr }
func (f *listStub) DeleteListEntry(context.Context, int) error                  { return f.saveErr }
func TestListsAnonymousReadsButCannotWrite(t *testing.T) {
	h := New(Deps{})
	w := httptest.NewRecorder()
	h.catalogList(w, httptest.NewRequest("GET", "/api/lists", nil))
	require.Equal(t, 200, w.Code)
	require.Contains(t, w.Body.String(), `"loggedIn":false`)
	r := httptest.NewRequest("POST", "/api/lists/1", strings.NewReader(`{"status":"watching"}`))
	r.SetPathValue("id", "1")
	w = httptest.NewRecorder()
	h.catalogListSave(w, r)
	require.Equal(t, 401, w.Code)
	require.Contains(t, w.Body.String(), "请先")
}
func TestListsInvalidatesAfterSuccessPartialFailureAndAccountSwitch(t *testing.T) {
	f := &listStub{}
	f.epoch.Store(1)
	s := NewListsService(f, catalogService(&catalogStub{}))
	ctx := context.Background()
	for range 2 {
		_, err := s.View(ctx)
		require.NoError(t, err)
	}
	require.Equal(t, int32(1), f.calls.Load())
	_, err := s.mutate(ctx, 1, "watching", 1, nil, false)
	require.NoError(t, err)
	_, err = s.View(ctx)
	require.NoError(t, err)
	require.Equal(t, int32(2), f.calls.Load())
	f.saveErr = &animego.ListEditError{Err: &animego.Error{Kind: animego.ErrRateLimited, Err: errors.New("限速")}, Removed: []int{5}, Remaining: []int{3}, LastConfirmed: 3, Stage: "撤销进度"}
	_, err = s.mutate(ctx, 1, "watching", 1, nil, false)
	require.Error(t, err)
	w := httptest.NewRecorder()
	writeErr(w, err)
	require.Equal(t, 429, w.Code)
	require.Contains(t, w.Body.String(), "第 3 集")
	require.Contains(t, w.Body.String(), "[5]")
	_, err = s.View(ctx)
	require.NoError(t, err)
	require.Equal(t, int32(3), f.calls.Load())
	f.epoch.Store(2)
	_, err = s.View(ctx)
	require.NoError(t, err)
	require.Equal(t, int32(4), f.calls.Load())
}
func TestListsDropsOldAccountResponse(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	f := &listStub{read: func() ([]animego.ListEntry, error) {
		close(entered)
		<-release
		return []animego.ListEntry{{AnilistID: 1, TitleChinese: "旧账号", Status: "watching"}}, nil
	}}
	f.epoch.Store(1)
	s := NewListsService(f, catalogService(&catalogStub{}))
	done := make(chan struct{})
	var view ListsView
	var err error
	go func() { view, err = s.View(context.Background()); close(done) }()
	<-entered
	f.epoch.Store(2)
	close(release)
	<-done
	require.Error(t, err)
	require.Empty(t, view.Entries)
}
