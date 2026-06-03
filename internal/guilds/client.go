// Package guilds is a small client for the relay's guild-listing endpoint, used
// to discover which Discord servers the relay is configured for.
package guilds

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

const defaultTimeout = 5 * time.Second

type Guild struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type guildsResponse struct {
	Guilds []Guild `json:"guilds"`
}

type Client struct {
	relayURL string
	apiKey   string
	client   *http.Client
}

func NewClient(relayURL, apiKey string) *Client {
	return &Client{
		relayURL: relayURL,
		apiKey:   apiKey,
		client:   &http.Client{Timeout: defaultTimeout},
	}
}

func (c *Client) FetchGuilds(query string) ([]Guild, error) {
	if c.relayURL == "" {
		return nil, fmt.Errorf("relay URL not configured")
	}

	u, err := url.Parse(c.relayURL + "/api/guilds")
	if err != nil {
		return nil, fmt.Errorf("parsing relay URL: %w", err)
	}
	if query != "" {
		q := u.Query()
		q.Set("q", query)
		u.RawQuery = q.Encode()
	}

	req, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}
	if c.apiKey != "" {
		req.Header.Set("X-API-Key", c.apiKey)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching guilds from relay: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("relay returned HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	var result guildsResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("parsing response: %w", err)
	}

	if result.Guilds == nil {
		result.Guilds = []Guild{}
	}
	return result.Guilds, nil
}
