package torrentstream

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/anacrolix/generics"
	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/storage"

	errs "github.com/nagare-project/nagare/internal/errors"
	"github.com/nagare-project/nagare/internal/library"
	"github.com/nagare-project/nagare/internal/store"
)

// Downloader 是「下载」按钮背后的东西：把一个种子完整下到用户的下载目录，下完进本地媒体库。
//
// 它与边下边播（Engine）是两条互不相干的路：
//   - 边下边播停播即删分片、一次一个（决议 M3-4）；下载是用户明确要留下来的，可以同时好几条，活过重启。
//   - 各用各的 BT client：Engine 改端口 / 做种开关时会重建 client，不能把下载一起掐掉。
//     下载的 client 懒建 —— 没有在下的任务就不起，也就不出网。
//
// 没下完的数据落在 <下载目录>/.nagare-incomplete/<infohash>/：库扫描整个跳过这个目录
// （library.IncompleteDirName），分片完成状态存在同目录的 bbolt 文件里，重启后不用整部重新校验。
// 下完把种子顶层的每一项挪到 <下载目录>/ 下（重名加「 (2)」），再通知调用方重扫媒体库。
// 同一块盘里挪只是改名，不复制。
//
// 下完即停：不继续做种（与「持续做种」开关无关，那个开关管的是边下边播）。
type Downloader struct {
	opts DownloaderOptions

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	mu     sync.Mutex
	client *torrent.Client
	tasks  map[string]*downloadTask
	// adding 是正在 Add 里、还没进 tasks 的 infohash：同一个种子同时点两次下载，第二次直接返回第一次那条
	adding map[string]bool
	closed bool
}

// DownloadStore 是下载任务的持久化（*store.Store 实现）。
type DownloadStore interface {
	Downloads() []store.Download
	Download(id string) (store.Download, bool)
	PutDownload(store.Download) error
	UpdateDownload(id string, mutate func(*store.Download)) (bool, error)
	DeleteDownload(id string) (bool, error)
}

// DownloaderOptions 是构造下载器的输入。函数字段在用到时才读，设置改了就按新的来。
type DownloaderOptions struct {
	Store DownloadStore
	// Root 返回当前的下载目录（绝对路径）。只影响之后新加的任务：老任务记着自己的目录。
	Root func() string
	// Trackers 返回要补的 tracker（与边下边播同一份配置；私有种子一律不补）。
	Trackers func() []string
	// PortForwarding 返回要不要做 UPnP/NAT-PMP 端口映射，建 client 时读一次。
	PortForwarding func() bool
	// OnComplete 在一条任务下完、文件挪进 root 之后调用（把 root 加进媒体库并重扫）。在后台 goroutine 里调。
	OnComplete func(root string)
}

// DownloadRequest 是新加一条下载：磁力或种子文件地址二选一。
type DownloadRequest struct {
	Magnet     string
	TorrentURL string
	Title      string
}

// DownloadView 是界面看的一条任务：持久化的记录，加上正在下的那几条的实时进度。
type DownloadView struct {
	store.Download
	BytesDone int64 `json:"bytesDone"`
	Peers     int   `json:"peers"`
	Seeders   int   `json:"seeders"`
	DownRate  int64 `json:"downRate"`
}

type downloadTask struct {
	id      string
	staging string
	tor     *torrent.Torrent
	storage storage.ClientImplCloser
	cancel  context.CancelFunc

	// 以下由 Downloader.mu 保护
	writeErr   error
	sampleAt   time.Time
	sampleDone int64
	rate       int64
}

const (
	// metaFileName 是暂存目录里存的一份种子信息：续传不用再靠 DHT 找元数据。
	metaFileName = ".meta.torrent"
	// boltFileName 是 anacrolix bbolt 分片完成记录的文件名（storage.NewBoltPieceCompletion 定的）。
	boltFileName = ".torrent.bolt.db"
)

// newDownloadClient 是下载 client 的构造点；做成变量让测试注入不出网的 client。
var newDownloadClient = newDownloadTorrentClient

