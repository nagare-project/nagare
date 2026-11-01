package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nagare-project/nagare/internal/animego"
	"github.com/nagare-project/nagare/internal/artcache"
	"github.com/stretchr/testify/require"
)

const diskCoverURL = "https://s4.anilist.co/cover-7.jpg"

// diskStub 是可切换「在线 / 断网」与返回标题的目录上游，不发任何网络请求。
type diskStub struct {
	mu    sync.Mutex
	title string
	fail  bool
	calls int
}

func (f *diskStub) hit() (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.fail {
		return "", errors.New("dial tcp: 网络不可达")
	}
	return f.title, nil
}
func (f *diskStub) callCount() int { f.mu.Lock(); defer f.mu.Unlock(); return f.calls }
func (f *diskStub) media(title string) animego.CatalogMedia {
	return animego.CatalogMedia{AnilistID: 7, TitleChinese: title, Format: "TV", Status: "RELEASING", CoverImageURL: diskCoverURL}
}
func (f *diskStub) rows(context.Context) ([]animego.CatalogMedia, error) {
	title, err := f.hit()
	if err != nil {
		return nil, err
	}
	return []animego.CatalogMedia{f.media(title)}, nil
}
func (f *diskStub) Trending(ctx context.Context) ([]animego.CatalogMedia, error) { return f.rows(ctx) }
func (f *diskStub) Seasonal(ctx context.Context, _ string, _ int) ([]animego.CatalogMedia, error) {
	return f.rows(ctx)
}
func (f *diskStub) Gems(ctx context.Context) ([]animego.CatalogMedia, error) { return f.rows(ctx) }
func (f *diskStub) YearlyTop(ctx context.Context, _ int) ([]animego.CatalogMedia, error) {
	return f.rows(ctx)
}
func (f *diskStub) Search(ctx context.Context, _ string) ([]animego.CatalogMedia, error) {
	return f.rows(ctx)
}
func (f *diskStub) Detail(_ context.Context, id int) (animego.CatalogMedia, error) {
	title, err := f.hit()
	if err != nil {
		return animego.CatalogMedia{}, err
	}
	m := f.media(title)
	m.AnilistID = id
	return m, nil
}
func (f *diskStub) Schedule(context.Context) (animego.ScheduleData, error) {
	title, err := f.hit()
	if err != nil {
		return animego.ScheduleData{}, err
	}
	item := animego.ScheduleItem{CatalogMedia: f.media(title), Episode: 2, AiringAt: diskT0.Add(-time.Hour).Unix()}
	return animego.ScheduleData{Groups: map[string][]animego.ScheduleItem{"d": {item}}}, nil
}

// diskClock 是加锁的假时钟：后台刷新会在别的 goroutine 里读它。
type diskClock struct {
	mu sync.Mutex
	at time.Time
}

func (c *diskClock) now() time.Time  { c.mu.Lock(); defer c.mu.Unlock(); return c.at }
func (c *diskClock) set(t time.Time) { c.mu.Lock(); c.at = t; c.mu.Unlock() }

var diskT0 = time.Date(2026, 8, 15, 10, 0, 0, 0, time.UTC)

// diskService 模拟一次进程启动：全新的内存缓存与图片登记表，只有磁盘目录是上次留下的。
func diskService(t *testing.T, dir string, f CatalogReader, at time.Time) (*DiscoverService, *diskClock) {
	t.Helper()
	a := NewRemoteArt("https://catalog.example", 64)
	a.SetPrefix("/art/test")
	s := NewDiscoverService(f, a)
	clock := &diskClock{at: at}
	s.cache.now = clock.now
	d, err := newCatalogDisk(dir, catalogDiskMaxBytes)
	require.NoError(t, err)
	s.useDisk(d)
	return s, clock
}

func TestCatalogDiskKeyWhitelist(t *testing.T) {
	for key, want := range map[string]bool{
		"trending:2026:2": true, "thisSeason:2026:2": true, "pastSeason:2026:2": true, "nextSeason:2026:2": true,
		"gems:2026:2": true, "yearly:2026:2": true, "seasonal:FALL:2024": true, "schedule": true, "detail:7": true,
		"search:芙莉莲": false, "1:0": false, "": false, "schedule:x": false, "lists": false, "total:7": false,
	} {
		require.Equal(t, want, catalogDiskKey(key), key)
	}
}

