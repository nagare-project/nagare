// LibraryService 维护「已扫描媒体库」的内存态：按目录跑完整解析管线，
// 供 HTTP 层查询视图与按 fileId 取条目。
//
// 管线按【每个库目录独立】执行（与网页端“每次导入一个根”的语义一致）；
// 跨目录的同番合并属于 seriesMatcher 先验复用，留给后续里程碑。
package api

import (
	"fmt"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	errs "github.com/nagare-project/nagare/internal/errors"
	"github.com/nagare-project/nagare/internal/library"
	"github.com/nagare-project/nagare/internal/store"
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
	FileID     string  `json:"fileId"`
	FileName   string  `json:"fileName"`
	Episode    *int    `json:"episode"`
	Kind       string  `json:"kind"`
	Resolution *string `json:"resolution"`
	// Group 是文件名里的字幕组（[ANi]、[LoliHouse]…）；同一集有多个版本时界面靠它区分。
	Group     *string `json:"group,omitempty"`
	SizeBytes int64   `json:"sizeBytes"`
	// Stream 是可直接放进 <video src> 的本机地址（浏览器内播放用）。
	// 空串表示媒体端点未挂载。⚠️ 能不能播【完全】取决于浏览器认不认这个编码 ——
	// 后端不转码，只原样喂字节。正常播放路径仍然是 mpv。
	Stream   string        `json:"stream,omitempty"`
	Progress *ViewProgress `json:"progress"`
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
	ClusterKey   string  `json:"clusterKey"`
	Title        string  `json:"title"`
	Season       *int    `json:"season"`
	Confidence   float64 `json:"confidence"`
	EpisodeCount int     `json:"episodeCount"`
	// Cover 是可以直接放进 <img src> 的本机地址，空串表示还没有图。
	// 「还没有图」是常态而非异常：封面来自播放前的 animego 匹配，
	// 一部从未播过的番就是没有。界面必须有无图版式，不能把空串当错误。
	Cover string `json:"cover,omitempty"`
	// Association 是用户手动认定的目录作品；nil 表示没认过，走自动匹配。
	// 认定为某部作品时 Cover 用那部作品的封面。
	Association *ViewAssociation `json:"association,omitempty"`
	// Matched 是没认过的分组里自动匹配到的作品（簇内匹配最多的那一部）；没播过、没匹配上时省略。
	// 界面据此显示「自动匹配：X」并让用户一键确认或更换。
	Matched *ViewMatch  `json:"matched,omitempty"`
	Groups  []ViewGroup `json:"groups"`
}

// ViewContinue 是「继续观看」的一张卡片。
//
// 数据全部来自本地：进度在 store.Progress，标题与封面在 store.Binding。
// 不发任何网络请求 —— 这也是为什么它只包含【播放过的】条目。
type ViewContinue struct {
	FileID       string  `json:"fileId"`
	Title        string  `json:"title"`
	EpisodeTitle string  `json:"episodeTitle,omitempty"`
	Episode      *int    `json:"episode"`
	EpisodeCount int     `json:"episodeCount"`
	Cover        string  `json:"cover,omitempty"`
	PositionSec  float64 `json:"positionSec"`
	DurationSec  float64 `json:"durationSec"`
	UpdatedAt    int64   `json:"updatedAt"`
}

// ViewDropGroup 是「本次扫描按同一个原因跳过了哪些东西」的投影。
//
// Reason 是稳定码（前端分组/测试断言用），Message 与 Recovery 是给用户看的。
// 两者都给，因为只给中文的话前端只能拿中文串当键。
type ViewDropGroup struct {
	Reason   string `json:"reason"`
	Count    int    `json:"count"`
	Message  string `json:"message"`
	Recovery string `json:"recovery"`
	// Samples 是完整路径（已拼上库目录），最多几条。给的是完整路径而不是
	// 相对路径：多个库目录的丢弃会在界面上合并显示，相对路径那时是有歧义的。
	Samples []string `json:"samples"`
}

// ViewDrops 是一次扫描丢掉的全部东西。
type ViewDrops struct {
	Total  int             `json:"total"`
	Groups []ViewDropGroup `json:"groups"`
}

// ViewFolder 是库目录投影；Error 非空表示该目录本次扫描失败（部分降级，CQ3）。
//
// Dropped 是「这次扫描跳过了什么」。nil 表示一个都没跳过——那是常态，
// 界面在这种时候【不出】任何提示：每次扫描都吓用户一跳是另一种病。
type ViewFolder struct {
	ID      string     `json:"id"`
	Path    string     `json:"path"`
	AddedAt int64      `json:"addedAt"`
	Error   string     `json:"error,omitempty"`
	Dropped *ViewDrops `json:"dropped,omitempty"`
}

