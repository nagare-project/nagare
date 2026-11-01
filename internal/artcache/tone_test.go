package artcache

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image/color"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const toneTestURL = "https://img.example/cover/blue.jpg"

// countSampler 把取色入口换成计数的包装，测试结束换回来。
func countSampler(t *testing.T) *atomic.Int32 {
	t.Helper()
	var n atomic.Int32
	prev := sampleHueFile
	sampleHueFile = func(path string) (float64, toneReason, error) {
		n.Add(1)
		return prev(path)
	}
	t.Cleanup(func() { sampleHueFile = prev })
	return &n
}

// placeCover 把一张图当作「已经缓存好的封面」放到 rawURL 对应的位置。
func placeCover(t *testing.T, c *Cache, rawURL string, data []byte) string {
	t.Helper()
	path := c.Path(rawURL)
	require.NoError(t, os.WriteFile(path, data, 0o600))
	return path
}

func readSidecar(t *testing.T, coverPath string) toneSidecar {
	t.Helper()
	raw, err := os.ReadFile(coverPath + toneSidecarSuffix)
	require.NoError(t, err)
	var sc toneSidecar
	require.NoError(t, json.Unmarshal(raw, &sc))
	return sc
}

func TestPosterTonesWithoutCover(t *testing.T) {
	c := newTestCache(t)
	calls := countSampler(t)
	assert.Nil(t, c.PosterTones(""))
	assert.Nil(t, c.PosterTones(toneTestURL), "封面还没缓存：没有颜色，也不去下载")
	c.toneWG.Wait()
	assert.Zero(t, calls.Load())
	assert.NoFileExists(t, c.Path(toneTestURL)+toneSidecarSuffix)
}

// 封面在、边车不在 → 这次给 null 并在后台算，下一次带颜色；边车只写一次。
func TestPosterTonesComputesMissingSidecarOnce(t *testing.T) {
	c := newTestCache(t)
	calls := countSampler(t)
	path := placeCover(t, c, toneTestURL, encodePNG(t, paint(30, 40, solid(blue))))

	var wg sync.WaitGroup
	for range 8 { // 一屏同时请求同一张封面
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.PosterTones(toneTestURL)
		}()
	}
	wg.Wait()
	c.toneWG.Wait()
	assert.EqualValues(t, 1, calls.Load(), "同一张封面只解码一次")

	sc := readSidecar(t, path)
	assert.Equal(t, toneAlgoVersion, sc.Version)
	require.NotNil(t, sc.Hue)
	assert.InDelta(t, 0, hueDiff(hueBlue, *sc.Hue), 0.1)
	st, err := os.Stat(path + toneSidecarSuffix)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), st.Mode().Perm())

	got := c.PosterTones(toneTestURL)
	require.NotNil(t, got)
	assert.Equal(t, LadderFor(*sc.Hue), *got)
	c.toneWG.Wait()
	assert.EqualValues(t, 1, calls.Load(), "算过就不再算")
}

// 边车是持久的：重启（新的 Cache、内存是空的）之后直接读边车，不再解码封面。
func TestPosterTonesReusesSidecarAcrossRestarts(t *testing.T) {
	c := newTestCache(t)
	path := placeCover(t, c, toneTestURL, encodePNG(t, paint(30, 40, solid(green))))
	c.PosterTones(toneTestURL)
	c.toneWG.Wait()
	before := readSidecar(t, path)

	restarted, err := New(c.dir)
	require.NoError(t, err)
	calls := countSampler(t)
	// 把封面换成解不了的内容：真去重算就拿不到颜色，能拿到说明读的是边车
	require.NoError(t, os.WriteFile(path, []byte("not an image"), 0o600))

	got := restarted.PosterTones(toneTestURL)
	require.NotNil(t, got, "第一次请求就有颜色，不用等")
	assert.Equal(t, *before.Hue, got.Hue)
	restarted.toneWG.Wait()
	assert.Zero(t, calls.Load())
}

// 算法版本变了，旧边车作废重算。
func TestPosterTonesRecomputesStaleSidecar(t *testing.T) {
	c := newTestCache(t)
	path := placeCover(t, c, toneTestURL, encodePNG(t, paint(30, 40, solid(red))))
	require.NoError(t, os.WriteFile(path+toneSidecarSuffix, []byte(`{"version":0,"hue":200}`), 0o600))

	assert.Nil(t, c.PosterTones(toneTestURL))
	c.toneWG.Wait()
	sc := readSidecar(t, path)
	assert.Equal(t, toneAlgoVersion, sc.Version)
	require.NotNil(t, c.PosterTones(toneTestURL))
	assert.InDelta(t, 0, hueDiff(hueRed, c.PosterTones(toneTestURL).Hue), 0.1)
}

