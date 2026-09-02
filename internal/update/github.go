package update

// 本文件负责 GitHub Releases API 的请求与响应校验。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	errs "github.com/nagare-project/nagare/internal/errors"
)

const (
	githubAPIBase    = "https://api.github.com"
	githubWebBase    = "https://github.com"
	githubAPIVersion = "2022-11-28"
	// maxBodyBytes 是响应体上限（一条 release 的 JSON 远小于此）。
	maxBodyBytes = 1 << 20
	// fetchTimeout 兜底整次检查的时长，即使注入的客户端没设超时。
	fetchTimeout = 15 * time.Second
	// maxLogField 限制写进日志的服务端字段长度（服务端内容不可信）。
	maxLogField = 40
)

// tagRE 是可接受的发布标签：vX.Y.Z 或 vX.Y.Z-预发布。
var tagRE = regexp.MustCompile(`^v\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?$`)

// release 是校验后的最新发布。Version 不带 v 前缀；空表示尚无正式发布。
type release struct {
	Version string
	URL     string
}

// ghRelease 只取 GitHub 响应里用到的字段。
type ghRelease struct {
	TagName    string `json:"tag_name"`
	HTMLURL    string `json:"html_url"`
	Draft      bool   `json:"draft"`
	Prerelease bool   `json:"prerelease"`
}

func (c *Checker) releaseURL() string {
	return c.apiBase + "/repos/" + c.repo + "/releases/latest"
}

func (c *Checker) userAgent() string {
	if c.current == "" {
		return "nagare/dev"
	}
	return "nagare/" + c.current
}

// fetchLatest 请求 latest 端点并分类结果：
// 200 → 解析校验；404 → 尚无发布（不是错误）；403/429 → 限流；3xx → 响应异常（不跟随）；
// 其余 → 上游错误。网络层失败归 CategoryNetwork，上游/内容问题归 CategoryUpstream。
func (c *Checker) fetchLatest(ctx context.Context) (release, error) {
	const op = "update.fetch"
	ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.releaseURL(), nil)
	if err != nil {
		return release{}, errs.Wrap(errs.CategoryInternal, op, "构造更新请求失败", "", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", githubAPIVersion)
	req.Header.Set("User-Agent", c.userAgent())

	resp, err := c.client.Do(req)
	if err != nil {
		return release{}, errs.Wrap(errs.CategoryNetwork, op,
			"检查更新时无法连接 GitHub", "请检查网络后重试", stripURL(err))
	}
	defer resp.Body.Close()

	if err := classifyStatus(op, resp.StatusCode); err != nil {
		return release{}, err
	}
	if resp.StatusCode == http.StatusNotFound {
		return release{}, nil
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes+1))
	if err != nil {
		return release{}, errs.Wrap(errs.CategoryNetwork, op,
			"读取更新响应失败", "请检查网络后重试", stripURL(err))
	}
	if len(body) > maxBodyBytes {
		return release{}, upstreamAbnormal(op, fmt.Errorf("响应体超过 %d KB 上限", maxBodyBytes>>10))
	}
	return parseRelease(c.repo, body)
}

// classifyStatus 把非成功状态码翻成分类错误；200 与 404 返回 nil（404 由调用方当「尚无发布」）。
func classifyStatus(op string, status int) error {
	switch {
	case status == http.StatusOK, status == http.StatusNotFound:
		return nil
	case status == http.StatusForbidden, status == http.StatusTooManyRequests:
		return errs.Wrap(errs.CategoryUpstream, op, "GitHub 接口限流", "请稍后再试",
			fmt.Errorf("HTTP %d", status))
	case status >= 300 && status < 400:
		return upstreamAbnormal(op, fmt.Errorf("意外的重定向 HTTP %d", status))
	default:
		return errs.Wrap(errs.CategoryUpstream, op, "更新服务器返回错误", "请稍后再试",
			fmt.Errorf("HTTP %d", status))
	}
}

// parseRelease 解析并校验响应体。draft / prerelease 视为「尚无正式发布」。
func parseRelease(repo string, body []byte) (release, error) {
	const op = "update.parse"
	var gr ghRelease
	if err := json.Unmarshal(body, &gr); err != nil {
		return release{}, upstreamAbnormal(op, fmt.Errorf("响应不是合法 JSON: %w", err))
	}
	if gr.Draft || gr.Prerelease {
		return release{}, nil
	}
	return validateRelease(op, repo, gr.TagName, gr.HTMLURL)
}

// validateRelease 是 tag 与发布页地址的唯一把关点（缓存读回也走这里）：
// tag 必须是 vX.Y.Z[-pre]，html_url 必须落在本仓库的 releases 下 —— 界面会把它渲染成链接。
func validateRelease(op, repo, tag, htmlURL string) (release, error) {
	if !tagRE.MatchString(tag) {
		return release{}, upstreamAbnormal(op, fmt.Errorf("版本标签格式不合法: %q", trunc(tag)))
	}
	prefix := githubWebBase + "/" + repo + "/releases/"
	if !strings.HasPrefix(htmlURL, prefix) {
		// 有意不把地址本身写进错误（日志不记 URL）。
		return release{}, upstreamAbnormal(op, errors.New("发布页地址不在本仓库下"))
	}
	return release{Version: strings.TrimPrefix(tag, "v"), URL: htmlURL}, nil
}

// upstreamAbnormal 构造「响应异常」错误：用户无能为力，只能等上游或新版本修。
func upstreamAbnormal(op string, cause error) error {
	return errs.Wrap(errs.CategoryUpstream, op, "更新服务器响应异常", "", cause)
}

// stripURL 去掉 *url.Error 里携带的请求地址（约定：日志不记 URL），只留底层原因。
func stripURL(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) && ue.Err != nil {
		return ue.Err
	}
	return err
}

// trunc 截断要写进日志的服务端字段。
func trunc(s string) string {
	if len(s) <= maxLogField {
		return s
	}
	return s[:maxLogField] + "…"
}