// 断网重启：发现页、详情、季度、放送都先给盘上的上一份，带上取回时间；
// 后台刷新失败后标记离线，一分钟内不再撞上游。
func TestCatalogOfflineRestartServesDiskCopy(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	online, _ := diskService(t, dir, &diskStub{title: "上次的标题"}, diskT0)
	first, err := online.View(ctx)
	require.NoError(t, err)
	require.Nil(t, first.Freshness, "刚从上游取回的不带 freshness")
	_, err = online.Detail(ctx, 7)
	require.NoError(t, err)
	_, err = online.Seasonal(ctx, "WINTER", 2024)
	require.NoError(t, err)

	down := &diskStub{fail: true}
	s, _ := diskService(t, dir, down, diskT0.Add(2*time.Hour))
	view, err := s.View(ctx)
	require.NoError(t, err)
	for _, sec := range view.Sections {
		require.Empty(t, sec.Error, sec.Key)
	}
	require.Len(t, view.Sections[0].Items, 1)
	require.Equal(t, "上次的标题", view.Sections[0].Items[0].Title)
	require.Equal(t, &Freshness{FetchedAt: "2026-08-15T10:00:00Z", Stale: true}, view.Freshness)
	require.Equal(t, diskT0.Unix(), view.FetchedAt)

	s.cache.waitBackground()
	require.Positive(t, down.callCount(), "过期的一份要在后台刷新")
	again, err := s.View(ctx)
	require.NoError(t, err)
	require.True(t, again.Freshness.Offline, "刷新失败后标记离线")
	require.Len(t, again.Sections[2].Items, 1)
	calls := down.callCount()
	_, err = s.View(ctx)
	require.NoError(t, err)
	s.cache.waitBackground()
	require.Equal(t, calls, down.callCount(), "退避期内不再重试")

	detail, err := s.Detail(ctx, 7)
	require.NoError(t, err)
	require.Equal(t, "上次的标题", detail.Title)
	require.Equal(t, "2026-08-15T10:00:00Z", detail.Freshness.FetchedAt)
	require.True(t, detail.Freshness.Stale)
	seasonal, err := s.Seasonal(ctx, "WINTER", 2024)
	require.NoError(t, err)
	require.Len(t, seasonal.Items, 1)
	require.NotNil(t, seasonal.Freshness)
	schedule, err := s.ScheduleView(ctx, nil)
	require.NoError(t, err)
	require.Len(t, schedule.Airings, 1)
	require.Equal(t, "2026-08-15T10:00:00Z", schedule.Freshness.FetchedAt)
	s.cache.waitBackground()

	// 盘上从没有过的作品：照常报错，不编造
	_, err = s.Detail(ctx, 99)
	require.Error(t, err)
}

// 缓存时限内重启：直接用盘上的，不出站；freshness 说明它来自上次运行但不算过期。
func TestCatalogDiskWithinTTLSkipsNetwork(t *testing.T) {
	dir := t.TempDir()
	online, _ := diskService(t, dir, &diskStub{title: "作品"}, diskT0)
	_, err := online.Detail(context.Background(), 7)
	require.NoError(t, err)

	down := &diskStub{fail: true}
	s, _ := diskService(t, dir, down, diskT0.Add(5*time.Minute))
	m, err := s.Detail(context.Background(), 7)
	require.NoError(t, err)
	s.cache.waitBackground()
	require.Zero(t, down.callCount())
	require.Equal(t, &Freshness{FetchedAt: "2026-08-15T10:00:00Z"}, m.Freshness)
}

// 后台刷新成功：新的一份替换盘上的旧文件，下一次读取就是新的；再重启读到的也是新的。
func TestCatalogBackgroundRefreshReplacesDiskCopy(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	old, _ := diskService(t, dir, &diskStub{title: "旧"}, diskT0)
	_, err := old.Detail(ctx, 7)
	require.NoError(t, err)

	later := diskT0.Add(time.Hour)
	up := &diskStub{title: "新"}
	s, _ := diskService(t, dir, up, later)
	m, err := s.Detail(ctx, 7)
	require.NoError(t, err)
	require.Equal(t, "旧", m.Title, "先给盘上的，不等上游")
	require.True(t, m.Freshness.Stale)
	s.cache.waitBackground()
	require.Equal(t, 1, up.callCount())

	raw, fetchedAt, ok := s.cache.disk.load("detail:7")
	require.True(t, ok)
	require.True(t, fetchedAt.Equal(later))
	require.Contains(t, string(raw), "新")

	m, err = s.Detail(ctx, 7)
	require.NoError(t, err)
	require.Equal(t, "新", m.Title)
	require.Nil(t, m.Freshness)

	restarted, _ := diskService(t, dir, &diskStub{fail: true}, later.Add(time.Minute))
	m, err = restarted.Detail(ctx, 7)
	require.NoError(t, err)
	require.Equal(t, "新", m.Title)
	require.Equal(t, later.Format(time.RFC3339), m.Freshness.FetchedAt)
}

