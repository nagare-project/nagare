// Package torrentstream 是磁力边下边播引擎（里程碑 M3）。
//
// 它把用户提供的一条磁力链接变成一个可以拖进度条的本机视频流，交给 mpv 播放。
// 本体不内置任何磁力源，也不内置任何 tracker —— 两者都只来自用户配置。
//
// 生命周期：New → (Prepare → Stop)* → Close。同一时刻只有一个播放会话，
// 这样种子、缓存目录与优先级窗口都只有一份，不必做多路生命周期记账。
package torrentstream

import (
	"context"
	"errors"
	"io/fs"
	"log"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/storage"

	errs "github.com/nagare-project/nagare/internal/errors"
)

// cacheDirPerm 是缓存目录权限：里面是用户正在看的内容，同机其他用户没有理由读得到。
const cacheDirPerm fs.FileMode = 0o700

// 两段等待的上限做成变量而非常量：失败路径（没人分享、缓冲上不来）的集成测试
// 要能把它们调短，否则一条用例就要干等一分钟。生产路径不改这两个值。
var (
	// metadataTimeout 是「等种子信息」的上限。没有上限的话，一条没人分享的磁力
	// 会让界面永远停在「查找分享者」，用户无法区分「在找」与「卡死」。
	metadataTimeout = 60 * time.Second
	// bufferTimeout 是「等起播缓冲」的上限。
	bufferTimeout = 120 * time.Second
)

const (
	// bufferStartBytes 是起播门槛：头部这么多字节到齐就可以启动 mpv（mpv 自己
	// 还有一层缓存）。它比 startupHeadBytes（16MB，弹幕匹配哈希的取样长度）小是
	// 有意的分工 —— 起播不等哈希，哈希在后台由钉住的头部补齐。
	bufferStartBytes = 8 * 1024 * 1024
	// statusPollInterval 是等待期间刷新状态与复查就绪的间隔。
	statusPollInterval = 250 * time.Millisecond
	// cacheStatInterval 是缓存占用的重算节流。Status 会被界面按秒轮询，
	// 每次都走一遍全盘会把「看一眼状态」变成一次 IO 负担。
	cacheStatInterval = 2 * time.Second
)

// 播放阶段。界面按它显示分阶段状态条（决议 M3-8）。
const (
	PhaseIdle      = "idle"
	PhaseMetadata  = "metadata"
	PhaseSelecting = "selecting"
	PhaseBuffering = "buffering"
	PhaseReady     = "ready"
)

// Config 是用户可调的配置（与 store.TorrentConfig 字段一一对应）。
type Config struct {
	// Seeding：停止播放后是否继续做种，默认关。
	//
	// 它映射到 anacrolix 的 ClientConfig.Seed。关掉【不等于】播放期间不上传：
	// PeerConn.uploadAllowed 在 t.seeding() 为假时仍会走互惠分支，只是把上传
	// 限制在「不超过已下载量 + 100KiB」；下载停止后对端不再有我们想要的分片，
	// 上传自然收敛到零。也就是播放期间正常参与交换（协议必需），停播即停做种，
	// 正是决议 M3-2 要的语义。
	Seeding bool
	// Trackers 只对【公开】种子补充，默认空、不硬编码。
	Trackers []string
	// PortForwarding：UPnP/NAT-PMP 自动端口映射。
	PortForwarding bool
	// ListenPort 是 BT 监听端口；0 表示交由系统分配。
	ListenPort int
}

// normalized 返回清洗过的副本：Trackers 深拷贝并过滤非法地址，
// 避免调用方之后改动切片影响引擎，也避免非法地址流到 AddTrackers。
func (c Config) normalized() Config {
	c.Trackers, _ = NormalizeTrackers(c.Trackers)
	return c
}

// needsNewClient 判断两份配置的差异是否只能靠重建 torrent client 生效。
// 这三项都是 ClientConfig 的构造期参数，运行时改字段是数据竞争（anacrolix
// 全仓无锁读 config），-race 会抓到，不能走那条路。
func (c Config) needsNewClient(other Config) bool {
	return c.Seeding != other.Seeding ||
		c.ListenPort != other.ListenPort ||
		c.PortForwarding != other.PortForwarding
}

// Options 是构造引擎的输入。
type Options struct {
	// CacheDir 是分片落盘目录，调用方给 <配置目录>/cache/torrent。
	// 不用系统临时目录：那里可能被系统在播放中途清理掉。
	CacheDir string
	// StreamBase 返回 "http://127.0.0.1:<port>/stream/<capability>"（不带尾斜杠）。
	// 用函数而不是字符串：能力段每进程轮换，取值必须是调用时的最新值。
	StreamBase func() string
	Config     Config
}

