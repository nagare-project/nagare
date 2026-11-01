package api

import (
	"sync/atomic"

	"github.com/nagare-project/nagare/internal/artcache"
	"github.com/nagare-project/nagare/internal/store"
)

// 海报色：界面显示封面的地方（目录卡片、作品详情、放送表、收藏、媒体库分组、继续观看）
// 都带一个 posterTones 字段，值是服务端按封面色相算好的最终 sRGB 色阶（见 artcache.PosterTones），
// null 表示界面用中性色 —— 封面还没缓存、颜色还在后台算、封面是灰的或格式解不了，都是 null。
//
// 颜色只从【已经缓存在本机】的封面上取：拼响应不会因此多发任何网络请求，也不会等解码。

// PosterToneSource 按封面的原始地址给出海报色阶；internal/artcache 的 *Cache 实现它。
type PosterToneSource interface {
	PosterTones(source string) *artcache.PosterTones
}

// 真实的封面缓存必须满足 PosterToneSource：接口签名一漂移，所有响应都会悄悄变成 null —— 让编译器来守。
var _ PosterToneSource = (*artcache.Cache)(nil)

// toneSourceBox 让 atomic.Value 能存一个可能为 nil 的接口值。
type toneSourceBox struct{ src PosterToneSource }

// posterToneSlot 是可以在运行中注入的取色来源：封面缓存比 Handler 晚建（要等能力段），
// 注入之前的响应一律是 null。
type posterToneSlot struct{ v atomic.Value }

func (p *posterToneSlot) set(src PosterToneSource) { p.v.Store(toneSourceBox{src}) }

func (p *posterToneSlot) get() PosterToneSource {
	box, _ := p.v.Load().(toneSourceBox)
	return box.src
}

// tonesFor 查一张封面的色阶；没有来源或没有地址时为 nil。
func tonesFor(src PosterToneSource, source string) *artcache.PosterTones {
	if src == nil || source == "" {
		return nil
	}
	return src.PosterTones(source)
}

// SetPosterTones 接上目录（发现、季度、放送、详情、搜索、收藏）的海报取色。启动时调用一次。
func (h *Handler) SetPosterTones(src PosterToneSource) { h.catalog.tones.set(src) }

// posterTones 是目录投影用的：只给有封面的条目查。
func (s *DiscoverService) posterTones(source string) *artcache.PosterTones {
	return tonesFor(s.tones.get(), source)
}

// SetPosterTones 接上媒体库视图（分组封面、继续观看）的海报取色。启动时调用一次。
func (s *LibraryService) SetPosterTones(src PosterToneSource) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tones = src
}

// associatedCoverSource 是 associatedCover 对应的封面原始地址（取色用），两者的取舍必须一致：
// 认定的作品有封面就用它，没有才用自动匹配来的；标为「不是目录里的作品」就没有封面。
func associatedCoverSource(a store.Association, matched string) string {
	if a.Mode != store.AssociationManual {
		return ""
	}
	if a.CoverURL != "" {
		return a.CoverURL
	}
	return matched
}
