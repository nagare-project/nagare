package api

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	errs "github.com/nagare-project/nagare/internal/errors"
)

func TestPlayerConfigAnime4K(t *testing.T) {
	env := newEnv(t)
	rec := env.do(t, http.MethodGet, "/api/settings", "")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"player":{"anime4k":"off"}`)

	rec = env.do(t, http.MethodPost, "/api/player/config", `{"anime4k":"ultra"}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)

	env.player.playing = true
	rec = env.do(t, http.MethodPost, "/api/player/config", `{"anime4k":"hq"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, "hq", env.store.PlayerConfig().Anime4K)
	assert.Contains(t, rec.Body.String(), `"applied":true`)
	assert.Equal(t, 1, env.player.refreshes, "改了就套到正在播的窗口上")

	env.player.playing = false
	env.player.refreshErr = errs.New(errs.CategoryPlayback, "player.shaders", "没能把画质增强套到正在播放的窗口上", "下次开始播放时会按新设置加载")
	rec = env.do(t, http.MethodPost, "/api/player/config", `{"anime4k":"off"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Empty(t, env.store.PlayerConfig().Anime4K, "off 存成空串")
	assert.Contains(t, rec.Body.String(), `"anime4k":"off"`)
	assert.Contains(t, rec.Body.String(), "下次开始播放时会按新设置加载", "设置存下了，没套上要说出来")
}
