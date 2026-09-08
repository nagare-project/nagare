package sourceplugin

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClientNegotiatesAndConsumesCandidateStream(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/manifest", func(w http.ResponseWriter, _ *http.Request) {
		writeTestJSON(w, Manifest{
			ID: "org.example.sources", Name: "Example sources", Version: "1.2.3",
			ProtocolVersions: []int{1}, SourceSchemaVersions: []int{1}, Capabilities: []string{"web", "bt", "ndjson"},
		})
	})
	mux.HandleFunc("GET /v1/sources", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "1", r.Header.Get("X-Nagare-Protocol-Version"))
		writeTestJSON(w, map[string]any{"sources": []Source{{
			ID: "example-web", Name: "Example", Kind: "web", Tier: 1, Version: "1", Enabled: true, Status: "healthy", Capabilities: []string{"direct"},
		}}})
	})
	mux.HandleFunc("POST /v1/candidates", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "1", r.Header.Get("X-Nagare-Protocol-Version"))
		var request ResolveRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&request))
		assert.Equal(t, "3", request.Episode.Number)
		w.Header().Set("Content-Type", "application/x-ndjson; charset=utf-8")
		_, _ = fmt.Fprintln(w, `{"event":"candidate","candidate":{"schema":"nagare-candidate/v1","id":"example-web:3","sourceId":"example-web","tier":1,"matchConfidence":1,"match":{"basis":["title_episode"],"episodeNumber":3},"transport":{"type":"hls","url":"https://media.example/3.m3u8"},"metadata":{"resolution":"1080P","episode":3}}}`)
		_, _ = fmt.Fprintln(w, `{"event":"source_error","sourceId":"example-bt","category":"search_timeout","message":"timed out","retryable":true}`)
		_, _ = fmt.Fprintln(w, `{"event":"done","queried":2,"succeeded":1,"failed":1,"durationMs":12}`)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	client, err := NewClient(server.URL)
	require.NoError(t, err)
	manifest, err := client.Negotiate(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "org.example.sources", manifest.ID)
	sources, err := client.Sources(context.Background())
	require.NoError(t, err)
	require.Len(t, sources, 1)
	assert.Equal(t, "example-web", sources[0].ID)

	request := ResolveRequest{
		Schema:  "nagare-resolve-request/v1",
		Subject: Subject{IDs: map[string]string{"anilist": "123"}, Titles: []string{"Example"}},
		Episode: Episode{Number: "3"},
	}
	var events []Event
	require.NoError(t, client.Candidates(context.Background(), request, func(event Event) error {
		events = append(events, event)
		return nil
	}))
	require.Len(t, events, 3)
	assert.Equal(t, "hls", events[0].Candidate.Transport.Type)
	assert.True(t, events[1].Retryable)
	assert.Equal(t, 2, events[2].Queried)
}

func TestClientRejectsUntrustedProtocolShapes(t *testing.T) {
	for _, raw := range []string{
		"https://127.0.0.1:1234",
		"http://localhost:1234",
		"http://192.0.2.1:1234",
		"http://127.0.0.1",
		"http://127.0.0.1:1234/path",
	} {
		_, err := NewClient(raw)
		require.Error(t, err, raw)
	}
	_, err := decodeEvent([]byte(`{"event":"candidate","candidate":{"schema":"nagare-candidate/v1"},"extra":true}`))
	require.Error(t, err)
	_, err = decodeEvent([]byte(`{"event":"done","queried":2,"succeeded":1,"failed":0,"durationMs":1}`))
	require.Error(t, err)
	_, err = decodeEvent([]byte(`{"event":"candidate","candidate":{"schema":"nagare-candidate/v1","id":"x","sourceId":"bad_source","tier":1,"matchConfidence":1,"match":{"basis":["title_episode"]},"transport":{"type":"hls","url":"https://example.test/x.m3u8"},"metadata":{}}}`))
	require.Error(t, err)
}

func TestManagerStartsNegotiatesAndStopsChild(t *testing.T) {
	t.Setenv("NAGARE_SOURCEPLUGIN_HELPER", "1")
	executable, err := filepath.Abs(os.Args[0])
	require.NoError(t, err)
	manager := NewManager()
	t.Cleanup(manager.Stop)
	require.NoError(t, manager.Start(LaunchConfig{
		Executable: executable,
		Root:       t.TempDir(),
		Version:    "test",
		Arguments:  []string{"-test.run=^TestSourcePluginHelperProcess$"},
	}))
	status := manager.Status()
	assert.Equal(t, "ready", status.Phase)
	require.NotNil(t, status.Manifest)
	assert.Equal(t, "org.example.helper", status.Manifest.ID)
	manager.Stop()
	assert.Equal(t, "stopped", manager.Status().Phase)
}

func TestSourcePluginHelperProcess(t *testing.T) {
	if os.Getenv("NAGARE_SOURCEPLUGIN_HELPER") != "1" {
		return
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		os.Exit(2)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/manifest", func(w http.ResponseWriter, _ *http.Request) {
		writeTestJSON(w, Manifest{
			ID: "org.example.helper", Name: "Helper", Version: "1", ProtocolVersions: []int{1}, SourceSchemaVersions: []int{1},
		})
	})
	_ = json.NewEncoder(os.Stdout).Encode(ReadyEvent{Event: "ready", Protocol: LaunchProtocol, URL: "http://" + listener.Addr().String()})
	if err := http.Serve(listener, mux); err != nil {
		os.Exit(3)
	}
}

func writeTestJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}
