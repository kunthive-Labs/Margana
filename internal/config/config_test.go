package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := Default()

	if cfg.General.Username != "" {
		t.Errorf("expected empty username, got '%s'", cfg.General.Username)
	}
	if cfg.General.Channel != "general" {
		t.Errorf("expected channel 'general', got '%s'", cfg.General.Channel)
	}
	if cfg.Auth.Discord.RedirectURL != "http://127.0.0.1:53682/callback" {
		t.Errorf("expected default redirect url, got '%s'", cfg.Auth.Discord.RedirectURL)
	}
	if cfg.UI.HistoryLimit != 100 {
		t.Errorf("expected history_limit 100, got %d", cfg.UI.HistoryLimit)
	}
	if cfg.UI.Theme != "default" {
		t.Errorf("expected theme 'default', got '%s'", cfg.UI.Theme)
	}
}

func TestValidateMissingFields(t *testing.T) {
	cfg := Default()
	cfg.Server.WebsocketURL = ""

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error for missing websocket_url")
	}

	cfg.Server.WebsocketURL = "wss://example.com"
	cfg.Server.WebhookURL = ""
	cfg.Server.RelayURL = ""
	err = cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error for missing webhook_url and relay_url")
	}

	cfg.Server.WebhookURL = "https://discord.com/api/webhooks/test"
	cfg.Auth.Discord.ClientID = "test-client-id"
	err = cfg.Validate()
	if err != nil {
		t.Fatalf("expected valid config, got: %v", err)
	}
}

func TestValidateMissingUsername(t *testing.T) {
	cfg := Default()
	cfg.General.Username = ""
	cfg.Auth.Enabled = false

	err := cfg.Validate()
	if err != nil {
		t.Fatalf("expected username to default to 'anon' when empty and auth disabled, got: %v", err)
	}
	if cfg.General.Username != "anon" {
		t.Fatalf("expected username to be set to 'anon', got '%s'", cfg.General.Username)
	}
}

func TestValidateAllowsMissingUsernameWithDiscordAuth(t *testing.T) {
	cfg := Default()
	cfg.Server.WebsocketURL = "wss://example.com"
	cfg.Server.WebhookURL = "https://discord.com/api/webhooks/test"
	cfg.Auth.Enabled = true
	cfg.Auth.Provider = "discord"
	cfg.Auth.Discord.ClientID = "client-id"
	cfg.Auth.Discord.ClientSecret = "client-secret"

	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected discord auth config to be valid without username, got: %v", err)
	}
}

func TestLoadFromTOMLFile(t *testing.T) {
	clearConfigEnvVars()

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")

	content := `
[general]
username = "testuser"
channel = "dev"

[server]
websocket_url = "wss://relay.test.com/ws"
webhook_url = "https://discord.com/api/webhooks/123/token"

[auth.discord]
client_id = "test-client-id"

[ui]
theme = "dracula"
history_limit = 50
`
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatalf("writing test config: %v", err)
	}

	origArgs := os.Args
	os.Args = []string{"marga", "--config", cfgPath}
	defer func() { os.Args = origArgs }()

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.General.Username != "testuser" {
		t.Errorf("expected username 'testuser', got '%s'", cfg.General.Username)
	}
	if cfg.General.Channel != "dev" {
		t.Errorf("expected channel 'dev', got '%s'", cfg.General.Channel)
	}
	if cfg.Server.WebsocketURL != "wss://relay.test.com/ws" {
		t.Errorf("expected websocket_url, got '%s'", cfg.Server.WebsocketURL)
	}
	if cfg.UI.Theme != "dracula" {
		t.Errorf("expected theme 'dracula', got '%s'", cfg.UI.Theme)
	}
	if cfg.UI.HistoryLimit != 50 {
		t.Errorf("expected history_limit 50, got %d", cfg.UI.HistoryLimit)
	}
}

