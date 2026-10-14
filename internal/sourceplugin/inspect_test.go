package sourceplugin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func validSummary() SourceSummary {
	return SourceSummary{
		ID: "demo", Name: "Demo", Kind: "bt", Tier: 2, Version: "1.0.0", Enabled: true,
		Ecosystem: "community", Upstream: "https://example.test/", License: "MIT",
		AllowedHosts: []string{"example.test", "*.cdn.example.test"},
	}
}

func inspectServer(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client, err := NewClient(server.URL)
	require.NoError(t, err)
	return client
}

func TestClientInspectSourceSendsDocumentAndDecodesSummary(t *testing.T) {
	var received map[string]string
	client := inspectServer(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/v1/sources/inspect", r.URL.Path)
		assert.Equal(t, "1", r.Header.Get("X-Nagare-Protocol-Version"))
		require.NoError(t, json.NewDecoder(r.Body).Decode(&received))
		writeTestJSON(w, map[string]any{"source": validSummary()})
	})

	summary, err := client.InspectSource(context.Background(), "yaml", "id: demo\n")
	require.NoError(t, err)
	assert.Equal(t, validSummary(), summary)
	assert.Equal(t, map[string]string{"format": "yaml", "content": "id: demo\n"}, received)

	_, err = client.InspectSource(context.Background(), "toml", "x")
	require.ErrorContains(t, err, "json or yaml")
}

func TestClientInspectSourceSurfacesPluginRejection(t *testing.T) {
	long := strings.Repeat("坏", 400)
	client := inspectServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"category": "invalid_source", "message": long}})
	})

	_, err := client.InspectSource(context.Background(), "json", `{"id":"x"}`)
	var inspectErr *InspectError
	require.True(t, errors.As(err, &inspectErr), "error = %v", err)
	assert.Equal(t, "invalid_source", inspectErr.Category)
	assert.LessOrEqual(t, len(inspectErr.Message), maximumInspectErrorBytes+len("…"))
	assert.True(t, strings.HasSuffix(inspectErr.Message, "…"))
}

func TestClientInspectSourceRejectsUntrustedResponses(t *testing.T) {
	cases := map[string]http.HandlerFunc{
		"unknown field": func(w http.ResponseWriter, _ *http.Request) {
			writeTestJSON(w, map[string]any{"source": validSummary(), "extra": true})
		},
		"missing source": func(w http.ResponseWriter, _ *http.Request) { writeTestJSON(w, map[string]any{}) },
		"bad kind": func(w http.ResponseWriter, _ *http.Request) {
			summary := validSummary()
			summary.Kind = "exec"
			writeTestJSON(w, map[string]any{"source": summary})
		},
		"bad id": func(w http.ResponseWriter, _ *http.Request) {
			summary := validSummary()
			summary.ID = "../etc"
			writeTestJSON(w, map[string]any{"source": summary})
		},
		"bad host": func(w http.ResponseWriter, _ *http.Request) {
			summary := validSummary()
			summary.AllowedHosts = []string{"evil.test/path"}
			writeTestJSON(w, map[string]any{"source": summary})
		},
		"text content type": func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/plain")
			_ = json.NewEncoder(w).Encode(map[string]any{"source": validSummary()})
		},
		"server error": func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusInternalServerError) },
	}
	for name, handler := range cases {
		t.Run(name, func(t *testing.T) {
			client := inspectServer(t, handler)
			_, err := client.InspectSource(context.Background(), "json", `{"id":"demo"}`)
			require.Error(t, err)
			var inspectErr *InspectError
			assert.False(t, errors.As(err, &inspectErr), "untrusted response must not look like a plugin rejection")
		})
	}
}

func TestManagerPassesInstalledDirToChild(t *testing.T) {
	t.Setenv("NAGARE_SOURCEPLUGIN_HELPER", "1")
	executable, err := filepath.Abs(os.Args[0])
	require.NoError(t, err)
	installed := filepath.Join(t.TempDir(), "installed-marker")
	manager := NewManager()
	t.Cleanup(manager.Stop)
	require.NoError(t, manager.Start(LaunchConfig{
		Executable:   executable,
		Root:         t.TempDir(),
		Version:      "test",
		InstalledDir: installed,
		Arguments:    []string{"-test.run=^TestSourcePluginHelperProcess$"},
	}))
	status := manager.Status()
	require.NotNil(t, status.Manifest)
	assert.Equal(t, "Helper installed-marker", status.Manifest.Name)
}

func TestManagerRejectsRelativeInstalledDir(t *testing.T) {
	executable, err := filepath.Abs(os.Args[0])
	require.NoError(t, err)
	manager := NewManager()
	err = manager.Start(LaunchConfig{Executable: executable, Root: t.TempDir(), Version: "test", InstalledDir: "relative/dir"})
	require.ErrorContains(t, err, "absolute")
}