// Engine 是磁力播放引擎。方法可并发调用。
type Engine struct {
	cacheDir   string
	streamBase func() string

	// rootCtx 跟随 New/Close，是所有会话 ctx 的父。
	// 会话【绝不能】挂在 Prepare 传进来的 ctx 上：那是 HTTP 请求的 ctx，
	// 响应一返回就被取消，mpv 会在刚起来的瞬间断流。
	rootCtx    context.Context
	rootCancel context.CancelFunc

	// prepMu 串行化 Prepare 与 client 重建：一次只播一个种子，
	// 两次准备没有并行的意义，重建期间也不能有人来开播。
	prepMu sync.Mutex

	mu  sync.Mutex
	cfg Config
	// liveCfg 是当前这个 client 实际是用哪份配置建起来的。
	// 它与 cfg 的差异就是「还没生效、要重启才生效」的精确定义 ——
	// 比记一个粘性布尔值准：改回去之后差异自动消失，不需要谁来清标志。
	liveCfg Config
	client  *torrent.Client
	sess    *session
	closed  bool
	// rebuildErr 记「client 重建失败且回滚也失败」的死态，
	// 让后续调用拿到明确错误而不是对着 nil client panic。
	rebuildErr error

	cacheMu    sync.Mutex
	cacheBytes int64
	cacheAt    time.Time
}

// New 建立引擎：清空并重建缓存目录，然后起 torrent client。
func New(opts Options) (*Engine, error) {
	if opts.CacheDir == "" {
		return nil, errs.New(errs.CategoryInternal, "torrentstream.new",
			"磁力缓存目录未配置", "这是 nagare 自身的问题，请反馈")
	}
	base := opts.StreamBase
	if base == nil {
		base = func() string { return "" }
	}
	cfg := opts.Config.normalized()

	engine := &Engine{cacheDir: opts.CacheDir, streamBase: base, cfg: cfg}
	// 启动即清空（决议 M3-4）：分片完成状态只在内存里，残留文件对下一次播放
	// 没有任何价值。先删后建，「磁盘被逐次播放写满」这类故障在结构上消失，
	// 也就不需要 LRU 记账。
	if err := engine.purgeCache(); err != nil {
		return nil, err
	}
	client, err := newClient(opts.CacheDir, cfg)
	if err != nil {
		return nil, err
	}
	engine.client = client
	engine.liveCfg = cfg
	engine.rootCtx, engine.rootCancel = context.WithCancel(context.Background())
	return engine, nil
}

// newClient 是 torrent client 的构造点。做成变量是为了让集成测试注入一个
// 完全不出网的 client —— 默认配置带公共 DHT bootstrap 节点，跑测试不该发包出去。
var newClient = newTorrentClient

// newTorrentClient 按配置组装 anacrolix client。
func newTorrentClient(cacheDir string, cfg Config) (*torrent.Client, error) {
	tc := torrent.NewDefaultClientConfig()
	tc.DataDir = cacheDir
	// 分片完成状态放内存（NewMapPieceCompletion）：磁盘上只剩媒体文件本身，
	// 于是 os.RemoveAll(cacheDir) 既安全又彻底 —— 没有一个需要保住的状态库。
	tc.DefaultStorage = storage.NewFileOpts(storage.NewFileClientOpts{
		ClientBaseDir:   cacheDir,
		PieceCompletion: storage.NewMapPieceCompletion(),
	})
	tc.Seed = cfg.Seeding
	tc.NoDefaultPortForwarding = !cfg.PortForwarding
	tc.ListenPort = cfg.ListenPort
	// DHT 与 PEX 保持默认开启：本体不内置 tracker，peer 发现全靠这两者。
	//
	// ⚠️ anacrolix v1.61.0【不会】对私有种子停用 DHT/PEX —— 全仓对 info.Private
	// 的引用只有 metainfo 的结构体定义和 cmd/torrent 的建种工具，dhtAnnouncer
	// 里一个字都没有。而 private 标记只存在于 info 字典里，磁力要靠 DHT 找 peer
	// 才拿得到 info，所以「先判断再决定要不要进 DHT」在磁力这条路上根本不成立。
	// 处理办法见 session.go：拿到 info 后一旦发现是私有种子就立刻中止会话并明确
	// 告知用户（见 errPrivateTorrent）。
	tc.DisableWebseeds = true   // 磁力的 ws= 参数会被当 webseed 执行：等于让用户粘的链接指使 nagare 对任意 URL 发 HTTP 请求
	tc.DisableWebtorrent = true // WebRTC 对等体不在设计范围内，开着还会牵出 ICE/STUN 的第三方出站

	// anacrolix 默认把大量调试日志刷到 stderr，会淹掉 nagare 自己的日志；
	// 而 tracker/peer 地址里可能带 passkey 一类凭证，也不该落盘。整条丢弃。
	// 只设 Slogger 就够：Logger 为零值时，anacrolix 会把模拟日志接到
	// Slogger 的 handler 上（client.go getLoggers），不会再回落到 stderr。
	tc.Slogger = slog.New(slog.DiscardHandler)

	client, err := torrent.NewClient(tc)
	if err != nil {
		return nil, errs.Wrap(errs.CategoryTorrent, "torrentstream.client",
			"磁力引擎启动失败", "在设置里换一个 BT 监听端口后重试（当前端口可能被占用）", err)
	}
	return client, nil
}