func TestEnvOverrides(t *testing.T) {
	cfg := Default()
	cfg.Server.WebsocketURL = "wss://original.com"
	cfg.Server.WebhookURL = "https://original.com/webhook"

	os.Setenv("MARGA_USERNAME", "envuser")
	os.Setenv("MARGA_CHANNEL", "envchannel")
	os.Setenv("MARGA_WEBSOCKET_URL", "wss://env.com/ws")
	os.Setenv("MARGA_WEBHOOK_URL", "https://env.com/webhook")
	os.Setenv("MARGA_RELAY_URL", "https://relay.env.com")
	os.Setenv("MARGA_AUTH_ENABLED", "true")
	os.Setenv("MARGA_AUTH_PROVIDER", "discord")
	os.Setenv("MARGA_DISCORD_CLIENT_ID", "discord-client-id")
	os.Setenv("MARGA_DISCORD_CLIENT_SECRET", "discord-client-secret")
	os.Setenv("MARGA_DISCORD_REDIRECT_URL", "http://127.0.0.1:9000/callback")
	os.Setenv("MARGA_THEME", "solarized")
	os.Setenv("MARGA_HISTORY_LIMIT", "200")
	defer func() {
		os.Unsetenv("MARGA_USERNAME")
		os.Unsetenv("MARGA_CHANNEL")
		os.Unsetenv("MARGA_WEBSOCKET_URL")
		os.Unsetenv("MARGA_WEBHOOK_URL")
		os.Unsetenv("MARGA_RELAY_URL")
		os.Unsetenv("MARGA_AUTH_ENABLED")
		os.Unsetenv("MARGA_AUTH_PROVIDER")
		os.Unsetenv("MARGA_DISCORD_CLIENT_ID")
		os.Unsetenv("MARGA_DISCORD_CLIENT_SECRET")
		os.Unsetenv("MARGA_DISCORD_REDIRECT_URL")
		os.Unsetenv("MARGA_THEME")
		os.Unsetenv("MARGA_HISTORY_LIMIT")
	}()

	applyEnvOverrides(cfg)

	if cfg.General.Username != "envuser" {
		t.Errorf("expected username 'envuser', got '%s'", cfg.General.Username)
	}
	if cfg.General.Channel != "envchannel" {
		t.Errorf("expected channel 'envchannel', got '%s'", cfg.General.Channel)
	}
	if cfg.Server.WebsocketURL != "wss://env.com/ws" {
		t.Errorf("expected overridden websocket_url, got '%s'", cfg.Server.WebsocketURL)
	}
	if cfg.Server.WebhookURL != "https://env.com/webhook" {
		t.Errorf("expected overridden webhook_url, got '%s'", cfg.Server.WebhookURL)
	}
	if cfg.Server.RelayURL != "https://relay.env.com" {
		t.Errorf("expected overridden relay_url, got '%s'", cfg.Server.RelayURL)
	}
	if !cfg.Auth.Enabled || cfg.Auth.Provider != "discord" {
		t.Errorf("expected discord auth env overrides to be applied, got enabled=%v provider=%q", cfg.Auth.Enabled, cfg.Auth.Provider)
	}
	if cfg.Auth.Discord.ClientID != "discord-client-id" {
		t.Errorf("expected discord client id override, got '%s'", cfg.Auth.Discord.ClientID)
	}
	if cfg.Auth.Discord.RedirectURL != "http://127.0.0.1:9000/callback" {
		t.Errorf("expected discord redirect override, got '%s'", cfg.Auth.Discord.RedirectURL)
	}
	if cfg.UI.Theme != "solarized" {
		t.Errorf("expected theme 'solarized', got '%s'", cfg.UI.Theme)
	}
	if cfg.UI.HistoryLimit != 200 {
		t.Errorf("expected history_limit 200, got %d", cfg.UI.HistoryLimit)
	}
}

func TestLoadNotificationsConfig(t *testing.T) {
	clearConfigEnvVars()

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")

	content := `
[general]
username = "testuser"

[server]
websocket_url = "wss://relay.test.com/ws"
webhook_url = "https://discord.com/api/webhooks/123/token"

[auth.discord]
client_id = "test-client-id"

[notifications]
bell_on_mention = true
muted_channels = ["bots", "ci-spam"]
`
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatalf("writing test config: %v", err)
	}

	origArgs := os.Args
	os.Args = []string{"marga", "--config", cfgPath}
	defer func() { os.Args = origArgs }()

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if !cfg.Notifications.BellOnMention {
		t.Error("expected bell_on_mention to be true")
	}
	if len(cfg.Notifications.MutedChannels) != 2 {
		t.Fatalf("expected 2 muted channels, got %d", len(cfg.Notifications.MutedChannels))
	}
	if cfg.Notifications.MutedChannels[0] != "bots" {
		t.Errorf("expected first muted channel 'bots', got %q", cfg.Notifications.MutedChannels[0])
	}
}

func TestEnvOverridesInvalidHistoryLimit(t *testing.T) {
	cfg := Default()

	os.Setenv("MARGA_HISTORY_LIMIT", "notanumber")
	defer os.Unsetenv("MARGA_HISTORY_LIMIT")

	applyEnvOverrides(cfg)

	if cfg.UI.HistoryLimit != 100 {
		t.Errorf("expected default history_limit 100 to remain, got %d", cfg.UI.HistoryLimit)
	}
}

