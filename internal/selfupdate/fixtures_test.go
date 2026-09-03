package selfupdate

// 测试用的脚手架：按 minisign 的格式造密钥与签名、按 goreleaser 的形态造归档与校验清单、
// 用 httptest 起一个假的发布站。整包测试【不联网】。

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/blake2b"
)

// testSigner 按 minisign 的文件格式造公钥串与 .minisig。
// 刻意不复用 internal/minisign 的内部函数：那样测的就只是「两段自洽的代码互相印证」了。
type testSigner struct {
	pub   string
	priv  ed25519.PrivateKey
	keyID [8]byte
}

func newTestSigner(t *testing.T) testSigner {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	s := testSigner{priv: priv}
	_, err = rand.Read(s.keyID[:])
	require.NoError(t, err)

	raw := append([]byte("Ed"), s.keyID[:]...)
	raw = append(raw, pub...)
	s.pub = "untrusted comment: minisign public key\n" +
		base64.StdEncoding.EncodeToString(raw) + "\n"
	return s
}

// sign 造一份 .minisig：四行，第二段全局签名覆盖「签名 ‖ trusted comment」。
// prehashed=true 时用 "ED"（签 BLAKE2b-512(消息)），即 minisign -H 的形态。
func (s testSigner) sign(t *testing.T, msg []byte, trusted string, prehashed bool) []byte {
	t.Helper()
	alg, signed := "Ed", msg
	if prehashed {
		sum := blake2b.Sum512(msg)
		alg, signed = "ED", sum[:]
	}
	sig := ed25519.Sign(s.priv, signed)
	raw := append([]byte(alg), s.keyID[:]...)
	raw = append(raw, sig...)
	global := ed25519.Sign(s.priv, append(append([]byte{}, sig...), []byte(trusted)...))
	return []byte(strings.Join([]string{
		"untrusted comment: signature from nagare test",
		base64.StdEncoding.EncodeToString(raw),
		"trusted comment: " + trusted,
		base64.StdEncoding.EncodeToString(global),
	}, "\n") + "\n")
}

// entry 是要塞进归档的一条内容。
type entry struct {
	name string
	body string
	exec bool
	dir  bool
	link string // 非空 = 软链接，指向这个目标
	hard string // 非空 = 硬链接（只有 tar 有）
}

func (e entry) mode() fs.FileMode {
	switch {
	case e.dir:
		return fs.ModeDir | 0o755
	case e.exec:
		return 0o755
	default:
		return 0o644
	}
}

func buildTarGz(t *testing.T, entries []entry) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		hdr := &tar.Header{Name: e.name, Mode: int64(e.mode().Perm()), Size: int64(len(e.body))}
		switch {
		case e.dir:
			hdr.Typeflag, hdr.Size = tar.TypeDir, 0
		case e.link != "":
			hdr.Typeflag, hdr.Linkname, hdr.Size = tar.TypeSymlink, e.link, 0
		case e.hard != "":
			hdr.Typeflag, hdr.Linkname, hdr.Size = tar.TypeLink, e.hard, 0
		default:
			hdr.Typeflag = tar.TypeReg
		}
		require.NoError(t, tw.WriteHeader(hdr))
		if hdr.Typeflag == tar.TypeReg {
			_, err := tw.Write([]byte(e.body))
			require.NoError(t, err)
		}
	}
	require.NoError(t, tw.Close())
	require.NoError(t, gz.Close())
	return buf.Bytes()
}

func buildZip(t *testing.T, entries []entry) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, e := range entries {
		name, body := e.name, e.body
		mode := e.mode()
		if e.dir {
			name += "/"
		}
		if e.link != "" {
			mode, body = fs.ModeSymlink|0o777, e.link
		}
		hdr := &zip.FileHeader{Name: name, Method: zip.Deflate}
		hdr.SetMode(mode)
		w, err := zw.CreateHeader(hdr)
		require.NoError(t, err)
		if !e.dir {
			_, err = w.Write([]byte(body))
			require.NoError(t, err)
		}
	}
	require.NoError(t, zw.Close())
	return buf.Bytes()
}

// checksumsFor 按 sha256sum 的格式（两个空格）造校验清单，顺序固定以便断言。
func checksumsFor(assets map[string][]byte) []byte {
	names := make([]string, 0, len(assets))
	for n := range assets {
		names = append(names, n)
	}
	sort.Strings(names)
	var b strings.Builder
	for _, n := range names {
		sum := sha256.Sum256(assets[n])
		fmt.Fprintf(&b, "%s  %s\n", hex.EncodeToString(sum[:]), n)
	}
	return []byte(b.String())
}

// serveRelease 起一个 TLS 的假发布站。用 TLS 而不是明文：resolveBaseURL 只接受 https，
// 这条约束本身就是要测的东西之一，不能为了测试把它放宽。
func serveRelease(t *testing.T, files map[string][]byte) *httptest.Server {
	t.Helper()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := files[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// newTestUpdater 造一个把运行环境完全注入的更新器：不碰真的 os.Executable，
// 也不依赖测试机自己装在哪。
func newTestUpdater(t *testing.T, srv *httptest.Server, pub, goos, goarch, exe string) *Updater {
	t.Helper()
	opts := Options{CurrentVersion: "0.1.0", PublicKey: pub}
	if srv != nil {
		opts.HTTPClient, opts.BaseURL = srv.Client(), srv.URL
	} else {
		opts.BaseURL = "https://example.invalid/releases"
	}
	u, err := New(opts)
	require.NoError(t, err)
	u.env = environment{
		goos:         goos,
		goarch:       goarch,
		executable:   func() (string, error) { return exe, nil },
		evalSymlinks: func(p string) (string, error) { return p, nil },
	}
	return u
}

// writeFile 建一个带内容的文件（连同父目录）。
func writeFile(t *testing.T, path, body string, mode fs.FileMode) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(body), mode))
	require.NoError(t, os.Chmod(path, mode))
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(b)
}

// stagingLeftovers 数一数安装目录里还剩几个暂存目录 —— 失败路径必须打扫干净。
func stagingLeftovers(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	var left []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), stagePrefix) {
			left = append(left, e.Name())
		}
	}
	return left
}
