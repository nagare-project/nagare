package sourceplugin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"
	"unicode/utf8"
)

// CapabilityInstalledSources 表示插件提供 POST /v1/sources/inspect，并在启动时
// 加载 InstalledDirEnv 指向目录中的独立来源（同 id 覆盖自带来源）。
const CapabilityInstalledSources = "installed_sources"

// InstalledDirEnv 是把独立安装目录交给插件的环境变量名。
const InstalledDirEnv = "NAGARE_SOURCE_INSTALLED_DIR"

const (
	maximumAllowedHosts      = 256
	maximumSummaryTextBytes  = 2048
	maximumInspectErrorBytes = 600
)

// SourceSummary 是插件校验一份来源文档后返回的展示摘要，供用户确认安装。
type SourceSummary struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Kind         string   `json:"kind"`
	Tier         int      `json:"tier"`
	Version      string   `json:"version"`
	Enabled      bool     `json:"enabled"`
	Description  string   `json:"description,omitempty"`
	Homepage     string   `json:"homepage,omitempty"`
	Ecosystem    string   `json:"ecosystem"`
	Upstream     string   `json:"upstream"`
	License      string   `json:"license"`
	AllowedHosts []string `json:"allowedHosts"`
}

// InspectError 是插件拒绝来源文档时的说明（HTTP 400/422）。Message 来自插件，
// 只能当纯文本展示。
type InspectError struct {
	Category string
	Message  string
}

func (e *InspectError) Error() string {
	return fmt.Sprintf("plugin rejected source (%s): %s", e.Category, e.Message)
}

// ValidSourceID 报告 id 是否符合 Source Spec 的来源 id 规则，也是安装文件名的唯一来源。
func ValidSourceID(value string) bool { return validSourceID(value) }

// InspectSource 让插件只读地校验一份来源文档并返回摘要。插件不写盘、不联网。
func (c *Client) InspectSource(ctx context.Context, format, content string) (SourceSummary, error) {
	if format != "json" && format != "yaml" {
		return SourceSummary{}, errors.New("source format must be json or yaml")
	}
	body, err := json.Marshal(map[string]string{"format": format, "content": content})
	if err != nil {
		return SourceSummary{}, err
	}
	if len(body) > maximumJSONBytes+64*1024 {
		return SourceSummary{}, errors.New("source document is too large")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/v1/sources/inspect", bytes.NewReader(body))
	if err != nil {
		return SourceSummary{}, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Nagare-Protocol-Version", "1")
	response, err := c.http.Do(request)
	if err != nil {
		return SourceSummary{}, fmt.Errorf("call source plugin: %w", err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, maximumJSONBytes+1))
	if err != nil {
		return SourceSummary{}, err
	}
	if len(data) > maximumJSONBytes {
		return SourceSummary{}, errors.New("plugin JSON response exceeds 1 MiB")
	}
	if response.StatusCode == http.StatusBadRequest || response.StatusCode == http.StatusUnprocessableEntity {
		return SourceSummary{}, decodeInspectError(data)
	}
	if response.StatusCode != http.StatusOK {
		return SourceSummary{}, fmt.Errorf("plugin returned HTTP %d", response.StatusCode)
	}
	if mediaType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type")); err != nil || mediaType != "application/json" {
		return SourceSummary{}, errors.New("plugin returned an invalid content type")
	}
	var result struct {
		Source *SourceSummary `json:"source"`
	}
	if err := decodeStrict(data, &result); err != nil || result.Source == nil {
		return SourceSummary{}, errors.New("plugin returned an invalid inspect response")
	}
	if err := validateSummary(*result.Source); err != nil {
		return SourceSummary{}, err
	}
	return *result.Source, nil
}

func decodeInspectError(data []byte) error {
	var wire struct {
		Error struct {
			Category string `json:"category"`
			Message  string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(data, &wire); err != nil || wire.Error.Category == "" {
		return &InspectError{Category: "invalid_source", Message: "插件拒绝了这份来源文件"}
	}
	return &InspectError{Category: wire.Error.Category, Message: clipText(wire.Error.Message, maximumInspectErrorBytes)}
}

func validateSummary(summary SourceSummary) error {
	if !validSourceID(summary.ID) || strings.TrimSpace(summary.Name) == "" ||
		(summary.Kind != "web" && summary.Kind != "bt") || summary.Tier < 0 || summary.Tier > 9 ||
		strings.TrimSpace(summary.Version) == "" || len(summary.AllowedHosts) > maximumAllowedHosts {
		return errors.New("plugin returned an invalid source summary")
	}
	for _, text := range []string{summary.Name, summary.Version, summary.Description, summary.Homepage, summary.Ecosystem, summary.Upstream, summary.License} {
		if len(text) > maximumSummaryTextBytes || !utf8.ValidString(text) {
			return errors.New("plugin returned an invalid source summary")
		}
	}
	for _, host := range summary.AllowedHosts {
		if !validHostPattern(host) {
			return errors.New("plugin returned an invalid allowed host")
		}
	}
	return nil
}

// validHostPattern 与 Source Spec 的 allowed_hosts 规则一致：主机名，可带 "*." 通配前缀。
func validHostPattern(host string) bool {
	host = strings.TrimPrefix(host, "*.")
	if host == "" || len(host) > 253 {
		return false
	}
	for _, char := range host {
		if (char < 'a' || char > 'z') && (char < 'A' || char > 'Z') && (char < '0' || char > '9') && char != '-' && char != '.' {
			return false
		}
	}
	return true
}

func clipText(text string, limit int) string {
	text = strings.ToValidUTF8(strings.Join(strings.Fields(text), " "), "")
	if len(text) <= limit {
		return text
	}
	cut := limit
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}
	return text[:cut] + "…"
}