// Handler 返回流端点处理器，由 httpserver 在校验完能力段后转发进来。
func (e *Engine) Handler() http.Handler { return streamHandler{src: e} }

// SetConfig 更新配置。
//
// Trackers 立即生效（下一次补 tracker 时读新值）。Seeding / ListenPort /
// PortForwarding 是 client 的构造期参数：没有活动会话时就地重建 client，
// 因此也立即生效；正在播时不重建（会把这一集掐掉），返回 restartRequired = true。
func (e *Engine) SetConfig(c Config) (bool, error) {
	c = c.normalized()

	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		return false, errEngineClosed()
	}
	old := e.cfg
	e.cfg = c
	rebuild := c.needsNewClient(old)
	active := e.sess != nil
	e.mu.Unlock()

	switch {
	case !rebuild:
		return false, nil
	case active:
		return true, nil
	default:
		return e.rebuildClient(c, old)
	}
}

// rebuildClient 就地换掉 torrent client。
//
// BT 监听端口是独占的，必须先关掉旧 client 才能重建，所以没有「保住旧 client」
// 这个选项：新配置起不来就回滚到旧配置再起一次，让用户至少能继续用改动前那套；
// 连回滚都失败才把引擎标成不可用。
func (e *Engine) rebuildClient(next, prev Config) (bool, error) {
	e.prepMu.Lock()
	defer e.prepMu.Unlock()

	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		return false, errEngineClosed()
	}
	if e.sess != nil {
		// 刚才那一瞬间有人开播了：不掐掉正在播的这一集。
		e.mu.Unlock()
		return true, nil
	}
	old := e.client
	e.client = nil
	e.mu.Unlock()

	closeClient(old)
	if err := e.purgeCache(); err != nil {
		log.Printf("torrent: 重建磁力引擎时清理缓存失败：%v", err)
	}

	client, err := newClient(e.cacheDir, next)
	if err == nil {
		e.mu.Lock()
		if e.closed {
			// Close 现在也要 prepMu，正常走不到这里；留着是纵深防御 ——
			// 万一将来有人加了一条不持锁的关闭路径，也不至于留下一个没人关的 client。
			e.mu.Unlock()
			closeClient(client)
			return false, errEngineClosed()
		}
		e.client, e.liveCfg, e.rebuildErr = client, next, nil
		e.mu.Unlock()
		return false, nil
	}

	fallback, rollbackErr := newClient(e.cacheDir, prev)
	e.mu.Lock()
	e.client = fallback           // 回滚也失败时是 nil
	e.cfg, e.liveCfg = prev, prev // 生效的是旧配置，存的也必须是旧配置
	if rollbackErr != nil {
		e.rebuildErr = errs.Wrap(errs.CategoryTorrent, "torrentstream.rebuild",
			"磁力引擎重启失败", "重启 nagare 后重试", rollbackErr)
	}
	e.mu.Unlock()
	return false, err
}

// closeClient 丢掉残留种子并关闭 client，错误只记日志。
func closeClient(client *torrent.Client) {
	if client == nil {
		return
	}
	for _, t := range client.Torrents() {
		t.Drop()
	}
	for _, err := range client.Close() {
		log.Printf("torrent: 关闭磁力引擎时出错：%v", err)
	}
}

// Close 停会话、关 client、清空缓存目录。可重复调用。
func (e *Engine) Close() error {
	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		return nil
	}
	e.closed = true
	sess := e.sess
	e.sess = nil
	cancel := e.rootCancel
	e.mu.Unlock()

	if sess != nil {
		sess.close()
	}
	// 顺序要紧：先取消根 ctx。在途的 Prepare 正卡在「等元数据 / 等缓冲」那两个
	// select 上，取消后它立刻返回并交还 prepMu —— 否则下面抢锁会把退出拖上一分钟。
	if cancel != nil {
		cancel()
	}

	// 必须拿到 prepMu 才能收 client：否则一个正卡在 newClient 里的 rebuildClient
	// 会在 Close 跑完之后把刚建好的 client 写回 e.client，那个 client 再没有人关，
	// 监听端口与 DHT 会一直活到进程退出。
	e.prepMu.Lock()
	defer e.prepMu.Unlock()

	e.mu.Lock()
	client := e.client
	e.client = nil
	e.mu.Unlock()
	closeClient(client)

	// 退出清空（决议 M3-4）。删不掉不致命：Windows 上文件可能还被占用，
	// 下次启动的「启动即清空」会把它收拾干净。
	if err := os.RemoveAll(e.cacheDir); err != nil && !errors.Is(err, fs.ErrNotExist) {
		log.Printf("torrent: 退出时清理缓存目录失败（下次启动会重试）：%v", err)
	}
	return nil
}

