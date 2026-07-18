package panels

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

const sampleGithubEvents = `[
  {
    "type": "PushEvent",
    "actor": {"login": "octocat"},
    "repo": {"name": "kunthive-Labs/Margana"},
    "payload": {"commits": [{"message": "fix: squash the bug\n\nlong body ignored"}]},
    "created_at": "2026-01-02T15:04:05Z"
  },
  {
    "type": "WatchEvent",
    "actor": {"login": "alice"},
    "repo": {"name": "kunthive-Labs/Margana"},
    "payload": {"action": "started"},
    "created_at": "2026-01-01T10:00:00Z"
  }
]`

func TestGithubSourceFetch(t *testing.T) {
	var gotPath, gotAuth, gotAccept string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotAccept = r.Header.Get("Accept")
		w.Header().Set("X-RateLimit-Remaining", "42")
		_, _ = w.Write([]byte(sampleGithubEvents))
	}))
	defer srv.Close()

	src := &githubSource{repo: "kunthive-Labs/Margana", token: "secret", baseURL: srv.URL}
	res := src.Fetch(context.Background())

	if res.Err != nil {
		t.Fatalf("unexpected error: %v", res.Err)
	}
	if res.Warning != "" {
		t.Errorf("expected no rate-limit warning at 42 remaining, got %q", res.Warning)
	}
	if gotPath != "/repos/kunthive-Labs/Margana/events" {
		t.Errorf("unexpected request path %q", gotPath)
	}
	if gotAuth != "Bearer secret" {
		t.Errorf("expected bearer auth header, got %q", gotAuth)
	}
	if gotAccept != "application/vnd.github+json" {
		t.Errorf("unexpected Accept header %q", gotAccept)
	}
	if len(res.Items) != 2 {
		t.Fatalf("expected 2 items, got %d: %#v", len(res.Items), res.Items)
	}

	if res.Items[0].Primary != "fix: squash the bug" {
		t.Errorf("expected commit subject as primary, got %q", res.Items[0].Primary)
	}
	if res.Items[0].Secondary != "Push @octocat" {
		t.Errorf("expected 'Push @octocat', got %q", res.Items[0].Secondary)
	}
	wantTS, _ := time.Parse(time.RFC3339, "2026-01-02T15:04:05Z")
	if !res.Items[0].Timestamp.Equal(wantTS) {
		t.Errorf("expected timestamp %v, got %v", wantTS, res.Items[0].Timestamp)
	}

	// Second event has no commits, only an action.
	if res.Items[1].Primary != "started" {
		t.Errorf("expected action as primary, got %q", res.Items[1].Primary)
	}
	if res.Items[1].Secondary != "Watch @alice" {
		t.Errorf("expected 'Watch @alice', got %q", res.Items[1].Secondary)
	}
}

func TestGithubSourceRateLimitWarning(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-RateLimit-Remaining", "2")
		_, _ = w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	src := &githubSource{repo: "o/r", baseURL: srv.URL}
	res := src.Fetch(context.Background())
	if res.Err != nil {
		t.Fatalf("unexpected error: %v", res.Err)
	}
	if res.Warning == "" {
		t.Error("expected a rate-limit warning when remaining is low")
	}
}

func TestGithubSourceFetchErrors(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		body    string
		wantErr bool
	}{
		{name: "not found", status: http.StatusNotFound, body: `{}`, wantErr: true},
		{name: "unauthorized", status: http.StatusUnauthorized, body: `{}`, wantErr: true},
		{name: "malformed json", status: http.StatusOK, body: `[{"type":`, wantErr: true},
		{name: "empty array", status: http.StatusOK, body: `[]`, wantErr: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()

			src := &githubSource{repo: "o/r", baseURL: srv.URL}
			res := src.Fetch(context.Background())
			if tc.wantErr && res.Err == nil {
				t.Fatalf("expected an error, got items %#v", res.Items)
			}
			if !tc.wantErr && res.Err != nil {
				t.Fatalf("unexpected error: %v", res.Err)
			}
		})
	}
}

func TestGithubSourceType(t *testing.T) {
	if (&githubSource{}).Type() != "github" {
		t.Errorf("expected type 'github'")
	}
}
