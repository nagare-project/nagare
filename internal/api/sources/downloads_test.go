package sources

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	errs "github.com/nagare-project/nagare/internal/errors"
	"github.com/nagare-project/nagare/internal/store"
	"github.com/nagare-project/nagare/internal/torrentstream"
)

type fakeDownloads struct {
	added   []torrentstream.DownloadRequest
	addErr  error
	list    []torrentstream.DownloadView
	removed []string
	missing bool
}

func (f *fakeDownloads) Add(_ context.Context, req torrentstream.DownloadRequest) (torrentstream.DownloadView, error) {
	f.added = append(f.added, req)
	if f.addErr != nil {
		return torrentstream.DownloadView{}, f.addErr
	}
	return torrentstream.DownloadView{Download: store.Download{ID: "abc", Title: req.Title, State: store.DownloadMetadata}}, nil
}
func (f *fakeDownloads) List() []torrentstream.DownloadView { return f.list }
func (f *fakeDownloads) Remove(id string) (bool, error) {
	f.removed = append(f.removed, id)
	return !f.missing, nil
}

func TestDownloadsAddListRemove(t *testing.T) {
	env := newEnv(t)
	rec := env.do(t, http.MethodPost, "/api/downloads", `{"magnet":"magnet:?xt=urn:btih:abc","title":"[组] 番 01-12"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, []torrentstream.DownloadRequest{{Magnet: "magnet:?xt=urn:btih:abc", Title: "[组] 番 01-12"}}, env.downloads.added)
	var added struct {
		Data struct {
			Download struct {
				ID    string `json:"id"`
				State string `json:"state"`
			} `json:"download"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &added))
	assert.Equal(t, "abc", added.Data.Download.ID)
	assert.Equal(t, "metadata", added.Data.Download.State)

	env.downloads.list = []torrentstream.DownloadView{{Download: store.Download{ID: "abc", State: store.DownloadDownloading, Size: 100}, BytesDone: 40, Peers: 3, DownRate: 1024}}
	rec = env.do(t, http.MethodGet, "/api/downloads", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var listed struct {
		Data struct {
			Dir       string `json:"dir"`
			Downloads []struct {
				ID        string `json:"id"`
				State     string `json:"state"`
				BytesDone int64  `json:"bytesDone"`
				Size      int64  `json:"size"`
				Peers     int    `json:"peers"`
			} `json:"downloads"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &listed))
	assert.Equal(t, "/home/you/Downloads/nagare", listed.Data.Dir, "没设置时用默认目录")
	require.Len(t, listed.Data.Downloads, 1)
	assert.Equal(t, int64(40), listed.Data.Downloads[0].BytesDone)
	assert.Equal(t, 3, listed.Data.Downloads[0].Peers)

	rec = env.do(t, http.MethodDelete, "/api/downloads/abc", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, []string{"abc"}, env.downloads.removed)
	env.downloads.missing = true
	rec = env.do(t, http.MethodDelete, "/api/downloads/abc", "")
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestDownloadsAddReportsUserFacingError(t *testing.T) {
	env := newEnv(t)
	env.downloads.addErr = errs.New(errs.CategoryInput, "torrentstream.private", "这是私有站（PT）的种子，nagare 不下载", "请改用该站推荐的客户端")
	rec := env.do(t, http.MethodPost, "/api/downloads", `{"magnet":"magnet:?xt=urn:btih:abc"}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "私有站")
}

func TestTorrentConfigDownloadDir(t *testing.T) {
	env := newEnv(t)
	rec := env.do(t, http.MethodPost, "/api/torrent/config", `{"downloadDir":"relative/dir"}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)

	dir := filepath.Join(t.TempDir(), "番剧")
	rec = env.do(t, http.MethodPost, "/api/torrent/config", `{"downloadDir":"`+dir+`/"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, dir, env.store.TorrentConfig().DownloadDir)
	assert.Contains(t, rec.Body.String(), `"downloadDirIsDefault":false`)

	rec = env.do(t, http.MethodPost, "/api/torrent/config", `{"downloadDir":""}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Empty(t, env.store.TorrentConfig().DownloadDir)
	assert.Contains(t, rec.Body.String(), `"downloadDir":"/home/you/Downloads/nagare"`)
}
