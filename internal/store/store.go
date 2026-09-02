// Package store 是 nagare 的本地持久化：单 JSON 文件 + 内存态，写入走原子替换。
// M1 的量级（千集）JSON 绰绰有余，真到瓶颈再换引擎。
// 文件里含 animego 会话凭证，权限必须 0600 —— 与 config 同一条安全要求。
package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/nagare-project/nagare/internal/random"
)

// Folder 是用户添加的媒体库根目录。
type Folder struct {
	ID      string `json:"id"`
	Path    string `json:"path"`
	AddedAt int64  `json:"addedAt"`
}

// Binding 是某个文件（软 id）与 dandanplay/animego 的匹配结果。
type Binding struct {
	AnilistID       int    `json:"anilistId,omitempty"`
	DandanEpisodeID int64  `json:"dandanEpisodeId,omitempty"`
	Episode         int    `json:"episode,omitempty"`
	Title           string `json:"title,omitempty"`
	EpisodeTitle    string `json:"episodeTitle,omitempty"`
	MatchedAt       int64  `json:"matchedAt"`
}

// Progress 是单文件的观看进度（挂软 id；文件改名后 id 变化即视为新条目，M1 接受）。
type Progress struct {
	PositionSec float64 `json:"positionSec"`
	DurationSec float64 `json:"durationSec"`
	UpdatedAt   int64   `json:"updatedAt"`
	Completed   bool    `json:"completed"`
	// Synced：完成标记是否已回写 animego（避免每次退出重复调接口）。
	Synced bool `json:"synced"`
}

// AnimegoSession 是 animego 账号会话（access 15 分钟 + refresh cookie 7 天）。
type AnimegoSession struct {
	Email         string `json:"email,omitempty"`
	AccessToken   string `json:"accessToken,omitempty"`
	RefreshCookie string `json:"refreshCookie,omitempty"`
}

// RulesConfig 是磁力源规则的用户配置：规则从哪来、哪些源被禁用（决议 A3：本体零内置源）。
type RulesConfig struct {
	RemoteURL  string   `json:"remoteUrl,omitempty"`
	LocalDir   string   `json:"localDir,omitempty"`
	Disabled   []string `json:"disabled,omitempty"`
	LastSyncAt int64    `json:"lastSyncAt,omitempty"`
}

// Data 是落盘的全部状态。
type Data struct {
	Folders  []Folder            `json:"folders"`
	Hashes   map[string]string   `json:"hashes"`   // fileID → 16MB MD5（懒算缓存）
	Bindings map[string]Binding  `json:"bindings"` // fileID → 匹配
	Progress map[string]Progress `json:"progress"` // fileID → 进度
	Animego  AnimegoSession      `json:"animego"`
	Rules    RulesConfig         `json:"rules"`
}

func emptyData() Data {
	return Data{
		Folders:  []Folder{},
		Hashes:   map[string]string{},
		Bindings: map[string]Binding{},
		Progress: map[string]Progress{},
	}
}

// Store 是并发安全的状态容器；每次变更立即原子落盘。
type Store struct {
	path string
	mu   sync.Mutex
	data Data
}

// Open 读取（或初始化）状态文件。解析失败是硬错误 —— 绝不静默重建用户状态。
func Open(path string) (*Store, error) {
	s := &Store{path: path, data: emptyData()}
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, fmt.Errorf("读取状态文件 %s: %w", path, err)
	}
	if err := json.Unmarshal(raw, &s.data); err != nil {
		return nil, fmt.Errorf("解析状态文件 %s（如需放弃旧状态请手动删除该文件）: %w", path, err)
	}
	// 老文件缺 map 字段时补齐，避免 nil map 写入 panic。
	if s.data.Hashes == nil {
		s.data.Hashes = map[string]string{}
	}
	if s.data.Bindings == nil {
		s.data.Bindings = map[string]Binding{}
	}
	if s.data.Progress == nil {
		s.data.Progress = map[string]Progress{}
	}
	if s.data.Folders == nil {
		s.data.Folders = []Folder{}
	}
	return s, nil
}

