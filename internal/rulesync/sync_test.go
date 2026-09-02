package rulesync

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const goodRule = `schema: 1
id: demo
name: Demo
request: { url: "https://x/rss?q={{query}}" }
format: xml
items: rss/channel/item
fields: { title: title, magnet: "enclosure@url" }
`

func sha(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }

func TestSyncInstallsValidatesAndPrunes(t *testing.T) {
	files := map[string]string{
		"/index.json":  `{"schema":1,"rules":[{"file":"demo.yaml","sha256":"` + sha(goodRule) + `"},{"file":"broken.yaml"},{"file":"bad.yaml","sha256":"00"},{"file":"../evil.yaml"}]}`,
		"/demo.yaml":   goodRule,
		"/broken.yaml": "schema: 1\nid: broken\n",
		"/bad.yaml":    goodRule,
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := files[r.URL.Path]
		if !ok {
			w.WriteHeader(404)
			return
		}
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "stale.yaml"), []byte("x"), 0o600))
	s := &Syncer{Client: srv.Client()}

	rep, err := s.Sync(context.Background(), srv.URL, dir)
	require.NoError(t, err)
	assert.Equal(t, 1, rep.Added, "只有合法且校验通过的 demo.yaml 安装")
	assert.Equal(t, 1, rep.Removed, "清单外的 stale.yaml 被下架")
	assert.Len(t, rep.Errors, 3, "坏规则 / 校验和不符 / 非法文件名 各记一条")
	_, err = os.Stat(filepath.Join(dir, "demo.yaml"))
	assert.NoError(t, err)
	_, err = os.Stat(filepath.Join(dir, "broken.yaml"))
	assert.True(t, os.IsNotExist(err), "无效规则不得落盘")

	// 再同步一次：无变化。
	rep, err = s.Sync(context.Background(), srv.URL, dir)
	require.NoError(t, err)
	assert.Equal(t, 1, rep.Unchanged)
	assert.Equal(t, 0, rep.Added)
}

func TestValidateRemoteURL(t *testing.T) {
	ok, err := ValidateRemoteURL("https://example.com/rules/")
	require.NoError(t, err)
	assert.Equal(t, "https://example.com/rules", ok)
	_, err = ValidateRemoteURL("http://example.com/rules")
	assert.Error(t, err, "非本机 http 拒绝")
	_, err = ValidateRemoteURL("https://example.com/rules?x=1")
	assert.Error(t, err)
	_, err = ValidateRemoteURL("not a url")
	assert.Error(t, err)
	_, err = ValidateRemoteURL("http://127.0.0.1:9/rules")
	assert.NoError(t, err, "本机 http 允许（开发用）")
}

func TestSyncIndexFailures(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"schema":7}`))
	}))
	defer srv.Close()
	_, err := (&Syncer{Client: srv.Client()}).Sync(context.Background(), srv.URL, t.TempDir())
	assert.Error(t, err)
}
