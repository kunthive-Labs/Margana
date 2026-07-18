package panels

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// ciSource surfaces a repository's most recent GitHub Actions workflow runs.
// It reuses the shared GitHub request/auth/rate-limit helpers from github.go.
type ciSource struct {
	repo    string
	token   string
	baseURL string       // defaults to defaultGithubAPI; set in tests
	client  *http.Client // defaults to the shared 10s client; set in tests
}

func (s *ciSource) Type() string { return "ci" }

func (s *ciSource) Fetch(ctx context.Context) Result {
	base := s.baseURL
	if base == "" {
		base = defaultGithubAPI
	}
	url := fmt.Sprintf("%s/repos/%s/actions/runs?per_page=%d", base, s.repo, maxItems)
	req, err := newGithubRequest(ctx, url, s.token)
	if err != nil {
		return Result{Err: err}
	}
	resp, err := clientOr(s.client).Do(req)
	if err != nil {
		return Result{Err: err}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Result{Err: describeGithubStatus(resp, s.repo)}
	}
	warning := githubRateLimitWarning(resp)

	var raw struct {
		WorkflowRuns []struct {
			Name         string `json:"name"`
			DisplayTitle string `json:"display_title"`
			HeadBranch   string `json:"head_branch"`
			Status       string `json:"status"`
			Conclusion   string `json:"conclusion"`
			Event        string `json:"event"`
			CreatedAt    string `json:"created_at"`
		} `json:"workflow_runs"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return Result{Err: err}
	}

	items := make([]Item, 0, len(raw.WorkflowRuns))
	for i, r := range raw.WorkflowRuns {
		if i >= maxItems {
			break
		}
		ts, _ := time.Parse(time.RFC3339, r.CreatedAt)

		primary := r.DisplayTitle
		if primary == "" {
			primary = r.Name
		}
		primary = firstLine(primary)

		// A completed run reports its conclusion (success/failure/...); an
		// in-flight run only has a status (queued/in_progress).
		state := r.Conclusion
		if state == "" {
			state = r.Status
		}
		secondary := state
		if r.HeadBranch != "" {
			secondary = fmt.Sprintf("%s · %s", state, r.HeadBranch)
		}
		secondary = strings.TrimSpace(secondary)

		items = append(items, Item{
			Primary:   primary,
			Secondary: secondary,
			Timestamp: ts,
		})
	}
	return Result{Items: items, Warning: warning}
}
