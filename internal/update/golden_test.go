package update

import (
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/nagare-project/nagare/internal/apigolden"
)

// TestGolden 把更新检查端点的真实响应与 testdata/api/update-*.json 逐字节比对（-update 时重写），
// 见 docs/api-goldens.md。假 GitHub、固定时钟，不联网。
func TestGolden(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("黄金响应在 linux 上比对（与 internal/api 一致）")
	}
	quietLogs(t)
	call := func(t *testing.T, s *stub, method, target, body string) []byte {
		t.Helper()
		c := newChecker(t, s, newClock(), Options{CurrentVersion: "0.7.0"})
		mux := http.NewServeMux()
		c.Register(mux)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(method, target, strings.NewReader(body)))
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		return rec.Body.Bytes()
	}
	cases := []struct {
		name string
		run  func(t *testing.T) []byte
	}{
		{"update-status-unchecked", func(t *testing.T) []byte {
			return call(t, newStub(t, http.StatusOK, goodRelease("v0.8.0")), http.MethodGet, "/api/update", "")
		}},
		{"update-status-available", func(t *testing.T) []byte {
			return call(t, newStub(t, http.StatusOK, goodRelease("v0.8.0")), http.MethodPost, "/api/update/check", "")
		}},
		{"update-status-check-failed", func(t *testing.T) []byte {
			return call(t, newStub(t, http.StatusInternalServerError, `{"message":"boom"}`), http.MethodPost, "/api/update/check", "")
		}},
		{"update-config-disabled", func(t *testing.T) []byte {
			return call(t, newStub(t, http.StatusOK, goodRelease("v0.8.0")), http.MethodPost, "/api/update/config", `{"enabled":false}`)
		}},
	}
	names := make([]string, 0, len(cases))
	for _, c := range cases {
		names = append(names, c.name)
		t.Run(c.name, func(t *testing.T) { apigolden.Assert(t, c.name, c.run(t)) })
	}
	apigolden.AssertOwned(t, func(name string) bool { return strings.HasPrefix(name, apigolden.PrefixUpdate) }, names)
}
