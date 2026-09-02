package api

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"

	errs "github.com/nagare-project/nagare/internal/errors"
	"github.com/nagare-project/nagare/internal/rules"
	"github.com/nagare-project/nagare/internal/rulesync"
	"github.com/nagare-project/nagare/internal/store"
)

// SourcesService 管磁力源规则的生命周期：加载（本地目录或同步目录）、启用状态持久化、
// 从用户指定的仓库同步、搜索与自检。本体零内置源：规则目录为空就是空。
type SourcesService struct {
	st       *store.Store
	reg      *rules.Registry
	syncer   *rulesync.Syncer
	rulesDir string // 同步落点：<configDir>/rules

	mu           sync.Mutex
	loaded       int
	loadErrors   []string
	lastLoadedAt int64
}

// NewSourcesService 构造服务（不自动加载；调用方决定时机）。
func NewSourcesService(st *store.Store, fetcher *rules.Fetcher, syncer *rulesync.Syncer, rulesDir string) *SourcesService {
	return &SourcesService{st: st, reg: rules.NewRegistry(fetcher), syncer: syncer, rulesDir: rulesDir}
}

// activeDir 返回当前生效的规则目录：用户指定的本地目录优先于同步目录。
func (s *SourcesService) activeDir() string {
	if cfg := s.st.RulesConfig(); cfg.LocalDir != "" {
		return cfg.LocalDir
	}
	return s.rulesDir
}

// Load 重新加载规则目录。目录不存在视为零规则（首次运行），不算错误；
// 单个坏文件记入 errors，其余规则照常生效。
func (s *SourcesService) Load() (int, []string) {
	cfg := s.st.RulesConfig()
	dir := s.activeDir()
	var loaded []*rules.Rule
	var errStrs []string
	if _, statErr := os.Stat(dir); statErr == nil {
		rs, loadErrs := rules.LoadDir(dir)
		loaded = rs
		for _, e := range loadErrs {
			errStrs = append(errStrs, e.Error())
		}
	}
	disabled := map[string]bool{}
	for _, id := range cfg.Disabled {
		disabled[id] = true
	}
	s.reg.ReplaceWithDisabled(loaded, disabled)
	s.mu.Lock()
	s.loaded, s.loadErrors, s.lastLoadedAt = len(loaded), errStrs, time.Now().UnixMilli()
	s.mu.Unlock()
	return len(loaded), errStrs
}

// SourceView 是一个源给界面的投影。
type SourceView struct {
	ID           string             `json:"id"`
	Name         string             `json:"name"`
	Homepage     string             `json:"homepage"`
	Enabled      bool               `json:"enabled"`
	Capabilities rules.Capabilities `json:"capabilities"`
	HasSelfTest  bool               `json:"hasSelfTest"`
}

// RulesView 是规则来源配置与加载状态。
type RulesView struct {
	RemoteURL    string   `json:"remoteUrl"`
	LocalDir     string   `json:"localDir"`
	Dir          string   `json:"dir"`
	Loaded       int      `json:"loaded"`
	Errors       []string `json:"errors"`
	LastLoadedAt *int64   `json:"lastLoadedAt"`
	LastSyncAt   *int64   `json:"lastSyncAt"`
}

// SourcesView 是 GET /api/sources 的数据。
type SourcesView struct {
	Sources []SourceView `json:"sources"`
	Rules   RulesView    `json:"rules"`
}

// View 组装源列表与规则状态。
func (s *SourcesService) View() SourcesView {
	view := SourcesView{Sources: []SourceView{}, Rules: s.rulesView()}
	for _, r := range s.reg.Rules() {
		view.Sources = append(view.Sources, SourceView{
			ID: r.ID, Name: r.Name, Homepage: r.Homepage,
			Enabled:      s.reg.Enabled(r.ID),
			Capabilities: r.Capabilities,
			HasSelfTest:  r.SelfTest.Query != "",
		})
	}
	return view
}

func (s *SourcesService) rulesView() RulesView {
	cfg := s.st.RulesConfig()
	s.mu.Lock()
	defer s.mu.Unlock()
	v := RulesView{
		RemoteURL: cfg.RemoteURL, LocalDir: cfg.LocalDir, Dir: s.activeDir(),
		Loaded: s.loaded, Errors: append([]string{}, s.loadErrors...),
	}
	if s.lastLoadedAt > 0 {
		t := s.lastLoadedAt
		v.LastLoadedAt = &t
	}
	if cfg.LastSyncAt > 0 {
		t := cfg.LastSyncAt
		v.LastSyncAt = &t
	}
	return v
}

