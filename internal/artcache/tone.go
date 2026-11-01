package artcache

import (
	"encoding/json"
	"errors"
	"fmt"
	"image"
	// 只注册标准库的三种解码器：不加依赖，也不解 WebP / AVIF。
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"time"
)

// 海报取色的结果存在封面旁边的边车文件里：<封面>.tone.json。
//
// 为什么落盘：取色要把整张封面解码一遍，每次拼接口响应都做一遍太贵；封面按 URL 的 sha256
// 落盘、内容不变，所以算一次就够。边车里只存色相，不存 sRGB：色阶的明度 / 彩度改了
// （tokens.json），旧的边车照样能用。
//
// 什么时候算：封面下载落盘之后立刻在后台排队；拼响应时发现封面在、边车不在（比如这个功能上线之前
// 就缓存好的封面），也排一次。都是异步的 —— 这一次响应给 null（界面用中性色），下一次就带颜色了。
// 不阻塞响应：一页目录几百张卡，不能让第一次打开等着解码。

const (
	// toneAlgoVersion 是取色算法的版本，写进边车。改了采样或判灰的规则就加一，旧边车自动作废重算。
	toneAlgoVersion   = 1
	toneSidecarSuffix = ".tone.json"

	// maxTonePixels 是肯解码的最大像素数。解码前先用 DecodeConfig 只读文件头拿到尺寸：
	// 8MB 的文件上限挡不住「一张压得很小的超大 PNG」，解开就是几百 MB 内存。
	// 封面通常不到一百万像素，4000 万已经极宽松。
	maxTonePixels = 40_000_000

	// maxPendingTones 是排队等取色的封面数上限。超出的这一次不排，下次拼响应时还会再排。
	maxPendingTones = 512
	// maxToneMemo 是内存里记住的结果数上限；满了整个清空，之后从边车重新读回来。
	maxToneMemo = 8192
	// toneRetryAfter：读封面出错（不是解码失败，是 I/O 失败）之后，隔多久再试。
	toneRetryAfter = 10 * time.Minute
)

// toneReason 是「这张封面没有颜色」的原因，写进边车，排查时看。
type toneReason string

const (
	reasonGrayscale   toneReason = "grayscale"   // 封面本身是灰的（或几乎全透明）
	reasonUnsupported toneReason = "unsupported" // 不是 JPEG / PNG / GIF（WebP、AVIF…）
	reasonTooLarge    toneReason = "too-large"   // 超过 maxTonePixels
	reasonDecode      toneReason = "decode"      // 文件损坏、截断
)

// toneSidecar 是边车文件的内容。Hue 为 null 时 Reason 说明原因。
type toneSidecar struct {
	Version int        `json:"version"`
	Hue     *float64   `json:"hue"`
	Reason  toneReason `json:"reason,omitempty"`
}

// toneResult 是内存里记住的一张封面的结果。failedAt 非零表示上次读封面出错、还没到重试时间。
type toneResult struct {
	tones    *PosterTones
	failedAt time.Time
}

// sampleHueFile 是取色的入口（包级变量：测试替换它来数调用次数、模拟 I/O 失败）。
var sampleHueFile = sampleHueFromFile

// PosterTones 返回 rawURL 那张封面的海报色阶；nil 表示界面用中性色。
//
// 不发网络请求，也不等解码：封面还没缓存 → nil；缓存了但还没算 → 排队并返回 nil；
// 算过了 → 内存或边车里的结果（算过但没有颜色也是 nil）。
func (c *Cache) PosterTones(rawURL string) *PosterTones {
	if rawURL == "" {
		return nil
	}
	path := c.Path(rawURL)
	if tones, known := c.memoTone(path); known {
		return tones
	}
	if tones, found := readToneSidecar(path); found {
		c.rememberTone(path, toneResult{tones: tones})
		return copyTones(tones)
	}
	if st, err := os.Stat(path); err != nil || st.Size() == 0 {
		return nil
	}
	c.scheduleTone(path)
	return nil
}

// memoTone 查内存。known = false 表示内存里没有定论，要去读边车。
func (c *Cache) memoTone(path string) (*PosterTones, bool) {
	c.toneMu.Lock()
	defer c.toneMu.Unlock()
	m, ok := c.toneMemo[path]
	if !ok {
		return nil, false
	}
	if !m.failedAt.IsZero() {
		if c.now().Sub(m.failedAt) < toneRetryAfter {
			return nil, true
		}
		delete(c.toneMemo, path)
		return nil, false
	}
	return copyTones(m.tones), true
}

// copyTones 给每个调用方一份自己的副本：内存里那份被多个响应共用，不能让谁改到它。
func copyTones(t *PosterTones) *PosterTones {
	if t == nil {
		return nil
	}
	cp := *t
	return &cp
}

func (c *Cache) rememberTone(path string, m toneResult) {
	c.toneMu.Lock()
	defer c.toneMu.Unlock()
	if len(c.toneMemo) >= maxToneMemo {
		clear(c.toneMemo)
	}
	c.toneMemo[path] = m
}