// 没有颜色也是结论：灰封面、WebP 写进边车，之后不再解码。
func TestPosterTonesRemembersNoColour(t *testing.T) {
	for name, data := range map[string][]byte{
		"灰":    encodePNG(t, paint(20, 20, solid(grey(90)))),
		"WebP": append([]byte("RIFF\x24\x00\x00\x00WEBPVP8 \x18\x00\x00\x00"), make([]byte, 24)...),
	} {
		t.Run(name, func(t *testing.T) {
			c := newTestCache(t)
			calls := countSampler(t)
			path := placeCover(t, c, toneTestURL, data)
			assert.Nil(t, c.PosterTones(toneTestURL))
			c.toneWG.Wait()
			sc := readSidecar(t, path)
			assert.Nil(t, sc.Hue)
			assert.NotEmpty(t, sc.Reason)

			fresh, err := New(c.dir)
			require.NoError(t, err)
			assert.Nil(t, fresh.PosterTones(toneTestURL))
			assert.Nil(t, c.PosterTones(toneTestURL))
			fresh.toneWG.Wait()
			c.toneWG.Wait()
			assert.EqualValues(t, 1, calls.Load())
		})
	}
}

// 读封面出错（I/O）不写边车，隔一段时间再试，期间不反复重试。
func TestPosterTonesRetriesIOFailureLater(t *testing.T) {
	c := newTestCache(t)
	now := time.Unix(1_800_000_000, 0)
	c.now = func() time.Time { return now }
	var calls atomic.Int32
	prev := sampleHueFile
	sampleHueFile = func(string) (float64, toneReason, error) {
		calls.Add(1)
		return 0, "", errors.New("模拟的读盘失败")
	}
	t.Cleanup(func() { sampleHueFile = prev })
	path := placeCover(t, c, toneTestURL, encodePNG(t, paint(8, 8, solid(red))))

	c.PosterTones(toneTestURL)
	c.toneWG.Wait()
	c.PosterTones(toneTestURL)
	c.toneWG.Wait()
	assert.EqualValues(t, 1, calls.Load(), "重试时间没到")
	assert.NoFileExists(t, path+toneSidecarSuffix)

	// 过了重试时间、读盘恢复正常（换回真正的取色函数）：这次就算出来了
	now = now.Add(toneRetryAfter + time.Second)
	sampleHueFile = prev
	assert.Nil(t, c.PosterTones(toneTestURL), "重新排队，这一次仍是 null")
	c.toneWG.Wait()
	require.NotNil(t, c.PosterTones(toneTestURL))
}

// 新下载的封面落盘就排队取色。
func TestDownloadSchedulesTone(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(encodePNG(t, paint(16, 16, solid(color.NRGBA{200, 40, 160, 255}))))
	}))
	defer srv.Close()

	c := newTestCache(t)
	path, err := c.getUnvalidated(context.Background(), srv.URL)
	require.NoError(t, err)
	c.toneWG.Wait()
	sc := readSidecar(t, path)
	require.NotNil(t, sc.Hue)
	require.NotNil(t, c.PosterTones(srv.URL))
}

// 封面请求优先要 JPEG / PNG：WebP 取不到颜色。
func TestDownloadPrefersDecodableFormats(t *testing.T) {
	var accept atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		accept.Store(r.Header.Get("Accept"))
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(pngBytes)
	}))
	defer srv.Close()

	c := newTestCache(t)
	require.NoError(t, c.download(context.Background(), srv.URL, c.Path(srv.URL)))
	assert.Equal(t, "image/jpeg,image/png;q=0.9,image/*;q=0.1", accept.Load())
}

// 取色时 panic 不带走进程：按读盘失败处理（不写边车，过了重试间隔再试）。
func TestPosterTonesSurvivesPanic(t *testing.T) {
	c := newTestCache(t)
	var calls atomic.Int32
	prev := sampleHueFile
	sampleHueFile = func(string) (float64, toneReason, error) {
		calls.Add(1)
		panic("模拟的解码器崩溃")
	}
	t.Cleanup(func() { sampleHueFile = prev })
	path := placeCover(t, c, toneTestURL, encodePNG(t, paint(8, 8, solid(red))))

	assert.Nil(t, c.PosterTones(toneTestURL))
	c.toneWG.Wait()
	assert.NoFileExists(t, path+toneSidecarSuffix, "panic 可能出在自己的代码里，不能记成永久的「解不了」")
	assert.Nil(t, c.PosterTones(toneTestURL))
	c.toneWG.Wait()
	assert.EqualValues(t, 1, calls.Load(), "重试间隔内不再试")
}

// 内存表满了只清算好的结果，失败记录留着：否则重试间隔形同虚设。
func TestRememberToneKeepsFailuresWhenFull(t *testing.T) {
	c := newTestCache(t)
	c.rememberTone("failed", toneResult{failedAt: c.now()})
	for i := range maxToneMemo {
		c.rememberTone(fmt.Sprintf("ok-%d", i), toneResult{})
	}
	_, known := c.memoTone("failed")
	assert.True(t, known)
	assert.Less(t, len(c.toneMemo), maxToneMemo)
}
