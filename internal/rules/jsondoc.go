package rules

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// parseJSONDoc 解析 JSON；数字保留原文（json.Number），既不丢精度也能拿到
// 与旧适配器 json.RawMessage 等价的字面量。
func parseJSONDoc(data []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	return v, nil
}

// jsonLookup 按点路径取值："$" 是根；"a.b.c" 逐层进对象。未命中返回 (nil,false)。
func jsonLookup(root any, path string) (any, bool) {
	if path == "$" || path == "" {
		return root, root != nil
	}
	cur := root
	for _, seg := range strings.Split(path, ".") {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		cur, ok = m[seg]
		if !ok {
			return nil, false
		}
	}
	return cur, true
}

// jsonItems 取条目数组；不是数组视为响应形状不对（解码类失败）。
func jsonItems(root any, path string) ([]any, error) {
	v, ok := jsonLookup(root, path)
	if !ok {
		return nil, fmt.Errorf("items 路径 %q 不存在", path)
	}
	arr, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("items 路径 %q 不是数组", path)
	}
	return arr, nil
}

// jsonScalar 把 JSON 标量转成字符串：字符串原样、数字取字面量、布尔 true/false、null → ""。
// 对象/数组不是标量，返回 ("", false)。
func jsonScalar(v any) (string, bool) {
	switch x := v.(type) {
	case nil:
		return "", true
	case string:
		return x, true
	case json.Number:
		return x.String(), true
	case bool:
		if x {
			return "true", true
		}
		return "false", true
	default:
		return "", false
	}
}
