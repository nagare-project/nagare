package selfupdate

// 本文件负责取字节：小文件（校验清单与签名）整块读进内存，归档边下边算 sha256。
// 约定：错误信息与日志里不出现任何 URL。

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"time"

	errs "github.com/nagare-project/nagare/internal/errors"
	"github.com/nagare-project/nagare/internal/minisign"
)

const (
	// maxMetaBytes 是 checksums.txt / .minisig 的大小上限（实际各不到 2KB）。
	maxMetaBytes = 1 << 20
	// maxArchiveBytes 是归档大小上限。Windows 的 zip 内含 mpv 约 120MB，留足余量。
	maxArchiveBytes = 512 << 20
	// metaTimeout 是取小文件的时长上限。
	metaTimeout = 30 * time.Second
	// archiveTimeout 兜底整个归档下载：慢网也够，挂死的连接不会永远占着更新信号量。
	archiveTimeout = 30 * time.Minute
	// maxRedirects 是允许的重定向跳数。GitHub 会把 releases/download 302 到
	// objects.githubusercontent.com（跨主机但同为 https），这一跳必须放行。
	maxRedirects = 5
)

// newHTTPClient 复制调用方的客户端并套上重定向策略，不改动传入的对象。
func newHTTPClient(base *http.Client) *http.Client {
	hc := &http.Client{}
	if base != nil {
		cp := *base
		hc = &cp
	}
	// 客户端级 Timeout 会把 120MB 的下载一刀切死（它算的是整个请求的墙钟时间），
	// 所以清掉，超时改由每一步各自的 ctx 承担。
	hc.Timeout = 0
	hc.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= maxRedirects {
			return fmt.Errorf("重定向超过 %d 跳", maxRedirects)
		}
		// 跨主机的 302 必须允许（GitHub 就是这么发的），但降级到明文一律拒绝。
		if req.URL.Scheme != "https" {
			return errors.New("更新下载被重定向到非 https 地址")
		}
		return nil
	}
	return hc
}

func (u *Updater) userAgent() string {
	if u.current == "" {
		return "nagare/dev"
	}
	return "nagare/" + u.current
}

// get 发一个 GET 并把非 200 翻成分类错误。成功时返回的响应体由调用方负责关闭。
func (u *Updater) get(ctx context.Context, target, what string) (*http.Response, error) {
	const op = "selfupdate.fetch"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, errs.Wrap(errs.CategoryInternal, op, "构造下载请求失败", "", err)
	}
	req.Header.Set("User-Agent", u.userAgent())
	resp, err := u.client.Do(req)
	if err != nil {
		return nil, errs.Wrap(errs.CategoryNetwork, op, "下载"+what+"失败",
			"请检查网络后重试", stripURL(err))
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		return nil, statusError(op, what, resp.StatusCode)
	}
	return resp, nil
}

func statusError(op, what string, status int) error {
	switch {
	case status == http.StatusNotFound:
		return errs.Wrap(errs.CategoryUpstream, op, "这个版本没有适用于当前平台的更新包",
			"请到项目发布页确认后手动下载", fmt.Errorf("HTTP %d", status))
	case status == http.StatusForbidden, status == http.StatusTooManyRequests:
		return errs.Wrap(errs.CategoryUpstream, op, "下载"+what+"被限流", "请稍后再试",
			fmt.Errorf("HTTP %d", status))
	default:
		return errs.Wrap(errs.CategoryUpstream, op, "下载"+what+"时服务器返回错误",
			"请稍后再试", fmt.Errorf("HTTP %d", status))
	}
}

// fetchMeta 取一个小文件（校验清单或签名）并整块返回，超出上限即视为响应异常。
func (u *Updater) fetchMeta(ctx context.Context, target, what string) ([]byte, error) {
	const op = "selfupdate.fetch"
	ctx, cancel := context.WithTimeout(ctx, metaTimeout)
	defer cancel()

	resp, err := u.get(ctx, target, what)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	buf, err := io.ReadAll(io.LimitReader(resp.Body, maxMetaBytes+1))
	if err != nil {
		return nil, errs.Wrap(errs.CategoryNetwork, op, "读取"+what+"失败",
			"请检查网络后重试", stripURL(err))
	}
	if len(buf) > maxMetaBytes {
		return nil, errs.Wrap(errs.CategoryUpstream, op, what+"异常地大，已中止更新",
			"请到项目发布页手动下载", fmt.Errorf("超过 %d KB 上限", maxMetaBytes>>10))
	}
	return buf, nil
}

