package rules

import (
	"fmt"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// SchemaVersion 是当前规则文件格式版本；不认识的版本拒绝加载而不是猜。
const SchemaVersion = 1

// defaultTimeoutSeconds 是单源请求默认超时（与旧适配器的 8s 一致）。
const defaultTimeoutSeconds = 8

// maxTimeoutSeconds 是规则可声明的超时上限（HTTP 客户端另有 20s 硬顶）。
const maxTimeoutSeconds = 60

// Rule 是一条声明式源规则（一个 YAML 文件）。
type Rule struct {
	Schema       int               `yaml:"schema"`
	ID           string            `yaml:"id"`
	Name         string            `yaml:"name"`
	Homepage     string            `yaml:"homepage"`
	Request      Request           `yaml:"request"`
	Format       string            `yaml:"format"` // xml | json
	Namespaces   map[string]string `yaml:"namespaces"`
	Items        string            `yaml:"items"`
	Fields       Fields            `yaml:"fields"`
	Capabilities Capabilities      `yaml:"capabilities"`
	SelfTest     SelfTest          `yaml:"selftest"`
}

// Request 描述请求怎么发。URL 里 {{query}} 会被 url.QueryEscape 后的关键词替换。
type Request struct {
	URL            string            `yaml:"url"`
	Headers        map[string]string `yaml:"headers"`
	TimeoutSeconds int               `yaml:"timeout_seconds"`
}

// Fields 是输出字段到表达式的映射；Title 与 Magnet 必填，其余可省。
type Fields struct {
	Title    *FieldSpec `yaml:"title"`
	Magnet   *FieldSpec `yaml:"magnet"`
	Size     *FieldSpec `yaml:"size"`
	Date     *FieldSpec `yaml:"date"`
	Fansub   *FieldSpec `yaml:"fansub"`
	Provider *FieldSpec `yaml:"provider"`
	Seeders  *FieldSpec `yaml:"seeders"`
	Infohash *FieldSpec `yaml:"infohash"`
}

// Capabilities 影响聚合：Seeders 声明该源给做种数；Priority 高者在去重/排序中占优。
type Capabilities struct {
	Seeders  bool `yaml:"seeders" json:"seeders"`
	Priority int  `yaml:"priority" json:"priority"`
}

// SelfTest 是规则自检：用一个已知有结果的关键词探测规则是否失效（CQ3）。
type SelfTest struct {
	Query string `yaml:"query"`
}

// FieldSpec 是一个字段的取值表达式：
//   - 标量简写 `title: title` 等价于 `{path: title}`
//   - `path`：从当前条目取一个值（XML 路径 / JSON 点路径，或 `$name` 引用已算出的字段）
//   - `any`：候选列表，取第一个非空结果（每个候选自带 path 与 transforms）
//   - `transforms`：对取到的值依次施加的转换
type FieldSpec struct {
	Path       string      `yaml:"path"`
	Any        []FieldSpec `yaml:"any"`
	Transforms []Transform `yaml:"transforms"`
}

// UnmarshalYAML 接受标量简写。
func (f *FieldSpec) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind == yaml.ScalarNode {
		f.Path = n.Value
		return nil
	}
	type plain FieldSpec
	var p plain
	if err := n.Decode(&p); err != nil {
		return err
	}
	*f = FieldSpec(p)
	return nil
}

// Transform 是一个转换步骤：标量写法 `trim`，或单键映射 `{regex: "..."}` /
// `{magnet: {dn: $title, trackers: [...]}}`。
type Transform struct {
	Name string
	Arg  string         // 标量参数（如 regex 的 pattern）
	Args map[string]any // 映射参数

	re *regexp.Regexp // 校验阶段预编译
}

// UnmarshalYAML 接受两种写法。
func (t *Transform) UnmarshalYAML(n *yaml.Node) error {
	switch n.Kind {
	case yaml.ScalarNode:
		t.Name = n.Value
		return nil
	case yaml.MappingNode:
		if len(n.Content) != 2 {
			return fmt.Errorf("转换步骤必须是单键映射，得到 %d 个键", len(n.Content)/2)
		}
		t.Name = n.Content[0].Value
		val := n.Content[1]
		if val.Kind == yaml.ScalarNode {
			t.Arg = val.Value
			return nil
		}
		return val.Decode(&t.Args)
	default:
		return fmt.Errorf("转换步骤必须是字符串或单键映射")
	}
}

