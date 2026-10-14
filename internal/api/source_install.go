package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	errs "github.com/nagare-project/nagare/internal/errors"
	"github.com/nagare-project/nagare/internal/httpserver"
	"github.com/nagare-project/nagare/internal/sourceinstall"
	"github.com/nagare-project/nagare/internal/sourceplugin"
	"github.com/nagare-project/nagare/internal/store"
)

// SourceFetcher 下载来源文件；生产实现是 sourceinstall.Fetcher，测试可替换。
type SourceFetcher interface {
	Fetch(ctx context.Context, rawURL string) (sourceinstall.Download, error)
}

// SetSourceInstaller 打开「从地址安装来源」。须在 StartConfigured 之前调用，
// 插件启动时才会拿到安装目录。
func (s *SourcePluginService) SetSourceInstaller(installs *sourceinstall.Store, fetcher SourceFetcher) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.installs, s.fetcher = installs, fetcher
}

// SourcePackageInfo 是一次安装的整体信息：来源包取自包头，单个来源取自插件给出的摘要。
type SourcePackageInfo struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Version     string `json:"version"`
	Pack        bool   `json:"pack"`
}

// SourcePreviewMember 是将要安装的一个来源。
type SourcePreviewMember struct {
	Source sourceplugin.SourceSummary `json:"source"`
	// Existing 是插件当前提供的同 id 来源（可能来自插件仓库）；安装后以新文件为准。
	Existing *sourceplugin.Source `json:"existing,omitempty"`
}

// SourceConflict 表示某个来源已经由另一个安装包装上，需要先卸载那个安装包。
type SourceConflict struct {
	SourceID    string `json:"sourceId"`
	PackageID   string `json:"packageId"`
	PackageName string `json:"packageName"`
}

// SourcePreview 是安装前给用户确认的内容：出处、包信息、每个来源的摘要与原文。
type SourcePreview struct {
	URL     string                `json:"url"`
	SHA256  string                `json:"sha256"`
	Bytes   int                   `json:"bytes"`
	Format  string                `json:"format"`
	Content string                `json:"content"`
	Package SourcePackageInfo     `json:"package"`
	Sources []SourcePreviewMember `json:"sources"`
	// Installed 是同 id 的已安装记录；安装会更新它。
	Installed *sourceinstall.Package `json:"installed,omitempty"`
	// Conflicts 非空时不能安装。
	Conflicts []SourceConflict `json:"conflicts"`
}

// sourceUnavailableError 表示功能当前不可用（插件未启用、未就绪或版本过旧），对应 HTTP 503。
type sourceUnavailableError struct{ message string }

func (e *sourceUnavailableError) Error() string { return e.message }

const (
	inspectTimeout     = 10 * time.Second
	verifySourcesAfter = 10 * time.Second
)

type inspectedMember struct {
	member  sourceinstall.BundleMember
	summary sourceplugin.SourceSummary
}

// inspectedBundle 是下载并逐个通过插件校验后的安装内容。
type inspectedBundle struct {
	download sourceinstall.Download
	info     SourcePackageInfo
	members  []inspectedMember
}

func (b inspectedBundle) record() (sourceinstall.Package, map[string][]byte) {
	pkg := sourceinstall.Package{
		ID: b.info.ID, Name: b.info.Name, Description: b.info.Description, Version: b.info.Version, Pack: b.info.Pack,
		URL: b.download.URL, SHA256: b.download.SHA256, InstalledAt: time.Now().UTC(),
		Sources: make([]sourceinstall.PackageSource, 0, len(b.members)),
	}
	files := make(map[string][]byte, len(b.members))
	for _, item := range b.members {
		pkg.Sources = append(pkg.Sources, sourceinstall.PackageSource{
			ID: item.summary.ID, Name: item.summary.Name, Kind: item.summary.Kind,
			Version: item.summary.Version, Format: item.member.Format,
		})
		files[item.summary.ID] = item.member.Content
	}
	return pkg, files
}

func (b inspectedBundle) sourceIDs() []string {
	ids := make([]string, 0, len(b.members))
	for _, item := range b.members {
		ids = append(ids, item.summary.ID)
	}
	return ids
}

