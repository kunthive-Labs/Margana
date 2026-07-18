package panels

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

const sampleCIRuns = `{
  "total_count": 2,
  "workflow_runs": [
    {
      "name": "CI",
      "display_title": "fix: squash the bug",
      "head_branch": "main",
      "status": "completed",
      "conclusion": "success",
      "event": "push",
      "created_at": "2026-01-02T15:04:05Z"
    },
    {
      "name": "Release",
      "display_title": "",
      "head_branch": "release",
      "status": "in_progress",
      "conclusion": "",
      "event": "push",
      "created_at": "2026-01-01T10:00:00Z"
    }
  ]
}`

func TestCISourceFetch(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = w.Write([]byte(sampleCIRuns))
	}))
	defer srv.Close()

	src := &ciSource{repo: "o/r", baseURL: srv.URL}
	res := src.Fetch(context.Background())

	if res.Err != nil {
		t.Fatalf("unexpected error: %v", res.Err)
	}
	if gotPath != "/repos/o/r/actions/runs" {
		t.Errorf("unexpected request path %q", gotPath)
	}
	if len(res.Items) != 2 {
		t.Fatalf("expected 2 items, got %d: %#v", len(res.Items), res.Items)
	}

	if res.Items[0].Primary != "fix: squash the bug" {
		t.Errorf("expected display_title as primary, got %q", res.Items[0].Primary)
	}
	if res.Items[0].Secondary != "success · main" {
		t.Errorf("expected 'success · main', got %q", res.Items[0].Secondary)
	}
	wantTS, _ := time.Parse(time.RFC3339, "2026-01-02T15:04:05Z")
	if !res.Items[0].Timestamp.Equal(wantTS) {
		t.Errorf("expected timestamp %v, got %v", wantTS, res.Items[0].Timestamp)
	}

	// Second run: no display_title (falls back to name), still running (status
	// used since conclusion is empty).
	if res.Items[1].Primary != "Release" {
		t.Errorf("expected name fallback as primary, got %q", res.Items[1].Primary)
	}
	if res.Items[1].Secondary != "in_progress · release" {
		t.Errorf("expected 'in_progress · release', got %q", res.Items[1].Secondary)
	}
}

func TestCISourceFetchErrors(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		body    string
		wantErr bool
	}{
		{name: "malformed json", status: http.StatusOK, body: `{"workflow_runs":`, wantErr: true},
		{name: "server error", status: http.StatusInternalServerError, body: `{}`, wantErr: true},
		{name: "no runs", status: http.StatusOK, body: `{"workflow_runs":[]}`, wantErr: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()

			src := &ciSource{repo: "o/r", baseURL: srv.URL}
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

func TestCISourceType(t *testing.T) {
	if (&ciSource{}).Type() != "ci" {
		t.Errorf("expected type 'ci'")
	}
}
