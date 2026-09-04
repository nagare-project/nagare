package api

import (
	"crypto/sha256"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/nagare-project/nagare/internal/animego"
	"github.com/nagare-project/nagare/internal/httpserver"
	"github.com/nagare-project/nagare/internal/store"
)

func (h *Handler) SetCatalogArtPrefix(prefix string)            { h.catalog.SetArtPrefix(prefix) }
func (h *Handler) CatalogImageSource(key string) (string, bool) { return h.catalog.ImageSource(key) }
func (h *Handler) registerCatalog(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/catalog/discover", h.catalogDiscover)
	mux.HandleFunc("GET /api/catalog/anime/{id}", h.catalogDetail)
	mux.HandleFunc("GET /api/catalog/list", h.catalogList)
	mux.HandleFunc("POST /api/catalog/list/{id}", h.catalogListSave)
	mux.HandleFunc("DELETE /api/catalog/list/{id}", h.catalogListDelete)
}
func catalogID(w http.ResponseWriter, r *http.Request) (int, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 32)
	if err != nil || id < 1 {
		httpserver.WriteError(w, 400, "无效的作品 ID")
		return 0, false
	}
	return int(id), true
}
func (h *Handler) catalogDiscover(w http.ResponseWriter, r *http.Request) {
	genre := r.URL.Query().Get("genre")
	if len(genre) > 40 {
		httpserver.WriteError(w, 400, "无效的类型")
		return
	}
	data, err := h.catalog.Discover(r.Context(), r.URL.Query().Get("section"), genre, time.Now())
	if err != nil {
		writeErr(w, err)
		return
	}
	httpserver.WriteJSON(w, http.StatusOK, data)
}
func (h *Handler) catalogDetail(w http.ResponseWriter, r *http.Request) {
	id, ok := catalogID(w, r)
	if !ok {
		return
	}
	data, err := h.catalog.Detail(r.Context(), id)
	if err != nil {
		writeErr(w, err)
		return
	}
	httpserver.WriteJSON(w, http.StatusOK, data)
}

type catalogListItem struct {
	animego.ListEntry
	store.ListNote
}

func (h *Handler) listKey(id int) string {
	return fmt.Sprintf("%x:%d", sha256.Sum256([]byte(h.deps.Store.AnimegoSession().Email)), id)
}
func (h *Handler) catalogList(w http.ResponseWriter, r *http.Request) {
	if h.deps.Lists == nil || !h.deps.Lists.LoggedIn() {
		httpserver.WriteJSON(w, http.StatusOK, map[string]any{"loggedIn": false, "entries": []catalogListItem{}})
		return
	}
	entries, err := h.deps.Lists.ListEntries(r.Context())
	if err != nil {
		writeErr(w, err)
		return
	}
	result := make([]catalogListItem, 0, len(entries))
	for _, e := range entries {
		e.CoverImageURL = h.catalog.Artwork(e.CoverImageURL)
		e.BannerImageURL = h.catalog.Artwork(e.BannerImageURL)
		result = append(result, catalogListItem{e, h.deps.Store.ListNote(h.listKey(e.AnilistID))})
	}
	httpserver.WriteJSON(w, http.StatusOK, map[string]any{"loggedIn": true, "entries": result})
}
func (h *Handler) requireListAccount(w http.ResponseWriter) bool {
	if h.deps.Lists == nil || !h.deps.Lists.LoggedIn() {
		httpserver.WriteError(w, http.StatusUnauthorized, "请先在设置中登录账号")
		return false
	}
	return true
}
func (h *Handler) catalogListSave(w http.ResponseWriter, r *http.Request) {
	id, ok := catalogID(w, r)
	if !ok || !h.requireListAccount(w) {
		return
	}
	var input struct {
		Status   string `json:"status"`
		Progress int    `json:"progress"`
		Score    *int   `json:"score"`
		store.ListNote
	}
	if !decodeBody(w, r, &input) {
		return
	}
	for _, date := range []string{input.StartedAt, input.CompletedAt} {
		if date != "" {
			if _, err := time.Parse("2006-01-02", date); err != nil {
				httpserver.WriteError(w, 400, "日期格式应为 YYYY-MM-DD")
				return
			}
		}
	}
	if input.Repeat < 0 || input.Repeat > 1000 {
		httpserver.WriteError(w, 400, "重看次数必须为 0–1000")
		return
	}
	status := input.Status
	input.Paused = status == "paused"
	if input.Paused {
		status = "watching"
	}
	key := h.listKey(id)
	if err := h.deps.Lists.SaveListEntry(r.Context(), id, status, input.Progress, input.Score); err != nil {
		writeErr(w, err)
		return
	}
	if err := h.deps.Store.SetListNote(key, &input.ListNote); err != nil {
		httpserver.WriteError(w, 500, "账号已保存，但本机日期与重看次数保存失败，请重试")
		return
	}
	httpserver.WriteJSON(w, http.StatusOK, map[string]bool{"saved": true})
}
func (h *Handler) catalogListDelete(w http.ResponseWriter, r *http.Request) {
	id, ok := catalogID(w, r)
	if !ok || !h.requireListAccount(w) {
		return
	}
	key := h.listKey(id)
	if err := h.deps.Lists.DeleteListEntry(r.Context(), id); err != nil {
		writeErr(w, err)
		return
	}
	if err := h.deps.Store.SetListNote(key, nil); err != nil {
		httpserver.WriteError(w, 500, "收藏已删除，本机备注清理失败，请重试")
		return
	}
	httpserver.WriteJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}
