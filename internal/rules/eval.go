package rules

import (
	"fmt"
	"strconv"
	"strings"
)

// State 是一次源调用的健康结论（CQ3：把「零结果」和「规则坏了」分开）。
type State string

const (
	StateOK       State = "ok"       // 有结果
	StateZero     State = "zero"     // 上游正常返回，确实没有匹配条目
	StateDead     State = "dead"     // 上游有条目但规则一条都解不出 —— 规则失效
	StateFailed   State = "failed"   // 网络 / HTTP / 解码失败
	StateDisabled State = "disabled" // 用户禁用
)

// Outcome 是一次源调用的完整结果，Items 之外的字段给界面展示「源异常」用。
type Outcome struct {
	Source   string `json:"source"`
	State    State  `json:"state"`
	Items    []Item `json:"-"`
	Count    int    `json:"count"`
	RawCount int    `json:"rawCount"`
	Dropped  int    `json:"dropped"`
	// FieldGaps 列出在全部条目上都为空的可选字段（size/date/fansub），提示规则可能部分失效。
	FieldGaps []string `json:"fieldGaps,omitempty"`
	// Reason 是用户可读的中文说明；Detail 是给日志/开发者的技术细节。
	Reason string `json:"reason,omitempty"`
	Detail string `json:"detail,omitempty"`
	// LatencyMs 是该源本次请求耗时（毫秒），纯 Evaluate 时为 0。
	LatencyMs int64 `json:"latencyMs"`
}

// itemSource 抽象一个条目的取值方式（XML 节点或 JSON 值）。
type itemSource interface {
	lookup(path string) (any, bool)
}

type xmlItem struct {
	node *xmlNode
	ns   map[string]string
}

func (x xmlItem) lookup(path string) (any, bool) {
	segs, err := compileXMLPath(path, x.ns)
	if err != nil {
		return nil, false
	}
	v, ok := xmlValue(x.node, segs)
	return v, ok
}

type jsonItem struct{ v any }

func (j jsonItem) lookup(path string) (any, bool) {
	return jsonLookup(j.v, path)
}

// Evaluate 对一份响应体跑完整规则（纯函数，不联网）：解析 → 逐条映射 → 健康分类。
func Evaluate(rule *Rule, body []byte) Outcome {
	out := Outcome{Source: rule.ID}
	items, err := extractItems(rule, body)
	if err != nil {
		out.State = StateFailed
		out.Reason = "响应无法解析，源站格式可能已变更"
		out.Detail = err.Error()
		return out
	}
	out.RawCount = len(items)
	results := make([]Item, 0, len(items))
	dropTitle, dropMagnet := 0, 0
	for _, it := range items {
		item, reason := rule.mapItem(it)
		switch reason {
		case "":
			results = append(results, item)
		case "title":
			dropTitle++
		case "magnet":
			dropMagnet++
		}
	}
	out.Items = results
	out.Count = len(results)
	out.Dropped = dropTitle + dropMagnet
	switch {
	case out.RawCount == 0:
		out.State = StateZero
	case out.Count == 0:
		out.State = StateDead
		out.Reason = "源站返回了条目但规则一条都解不出，规则可能已失效"
		out.Detail = fmt.Sprintf("%d 条原始条目：%d 条标题为空，%d 条 magnet 无效", out.RawCount, dropTitle, dropMagnet)
	default:
		out.State = StateOK
		out.FieldGaps = fieldGaps(results)
	}
	return out
}