// toViewDrops 把扫描层的丢弃汇总翻成视图，样本路径拼上库目录变成完整路径。
func toViewDrops(root string, s library.DropSummary) *ViewDrops {
	if s.Total == 0 {
		return nil
	}
	groups := make([]ViewDropGroup, 0, len(s.Groups))
	for _, g := range s.Groups {
		samples := make([]string, 0, len(g.Samples))
		for _, rel := range g.Samples {
			samples = append(samples, filepath.Join(root, filepath.FromSlash(rel)))
		}
		groups = append(groups, ViewDropGroup{
			Reason:   string(g.Reason),
			Count:    g.Count,
			Message:  g.UserMsg,
			Recovery: g.Recovery,
			Samples:  samples,
		})
	}
	return &ViewDrops{Total: s.Total, Groups: groups}
}

// LibraryView 是 GET /api/library 的完整数据。
type LibraryView struct {
	Folders  []ViewFolder  `json:"folders"`
	Clusters []ViewCluster `json:"clusters"`
	// ContinueWatching 按最近观看倒序，最多 continueLimit 条。
	ContinueWatching []ViewContinue `json:"continueWatching"`
	ScannedAt        *int64         `json:"scannedAt"`
	// Identify 是后台作品识别的进度：正在识别或上一轮被打断时才有，其余时候省略。
	// 界面据此显示「正在识别作品」并在识别期间定时刷新（封面一张张出来）。
	Identify *IdentifyStatus `json:"identify,omitempty"`
}

const (
	// continueLimit 是「继续观看」最多显示几条。横向一行放得下的量级；
	// 再多用户也不会横向滚到底，只是徒增首屏数据。
	continueLimit = 12
	// continueMinSec 是进入「继续观看」的最低已看时长。
	// 点开三秒就退出的不算「在看」，那多半是点错了。
	continueMinSec = 30
	// continueDoneRatio 是「看到这个比例就算这一集结束了」。
	// 与 player 的完成判定分开：那边决定要不要回写账号，这里只决定要不要还挂在首页。
	continueDoneRatio = 0.95
)

type clusterEntry struct {
	cluster    library.Cluster
	confidence float64
}

// LibraryService 并发安全；Rescan 全量重建内存态。
type LibraryService struct {
	st *store.Store

	// rescanMu 让重扫串行：扫描结果与关联迁移分两步写进内存和 store，两次重扫交错时
	// 关联可能按一份结果迁、媒体库却装上另一份。
	rescanMu sync.Mutex

	mu       sync.RWMutex
	folders  []ViewFolder
	clusters []clusterEntry
	items    map[string]library.Item
	subs     map[string]library.SubtitleRef
	// fileCluster：fileId → 所在作品分组的 clusterKey（播放时按它查手动关联）
	fileCluster map[string]string
	scannedAt   int64 // 0 = 从未扫描

	// artPrefix 形如 /art/<能力段>；空串表示封面端点未挂载，视图里一律不给封面地址。
	// 由启动流程注入（能力段归 httpserver 生成），不在这里自己造。
	artPrefix string
	// mediaPrefix 形如 /media/<能力段>；空串表示浏览器内播放不可用。
	mediaPrefix string

	// onRescan 在每次重扫完成后调用（启动后台作品识别）；identifyStatus 给视图取识别进度。
	// 都由启动流程注入一次（SetIdentify）。
	onRescan       func()
	identifyStatus func() IdentifyStatus
}

// SetMediaPrefix 注入本地媒体流前缀（形如 /media/<能力段>）。启动时调用一次。
func (s *LibraryService) SetMediaPrefix(prefix string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.mediaPrefix = prefix
}

// SetIdentify 接上后台作品识别：每次重扫完成后 kick 一下，视图里带上 status 给出的进度。
// 启动时调用一次，必须在第一次 Rescan 之前。
func (s *LibraryService) SetIdentify(kick func(), status func() IdentifyStatus) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onRescan, s.identifyStatus = kick, status
}

// SetArtPrefix 注入封面端点前缀（形如 /art/<能力段>）。启动时调用一次。
func (s *LibraryService) SetArtPrefix(prefix string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.artPrefix = prefix
}