func newDownloadTorrentClient(portForwarding bool) (*torrent.Client, error) {
	tc := torrent.NewDefaultClientConfig()
	// 每个任务自带存储（见 start），默认存储不会被用到；显式给一个，免得 anacrolix 按 DataDir 在工作目录建文件
	tc.DefaultStorage = storage.NewFileOpts(storage.NewFileClientOpts{
		ClientBaseDir:   os.TempDir(),
		PieceCompletion: storage.NewMapPieceCompletion(),
	})
	tc.Seed = false
	// 端口交给系统分配：固定端口归边下边播那个 client
	tc.ListenPort = 0
	tc.NoDefaultPortForwarding = !portForwarding
	tc.DisableWebseeds = true
	tc.DisableWebtorrent = true
	tc.Slogger = slog.New(slog.DiscardHandler)
	client, err := torrent.NewClient(tc)
	if err != nil {
		return nil, errs.Wrap(errs.CategoryTorrent, "torrentstream.download-client",
			"下载引擎启动失败", "稍后重试；反复失败请查看日志", err)
	}
	return client, nil
}

// NewDownloader 建下载器。不起 client、不碰网络：Resume 或 Add 时才按需起。
func NewDownloader(opts DownloaderOptions) *Downloader {
	ctx, cancel := context.WithCancel(context.Background())
	return &Downloader{opts: opts, ctx: ctx, cancel: cancel, tasks: map[string]*downloadTask{}, adding: map[string]bool{}}
}

// Resume 接着下上次没下完的任务（启动时调一次）。单条失败只记在那条任务上。
func (d *Downloader) Resume() {
	for _, rec := range d.opts.Store.Downloads() {
		if !rec.Active() {
			continue
		}
		if err := d.start(rec, nil); err != nil {
			d.markFailed(rec.ID, err)
		}
	}
}

// Add 新加一条下载。同一个种子已经在下就直接返回那条；已经下完、文件也还在，同样直接返回。
func (d *Downloader) Add(ctx context.Context, req DownloadRequest) (DownloadView, error) {
	magnet := strings.TrimSpace(req.Magnet)
	torrentURL := strings.TrimSpace(req.TorrentURL)
	if (magnet == "") == (torrentURL == "") {
		return DownloadView{}, errs.New(errs.CategoryInput, "torrentstream.download",
			"需要提供一条磁力链接或种子文件地址", "换一条资源后重试")
	}
	root := d.opts.Root()
	if root == "" || !filepath.IsAbs(root) {
		return DownloadView{}, errs.New(errs.CategoryInput, "torrentstream.download",
			"下载目录无效", "到设置页的「磁力」里设置一个下载目录")
	}

	var meta *metainfo.MetaInfo
	var id string
	if magnet != "" {
		if !strings.HasPrefix(strings.ToLower(magnet), "magnet:") {
			return DownloadView{}, errs.New(errs.CategoryInput, "torrentstream.download",
				"这不是一条磁力链接", "复制完整的 magnet: 链接后重试")
		}
		spec, err := torrent.TorrentSpecFromMagnetUri(magnet)
		if err != nil || spec.InfoHash == (metainfo.Hash{}) {
			return DownloadView{}, errs.New(errs.CategoryInput, "torrentstream.download",
				"磁力链接无法解析", "复制完整的 magnet: 链接后重试")
		}
		id = spec.InfoHash.HexString()
	} else {
		fetched, err := fetchTorrentMetaInfo(ctx, torrentURL)
		if err != nil {
			return DownloadView{}, err
		}
		info, err := fetched.UnmarshalInfo()
		if err != nil {
			return DownloadView{}, errs.Wrap(errs.CategoryInput, "torrentstream.metainfo",
				"种子文件内容无效", "换一条资源后重试", err)
		}
		if isPrivate(&info) {
			return DownloadView{}, errPrivateDownload()
		}
		meta = fetched
		id = fetched.HashInfoBytes().HexString()
	}
	id = strings.ToLower(id)

	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		return DownloadView{}, errEngineClosed()
	}
	_, running := d.tasks[id]
	busy := running || d.adding[id]
	if !busy {
		d.adding[id] = true
		defer func() {
			d.mu.Lock()
			delete(d.adding, id)
			d.mu.Unlock()
		}()
	}
	d.mu.Unlock()
	existing, known := d.opts.Store.Download(id)
	if busy {
		if known {
			return d.view(existing), nil
		}
		return DownloadView{Download: store.Download{ID: id, Title: strings.TrimSpace(req.Title), Root: root, State: store.DownloadMetadata}}, nil
	}
	if known && existing.State == store.DownloadDone && pathsExist(existing.Paths) {
		return d.view(existing), nil
	}

	rec := store.Download{
		ID: id, Magnet: magnet, TorrentURL: torrentURL, Title: strings.TrimSpace(req.Title),
		Root: root, State: store.DownloadMetadata, AddedAt: time.Now().UnixMilli(),
	}
	if err := d.opts.Store.PutDownload(rec); err != nil {
		return DownloadView{}, err
	}
	if err := d.start(rec, meta); err != nil {
		d.markFailed(id, err)
		return DownloadView{}, err
	}
	return d.view(rec), nil
}