// SetEnabled 切换某源启用状态并持久化。
func (s *SourcesService) SetEnabled(id string, on bool) error {
	found := false
	for _, r := range s.reg.Rules() {
		if r.ID == id {
			found = true
		}
	}
	if !found {
		return errs.New(errs.CategoryInput, "sources.enable", "未知的源："+id, "")
	}
	s.reg.SetEnabled(id, on)
	return s.st.UpdateRulesConfig(func(cfg *store.RulesConfig) {
		var disabled []string
		for _, d := range cfg.Disabled {
			if d != id {
				disabled = append(disabled, d)
			}
		}
		if !on {
			disabled = append(disabled, id)
		}
		cfg.Disabled = disabled
	})
}

// SetConfig 更新规则来源（nil 表示不改），校验后落盘并重新加载。
func (s *SourcesService) SetConfig(remoteURL, localDir *string) (RulesView, error) {
	// 先校验、后在锁内一次写入，避免与并发的启停/同步互相覆盖。
	var nextRemote, nextLocal *string
	if remoteURL != nil {
		v := ""
		if *remoteURL != "" {
			u, err := rulesync.ValidateRemoteURL(*remoteURL)
			if err != nil {
				return RulesView{}, errs.Wrap(errs.CategoryInput, "sources.config", err.Error(), "", err)
			}
			v = u
		}
		nextRemote = &v
	}
	if localDir != nil {
		v := ""
		if *localDir != "" {
			p := filepath.Clean(*localDir)
			info, err := os.Stat(p)
			if !filepath.IsAbs(p) || err != nil || !info.IsDir() {
				return RulesView{}, errs.New(errs.CategoryInput, "sources.config", "本地规则目录必须是存在的绝对路径", "")
			}
			v = p
		}
		nextLocal = &v
	}
	err := s.st.UpdateRulesConfig(func(cfg *store.RulesConfig) {
		if nextRemote != nil {
			cfg.RemoteURL = *nextRemote
		}
		if nextLocal != nil {
			cfg.LocalDir = *nextLocal
		}
	})
	if err != nil {
		return RulesView{}, err
	}
	s.Load()
	return s.rulesView(), nil
}

// Sync 从用户配置的仓库地址拉取规则到同步目录，然后重新加载。
func (s *SourcesService) Sync(ctx context.Context) (rulesync.Report, error) {
	cfg := s.st.RulesConfig()
	if cfg.RemoteURL == "" {
		return rulesync.Report{}, errs.New(errs.CategoryInput, "sources.sync", "还没有配置规则仓库地址", "先在设置里填写规则仓库的 HTTPS 地址")
	}
	if s.syncer == nil {
		return rulesync.Report{}, errors.New("同步器未初始化")
	}
	rep, err := s.syncer.Sync(ctx, cfg.RemoteURL, s.rulesDir)
	if err != nil {
		return rulesync.Report{}, errs.Wrap(errs.CategoryNetwork, "sources.sync", "同步规则失败："+err.Error(), "检查地址与网络后重试", err)
	}
	if err := s.st.UpdateRulesConfig(func(cfg *store.RulesConfig) {
		cfg.LastSyncAt = time.Now().UnixMilli()
	}); err != nil {
		return rep, err
	}
	s.Load()
	if rep.Errors == nil {
		rep.Errors = []string{}
	}
	return rep, nil
}

// Search 聚合搜索。
func (s *SourcesService) Search(ctx context.Context, q string) rules.SearchResult {
	return s.reg.Search(ctx, q)
}

// SelfCheck 探活某源。
func (s *SourcesService) SelfCheck(ctx context.Context, id string) (rules.Outcome, error) {
	out, err := s.reg.SelfCheck(ctx, id)
	if err != nil {
		return rules.Outcome{}, errs.Wrap(errs.CategoryInput, "sources.selfcheck", err.Error(), "", err)
	}
	return out, nil
}

// RuleCount 返回已加载规则数（日志用）。
func (s *SourcesService) RuleCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loaded
}
