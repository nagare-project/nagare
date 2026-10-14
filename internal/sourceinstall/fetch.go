// Package sourceinstall 下载、校验并保存用户确认安装的独立来源文件。
//
// nagare 不内置、不推荐任何来源：地址只来自用户点击的安装链接或手动粘贴，
// 文件内容交给用户已安装的来源插件校验，用户看过摘要并确认后才落盘。
package sourceinstall

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	// MaxSourceBytes 与插件端独立来源的上限一致。
	MaxSourceBytes  = 1 << 20
	maxURLBytes     = 2048
	maxRedirects    = 3
	downloadTimeout = 20 * time.Second
)

// Download 是一次下载的结果。SHA256 覆盖 Content 的原始字节。
type Download struct {
	URL     string
	Content []byte
	SHA256  string
	Format  string
}

// Fetcher 只走 https，并在连接前拒绝回环与内网地址（安装链接来自网页，不可信）。
type Fetcher struct {
	Client    *http.Client
	UserAgent string
}

func NewFetcher(userAgent string) *Fetcher {
	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           guardedDial,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 15 * time.Second,
		MaxIdleConns:          2,
		IdleConnTimeout:       30 * time.Second,
	}
	return &Fetcher{
		Client:    &http.Client{Transport: transport, Timeout: downloadTimeout, CheckRedirect: checkRedirect},
		UserAgent: userAgent,
	}
}

// ValidateURL 规范化并检查来源文件地址：必须是 https、带主机名、不含账号与片段。
func ValidateURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.New("来源文件地址不能为空")
	}
	if len(raw) > maxURLBytes {
		return "", errors.New("来源文件地址过长")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.Hostname() == "" {
		return "", errors.New("来源文件地址不合法")
	}
	if u.Scheme != "https" {
		return "", errors.New("来源文件地址必须是 https")
	}
	if u.User != nil {
		return "", errors.New("来源文件地址不能包含账号信息")
	}
	if u.Fragment != "" || strings.Contains(raw, "#") {
		return "", errors.New("来源文件地址不能包含片段（#）")
	}
	return u.String(), nil
}

// NormalizeSHA256 接受 64 位十六进制摘要（大小写均可），返回小写形式。
func NormalizeSHA256(value string) (string, bool) {
	value = strings.ToLower(strings.TrimSpace(value))
	if len(value) != sha256.Size*2 {
		return "", false
	}
	if _, err := hex.DecodeString(value); err != nil {
		return "", false
	}
	return value, true
}

// Fetch 下载来源文件并计算摘要。内容必须是 UTF-8 文本且不超过 MaxSourceBytes。
func (f *Fetcher) Fetch(ctx context.Context, rawURL string) (Download, error) {
	normalized, err := ValidateURL(rawURL)
	if err != nil {
		return Download{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, normalized, nil)
	if err != nil {
		return Download{}, err
	}
	if f.UserAgent != "" {
		request.Header.Set("User-Agent", f.UserAgent)
	}
	request.Header.Set("Accept", "application/json, application/yaml, text/plain;q=0.8, */*;q=0.1")
	response, err := f.Client.Do(request)
	if err != nil {
		// url.Error 会把完整地址拼进消息；界面只需要原因，地址已经展示过。
		var urlError *url.Error
		if errors.As(err, &urlError) {
			return Download{}, fmt.Errorf("下载来源文件失败：%w", urlError.Err)
		}
		return Download{}, fmt.Errorf("下载来源文件失败：%w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return Download{}, fmt.Errorf("下载来源文件失败：服务器返回 HTTP %d", response.StatusCode)
	}
	if response.ContentLength > MaxSourceBytes {
		return Download{}, errors.New("来源文件超过 1 MiB")
	}
	content, err := io.ReadAll(io.LimitReader(response.Body, MaxSourceBytes+1))
	if err != nil {
		return Download{}, fmt.Errorf("读取来源文件失败：%w", err)
	}
	if len(content) > MaxSourceBytes {
		return Download{}, errors.New("来源文件超过 1 MiB")
	}
	if len(bytes.TrimSpace(content)) == 0 {
		return Download{}, errors.New("来源文件是空的")
	}
	if !utf8.Valid(content) {
		return Download{}, errors.New("来源文件不是 UTF-8 文本")
	}
	digest := sha256.Sum256(content)
	return Download{
		URL: normalized, Content: content, SHA256: hex.EncodeToString(digest[:]), Format: DetectFormat(content),
	}, nil
}

// DetectFormat 以首个非空白字符区分 JSON 与 YAML；JSON 也是合法 YAML，但按 JSON 校验更严格。
func DetectFormat(content []byte) string {
	trimmed := bytes.TrimLeft(bytes.TrimPrefix(content, []byte("\xef\xbb\xbf")), " \t\r\n")
	if len(trimmed) > 0 && trimmed[0] == '{' {
		return "json"
	}
	return "yaml"
}

func checkRedirect(request *http.Request, via []*http.Request) error {
	if len(via) >= maxRedirects {
		return errors.New("来源文件地址重定向次数过多")
	}
	if request.URL.Scheme != "https" {
		return errors.New("来源文件地址重定向到了非 https 地址")
	}
	return nil
}

// guardedDial 在连接前拒绝回环、内网、链路本地等地址，并直接连已判定的 IP，
// 避免两次解析之间被换答案（DNS rebinding）。
func guardedDial(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	addresses, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	dialer := net.Dialer{Timeout: 10 * time.Second}
	for _, candidate := range addresses {
		if !publicIP(candidate.IP) {
			continue
		}
		connection, err := dialer.DialContext(ctx, network, net.JoinHostPort(candidate.IP.String(), port))
		if err == nil {
			return connection, nil
		}
	}
	return nil, fmt.Errorf("来源文件主机 %s 没有可用的公网地址", host)
}

func publicIP(ip net.IP) bool {
	return ip != nil && ip.IsGlobalUnicast() && !ip.IsLoopback() && !ip.IsPrivate() &&
		!ip.IsLinkLocalUnicast() && !ip.IsLinkLocalMulticast() && !ip.IsUnspecified() && !ip.IsMulticast()
}