// List 返回全部任务（从新到旧），在下的带实时进度。
func (d *Downloader) List() []DownloadView {
	recs := d.opts.Store.Downloads()
	out := make([]DownloadView, 0, len(recs))
	for _, rec := range recs {
		out = append(out, d.view(rec))
	}
	return out
}

// Remove 删掉一条任务：没下完的停下并删掉暂存数据；下完的只删记录，文件留在媒体库里。
func (d *Downloader) Remove(id string) (bool, error) {
	id = strings.ToLower(strings.TrimSpace(id))
	rec, ok := d.opts.Store.Download(id)
	if !ok {
		return false, nil
	}
	if task := d.detach(id); task != nil {
		if err := os.RemoveAll(task.staging); err != nil {
			log.Printf("download: 删除暂存目录失败：%v", err)
		}
	} else if rec.State != store.DownloadDone && rec.Root != "" {
		// 失败的任务也可能在盘上留着暂存数据
		_ = os.RemoveAll(stagingDir(rec.Root, id))
	}
	d.dropIncompleteParent(rec.Root)
	return d.opts.Store.DeleteDownload(id)
}

// Close 停下所有任务并关掉 client。没下完的任务记录留着，下次启动 Resume 接着下。
func (d *Downloader) Close() error {
	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		return nil
	}
	d.closed = true
	tasks := make([]*downloadTask, 0, len(d.tasks))
	for id, task := range d.tasks {
		tasks = append(tasks, task)
		delete(d.tasks, id)
	}
	d.mu.Unlock()
	d.cancel()
	for _, task := range tasks {
		d.release(task)
	}
	d.wg.Wait()
	d.mu.Lock()
	client := d.client
	d.client = nil
	d.mu.Unlock()
	if client != nil {
		client.Close()
	}
	return nil
}

func stagingDir(root, id string) string {
	return filepath.Join(root, library.IncompleteDirName, id)
}

// start 把一条任务加进 client 并在后台跑完它。meta 非 nil 时先存进暂存目录。
func (d *Downloader) start(rec store.Download, meta *metainfo.MetaInfo) error {
	staging := stagingDir(rec.Root, rec.ID)
	if err := os.MkdirAll(staging, cacheDirPerm); err != nil {
		return errs.Wrap(errs.CategoryStorage, "torrentstream.download",
			"无法创建下载目录", "确认下载目录存在且可写，或在设置里换一个", err)
	}
	if meta != nil {
		if err := writeMetaFile(staging, meta); err != nil {
			return err
		}
	}
	spec, info, err := downloadSpec(rec, staging)
	if err != nil {
		return err
	}
	client, err := d.ensureClient()
	if err != nil {
		return err
	}
	completion, err := storage.NewBoltPieceCompletion(staging)
	if err != nil {
		return errs.Wrap(errs.CategoryStorage, "torrentstream.download",
			"无法在下载目录里记录进度", "确认下载目录可写；同一个目录不要被两个 nagare 同时使用", err)
	}
	st := storage.NewFileOpts(storage.NewFileClientOpts{
		ClientBaseDir:   staging,
		PieceCompletion: completion,
		UsePartFiles:    generics.Some(false),
	})
	spec.Storage = st
	tor, _, err := client.AddTorrentSpec(spec)
	if err != nil {
		_ = st.Close()
		return errs.Wrap(errs.CategoryTorrent, "torrentstream.download",
			"无法加入下载", "换一条资源后重试", err)
	}
	applyTrackers(tor, info, d.trackers())

	ctx, cancel := context.WithCancel(d.ctx)
	task := &downloadTask{id: rec.ID, staging: staging, tor: tor, storage: st, cancel: cancel}
	tor.SetOnWriteChunkError(func(err error) { d.noteWriteError(task, err) })

	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		cancel()
		tor.Drop()
		_ = st.Close()
		return errEngineClosed()
	}
	d.tasks[rec.ID] = task
	d.mu.Unlock()

	d.wg.Add(1)
	go d.run(ctx, task)
	return nil
}

