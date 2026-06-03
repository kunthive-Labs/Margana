// Package webhook sends outbound Discord messages — plain text, file uploads,
// and edits — through a configured Discord webhook or the relay's send API.
package webhook

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

const defaultTimeout = 5 * time.Second

type Sender struct {
	webhookURL string
	relayURL   string
	apiKey     string
	username   string
	avatarURL  string
	guildID    string
	client     *http.Client
}

type discordPayload struct {
	Username string `json:"username"`
	Content  string `json:"content"`
}

type relayPayload struct {
	Channel   string `json:"channel"`
	GuildID   string `json:"guild_id,omitempty"`
	Username  string `json:"username"`
	AvatarURL string `json:"avatar_url,omitempty"`
	Content   string `json:"content"`
	ReplyToID string `json:"reply_to_id,omitempty"`
}

type SendResultMsg struct {
	Content   string
	MessageID string
	Err       error
}

type SendFileResultMsg struct {
	Path      string
	Content   string
	MessageID string
	Err       error
}

type EditResultMsg struct {
	MessageID string
	Content   string
	Err       error
}

type apiErrorResponse struct {
	Error string `json:"error"`
}

func New(webhookURL, relayURL, apiKey, username, avatarURL, guildID string) *Sender {
	return &Sender{
		webhookURL: webhookURL,
		relayURL:   relayURL,
		apiKey:     apiKey,
		username:   username,
		avatarURL:  avatarURL,
		guildID:    guildID,
		client: &http.Client{
			Timeout: defaultTimeout,
		},
	}
}

func (s *Sender) Send(content, channel, replyToID string) (string, error) {
	if s.relayURL != "" {
		return s.sendViaRelay(content, channel, replyToID)
	}
	return "", s.sendViaWebhook(content)
}

func (s *Sender) SendFile(path, channel, content string) (string, error) {
	if s.relayURL == "" {
		return "", fmt.Errorf("file attachments require server.relay_url")
	}
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("opening file: %w", err)
	}
	defer file.Close()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	_ = writer.WriteField("channel", channel)
	_ = writer.WriteField("username", s.username)
	_ = writer.WriteField("avatar_url", s.avatarURL)
	_ = writer.WriteField("content", content)
	if s.guildID != "" {
		_ = writer.WriteField("guild_id", s.guildID)
	}
	part, err := writer.CreateFormFile("file", filepath.Base(path))
	if err != nil {
		return "", fmt.Errorf("building file form: %w", err)
	}
	if _, err := io.Copy(part, file); err != nil {
		return "", fmt.Errorf("reading file: %w", err)
	}
	if err := writer.Close(); err != nil {
		return "", fmt.Errorf("closing file form: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, s.relayURL+"/file", &body)
	if err != nil {
		return "", fmt.Errorf("failed to build request: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	if s.apiKey != "" {
		req.Header.Set("X-API-Key", s.apiKey)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to send file to relay: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("relay rejected file (HTTP %d)", resp.StatusCode)
	}
	return parseMessageID(resp.Body), nil
}

func (s *Sender) Edit(messageID, channel, content string) error {
	if s.relayURL == "" {
		return fmt.Errorf("message editing requires server.relay_url")
	}
	payload := relayPayload{
		Channel:  channel,
		GuildID:  s.guildID,
		Username: s.username,
		Content:  content,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to encode edit: %w", err)
	}
	req, err := http.NewRequest(http.MethodPatch, s.relayURL+"/message/"+messageID, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if s.apiKey != "" {
		req.Header.Set("X-API-Key", s.apiKey)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to edit message via relay: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var apiErr apiErrorResponse
		if err := json.NewDecoder(resp.Body).Decode(&apiErr); err == nil && strings.TrimSpace(apiErr.Error) != "" {
			return fmt.Errorf("%s (HTTP %d)", apiErr.Error, resp.StatusCode)
		}
		return fmt.Errorf("relay rejected edit (HTTP %d)", resp.StatusCode)
	}
	return nil
}

func (s *Sender) EditAsync(messageID, channel, content string) tea.Cmd {
	return func() tea.Msg {
		err := s.Edit(messageID, channel, content)
		return EditResultMsg{MessageID: messageID, Content: content, Err: err}
	}
}

func (s *Sender) sendViaRelay(content, channel, replyToID string) (string, error) {
	if s.relayURL == "" {
		return "", fmt.Errorf("relay URL not configured")
	}

	payload := relayPayload{
		Channel:   channel,
		GuildID:   s.guildID,
		Username:  s.username,
		AvatarURL: s.avatarURL,
		Content:   content,
		ReplyToID: replyToID,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("failed to encode message: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, s.relayURL+"/message", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("failed to build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if s.apiKey != "" {
		req.Header.Set("X-API-Key", s.apiKey)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to send message to relay: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("relay rejected message (HTTP %d)", resp.StatusCode)
	}

	return parseMessageID(resp.Body), nil
}

func parseMessageID(r io.Reader) string {
	var result struct {
		MessageID string `json:"message_id"`
	}
	_ = json.NewDecoder(r).Decode(&result)
	return result.MessageID
}

func (s *Sender) sendViaWebhook(content string) error {
	if s.webhookURL == "" {
		return fmt.Errorf("webhook URL is not configured — set server.webhook_url or server.relay_url in config")
	}

	payload := discordPayload{
		Username: s.username,
		Content:  content,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to encode message payload: %w", err)
	}

	resp, err := s.client.Post(s.webhookURL, "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to send message to Discord: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("message rejected by Discord (HTTP %d)", resp.StatusCode)
	}

	return nil
}

func (s *Sender) SendAsync(content, channel, replyToID string) tea.Cmd {
	return func() tea.Msg {
		msgID, err := s.Send(content, channel, replyToID)
		return SendResultMsg{
			Content:   content,
			MessageID: msgID,
			Err:       err,
		}
	}
}

func (s *Sender) SendFileAsync(path, channel, content string) tea.Cmd {
	return func() tea.Msg {
		msgID, err := s.SendFile(path, channel, content)
		return SendFileResultMsg{Path: path, Content: content, MessageID: msgID, Err: err}
	}
}
