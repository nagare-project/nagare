package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	errs "github.com/nagare-project/nagare/internal/errors"
	"github.com/nagare-project/nagare/internal/player"
	"github.com/nagare-project/nagare/internal/store"
	"github.com/nagare-project/nagare/internal/torrentstream"
)

// fakeTorrent 是 TorrentAPI 替身。
type fakeTorrent struct {
	prepareRes torrentstream.PrepareResult
	prepareErr error
	lastReq    torrentstream.PrepareRequest

	status     torrentstream.Status
	cacheBytes int64
	clearErr   error

	lastConfig torrentstream.Config
	setErr     error
	restart    bool

	stops int
	log   func(string)
}

func (f *fakeTorrent) record(op string) {
	if f.log != nil {
		f.log(op)
	}
}

func (f *fakeTorrent) Prepare(_ context.Context, req torrentstream.PrepareRequest) (torrentstream.PrepareResult, error) {
	f.record("torrent.prepare")
	f.lastReq = req
	return f.prepareRes, f.prepareErr
}
func (f *fakeTorrent) Stop()                        { f.record("torrent.stop"); f.stops++ }
func (f *fakeTorrent) Status() torrentstream.Status { return f.status }
func (f *fakeTorrent) CacheBytes() int64            { return f.cacheBytes }
func (f *fakeTorrent) ClearCache() error            { f.cacheBytes = 0; return f.clearErr }
func (f *fakeTorrent) RestartRequired() bool        { return f.restart }
func (f *fakeTorrent) SetConfig(c torrentstream.Config) (bool, error) {
	f.lastConfig = c
	return f.restart, f.setErr
}

// ── 播放 ──

