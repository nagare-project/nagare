package torrentstream

import (
	"context"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nagare-project/nagare/internal/library"
	"github.com/nagare-project/nagare/internal/store"
)

// 下载器的测试与边下边播的集成测试同一个原则：同进程做种、全程不出网。
// 注入是替换包级变量 newDownloadClient，所以这些用例同样不能并行。

func offlineDownloadClient(bool) (*torrent.Client, error) {
	tc := torrent.NewDefaultClientConfig()
	tc.DefaultStorage = storage.NewFileOpts(storage.NewFileClientOpts{
		ClientBaseDir:   os.TempDir(),
		PieceCompletion: storage.NewMapPieceCompletion(),
	})
	tc.ListenPort = 0
	tc.NoDefaultPortForwarding = true
	tc.NoDHT = true
	tc.DisableTrackers = true
	tc.DisablePEX = true
	tc.DisableIPv6 = true // 同 startSeeder：只走 127.0.0.1
	tc.Slogger = slog.New(slog.DiscardHandler)
	return torrent.NewClient(tc)
}

type downloadFixture struct {
	t         *testing.T
	st        *store.Store
	root      string
	mu        sync.Mutex
	completed []string
}

func newDownloadFixture(t *testing.T) *downloadFixture {
	t.Helper()
	prev := newDownloadClient
	newDownloadClient = offlineDownloadClient
	t.Cleanup(func() { newDownloadClient = prev })
	st, err := store.Open(filepath.Join(t.TempDir(), "state.json"))
	require.NoError(t, err)
	return &downloadFixture{t: t, st: st, root: t.TempDir()}
}

func (f *downloadFixture) downloader() *Downloader {
	d := NewDownloader(DownloaderOptions{
		Store: f.st,
		Root:  func() string { return f.root },
		OnComplete: func(root string) {
			f.mu.Lock()
			f.completed = append(f.completed, root)
			f.mu.Unlock()
		},
	})
	f.t.Cleanup(func() { require.NoError(f.t, d.Close()) })
	return d
}

func (f *downloadFixture) completions() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.completed...)
}

// waitState 等任务进入某个状态（或超时失败）。
func (f *downloadFixture) waitState(id, state string) store.Download {
	f.t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if rec, ok := f.st.Download(id); ok && rec.State == state {
			return rec
		}
		time.Sleep(20 * time.Millisecond)
	}
	rec, _ := f.st.Download(id)
	f.t.Fatalf("任务 %s 没有进入 %s，当前：%+v", id, state, rec)
	return store.Download{}
}

// linkDownloadSeeder 把做种方接到下载器的监听端口（client 懒建、关了又建时端口会变，所以反复喂）。
func linkDownloadSeeder(t *testing.T, seedTor *torrent.Torrent, d *Downloader) {
	t.Helper()
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			if port := d.listenPort(); port > 0 {
				seedTor.AddPeers([]torrent.PeerInfo{{
					Addr:    torrent.StringAddr(net.JoinHostPort("127.0.0.1", strconv.Itoa(port))),
					Source:  torrent.PeerSourceDirect,
					Trusted: true,
				}})
			}
			select {
			case <-stop:
				return
			case <-time.After(linkRetryInterval):
			}
		}
	}()
	t.Cleanup(func() {
		close(stop)
		<-done
	})
}

func TestDownloadCompletesIntoRootAndLeavesNoStaging(t *testing.T) {
	f := newDownloadFixture(t)
	seedDir := t.TempDir()
	magnet, mi, data := buildTestTorrent(t, seedDir, "Show", testFile{name: "01.mkv", size: 2 << 20}, testFile{name: "02.mkv", size: 1 << 20})
	seedTor := startSeeder(t, seedDir, mi)
	d := f.downloader()
	linkDownloadSeeder(t, seedTor, d)

	view, err := d.Add(context.Background(), DownloadRequest{Magnet: magnet, Title: "[Group] Show 01-02"})
	require.NoError(t, err)
	assert.Equal(t, mi.HashInfoBytes().HexString(), view.ID)
	assert.Equal(t, store.DownloadMetadata, view.State)

	rec := f.waitState(view.ID, store.DownloadDone)
	assert.Equal(t, []string{filepath.Join(f.root, "Show")}, rec.Paths)
	assert.Equal(t, "Show", rec.Name)
	assert.Equal(t, int64(3<<20), rec.Size)
	for name, want := range data {
		got, err := os.ReadFile(filepath.Join(f.root, filepath.FromSlash(name)))
		require.NoError(t, err)
		assert.Equal(t, want, got, "%s 内容要与做种方逐字节一致", name)
	}
	assert.NoDirExists(t, filepath.Join(f.root, library.IncompleteDirName), "下完不留暂存目录")
	assert.Equal(t, []string{f.root}, f.completions(), "下完通知重扫一次")
	assert.Zero(t, d.listenPort(), "没有在下的任务就关掉 client，不出网")

	views := d.List()
	require.Len(t, views, 1)
	assert.Equal(t, int64(3<<20), views[0].BytesDone)
}

func TestDownloadSingleFileDoesNotOverwriteExisting(t *testing.T) {
	f := newDownloadFixture(t)
	seedDir := t.TempDir()
	magnet, mi, data := buildTestTorrent(t, seedDir, "", testFile{name: "movie.mkv", size: 1 << 20})
	require.NoError(t, os.WriteFile(filepath.Join(f.root, "movie.mkv"), []byte("用户自己的文件"), 0o600))
	seedTor := startSeeder(t, seedDir, mi)
	d := f.downloader()
	linkDownloadSeeder(t, seedTor, d)

	view, err := d.Add(context.Background(), DownloadRequest{Magnet: magnet})
	require.NoError(t, err)
	rec := f.waitState(view.ID, store.DownloadDone)
	target := filepath.Join(f.root, "movie (2).mkv")
	assert.Equal(t, []string{target}, rec.Paths)
	got, err := os.ReadFile(target)
	require.NoError(t, err)
	assert.Equal(t, data["movie.mkv"], got)
	mine, err := os.ReadFile(filepath.Join(f.root, "movie.mkv"))
	require.NoError(t, err)
	assert.Equal(t, "用户自己的文件", string(mine))
}

