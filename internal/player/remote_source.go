package player

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"strings"
	"time"

	errs "github.com/nagare-project/nagare/internal/errors"
	"github.com/nagare-project/nagare/internal/library"
)

// RemoteSourceOptions 是用户已选择的短期 HTTP/HLS 候选。Headers 只保存在
// 当前内存会话中，不进入 library.Item、Store 或日志。
type RemoteSourceOptions struct {
	CandidateID string
	SourceID    string
	URL         string
	Headers     map[string]string
	ExpiresAt   int64
	Title       string
	Episode     int
	SizeBytes   int64
}

type remoteSource struct {
	item      library.Item
	url       string
	headers   map[string]string
	expiresAt int64
}

func NewRemoteSource(options RemoteSourceOptions) MediaSource {
	title := strings.TrimSpace(options.Title)
	episode := options.Episode
	digest := sha256.Sum256([]byte(options.SourceID + "\x00" + options.CandidateID))
	item := library.Item{
		FileID:       "remote|" + hex.EncodeToString(digest[:]),
		FileName:     title,
		Size:         options.SizeBytes,
		Episode:      &episode,
		ParsedTitle:  &title,
		ParsedNumber: &episode,
	}
	return &remoteSource{
		item: item, url: options.URL, headers: cloneHeaders(options.Headers), expiresAt: options.ExpiresAt,
	}
}

func (s *remoteSource) Item() library.Item { return s.item }

func (s *remoteSource) MPVPath() string { return s.url }

func (s *remoteSource) HTTPHeaders() map[string]string { return cloneHeaders(s.headers) }

func (s *remoteSource) RedactMediaDiagnostics() bool { return true }

func (s *remoteSource) Probe(context.Context) error {
	parsed, err := url.Parse(s.url)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil {
		return errs.New(errs.CategoryInput, "player.remote.probe", "在线候选地址无效", "重新解析来源后再试")
	}
	if s.expiresAt > 0 && time.Now().UnixMilli() >= s.expiresAt {
		return errs.New(errs.CategoryNetwork, "player.remote.probe", "在线候选已经过期", "重新解析来源后再试")
	}
	return nil
}

// 在线媒体不为弹幕匹配额外下载前 16 MiB；失败只降级弹幕，播放已经先启动。
func (s *remoteSource) Hash16M(context.Context) (string, error) {
	return "", errs.New(errs.CategoryNetwork, "player.remote.hash", "在线播放不计算文件指纹", "弹幕匹配将跳过")
}

func cloneHeaders(headers map[string]string) map[string]string {
	if len(headers) == 0 {
		return nil
	}
	copy := make(map[string]string, len(headers))
	for name, value := range headers {
		copy[name] = value
	}
	return copy
}
