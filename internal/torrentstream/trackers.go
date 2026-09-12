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