// save 原子落盘（tmp + rename，0600/0700）。调用方必须已持锁。
func (s *Store) save() error {
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("创建状态目录: %w", err)
	}
	// MkdirAll 只对新建目录生效 0700；state.json 含会话凭证，已存在的宽权限
	// 目录也要收紧（与 config.Save 同款处理，不依赖调用顺序）。
	if err := os.Chmod(dir, 0o700); err != nil {
		return fmt.Errorf("收紧状态目录权限: %w", err)
	}
	buf, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化状态: %w", err)
	}
	// 写临时文件 → fsync → 原子改名：断电/崩溃时要么是旧内容要么是新内容，
	// 不会留下半截 JSON（解析失败是硬错误，半截文件会让下次启动直接失败）。
	tmp := s.path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("创建临时状态 %s: %w", tmp, err)
	}
	if _, err := f.Write(buf); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("写入临时状态 %s: %w", tmp, err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("刷盘临时状态 %s: %w", tmp, err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("关闭临时状态 %s: %w", tmp, err)
	}
	// O_CREATE 的权限受 umask 影响，显式收紧（文件含会话凭证）。
	if err := os.Chmod(tmp, 0o600); err != nil {
		return fmt.Errorf("收紧状态权限: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		return fmt.Errorf("替换状态文件: %w", err)
	}
	return nil
}

// Snapshot 返回当前状态的深拷贝（读方随意持有，不与内部共享）。
func (s *Store) Snapshot() Data {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := Data{
		Folders:  append([]Folder{}, s.data.Folders...),
		Hashes:   map[string]string{},
		Bindings: map[string]Binding{},
		Progress: map[string]Progress{},
		Animego:  s.data.Animego,
	}
	for k, v := range s.data.Hashes {
		out.Hashes[k] = v
	}
	for k, v := range s.data.Bindings {
		out.Bindings[k] = v
	}
	for k, v := range s.data.Progress {
		out.Progress[k] = v
	}
	return out
}

// AddFolder 添加媒体库目录；重复路径报错。
func (s *Store) AddFolder(path string) (Folder, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, f := range s.data.Folders {
		if f.Path == path {
			return Folder{}, fmt.Errorf("目录已在库中：%s", path)
		}
	}
	f := Folder{ID: "f-" + random.Hex(4), Path: path, AddedAt: time.Now().UnixMilli()}
	s.data.Folders = append(s.data.Folders, f)
	if err := s.save(); err != nil {
		return Folder{}, err
	}
	return f, nil
}

// RemoveFolder 按 id 移除目录；返回是否存在。
func (s *Store) RemoveFolder(id string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	kept := s.data.Folders[:0]
	found := false
	for _, f := range s.data.Folders {
		if f.ID == id {
			found = true
			continue
		}
		kept = append(kept, f)
	}
	s.data.Folders = kept
	if !found {
		return false, nil
	}
	return true, s.save()
}

// SetHash 缓存懒算出的 16MB hash。
func (s *Store) SetHash(fileID, hash string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data.Hashes[fileID] = hash
	return s.save()
}

// Hash 读取缓存的 hash；无则空串。
func (s *Store) Hash(fileID string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.data.Hashes[fileID]
}

// SetBinding 保存匹配结果。
func (s *Store) SetBinding(fileID string, b Binding) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data.Bindings[fileID] = b
	return s.save()
}

// Binding 读取匹配结果。
func (s *Store) Binding(fileID string) (Binding, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.data.Bindings[fileID]
	return b, ok
}

// SetProgress 保存进度。
func (s *Store) SetProgress(fileID string, p Progress) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data.Progress[fileID] = p
	return s.save()
}

// Progress 读取进度。
func (s *Store) Progress(fileID string) (Progress, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.data.Progress[fileID]
	return p, ok
}

// SetAnimegoSession 保存 animego 会话（登录/刷新后调用）。
func (s *Store) SetAnimegoSession(sess AnimegoSession) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data.Animego = sess
	return s.save()
}

// AnimegoSession 读取会话。
func (s *Store) AnimegoSession() AnimegoSession {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.data.Animego
}

// RulesConfig 读取磁力源规则配置（Disabled 是副本）。
func (s *Store) RulesConfig() RulesConfig {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.data.Rules
	c.Disabled = append([]string(nil), c.Disabled...)
	return c
}

// SetRulesConfig 保存磁力源规则配置。
func (s *Store) SetRulesConfig(c RulesConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data.Rules = c
	return s.save()
}

// UpdateRulesConfig 在锁内读改写规则配置：并发的两个变更不会互相盖掉对方的改动
// （分开 Get/Set 是经典的丢失更新）。
func (s *Store) UpdateRulesConfig(mutate func(c *RulesConfig)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.data.Rules
	c.Disabled = append([]string(nil), c.Disabled...)
	mutate(&c)
	s.data.Rules = c
	return s.save()
}
