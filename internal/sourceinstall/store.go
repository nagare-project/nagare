package sourceinstall

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

const (
	metadataSchema = "nagare-installed-sources/v1"
	metadataFile   = "installed.json"
	sourcesDirName = "sources"
)

// PackageSource 是安装包里的一个来源；文件保存为 sources/<ID>.<Format 对应扩展名>。
type PackageSource struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	Version string `json:"version"`
	Format  string `json:"format"`
}

// Package 记录一次安装的出处：可以是单个来源文档，也可以是一个来源包。
// 设置页按 Package 展示和卸载；卸载会删除它装上的全部来源。
type Package struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Version     string          `json:"version"`
	Pack        bool            `json:"pack"`
	URL         string          `json:"url"`
	SHA256      string          `json:"sha256"`
	InstalledAt time.Time       `json:"installedAt"`
	Sources     []PackageSource `json:"sources"`
}

// ConflictError 表示要安装的来源已经属于另一个安装包。
type ConflictError struct {
	SourceID    string
	PackageID   string
	PackageName string
}

func (e *ConflictError) Error() string {
	return fmt.Sprintf("来源 %s 已由「%s」安装，请先卸载", e.SourceID, e.PackageName)
}

type metadata struct {
	Schema   string    `json:"schema"`
	Packages []Package `json:"packages"`
}

// Store 管理 <dir>/sources/<id>.<json|yaml> 与 <dir>/installed.json。
// 插件只读取 sources 目录本身；以点开头的临时文件会被插件忽略。
type Store struct {
	dir string
	mu  sync.Mutex
}

func NewStore(dir string) *Store {
	return &Store{dir: dir}
}

// SourcesDir 是交给来源插件的 NAGARE_SOURCE_INSTALLED_DIR。
func (s *Store) SourcesDir() string {
	return filepath.Join(s.dir, sourcesDirName)
}

func (s *Store) List() ([]Package, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	meta, err := s.load()
	if err != nil {
		return nil, err
	}
	return meta.Packages, nil
}

func (s *Store) Get(id string) (Package, bool, error) {
	packages, err := s.List()
	if err != nil {
		return Package{}, false, err
	}
	for _, pkg := range packages {
		if pkg.ID == id {
			return pkg, true, nil
		}
	}
	return Package{}, false, nil
}

// Conflicts 返回 pkg 里已经属于其他安装包的来源；同 id 的安装包视为更新，不算冲突。
func (s *Store) Conflicts(pkg Package) ([]ConflictError, error) {
	packages, err := s.List()
	if err != nil {
		return nil, err
	}
	return conflicts(packages, pkg), nil
}

func conflicts(installed []Package, pkg Package) []ConflictError {
	wanted := make(map[string]bool, len(pkg.Sources))
	for _, source := range pkg.Sources {
		wanted[source.ID] = true
	}
	result := []ConflictError{}
	for _, other := range installed {
		if other.ID == pkg.ID {
			// 同一个 id 一边是来源包、一边是单个来源时不能互相覆盖。
			if other.Pack != pkg.Pack {
				result = append(result, ConflictError{SourceID: pkg.ID, PackageID: other.ID, PackageName: other.Name})
			}
			continue
		}
		for _, source := range other.Sources {
			if wanted[source.ID] {
				result = append(result, ConflictError{SourceID: source.ID, PackageID: other.ID, PackageName: other.Name})
			}
		}
	}
	return result
}

