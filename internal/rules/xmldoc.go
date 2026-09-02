package rules

import (
	"bytes"
	"encoding/xml"
	"fmt"
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
		if err != nil {
			if err.Error() == "EOF" {
				break
			}
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
		if seg.local == "" && seg.attr == "" {
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
		if seg.local == "" { // 仅 "@attr" 这种引用自身属性的写法
			break
		}
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
	target := from
	if last.local != "" {
		nodes := selectXML(from, segs)
		if len(nodes) == 0 {
			return "", false
		}
		target = nodes[0]
	}
	if last.attr != "" {
		v, ok := target.attrs[last.attr]
		return v, ok
	}
	return target.text.String(), true
}
