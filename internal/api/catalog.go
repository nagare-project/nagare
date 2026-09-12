package api

import (
	"github.com/nagare-project/nagare/internal/httpserver"
	"net/http"
	"strconv"
)

func (h *Handler) SetCatalogArtPrefix(prefix string) { h.remoteArt.SetPrefix(prefix) }
func (h *Handler) RemoteArtwork() RemoteArtSource    { return h.remoteArt }
func (h *Handler) registerCatalog(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/discover", h.catalogDiscover)
	mux.HandleFunc("GET /api/seasonal", h.catalogSeasonal)
	mux.HandleFunc("GET /api/anime/{id}", h.catalogDetail)
	mux.HandleFunc("GET /api/schedule", h.catalogSchedule)
	mux.HandleFunc("GET /api/lists", h.catalogList)
	mux.HandleFunc("POST /api/lists/{id}", h.catalogListSave)
	mux.HandleFunc("DELETE /api/lists/{id}", h.catalogListDelete)
}
func catalogID(w http.ResponseWriter, r *http.Request) (int, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 32)
	if err != nil || id < 1 {
		httpserver.WriteError(w, 400, "无效的作品 ID")
		return 0, false
	}
	return int(id), true
}
func (h *Handler) requireCatalog(w http.ResponseWriter) bool {
	if h.deps.Catalog == nil {
		httpserver.WriteError(w, 503, "作品目录暂不可用")
		return false
	}
	return true
}
func (h *Handler) catalogDiscover(w http.ResponseWriter, r *http.Request) {
	if !h.requireCatalog(w) {
		return
	}
	v, err := h.catalog.View(r.Context())
	if err != nil {
		writeErr(w, err)
		return
	}
	httpserver.WriteJSON(w, 200, v)
}
func (h *Handler) catalogDetail(w http.ResponseWriter, r *http.Request) {
	id, ok := catalogID(w, r)
	if !ok || !h.requireCatalog(w) {
		return
	}
	v, err := h.catalog.Detail(r.Context(), id)
	if err != nil {
		writeErr(w, err)
		return
	}
	httpserver.WriteJSON(w, 200, v)
}
func (h *Handler) catalogSchedule(w http.ResponseWriter, r *http.Request) {
	if !h.requireCatalog(w) {
		return
	}
	known := map[int]bool{}
	if h.deps.Store != nil {
		for _, b := range h.deps.Store.Snapshot().Bindings {
			if b.AnilistID > 0 {
				known[b.AnilistID] = true
			}
		}
	}
	v, err := h.catalog.ScheduleView(r.Context(), func(id int) bool { return known[id] })
	if err != nil {
		writeErr(w, err)
		return
	}
	httpserver.WriteJSON(w, 200, v)
}
func (h *Handler) catalogList(w http.ResponseWriter, r *http.Request) {
	v, err := h.lists.View(r.Context())
	if err != nil {
		writeErr(w, err)
		return
	}
	httpserver.WriteJSON(w, 200, v)
}
func (h *Handler) catalogListSave(w http.ResponseWriter, r *http.Request) {
	id, ok := catalogID(w, r)
	if !ok {
		return
	}
	var input struct {
		Status         string `json:"status"`
		CurrentEpisode int    `json:"currentEpisode"`
		Score          *int   `json:"score"`
	}
	if !decodeBody(w, r, &input) {
		return
	}
	v, err := h.lists.mutate(r.Context(), id, input.Status, input.CurrentEpisode, input.Score, false)
	if err != nil {
		writeErr(w, err)
		return
	}
	httpserver.WriteJSON(w, 200, v)
}
func (h *Handler) catalogListDelete(w http.ResponseWriter, r *http.Request) {
	id, ok := catalogID(w, r)
	if !ok {
		return
	}
	if _, err := h.lists.mutate(r.Context(), id, "", 0, nil, true); err != nil {
		writeErr(w, err)
		return
	}
	httpserver.WriteJSON(w, 200, map[string]bool{"deleted": true})
}