// Put 写入安装包的全部来源文件并更新记录；files 的键是来源 id。
// 同 id 的安装包被替换，新版本里不再包含的旧来源文件一并删除。
// 返回的 undo 把目录恢复到写入前的状态，供插件重启失败时回滚。
func (s *Store) Put(pkg Package, files map[string][]byte) (func() error, error) {
	if !validID(pkg.ID) {
		return nil, fmt.Errorf("安装包 ID %q 不合法", pkg.ID)
	}
	if len(pkg.Sources) == 0 || len(files) != len(pkg.Sources) {
		return nil, errors.New("安装包没有来源，或来源文件与记录不一致")
	}
	extensions := make(map[string]string, len(pkg.Sources))
	for _, source := range pkg.Sources {
		if !validID(source.ID) {
			return nil, fmt.Errorf("来源 ID %q 不合法", source.ID)
		}
		if _, duplicate := extensions[source.ID]; duplicate {
			return nil, fmt.Errorf("来源 %s 在安装包里重复", source.ID)
		}
		if _, ok := files[source.ID]; !ok {
			return nil, fmt.Errorf("缺少来源 %s 的文件", source.ID)
		}
		extension, err := extensionFor(source.Format)
		if err != nil {
			return nil, err
		}
		extensions[source.ID] = extension
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(s.SourcesDir(), 0o700); err != nil {
		return nil, err
	}
	meta, err := s.load()
	if err != nil {
		return nil, err
	}
	if found := conflicts(meta.Packages, pkg); len(found) > 0 {
		conflict := found[0]
		return nil, &conflict
	}
	affected := make([]string, 0, len(pkg.Sources))
	for _, source := range pkg.Sources {
		affected = append(affected, source.ID)
	}
	var dropped []string
	if previous, ok := find(meta.Packages, pkg.ID); ok {
		for _, source := range previous.Sources {
			if _, kept := extensions[source.ID]; !kept {
				dropped = append(dropped, source.ID)
			}
		}
	}
	snapshot, err := s.snapshot(append(affected, dropped...))
	if err != nil {
		return nil, err
	}
	fail := func(err error) (func() error, error) {
		_ = snapshot.restore(s)
		return nil, err
	}
	for _, source := range pkg.Sources {
		extension := extensions[source.ID]
		if err := writeFileAtomic(filepath.Join(s.SourcesDir(), source.ID+extension), files[source.ID]); err != nil {
			return fail(err)
		}
		for _, other := range sourceExtensions {
			if other != extension {
				if err := removeIfExists(filepath.Join(s.SourcesDir(), source.ID+other)); err != nil {
					return fail(err)
				}
			}
		}
	}
	for _, id := range dropped {
		if err := s.removeSourceFiles(id); err != nil {
			return fail(err)
		}
	}
	meta.Packages = append(without(meta.Packages, pkg.ID), pkg)
	if err := s.save(meta); err != nil {
		return fail(err)
	}
	return func() error {
		s.mu.Lock()
		defer s.mu.Unlock()
		return snapshot.restore(s)
	}, nil
}

// Remove 删除安装包的全部来源文件与记录；不存在时返回 os.ErrNotExist。
func (s *Store) Remove(id string) (func() error, error) {
	if !validID(id) {
		return nil, fmt.Errorf("安装包 ID %q 不合法", id)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	meta, err := s.load()
	if err != nil {
		return nil, err
	}
	pkg, ok := find(meta.Packages, id)
	if !ok {
		return nil, os.ErrNotExist
	}
	ids := make([]string, 0, len(pkg.Sources))
	for _, source := range pkg.Sources {
		ids = append(ids, source.ID)
	}
	snapshot, err := s.snapshot(ids)
	if err != nil {
		return nil, err
	}
	for _, sourceID := range ids {
		if err := s.removeSourceFiles(sourceID); err != nil {
			_ = snapshot.restore(s)
			return nil, err
		}
	}
	meta.Packages = without(meta.Packages, id)
	if err := s.save(meta); err != nil {
		_ = snapshot.restore(s)
		return nil, err
	}
	return func() error {
		s.mu.Lock()
		defer s.mu.Unlock()
		return snapshot.restore(s)
	}, nil
}

func (s *Store) removeSourceFiles(id string) error {
	for _, extension := range sourceExtensions {
		if err := removeIfExists(filepath.Join(s.SourcesDir(), id+extension)); err != nil {
			return err
		}
	}
	return nil
}

var sourceExtensions = []string{".json", ".yaml", ".yml"}

func extensionFor(format string) (string, error) {
	switch format {
	case "json":
		return ".json", nil
	case "yaml":
		return ".yaml", nil
	default:
		return "", fmt.Errorf("不支持的来源文件格式 %q", format)
	}
}

// snapshot 保存记录文件原文与受影响来源的全部文件，restore 逐字节还原。
type snapshot struct {
	metadata    []byte
	hadMetadata bool
	// paths 是受影响的全部来源文件路径；files 只含其中原本存在的。
	paths []string
	files map[string][]byte
}

func (s *Store) snapshot(ids []string) (snapshot, error) {
	result := snapshot{files: map[string][]byte{}}
	content, err := os.ReadFile(filepath.Join(s.dir, metadataFile))
	if err == nil {
		result.metadata, result.hadMetadata = content, true
	} else if !errors.Is(err, fs.ErrNotExist) {
		return snapshot{}, err
	}
	for _, id := range ids {
		for _, extension := range sourceExtensions {
			path := filepath.Join(s.SourcesDir(), id+extension)
			result.paths = append(result.paths, path)
			content, err := os.ReadFile(path)
			if err == nil {
				result.files[path] = content
			} else if !errors.Is(err, fs.ErrNotExist) {
				return snapshot{}, err
			}
		}
	}
	return result, nil
}

// restore 需要在持有 s.mu 时调用。
func (snap snapshot) restore(s *Store) error {
	var failures []error
	for _, path := range snap.paths {
		if content, ok := snap.files[path]; ok {
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				failures = append(failures, err)
				continue
			}
			failures = append(failures, writeFileAtomic(path, content))
		} else {
			failures = append(failures, removeIfExists(path))
		}
	}
	metadataPath := filepath.Join(s.dir, metadataFile)
	if snap.hadMetadata {
		failures = append(failures, writeFileAtomic(metadataPath, snap.metadata))
	} else {
		failures = append(failures, removeIfExists(metadataPath))
	}
	return errors.Join(failures...)
}

func (s *Store) load() (metadata, error) {
	content, err := os.ReadFile(filepath.Join(s.dir, metadataFile))
	if errors.Is(err, fs.ErrNotExist) {
		return metadata{Schema: metadataSchema, Packages: []Package{}}, nil
	}
	if err != nil {
		return metadata{}, err
	}
	var meta metadata
	if err := json.Unmarshal(content, &meta); err != nil {
		return metadata{}, fmt.Errorf("已安装来源记录损坏: %w", err)
	}
	if meta.Schema != metadataSchema {
		return metadata{}, fmt.Errorf("已安装来源记录版本 %q 不受支持", meta.Schema)
	}
	if meta.Packages == nil {
		meta.Packages = []Package{}
	}
	for index := range meta.Packages {
		if meta.Packages[index].Sources == nil {
			meta.Packages[index].Sources = []PackageSource{}
		}
	}
	return meta, nil
}

func (s *Store) save(meta metadata) error {
	meta.Schema = metadataSchema
	sort.Slice(meta.Packages, func(i, j int) bool { return meta.Packages[i].ID < meta.Packages[j].ID })
	content, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return err
	}
	return writeFileAtomic(filepath.Join(s.dir, metadataFile), append(content, '\n'))
}

