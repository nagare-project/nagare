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
func (a *RemoteArt) Register(source string) string {
	u, err := url.Parse(source)
	// AniList 图床与配置的 animego HTTPS 站点是目前已核实的两类来源；重定向仍由 artcache 逐跳检查。
	if err != nil || u.Scheme != "https" || u.User != nil || u.Fragment != "" || u.Host == "" {
		return ""
	}
	host := strings.ToLower(u.Host)
	if host != "s4.anilist.co" && host != a.upstreamHost {
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
