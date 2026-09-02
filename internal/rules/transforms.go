package rules

import (
	"strconv"
	"strings"
	"time"
)

// applyTransforms 依次施加转换。computed 是已算出的字段（供 $name 引用）。
func applyTransforms(v string, steps []Transform, computed map[string]string) string {
	for i := range steps {
		v = steps[i].apply(v, computed)
	}
	return v
}

func (t *Transform) apply(v string, computed map[string]string) string {
	switch t.Name {
	case "trim":
		return strings.TrimSpace(v)
	case "lower":
		return strings.ToLower(v)
	case "upper":
		return strings.ToUpper(v)
	case "format_bytes":
		return formatBytes(v)
	case "format_kb":
		return formatKb(v)
	case "parse_fansub":
		return parseFansub(v)
	case "regex":
		if t.re == nil {
			return ""
		}
		return t.re.FindString(v)
	case "unix_rfc3339":
		// 非正的时间戳（缺席/0）→ 空串 → 映射层转 null。
		ts, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		if err != nil || ts <= 0 {
			return ""
		}
		return time.Unix(ts, 0).UTC().Format(time.RFC3339)
	case "magnet":
		title := ""
		if dn, _ := t.Args["dn"].(string); dn != "" {
			title = resolveRef(dn, computed)
		}
		return buildMagnet(v, title, t.trackers())
	}
	return v
}

// trackers 读 magnet 转换的 tracker 列表参数。
func (t *Transform) trackers() []string {
	raw, _ := t.Args["trackers"].([]any)
	out := make([]string, 0, len(raw))
	for _, x := range raw {
		if s, ok := x.(string); ok && s != "" {
			out = append(out, s)
		}
	}
	return out
}

// resolveRef 把 "$title" 这类引用解析成已算出的字段值；非引用原样返回。
func resolveRef(s string, computed map[string]string) string {
	if strings.HasPrefix(s, "$") {
		return computed[s[1:]]
	}
	return s
}