var idRE = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,31}$`)

// knownTransforms 是转换词汇表；不认识的名字在加载时就拒绝。
var knownTransforms = map[string]struct{}{
	"trim": {}, "lower": {}, "upper": {},
	"format_bytes": {}, "format_kb": {},
	"parse_fansub": {}, "regex": {}, "unix_rfc3339": {}, "magnet": {},
}

// Validate 校验规则并预编译正则；错误信息面向写规则的人（中文，指明字段）。
func (r *Rule) Validate() error {
	if r.Schema != SchemaVersion {
		return fmt.Errorf("schema 版本 %d 不受支持（当前引擎支持 %d）", r.Schema, SchemaVersion)
	}
	if !idRE.MatchString(r.ID) {
		return fmt.Errorf("id %q 不合法：小写字母/数字/下划线/连字符，1–32 位", r.ID)
	}
	if r.Format != "xml" && r.Format != "json" {
		return fmt.Errorf("[%s] format 必须是 xml 或 json，得到 %q", r.ID, r.Format)
	}
	if !strings.Contains(r.Request.URL, "{{query}}") {
		return fmt.Errorf("[%s] request.url 必须包含 {{query}} 占位符", r.ID)
	}
	if r.Items == "" {
		return fmt.Errorf("[%s] items 路径不能为空", r.ID)
	}
	if err := r.checkPath(r.Items); err != nil {
		return fmt.Errorf("[%s] items: %w", r.ID, err)
	}
	if r.Format == "xml" {
		segs, _ := compileXMLPath(r.Items, r.Namespaces)
		if len(segs) > 0 && segs[len(segs)-1].attr != "" {
			return fmt.Errorf("[%s] items 路径不能指向属性", r.ID)
		}
	}
	if r.Fields.Title == nil || r.Fields.Magnet == nil {
		return fmt.Errorf("[%s] fields.title 与 fields.magnet 必填", r.ID)
	}
	if r.Request.TimeoutSeconds < 0 || r.Request.TimeoutSeconds > maxTimeoutSeconds {
		return fmt.Errorf("[%s] request.timeout_seconds 必须在 0–%d 之间", r.ID, maxTimeoutSeconds)
	}
	for name, spec := range r.fieldSpecs() {
		if spec == nil {
			continue
		}
		if err := spec.validate(r); err != nil {
			return fmt.Errorf("[%s] fields.%s: %w", r.ID, name, err)
		}
	}
	return nil
}

// fieldSpecs 按固定求值顺序列出字段（先 title，后续字段可用 $title 引用）。
func (r *Rule) fieldSpecs() map[string]*FieldSpec {
	return map[string]*FieldSpec{
		"title": r.Fields.Title, "infohash": r.Fields.Infohash, "magnet": r.Fields.Magnet,
		"size": r.Fields.Size, "date": r.Fields.Date, "fansub": r.Fields.Fansub,
		"provider": r.Fields.Provider, "seeders": r.Fields.Seeders,
	}
}

// fieldOrder 是求值顺序：后面的字段可以用 $name 引用前面的。
var fieldOrder = []string{"title", "infohash", "magnet", "size", "date", "fansub", "provider", "seeders"}

func (f *FieldSpec) validate(r *Rule) error {
	if f.Path == "" && len(f.Any) == 0 {
		return fmt.Errorf("需要 path 或 any")
	}
	if f.Path != "" && len(f.Any) > 0 {
		return fmt.Errorf("path 与 any 不能同时出现")
	}
	if f.Path != "" && !strings.HasPrefix(f.Path, "$") {
		if err := r.checkPath(f.Path); err != nil {
			return err
		}
	}
	for i := range f.Any {
		if err := f.Any[i].validate(r); err != nil {
			return fmt.Errorf("any[%d]: %w", i, err)
		}
	}
	for i := range f.Transforms {
		if err := f.Transforms[i].validate(); err != nil {
			return fmt.Errorf("transforms[%d]: %w", i, err)
		}
	}
	return nil
}

// checkPath 在加载期完整编译 XML 路径：命名空间前缀、属性写法、空段全部在这里报错，
// 不留到搜索时静默出错。
func (r *Rule) checkPath(path string) error {
	if r.Format != "xml" {
		return nil
	}
	_, err := compileXMLPath(path, r.Namespaces)
	return err
}

func (t *Transform) validate() error {
	if _, ok := knownTransforms[t.Name]; !ok {
		return fmt.Errorf("未知转换 %q", t.Name)
	}
	switch t.Name {
	case "regex":
		if t.Arg == "" {
			if p, _ := t.Args["pattern"].(string); p != "" {
				t.Arg = p
			}
		}
		if t.Arg == "" {
			return fmt.Errorf("regex 需要 pattern")
		}
		re, err := regexp.Compile(t.Arg)
		if err != nil {
			return fmt.Errorf("regex %q 编译失败：%w", t.Arg, err)
		}
		t.re = re
	case "magnet":
		if _, ok := t.Args["trackers"]; ok {
			if _, isList := t.Args["trackers"].([]any); !isList {
				return fmt.Errorf("magnet.trackers 必须是列表")
			}
		}
	}
	return nil
}