// extractItems 按格式解析响应并选出条目集合。
func extractItems(rule *Rule, body []byte) ([]itemSource, error) {
	switch rule.Format {
	case "xml":
		doc, err := parseXMLDoc(body)
		if err != nil {
			return nil, fmt.Errorf("XML 解析失败: %w", err)
		}
		segs, err := compileXMLPath(rule.Items, rule.Namespaces)
		if err != nil {
			return nil, err
		}
		nodes, err := selectItems(doc, segs)
		if err != nil {
			return nil, err
		}
		out := make([]itemSource, 0, len(nodes))
		for _, n := range nodes {
			out = append(out, xmlItem{node: n, ns: rule.Namespaces})
		}
		return out, nil
	case "json":
		root, err := parseJSONDoc(body)
		if err != nil {
			return nil, fmt.Errorf("JSON 解析失败: %w", err)
		}
		arr, err := jsonItems(root, rule.Items)
		if err != nil {
			return nil, err
		}
		out := make([]itemSource, 0, len(arr))
		for _, v := range arr {
			out = append(out, jsonItem{v: v})
		}
		return out, nil
	}
	return nil, fmt.Errorf("未知格式 %q", rule.Format)
}

// mapItem 把一个条目按字段表达式映射成 Item；返回丢弃原因（""=保留）。
// 不变量与旧适配器一致：标题为空或 magnet 不以 magnet: 开头的条目丢弃。
func (r *Rule) mapItem(src itemSource) (Item, string) {
	computed := map[string]string{}
	specs := r.fieldSpecs()
	for _, name := range fieldOrder {
		spec := specs[name]
		if spec == nil {
			continue
		}
		computed[name] = evalField(spec, src, computed)
	}
	if computed["title"] == "" {
		return Item{}, "title"
	}
	if !strings.HasPrefix(computed["magnet"], magnetScheme) {
		return Item{}, "magnet"
	}
	item := Item{
		Title:    computed["title"],
		Magnet:   computed["magnet"],
		Size:     computed["size"],
		Fansub:   stringPtr(computed["fansub"]),
		Date:     stringPtr(computed["date"]),
		Source:   r.ID,
		Provider: stringPtr(computed["provider"]),
		Infohash: computed["infohash"],
	}
	if specs["seeders"] != nil {
		item.Seeders = evalSeeders(specs["seeders"], src)
	}
	return item, ""
}

// evalField 求一个字段的字符串值：any 取首个非空候选；path 取值后施加 transforms。
func evalField(spec *FieldSpec, src itemSource, computed map[string]string) string {
	if len(spec.Any) > 0 {
		for i := range spec.Any {
			if v := evalField(&spec.Any[i], src, computed); v != "" {
				return applyTransforms(v, spec.Transforms, computed)
			}
		}
		return ""
	}
	var v string
	if strings.HasPrefix(spec.Path, "$") {
		v = computed[spec.Path[1:]]
	} else if raw, ok := src.lookup(spec.Path); ok {
		v, _ = scalarString(raw)
	}
	return applyTransforms(v, spec.Transforms, computed)
}

// evalSeeders 求做种数：缺席 / null / 非数字 → nil（未知），数字 → 已知值（含 0）。
func evalSeeders(spec *FieldSpec, src itemSource) *int {
	raw, ok := src.lookup(spec.Path)
	if !ok || raw == nil {
		return nil
	}
	s, ok := scalarString(raw)
	if !ok || strings.TrimSpace(s) == "" {
		return nil
	}
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return nil
	}
	return &n
}

func scalarString(raw any) (string, bool) {
	if s, ok := raw.(string); ok {
		return s, true
	}
	return jsonScalar(raw)
}

// fieldGaps 找出在全部条目上都为空的可选字段。
func fieldGaps(items []Item) []string {
	if len(items) == 0 {
		return nil
	}
	hasSize, hasDate, hasFansub := false, false, false
	for _, it := range items {
		hasSize = hasSize || it.Size != ""
		hasDate = hasDate || it.Date != nil
		hasFansub = hasFansub || it.Fansub != nil
	}
	var gaps []string
	if !hasSize {
		gaps = append(gaps, "size")
	}
	if !hasDate {
		gaps = append(gaps, "date")
	}
	if !hasFansub {
		gaps = append(gaps, "fansub")
	}
	return gaps
}
