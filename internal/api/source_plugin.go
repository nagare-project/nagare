package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	errs "github.com/nagare-project/nagare/internal/errors"
	"github.com/nagare-project/nagare/internal/httpserver"
	"github.com/nagare-project/nagare/internal/player"
	"github.com/nagare-project/nagare/internal/sourceplugin"
	"github.com/nagare-project/nagare/internal/store"
)

// SourcePluginRuntime 是服务需要的最小插件生命周期与查询接口。
type SourcePluginRuntime interface {
	sourceplugin.Runtime
	Start(sourceplugin.LaunchConfig) error
	Stop()
}

// SourcePluginService 把持久化配置、进程生命周期和 Plugin API 查询收在一起。
// 配置变更串行执行，启动失败会恢复上一份可用配置。
type SourcePluginService struct {
	store       *store.Store
	runtime     SourcePluginRuntime
	hostVersion string
	mu          sync.Mutex
}

type SourcePluginView struct {
	Config       store.SourcePluginConfig `json:"config"`
	Status       sourceplugin.Status      `json:"status"`
	Sources      []sourceplugin.Source    `json:"sources"`
	SourcesError string                   `json:"sourcesError,omitempty"`
}

func NewSourcePluginService(store *store.Store, runtime SourcePluginRuntime, hostVersion string) *SourcePluginService {
	return &SourcePluginService{store: store, runtime: runtime, hostVersion: hostVersion}
}

// StartConfigured 在应用启动时恢复用户上次启用的插件。失败只让插件降级，
// 不阻止本地媒体库和磁力播放启动。
func (s *SourcePluginService) StartConfigured() error {
	config := s.store.SourcePluginConfig()
	if !config.Enabled {
		return nil
	}
	return s.runtime.Start(s.launchConfig(config))
}

func (s *SourcePluginService) Stop() { s.runtime.Stop() }

func (s *SourcePluginService) Status() sourceplugin.Status { return s.runtime.Status() }

func (s *SourcePluginService) View(ctx context.Context) SourcePluginView {
	view := SourcePluginView{
		Config: s.store.SourcePluginConfig(), Status: s.runtime.Status(), Sources: []sourceplugin.Source{},
	}
	if view.Status.Phase != "ready" {
		return view
	}
	requestContext, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	sources, err := s.runtime.Sources(requestContext)
	if err != nil {
		view.SourcesError = "读取插件来源失败，请重启插件或检查其日志"
		return view
	}
	view.Sources = sources
	return view
}

func (s *SourcePluginService) Configure(enabled *bool, executable, root *string) (SourcePluginView, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	previous := s.store.SourcePluginConfig()
	next := previous
	if enabled != nil {
		next.Enabled = *enabled
	}
	if executable != nil {
		next.Executable = strings.TrimSpace(*executable)
	}
	if root != nil {
		next.Root = strings.TrimSpace(*root)
	}

	if next.Enabled {
		if err := s.runtime.Start(s.launchConfig(next)); err != nil {
			s.restore(previous)
			return SourcePluginView{}, errs.Wrap(errs.CategoryInput, "sourceplugin.configure",
				"来源插件启动失败", "检查可执行文件、仓库目录和插件版本后重试", err)
		}
	} else {
		s.runtime.Stop()
	}
	if err := s.store.SetSourcePluginConfig(next); err != nil {
		s.restore(previous)
		return SourcePluginView{}, err
	}
	return s.View(context.Background()), nil
}

func (s *SourcePluginService) restore(config store.SourcePluginConfig) {
	if config.Enabled {
		_ = s.runtime.Start(s.launchConfig(config))
	} else {
		s.runtime.Stop()
	}
}

func (s *SourcePluginService) launchConfig(config store.SourcePluginConfig) sourceplugin.LaunchConfig {
	return sourceplugin.LaunchConfig{Executable: config.Executable, Root: config.Root, Version: s.hostVersion}
}

func (s *SourcePluginService) Candidates(ctx context.Context, request sourceplugin.ResolveRequest, emit func(sourceplugin.Event) error) error {
	return s.runtime.Candidates(ctx, request, emit)
}

func (h *Handler) sourcePluginView(w http.ResponseWriter, r *http.Request) {
	if h.deps.SourcePlugin == nil {
		httpserver.WriteError(w, http.StatusServiceUnavailable, "来源插件服务未启用")
		return
	}
	httpserver.WriteJSON(w, http.StatusOK, h.deps.SourcePlugin.View(r.Context()))
}

func (h *Handler) sourcePluginConfig(w http.ResponseWriter, r *http.Request) {
	if h.deps.SourcePlugin == nil {
		httpserver.WriteError(w, http.StatusServiceUnavailable, "来源插件服务未启用")
		return
	}
	var request struct {
		Enabled    *bool   `json:"enabled"`
		Executable *string `json:"executable"`
		Root       *string `json:"root"`
	}
	if !decodeStrictBody(w, r, &request) {
		return
	}
	view, err := h.deps.SourcePlugin.Configure(request.Enabled, request.Executable, request.Root)
	if err != nil {
		writeErr(w, err)
		return
	}
	httpserver.WriteJSON(w, http.StatusOK, view)
}

