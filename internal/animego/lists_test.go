package animego_test

import (
	"context"
	"github.com/nagare-project/nagare/internal/animego"
	"github.com/stretchr/testify/require"
	"net/http"
	"testing"
)

func TestListReadAndLowerProgress(t *testing.T) {
	rec := &recorder{}
	client := newTestClient(t, rec, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" && r.URL.Path == "/api/subscriptions" {
			respond(w, 200, `{"data":[{"anilistId":154587,"status":"watching","currentEpisode":4,"episodes":28,"titleRomaji":"Frieren","score":null}]}`)
			return
		}
		if r.Method == "GET" {
			respond(w, 200, `{"data":{"anilistId":154587,"status":"watching","currentEpisode":4,"watchedEpisodes":[1,2,4]}}`)
			return
		}
		respond(w, 200, `{"data":{}}`)
	})
	client.RestoreSession(animego.Session{AccessToken: "test", RefreshCookie: "test"})
	entries, err := client.ListEntries(context.Background())
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.Equal(t, 4, entries[0].CurrentEpisode)
	require.NoError(t, client.SaveListEntry(context.Background(), 154587, "watching", 1, nil))
	requests := rec.all()
	require.Len(t, requests, 5)
	require.Equal(t, "/api/subscriptions/154587/episodes/2", requests[2].Path)
	require.Equal(t, "/api/subscriptions/154587/episodes/4", requests[3].Path)
	require.Equal(t, "DELETE", requests[3].Method)
	require.Equal(t, "PATCH", requests[4].Method)
	require.JSONEq(t, `{"status":"watching","currentEpisode":1,"score":null}`, string(requests[4].Body))
	for _, r := range requests {
		require.Equal(t, "Bearer test", r.Auth)
	}
}
func TestListCreateAndValidation(t *testing.T) {
	rec := &recorder{}
	client := newTestClient(t, rec, func(w http.ResponseWriter, r *http.Request) { respond(w, 200, `{"data":null}`) })
	client.RestoreSession(animego.Session{AccessToken: "test", RefreshCookie: "test"})
	require.NoError(t, client.SaveListEntry(context.Background(), 1, "plan_to_watch", 0, nil))
	requests := rec.all()
	require.Len(t, requests, 3)
	require.Equal(t, "POST", requests[1].Method)
	require.JSONEq(t, `{"anilistId":1,"status":"plan_to_watch","ifAbsent":true}`, string(requests[1].Body))
	require.Error(t, client.SaveListEntry(context.Background(), 1, "invalid", 0, nil))
	require.Error(t, client.SaveListEntry(context.Background(), 1, "watching", -1, nil))
	require.Len(t, rec.all(), 3)
}
func TestListStopsWhenUnmarkFails(t *testing.T) {
	rec := &recorder{}
	client := newTestClient(t, rec, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			respond(w, 200, `{"data":{"currentEpisode":2,"watchedEpisodes":[1,2]}}`)
			return
		}
		respond(w, 503, `{"error":"unavailable"}`)
	})
	client.RestoreSession(animego.Session{AccessToken: "test", RefreshCookie: "test"})
	require.Error(t, client.SaveListEntry(context.Background(), 1, "watching", 0, nil))
	require.Len(t, rec.all(), 2)
}