// PreviewSource 下载并让插件逐个校验来源，不写盘。expectedSHA 为空时不比对。
func (s *SourcePluginService) PreviewSource(ctx context.Context, rawURL, expectedSHA string) (SourcePreview, error) {
	bundle, err := s.fetchAndInspect(ctx, rawURL, expectedSHA)
	if err != nil {
		return SourcePreview{}, err
	}
	download := bundle.download
	preview := SourcePreview{
		URL: download.URL, SHA256: download.SHA256, Bytes: len(download.Content), Format: download.Format,
		Content: string(download.Content), Package: bundle.info,
		Sources: make([]SourcePreviewMember, 0, len(bundle.members)), Conflicts: []SourceConflict{},
	}
	s.mu.Lock()
	installs := s.installs
	s.mu.Unlock()
	if installed, ok, err := installs.Get(bundle.info.ID); err == nil && ok {
		preview.Installed = &installed
	}
	pkg, _ := bundle.record()
	if conflicts, err := installs.Conflicts(pkg); err == nil {
		for _, conflict := range conflicts {
			preview.Conflicts = append(preview.Conflicts, SourceConflict{
				SourceID: conflict.SourceID, PackageID: conflict.PackageID, PackageName: conflict.PackageName,
			})
		}
	}
	existing := map[string]sourceplugin.Source{}
	listContext, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if sources, err := s.runtime.Sources(listContext); err == nil {
		for _, source := range sources {
			existing[source.ID] = source
		}
	}
	for _, item := range bundle.members {
		member := SourcePreviewMember{Source: item.summary}
		if source, ok := existing[item.summary.ID]; ok {
			member.Existing = &source
		}
		preview.Sources = append(preview.Sources, member)
	}
	return preview, nil
}

// InstallSource 重新下载并校验（摘要必须与用户确认的一致），保存后重启插件。
// 插件重启失败或任何一个新来源没有加载出来时撤销整次安装并恢复原插件。
func (s *SourcePluginService) InstallSource(ctx context.Context, rawURL, expectedSHA string) (SourcePluginView, error) {
	if _, ok := sourceinstall.NormalizeSHA256(expectedSHA); !ok {
		return SourcePluginView{}, errs.New(errs.CategoryInput, "sourceinstall.install",
			"安装请求缺少有效的 sha256 摘要", "回到安装页重新预览后再确认")
	}
	bundle, err := s.fetchAndInspect(ctx, rawURL, expectedSHA)
	if err != nil {
		return SourcePluginView{}, err
	}
	for _, item := range bundle.members {
		if !item.summary.Enabled {
			return SourcePluginView{}, errs.New(errs.CategoryInput, "sourceinstall.install",
				"来源 "+item.summary.ID+" 在文件里标记为停用，不能安装", "联系来源作者确认")
		}
	}

	s.mu.Lock()
	config := s.store.SourcePluginConfig()
	if !config.Enabled {
		s.mu.Unlock()
		return SourcePluginView{}, &sourceUnavailableError{"来源插件未启用，无法安装来源"}
	}
	pkg, files := bundle.record()
	undo, err := s.installs.Put(pkg, files)
	if err != nil {
		s.mu.Unlock()
		var conflict *sourceinstall.ConflictError
		if errors.As(err, &conflict) {
			return SourcePluginView{}, errs.Wrap(errs.CategoryInput, "sourceinstall.install",
				conflict.Error(), "在设置里卸载「"+conflict.PackageName+"」后再安装", err)
		}
		return SourcePluginView{}, errs.Wrap(errs.CategoryStorage, "sourceinstall.install",
			"保存来源文件失败", "检查配置目录是否可写", err)
	}
	if err := s.restartWith(ctx, config, bundle.sourceIDs(), true); err != nil {
		_ = undo()
		_ = s.runtime.Start(s.launchConfig(config))
		s.mu.Unlock()
		return SourcePluginView{}, errs.Wrap(errs.CategoryUpstream, "sourceinstall.install",
			"来源插件没能加载这次安装的来源，已撤销安装", "查看插件日志，或联系来源作者", err)
	}
	s.mu.Unlock()
	return s.View(ctx), nil
}