func TestEnvOverridesNegativeHistoryLimit(t *testing.T) {
	cfg := Default()

	os.Setenv("MARGA_HISTORY_LIMIT", "-5")
	defer os.Unsetenv("MARGA_HISTORY_LIMIT")

	applyEnvOverrides(cfg)

	if cfg.UI.HistoryLimit != 100 {
		t.Errorf("expected default history_limit 100 to remain for negative value, got %d", cfg.UI.HistoryLimit)
	}
}

func TestLoadWithCLIConfigFlag(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")

	content := `
[general]
username = "cliuser"
channel = "cli-channel"

[server]
websocket_url = "wss://cli.example.com/ws"
webhook_url = "https://discord.com/api/webhooks/cli/token"

[auth.discord]
client_id = "test-client-id"

[ui]
theme = "dracula"
history_limit = 75
`
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatalf("writing test config: %v", err)
	}

	origArgs := os.Args
	os.Args = []string{"marga", "--config", cfgPath}
	defer func() { os.Args = origArgs }()

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	if cfg.General.Username != "cliuser" {
		t.Errorf("expected username 'cliuser', got '%s'", cfg.General.Username)
	}
	if cfg.General.Channel != "cli-channel" {
		t.Errorf("expected channel 'cli-channel', got '%s'", cfg.General.Channel)
	}
	if cfg.Server.WebsocketURL != "wss://cli.example.com/ws" {
		t.Errorf("expected websocket_url from file, got '%s'", cfg.Server.WebsocketURL)
	}
	if cfg.UI.HistoryLimit != 75 {
		t.Errorf("expected history_limit 75, got %d", cfg.UI.HistoryLimit)
	}
}

func TestLoadWithShorthandConfigFlag(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")

	content := `
[general]
username = "shortuser"
channel = "short-channel"

[server]
websocket_url = "wss://short.example.com/ws"
webhook_url = "https://discord.com/api/webhooks/short/token"

[auth.discord]
client_id = "test-client-id"
`
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatalf("writing test config: %v", err)
	}

	origArgs := os.Args
	os.Args = []string{"marga", "-c", cfgPath}
	defer func() { os.Args = origArgs }()

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	if cfg.General.Username != "shortuser" {
		t.Errorf("expected username 'shortuser', got '%s'", cfg.General.Username)
	}
}

func TestEnvOverridesTakePrecedenceOverFile(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")

	content := `
[general]
username = "fileuser"
channel = "file-channel"

[server]
websocket_url = "wss://file.example.com/ws"
webhook_url = "https://discord.com/api/webhooks/file/token"

[auth.discord]
client_id = "test-client-id"
`
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatalf("writing test config: %v", err)
	}

	os.Setenv("MARGA_USERNAME", "envoverride")
	defer os.Unsetenv("MARGA_USERNAME")

	origArgs := os.Args
	os.Args = []string{"marga", "--config", cfgPath}
	defer func() { os.Args = origArgs }()

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	if cfg.General.Username != "envoverride" {
		t.Errorf("env var should override file value, got '%s'", cfg.General.Username)
	}
	if cfg.General.Channel != "file-channel" {
		t.Errorf("channel should come from file since no env override, got '%s'", cfg.General.Channel)
	}
}

func TestLoadMissingRequiredField(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")

	content := `
[general]
username = "testuser"

[server]
websocket_url = ""
`
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatalf("writing test config: %v", err)
	}

	origArgs := os.Args
	os.Args = []string{"marga", "--config", cfgPath}
	defer func() { os.Args = origArgs }()

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for missing websocket_url")
	}
}

func TestLoadNonexistentConfigFile(t *testing.T) {
	origArgs := os.Args
	os.Args = []string{"marga", "--config", "/nonexistent/path/config.toml"}
	defer func() { os.Args = origArgs }()

	clearConfigEnvVars()

	// Marga ships no hosted relay. With no config file and no env, Discord is
	// enabled by default but has no endpoints or client_id, so Load must fail
	// with an actionable error rather than silently using a dead default.
	if _, err := Load(); err == nil {
		t.Fatal("expected an error when no config exists and Discord is unconfigured")
	}
}

func TestDefaultConfigPath(t *testing.T) {
	path, err := DefaultConfigPath()
	if err != nil {
		t.Fatalf("DefaultConfigPath() error: %v", err)
	}
	if path == "" {
		t.Error("DefaultConfigPath() returned empty path")
	}
}

func clearConfigEnvVars() {
	os.Unsetenv("MARGA_USERNAME")
	os.Unsetenv("MARGA_CHANNEL")
	os.Unsetenv("MARGA_WEBSOCKET_URL")
	os.Unsetenv("MARGA_WEBHOOK_URL")
	os.Unsetenv("MARGA_RELAY_URL")
	os.Unsetenv("MARGA_THEME")
	os.Unsetenv("MARGA_HISTORY_LIMIT")
}