// artURL 拼出某个文件的封面地址；没有前缀或该文件没有封面时返回空串。
//
// 给客户端的是 fileId 而不是真实图床地址：客户端只知道键，地址留在服务端 ——
// 这样客户端侧压根没有「让 nagare 去请求任意地址」的入口（SSRF）。
// 封面按 immutable 缓存，而改关联会作废匹配、重新匹配到别的作品：地址带上匹配时间。
func artURL(prefix, fileID string, b store.Binding) string {
	if prefix == "" || b.CoverURL == "" {
		return ""
	}
	return prefix + "/" + url.PathEscape(fileID) + "?v=" + strconv.FormatInt(b.MatchedAt, 10)
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
	s.rescanMu.Lock()
	defer s.rescanMu.Unlock()
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
		res, err := library.ScanDir(f.Path)
		if err != nil {
			fv.Error = fmt.Sprintf("扫描失败：%v", err)
			fviews = append(fviews, fv)
			continue
		}
		fv.Dropped = toViewDrops(f.Path, res.Dropped)
		var srcs []library.SourceFile
		var subFiles []library.ScannedFile
		for _, sf := range res.Files {
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

	fileCluster := map[string]string{}
	for _, ce := range clusters {
		for _, it := range ce.cluster.Items {
			if _, dup := fileCluster[it.FileID]; !dup {
				fileCluster[it.FileID] = ce.cluster.ClusterKey
			}
		}
	}

	s.mu.Lock()
	// 关联迁到新键与换上新的分组索引在同一把锁里：否则这中间开播的文件按旧键查，
	// 关联已经搬走了，就会落回自动匹配（锁序 s.mu → store.mu，store 从不回调这里）。
	s.syncAssociations(clusters)
	s.folders = fviews
	s.clusters = clusters
	s.items = items
	s.subs = subs
	s.fileCluster = fileCluster
	s.scannedAt = time.Now().UnixMilli()
	kick := s.onRescan
	s.mu.Unlock()
	if kick != nil {
		kick()
	}
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

// View 组装 GET /api/library 的完整视图（进度与匹配结果实时从 store 取）。
func (s *LibraryService) View() LibraryView {
	// 一次性取快照：千集规模下逐条目加锁读会把 store 的互斥锁打成热点。
	snap := s.st.Snapshot()
	progress, bindings, assocs := snap.Progress, snap.Bindings, snap.Associations
	identify := s.identifySnapshot()

	s.mu.RLock()
	defer s.mu.RUnlock()

	prefix := s.artPrefix
	mediaPrefix := s.mediaPrefix
	view := LibraryView{
		Folders:          []ViewFolder{},
		Clusters:         []ViewCluster{},
		ContinueWatching: []ViewContinue{},
	}
	view.Folders = append(view.Folders, s.folders...)
	if s.scannedAt > 0 {
		at := s.scannedAt
		view.ScannedAt = &at
	}

	for _, ce := range mergeByKey(s.clusters) {
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
		// 簇封面取簇内【任意一个】已匹配条目的封面：同一部番的每一集
		// 匹配回来的都是同一张图，取第一个有的即可，不必挑「代表集」。
		// 认定过作品时只看与它一致的匹配（后台匹配迟到时可能写回一条指向别处的）。
		a, hasAssoc := assocs[c.ClusterKey]
		for _, it := range c.Items {
			b := bindings[it.FileID]
			if hasAssoc && b.AnilistID != a.AnilistID {
				continue
			}
			if u := artURL(prefix, it.FileID, b); u != "" {
				vc.Cover = u
				break
			}
		}
		if hasAssoc {
			vc.Association = toViewAssociation(a)
			vc.Cover = associatedCover(prefix, c.ClusterKey, a, vc.Cover)
		} else {
			vc.Matched = matchedWork(c.Items, bindings)
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
					Group:      it.ParsedGroup,
					SizeBytes:  it.Size,
				}
				if mediaPrefix != "" {
					vi.Stream = mediaPrefix + "/" + url.PathEscape(it.FileID)
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
	view.ContinueWatching = s.continueWatching(prefix, progress, bindings, assocs)
	if identify.Running || identify.Error != "" {
		view.Identify = &identify
	}
	return view
}

// identifySnapshot 取后台识别的进度。在拿 s.mu 之前调用：识别器的锁不该嵌进媒体库的锁里。
func (s *LibraryService) identifySnapshot() IdentifyStatus {
	s.mu.RLock()
	status := s.identifyStatus
	s.mu.RUnlock()
	if status == nil {
		return IdentifyStatus{}
	}
	return status()
}

// continueWatching 组装「继续观看」：看过一点、又没看完的条目，按最近观看倒序。
//
// 调用方必须已持有 s.mu 读锁（本函数读 s.clusters）。
func (s *LibraryService) continueWatching(
	prefix string,
	progress map[string]store.Progress,
	bindings map[string]store.Binding,
	assocs map[string]store.Association,
) []ViewContinue {
	// 集数取自所属簇：Binding 里没有总集数，而「第 5 集 / 共 13 集」
	// 里的分母正是用户判断还剩多少的依据。
	// 按 clusterKey 合并之后再算：两块盘上一模一样的副本只出一张卡，集数也按合并后的算
	clusters := mergeByKey(s.clusters)
	total := map[string]int{}
	for _, ce := range clusters {
		n := 0
		for _, it := range ce.cluster.Items {
			if it.ParsedKind == "main" {
				n++
			}
		}
		if n == 0 {
			n = len(ce.cluster.Items)
		}
		for _, it := range ce.cluster.Items {
			total[it.FileID] = n
		}
	}

	out := []ViewContinue{}
	for _, ce := range clusters {
		for _, it := range ce.cluster.Items {
			p, ok := progress[it.FileID]
			if !ok || p.Completed || p.PositionSec < continueMinSec {
				continue
			}
			// 已经看到尾巴的不再挂在首页：即使 Completed 还没置位
			//（用户在片尾手动退出，没触发 EOF），它对用户也已经"看完了"。
			if p.DurationSec > 0 && p.PositionSec/p.DurationSec >= continueDoneRatio {
				continue
			}
			b := bindings[it.FileID]
			// 手动认定的作品优先：自动匹配的标题、集标题与封面可能正是认错的那部
			if a, ok := assocs[ce.cluster.ClusterKey]; ok && b.AnilistID != a.AnilistID {
				b = store.Binding{}
			}
			title, cover := b.Title, artURL(prefix, it.FileID, b)
			if a, ok := assocs[ce.cluster.ClusterKey]; ok {
				title = a.Title
				cover = associatedCover(prefix, ce.cluster.ClusterKey, a, cover)
			}
			if title == "" {
				title = clusterTitle(ce.cluster)
			}
			out = append(out, ViewContinue{
				FileID:       it.FileID,
				Title:        title,
				EpisodeTitle: b.EpisodeTitle,
				Episode:      it.Episode,
				EpisodeCount: total[it.FileID],
				Cover:        cover,
				PositionSec:  p.PositionSec,
				DurationSec:  p.DurationSec,
				UpdatedAt:    p.UpdatedAt,
			})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt > out[j].UpdatedAt })
	if len(out) > continueLimit {
		out = out[:continueLimit]
	}
	return out
}

// mergeByKey 把同一个 clusterKey 的分组并成一个（同一部番分在两个库目录里时会出现两次）：
// 视图里一个键只能有一张海报 —— 前端按键渲染列表、按键找作品页，重复的键会让第二个永远点不到。
// 合并时保留第一次出现的位置与代表集，文件与目录分组按出现顺序接上，置信度取高的。
func mergeByKey(clusters []clusterEntry) []clusterEntry {
	out := make([]clusterEntry, 0, len(clusters))
	at := map[string]int{}
	for _, ce := range clusters {
		i, ok := at[ce.cluster.ClusterKey]
		if !ok {
			at[ce.cluster.ClusterKey] = len(out)
			out = append(out, ce)
			continue
		}
		m := &out[i]
		// 两个库目录里一模一样的副本是同一个 fileId（名字|大小|修改时间）：只留一份
		seen := map[string]bool{}
		for _, it := range m.cluster.Items {
			seen[it.FileID] = true
		}
		items := append([]library.Item(nil), m.cluster.Items...)
		groups := append([]library.Group(nil), m.cluster.Groups...)
		for _, g := range ce.cluster.Groups {
			kept := make([]library.Item, 0, len(g.Items))
			for _, it := range g.Items {
				if !seen[it.FileID] {
					seen[it.FileID] = true
					kept = append(kept, it)
				}
			}
			if len(kept) == 0 {
				continue
			}
			items = append(items, kept...)
			// 两块盘上同名的子目录得到同一个 groupKey（目录键相对各自的库根）：并进同一个分组，
			// 界面按 groupKey 渲染列表，重复的键会让 React 丢失或重复兄弟节点
			if j := slices.IndexFunc(groups, func(x library.Group) bool { return x.GroupKey == g.GroupKey }); j >= 0 {
				merged := groups[j]
				merged.Items = append(append([]library.Item(nil), merged.Items...), kept...)
				sortGroupItems(merged.Items)
				groups[j] = merged
				continue
			}
			g.Items = kept
			groups = append(groups, g)
		}
		m.cluster.Items, m.cluster.Groups = items, groups
		if m.cluster.Representative == nil {
			m.cluster.Representative = ce.cluster.Representative
		}
		m.confidence = max(m.confidence, ce.confidence)
	}
	return out
}

// sortGroupItems 按集号升序（没有集号的排最后），同集号按文件名 —— 与扫描时分组内的排序一致。
func sortGroupItems(items []library.Item) {
	slices.SortStableFunc(items, func(a, b library.Item) int {
		ea, eb := episodeOrMax(a), episodeOrMax(b)
		if ea != eb {
			return ea - eb
		}
		return strings.Compare(a.FileName, b.FileName)
	})
}

func episodeOrMax(it library.Item) int {
	if it.Episode == nil {
		return math.MaxInt
	}
	return *it.Episode
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
