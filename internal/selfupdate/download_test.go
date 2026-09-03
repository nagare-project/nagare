package selfupdate

// 下载层的两件事：重定向策略（GitHub 的 302 必须放行，降级到明文必须拒绝），
// 以及「读失败」与「写失败」要分得开。

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// clientTrusting 造一个信任若干 httptest 服务端证书的客户端（跨主机重定向要用）。
func clientTrusting(servers ...*httptest.Server) *http.Client {
	pool := x509.NewCertPool()
	for _, s := range servers {
		if c := s.Certificate(); c != nil {
			pool.AddCert(c)
		}
	}
	return &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{
		RootCAs:    pool,
		MinVersion: tls.VersionTLS12,
	}}}
}

// TestApplyFollowsCrossHostHTTPSRedirect：GitHub 会把 releases/download 302 到
// objects.githubusercontent.com，这一跳必须能跟。
func TestApplyFollowsCrossHostHTTPSRedirect(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "nagare")
	writeFile(t, exe, "OLD BINARY", 0o755)

	signer := newTestSigner(t)
	archive := buildTarGz(t, []entry{{name: "nagare", body: "NEW BINARY", exec: true}})
	origin := serveRelease(t, releaseFiles(t, signer, tarAsset, archive, false))

	// front 什么都不存，只把请求原样 302 到 origin（另一台主机）。
	front := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, origin.URL+r.URL.Path, http.StatusFound)
	}))
	t.Cleanup(front.Close)

	u, err := New(Options{
		CurrentVersion: "0.1.0",
		PublicKey:      signer.pub,
		HTTPClient:     clientTrusting(front, origin),
		BaseURL:        front.URL,
	})
	require.NoError(t, err)
	u.env = environment{goos: "linux", goarch: "amd64",
		executable:   func() (string, error) { return exe, nil },
		evalSymlinks: func(p string) (string, error) { return p, nil }}

	require.NoError(t, u.Apply(context.Background(), testVersion))
	require.Equal(t, "NEW BINARY", readFile(t, exe))
}

// TestApplyRefusesRedirectDowngrade：被 302 到明文 http 一律中止。更新包的完整性
// 靠 minisign 保证，但「有没有连上声称的那台主机」仍然要靠 TLS。
func TestApplyRefusesRedirectDowngrade(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "nagare")
	writeFile(t, exe, "OLD BINARY", 0o755)

	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("明文的东西，一个字节都不该被读到"))
	}))
	t.Cleanup(plain.Close)
	front := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, plain.URL+r.URL.Path, http.StatusFound)
	}))
	t.Cleanup(front.Close)

	signer := newTestSigner(t)
	u, err := New(Options{
		CurrentVersion: "0.1.0",
		PublicKey:      signer.pub,
		HTTPClient:     clientTrusting(front),
		BaseURL:        front.URL,
	})
	require.NoError(t, err)
	u.env = environment{goos: "linux", goarch: "amd64",
		executable:   func() (string, error) { return exe, nil },
		evalSymlinks: func(p string) (string, error) { return p, nil }}

	err = u.Apply(context.Background(), testVersion)
	require.Error(t, err)
	require.Contains(t, userFacing(err), "下载校验清单失败")
	require.Equal(t, "OLD BINARY", readFile(t, exe))
}

// TestApplyStopsRedirectLoop：无限重定向不能把一次更新卡死。
func TestApplyStopsRedirectLoop(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "nagare")
	writeFile(t, exe, "OLD BINARY", 0o755)

	var loop *httptest.Server
	loop = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, loop.URL+r.URL.Path, http.StatusFound)
	}))
	t.Cleanup(loop.Close)

	signer := newTestSigner(t)
	u, err := New(Options{
		CurrentVersion: "0.1.0",
		PublicKey:      signer.pub,
		HTTPClient:     clientTrusting(loop),
		BaseURL:        loop.URL,
	})
	require.NoError(t, err)
	u.env = environment{goos: "linux", goarch: "amd64",
		executable:   func() (string, error) { return exe, nil },
		evalSymlinks: func(p string) (string, error) { return p, nil }}

	err = u.Apply(context.Background(), testVersion)
	require.Error(t, err)
	require.Equal(t, "OLD BINARY", readFile(t, exe))
	require.Empty(t, stagingLeftovers(t, dir))
}

// TestDownloadArchiveReportsCreateFailure：文件建不出来是存储问题，不是网络问题。
func TestDownloadArchiveReportsCreateFailure(t *testing.T) {
	signer := newTestSigner(t)
	srv := serveRelease(t, releaseFiles(t, signer, tarAsset, []byte("payload"), false))
	u := newTestUpdater(t, srv, signer.pub, "linux", "amd64", "/tmp/nagare")

	// 目标路径的父目录不存在 → OpenFile 必失败。
	err := u.downloadArchive(context.Background(),
		u.assetURL(testVersion, tarAsset), filepath.Join(t.TempDir(), "missing", "archive"), "deadbeef")
	require.Error(t, err)
	require.Contains(t, userFacing(err), "创建更新包临时文件失败")
}

// TestRecordingWriter：写侧的第一个错误要被记住，用来把磁盘满与网络中断分开。
func TestRecordingWriter(t *testing.T) {
	boom := errors.New("no space left on device")
	w := &recordingWriter{w: failingWriter{err: boom}}
	_, err := w.Write([]byte("x"))
	require.ErrorIs(t, err, boom)
	require.ErrorIs(t, w.err, boom)

	// 只记第一个错误。
	_, _ = w.Write([]byte("y"))
	require.ErrorIs(t, w.err, boom)
}

type failingWriter struct{ err error }

func (f failingWriter) Write([]byte) (int, error) { return 0, f.err }
