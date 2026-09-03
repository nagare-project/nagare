package torrentstream

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testInfohash = "0123456789abcdef0123456789abcdef01234567"

// nopSeekCloser 让内存里的字节能被 http.ServeContent 消费。
type nopSeekCloser struct{ *bytes.Reader }

func (nopSeekCloser) Close() error { return nil }

// fakeStreamSource 把「怎么拿到文件与 reader」替换成内存实现，
// 于是 handler 的路由、404 判定与 Range 转交都能脱离真实种子测试。
type fakeStreamSource struct {
	infohash  string
	index     int
	name      string
	data      []byte
	opens     int
	lastStart int64
}

func (f *fakeStreamSource) streamFor(infohash string, index int) *streamTarget {
	if infohash != f.infohash || index != f.index {
		return nil
	}
	return &streamTarget{
		name: f.name,
		open: func(context.Context, int64) (io.ReadSeekCloser, error) {
			return nopSeekCloser{bytes.NewReader(f.data)}, nil
		},
	}
}

func newTestHandler() (http.Handler, *fakeStreamSource) {
	src := &fakeStreamSource{
		infohash: testInfohash,
		index:    3,
		name:     "[DBD-Raws][摇曳露营△][01][1080P][BDRip].mkv",
		data:     []byte("0123456789abcdefghij"),
	}
	// 用一层包装记录 open 的起点，验证 Range 起点确实被提前交给了优先级窗口。
	wrapped := &recordingSource{inner: src}
	return streamHandler{src: wrapped}, src
}

type recordingSource struct{ inner *fakeStreamSource }

func (r *recordingSource) streamFor(infohash string, index int) *streamTarget {
	target := r.inner.streamFor(infohash, index)
	if target == nil {
		return nil
	}
	open := target.open
	target.open = func(ctx context.Context, startAt int64) (io.ReadSeekCloser, error) {
		r.inner.opens++
		r.inner.lastStart = startAt
		return open(ctx, startAt)
	}
	return target
}

func doGet(h http.Handler, path, rangeHeader string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if rangeHeader != "" {
		req.Header.Set("Range", rangeHeader)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestStreamHandlerServesFullBody(t *testing.T) {
	h, src := newTestHandler()
	rec := doGet(h, "/t/"+testInfohash+"/3", "")

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, string(src.data), rec.Body.String())
	assert.Equal(t, "bytes", rec.Header().Get("Accept-Ranges"))
	assert.Equal(t, int64(0), src.lastStart, "无 Range 时窗口起点是 0")
}

func TestStreamHandlerServesRange(t *testing.T) {
	h, src := newTestHandler()
	rec := doGet(h, "/t/"+testInfohash+"/3", "bytes=4-9")

	require.Equal(t, http.StatusPartialContent, rec.Code)
	assert.Equal(t, "456789", rec.Body.String())
	assert.Equal(t, "bytes 4-9/20", rec.Header().Get("Content-Range"))
	assert.Equal(t, int64(4), src.lastStart, "Range 起点应在开 reader 时就交给优先级窗口")
}

func TestStreamHandlerServesOpenEndedRange(t *testing.T) {
	h, src := newTestHandler()
	rec := doGet(h, "/t/"+testInfohash+"/3", "bytes=10-")

	require.Equal(t, http.StatusPartialContent, rec.Code)
	assert.Equal(t, "abcdefghij", rec.Body.String())
	assert.Equal(t, int64(10), src.lastStart)
}

func TestStreamHandlerNotFound(t *testing.T) {
	tests := []struct {
		name string
		path string
	}{
		{"路径段数不对", "/t/" + testInfohash},
		{"多余的路径段", "/t/" + testInfohash + "/3/extra"},
		{"前缀不是 t", "/x/" + testInfohash + "/3"},
		{"infohash 不是十六进制", "/t/" + testInfohash[:39] + "z/3"},
		{"infohash 长度不对", "/t/abcdef/3"},
		{"下标不是数字", "/t/" + testInfohash + "/abc"},
		{"下标为负", "/t/" + testInfohash + "/-1"},
		{"infohash 与当前会话不符", "/t/" + "89abcdef0123456789abcdef0123456789abcdef" + "/3"},
		{"文件下标与当前会话不符", "/t/" + testInfohash + "/4"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h, _ := newTestHandler()
			rec := doGet(h, tc.path, "")
			assert.Equal(t, http.StatusNotFound, rec.Code, "不合法请求一律 404，不泄露端点是否存在")
		})
	}
}

func TestStreamHandlerAcceptsUppercaseInfohash(t *testing.T) {
	h, _ := newTestHandler()
	upper := "0123456789ABCDEF0123456789ABCDEF01234567"
	rec := doGet(h, "/t/"+upper+"/3", "")
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestRangeStart(t *testing.T) {
	tests := []struct {
		header string
		want   int64
	}{
		{"", 0},
		{"bytes=0-", 0},
		{"bytes=1234-", 1234},
		{"bytes=1234-5678", 1234},
		{"bytes=100-200,300-400", 100},
		{"bytes=-500", 0}, // 后缀式没有可用起点，交给 ServeContent
		{"bytes=abc-", 0},
		{"items=1-2", 0},
	}
	for _, tc := range tests {
		t.Run(tc.header, func(t *testing.T) {
			assert.Equal(t, tc.want, rangeStart(tc.header))
		})
	}
}
