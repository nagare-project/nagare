package torrentstream

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/anacrolix/torrent/metainfo"

	errs "github.com/nagare-project/nagare/internal/errors"
)

const (
	maximumTorrentBytes     = 4 << 20
	torrentFetchTimeout     = 20 * time.Second
	maximumTorrentRedirects = 3
)

func fetchTorrentMetaInfo(ctx context.Context, rawURL string) (*metainfo.MetaInfo, error) {
	if err := validateTorrentURL(rawURL); err != nil {
		return nil, errs.Wrap(errs.CategoryInput, "torrentstream.torrent-url",
			"种子文件地址无效", "换一条资源后重试", err)
	}
	client := &http.Client{
		Timeout:   torrentFetchTimeout,
		Transport: &http.Transport{Proxy: nil, DialContext: guardedTorrentDial},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) > maximumTorrentRedirects {
				return fmt.Errorf("种子文件重定向超过 %d 跳", maximumTorrentRedirects)
			}
			return validateTorrentURL(req.URL.String())
		},
	}
	return fetchTorrentMetaInfoWithClient(ctx, rawURL, client)
}

func fetchTorrentMetaInfoWithClient(ctx context.Context, rawURL string, client *http.Client) (*metainfo.MetaInfo, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, errs.Wrap(errs.CategoryInput, "torrentstream.torrent-url",
			"种子文件地址无效", "换一条资源后重试", err)
	}
	request.Header.Set("Accept", "application/x-bittorrent, application/octet-stream;q=0.9")
	response, err := client.Do(request)
	if err != nil {
		return nil, errs.Wrap(errs.CategoryNetwork, "torrentstream.torrent-fetch",
			"无法下载种子文件", "换一条资源或稍后重试", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, errs.New(errs.CategoryNetwork, "torrentstream.torrent-fetch",
			fmt.Sprintf("种子文件下载失败（HTTP %d）", response.StatusCode), "换一条资源或稍后重试")
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maximumTorrentBytes+1))
	if err != nil {
		return nil, errs.Wrap(errs.CategoryNetwork, "torrentstream.torrent-fetch",
			"种子文件读取失败", "换一条资源或稍后重试", err)
	}
	if len(data) == 0 || len(data) > maximumTorrentBytes {
		return nil, errs.New(errs.CategoryInput, "torrentstream.torrent-fetch",
			"种子文件为空或超过 4 MiB 上限", "换一条资源后重试")
	}
	metadata, err := metainfo.Load(bytes.NewReader(data))
	if err != nil {
		return nil, errs.Wrap(errs.CategoryInput, "torrentstream.metainfo",
			"种子文件内容无效", "换一条资源后重试", err)
	}
	return metadata, nil
}

func validateTorrentURL(rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return err
	}
	if (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil {
		return errors.New("torrent URL must be an HTTP(S) URL without credentials")
	}
	return nil
}

func guardedTorrentDial(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	addresses, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	var dialer net.Dialer
	for _, address := range addresses {
		if unsafeTorrentAddress(address.IP) {
			continue
		}
		connection, dialErr := dialer.DialContext(ctx, network, net.JoinHostPort(address.IP.String(), port))
		if dialErr == nil {
			return connection, nil
		}
	}
	return nil, fmt.Errorf("种子文件主机 %s 没有可用的公网地址", host)
}

func unsafeTorrentAddress(ip net.IP) bool {
	if ip == nil || !ip.IsGlobalUnicast() || ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast() {
		return true
	}
	value := ip.To4()
	return value != nil && value[0] == 100 && value[1]&0xc0 == 0x40
}
