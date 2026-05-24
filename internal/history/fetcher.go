package history

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/kunthive-Labs/Margana/internal/model"
)

const defaultTimeout = 5 * time.Second
const defaultLimit = 100

type Fetcher struct {
	baseURL    string
	apiKey     string
	guildID    string
	httpClient *http.Client
}

type FetchResultMsg struct {
	Messages []model.Message
	Channel  string
	Err      error
}

type ChannelsResultMsg struct {
	Channels []string
	Err      error
}

func New(baseURL, apiKey string) *Fetcher {
	return &Fetcher{
		baseURL: baseURL,
		apiKey:  apiKey,
		httpClient: &http.Client{
			Timeout: defaultTimeout,
		},
	}
}

func (f *Fetcher) WithGuild(guildID string) *Fetcher {
	f.guildID = guildID
	return f
}

func (f *Fetcher) Fetch(channel string, limit int, before *time.Time) ([]model.Message, error) {
	if f.baseURL == "" {
		return nil, nil
	}

	url := fmt.Sprintf("%s/api/channels/%s/messages?limit=%d", f.baseURL, channel, limit)
	if before != nil {
		url += fmt.Sprintf("&before=%s", before.UTC().Format(time.RFC3339Nano))
	}
	if f.guildID != "" {
		url += fmt.Sprintf("&guild_id=%s", f.guildID)
	}

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("building history request: %w", err)
	}
	if f.apiKey != "" {
		req.Header.Set("X-API-Key", f.apiKey)
	}

	resp, err := f.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching message history: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("relay API returned HTTP %d", resp.StatusCode)
	}

	var msgs []model.Message
	if err := json.NewDecoder(resp.Body).Decode(&msgs); err != nil {
		return nil, fmt.Errorf("decoding message history response: %w", err)
	}

	return msgs, nil
}

func (f *Fetcher) FetchChannels() ([]string, error) {
	if f.baseURL == "" {
		return nil, nil
	}

	var url string
	if f.guildID != "" {
		url = fmt.Sprintf("%s/api/guilds/%s/channels", f.baseURL, f.guildID)
	} else {
		url = fmt.Sprintf("%s/api/channels", f.baseURL)
	}

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("building channels request: %w", err)
	}
	if f.apiKey != "" {
		req.Header.Set("X-API-Key", f.apiKey)
	}

	resp, err := f.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching channels: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("relay API returned HTTP %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)

	var wrapped struct {
		Channels []struct {
			Name string `json:"name"`
			Type string `json:"type,omitempty"`
		} `json:"channels"`
	}
	var channels []struct {
		Name string `json:"name"`
		Type string `json:"type,omitempty"`
	}

	if err := json.Unmarshal(body, &wrapped); err == nil && len(wrapped.Channels) > 0 {
		channels = wrapped.Channels
	} else {
		_ = json.Unmarshal(body, &channels)
	}

	names := make([]string, 0, len(channels))
	for _, ch := range channels {
		if ch.Name != "" && isTextChannel(ch.Type) {
			names = append(names, ch.Name)
		}
	}
	return names, nil
}

func (f *Fetcher) FetchAsync(channel string, limit int, before *time.Time) tea.Cmd {
	return func() tea.Msg {
		msgs, err := f.Fetch(channel, limit, before)
		return FetchResultMsg{
			Messages: msgs,
			Channel:  channel,
			Err:      err,
		}
	}
}

func (f *Fetcher) FetchChannelsAsync() tea.Cmd {
	return func() tea.Msg {
		channels, err := f.FetchChannels()
		return ChannelsResultMsg{
			Channels: channels,
			Err:      err,
		}
	}
}

func InitialFetch(f *Fetcher, channel string, limit int) tea.Cmd {
	if f == nil || f.baseURL == "" {
		return nil
	}
	if limit <= 0 {
		limit = defaultLimit
	}
	return f.FetchAsync(channel, limit, nil)
}

func FetchChannels(f *Fetcher) tea.Cmd {
	if f == nil || f.baseURL == "" {
		return nil
	}
	return f.FetchChannelsAsync()
}

func LoadOlder(f *Fetcher, channel string, oldestTimestamp time.Time) tea.Cmd {
	if f == nil || f.baseURL == "" {
		return nil
	}
	before := oldestTimestamp
	return f.FetchAsync(channel, defaultLimit, &before)
}

// FetchSinceMessages fetches messages newer than `since` and returns them
// directly (the network adapter wraps this for its catch-up polling).
func (f *Fetcher) FetchSinceMessages(channel string, since time.Time) ([]model.Message, error) {
	if f.baseURL == "" {
		return nil, nil
	}
	url := fmt.Sprintf("%s/api/channels/%s/messages?since=%s&limit=50",
		f.baseURL, channel, since.UTC().Format(time.RFC3339Nano))
	if f.guildID != "" {
		url += fmt.Sprintf("&guild_id=%s", f.guildID)
	}
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if f.apiKey != "" {
		req.Header.Set("X-API-Key", f.apiKey)
	}
	resp, err := f.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, nil
	}
	var msgs []model.Message
	if err := json.NewDecoder(resp.Body).Decode(&msgs); err != nil {
		return nil, err
	}
	return msgs, nil
}

func (f *Fetcher) FetchSince(channel string, since time.Time) tea.Cmd {
	return func() tea.Msg {
		if f.baseURL == "" {
			return FetchResultMsg{Channel: channel}
		}
		url := fmt.Sprintf("%s/api/channels/%s/messages?since=%s&limit=50",
			f.baseURL, channel, since.UTC().Format(time.RFC3339Nano))
		if f.guildID != "" {
			url += fmt.Sprintf("&guild_id=%s", f.guildID)
		}
		req, err := http.NewRequest(http.MethodGet, url, nil)
		if err != nil {
			return FetchResultMsg{Channel: channel, Err: err}
		}
		if f.apiKey != "" {
			req.Header.Set("X-API-Key", f.apiKey)
		}
		resp, err := f.httpClient.Do(req)
		if err != nil {
			return FetchResultMsg{Channel: channel, Err: err}
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return FetchResultMsg{Channel: channel}
		}
		var msgs []model.Message
		if err := json.NewDecoder(resp.Body).Decode(&msgs); err != nil {
			return FetchResultMsg{Channel: channel, Err: err}
		}
		return FetchResultMsg{Messages: msgs, Channel: channel}
	}
}

func FetchNewerSince(f *Fetcher, channel string, since time.Time) tea.Cmd {
	if f == nil || f.baseURL == "" {
		return nil
	}
	return f.FetchSince(channel, since)
}

func isTextChannel(channelType string) bool {
	return channelType == "" || channelType == "text"
}
