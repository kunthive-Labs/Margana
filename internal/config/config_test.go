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

func TestApplyDefaultsSetsLogFormat(t *testing.T) {
	cfg := &Config{}
	cfg.ApplyDefaults()
	if cfg.Logging.Format != "text" {
		t.Errorf("expected default log format 'text', got '%s'", cfg.Logging.Format)
	}
}

func TestLoggingEnvOverrides(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")
	content := `
[general]
username = "anon"

[auth]
enabled = false

[logging]
level = "info"
file = "/from/file.log"
format = "text"
`
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatalf("writing config: %v", err)
	}

	origArgs := os.Args
	os.Args = []string{"marga", "--config", cfgPath}
	defer func() { os.Args = origArgs }()

	t.Setenv("MARGA_LOG_LEVEL", "debug")
	t.Setenv("MARGA_LOG_FILE", "/from/env.log")
	t.Setenv("MARGA_LOG_FORMAT", "json")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Logging.Level != "debug" {
		t.Errorf("env should override level: got %q", cfg.Logging.Level)
	}
	if cfg.Logging.File != "/from/env.log" {
		t.Errorf("env should override file: got %q", cfg.Logging.File)
	}
	if cfg.Logging.Format != "json" {
		t.Errorf("env should override format: got %q", cfg.Logging.Format)
	}
}

func TestConfigPathFromArgsIgnoresUnknownFlags(t *testing.T) {
	// Regression: unknown flags (e.g. --setup, --debug) must not break config
	// path resolution. The old flag.FlagSet parser errored on them.
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"--setup"}, ""}, // falls back to default path (non-empty)
		{[]string{"--config", "/a.toml"}, "/a.toml"},
		{[]string{"-c", "/b.toml"}, "/b.toml"},
		{[]string{"--config=/c.toml"}, "/c.toml"},
		{[]string{"-c=/d.toml"}, "/d.toml"},
		{[]string{"--debug", "--config", "/e.toml", "--log-file", "/x.log"}, "/e.toml"},
		{[]string{"--setup", "-s", "--config", "/f.toml"}, "/f.toml"},
	}
	for _, tc := range cases {
		got, err := ConfigPathFromArgs(tc.args)
		if err != nil {
			t.Errorf("ConfigPathFromArgs(%v) unexpected error: %v", tc.args, err)
			continue
		}
		if tc.want == "" {
			if got == "" {
				t.Errorf("ConfigPathFromArgs(%v) returned empty; expected default path", tc.args)
			}
			continue
		}
		if got != tc.want {
			t.Errorf("ConfigPathFromArgs(%v) = %q, want %q", tc.args, got, tc.want)
		}
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

func TestLoadPanelsConfig(t *testing.T) {
	clearConfigEnvVars()

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")

	content := `
[general]
username = "anon"

[auth]
enabled = false

[[panels]]
type = "github"
source = "kunthive-Labs/Margana"
refresh = "30s"

[[panels]]
type = "rss"
title = "Blog"
source = "https://example.com/feed.xml"

[[panels]]
type = "ci"
source = "kunthive-Labs/Margana"
enabled = false
`
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatalf("writing config: %v", err)
	}

	origArgs := os.Args
	os.Args = []string{"marga", "--config", cfgPath}
	defer func() { os.Args = origArgs }()

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if len(cfg.Panels) != 3 {
		t.Fatalf("expected 3 panels parsed, got %d", len(cfg.Panels))
	}
	if cfg.Panels[0].Type != "github" || cfg.Panels[0].Source != "kunthive-Labs/Margana" {
		t.Errorf("unexpected panel[0]: %#v", cfg.Panels[0])
	}
	if cfg.Panels[0].Refresh != "30s" {
		t.Errorf("expected explicit refresh 30s, got %q", cfg.Panels[0].Refresh)
	}
	if cfg.Panels[1].Title != "Blog" {
		t.Errorf("expected title 'Blog', got %q", cfg.Panels[1].Title)
	}
	// The rss panel omitted refresh, so ApplyDefaults (via Load) fills it in.
	if cfg.Panels[1].Refresh != "60s" {
		t.Errorf("expected defaulted refresh 60s, got %q", cfg.Panels[1].Refresh)
	}
	if cfg.Panels[2].Enabled == nil {
		t.Fatal("expected ci panel to record enabled=false")
	} else if *cfg.Panels[2].Enabled {
		t.Errorf("expected ci panel enabled=false")
	}

	// EnabledPanels drops the disabled ci panel and keeps the other two.
	enabled := cfg.EnabledPanels()
	if len(enabled) != 2 {
		t.Fatalf("expected 2 enabled panels, got %d", len(enabled))
	}
	for _, p := range enabled {
		if p.Type == "ci" {
			t.Errorf("disabled ci panel should be excluded from EnabledPanels")
		}
	}
}

func TestPanelRefreshDefaulting(t *testing.T) {
	cfg := &Config{Panels: []PanelConfig{
		{Type: "github", Source: "o/r"},
		{Type: "rss", Source: "https://x/feed", Refresh: "5m"},
	}}
	cfg.ApplyDefaults()
	if cfg.Panels[0].Refresh != "60s" {
		t.Errorf("expected default refresh 60s, got %q", cfg.Panels[0].Refresh)
	}
	if cfg.Panels[1].Refresh != "5m" {
		t.Errorf("expected explicit refresh preserved, got %q", cfg.Panels[1].Refresh)
	}
}

