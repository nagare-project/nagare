// Package rulesync 从用户指定的规则仓库地址拉取规则文件到本地目录（决议 A3）。
//
// 本体不内置、不预填任何地址 —— 规则来源由用户提供（红线 1）。仓库结构约定：
//
//	<remoteURL>/index.json        {"schema":1,"rules":[{"file":"foo.yaml","sha256":"..."}]}
//	<remoteURL>/<file>            规则文件本体
//
// 每个文件在落盘前先用 rules.Parse 校验：坏规则不安装、保留本地旧版本、记入报告。
package rulesync

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/nagare-project/nagare/internal/rules"
)

// IndexSchemaVersion 是清单格式版本。
const IndexSchemaVersion = 1

// maxFileBytes 限制单个规则/清单文件大小。
const maxFileBytes = 1 << 20

// Index 是仓库清单。
type Index struct {
	Schema int          `json:"schema"`
	Rules  []IndexEntry `json:"rules"`
}

// IndexEntry 是清单里的一个规则文件。
type IndexEntry struct {
	File   string `json:"file"`
	SHA256 string `json:"sha256,omitempty"`
}

// Report 是一次同步的结果；Errors 里是单文件级失败，不影响其他文件。
type Report struct {
	Added     int      `json:"added"`
	Updated   int      `json:"updated"`
	Unchanged int      `json:"unchanged"`
	Removed   int      `json:"removed"`
	Errors    []string `json:"errors"`
}

// Syncer 执行同步；HTTP 客户端与 UA 由调用方注入。
type Syncer struct {
	Client    *http.Client
	UserAgent string
}

var fileNameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*\.ya?ml$`)

// ValidateRemoteURL 只接受 https（或本机 http，开发用）；拒绝带查询串/片段的地址。
func ValidateRemoteURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "", fmt.Errorf("规则仓库地址不合法")
	}
	isLocal := u.Hostname() == "127.0.0.1" || u.Hostname() == "localhost"
	if u.Scheme != "https" && !(u.Scheme == "http" && isLocal) {
		return "", fmt.Errorf("规则仓库地址必须是 https（本机调试可用 http://127.0.0.1）")
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("规则仓库地址不能带查询串或片段")
	}
	return strings.TrimRight(u.String(), "/"), nil
}

// Sync 拉取清单与规则文件到 dir。返回 error 只在「整体无法进行」（地址坏、清单拿不到）时；
// 单个文件的问题进 Report.Errors。
func (s *Syncer) Sync(ctx context.Context, remoteURL, dir string) (Report, error) {
	var rep Report
	base, err := ValidateRemoteURL(remoteURL)
	if err != nil {
		return rep, err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return rep, fmt.Errorf("创建规则目录: %w", err)
	}

	raw, err := s.get(ctx, base+"/index.json")
	if err != nil {
		return rep, fmt.Errorf("获取规则清单失败: %w", err)
	}
	var idx Index
	if err := json.Unmarshal(raw, &idx); err != nil {
		return rep, fmt.Errorf("规则清单不是合法 JSON: %w", err)
	}
	if idx.Schema != IndexSchemaVersion {
		return rep, fmt.Errorf("规则清单版本 %d 不受支持", idx.Schema)
	}

	wanted := map[string]bool{}
	for _, e := range idx.Rules {
		if !fileNameRE.MatchString(e.File) {
			rep.Errors = append(rep.Errors, fmt.Sprintf("%s: 文件名不合法，已跳过", e.File))
			continue
		}
		wanted[e.File] = true
		body, err := s.get(ctx, base+"/"+e.File)
		if err != nil {
			rep.Errors = append(rep.Errors, fmt.Sprintf("%s: 下载失败：%v", e.File, err))
			continue
		}
		if e.SHA256 != "" {
			sum := sha256.Sum256(body)
			if !strings.EqualFold(hex.EncodeToString(sum[:]), e.SHA256) {
				rep.Errors = append(rep.Errors, fmt.Sprintf("%s: 校验和不匹配，已拒绝", e.File))
				continue
			}
		}
		if _, err := rules.Parse(body); err != nil {
			rep.Errors = append(rep.Errors, fmt.Sprintf("%s: 规则无效，未安装：%v", e.File, err))
			continue
		}
		target := filepath.Join(dir, e.File)
		old, readErr := os.ReadFile(target)
		switch {
		case readErr == nil && string(old) == string(body):
			rep.Unchanged++
			continue
		case readErr == nil:
			rep.Updated++
		default:
			rep.Added++
		}
		if err := writeAtomic(target, body); err != nil {
			rep.Errors = append(rep.Errors, fmt.Sprintf("%s: 写入失败：%v", e.File, err))
		}
	}

	// 清单里已不存在的规则文件视为下架，删除。
	entries, _ := os.ReadDir(dir)
	for _, ent := range entries {
		name := ent.Name()
		if ent.IsDir() || !fileNameRE.MatchString(name) || wanted[name] {
			continue
		}
		if err := os.Remove(filepath.Join(dir, name)); err == nil {
			rep.Removed++
		}
	}
	return rep, nil
}

func (s *Syncer) get(ctx context.Context, u string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	ua := s.UserAgent
	if ua == "" {
		ua = "nagare"
	}
	req.Header.Set("User-Agent", ua)
	client := s.Client
	if client == nil {
		client = http.DefaultClient
	}
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", res.StatusCode)
	}
	return io.ReadAll(io.LimitReader(res.Body, maxFileBytes))
}

func writeAtomic(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