// 版本不符、损坏、键不符、结构对不上的文件：丢掉，回到网络，并用新的一份覆盖。
func TestCatalogDiskDiscardsUnusableFiles(t *testing.T) {
	envelope := func(f catalogDiskFile) []byte {
		b, err := json.Marshal(f)
		require.NoError(t, err)
		return b
	}
	good := json.RawMessage(`{"anilistId":7,"titleChinese":"盘上"}`)
	cases := map[string][]byte{
		"版本不符": envelope(catalogDiskFile{Version: catalogDiskVersion + 1, Key: "detail:7", FetchedAt: diskT0, Data: good}),
		"旧版本":  envelope(catalogDiskFile{Version: 0, Key: "detail:7", FetchedAt: diskT0, Data: good}),
		"损坏":   []byte(`{"version":1,"key":"detail:7","data":{`),
		"键不符":  envelope(catalogDiskFile{Version: catalogDiskVersion, Key: "detail:8", FetchedAt: diskT0, Data: good}),
		"结构不符": envelope(catalogDiskFile{Version: catalogDiskVersion, Key: "detail:7", FetchedAt: diskT0, Data: json.RawMessage(`"字符串"`)}),
		"缺时间":  envelope(catalogDiskFile{Version: catalogDiskVersion, Key: "detail:7", Data: good}),
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(dir, catalogDiskName("detail:7")), content, 0o600))
			up := &diskStub{title: "网络"}
			now := diskT0.Add(time.Minute)
			s, _ := diskService(t, dir, up, now)
			m, err := s.Detail(context.Background(), 7)
			require.NoError(t, err)
			require.Equal(t, "网络", m.Title)
			require.Nil(t, m.Freshness)
			require.Equal(t, 1, up.callCount())

			raw, fetchedAt, ok := s.cache.disk.load("detail:7")
			require.True(t, ok, "已被新的一份覆盖")
			require.True(t, fetchedAt.Equal(now))
			require.Contains(t, string(raw), "网络")
		})
	}
}

// 超过总上限按最近使用淘汰；重启后按 mtime 重建的顺序与运行时一致。
func TestCatalogDiskEvictsLeastRecentlyUsed(t *testing.T) {
	require.Equal(t, int64(50<<20), int64(catalogDiskMaxBytes))
	dir := t.TempDir()
	clock := &diskClock{at: diskT0}
	d, err := newCatalogDisk(dir, catalogDiskMaxBytes)
	require.NoError(t, err)
	d.now = clock.now
	payload := map[string]string{"x": strings.Repeat("目", 300)}
	d.store("detail:1", payload, diskT0)
	size := d.total
	require.Positive(t, size)
	d.maxBytes = 3*size + size/2 // 放得下三个，放不下四个

	clock.set(diskT0.Add(1 * time.Minute))
	d.store("detail:2", payload, diskT0)
	clock.set(diskT0.Add(2 * time.Minute))
	d.store("detail:3", payload, diskT0)
	clock.set(diskT0.Add(3 * time.Minute))
	_, _, ok := d.load("detail:1") // 用过一次：不再是最旧的
	require.True(t, ok)
	clock.set(diskT0.Add(4 * time.Minute))
	d.store("detail:4", payload, diskT0)

	exists := func(key string) bool {
		_, err := os.Stat(filepath.Join(dir, catalogDiskName(key)))
		return err == nil
	}
	require.True(t, exists("detail:1"))
	require.False(t, exists("detail:2"), "最久没用的被淘汰")
	require.True(t, exists("detail:3"))
	require.True(t, exists("detail:4"))
	require.Equal(t, 3*size, d.total)

	restarted, err := newCatalogDisk(dir, 2*size+size/2)
	require.NoError(t, err)
	require.Equal(t, 2*size, restarted.total)
	require.False(t, exists("detail:3"), "重启后按 mtime 淘汰最旧的")
	require.True(t, exists("detail:1"))
	require.True(t, exists("detail:4"))

	// 单条超过总上限的不写
	restarted.store("detail:5", map[string]string{"x": strings.Repeat("大", int(size))}, diskT0)
	require.False(t, exists("detail:5"))
	require.Equal(t, 2*size, restarted.total)
}

