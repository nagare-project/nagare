package api

import (
	"context"
	"errors"
	"fmt"
	"github.com/nagare-project/nagare/internal/animego"
	"sync"
	"time"
)

// ListsBackend 只暴露收藏和账号代次，避免测试构造真实 HTTP 客户端。
type ListsBackend interface {
	LoggedIn() bool
	SessionGeneration() uint64
	ListEntries(context.Context) ([]animego.ListEntry, error)
	GetListEntry(context.Context, int) (*animego.ListEntry, error)
	SaveListEntry(context.Context, int, string, int, *int) error
	DeleteListEntry(context.Context, int) error
}
type ListEntryView struct {
	AnilistID      int          `json:"anilistId"`
	Status         string       `json:"status"`
	CurrentEpisode int          `json:"currentEpisode"`
	Score          *int         `json:"score"`
	Media          SummaryMedia `json:"media"`
	LastWatchedAt  int64        `json:"lastWatchedAt,omitempty"`
}
type ListsView struct {
	LoggedIn bool            `json:"loggedIn"`
	Entries  []ListEntryView `json:"entries"`
}
type ListsService struct {
	source   ListsBackend
	catalog  *DiscoverService
	cache    *readCache
	mu       sync.Mutex
	revision uint64
	editing  chan struct{}
}

func NewListsService(source ListsBackend, catalog *DiscoverService) *ListsService {
	return &ListsService{source: source, catalog: catalog, cache: newReadCache(), editing: make(chan struct{}, 1)}
}
func (s *ListsService) Invalidate()     { s.mu.Lock(); s.revision++; s.mu.Unlock() }
func (s *ListsService) version() uint64 { s.mu.Lock(); defer s.mu.Unlock(); return s.revision }
func (s *ListsService) entry(e animego.ListEntry) ListEntryView {
	m := s.catalog.summary(animego.CatalogMedia{AnilistID: e.AnilistID, TitleChinese: e.TitleChinese, TitleRomaji: e.TitleRomaji, TitleNative: e.TitleNative, CoverImageURL: e.CoverImageURL, BannerImageURL: e.BannerImageURL, Episodes: e.Episodes, Season: e.Season, SeasonYear: e.SeasonYear, Status: e.AnimeStatus})
	at := int64(0)
	if t, err := time.Parse(time.RFC3339, e.LastWatchedAt); err == nil {
		at = t.Unix()
	}
	return ListEntryView{e.AnilistID, e.Status, e.CurrentEpisode, e.Score, m, at}
}
func (s *ListsService) View(ctx context.Context) (ListsView, error) {
	empty := ListsView{Entries: []ListEntryView{}}
	if s.source == nil || !s.source.LoggedIn() {
		return empty, nil
	}
	epoch, revision := s.source.SessionGeneration(), s.version()
	rows, err := cachedRead(ctx, s.cache, fmt.Sprintf("%d:%d", epoch, revision), time.Minute, s.source.ListEntries)
	if epoch != s.source.SessionGeneration() || revision != s.version() {
		if !s.source.LoggedIn() {
			return empty, nil
		}
		return empty, &animego.Error{Kind: animego.ErrUnavailable, Op: "list", Err: fmt.Errorf("账号或收藏已变更，请刷新")}
	}
	if err != nil {
		var ae *animego.Error
		if errors.As(err, &ae) && ae.Kind == animego.ErrAuthExpired {
			s.Invalidate()
			return empty, nil
		}
		return empty, err
	}
	empty.LoggedIn = true
	for _, e := range rows {
		switch e.Status {
		case "watching", "completed", "plan_to_watch", "dropped":
		default:
			continue
		}
		v := s.entry(e)
		if v.Media.Title != "" {
			empty.Entries = append(empty.Entries, v)
		}
	}
	return empty, nil
}
func (s *ListsService) mutate(ctx context.Context, id int, status string, progress int, score *int, remove bool) (*ListEntryView, error) {
	if s.source == nil || !s.source.LoggedIn() {
		return nil, &animego.Error{Kind: animego.ErrAuthExpired, Op: "list", Err: fmt.Errorf("请先在设置中登录账号")}
	}
	// 逐集撤销与 PATCH 是一项复合编辑；拒绝重叠编辑，避免后来的保存插进前一个操作。
	select {
	case s.editing <- struct{}{}:
		defer func() { <-s.editing }()
	default:
		return nil, &animego.Error{Kind: animego.ErrRateLimited, Op: "list", Err: fmt.Errorf("已有收藏正在保存，请稍候")}
	}
	epoch := s.source.SessionGeneration()
	defer s.Invalidate()
	var err error
	if remove {
		err = s.source.DeleteListEntry(ctx, id)
	} else {
		err = s.source.SaveListEntry(ctx, id, status, progress, score)
	}
	if err != nil {
		return nil, err
	}
	if epoch != s.source.SessionGeneration() {
		return nil, &animego.Error{Kind: animego.ErrAuthExpired, Op: "list", Err: fmt.Errorf("账号已变更，请刷新确认保存结果")}
	}
	if remove {
		return nil, nil
	}
	entry, err := s.source.GetListEntry(ctx, id)
	if err != nil {
		return nil, &animego.Error{Kind: animego.ErrUnavailable, Op: "list", Err: fmt.Errorf("收藏写入已完成，重新读取结果失败，请刷新核对：%w", err)}
	}
	if epoch != s.source.SessionGeneration() {
		return nil, &animego.Error{Kind: animego.ErrAuthExpired, Op: "list", Err: fmt.Errorf("账号已变更，请刷新")}
	}
	if entry == nil {
		return nil, &animego.Error{Kind: animego.ErrDecode, Op: "list", Err: fmt.Errorf("收藏写入后未读到条目，请刷新")}
	}
	view := s.entry(*entry)
	return &view, nil
}