func (h *Handler) sourcePluginCandidates(w http.ResponseWriter, r *http.Request) {
	if h.deps.SourcePlugin == nil || h.deps.SourcePlugin.Status().Phase != "ready" {
		httpserver.WriteError(w, http.StatusServiceUnavailable, "来源插件尚未就绪")
		return
	}
	var request sourceplugin.ResolveRequest
	if !decodeStrictBody(w, r, &request) {
		return
	}
	if request.Schema != "nagare-resolve-request/v1" || len(request.Subject.IDs) == 0 || len(request.Subject.Titles) == 0 {
		httpserver.WriteError(w, http.StatusBadRequest, "来源解析请求缺少作品标识或标题")
		return
	}
	episode, err := strconv.ParseFloat(request.Episode.Number, 64)
	if err != nil || episode <= 0 {
		httpserver.WriteError(w, http.StatusBadRequest, "集号必须大于零")
		return
	}
	w.Header().Set("Content-Type", "application/x-ndjson; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)
	encoder := json.NewEncoder(w)
	emitted := 0
	err = h.deps.SourcePlugin.Candidates(r.Context(), request, func(event sourceplugin.Event) error {
		if err := encoder.Encode(sourcePluginEventView(event)); err != nil {
			return err
		}
		emitted++
		if flusher != nil {
			flusher.Flush()
		}
		return nil
	})
	if err != nil && !errors.Is(err, context.Canceled) {
		_ = encoder.Encode(map[string]any{
			"event": "host_error", "message": "来源插件候选流中断，请重试或检查插件状态", "afterEvents": emitted,
		})
		if flusher != nil {
			flusher.Flush()
		}
	}
}

func (h *Handler) sourcePluginPlay(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Candidate sourceplugin.Candidate `json:"candidate"`
		Title     string                 `json:"title"`
		Episode   int                    `json:"episode"`
	}
	if !decodeStrictBody(w, r, &request) {
		return
	}
	if strings.TrimSpace(request.Title) == "" || request.Episode < 1 {
		httpserver.WriteError(w, http.StatusBadRequest, "标题和正集号不能为空")
		return
	}
	if err := sourceplugin.ValidateCandidate(request.Candidate); err != nil {
		httpserver.WriteError(w, http.StatusBadRequest, "候选格式无效")
		return
	}
	transport := request.Candidate.Transport
	if transport.Type != "hls" && transport.Type != "http" {
		httpserver.WriteError(w, http.StatusBadRequest, "该端点只播放 HTTP/HLS 候选")
		return
	}
	source := player.NewRemoteSource(player.RemoteSourceOptions{
		CandidateID: request.Candidate.ID,
		SourceID:    request.Candidate.SourceID,
		URL:         transport.URL,
		Headers:     transport.Headers,
		ExpiresAt:   transport.ExpiresAt,
		Title:       request.Title,
		Episode:     request.Episode,
		SizeBytes:   request.Candidate.Metadata.SizeBytes,
	})
	result, err := h.deps.Player.Play(r.Context(), source, "")
	if err != nil {
		writeErr(w, err)
		return
	}
	httpserver.WriteJSON(w, http.StatusOK, result)
}

func sourcePluginEventView(event sourceplugin.Event) any {
	switch event.Event {
	case "candidate":
		return struct {
			Event     string                  `json:"event"`
			Candidate *sourceplugin.Candidate `json:"candidate"`
		}{Event: event.Event, Candidate: event.Candidate}
	case "source_error":
		return struct {
			Event     string `json:"event"`
			SourceID  string `json:"sourceId"`
			Category  string `json:"category"`
			Message   string `json:"message"`
			Retryable bool   `json:"retryable"`
		}{Event: event.Event, SourceID: event.SourceID, Category: event.Category, Message: event.Message, Retryable: event.Retryable}
	default:
		return struct {
			Event      string `json:"event"`
			Queried    int    `json:"queried"`
			Succeeded  int    `json:"succeeded"`
			Failed     int    `json:"failed"`
			DurationMS int64  `json:"durationMs"`
		}{Event: event.Event, Queried: event.Queried, Succeeded: event.Succeeded, Failed: event.Failed, DurationMS: event.DurationMS}
	}
}

func decodeStrictBody(w http.ResponseWriter, r *http.Request, output any) bool {
	data, err := io.ReadAll(io.LimitReader(r.Body, maxBodyBytes+1))
	if err != nil || len(data) > maxBodyBytes {
		httpserver.WriteError(w, http.StatusBadRequest, "请求体读取失败或超过 1 MiB")
		return false
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(output); err != nil || decoder.Decode(&struct{}{}) != io.EOF {
		httpserver.WriteError(w, http.StatusBadRequest, "请求体不是严格的 JSON")
		return false
	}
	return true
}
