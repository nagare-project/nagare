// LibraryService 维护「已扫描媒体库」的内存态：按目录跑完整解析管线，
// 供 HTTP 层查询视图与按 fileId 取条目。
//
// 管线按【每个库目录独立】执行（与网页端“每次导入一个根”的语义一致）；
// 跨目录的同番合并属于 seriesMatcher 先验复用，留给后续里程碑。
package api

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	errs "github.com/nagare-player/nagare/internal/errors"
	"github.com/nagare-player/nagare/internal/library"
	"github.com/nagare-player/nagare/internal/store"
)

// Stats 是一次扫描的汇总。
type Stats struct {
	Videos   int `json:"videos"`
	Clusters int `json:"clusters"`
}

// ViewProgress 是条目的观看进度投影。
type ViewProgress struct {
	PositionSec float64 `json:"positionSec"`
	DurationSec float64 `json:"durationSec"`
	Completed   bool    `json:"completed"`
}

// ViewItem 是给前端的条目投影。
type ViewItem struct {
	FileID     string        `json:"fileId"`
	FileName   string        `json:"fileName"`
	Episode    *int          `json:"episode"`
	Kind       string        `json:"kind"`
	Resolution *string       `json:"resolution"`
	SizeBytes  int64         `json:"sizeBytes"`
	Progress   *ViewProgress `json:"progress"`
}

// ViewGroup 是目录分组投影。
type ViewGroup struct {
	GroupKey string     `json:"groupKey"`
	Label    string     `json:"label"`
	SortMode string     `json:"sortMode"`
	Items    []ViewItem `json:"items"`
}

// ViewCluster 是剧集簇投影。
type ViewCluster struct {
	ClusterKey   string      `json:"clusterKey"`
	Title        string      `json:"title"`
	Season       *int        `json:"season"`
	Confidence   float64     `json:"confidence"`
	EpisodeCount int         `json:"episodeCount"`
	Groups       []ViewGroup `json:"groups"`
}

// ViewFolder 是库目录投影；Error 非空表示该目录本次扫描失败（部分降级，CQ3）。
type ViewFolder struct {
	ID      string `json:"id"`
	Path    string `json:"path"`
	AddedAt int64  `json:"addedAt"`
	Error   string `json:"error,omitempty"`
}

// LibraryView 是 GET /api/library 的完整数据。
type LibraryView struct {
	Folders   []ViewFolder  `json:"folders"`
	Clusters  []ViewCluster `json:"clusters"`
	ScannedAt *int64        `json:"scannedAt"`
}

type clusterEntry struct {
	cluster    library.Cluster
	confidence float64
}

// LibraryService 并发安全；Rescan 全量重建内存态。
type LibraryService struct {
	st *store.Store

	mu        sync.RWMutex
	folders   []ViewFolder
	clusters  []clusterEntry
	items     map[string]library.Item
	subs      map[string]library.SubtitleRef
	scannedAt int64 // 0 = 从未扫描
}

// NewLibraryService 构造服务（不自动扫描；调用方决定时机）。
func NewLibraryService(st *store.Store) *LibraryService {
	return &LibraryService{
		st:    st,
		items: map[string]library.Item{},
		subs:  map[string]library.SubtitleRef{},
	}
}

// Rescan 重扫全部库目录。单个目录失败只标注该目录，不影响其他目录（CQ3 降级）。
func (s *LibraryService) Rescan() Stats {
	folders := s.st.Snapshot().Folders

	var (
		fviews   []ViewFolder
		clusters []clusterEntry
		items    = map[string]library.Item{}
		subs     = map[string]library.SubtitleRef{}
		stats    Stats
	)
	for _, f := range folders {
		fv := ViewFolder{ID: f.ID, Path: f.Path, AddedAt: f.AddedAt}
		scanned, err := library.ScanDir(f.Path)
		if err != nil {
			fv.Error = fmt.Sprintf("扫描失败：%v", err)
			fviews = append(fviews, fv)
			continue
		}
		var srcs []library.SourceFile
		var subFiles []library.ScannedFile
		for _, sf := range scanned {
			if sf.Kind == "video" {
				srcs = append(srcs, library.SourceFile{
					RelPath: sf.RelPath, AbsPath: sf.AbsPath, Size: sf.Size, MTimeMs: sf.MTimeMs,
				})
			} else {
				subFiles = append(subFiles, sf)
			}
		}
		its := library.BuildItems(srcs)
		for _, it := range its {
			items[it.FileID] = it
		}
		for id, ref := range library.PairSubtitles(its, subFiles) {
			subs[id] = ref
		}
		groups := library.GroupByFolder(its)
		for _, c := range library.Clusterize(groups, nil) {
			verdict := library.MatchCluster(c, nil)
			conf := verdict.Confidence
			clusters = append(clusters, clusterEntry{cluster: c, confidence: conf})
		}
		stats.Videos += len(its)
		fviews = append(fviews, fv)
	}
	stats.Clusters = len(clusters)

	s.mu.Lock()
	s.folders = fviews
	s.clusters = clusters
	s.items = items
	s.subs = subs
	s.scannedAt = time.Now().UnixMilli()
	s.mu.Unlock()
	return stats
}