func TestDownloadRefusesPrivateTorrent(t *testing.T) {
	f := newDownloadFixture(t)
	seedDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(seedDir, "pt.mkv"), deterministicBytes(1<<20, 7), 0o600))
	private := true
	info := metainfo.Info{PieceLength: testPieceLength, Private: &private}
	require.NoError(t, info.BuildFromFilePath(filepath.Join(seedDir, "pt.mkv")))
	mi := &metainfo.MetaInfo{}
	mi.SetDefaults()
	infoBytes, err := bencode.Marshal(info)
	require.NoError(t, err)
	mi.InfoBytes = infoBytes
	magnet := metainfo.Magnet{InfoHash: mi.HashInfoBytes(), DisplayName: "pt.mkv"}.String()
	seedTor := startSeeder(t, seedDir, mi)
	d := f.downloader()
	linkDownloadSeeder(t, seedTor, d)

	view, err := d.Add(context.Background(), DownloadRequest{Magnet: magnet})
	require.NoError(t, err)
	rec := f.waitState(view.ID, store.DownloadFailed)
	assert.Contains(t, rec.Error, "私有站")
	assert.NoDirExists(t, stagingDir(f.root, view.ID), "私有种子的数据一个字节都不留")
	assert.Empty(t, f.completions())
}

func TestDownloadResumesAfterRestart(t *testing.T) {
	f := newDownloadFixture(t)
	seedDir := t.TempDir()
	magnet, mi, data := buildTestTorrent(t, seedDir, "Show", testFile{name: "01.mkv", size: 1 << 20})
	seedTor := startSeeder(t, seedDir, mi)

	// 第一次：没有任何分享者，任务停在找元数据，然后 nagare 退出
	first := NewDownloader(DownloaderOptions{Store: f.st, Root: func() string { return f.root }})
	view, err := first.Add(context.Background(), DownloadRequest{Magnet: magnet})
	require.NoError(t, err)
	require.NoError(t, first.Close())
	rec, ok := f.st.Download(view.ID)
	require.True(t, ok)
	assert.Equal(t, store.DownloadMetadata, rec.State, "退出不改任务状态，下次接着下")

	// 第二次启动：Resume 接着下
	second := f.downloader()
	linkDownloadSeeder(t, seedTor, second)
	second.Resume()
	f.waitState(view.ID, store.DownloadDone)
	got, err := os.ReadFile(filepath.Join(f.root, "Show", "01.mkv"))
	require.NoError(t, err)
	assert.Equal(t, data["Show/01.mkv"], got)
}

func TestDownloadRemoveStopsAndDeletesStaging(t *testing.T) {
	f := newDownloadFixture(t)
	magnet, mi, _ := buildTestTorrent(t, t.TempDir(), "Show", testFile{name: "01.mkv", size: 1 << 20})
	d := f.downloader()
	view, err := d.Add(context.Background(), DownloadRequest{Magnet: magnet})
	require.NoError(t, err)
	assert.DirExists(t, stagingDir(f.root, view.ID))
	assert.NotZero(t, d.listenPort(), "有在下的任务才起 client")

	again, err := d.Add(context.Background(), DownloadRequest{Magnet: magnet})
	require.NoError(t, err)
	assert.Equal(t, view.ID, again.ID, "同一个种子再点一次下载，返回同一条任务")
	assert.Len(t, d.List(), 1)

	removed, err := d.Remove(mi.HashInfoBytes().HexString())
	require.NoError(t, err)
	assert.True(t, removed)
	assert.NoDirExists(t, filepath.Join(f.root, library.IncompleteDirName))
	assert.Empty(t, d.List())
	assert.Zero(t, d.listenPort())
}

func TestDownloadConcurrentAddsStartOnce(t *testing.T) {
	f := newDownloadFixture(t)
	magnet, _, _ := buildTestTorrent(t, t.TempDir(), "Show", testFile{name: "01.mkv", size: 1 << 20})
	d := f.downloader()
	var wg sync.WaitGroup
	errs := make([]error, 8)
	for i := range errs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = d.Add(context.Background(), DownloadRequest{Magnet: magnet})
		}()
	}
	wg.Wait()
	for _, err := range errs {
		require.NoError(t, err)
	}
	views := d.List()
	require.Len(t, views, 1)
	assert.Equal(t, store.DownloadMetadata, views[0].State, "同时点几次也只起一条，不会因为抢同一个进度文件把任务标成失败")
}

func TestDownloadRejectsBadInput(t *testing.T) {
	f := newDownloadFixture(t)
	d := f.downloader()
	_, err := d.Add(context.Background(), DownloadRequest{})
	require.Error(t, err)
	_, err = d.Add(context.Background(), DownloadRequest{Magnet: "https://example.invalid/a.torrent"})
	require.Error(t, err)
	_, err = d.Add(context.Background(), DownloadRequest{Magnet: "magnet:?dn=没有infohash"})
	require.Error(t, err)

	f.root = "relative/dir"
	_, err = d.Add(context.Background(), DownloadRequest{Magnet: "magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567"})
	require.Error(t, err)
	assert.Empty(t, d.List())
}
