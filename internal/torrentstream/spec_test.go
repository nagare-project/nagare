package torrentstream

import (
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nagare-project/nagare/internal/store"
)

const specTestHash = "0123456789abcdef0123456789abcdef01234567"

// hostileMagnet 造一条把所有「直连地址」参数都带上的磁力，外加 tr= 与 dn=（这两项要保留）。
func hostileMagnet() string {
	q := url.Values{}
	q.Set("dn", "Some Show - 01")
	q.Add("tr", "udp://tracker.example:1337/announce")
	q.Add("ws", "http://192.168.1.1/ws")
	q.Add("xs", "http://10.0.0.1/xs.torrent")
	q.Add("as", "http://127.0.0.1:9/as.torrent")
	q.Add("x.pe", "192.168.1.2:6881")
	q.Add("x.pe", "[::1]:6881")
	return "magnet:?xt=urn:btih:" + specTestHash + "&" + q.Encode()
}

// assertNoDirectEndpoints 断言 spec 里没有任何会让 client 直接去碰某个地址的字段。
func assertNoDirectEndpoints(t *testing.T, spec *torrent.TorrentSpec) {
	t.Helper()
	assert.Empty(t, spec.Webseeds, "ws= / url-list")
	assert.Empty(t, spec.Sources, "xs= / as=")
	assert.Empty(t, spec.PeerAddrs, "x.pe")
	assert.Empty(t, spec.DhtNodes, ".torrent nodes")
}

// 先确认前提：anacrolix 原样解析时这些字段确实都填上了。上游哪天改了字段名或
// 不再解析某个参数，这条会先红，提醒去复核 withoutDirectEndpoints 的清单。
func TestUpstreamMagnetSpecCarriesDirectEndpoints(t *testing.T) {
	raw, err := torrent.TorrentSpecFromMagnetUri(hostileMagnet())
	require.NoError(t, err)
	assert.Equal(t, []string{"http://192.168.1.1/ws"}, raw.Webseeds)
	assert.ElementsMatch(t, []string{"http://10.0.0.1/xs.torrent", "http://127.0.0.1:9/as.torrent"}, raw.Sources)
	assert.ElementsMatch(t, []string{"192.168.1.2:6881", "[::1]:6881"}, raw.PeerAddrs)
}

func TestMagnetSpecStripsDirectEndpoints(t *testing.T) {
	spec, err := magnetSpec(hostileMagnet())
	require.NoError(t, err)

	assertNoDirectEndpoints(t, spec)
	assert.Equal(t, specTestHash, spec.InfoHash.HexString())
	assert.Equal(t, "Some Show - 01", spec.DisplayName)
	assert.Equal(t, [][]string{{"udp://tracker.example:1337/announce"}}, spec.Trackers, "tr= 照旧保留")
}

func TestMagnetSpecRejectsInvalidURI(t *testing.T) {
	for _, uri := range []string{
		"http://example.com/a.torrent",
		"magnet:?xt=urn:btih:nothex",
		"magnet:?xt=urn:btih:" + specTestHash + "&xt=urn:btih:" + strings.Repeat("ab", 20),
	} {
		_, err := magnetSpec(uri)
		assert.Error(t, err, uri)
	}
}

// buildHostileMetaInfo 造一个带 url-list 与 nodes 的 .torrent。
func buildHostileMetaInfo(t *testing.T) *metainfo.MetaInfo {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "Some Show - 01.mkv")
	require.NoError(t, os.WriteFile(path, deterministicBytes(64*1024, 3), 0o600))
	info := metainfo.Info{PieceLength: testPieceLength}
	require.NoError(t, info.BuildFromFilePath(path))
	infoBytes, err := bencode.Marshal(info)
	require.NoError(t, err)

	mi := &metainfo.MetaInfo{
		InfoBytes: infoBytes,
		Announce:  "udp://tracker.example:1337/announce",
		UrlList:   metainfo.UrlList{"http://192.168.1.1/seed/"},
		Nodes:     []metainfo.Node{"192.168.1.3:6881"},
	}
	return mi
}

func TestMetaInfoSpecStripsDirectEndpoints(t *testing.T) {
	mi := buildHostileMetaInfo(t)

	raw, err := torrent.TorrentSpecFromMetaInfoErr(mi)
	require.NoError(t, err)
	require.NotEmpty(t, raw.Webseeds, "前提：上游会把 url-list 当 webseed")
	require.NotEmpty(t, raw.DhtNodes, "前提：上游会把 nodes 塞进 DHT")

	spec, err := metaInfoSpec(mi)
	require.NoError(t, err)
	assertNoDirectEndpoints(t, spec)
	assert.Equal(t, mi.HashInfoBytes(), spec.InfoHash)
	assert.Equal(t, []byte(mi.InfoBytes), []byte(spec.InfoBytes))
	assert.Equal(t, [][]string{{"udp://tracker.example:1337/announce"}}, spec.Trackers)

	// 副本语义：剥的是返回值，传进来的上游 spec 不动。
	clean := withoutDirectEndpoints(raw)
	assert.NotEmpty(t, raw.Webseeds)
	assert.NotEmpty(t, raw.DhtNodes)
	assertNoDirectEndpoints(t, clean)
}

func TestMetaInfoSpecRejectsBadInfo(t *testing.T) {
	_, err := metaInfoSpec(&metainfo.MetaInfo{InfoBytes: []byte("not bencode")})
	assert.Error(t, err)
}

// 下载进库的两个分支（暂存目录里已有 .torrent / 只有磁力）都要剥干净。
func TestDownloadSpecStripsDirectEndpoints(t *testing.T) {
	t.Run("magnet", func(t *testing.T) {
		spec, info, err := downloadSpec(store.Download{Magnet: hostileMagnet()}, t.TempDir())
		require.NoError(t, err)
		assert.Nil(t, info)
		assertNoDirectEndpoints(t, spec)
		assert.NotEmpty(t, spec.Trackers)
	})
	t.Run("staged torrent", func(t *testing.T) {
		staging := t.TempDir()
		mi := buildHostileMetaInfo(t)
		f, err := os.Create(filepath.Join(staging, metaFileName))
		require.NoError(t, err)
		require.NoError(t, mi.Write(f))
		require.NoError(t, f.Close())

		spec, info, err := downloadSpec(store.Download{Magnet: hostileMagnet()}, staging)
		require.NoError(t, err)
		require.NotNil(t, info)
		assertNoDirectEndpoints(t, spec)
	})
}

// 结构守卫：包里（除 spec.go 外）的生产代码不得绕过 magnetSpec / metaInfoSpec 直接把
// 磁力或种子文件交给 anacrolix。AddMagnet / AddTorrent 会照单执行 xs= / as= / x.pe /
// nodes；以后新加一条加种子的路径时，这条会先红。
func TestNoDirectMagnetOrMetaInfoAdds(t *testing.T) {
	forbidden := []string{
		".AddMagnet(",
		".AddTorrent(",
		"TorrentSpecFromMagnetUri(",
		"TorrentSpecFromMetaInfo(",
		"TorrentSpecFromMetaInfoErr(",
	}
	files, err := filepath.Glob("*.go")
	require.NoError(t, err)
	require.NotEmpty(t, files)
	for _, name := range files {
		if name == "spec.go" || strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, err := os.ReadFile(name)
		require.NoError(t, err)
		for _, needle := range forbidden {
			assert.NotContains(t, string(src), needle, "%s 必须改走 magnetSpec / metaInfoSpec", name)
		}
	}
}
