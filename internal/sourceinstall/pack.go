package sourceinstall

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	// PackSchema 是来源包文档的格式标识：一个文件携带一组完整的独立来源文档。
	PackSchema = "nagare-source-pack/v1"
	// MaxPackSources 限制单个来源包的来源数量，确认页和插件重启都要逐个处理。
	MaxPackSources = 64

	packSchemaPrefix  = "nagare-source-pack/"
	maxPackNameRunes  = 80
	maxPackDescRunes  = 500
	maxPackVersionLen = 64
)

// ErrUnsupportedPack 表示文件声明为来源包，但版本不是当前支持的 v1。
var ErrUnsupportedPack = errors.New("不支持的来源包格式")

// BundleMember 是待安装的一个来源文档原文。
type BundleMember struct {
	Format  string
	Content []byte
}

// Bundle 是一次下载拆出来的安装内容。单个来源文档是只有一个成员、Pack 为 false 的 Bundle，
// 其 ID/Name/Version 由调用方在插件校验后用来源摘要补上。
type Bundle struct {
	Pack        bool
	ID          string
	Name        string
	Description string
	Version     string
	Members     []BundleMember
}

type packDocument struct {
	Schema      string            `json:"schema"`
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Version     string            `json:"version"`
	Sources     []json.RawMessage `json:"sources"`
}

// ParseBundle 识别来源包；不是来源包的内容原样作为单个来源交给插件校验。
// 来源包里的每个成员都会被重新排版成独立的 JSON 文件，安装后与单独安装的来源没有区别。
func ParseBundle(download Download) (Bundle, error) {
	single := Bundle{Members: []BundleMember{{Format: download.Format, Content: download.Content}}}
	if download.Format != "json" {
		return single, nil
	}
	var header struct {
		Schema any `json:"schema"`
	}
	// 只看第一个 JSON 值，这样声明为来源包但末尾有多余内容的文件会得到明确的错误。
	if err := json.NewDecoder(bytes.NewReader(download.Content)).Decode(&header); err != nil {
		// 解析失败的 JSON 交给插件给出具体的错误。
		return single, nil
	}
	schema, _ := header.Schema.(string)
	if !strings.HasPrefix(schema, packSchemaPrefix) {
		return single, nil
	}
	if schema != PackSchema {
		return Bundle{}, fmt.Errorf("%w %q", ErrUnsupportedPack, schema)
	}
	var document packDocument
	decoder := json.NewDecoder(bytes.NewReader(download.Content))
	if err := decoder.Decode(&document); err != nil {
		return Bundle{}, fmt.Errorf("来源包格式不对: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return Bundle{}, errors.New("来源包格式不对: 文件末尾有多余内容")
	}
	if err := validatePackHeader(document); err != nil {
		return Bundle{}, err
	}
	bundle := Bundle{
		Pack: true, ID: document.ID, Name: strings.TrimSpace(document.Name),
		Description: strings.TrimSpace(document.Description), Version: document.Version,
		Members: make([]BundleMember, 0, len(document.Sources)),
	}
	for index, raw := range document.Sources {
		trimmed := bytes.TrimSpace(raw)
		if len(trimmed) == 0 || trimmed[0] != '{' {
			return Bundle{}, fmt.Errorf("来源包第 %d 个来源不是 JSON 对象", index+1)
		}
		var formatted bytes.Buffer
		if err := json.Indent(&formatted, trimmed, "", "  "); err != nil {
			return Bundle{}, fmt.Errorf("来源包第 %d 个来源格式不对: %w", index+1, err)
		}
		formatted.WriteByte('\n')
		if formatted.Len() > MaxSourceBytes {
			return Bundle{}, fmt.Errorf("来源包第 %d 个来源超过 1 MiB", index+1)
		}
		bundle.Members = append(bundle.Members, BundleMember{Format: "json", Content: formatted.Bytes()})
	}
	return bundle, nil
}

func validatePackHeader(document packDocument) error {
	if !validID(document.ID) {
		return fmt.Errorf("来源包 ID %q 不合法", document.ID)
	}
	name := strings.TrimSpace(document.Name)
	if name == "" || utf8.RuneCountInString(name) > maxPackNameRunes || hasControl(name) {
		return errors.New("来源包名称为空、过长或含控制字符")
	}
	if utf8.RuneCountInString(document.Description) > maxPackDescRunes || hasControl(document.Description) {
		return errors.New("来源包说明过长或含控制字符")
	}
	if document.Version == "" || len(document.Version) > maxPackVersionLen || hasControl(document.Version) || strings.TrimSpace(document.Version) != document.Version {
		return errors.New("来源包版本为空或不合法")
	}
	if len(document.Sources) == 0 {
		return errors.New("来源包里没有来源")
	}
	if len(document.Sources) > MaxPackSources {
		return fmt.Errorf("来源包最多包含 %d 个来源", MaxPackSources)
	}
	return nil
}

func hasControl(value string) bool {
	return strings.IndexFunc(value, unicode.IsControl) >= 0
}
