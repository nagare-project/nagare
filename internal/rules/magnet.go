package rules

import (
	"encoding/base32"
	"encoding/hex"
	"net/url"
	"strings"
)

// infohash 归一化与 magnet 拼装 —— 移植自 animego infohash.go / nyaa.go。

const (
	btihPrefix  = "urn:btih:"
	hexLenV1    = 40
	hexLenV2    = 64
	base32LenV1 = 32
)

// ParseInfohash 从 magnet 里取出归一化的 infohash（小写 hex；base32 v1 解成 hex；
// v2 保留 64 位不截断），拿不到返回空串。空串表示「不可去重」。
func ParseInfohash(magnet string) string {
	raw := extractBtih(magnet)
	if raw == "" {
		return ""
	}
	return normaliseHash(raw)
}

// extractBtih 手写解析而不用 net/url：magnet 的参数体在实践中不做百分号编码，
// url.Parse 对不透明体过于严格。参数分隔符接受 & 与 ;。
func extractBtih(magnet string) string {
	if !strings.HasPrefix(magnet, magnetScheme) {
		return ""
	}
	q := magnet[len(magnetScheme):]
	i := strings.IndexByte(q, '?')
	if i < 0 {
		return ""
	}
	q = strings.ReplaceAll(q[i+1:], ";", "&")
	for _, param := range strings.Split(q, "&") {
		eq := strings.IndexByte(param, '=')
		if eq < 0 || !strings.EqualFold(param[:eq], "xt") {
			continue
		}
		val := param[eq+1:]
		if len(val) >= len(btihPrefix) && strings.EqualFold(val[:len(btihPrefix)], btihPrefix) {
			return val[len(btihPrefix):]
		}
	}
	return ""
}

func normaliseHash(raw string) string {
	raw = strings.TrimSpace(raw)
	switch len(raw) {
	case hexLenV1, hexLenV2:
		lower := strings.ToLower(raw)
		if !isHex(lower) {
			return ""
		}
		return lower
	case base32LenV1:
		decoded, err := base32.StdEncoding.DecodeString(strings.ToUpper(raw))
		if err != nil {
			return ""
		}
		return hex.EncodeToString(decoded)
	default:
		return ""
	}
}

func isHex(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// buildMagnet 用 infohash + 标题 + tracker 列表拼 magnet：
// hash 空则返回空串（映射层随后按「非 magnet」丢弃条目）；
// 标题走 url.QueryEscape 进 dn=；tracker 逐个 QueryEscape 进 tr=。
// 与旧实现的差别仅在 tracker 由规则提供（旧版把两条 nyaa tracker 写死并预编码）。
func buildMagnet(hash, title string, trackers []string) string {
	hash = strings.TrimSpace(hash)
	// 只接受合法形状的 infohash（40/64 位 hex 或 32 位 base32），原样保留大小写。
	// 规则解出的垃圾值（截断的 hash、HTML 片段）在这里就丢弃，不会以 magnet 的
	// 外形流到下载器（M3）。
	if hash == "" || normaliseHash(hash) == "" {
		return ""
	}
	var b strings.Builder
	b.WriteString("magnet:?xt=urn:btih:")
	b.WriteString(hash)
	b.WriteString("&dn=")
	b.WriteString(url.QueryEscape(title))
	for _, tr := range trackers {
		b.WriteString("&tr=")
		b.WriteString(url.QueryEscape(tr))
	}
	return b.String()
}
