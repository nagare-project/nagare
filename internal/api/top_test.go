package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nagare-project/nagare/internal/animego"
)

type topStub struct {
	catalogStub
	lastQuery animego.TopQuery
	topCalls  int
	yearCalls int
}

func (s *topStub) Top(_ context.Context, q animego.TopQuery) ([]animego.CatalogMedia, error) {
	s.topCalls++
	s.lastQuery = q
	seq := true
	return []animego.CatalogMedia{
		{AnilistID: 5114, TitleChinese: "钢之炼金术师", PosterAccent: "#d88852", AverageScore: 91, BangumiScore: 8.8, Popularity: 100000, Favourites: 25000, IsSequel: &seq, Studio: "BONES", Format: "TV", Season: "SPRING", SeasonYear: 2009, Rank: 1},
		{AnilistID: 154587, TitleChinese: "葬送的芙莉莲", AverageScore: 90, Format: "TV", Season: "FALL", SeasonYear: 2023},
	}, nil
}

func (s *topStub) TopYears(context.Context) ([]animego.TopYearCount, error) {
	s.yearCalls++
	return []animego.TopYearCount{{Year: 2026, Count: 348}, {Year: 2025, Count: 339}}, nil
}

func TestTopEndpointsValidateAndCache(t *testing.T) {
	stub := &topStub{catalogStub: catalogStub{calls: map[string]int{}}}
	h := New(Deps{Catalog: stub, RemoteArt: NewRemoteArt(animego.DefaultBaseURL, 64)})
	mux := http.NewServeMux()
	h.Register(mux)

	// 参数校验
	for _, bad := range []string{"/api/top?year=abc", "/api/top?year=1800", "/api/top?format=MUSIC", "/api/top?season=SPRING"} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, bad, nil))
		assert.Equal(t, http.StatusBadRequest, rec.Code, bad)
	}

	// 正常查询与缓存命中
	req := "/api/top?year=2025&format=tv&season=fall&genre=Action"
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, req, nil))
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, animego.TopQuery{Year: 2025, Format: "TV", Season: "FALL", Genre: "Action"}, stub.lastQuery)

	var env struct {
		Success bool    `json:"success"`
		Data    TopView `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
	require.True(t, env.Success)
	require.Len(t, env.Data.Items, 2)
	assert.Equal(t, 1, env.Data.Items[0].Rank)
	assert.Equal(t, 2, env.Data.Items[1].Rank)
	assert.NotNil(t, env.Data.Items[0].Hue)
	assert.Equal(t, []string{"BONES"}, env.Data.Items[0].Studios)
	assert.Equal(t, 100000, env.Data.Items[0].Popularity)
	require.NotNil(t, env.Data.Items[0].Sequel)
	assert.True(t, *env.Data.Items[0].Sequel)

	// 第二次同样参数命中缓存
	rec2 := httptest.NewRecorder()
	mux.ServeHTTP(rec2, httptest.NewRequest(http.MethodGet, req, nil))
	require.Equal(t, http.StatusOK, rec2.Code)
	assert.Equal(t, 1, stub.topCalls)

	// 年份接口
	yRec := httptest.NewRecorder()
	mux.ServeHTTP(yRec, httptest.NewRequest(http.MethodGet, "/api/top/years", nil))
	require.Equal(t, http.StatusOK, yRec.Code)
	var yEnv struct {
		Data TopYearsView `json:"data"`
	}
	require.NoError(t, json.Unmarshal(yRec.Body.Bytes(), &yEnv))
	require.Len(t, yEnv.Data.Years, 2)
	assert.Equal(t, 2026, yEnv.Data.Years[0].Year)
	assert.Equal(t, 1, stub.yearCalls)
}
