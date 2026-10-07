package api

import (
	"container/list"
	"crypto/sha256"
	"fmt"
	"net/url"
	"strings"
	"sync"
)

// RemoteArtSource 只按服务端登记的键反查地址；请求方不能提供 URL。
type RemoteArtSource interface{ Source(string) (string, bool) }
type NoRemoteArt struct{}

func (NoRemoteArt) Source(string) (string, bool) { return "", false }

type remoteImage struct{ key, source string }

// RemoteArt 维护有界 LRU。缓存淘汰只导致图片缺失，不扩张成任意 URL 代理。
type RemoteArt struct {
	mu                   sync.Mutex
	capacity             int
	entries              map[string]*list.Element
	order                *list.List
	prefix, upstreamHost string
}

func NewRemoteArt(baseURL string, capacity int) *RemoteArt {
	if capacity < 1 {
		capacity = 4096
	}
	u, _ := url.Parse(baseURL)
	host := ""
	if u != nil && u.Scheme == "https" && u.User == nil {
		host = strings.ToLower(u.Host)
	}
	return &RemoteArt{capacity: capacity, entries: map[string]*list.Element{}, order: list.New(), upstreamHost: host}
}
func (a *RemoteArt) SetPrefix(prefix string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.prefix = strings.TrimRight(prefix, "/") + "/remote/"
}

// Allowed 判断一个图片地址能不能经 /art 转发：只接 https、不带 userinfo 与 fragment，
// 且是已核实的海报图床、AniZip 使用的 TVDB 截图图床或配置的元数据站点。
// 落盘的封面地址（作品关联）也走这一道，上游数据被污染时不会变成长期的外联信标。
func (a *RemoteArt) Allowed(source string) bool {
	u, err := url.Parse(source)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Fragment != "" || u.Host == "" {
		return false
	}
	host := strings.ToLower(u.Host)
	return host == "s4.anilist.co" || host == "artworks.thetvdb.com" || host == a.upstreamHost
}

func (a *RemoteArt) Register(source string) string {
	if !a.Allowed(source) {
		return ""
	}
	key := fmt.Sprintf("%x", sha256.Sum256([]byte(source)))
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.prefix == "" {
		return ""
	}
	if e, ok := a.entries[key]; ok {
		a.order.MoveToFront(e)
	} else {
		a.entries[key] = a.order.PushFront(remoteImage{key, source})
	}
	if a.order.Len() > a.capacity {
		old := a.order.Back()
		delete(a.entries, old.Value.(remoteImage).key)
		a.order.Remove(old)
	}
	return a.prefix + key
}
func (a *RemoteArt) Source(key string) (string, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	e, ok := a.entries[key]
	if !ok {
		return "", false
	}
	a.order.MoveToFront(e)
	return e.Value.(remoteImage).source, true
}