// UninstallSource 删除一个安装包（单个来源或来源包）装上的全部来源；插件启用时重启使其生效。
func (s *SourcePluginService) UninstallSource(ctx context.Context, id string) (SourcePluginView, error) {
	s.mu.Lock()
	if s.installs == nil {
		s.mu.Unlock()
		return SourcePluginView{}, &sourceUnavailableError{"这个版本没有开启来源安装"}
	}
	if !sourceplugin.ValidSourceID(id) {
		s.mu.Unlock()
		return SourcePluginView{}, errs.New(errs.CategoryInput, "sourceinstall.uninstall", "安装包 ID 不合法", "")
	}
	undo, err := s.installs.Remove(id)
	if errors.Is(err, os.ErrNotExist) {
		s.mu.Unlock()
		return SourcePluginView{}, errs.New(errs.CategoryFS, "sourceinstall.uninstall", "没有找到这个已安装的来源", "刷新设置页")
	}
	if err != nil {
		s.mu.Unlock()
		return SourcePluginView{}, errs.Wrap(errs.CategoryStorage, "sourceinstall.uninstall",
			"删除来源文件失败", "检查配置目录是否可写", err)
	}
	if config := s.store.SourcePluginConfig(); config.Enabled {
		if err := s.restartWith(ctx, config, nil, false); err != nil {
			_ = undo()
			_ = s.runtime.Start(s.launchConfig(config))
			s.mu.Unlock()
			return SourcePluginView{}, errs.Wrap(errs.CategoryUpstream, "sourceinstall.uninstall",
				"卸载后来源插件没能重新启动，已恢复这些来源", "检查插件状态后重试", err)
		}
	}
	s.mu.Unlock()
	return s.View(ctx), nil
}

// restartWith 重启插件；wantLoaded 时还确认插件确实加载了 ids 里的每个来源。调用方持有 s.mu。
func (s *SourcePluginService) restartWith(ctx context.Context, config store.SourcePluginConfig, ids []string, wantLoaded bool) error {
	if err := s.runtime.Start(s.launchConfig(config)); err != nil {
		return err
	}
	if !wantLoaded {
		return nil
	}
	listContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), verifySourcesAfter)
	defer cancel()
	sources, err := s.runtime.Sources(listContext)
	if err != nil {
		return err
	}
	loaded := make(map[string]bool, len(sources))
	for _, source := range sources {
		loaded[source.ID] = true
	}
	var missing []string
	for _, id := range ids {
		if !loaded[id] {
			missing = append(missing, id)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("installed sources were not loaded by the plugin: %s", strings.Join(missing, ", "))
	}
	return nil
}

func (s *SourcePluginService) fetchAndInspect(ctx context.Context, rawURL, expectedSHA string) (inspectedBundle, error) {
	s.mu.Lock()
	installs, fetcher := s.installs, s.fetcher
	s.mu.Unlock()
	if installs == nil || fetcher == nil {
		return inspectedBundle{}, &sourceUnavailableError{"这个版本没有开启来源安装"}
	}
	normalized, err := sourceinstall.ValidateURL(rawURL)
	if err != nil {
		return inspectedBundle{}, errs.Wrap(errs.CategoryInput, "sourceinstall.url", err.Error(), "", err)
	}
	want := ""
	if expectedSHA != "" {
		var ok bool
		if want, ok = sourceinstall.NormalizeSHA256(expectedSHA); !ok {
			return inspectedBundle{}, errs.New(errs.CategoryInput,
				"sourceinstall.sha256", "安装链接里的 sha256 摘要格式不对", "应为 64 位十六进制")
		}
	}
	status := s.runtime.Status()
	if status.Phase != "ready" {
		return inspectedBundle{}, &sourceUnavailableError{"来源插件尚未就绪，请先在设置里启用来源插件"}
	}
	if !supportsInstalledSources(status) {
		return inspectedBundle{}, &sourceUnavailableError{"当前来源插件版本过旧，不支持安装来源，请更新来源插件"}
	}
	download, err := fetcher.Fetch(ctx, normalized)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return inspectedBundle{}, err
		}
		return inspectedBundle{}, errs.Wrap(errs.CategoryNetwork,
			"sourceinstall.fetch", err.Error(), "确认地址可以公开访问后重试", err)
	}
	if want != "" && download.SHA256 != want {
		return inspectedBundle{}, errs.New(errs.CategoryInput,
			"sourceinstall.sha256", "下载到的文件与安装链接的摘要不符",
			"文件可能已被更新或替换；请回到发布页获取最新的安装链接")
	}
	parsed, err := sourceinstall.ParseBundle(download)
	if err != nil {
		return inspectedBundle{}, errs.Wrap(errs.CategoryInput, "sourceinstall.pack", err.Error(), "联系来源包作者修正", err)
	}
	result := inspectedBundle{
		download: download,
		info: SourcePackageInfo{
			ID: parsed.ID, Name: parsed.Name, Description: parsed.Description, Version: parsed.Version, Pack: parsed.Pack,
		},
		members: make([]inspectedMember, 0, len(parsed.Members)),
	}
	seen := make(map[string]bool, len(parsed.Members))
	for index, member := range parsed.Members {
		summary, err := s.inspectMember(ctx, member)
		if err != nil {
			var rejected *sourceplugin.InspectError
			if errors.As(err, &rejected) {
				message := "来源文件没有通过插件校验：" + rejected.Message
				if parsed.Pack {
					message = fmt.Sprintf("来源包第 %d 个来源没有通过插件校验：%s", index+1, rejected.Message)
				}
				return inspectedBundle{}, errs.Wrap(errs.CategoryInput, "sourceinstall.inspect", message, "联系来源作者修正", err)
			}
			if errors.Is(err, context.Canceled) {
				return inspectedBundle{}, err
			}
			return inspectedBundle{}, errs.Wrap(errs.CategoryUpstream,
				"sourceinstall.inspect", "来源插件校验失败", "检查插件状态后重试", err)
		}
		if !sourceplugin.ValidSourceID(summary.ID) {
			return inspectedBundle{}, errs.New(errs.CategoryInput, "sourceinstall.inspect",
				fmt.Sprintf("来源 ID %q 不合法", summary.ID), "联系来源作者修正")
		}
		if seen[summary.ID] {
			return inspectedBundle{}, errs.New(errs.CategoryInput, "sourceinstall.pack",
				"来源包里有重复的来源 "+summary.ID, "联系来源包作者修正")
		}
		seen[summary.ID] = true
		result.members = append(result.members, inspectedMember{member: member, summary: summary})
	}
	if !parsed.Pack {
		summary := result.members[0].summary
		result.info = SourcePackageInfo{ID: summary.ID, Name: summary.Name, Description: summary.Description, Version: summary.Version}
	}
	return result, nil
}

