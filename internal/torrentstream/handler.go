// 流端点：mpv 带 Range 头来拉字节。
//
// 挂载点是 /stream/{capability}/...，httpserver 校验完能力段后把路径改写成
// /t/{infohash}/{index} 再转进来。能力段因此从不流经本文件，也就不可能被写进
// 日志或错误信息。
package torrentstream

import (
	"context"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/anacrolix/torrent"
)

const (
	// readerReadahead 是种子 reader 的预读窗口。
	readerReadahead = 5 << 20
	// infohashHexLen 是 v1 infohash 的十六进制长度（v2 也截断到 20 字节）。
	infohashHexLen = 40
)

// streamTarget 是流处理器对「当前可服务的媒体」的全部需求。
type streamTarget struct {
	name string
	open func(ctx context.Context, startAt int64) (io.ReadSeekCloser, error)
}

// streamSource 把「怎么找到文件与 reader」收成一个窄接口：
// handler 因此可以脱离真实种子做测试。
type streamSource interface {
	streamFor(infohash string, index int) *streamTarget
}

type streamHandler struct{ src streamSource }

func (h streamHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	infohash, index, ok := parseStreamPath(r.URL.Path)
	if !ok {
		streamNotFound(w)
		return
	}
	target := h.src.streamFor(infohash, index)
	if target == nil {
		// 路径合法但对不上当前会话，同样 404 而不是 400：
		// 「这个种子存在过但现在没在播」对探测者也是信息。
		streamNotFound(w)
		return
	}

	w.Header().Set("Accept-Ranges", "bytes")
	// 按 Range 起点开 reader：优先级窗口在 ServeContent 解析请求之前就挪到
	// 请求区间，分片请求早一步发出去。
	body, err := target.open(r.Context(), rangeStart(r.Header.Get("Range")))
	if err != nil {
		streamNotFound(w)
		return
	}
	defer body.Close()

	// 206 / Range / 断点续传全部交给标准库，自己绝不解析 Range 语义。
	// modtime 传零值：种子内容不可变，没有条件请求可谈。
	// mpv 退出导致的读中断由 r.Context() 收敛，不是错误，不记日志。
	http.ServeContent(w, r, target.name, time.Time{}, body)
}

func streamNotFound(w http.ResponseWriter) {
	http.Error(w, "未找到", http.StatusNotFound)
}

// parseStreamPath 解析 /t/{infohash}/{index}。形状不对一律判失败（调用方回 404）。
func parseStreamPath(p string) (infohash string, index int, ok bool) {
	segs := strings.Split(strings.TrimPrefix(p, "/"), "/")
	if len(segs) != 3 || segs[0] != "t" {
		return "", 0, false
	}
	infohash = strings.ToLower(segs[1])
	if len(infohash) != infohashHexLen || !isLowerHex(infohash) {
		return "", 0, false
	}
	index, err := strconv.Atoi(segs[2])
	if err != nil || index < 0 {
		return "", 0, false
	}
	return infohash, index, true
}

func isLowerHex(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// rangeStart 取出请求区间的起点，只认最常见的 "bytes=<start>-[end]"。
// 取不到就当 0 —— 这只是给优先级窗口的提示，真正的区间语义由 ServeContent 决定。
func rangeStart(header string) int64 {
	const prefix = "bytes="
	if !strings.HasPrefix(header, prefix) {
		return 0
	}
	spec := header[len(prefix):]
	if i := strings.IndexByte(spec, ','); i >= 0 {
		spec = spec[:i]
	}
	dash := strings.IndexByte(spec, '-')
	if dash <= 0 {
		// 后缀式 "-500"（最后 500 字节）没有可用的起点，交给 ServeContent。
		return 0
	}
	n, err := strconv.ParseInt(strings.TrimSpace(spec[:dash]), 10, 64)
	if err != nil || n < 0 {
		return 0
	}
	return n
}

// streamFor 实现 streamSource：只有与当前会话完全对上的 infohash + index 才给流。
func (e *Engine) streamFor(infohash string, index int) *streamTarget {
	e.mu.Lock()
	sess := e.sess
	e.mu.Unlock()
	if sess == nil {
		return nil
	}
	return sess.target(infohash, index)
}

// target 返回当前会话可服务的流；对不上返回 nil。
func (s *session) target(infohash string, index int) *streamTarget {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.file == nil || s.index != index || !strings.EqualFold(s.infohash, infohash) {
		return nil
	}
	file, pm, name := s.file, s.pm, s.item.FileName
	return &streamTarget{
		name: name,
		open: func(ctx context.Context, startAt int64) (io.ReadSeekCloser, error) {
			return openReader(ctx, file, pm, startAt), nil
		},
	}
}

// openReader 开一个跟随优先级窗口的种子 reader。
func openReader(ctx context.Context, file *torrent.File, pm *priorityManager, startAt int64) io.ReadSeekCloser {
	r := file.NewReader()
	r.SetContext(ctx)
	r.SetResponsive()
	r.SetReadahead(readerReadahead)
	tracked := &trackedReader{inner: r, pm: pm}
	if pm != nil {
		tracked.id = pm.addReader(startAt)
	}
	return tracked
}

// trackedReader 把 reader 的实际读位置喂给优先级管理器。
//
// 只在 Read 之后上报、不在 Seek 时上报：http.ServeContent 会先 Seek 到文件尾取
// 长度、再 Seek 回开头，跟着那两跳挪窗口只会让优先级来回抖动。真正决定该先下
// 哪些分片的是字节被消费的位置。
type trackedReader struct {
	inner  torrent.Reader
	pm     *priorityManager
	id     int
	pos    int64
	closed bool
}

func (t *trackedReader) Read(p []byte) (int, error) {
	n, err := t.inner.Read(p)
	if n > 0 {
		t.pos += int64(n)
		if t.pm != nil {
			t.pm.report(t.id, t.pos)
		}
	}
	return n, err
}

func (t *trackedReader) Seek(offset int64, whence int) (int64, error) {
	pos, err := t.inner.Seek(offset, whence)
	if err == nil {
		t.pos = pos
	}
	return pos, err
}

func (t *trackedReader) Close() error {
	if t.closed {
		return nil
	}
	t.closed = true
	if t.pm != nil {
		t.pm.removeReader(t.id)
	}
	return t.inner.Close()
}