// scheduleTone 把一张封面排进取色队列：同一张只排一次，同时只解码一张（控制内存峰值）。
func (c *Cache) scheduleTone(path string) {
	c.toneMu.Lock()
	if c.tonePending[path] || len(c.tonePending) >= maxPendingTones {
		c.toneMu.Unlock()
		return
	}
	if m, ok := c.toneMemo[path]; ok && m.failedAt.IsZero() {
		c.toneMu.Unlock()
		return // 已经有结果了（比如封面重新下载过）
	}
	c.tonePending[path] = true
	c.toneWG.Add(1)
	c.toneMu.Unlock()

	go func() {
		defer c.toneWG.Done()
		defer func() {
			c.toneMu.Lock()
			delete(c.tonePending, path)
			c.toneMu.Unlock()
		}()
		c.toneSem <- struct{}{}
		defer func() { <-c.toneSem }()
		defer func() {
			// 后台 goroutine 里的 panic 会带走整个进程（包括正在放的片子）。标准库解码器不该 panic，
			// 但封面来自外部，这里兜一层：记成「解不了」写进边车，不再重算。
			if r := recover(); r != nil {
				log.Printf("artcache: 封面取色时解码器异常（%s）：%v", filepath.Base(path), r)
				if err := writeToneSidecar(path, toneSidecar{Version: toneAlgoVersion, Reason: reasonDecode}); err != nil {
					log.Printf("artcache: 写取色结果失败（%s）：%v", filepath.Base(path), err)
				}
				c.rememberTone(path, toneResult{})
			}
		}()
		c.computeTone(path)
	}()
}

// computeTone 取色、写边车、记进内存。日志里只有封面文件名（URL 的哈希），不出现 URL。
func (c *Cache) computeTone(path string) {
	if tones, found := readToneSidecar(path); found {
		c.rememberTone(path, toneResult{tones: tones})
		return
	}
	hue, reason, err := sampleHueFile(path)
	if err != nil {
		log.Printf("artcache: 封面取色时读文件失败（%s），%v 后再试：%v", filepath.Base(path), toneRetryAfter, err)
		c.rememberTone(path, toneResult{failedAt: c.now()})
		return
	}
	sc := toneSidecar{Version: toneAlgoVersion}
	var tones *PosterTones
	if reason == "" {
		h := roundHue(hue)
		ladder := LadderFor(h)
		sc.Hue, tones = &h, &ladder
	} else {
		sc.Reason = reason
	}
	if err := writeToneSidecar(path, sc); err != nil {
		// 结果照样记在内存里：这次进程内不重算，下次启动再试着落盘
		log.Printf("artcache: 写取色结果失败（%s）：%v", filepath.Base(path), err)
	}
	c.rememberTone(path, toneResult{tones: tones})
}

// readToneSidecar 读边车。found = false：没有、读不了、格式不对或算法版本旧了 —— 都按「要重算」处理。
func readToneSidecar(coverPath string) (tones *PosterTones, found bool) {
	raw, err := os.ReadFile(coverPath + toneSidecarSuffix)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			log.Printf("artcache: 读取色结果失败（%s），将重算：%v", filepath.Base(coverPath), err)
		}
		return nil, false
	}
	var sc toneSidecar
	if err := json.Unmarshal(raw, &sc); err != nil || sc.Version != toneAlgoVersion {
		return nil, false
	}
	if sc.Hue == nil {
		return nil, true
	}
	ladder := LadderFor(*sc.Hue)
	return &ladder, true
}

// writeToneSidecar 原子落盘：先写临时文件再改名，读的一方永远看不到半个 JSON。
func writeToneSidecar(coverPath string, sc toneSidecar) error {
	data, err := json.Marshal(sc)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(coverPath), ".tone-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // 成功路径上已经改名走了
	_, werr := tmp.Write(data)
	if cerr := tmp.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		return werr
	}
	if err := os.Chmod(tmpName, 0o600); err != nil {
		return err
	}
	return os.Rename(tmpName, coverPath+toneSidecarSuffix)
}

// sampleHueFromFile 打开封面并取色。err 只表示 I/O 失败（会重试）；
// 文件本身解不了是确定的结果，走 reason（写进边车，不再重算）。
func sampleHueFromFile(path string) (float64, toneReason, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, "", err
	}
	defer f.Close()
	return sampleHue(f)
}

// sampleHue 先只读文件头核对格式与尺寸，再整张解码、交给 dominantHue。
func sampleHue(r io.ReadSeeker) (float64, toneReason, error) {
	cfg, format, err := image.DecodeConfig(r)
	if err != nil {
		reason, ioErr := decodeFailure(err)
		return 0, reason, ioErr
	}
	switch format {
	case "jpeg", "png", "gif":
	default:
		// 别的依赖哪天注册了 WebP 之类的解码器，这里也不接：只认标准库的三种
		return 0, reasonUnsupported, nil
	}
	if cfg.Width <= 0 || cfg.Height <= 0 {
		return 0, reasonDecode, nil
	}
	if int64(cfg.Width)*int64(cfg.Height) > maxTonePixels {
		return 0, reasonTooLarge, nil
	}
	if _, err := r.Seek(0, io.SeekStart); err != nil {
		return 0, "", fmt.Errorf("回到封面开头: %w", err)
	}
	img, _, err := image.Decode(r)
	if err != nil {
		reason, ioErr := decodeFailure(err)
		return 0, reason, ioErr
	}
	hue, ok := dominantHue(img)
	if !ok {
		return 0, reasonGrayscale, nil
	}
	return hue, "", nil
}

// decodeFailure 区分「文件读不了」（I/O，返回 error 以便重试）与「文件内容解不了」（确定的结果）。
func decodeFailure(err error) (toneReason, error) {
	var pe *fs.PathError
	switch {
	case errors.As(err, &pe):
		return "", err
	case errors.Is(err, image.ErrFormat):
		return reasonUnsupported, nil
	default:
		return reasonDecode, nil
	}
}
