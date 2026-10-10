package api

import (
	"errors"
	"net/http"
	"strings"

	errs "github.com/nagare-project/nagare/internal/errors"

	"github.com/nagare-project/nagare/internal/httpserver"
	"github.com/nagare-project/nagare/internal/mpv"
	"github.com/nagare-project/nagare/internal/store"
)

// playerView 是设置页看的播放器设置。anime4k：off / fast / hq（存的时候 off 是空串）。
func playerView(c store.PlayerConfig) map[string]any {
	preset := c.Anime4K
	if preset == mpv.Anime4KOff {
		preset = "off"
	}
	return map[string]any{"anime4k": preset}
}

// playerConfig 保存播放器设置，并立刻套到正在播放的窗口上（applied 说明是否在播）。
func (h *Handler) playerConfig(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Anime4K *string `json:"anime4k"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	if req.Anime4K != nil {
		preset := strings.TrimSpace(*req.Anime4K)
		if preset == "off" {
			preset = mpv.Anime4KOff
		}
		if !mpv.ValidAnime4K(preset) {
			httpserver.WriteError(w, http.StatusBadRequest, "画质增强只能是 off / fast / hq")
			return
		}
		*req.Anime4K = preset
	}
	cfg, err := h.deps.Store.UpdatePlayerConfig(func(c *store.PlayerConfig) {
		if req.Anime4K != nil {
			c.Anime4K = *req.Anime4K
		}
	})
	if err != nil {
		writeErr(w, err)
		return
	}
	view := playerView(cfg)
	applied, err := h.deps.Player.RefreshShaders()
	view["applied"] = applied
	if err != nil {
		// 设置已经存下了，只是这一个窗口没换成功：如实说，下次起播会按新设置来
		message := "没能把画质增强套到正在播放的窗口上，下次开始播放时会按新设置加载"
		var ce *errs.E
		if errors.As(err, &ce) {
			message = ce.UserFacing()
		}
		view["applyError"] = message
	}
	httpserver.WriteJSON(w, http.StatusOK, view)
}