func TestEnabledPanelsDefaultsAndSynthesis(t *testing.T) {
	tru, fls := true, false

	// enabled omitted → included; explicit true → included; explicit false → excluded.
	cfg := &Config{Panels: []PanelConfig{
		{Type: "rss", Source: "a", Refresh: "60s"},
		{Type: "rss", Source: "b", Refresh: "60s", Enabled: &tru},
		{Type: "rss", Source: "c", Refresh: "60s", Enabled: &fls},
	}}
	if got := cfg.EnabledPanels(); len(got) != 2 {
		t.Fatalf("expected 2 enabled panels (nil + true), got %d: %#v", len(got), got)
	}

	// Legacy [github] block synthesizes a github panel when none is explicit.
	cfg2 := &Config{Github: GithubConfig{Repo: "o/r", Token: "t"}}
	got2 := cfg2.EnabledPanels()
	if len(got2) != 1 {
		t.Fatalf("expected 1 synthesized github panel, got %d", len(got2))
	}
	if got2[0].Type != "github" || got2[0].Source != "o/r" || got2[0].Token != "t" {
		t.Errorf("unexpected synthesized panel: %#v", got2[0])
	}
	if got2[0].Refresh != "60s" {
		t.Errorf("expected synthesized refresh 60s, got %q", got2[0].Refresh)
	}

	// No synthesis when an explicit github panel already exists.
	cfg3 := &Config{
		Github: GithubConfig{Repo: "o/r"},
		Panels: []PanelConfig{{Type: "github", Source: "other/repo", Refresh: "60s"}},
	}
	got3 := cfg3.EnabledPanels()
	if len(got3) != 1 {
		t.Fatalf("expected no duplicate github synthesis, got %d panels", len(got3))
	}
	if got3[0].Source != "other/repo" {
		t.Errorf("expected explicit github panel to win, got %q", got3[0].Source)
	}

	// Nothing configured → no panels.
	if got4 := (&Config{}).EnabledPanels(); len(got4) != 0 {
		t.Fatalf("expected no panels, got %d", len(got4))
	}
}

func TestValidatePanels(t *testing.T) {
	base := func() *Config {
		c := Default()
		c.Server.WebsocketURL = "wss://x"
		c.Server.WebhookURL = "https://discord.com/api/webhooks/x"
		c.Auth.Discord.ClientID = "id"
		return c
	}

	c := base()
	c.Panels = []PanelConfig{{Type: "", Source: "x"}}
	if err := c.Validate(); err == nil {
		t.Error("expected error for panel missing type")
	}

	c = base()
	c.Panels = []PanelConfig{{Type: "rss", Source: ""}}
	if err := c.Validate(); err == nil {
		t.Error("expected error for panel missing source")
	}

	c = base()
	c.Panels = []PanelConfig{{Type: "rss", Source: "https://x/feed"}}
	if err := c.Validate(); err != nil {
		t.Errorf("expected valid panels config, got %v", err)
	}
}

func TestLoadPluginsConfig(t *testing.T) {
	clearConfigEnvVars()
	os.Unsetenv("MARGA_PLUGINS_ENABLED")
	os.Unsetenv("MARGA_PLUGINS_DIR")

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

[plugins]
enabled = true
dir = "/custom/plugins"

[[plugins.entries]]
path = "roll.lua"
enabled = true

[[plugins.entries]]
path = "off.lua"
enabled = false
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

	if !cfg.Plugins.Enabled {
		t.Error("expected plugins.enabled to be true")
	}
	if cfg.Plugins.Dir != "/custom/plugins" {
		t.Errorf("expected plugins.dir '/custom/plugins', got %q", cfg.Plugins.Dir)
	}
	if len(cfg.Plugins.Entries) != 2 {
		t.Fatalf("expected 2 plugin entries, got %d", len(cfg.Plugins.Entries))
	}
	if cfg.Plugins.Entries[0].Path != "roll.lua" || !cfg.Plugins.Entries[0].Enabled {
		t.Errorf("unexpected first entry: %+v", cfg.Plugins.Entries[0])
	}
	if cfg.Plugins.Entries[1].Path != "off.lua" || cfg.Plugins.Entries[1].Enabled {
		t.Errorf("unexpected second entry: %+v", cfg.Plugins.Entries[1])
	}
}

func TestPluginsDefaultDir(t *testing.T) {
	clearConfigEnvVars()
	os.Unsetenv("MARGA_PLUGINS_ENABLED")
	os.Unsetenv("MARGA_PLUGINS_DIR")

	configHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)

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

[plugins]
enabled = true
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

	want := filepath.Join(configHome, "marga", "plugins")
	if cfg.Plugins.Dir != want {
		t.Errorf("expected default plugin dir %q, got %q", want, cfg.Plugins.Dir)
	}
}

func TestPluginsEnvOverrides(t *testing.T) {
	cfg := Default()
	t.Setenv("MARGA_PLUGINS_ENABLED", "true")
	t.Setenv("MARGA_PLUGINS_DIR", "/env/plugins")

	applyEnvOverrides(cfg)

	if !cfg.Plugins.Enabled {
		t.Error("expected plugins.enabled=true from MARGA_PLUGINS_ENABLED")
	}
	if cfg.Plugins.Dir != "/env/plugins" {
		t.Errorf("expected plugins.dir '/env/plugins' from env, got %q", cfg.Plugins.Dir)
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
