// 测试公共设施：可记录请求的 httptest 服务器与错误分类断言。
// 全部测试不发真实网络请求（除 client_test.go 里刻意打关闭端口的用例）。
package animego_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/nagare-project/nagare/internal/animego"
)

// recorded 是测试服务器捕获的一次请求快照。
type recorded struct {
	Method  string
	Path    string
	Auth    string // Authorization 头原文
	UA      string
	CT      string // Content-Type
	Cookie  string // refreshToken cookie 值（无则空）
	HasBody bool
	Body    []byte
}

// recorder 线程安全地记录请求 —— httptest 的 handler 跑在别的 goroutine，
// -race 下必须靠锁建立同步边界。
type recorder struct {
	mu   sync.Mutex
	reqs []recorded
}

func (rec *recorder) capture(r *http.Request) {
	body, _ := io.ReadAll(r.Body) // 测试服务器，读失败让后续断言自然暴露
	entry := recorded{
		Method:  r.Method,
		Path:    r.URL.Path,
		Auth:    r.Header.Get("Authorization"),
		UA:      r.Header.Get("User-Agent"),
		CT:      r.Header.Get("Content-Type"),
		HasBody: len(body) > 0,
		Body:    body,
	}
	if ck, err := r.Cookie("refreshToken"); err == nil {
		entry.Cookie = ck.Value
	}
	rec.mu.Lock()
	rec.reqs = append(rec.reqs, entry)
	rec.mu.Unlock()
}

// all 返回全部快照的副本。
func (rec *recorder) all() []recorded {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	out := make([]recorded, len(rec.reqs))
	copy(out, rec.reqs)
	return out
}

// filter 返回路径前缀匹配的快照。
func (rec *recorder) filter(pathPrefix string) []recorded {
	var out []recorded
	for _, r := range rec.all() {
		if strings.HasPrefix(r.Path, pathPrefix) {
			out = append(out, r)
		}
	}
	return out
}

// newTestClient 起一个记录请求的测试服务器，并返回指向它的客户端。
// rec 可为 nil（不需要断言请求形状时）。
func newTestClient(t *testing.T, rec *recorder, h http.HandlerFunc) *animego.Client {
	t.Helper()
	url := newRecordingServer(t, rec, h)
	return animego.New(animego.Options{BaseURL: url, UserAgent: "nagare/test"})
}

// newRecordingServer 与 newTestClient 相同，但返回服务器 URL 而非客户端 ——
// 给需要自定义 Options（如缺省 UA、末尾斜杠）的测试用。
func newRecordingServer(t *testing.T, rec *recorder, h http.HandlerFunc) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if rec != nil {
			rec.capture(r)
		}
		h(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

// respond 写一个 JSON 响应（错误忽略：httptest 内存管道不会失败）。
func respond(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, body)
}

// assertKind 断言错误是 *animego.Error 且分类符合预期。
func assertKind(t *testing.T, err error, want animego.ErrKind) *animego.Error {
	t.Helper()
	require.Error(t, err)
	var ae *animego.Error
	require.ErrorAs(t, err, &ae, "错误应是 *animego.Error，实际: %v", err)
	require.Equal(t, want, ae.Kind, "错误分类不符，完整错误: %v", err)
	return ae
}