// 收藏与搜索永远不落盘；收藏的缓存实例连磁盘层都没有。
func TestCatalogDiskNeverStoresListsOrSearch(t *testing.T) {
	dir := t.TempDir()
	const marker = "只在收藏里出现的标题"
	lists := &listStub{read: func() ([]animego.ListEntry, error) {
		return []animego.ListEntry{{AnilistID: 42, TitleChinese: marker, Status: "watching"}}, nil
	}}
	lists.epoch.Store(1)
	h := New(Deps{Catalog: &diskStub{title: "公开作品"}, Lists: lists, CatalogCacheDir: dir})
	require.NotNil(t, h.catalog.cache.disk)
	require.Nil(t, h.lists.cache.disk)

	w := httptest.NewRecorder()
	h.catalogList(w, httptest.NewRequest("GET", "/api/lists", nil))
	require.Equal(t, 200, w.Code)
	require.Contains(t, w.Body.String(), marker)
	_, err := h.catalog.Search(context.Background(), "关键词")
	require.NoError(t, err)
	w = httptest.NewRecorder()
	h.catalogDiscover(w, httptest.NewRequest("GET", "/api/discover", nil))
	require.Equal(t, 200, w.Code)

	files, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.NotEmpty(t, files)
	for _, f := range files {
		content, err := os.ReadFile(filepath.Join(dir, f.Name()))
		require.NoError(t, err)
		var file catalogDiskFile
		require.NoError(t, json.Unmarshal(content, &file))
		require.True(t, catalogDiskKey(file.Key), file.Key)
		require.NotContains(t, string(content), marker)
		require.NotContains(t, string(content), "关键词")
	}
}

// 重启之后图片登记表是空的；盘上的目录重新投影时按同一个键登记回来，
// 已缓存的封面离线照样能从 /art/<能力段>/remote/<键> 出来。
func TestCatalogArtResolvesAfterRestartOffline(t *testing.T) {
	dir := t.TempDir()
	online, _ := diskService(t, dir, &diskStub{title: "作品"}, diskT0)
	before, err := online.View(context.Background())
	require.NoError(t, err)
	cover := before.Sections[0].Items[0].Cover
	require.True(t, strings.HasPrefix(cover, "/art/test/remote/"), cover)
	key := strings.TrimPrefix(cover, "/art/test/remote/")

	art, err := artcache.New(t.TempDir())
	require.NoError(t, err)
	image := []byte("\x89PNG\r\n\x1a\n已缓存的封面")
	require.NoError(t, os.WriteFile(art.Path(diskCoverURL), image, 0o600))

	s, _ := diskService(t, dir, &diskStub{fail: true}, diskT0.Add(3*time.Hour))
	_, known := s.art.Source(key)
	require.False(t, known, "新进程的登记表从空开始")
	after, err := s.View(context.Background())
	require.NoError(t, err)
	s.cache.waitBackground()
	require.Equal(t, cover, after.Sections[0].Items[0].Cover, "键是地址的 SHA-256，跨进程不变")

	w := httptest.NewRecorder()
	NewArtHandler(nil, art, s.art).ServeHTTP(w, httptest.NewRequest("GET", "/remote/"+key, nil))
	require.Equal(t, 200, w.Code)
	require.Equal(t, image, w.Body.Bytes())
}

// 原子写：目录 0700、文件 0600、不留临时文件；上次残留的临时文件启动时清掉。
func TestCatalogDiskAtomicWritePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows 没有 Unix 权限位")
	}
	dir := filepath.Join(t.TempDir(), "cache", "catalog")
	require.NoError(t, os.MkdirAll(dir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, catalogDiskTempPrefix+"123"), []byte("半截"), 0o600))
	d, err := newCatalogDisk(dir, catalogDiskMaxBytes)
	require.NoError(t, err)
	d.store("schedule", map[string]int{"a": 1}, diskT0)

	info, err := os.Stat(dir)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o700), info.Mode().Perm())
	files, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, files, 1)
	require.Equal(t, catalogDiskName("schedule"), files[0].Name())
	fi, err := files[0].Info()
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), fi.Mode().Perm())
}

// 时钟回拨过：盘上的「未来」副本当作过期，后台去刷新。
func TestCatalogDiskFutureCopyIsStale(t *testing.T) {
	dir := t.TempDir()
	future, _ := diskService(t, dir, &diskStub{title: "未来"}, diskT0.Add(24*time.Hour))
	_, err := future.Detail(context.Background(), 7)
	require.NoError(t, err)

	up := &diskStub{title: "现在"}
	s, _ := diskService(t, dir, up, diskT0)
	m, err := s.Detail(context.Background(), 7)
	require.NoError(t, err)
	require.True(t, m.Freshness.Stale)
	s.cache.waitBackground()
	require.Equal(t, 1, up.callCount())
}