// AddFolder 校验并添加库目录，随后全量重扫。
func (s *LibraryService) AddFolder(path string) (store.Folder, Stats, error) {
	path = filepath.Clean(path)
	if !filepath.IsAbs(path) {
		return store.Folder{}, Stats{}, errs.New(errs.CategoryInput, "library.addFolder",
			"请输入绝对路径", "例如 /Users/you/Movies/Anime")
	}
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		return store.Folder{}, Stats{}, errs.Wrap(errs.CategoryFS, "library.addFolder",
			"路径不存在或不是目录："+path, "确认路径拼写后重试", err)
	}
	f, err := s.st.AddFolder(path)
	if err != nil {
		return store.Folder{}, Stats{}, errs.Wrap(errs.CategoryInput, "library.addFolder", err.Error(), "", err)
	}
	return f, s.Rescan(), nil
}

// RemoveFolder 移除库目录并重扫；返回是否存在。
func (s *LibraryService) RemoveFolder(id string) (bool, error) {
	ok, err := s.st.RemoveFolder(id)
	if err != nil {
		return false, err
	}
	if ok {
		s.Rescan()
	}
	return ok, nil
}

// Item 按软 id 取条目。
func (s *LibraryService) Item(fileID string) (library.Item, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	it, ok := s.items[fileID]
	return it, ok
}

// SubtitlePath 返回条目配对的外挂字幕绝对路径，无配对返回空串。
func (s *LibraryService) SubtitlePath(fileID string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if ref, ok := s.subs[fileID]; ok {
		return ref.AbsPath
	}
	return ""
}

// View 组装 GET /api/library 的完整视图（进度实时从 store 取）。
func (s *LibraryService) View() LibraryView {
	// 一次性取进度快照：千集规模下逐条目加锁读会把 store 的互斥锁打成热点。
	progress := s.st.Snapshot().Progress

	s.mu.RLock()
	defer s.mu.RUnlock()

	view := LibraryView{Folders: []ViewFolder{}, Clusters: []ViewCluster{}}
	view.Folders = append(view.Folders, s.folders...)
	if s.scannedAt > 0 {
		at := s.scannedAt
		view.ScannedAt = &at
	}

	for _, ce := range s.clusters {
		c := ce.cluster
		vc := ViewCluster{
			ClusterKey: c.ClusterKey,
			Title:      clusterTitle(c),
			Confidence: ce.confidence,
			Groups:     []ViewGroup{},
		}
		if c.Representative != nil {
			vc.Season = c.Representative.ParsedSeason
		}
		mainCount := 0
		for _, it := range c.Items {
			if it.ParsedKind == "main" {
				mainCount++
			}
		}
		if mainCount > 0 {
			vc.EpisodeCount = mainCount
		} else {
			vc.EpisodeCount = len(c.Items)
		}
		for _, g := range c.Groups {
			vg := ViewGroup{GroupKey: g.GroupKey, Label: g.Label, SortMode: g.SortMode, Items: []ViewItem{}}
			for _, it := range g.Items {
				vi := ViewItem{
					FileID:     it.FileID,
					FileName:   it.FileName,
					Episode:    it.Episode,
					Kind:       it.ParsedKind,
					Resolution: it.ParsedResolution,
					SizeBytes:  it.Size,
				}
				if p, ok := progress[it.FileID]; ok {
					vi.Progress = &ViewProgress{
						PositionSec: p.PositionSec, DurationSec: p.DurationSec, Completed: p.Completed,
					}
				}
				vg.Items = append(vg.Items, vi)
			}
			vc.Groups = append(vc.Groups, vg)
		}
		view.Clusters = append(view.Clusters, vc)
	}
	return view
}

func clusterTitle(c library.Cluster) string {
	if c.Representative != nil && c.Representative.ParsedTitle != nil && *c.Representative.ParsedTitle != "" {
		return *c.Representative.ParsedTitle
	}
	if len(c.Groups) > 0 {
		return c.Groups[0].Label
	}
	return c.ClusterKey
}