// 需要用户选集时原样透出候选列表，并且【不】启动播放器。
func TestTorrentPlayNeedsSelection(t *testing.T) {
	env := newEnv(t)
	ep := 2
	env.torrent.prepareRes = torrentstream.PrepareResult{
		NeedSelection: true,
		Files: []torrentstream.FileChoice{
			{Index: 0, Name: "[组] 番 - 01.mkv", Path: "番/01.mkv", Size: 1 << 30, Episode: nil},
			{Index: 1, Name: "[组] 番 - 02.mkv", Path: "番/02.mkv", Size: 2 << 30, Episode: &ep},
		},
	}

	rec := env.do(t, http.MethodPost, "/api/torrent/play", `{"magnet":"magnet:?xt=urn:btih:abc","title":"番"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var got struct {
		NeedSelection bool `json:"needSelection"`
		Files         []struct {
			Index     int    `json:"index"`
			Name      string `json:"name"`
			SizeBytes int64  `json:"sizeBytes"`
			Episode   *int   `json:"episode"`
		} `json:"files"`
	}
	require.NoError(t, json.Unmarshal(decode(t, rec).Data, &got))
	assert.True(t, got.NeedSelection)
	require.Len(t, got.Files, 2)
	// 字段名必须是 sizeBytes：前端与库页共用同一套格式化，不接受第二种命名。
	assert.Equal(t, int64(2<<30), got.Files[1].SizeBytes)
	require.NotNil(t, got.Files[1].Episode)
	assert.Equal(t, 2, *got.Files[1].Episode)
	// 选集阶段绝不能启动播放器：种子还留着等用户挑，这时起 mpv 会播到一个空流。
	assert.Equal(t, []string{"player.stop", "torrent.prepare"}, *env.calls)
}

// 选定文件后走完整条链：磁力来源交给播放管线，返回标题与弹幕状态。
func TestTorrentPlayStartsPlayer(t *testing.T) {
	env := newEnv(t)
	env.torrent.prepareRes = torrentstream.PrepareResult{Source: &torrentstream.Source{}}
	env.player.playRes = player.PlayResult{
		Title:   "葬送的芙莉莲 第2集",
		Danmaku: player.DanmakuInfo{State: "loading", Reason: "正在匹配弹幕…"},
	}

	rec := env.do(t, http.MethodPost, "/api/torrent/play",
		`{"magnet":"magnet:?xt=urn:btih:abc","title":"芙莉莲","episodeHint":2,"fileIndex":3}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var got struct {
		NeedSelection bool               `json:"needSelection"`
		Title         string             `json:"title"`
		Danmaku       player.DanmakuInfo `json:"danmaku"`
	}
	require.NoError(t, json.Unmarshal(decode(t, rec).Data, &got))
	assert.False(t, got.NeedSelection)
	assert.Equal(t, "葬送的芙莉莲 第2集", got.Title)
	assert.Equal(t, "loading", got.Danmaku.State)

	assert.Equal(t, 2, env.torrent.lastReq.EpisodeHint)
	assert.Equal(t, 3, env.torrent.lastReq.FileIndex, "用户手选的下标要原样传给引擎")
	assert.Equal(t, "芙莉莲", env.torrent.lastReq.Title)
}

// fileIndex 缺席时必须传 -1 而不是 0 —— 0 是合法下标，混淆会让引擎跳过选集
// 直接播第一个文件（合集里往往是 NCOP 或第 1 集）。
func TestTorrentPlayAbsentFileIndexMeansUnselected(t *testing.T) {
	env := newEnv(t)
	env.torrent.prepareRes = torrentstream.PrepareResult{Source: &torrentstream.Source{}}

	rec := env.do(t, http.MethodPost, "/api/torrent/play", `{"magnet":"magnet:?xt=urn:btih:abc"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, -1, env.torrent.lastReq.FileIndex)
}

// 顺序是正确性的一部分：必须先停旧的播放会话再准备新种子。
// 反过来的话，旧会话终结时的「停止播放即停做种」回调会把刚建好的新种子掐掉。
func TestTorrentPlayStopsPlayerBeforePrepare(t *testing.T) {
	env := newEnv(t)
	env.torrent.prepareRes = torrentstream.PrepareResult{Source: &torrentstream.Source{}}

	rec := env.do(t, http.MethodPost, "/api/torrent/play", `{"magnet":"magnet:?xt=urn:btih:abc"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, []string{"player.stop", "torrent.prepare", "player.play"}, *env.calls)
}

func TestTorrentPlayRejectsEmptyMagnet(t *testing.T) {
	env := newEnv(t)
	rec := env.do(t, http.MethodPost, "/api/torrent/play", `{"magnet":"   "}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, decode(t, rec).Error, "磁力链接")
}

// swarm 侧的失败映射成 502，并且原样透出中文提示 + 恢复动作（CQ3：失败必须可行动）。
func TestTorrentPlayNoPeersIsGatewayError(t *testing.T) {
	env := newEnv(t)
	env.torrent.prepareErr = errs.New(errs.CategoryTorrent, "torrentstream.metadata",
		"找不到可用的分享者", "换一条资源试试")

	rec := env.do(t, http.MethodPost, "/api/torrent/play", `{"magnet":"magnet:?xt=urn:btih:abc"}`)
	assert.Equal(t, http.StatusBadGateway, rec.Code)
	body := decode(t, rec).Error
	assert.Contains(t, body, "找不到可用的分享者")
	assert.Contains(t, body, "换一条资源")
}

// 本机磁盘写不进去归 500，不能借用 CategoryFS 的 404 —— 那会让界面提示「重新扫描」，
// 而用户真正该做的是清磁盘。
func TestTorrentPlayStorageErrorIsServerError(t *testing.T) {
	env := newEnv(t)
	env.torrent.prepareErr = errs.New(errs.CategoryStorage, "torrentstream.write",
		"磁盘空间不足", "清理磁盘后重试")

	rec := env.do(t, http.MethodPost, "/api/torrent/play", `{"magnet":"magnet:?xt=urn:btih:abc"}`)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Contains(t, decode(t, rec).Error, "磁盘空间不足")
}

// mpv 起不来时要把种子收掉，否则它会继续占带宽和磁盘，而界面上什么都没在播。
func TestTorrentPlayStopsTorrentWhenLaunchFails(t *testing.T) {
	env := newEnv(t)
	env.torrent.prepareRes = torrentstream.PrepareResult{Source: &torrentstream.Source{}}
	env.player.playErr = errs.New(errs.CategoryPlayback, "player.launch",
		"启动 mpv 失败", "确认 mpv 已安装")

	rec := env.do(t, http.MethodPost, "/api/torrent/play", `{"magnet":"magnet:?xt=urn:btih:abc"}`)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Equal(t, 1, env.torrent.stops, "起播失败必须释放种子")
}

// ── 状态 / 停止 / 缓存 ──

func TestTorrentStatusPassthrough(t *testing.T) {
	env := newEnv(t)
	env.torrent.status = torrentstream.Status{
		Active: true, Phase: torrentstream.PhaseBuffering,
		Peers: 7, Seeders: 3, DownRate: 1 << 20, Buffered: 0.42, CacheBytes: 1 << 24,
	}
	rec := env.do(t, http.MethodGet, "/api/torrent/status", "")
	require.Equal(t, http.StatusOK, rec.Code)

	var got torrentstream.Status
	require.NoError(t, json.Unmarshal(decode(t, rec).Data, &got))
	assert.Equal(t, env.torrent.status, got)
}

// 停止要两边都收：只停一边会留下还在下载的种子，或对着断流地址空转的 mpv。
func TestTorrentStopStopsBoth(t *testing.T) {
	env := newEnv(t)
	rec := env.do(t, http.MethodPost, "/api/torrent/stop", "")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.True(t, env.player.stopped)
	assert.Equal(t, 1, env.torrent.stops)
}

// 清缓存返回【实际剩余】字节数。写死 0 的话，文件被占用只清掉一半时就是撒谎。
func TestTorrentCacheClearReportsRemaining(t *testing.T) {
	env := newEnv(t)
	env.torrent.cacheBytes = 5 << 30
	rec := env.do(t, http.MethodPost, "/api/torrent/cache/clear", "")
	require.Equal(t, http.StatusOK, rec.Code)

	var got struct {
		CacheBytes int64 `json:"cacheBytes"`
	}
	require.NoError(t, json.Unmarshal(decode(t, rec).Data, &got))
	assert.Equal(t, int64(0), got.CacheBytes)
	assert.True(t, env.player.stopped, "清缓存前要先停播放，否则删的是正在读的文件")
}

// ── 配置 ──

func TestTorrentConfigPersistsAndReachesEngine(t *testing.T) {
	env := newEnv(t)
	rec := env.do(t, http.MethodPost, "/api/torrent/config",
		`{"seeding":true,"portForwarding":false,"listenPort":51413,"trackers":["udp://tracker.example:6969"," ","not-a-url"]}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	saved := env.store.TorrentConfig()
	assert.True(t, saved.Seeding)
	assert.False(t, saved.PortForwarding)
	assert.Equal(t, 51413, saved.ListenPort)
	assert.Equal(t, []string{"udp://tracker.example:6969"}, saved.Trackers, "非法地址不该落盘")
	assert.Equal(t, engineConfig(saved), env.torrent.lastConfig, "落盘的配置要原样交给引擎")

	var got struct {
		Seeding          bool     `json:"seeding"`
		Trackers         []string `json:"trackers"`
		RejectedTrackers []string `json:"rejectedTrackers"`
		RestartRequired  bool     `json:"restartRequired"`
	}
	require.NoError(t, json.Unmarshal(decode(t, rec).Data, &got))
	assert.True(t, got.Seeding)
	assert.Equal(t, []string{"udp://tracker.example:6969"}, got.Trackers)
	// 被丢掉的行必须让用户看见 —— 静默吞掉等于用户以为自己填的地址在生效。
	assert.Contains(t, got.RejectedTrackers, "not-a-url")
}

func TestTorrentConfigRejectsBadPort(t *testing.T) {
	env := newEnv(t)
	for _, port := range []int{-1, 70000} {
		rec := env.do(t, http.MethodPost, "/api/torrent/config", `{"listenPort":`+itoa(port)+`}`)
		assert.Equal(t, http.StatusBadRequest, rec.Code, "端口 %d 应被拒", port)
		assert.Contains(t, decode(t, rec).Error, "65535")
	}
	assert.Equal(t, store.DefaultTorrentConfig(), env.store.TorrentConfig(), "被拒的请求不该改动落盘配置")
}

// 「重启后生效」必须活过一次页面刷新：只出现在一次响应里的提示等于没提示。
func TestSettingsCarriesRestartRequired(t *testing.T) {
	env := newEnv(t)
	env.torrent.restart = true
	rec := env.do(t, http.MethodGet, "/api/settings", "")
	require.Equal(t, http.StatusOK, rec.Code)

	var got struct {
		Torrent struct {
			Enabled         bool   `json:"enabled"`
			CacheDir        string `json:"cacheDir"`
			RestartRequired bool   `json:"restartRequired"`
			PortForwarding  bool   `json:"portForwarding"`
		} `json:"torrent"`
	}
	require.NoError(t, json.Unmarshal(decode(t, rec).Data, &got))
	assert.True(t, got.Torrent.Enabled)
	assert.True(t, got.Torrent.RestartRequired)
	assert.Equal(t, "/data/nagare/cache/torrent", got.Torrent.CacheDir)
	assert.True(t, got.Torrent.PortForwarding, "未配置过时端口映射默认开")
}

// ── 引擎缺席时的降级 ──

// 引擎起不来时磁力端点整体 503，且设置页看得见 enabled=false。
// 降级必须可见：按钮点了没反应是最糟的形态。
func TestTorrentEndpointsDegradeWhenEngineMissing(t *testing.T) {
	env := newEnvNoTorrent(t)
	cases := []struct{ method, path, body string }{
		{http.MethodPost, "/api/torrent/play", `{"magnet":"magnet:?xt=urn:btih:abc"}`},
		{http.MethodGet, "/api/torrent/status", ""},
		{http.MethodPost, "/api/torrent/stop", ""},
		{http.MethodPost, "/api/torrent/cache/clear", ""},
		{http.MethodPost, "/api/torrent/config", `{"seeding":true}`},
	}
	for _, c := range cases {
		rec := env.do(t, c.method, c.path, c.body)
		assert.Equal(t, http.StatusServiceUnavailable, rec.Code, "%s %s", c.method, c.path)
		assert.Contains(t, decode(t, rec).Error, "磁力播放未启用", "%s %s", c.method, c.path)
	}

	rec := env.do(t, http.MethodGet, "/api/settings", "")
	require.Equal(t, http.StatusOK, rec.Code)
	var got struct {
		Torrent struct {
			Enabled    bool  `json:"enabled"`
			CacheBytes int64 `json:"cacheBytes"`
		} `json:"torrent"`
	}
	require.NoError(t, json.Unmarshal(decode(t, rec).Data, &got))
	assert.False(t, got.Torrent.Enabled)
	assert.Zero(t, got.Torrent.CacheBytes)
}

// itoa 避免为了两个数字引入 strconv 的可读性负担。
func itoa(n int) string {
	if n < 0 {
		return "-" + itoa(-n)
	}
	if n < 10 {
		return string(rune('0' + n))
	}
	return itoa(n/10) + string(rune('0'+n%10))
}