// verifiedChecksum 走完信任链的前半段：验签校验清单，再从清单里取出本平台归档的 sha256。
//
// 顺序是硬性的 —— 先验签名再用清单里的任何一个字节。公钥为空时根本不会走到这里
// （Capability 已经拦下），这里再判一次是纵深防御：这个函数是唯一给出「可信哈希」的地方。
func (u *Updater) verifiedChecksum(ctx context.Context, version, asset string) (string, error) {
	const op = "selfupdate.verify"
	if u.pub == "" {
		return "", errs.New(errs.CategoryInternal, op,
			"这个构建没有内嵌更新公钥，无法校验更新包", "请到项目发布页手动下载新版本")
	}
	pub, err := minisign.ParsePublicKey(u.pub)
	if err != nil {
		return "", errs.Wrap(errs.CategoryInternal, op, "内嵌的更新公钥不合法",
			"请到项目发布页手动下载新版本", err)
	}
	manifest, err := u.fetchMeta(ctx, u.assetURL(version, checksumsName), "校验清单")
	if err != nil {
		return "", err
	}
	sig, err := u.fetchMeta(ctx, u.assetURL(version, checksumsName+minisigExt), "校验清单签名")
	if err != nil {
		return "", err
	}
	if err := minisign.Verify(pub, manifest, sig); err != nil {
		return "", errs.Wrap(errs.CategoryUpstream, op, "更新包的签名校验失败，已中止更新",
			"请到项目发布页手动下载；如果反复出现，说明下载来源可能被篡改", err)
	}
	if err := checkTrustedComment(sig, version); err != nil {
		return "", err
	}
	return lookupChecksum(manifest, asset)
}

// checkTrustedComment 校验签名里的可信注释与要装的版本一致。
//
// 可信注释是被签名覆盖的（这正是 minisign 里「trusted」的含义），所以它能挡住一类
// 签名校验看不见的攻击：把某个旧版本【签名完全合法】的 checksums.txt 与归档，
// 摆到新版本的下载路径上。发布流水线写的是 "nagare <版本> checksums"。
//
// 注释里根本没有版本号时只记日志不拦 —— 这条约束住的是流水线，而不是用户；
// 万一将来 -t 参数改了形状，用户不该因此更新不了。
func checkTrustedComment(sig []byte, version string) error {
	comment, err := minisign.TrustedComment(sig)
	if err != nil {
		log.Printf("selfupdate: 读取签名可信注释失败，跳过版本一致性检查：%v", err)
		return nil
	}
	found := versionTokenRE.FindAllString(comment, -1)
	if len(found) == 0 {
		log.Printf("selfupdate: 签名可信注释里没有版本号，跳过一致性检查：%q", trunc(comment, 60))
		return nil
	}
	for _, v := range found {
		if v == version {
			return nil
		}
	}
	return errs.New(errs.CategoryUpstream, "selfupdate.verify",
		"更新包的签名对应的不是要安装的版本，已中止更新",
		"请到项目发布页手动下载；如果反复出现，说明下载来源可能被篡改")
}

// downloadArchive 把归档下到 dst，边下边算 sha256。
// 哈希对不上、超出大小上限、或中途出错都会立刻删掉 dst —— 绝不留下一个没通过校验的文件。
func (u *Updater) downloadArchive(ctx context.Context, target, dst, want string) (err error) {
	const op = "selfupdate.download"
	ctx, cancel := context.WithTimeout(ctx, archiveTimeout)
	defer cancel()

	resp, err := u.get(ctx, target, "更新包")
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	f, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return errs.Wrap(errs.CategoryStorage, op, "创建更新包临时文件失败",
			"请确认磁盘空间充足且安装目录可写", err)
	}
	defer func() {
		cerr := f.Close()
		if err == nil && cerr != nil {
			err = errs.Wrap(errs.CategoryStorage, op, "写入更新包失败",
				"请确认磁盘空间充足", cerr)
		}
		if err != nil {
			// 任何失败路径都不留半个文件：下一次重试必须从零开始。
			_ = os.Remove(dst)
		}
	}()

	sum := sha256.New()
	// 包一层记录写侧错误：io.Copy 只给一个 error，分不清是「网断了」还是「磁盘满了」。
	// 两者的恢复动作完全相反（重试 vs 腾空间），把磁盘满报成网络问题会把用户带偏。
	sink := &recordingWriter{w: io.MultiWriter(f, sum)}
	n, err := io.Copy(sink, io.LimitReader(resp.Body, maxArchiveBytes+1))
	if err != nil {
		if sink.err != nil {
			return errs.Wrap(errs.CategoryStorage, op, "写入更新包失败",
				"请确认磁盘空间充足且安装目录可写", sink.err)
		}
		return errs.Wrap(errs.CategoryNetwork, op, "下载更新包中断",
			"请检查网络后重试", stripURL(err))
	}
	if n > maxArchiveBytes {
		return errs.Wrap(errs.CategoryUpstream, op, "更新包异常地大，已中止更新",
			"请到项目发布页手动下载", fmt.Errorf("超过 %d MB 上限", maxArchiveBytes>>20))
	}
	if got := hex.EncodeToString(sum.Sum(nil)); got != want {
		return errs.New(errs.CategoryUpstream, op, "更新包的校验和与清单不符，已中止更新",
			"请稍后重试；如果反复出现，请到项目发布页手动下载")
	}
	return nil
}

// recordingWriter 记住写侧的第一个错误，供上层区分「读失败」与「写失败」。
type recordingWriter struct {
	w   io.Writer
	err error
}

func (r *recordingWriter) Write(p []byte) (int, error) {
	n, err := r.w.Write(p)
	if err != nil && r.err == nil {
		r.err = err
	}
	return n, err
}

// stripURL 去掉 *url.Error 携带的请求地址（约定：不记 URL），只留底层原因。
func stripURL(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) && ue.Err != nil {
		return ue.Err
	}
	return err
}
