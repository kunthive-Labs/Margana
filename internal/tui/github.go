package tui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func (m Model) githubPollCmd() tea.Cmd {
	if m.githubRepo == "" {
		return nil
	}
	return tea.Tick(60*time.Second, func(t time.Time) tea.Msg {
		return m.fetchGithubActivity()
	})
}

// fetchGithubActivity fetches recent events from the GitHub API.
func (m Model) fetchGithubActivity() githubActivityMsg {
	if m.githubRepo == "" {
		return githubActivityMsg{}
	}
	url := fmt.Sprintf("https://api.github.com/repos/%s/events", m.githubRepo)
	req, err := newGithubRequest(url, m.githubToken)
	if err != nil {
		return githubActivityMsg{Err: err}
	}
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return githubActivityMsg{Err: err}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return githubActivityMsg{Err: describeGithubStatus(resp, m.githubRepo)}
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
		return githubActivityMsg{Err: err}
	}
	events := make([]GithubActivityEvent, 0, len(raw))
	for i, r := range raw {
		if i >= 10 {
			break
		}
		ts, _ := time.Parse(time.RFC3339, r.CreatedAt)
		title := r.Payload.Action
		if len(r.Payload.Commits) > 0 {
			title = r.Payload.Commits[0].Message
			if len(title) > 60 {
				title = title[:60] + "..."
			}
		}
		events = append(events, GithubActivityEvent{
			Type:      r.Type,
			Repo:      r.Repo.Name,
			Actor:     r.Actor.Login,
			Title:     title,
			Timestamp: ts,
		})
	}
	return githubActivityMsg{Events: events, Warning: warning}
}

// describeGithubStatus turns a non-200 GitHub API response into an actionable error.
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

// githubRateLimitWarning returns a short warning if the remaining quota is low, else "".
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

// newGithubRequest builds an HTTP GET request with optional auth token.
func newGithubRequest(url, token string) (*http.Request, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return req, nil
}

// centerInTerm pads content (modalW x modalH) to full terminal dimensions
// using spaces. Avoids lipgloss.Place ANSI measurement issues.
