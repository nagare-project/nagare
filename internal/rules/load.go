package rules

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Parse 解析并校验一条 YAML 规则。
func Parse(data []byte) (*Rule, error) {
	var r Rule
	dec := yaml.NewDecoder(strings.NewReader(string(data)))
	dec.KnownFields(true) // 拼错的键直接报错，不静默忽略
	if err := dec.Decode(&r); err != nil {
		return nil, fmt.Errorf("解析规则 YAML: %w", err)
	}
	if err := r.Validate(); err != nil {
		return nil, err
	}
	return &r, nil
}

// LoadFile 读取一个规则文件。
func LoadFile(path string) (*Rule, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取规则 %s: %w", path, err)
	}
	r, err := Parse(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	return r, nil
}

// LoadDir 读取目录下全部 *.yaml / *.yml 规则，按文件名排序（决定合并顺序）。
// 单个文件坏了记进 errs 但不影响其他规则加载 —— 一条规则坏不该让整个搜索瘫痪。
func LoadDir(dir string) (rules []*Rule, errs []error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, []error{fmt.Errorf("读取规则目录 %s: %w", dir, err)}
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(e.Name()))
		if ext == ".yaml" || ext == ".yml" {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	seen := map[string]string{}
	for _, name := range names {
		r, err := LoadFile(filepath.Join(dir, name))
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if prev, dup := seen[r.ID]; dup {
			errs = append(errs, fmt.Errorf("%s: id %q 与 %s 重复", name, r.ID, prev))
			continue
		}
		seen[r.ID] = name
		rules = append(rules, r)
	}
	return rules, errs
}