func (s *SourcePluginService) inspectMember(ctx context.Context, member sourceinstall.BundleMember) (sourceplugin.SourceSummary, error) {
	inspectContext, cancel := context.WithTimeout(ctx, inspectTimeout)
	defer cancel()
	return s.runtime.InspectSource(inspectContext, member.Format, string(member.Content))
}

func supportsInstalledSources(status sourceplugin.Status) bool {
	if status.Phase != "ready" || status.Manifest == nil {
		return false
	}
	for _, capability := range status.Manifest.Capabilities {
		if capability == sourceplugin.CapabilityInstalledSources {
			return true
		}
	}
	return false
}

type sourceInstallRequest struct {
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
}

func (h *Handler) sourcePluginPreview(w http.ResponseWriter, r *http.Request) {
	if h.deps.SourcePlugin == nil {
		httpserver.WriteError(w, http.StatusServiceUnavailable, "来源插件服务未启用")
		return
	}
	var request sourceInstallRequest
	if !decodeStrictBody(w, r, &request) {
		return
	}
	preview, err := h.deps.SourcePlugin.PreviewSource(r.Context(), request.URL, request.SHA256)
	if err != nil {
		writeSourceInstallErr(w, err)
		return
	}
	httpserver.WriteJSON(w, http.StatusOK, preview)
}

func (h *Handler) sourcePluginInstall(w http.ResponseWriter, r *http.Request) {
	if h.deps.SourcePlugin == nil {
		httpserver.WriteError(w, http.StatusServiceUnavailable, "来源插件服务未启用")
		return
	}
	var request sourceInstallRequest
	if !decodeStrictBody(w, r, &request) {
		return
	}
	view, err := h.deps.SourcePlugin.InstallSource(r.Context(), request.URL, request.SHA256)
	if err != nil {
		writeSourceInstallErr(w, err)
		return
	}
	httpserver.WriteJSON(w, http.StatusOK, view)
}

func (h *Handler) sourcePluginUninstall(w http.ResponseWriter, r *http.Request) {
	if h.deps.SourcePlugin == nil {
		httpserver.WriteError(w, http.StatusServiceUnavailable, "来源插件服务未启用")
		return
	}
	view, err := h.deps.SourcePlugin.UninstallSource(r.Context(), r.PathValue("id"))
	if err != nil {
		writeSourceInstallErr(w, err)
		return
	}
	httpserver.WriteJSON(w, http.StatusOK, view)
}

func writeSourceInstallErr(w http.ResponseWriter, err error) {
	var unavailable *sourceUnavailableError
	if errors.As(err, &unavailable) {
		httpserver.WriteError(w, http.StatusServiceUnavailable, unavailable.message)
		return
	}
	writeErr(w, err)
}