// downloadSpec 拼出加种子用的 spec：暂存目录里有种子信息就用它（不用再等元数据），否则用磁力。
// 磁力里的 ws= / xs= / x.pe 一律丢掉：那等于让一条链接指使 nagare 去请求任意地址（同 Engine 的 DisableWebseeds）。
func downloadSpec(rec store.Download, staging string) (*torrent.TorrentSpec, *metainfo.Info, error) {
	var spec *torrent.TorrentSpec
	var info *metainfo.Info
	if mi, err := metainfo.LoadFromFile(filepath.Join(staging, metaFileName)); err == nil {
		parsed, err := mi.UnmarshalInfo()
		if err != nil {
			return nil, nil, errs.Wrap(errs.CategoryInput, "torrentstream.metainfo",
				"种子文件内容无效", "删掉这条下载后换一条资源", err)
		}
		if isPrivate(&parsed) {
			return nil, nil, errPrivateDownload()
		}
		spec, err = torrent.TorrentSpecFromMetaInfoErr(mi)
		if err != nil {
			return nil, nil, errs.Wrap(errs.CategoryInput, "torrentstream.metainfo",
				"种子文件内容无效", "删掉这条下载后换一条资源", err)
		}
		info = &parsed
	} else if rec.Magnet != "" {
		spec, err = torrent.TorrentSpecFromMagnetUri(rec.Magnet)
		if err != nil {
			return nil, nil, errs.Wrap(errs.CategoryInput, "torrentstream.download",
				"磁力链接无法解析", "删掉这条下载后换一条资源", err)
		}
	} else {
		return nil, nil, errs.New(errs.CategoryInput, "torrentstream.download",
			"找不到这条下载的种子信息", "删掉这条下载后重新加一次")
	}
	spec.Webseeds = nil
	spec.Sources = nil
	spec.PeerAddrs = nil
	return spec, info, nil
}

// run 等种子信息、下完、挪出暂存目录。ctx 取消（删任务、退出）就直接返回，收尾由取消方负责。
func (d *Downloader) run(ctx context.Context, task *downloadTask) {
	defer d.wg.Done()
	tor := task.tor
	select {
	case <-tor.GotInfo():
	case <-ctx.Done():
		return
	}
	if isPrivate(tor.Info()) {
		d.fail(task, errPrivateDownload())
		return
	}
	if _, err := os.Stat(filepath.Join(task.staging, metaFileName)); errors.Is(err, fs.ErrNotExist) {
		mi := tor.Metainfo()
		if err := writeMetaFile(task.staging, &mi); err != nil {
			log.Printf("download: 保存种子信息失败（不影响这次下载，只是重启后要重新找元数据）：%v", err)
		}
	}
	name, size := tor.Name(), tor.Length()
	if _, err := d.opts.Store.UpdateDownload(task.id, func(r *store.Download) {
		r.State = store.DownloadDownloading
		r.Name = name
		r.Size = size
	}); err != nil {
		log.Printf("download: 记录下载状态失败：%v", err)
	}
	tor.DownloadAll()

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-tor.Complete().On():
			d.finish(task)
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := d.writeFailure(task); err != nil {
				d.fail(task, err)
				return
			}
			d.sample(task)
		}
	}
}

