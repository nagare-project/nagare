package player

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
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
	// AnilistID 是用户在目录里点的那部作品；弹幕匹配必须对上它，否则宁可没有弹幕。
	AnilistID int
	// AltTitles 是目录里的其他标题（原名/英文名），主标题匹配不上时逐个再试。
	AltTitles []string
}

type remoteSource struct {
	item      library.Item
	url       string
	headers   map[string]string
	expiresAt int64
	anilistID int
	altTitles []string
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
		anilistID: options.AnilistID, altTitles: append([]string(nil), options.AltTitles...),
	}
}

func (s *remoteSource) MatchHints() (int, []string) {
	return s.anilistID, append([]string(nil), s.altTitles...)
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

// ErrNoFingerprint 表示这种媒体源天生没有文件指纹（不是计算失败）。
// ensureBinding 收到它会改走关键词匹配，而不是放弃弹幕。
var ErrNoFingerprint = errors.New("该媒体源不提供文件指纹")

// 在线媒体不为弹幕匹配额外下载前 16 MiB：起播不等指纹，弹幕靠标题 + 集号走
// animego 的关键词匹配（phase 2）。
func (s *remoteSource) Hash16M(context.Context) (string, error) {
	return "", ErrNoFingerprint
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
