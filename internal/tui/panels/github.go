package panels

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// defaultGithubAPI is the GitHub REST base URL. It is a field-overridable
// default (see githubSource.baseURL / ciSource.baseURL) so tests can point the
// sources at an httptest.Server.
const defaultGithubAPI = "https://api.github.com"

// githubSource surfaces a repository's recent public events (pushes, PRs,
// issues, ...). It generalizes Marga's original hardcoded GitHub sidebar.
type githubSource struct {
	repo    string
	token   string
	baseURL string       // defaults to defaultGithubAPI; set in tests
	client  *http.Client // defaults to the shared 10s client; set in tests
}

func (s *githubSource) Type() string { return "github" }

func (s *githubSource) Fetch(ctx context.Context) Result {
	base := s.baseURL
	if base == "" {
		base = defaultGithubAPI
	}
	url := fmt.Sprintf("%s/repos/%s/events", base, s.repo)
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

	var raw []struct {
		Type  string `json:"type"`
		Actor struct {
			Login string `json:"login"`
		} `json:"actor"`
		Repo struct {
			Name string `json:"name"`
		} `json:"repo"`
		Payload struct {
			Action  string `json:"action"`
			Commits []struct {
				Message string `json:"message"`
			} `json:"commits"`
		} `json:"payload"`
		CreatedAt string `json:"created_at"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return Result{Err: err}
	}

	items := make([]Item, 0, len(raw))
	for i, r := range raw {
		if i >= maxItems {
			break
		}
		ts, _ := time.Parse(time.RFC3339, r.CreatedAt)
		evType := strings.TrimSuffix(r.Type, "Event")

		primary := ""
		if len(r.Payload.Commits) > 0 {
			primary = firstLine(r.Payload.Commits[0].Message)
		} else if r.Payload.Action != "" {
			primary = r.Payload.Action
		} else {
			primary = evType
		}

		secondary := evType
		if r.Actor.Login != "" {
			secondary = fmt.Sprintf("%s @%s", evType, r.Actor.Login)
		}

		items = append(items, Item{
			Primary:   primary,
			Secondary: secondary,
			Timestamp: ts,
		})
	}
	return Result{Items: items, Warning: warning}
}

// firstLine returns the first non-empty line of s, trimmed. Commit messages
// often carry a body after a blank line; only the subject belongs in a panel.
func firstLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		if t := strings.TrimSpace(line); t != "" {
			return t
		}
	}
	return strings.TrimSpace(s)
}

// newGithubRequest builds a GET request against the GitHub API with the JSON
// Accept header and an optional bearer token. Shared by the github and ci
// sources.
func newGithubRequest(ctx context.Context, url, token string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return req, nil
}

// describeGithubStatus turns a non-200 GitHub API response into an actionable
// error.
func describeGithubStatus(resp *http.Response, repo string) error {
	switch resp.StatusCode {
	case http.StatusUnauthorized:
		return fmt.Errorf("GitHub: token invalid or expired")
	case http.StatusForbidden:
		if resp.Header.Get("X-RateLimit-Remaining") == "0" {
			if reset, err := strconv.ParseInt(resp.Header.Get("X-RateLimit-Reset"), 10, 64); err == nil && reset > 0 {
				return fmt.Errorf("GitHub: rate limit exceeded (resets %s)", time.Unix(reset, 0).Local().Format("15:04:05"))
			}
			return fmt.Errorf("GitHub: rate limit exceeded")
		}
		return fmt.Errorf("GitHub: forbidden — check token scopes for %s", repo)
	case http.StatusNotFound:
		return fmt.Errorf("GitHub: repo %q not found or private", repo)
	default:
		return fmt.Errorf("github API: HTTP %d", resp.StatusCode)
	}
}

// githubRateLimitWarning returns a short warning if the remaining quota is low,
// else "".
func githubRateLimitWarning(resp *http.Response) string {
	remainingStr := resp.Header.Get("X-RateLimit-Remaining")
	if remainingStr == "" {
		return ""
	}
	remaining, err := strconv.Atoi(remainingStr)
	if err != nil || remaining >= 5 {
		return ""
	}
	if reset, err := strconv.ParseInt(resp.Header.Get("X-RateLimit-Reset"), 10, 64); err == nil && reset > 0 {
		return fmt.Sprintf("rate limit low (%d left, resets %s)", remaining, time.Unix(reset, 0).Local().Format("15:04:05"))
	}
	return fmt.Sprintf("rate limit low (%d left)", remaining)
}