// finish 把下完的种子挪进下载目录、记成已完成、通知重扫。
func (d *Downloader) finish(task *downloadTask) {
	if d.detach(task.id) == nil {
		return // 同时被删掉了
	}
	rec, ok := d.opts.Store.Download(task.id)
	if !ok {
		_ = os.RemoveAll(task.staging)
		return
	}
	paths, err := moveCompleted(task.staging, rec.Root)
	if err != nil {
		d.markFailed(task.id, errs.Wrap(errs.CategoryStorage, "torrentstream.download",
			"下载完成，但没能把文件挪进下载目录", "文件还在下载目录下的 .nagare-incomplete 里，可以手动挪出来", err))
		return
	}
	_ = os.RemoveAll(task.staging)
	d.dropIncompleteParent(rec.Root)
	if _, err := d.opts.Store.UpdateDownload(task.id, func(r *store.Download) {
		r.State = store.DownloadDone
		r.Error = ""
		r.Paths = paths
		r.CompletedAt = time.Now().UnixMilli()
	}); err != nil {
		log.Printf("download: 记录下载完成失败：%v", err)
	}
	log.Printf("download: 一条下载已完成（%d 项挪进下载目录）", len(paths))
	if d.opts.OnComplete != nil {
		d.opts.OnComplete(rec.Root)
	}
}

// fail 停下这条任务并记下原因；暂存数据删掉（私有种子、写盘失败都不会自己好）。
func (d *Downloader) fail(task *downloadTask, cause error) {
	if d.detach(task.id) == nil {
		return
	}
	_ = os.RemoveAll(task.staging)
	d.markFailed(task.id, cause)
}

func (d *Downloader) markFailed(id string, cause error) {
	if _, err := d.opts.Store.UpdateDownload(id, func(r *store.Download) {
		r.State = store.DownloadFailed
		r.Error = userFacing(cause)
	}); err != nil {
		log.Printf("download: 记录下载失败原因失败：%v", err)
	}
}

// detach 把任务从运行表里摘下来并停掉它（谁摘到谁负责收尾）；没有就返回 nil。
// 摘掉的是最后一条时顺手关掉 client：没有在下的任务就不出网。
func (d *Downloader) detach(id string) *downloadTask {
	d.mu.Lock()
	task := d.tasks[id]
	delete(d.tasks, id)
	var idle *torrent.Client
	if task != nil && len(d.tasks) == 0 && !d.closed {
		idle, d.client = d.client, nil
	}
	d.mu.Unlock()
	if task == nil {
		return nil
	}
	d.release(task)
	if idle != nil {
		idle.Close()
	}
	return task
}

// release 停种子、关存储（bbolt 文件与文件句柄要关掉，才能挪文件、删目录）。
func (d *Downloader) release(task *downloadTask) {
	task.cancel()
	task.tor.Drop()
	if err := task.storage.Close(); err != nil {
		log.Printf("download: 关闭存储失败：%v", err)
	}
}

func (d *Downloader) ensureClient() (*torrent.Client, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return nil, errEngineClosed()
	}
	if d.client != nil {
		return d.client, nil
	}
	forwarding := d.opts.PortForwarding != nil && d.opts.PortForwarding()
	client, err := newDownloadClient(forwarding)
	if err != nil {
		return nil, err
	}
	d.client = client
	return client, nil
}

func (d *Downloader) trackers() []string {
	if d.opts.Trackers == nil {
		return nil
	}
	return d.opts.Trackers()
}

// listenPort 是下载 client 的 BT 监听端口（测试接做种方用）；没起 client 时为 0。
func (d *Downloader) listenPort() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.client == nil {
		return 0
	}
	return d.client.LocalPort()
}

func (d *Downloader) noteWriteError(task *downloadTask, err error) {
	d.mu.Lock()
	first := task.writeErr == nil
	if first {
		task.writeErr = err
	}
	d.mu.Unlock()
	if first {
		log.Printf("download: 分片写盘失败，停止这条下载：%v", err)
	}
}

func (d *Downloader) writeFailure(task *downloadTask) error {
	d.mu.Lock()
	err := task.writeErr
	d.mu.Unlock()
	if err == nil {
		return nil
	}
	return errs.Wrap(errs.CategoryStorage, "torrentstream.download",
		"磁盘写入失败，已停止下载", "确认磁盘还有空间、下载目录可写，然后删掉这条重新下", err)
}

// sample 每秒记一次已完成字节，算下载速度。
func (d *Downloader) sample(task *downloadTask) {
	done := task.tor.BytesCompleted()
	now := time.Now()
	d.mu.Lock()
	defer d.mu.Unlock()
	if !task.sampleAt.IsZero() {
		if elapsed := now.Sub(task.sampleAt).Seconds(); elapsed > 0 {
			task.rate = int64(float64(max(done-task.sampleDone, 0)) / elapsed)
		}
	}
	task.sampleAt, task.sampleDone = now, done
}

