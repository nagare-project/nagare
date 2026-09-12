// tracker 补充。
//
// 本体不内置任何 tracker（与规则引擎「不内置任何源与 tracker」一致），
// 列表全部来自用户配置、默认为空。
package torrentstream

import (
	"log"
	"strings"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"
)

// DefaultTrackers 是内置的公共 tracker（2026-09-12 用户定，改写决议 M3-3）。
//
// 为什么内置：Anime Garden 一类的索引站给的磁力不带 tracker，纯靠 DHT 找元数据
// 实测 50–125 秒、有时找不到；带公共 tracker 2.6 秒。tracker 只回答「谁在分享
// 这个 infohash」，不存内容、不传数据 —— 与 anacrolix 内置的 DHT 引导节点同属
// 「怎么找人」的基础设施，不是「找什么」的内容源，不在红线 1 的范围内。
//
// 挑选标准（2026-09-12 实测，逐条 announce 5 秒内应答）：通用高可用的
// UDP 三件套 + nyaa 自家 + 两个二次元向 + 两个国内可达的 HTTPS。
// animeTrackerList（DeSireFire，2024-01 后停更）26 条「best」里只剩 5 条活着、
// 还混着 M-Team 这种私有站，不能整表照抄；这里只取实测活的。
// 用户可在设置里关掉整组，或在此之外追加自己的。
var DefaultTrackers = []string{
	"udp://tracker.opentrackr.org:1337/announce",
	"udp://open.stealth.si:80/announce",
	"udp://tracker.torrent.eu.org:451/announce",
	"udp://open.demonii.com:1337/announce",
	"http://nyaa.tracker.wf:7777/announce",
	"udp://leet-tracker.moe:1337/announce",
	"https://tracker.nekomi.cn:443/announce",
	"https://tracker.zhuqiy.com:443/announce",
}

// EffectiveTrackers 合成引擎实际使用的列表：内置组（未关闭时）在前、用户追加在后，
// 去重与合法性检查沿用 NormalizeTrackers。
func EffectiveTrackers(useDefaults bool, extra []string) []string {
	merged := make([]string, 0, len(DefaultTrackers)+len(extra))
	if useDefaults {
		merged = append(merged, DefaultTrackers...)
	}
	merged = append(merged, extra...)
	kept, _ := NormalizeTrackers(merged)
	return kept
}

// maxTrackers 是接受的 tracker 条数上限。
// 网上流传的「公开 tracker 大列表」动辄几千行，整份粘进来只会让每次
// announce 都变成一轮无意义的广播。
const maxTrackers = 100

// supportedTrackerSchemes 是接受的协议。其余（尤其是 file:// 之类）一律丢弃。
var supportedTrackerSchemes = []string{"http://", "https://", "udp://", "ws://", "wss://"}

// NormalizeTrackers 清洗用户填的 tracker 列表：去空白、丢空行、去重、
// 只保留支持的协议，并限制条数。
//
// 保序，同一个地址只留第一次出现。kept 永远非 nil（空时是空切片），
// rejected 可能为 nil。界面据此明确告诉用户「这几行没被接受」，
// 而不是默默吞掉。
func NormalizeTrackers(in []string) (kept, rejected []string) {
	kept = []string{}
	seen := make(map[string]struct{}, len(in))
	for _, raw := range in {
		addr := strings.TrimSpace(raw)
		if addr == "" {
			continue
		}
		if _, dup := seen[addr]; dup {
			continue
		}
		seen[addr] = struct{}{}
		switch {
		case !hasSupportedScheme(addr):
			rejected = append(rejected, addr)
		case len(kept) >= maxTrackers:
			rejected = append(rejected, addr)
		default:
			kept = append(kept, addr)
		}
	}
	return kept, rejected
}

func hasSupportedScheme(addr string) bool {
	lower := strings.ToLower(addr)
	for _, scheme := range supportedTrackerSchemes {
		if strings.HasPrefix(lower, scheme) && len(lower) > len(scheme) {
			return true
		}
	}
	return false
}

// isPrivate 判断种子是否声明了 BEP27 的 private 标记。
func isPrivate(info *metainfo.Info) bool {
	return info != nil && info.Private != nil && *info.Private
}

// trackersFor 决定给这个种子补哪些 tracker。
//
// 私有种子（.torrent 文件路径，info 一开始就在手上）返回空：私有站的 announce
// 地址里带 passkey，把同一个 infohash 报到公共 tracker 上等于把用户的下载行为
// 暴露给站外，实践中直接导致封号。磁力路径传 nil info：那时还不知道是否私有，
// 但磁力本就要先经 DHT 公开找 peer，补 tracker 不会多暴露什么（见 session.ensureTorrent）。
func trackersFor(info *metainfo.Info, configured []string) []string {
	if len(configured) == 0 {
		return nil
	}
	if isPrivate(info) {
		log.Printf("torrent: 私有种子跳过补充 tracker（避免 passkey 外泄）")
		return nil
	}
	kept, _ := NormalizeTrackers(configured)
	if len(kept) == 0 {
		return nil
	}
	return kept
}

// applyTrackers 把该补的 tracker 挂到种子上，返回实际补了几条。
func applyTrackers(t *torrent.Torrent, info *metainfo.Info, configured []string) int {
	if t == nil {
		return 0
	}
	kept := trackersFor(info, configured)
	if len(kept) == 0 {
		return 0
	}
	// 每条单独成一层：同层内 anacrolix 只轮询到第一个可用的，
	// 分层才能让用户配的每个 tracker 都真的被 announce。
	tiers := make([][]string, 0, len(kept))
	for _, addr := range kept {
		tiers = append(tiers, []string{addr})
	}
	t.AddTrackers(tiers)
	return len(kept)
}