func find(packages []Package, id string) (Package, bool) {
	for _, pkg := range packages {
		if pkg.ID == id {
			return pkg, true
		}
	}
	return Package{}, false
}

func without(packages []Package, id string) []Package {
	result := make([]Package, 0, len(packages))
	for _, pkg := range packages {
		if pkg.ID != id {
			result = append(result, pkg)
		}
	}
	return result
}

func writeFileAtomic(path string, content []byte) error {
	temporary, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	name := temporary.Name()
	cleanup := func() { _ = os.Remove(name) }
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		cleanup()
		return err
	}
	if _, err := temporary.Write(content); err != nil {
		temporary.Close()
		cleanup()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		cleanup()
		return err
	}
	if err := temporary.Close(); err != nil {
		cleanup()
		return err
	}
	if err := os.Rename(name, path); err != nil {
		cleanup()
		return err
	}
	return nil
}

func removeIfExists(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

// validID 与 Source Spec 的 id 规则一致：^[a-z0-9][a-z0-9-]{1,62}$。
func validID(id string) bool {
	if len(id) < 2 || len(id) > 63 || !((id[0] >= 'a' && id[0] <= 'z') || (id[0] >= '0' && id[0] <= '9')) {
		return false
	}
	for _, char := range id {
		if (char < 'a' || char > 'z') && (char < '0' || char > '9') && char != '-' {
			return false
		}
	}
	return true
}