func (d *Downloader) view(rec store.Download) DownloadView {
	v := DownloadView{Download: rec}
	if rec.State == store.DownloadDone {
		v.BytesDone = rec.Size
	}
	d.mu.Lock()
	task := d.tasks[rec.ID]
	var rate int64
	if task != nil {
		rate = task.rate
	}
	d.mu.Unlock()
	if task == nil {
		return v
	}
	stats := task.tor.Stats()
	v.Peers = stats.ActivePeers
	v.Seeders = stats.ConnectedSeeders
	v.DownRate = rate
	if task.tor.Info() != nil {
		v.BytesDone = task.tor.BytesCompleted()
		if v.Size == 0 {
			v.Size = task.tor.Length()
		}
	}
	return v
}

// dropIncompleteParent 在 .nagare-incomplete 空了以后把它删掉，下载目录里不留空的隐藏目录。
func (d *Downloader) dropIncompleteParent(root string) {
	if root == "" {
		return
	}
	_ = os.Remove(filepath.Join(root, library.IncompleteDirName)) // 非空时删不掉，正好
}

func writeMetaFile(staging string, mi *metainfo.MetaInfo) error {
	f, err := os.OpenFile(filepath.Join(staging, metaFileName), os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return errs.Wrap(errs.CategoryStorage, "torrentstream.download",
			"无法在下载目录里保存种子信息", "确认下载目录可写", err)
	}
	if err := mi.Write(f); err != nil {
		_ = f.Close()
		return errs.Wrap(errs.CategoryStorage, "torrentstream.download",
			"无法在下载目录里保存种子信息", "确认下载目录可写", err)
	}
	return f.Close()
}

// moveCompleted 把暂存目录顶层的每一项（单文件种子是那个文件，合集是那个目录）挪进 root。
// 重名不覆盖：加「 (2)」「 (3)」……。返回挪过去的新路径。
func moveCompleted(staging, root string) ([]string, error) {
	entries, err := os.ReadDir(staging)
	if err != nil {
		return nil, err
	}
	var moved []string
	for _, entry := range entries {
		name := entry.Name()
		if name == metaFileName || name == boltFileName || strings.HasPrefix(name, boltFileName) {
			continue
		}
		target, err := freeName(root, name)
		if err != nil {
			return moved, err
		}
		if err := os.Rename(filepath.Join(staging, name), target); err != nil {
			return moved, err
		}
		moved = append(moved, target)
	}
	if len(moved) == 0 {
		return nil, fmt.Errorf("暂存目录里没有下载好的文件")
	}
	return moved, nil
}

func freeName(root, name string) (string, error) {
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	for i := 1; i < 1000; i++ {
		candidate := name
		if i > 1 {
			candidate = fmt.Sprintf("%s (%d)%s", stem, i, ext)
		}
		target := filepath.Join(root, candidate)
		if _, err := os.Lstat(target); errors.Is(err, fs.ErrNotExist) {
			return target, nil
		} else if err != nil {
			return "", err
		}
	}
	return "", fmt.Errorf("下载目录里同名的东西太多：%s", name)
}

func pathsExist(paths []string) bool {
	if len(paths) == 0 {
		return false
	}
	for _, p := range paths {
		if _, err := os.Stat(p); err != nil {
			return false
		}
	}
	return true
}

// userFacing 取给界面看的那句话：分类错误用它自己的中文提示，其余原样。
func userFacing(err error) string {
	var e *errs.E
	if errors.As(err, &e) {
		return e.UserFacing()
	}
	return err.Error()
}

// errPrivateDownload 是私有站（PT）种子：理由与边下边播同一条（errPrivateTorrent）。
func errPrivateDownload() error {
	return errs.New(errs.CategoryInput, "torrentstream.private",
		"这是私有站（PT）的种子，nagare 不下载",
		"当前 BT 引擎无法只对单个种子关闭 DHT/PEX，继续下载可能导致私有站账号被封；请改用该站推荐的客户端")
}