// CacheBytes 返回缓存目录占用（字节）。按 cacheStatInterval 节流，
// 界面按秒轮询状态时不会每次都走一遍全盘。
func (e *Engine) CacheBytes() int64 {
	e.cacheMu.Lock()
	defer e.cacheMu.Unlock()
	if !e.cacheAt.IsZero() && time.Since(e.cacheAt) < cacheStatInterval {
		return e.cacheBytes
	}
	e.cacheBytes = dirSize(e.cacheDir)
	e.cacheAt = time.Now()
	return e.cacheBytes
}

// ClearCache 停掉当前会话并清空缓存目录。
func (e *Engine) ClearCache() error {
	e.Stop()
	return e.purgeCache()
}

// purgeCache 删掉整个缓存目录再重建。一次只播一个种子，
// 因此「整目录删掉」就是「删掉这次的分片」，不需要按种子挑文件。
func (e *Engine) purgeCache() error {
	if err := os.RemoveAll(e.cacheDir); err != nil && !errors.Is(err, fs.ErrNotExist) {
		// 正在播完的文件可能还被占用（Windows），记日志后仍然报出去 ——
		// 「以为清干净了其实没清」比报错更糟。
		log.Printf("torrent: 清理缓存目录失败：%v", err)
		return errs.Wrap(errs.CategoryStorage, "torrentstream.purge",
			"清理磁力缓存失败", "关掉可能占用这些文件的程序（比如 mpv）后重试，或在设置里换一个数据目录", err)
	}
	if err := os.MkdirAll(e.cacheDir, cacheDirPerm); err != nil {
		return errs.Wrap(errs.CategoryStorage, "torrentstream.purge",
			"无法创建磁力缓存目录", "检查磁盘剩余空间与目录权限后重试", err)
	}
	e.cacheMu.Lock()
	e.cacheBytes, e.cacheAt = 0, time.Now()
	e.cacheMu.Unlock()
	return nil
}

// dirSize 累加目录下所有普通文件的大小。读不到的条目跳过 —— 这是给界面看的
// 占用估计，不值得为一个瞬时消失的临时文件整体失败。
func dirSize(dir string) int64 {
	var total int64
	_ = filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if info, statErr := d.Info(); statErr == nil {
			total += info.Size()
		}
		return nil
	})
	return total
}

// activeClient 取当前可用的 client，取不到时给出分类错误而不是返回 nil。
func (e *Engine) activeClient() (*torrent.Client, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return nil, errEngineClosed()
	}
	if e.client == nil {
		if e.rebuildErr != nil {
			return nil, e.rebuildErr
		}
		return nil, errs.New(errs.CategoryTorrent, "torrentstream.client",
			"磁力引擎不可用", "重启 nagare 后重试")
	}
	return e.client, nil
}

func errEngineClosed() error {
	return errs.New(errs.CategoryInternal, "torrentstream",
		"磁力引擎已关闭", "重启 nagare 后重试")
}

// trackers 返回当前配置里的 tracker 列表副本。
func (e *Engine) trackers() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string(nil), e.cfg.Trackers...)
}

// RestartRequired 表示「已保存的配置里，有构造期参数还没被正在跑的 client 采纳」。
//
// 它必须能活过一次页面刷新：只在一次响应里出现的「重启后生效」等于没提示，
// 用户刷新一下就会以为改动已经生效了。
func (e *Engine) RestartRequired() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.cfg.needsNewClient(e.liveCfg)
}

// listenPort 返回 BT 监听端口（还没监听上时为 0）。
//
// 不导出：这是集成测试把「同进程做种方」接到引擎上的唯一入口
// （两边都不出网，端口号是它们之间唯一的牵线），不该出现在公开 API 里。
func (e *Engine) listenPort() int {
	e.mu.Lock()
	client := e.client
	e.mu.Unlock()
	if client == nil {
		return 0
	}
	return client.LocalPort()
}

// seedingEnabled 返回当前的做种开关（Status 用）。
func (e *Engine) seedingEnabled() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.cfg.Seeding
}
