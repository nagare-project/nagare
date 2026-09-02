package rules

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strings"
)

// xmlNode 是通用 XML 树节点；规则用路径在树上取值，不需要每个源一套 struct。
type xmlNode struct {
	space, local string
	attrs        map[string]string // 属性按本地名索引（RSS 属性无命名空间）
	children     []*xmlNode
	text         strings.Builder
}

// parseXMLDoc 把整份文档解析成树，返回一个虚拟的文档节点（其唯一子节点是根元素）。
// 实体（&amp; 等）由 encoding/xml 解码，与旧适配器行为一致。
func parseXMLDoc(data []byte) (*xmlNode, error) {
	dec := xml.NewDecoder(bytes.NewReader(data))
	doc := &xmlNode{}
	stack := []*xmlNode{doc}
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			n := &xmlNode{space: t.Name.Space, local: t.Name.Local, attrs: map[string]string{}}
			for _, a := range t.Attr {
				n.attrs[a.Name.Local] = a.Value
			}
			parent := stack[len(stack)-1]
			parent.children = append(parent.children, n)
			stack = append(stack, n)
		case xml.EndElement:
			if len(stack) > 1 {
				stack = stack[:len(stack)-1]
			}
		case xml.CharData:
			// 字符数据归属最内层元素；与 struct 解码一致，嵌套元素的文本也会并入。
			for i := 1; i < len(stack); i++ {
				stack[i].text.Write(t)
			}
		}
	}
	if len(doc.children) == 0 {
		return nil, fmt.Errorf("文档没有根元素")
	}
	return doc, nil
}

// xmlSegment 是路径的一段：元素本地名 + 命名空间 URI（空串 = 无命名空间）+ 可选属性名。
type xmlSegment struct {
	local, space, attr string
}

// compileXMLPath 把 "a/ns:b@attr" 解析成段序列；前缀通过规则 namespaces 解析成 URI。
func compileXMLPath(path string, ns map[string]string) ([]xmlSegment, error) {
	parts := strings.Split(path, "/")
	segs := make([]xmlSegment, 0, len(parts))
	for i, p := range parts {
		var seg xmlSegment
		name := p
		if at := strings.IndexByte(p, '@'); at >= 0 {
			if i != len(parts)-1 {
				return nil, fmt.Errorf("路径 %q：@属性只能出现在最后一段", path)
			}
			seg.attr = p[at+1:]
			name = p[:at]
		}
		if colon := strings.IndexByte(name, ':'); colon > 0 {
			uri, ok := ns[name[:colon]]
			if !ok {
				return nil, fmt.Errorf("路径 %q 用到未声明的命名空间前缀 %q", path, name[:colon])
			}
			seg.space, seg.local = uri, name[colon+1:]
		} else {
			seg.local = name
		}
		if seg.local == "" {
			// 属性必须写成 elem@attr 贴在元素名后；单独一段的 "@attr" / "elem/@attr"
			// 会被误读成当前节点自己的属性，错得很安静，所以在加载期直接拒绝。
			if seg.attr != "" {
				return nil, fmt.Errorf("路径 %q：@属性必须紧跟元素名（写成 elem@attr），不能单独成段", path)
			}
			return nil, fmt.Errorf("路径 %q 含空段", path)
		}
		segs = append(segs, seg)
	}
	return segs, nil
}

// selectXML 从 from 出发按段选出全部匹配节点（每段可多匹配）。
func selectXML(from *xmlNode, segs []xmlSegment) []*xmlNode {
	current := []*xmlNode{from}
	for _, seg := range segs {
		var next []*xmlNode
		for _, n := range current {
			for _, c := range n.children {
				if c.local == seg.local && c.space == seg.space {
					next = append(next, c)
				}
			}
		}
		current = next
		if len(current) == 0 {
			return nil
		}
	}
	return current
}

// xmlValue 取路径的第一个匹配：属性值或元素文本；未命中返回 ("", false)。
func xmlValue(from *xmlNode, segs []xmlSegment) (string, bool) {
	if len(segs) == 0 {
		return "", false
	}
	last := segs[len(segs)-1]
	nodes := selectXML(from, segs)
	if len(nodes) == 0 {
		return "", false
	}
	target := nodes[0]
	if last.attr != "" {
		v, ok := target.attrs[last.attr]
		return v, ok
	}
	return target.text.String(), true
}

// selectItems 解析条目集合路径。父链（最后一段之前）在文档里一个都对不上视为
// 「响应结构变了」，返回错误 —— 不能和「父链在、只是没有条目」（合法的零结果）混为一谈，
// 否则站点改版就会被当成"没有资源"（决议 CQ3）。
func selectItems(doc *xmlNode, segs []xmlSegment) ([]*xmlNode, error) {
	if len(segs) == 0 {
		return nil, fmt.Errorf("items 路径为空")
	}
	if segs[len(segs)-1].attr != "" {
		return nil, fmt.Errorf("items 路径不能指向属性")
	}
	parents := []*xmlNode{doc}
	if len(segs) > 1 {
		parents = selectXML(doc, segs[:len(segs)-1])
		if len(parents) == 0 {
			return nil, fmt.Errorf("items 路径的父级在响应里不存在（响应结构可能已变更）")
		}
	}
	var out []*xmlNode
	last := segs[len(segs)-1]
	for _, p := range parents {
		out = append(out, selectXML(p, []xmlSegment{last})...)
	}
	return out, nil
}
