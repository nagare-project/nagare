package rules

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// maxBodyBytes 限制上游响应体大小（一页搜索结果远小于此）。
const maxBodyBytes = 8 << 20

// Fetcher 负责按规则发请求；HTTP 客户端与 UA 由调用方注入。
type Fetcher struct {
	Client    *http.Client
	UserAgent string
}

// BuildURL 把关键词填进 URL 模板（url.QueryEscape，与旧适配器 url.Values.Encode 等价）。
func BuildURL(rule *Rule, query string) string {
	return strings.ReplaceAll(rule.Request.URL, "{{query}}", url.QueryEscape(query))
}

// Run 执行一次搜索：请求 + Evaluate。任何失败都落进 Outcome（State=failed），不返回 error，
// 这样聚合层可以把「一个源坏了」当成结果的一部分而不是异常。
func (f *Fetcher) Run(ctx context.Context, rule *Rule, query string) Outcome {
	start := time.Now()
	timeout := time.Duration(rule.Request.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = defaultTimeoutSeconds * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	fail := func(reason, detail string) Outcome {
		return Outcome{Source: rule.ID, State: StateFailed, Reason: reason, Detail: detail, LatencyMs: time.Since(start).Milliseconds()}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, BuildURL(rule, query), nil)
	if err != nil {
		return fail("规则里的请求地址不合法", err.Error())
	}
	req.Header.Set("User-Agent", f.userAgent())
	for k, v := range rule.Request.Headers {
		req.Header.Set(k, v)
	}
	client := f.Client
	if client == nil {
		client = http.DefaultClient
	}
	res, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return fail("源站响应超时", err.Error())
		}
		return fail("无法连接源站", err.Error())
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return fail(fmt.Sprintf("源站返回 HTTP %d", res.StatusCode), res.Status)
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, maxBodyBytes))
	if err != nil {
		return fail("读取源站响应失败", err.Error())
	}
	out := Evaluate(rule, body)
	out.LatencyMs = time.Since(start).Milliseconds()
	return out
}

func (f *Fetcher) userAgent() string {
	if f.UserAgent != "" {
		return f.UserAgent
	}
	return "nagare"
}
