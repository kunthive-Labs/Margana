// Package config loads and persists Marga's configuration. It layers a TOML
// config file, MARGA_* environment overrides, and OS-keyring secrets, and
// resolves the platform-specific paths for config, data, logs, and per-server
// databases. See docs/CONFIGURATION.md for the full reference.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/kunthive-Labs/Margana/internal/network/credstore"
)

type GithubConfig struct {
	Token string `toml:"token"`
	Repo  string `toml:"repo"`
}

type Config struct {
	General          GeneralConfig      `toml:"general"`
	Server           ServerConfig       `toml:"server"`
	Auth             AuthConfig         `toml:"auth"`
	UI               UIConfig           `toml:"ui"`
	Github           GithubConfig       `toml:"github"`
	Notifications    NotificationConfig `toml:"notifications"`
	Logging          LoggingConfig      `toml:"logging"`
	ConfiguredGuilds []GuildEntry       `toml:"configured_guilds"`
	Networks         []NetworkConfig    `toml:"networks"`
}

// NetworkConfig describes one chat network connection. Discord is implicit via
// the legacy [auth.discord]/[server] blocks; additional networks (Matrix, ...)
// are configured as [[networks]] entries. Secrets live in the OS keyring, not
// here.
type NetworkConfig struct {
	ID      string `toml:"id"`
	Type    string `toml:"type"`
	Enabled bool   `toml:"enabled"`
	// Matrix-specific (non-secret).
	Homeserver string `toml:"homeserver,omitempty"`
	UserID     string `toml:"user_id,omitempty"`
}

// EnabledNetworks returns the configured networks, synthesizing the implicit
// Discord entry when no explicit [[networks]] are present. This keeps existing
// single-network configs working untouched.
func (c *Config) EnabledNetworks() []NetworkConfig {
	var out []NetworkConfig
	hasDiscord := false
	for _, n := range c.Networks {
		if !n.Enabled {
			continue
		}
		if n.Type == "discord" || n.ID == "discord" {
			hasDiscord = true
		}
		out = append(out, n)
	}
	if !hasDiscord && c.UsesDiscordAuth() {
		out = append([]NetworkConfig{{ID: "discord", Type: "discord", Enabled: true}}, out...)
	}
	return out
}

type NotificationConfig struct {
	// BellOnMention emits the terminal bell when you are @-mentioned in a
	// channel that isn't muted.
	BellOnMention bool `toml:"bell_on_mention"`
	// MutedChannels suppresses mention notifications (and the bell) for the
	// listed channel names. Case-insensitive.
	MutedChannels []string `toml:"muted_channels"`
}

type GeneralConfig struct {
	Username          string `toml:"username"`
	Channel           string `toml:"channel"`
	DiscordID         string `toml:"discord_id"`
	DiscordUsername   string `toml:"discord_username"`
	DiscordGlobalName string `toml:"discord_global_name"`
	DiscordAvatarURL  string `toml:"discord_avatar_url"`
	GuildID           string `toml:"guild_id"`
	GuildName         string `toml:"guild_name"`
}

type GuildEntry struct {
	ID         string `toml:"id"`
	Name       string `toml:"name"`
	Channel    string `toml:"channel"`
	Configured bool   `toml:"configured"`
}

type ServerConfig struct {
	WebsocketURL string `toml:"websocket_url"`
	WebhookURL   string `toml:"webhook_url"`
	RelayURL     string `toml:"relay_url"`
	APIKey       string `toml:"api_key"`
	BotClientID  string `toml:"bot_client_id"`
	WebSetupURL  string `toml:"web_setup_url"`
}

type AuthConfig struct {
	Enabled  bool              `toml:"enabled"`
	Provider string            `toml:"provider"`
	Discord  DiscordAuthConfig `toml:"discord"`
}

type DiscordAuthConfig struct {
	ClientID     string `toml:"client_id"`
	ClientSecret string `toml:"client_secret"`
	RedirectURL  string `toml:"redirect_url"`
	AccessToken  string `toml:"-"`
	RefreshToken string `toml:"-"`
	TokenType    string `toml:"token_type"`
	Scope        string `toml:"scope"`
	Expiry       string `toml:"expiry"`
}

type UIConfig struct {
	Theme         string `toml:"theme"`
	HistoryLimit  int    `toml:"history_limit"`
	ImageProtocol string `toml:"image_protocol"`
}

// LoggingConfig controls Marga's diagnostic logging. Logging is disabled unless
// a destination is set here (File) or on the command line (--debug/--log-file).
// Because Marga is a full-screen TUI, logs are written to a file, never the
// terminal. See docs/CONFIGURATION.md and docs/TROUBLESHOOTING.md.
type LoggingConfig struct {
	// Level is the minimum severity to record: debug, info, warn, or error.
	// Defaults to info when a destination is set but no level is given.
	Level string `toml:"level"`
	// File is the destination path. Empty disables logging. Relative paths are
	// resolved against the working directory.
	File string `toml:"file"`
	// Format is "text" (human-readable, default) or "json" (one object per
	// line, for log processors).
	Format string `toml:"format"`
}

func Default() *Config {
	return &Config{
		General: GeneralConfig{
			Username: "",
			Channel:  "general",
		},
		Server: ServerConfig{
			// Endpoints are intentionally empty: Marga ships no hosted relay.
			// Point these at your own relay (config or MARGA_* env) before using
			// Discord. See docs/SELF_HOSTING.md.
			WebsocketURL: "",
			WebhookURL:   "",
			RelayURL:     "",
			BotClientID:  "",
			WebSetupURL:  "",
		},
		Auth: AuthConfig{
			Enabled:  true,
			Provider: "discord",
			Discord: DiscordAuthConfig{
				// No default client_id: register your own Discord application
				// and set auth.discord.client_id (or MARGA_DISCORD_CLIENT_ID).
				ClientID:     "",
				ClientSecret: "",
				RedirectURL:  "http://127.0.0.1:53682/callback",
			},
		},
		UI: UIConfig{
			Theme:        "default",
			HistoryLimit: 100,
		},
	}
}

func (c *Config) ApplyDefaults() {
	if c.General.Channel == "" {
		c.General.Channel = "general"
	}
	if c.UI.Theme == "" {
		c.UI.Theme = "default"
	}
	if c.UI.HistoryLimit <= 0 {
		c.UI.HistoryLimit = 100
	}
	if c.Logging.Format == "" {
		c.Logging.Format = "text"
	}
}

func configDir() (string, error) {
	if runtime.GOOS == "windows" {
		appData := os.Getenv("APPDATA")
		if appData == "" {
			return "", fmt.Errorf("APPDATA environment variable not set")
		}
		return filepath.Join(appData, "marga"), nil
	}
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "marga"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}
	return filepath.Join(home, ".config", "marga"), nil
}

func DefaultConfigPath() (string, error) {
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.toml"), nil
}

func dataDir() (string, error) {
	if runtime.GOOS == "windows" {
		localAppData := os.Getenv("LOCALAPPDATA")
		if localAppData == "" {
			localAppData = os.Getenv("APPDATA")
		}
		if localAppData == "" {
			return "", fmt.Errorf("cannot determine Windows data directory")
		}
		return filepath.Join(localAppData, "marga"), nil
	}
	if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
		return filepath.Join(xdg, "marga"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}
	return filepath.Join(home, ".local", "share", "marga"), nil
}

// DefaultLogPath returns the default diagnostic log destination,
// <dataDir>/marga.log. It is used when logging is enabled (e.g. via --debug)
// without an explicit file. The file is only created when logging is active.
func DefaultLogPath() (string, error) {
	dir, err := dataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "marga.log"), nil
}

func GuildDBPath(guildID string) (string, error) {
	dir, err := dataDir()
	if err != nil {
		return "", err
	}
	if guildID == "" {
		return filepath.Join(dir, "marga.db"), nil
	}
	return filepath.Join(dir, "servers", guildID+".db"), nil
}

// NetworkDBPath returns the local SQLite path for a (network, server) pair.
// Discord preserves the historical servers/<id>.db layout for continuity;
// other networks are isolated under <network>/<server>.db.
func NetworkDBPath(networkID, serverID string) (string, error) {
	if networkID == "" || networkID == "discord" {
		return GuildDBPath(serverID)
	}
	dir, err := dataDir()
	if err != nil {
		return "", err
	}
	name := serverID
	if name == "" {
		name = "default"
	}
	return filepath.Join(dir, networkID, name+".db"), nil
}

// NetworkStatePath returns a file path for a network adapter's non-DB state
// (e.g. the Matrix /sync token cache).
func NetworkStatePath(networkID, name string) (string, error) {
	dir, err := dataDir()
	if err != nil {
		return "", err
	}
	if name == "" {
		name = "state"
	}
	return filepath.Join(dir, networkID, name+".json"), nil
}

// ConfigPathFromArgs returns the config file path, honoring -c/-config (in
// single- or double-dash, space- or =-separated form) anywhere on the command
// line. It deliberately ignores every other flag: Load calls this on each
// startup, so a strict parser here would reject any new CLI flag (this is why
// `marga --setup` once failed at launch). When no flag is present it falls back
// to the platform default path.
func ConfigPathFromArgs(args []string) (string, error) {
	if p := configPathFlag(args); p != "" {
		return p, nil
	}

	defaultPath, err := DefaultConfigPath()
	if err != nil {
		return "", fmt.Errorf("resolving default config path: %w", err)
	}
	return defaultPath, nil
}

// configPathFlag scans args for -c/-config and returns the last value seen, or
// "" if absent. Unknown flags are skipped rather than rejected.
func configPathFlag(args []string) string {
	isKey := func(s string) bool {
		return s == "-c" || s == "--c" || s == "-config" || s == "--config"
	}
	path := ""
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case isKey(a):
			if i+1 < len(args) {
				path = args[i+1]
				i++
			}
		case strings.HasPrefix(a, "-c="):
			path = strings.TrimPrefix(a, "-c=")
		case strings.HasPrefix(a, "--c="):
			path = strings.TrimPrefix(a, "--c=")
		case strings.HasPrefix(a, "-config="):
			path = strings.TrimPrefix(a, "-config=")
		case strings.HasPrefix(a, "--config="):
			path = strings.TrimPrefix(a, "--config=")
		}
	}
	return path
}

func Load() (*Config, error) {
	cfg := Default()

	configPath, err := ConfigPathFromArgs(os.Args[1:])
	if err != nil {
		return nil, err
	}

	if _, err := os.Stat(configPath); err == nil {
		if _, err := toml.DecodeFile(configPath, cfg); err != nil {
			return nil, fmt.Errorf("parsing config file %s: %w", configPath, err)
		}
	}

	cfg.ApplyDefaults()
	applyEnvOverrides(cfg)

	// Load Discord tokens from the keyring, migrating any legacy un-namespaced
	// entries into the "marga-discord" namespace first.
	_ = credstore.MigrateLegacyDiscord()
	if token, err := credstore.Get("discord", "access_token"); err == nil {
		cfg.Auth.Discord.AccessToken = token
	}
	if token, err := credstore.Get("discord", "refresh_token"); err == nil {
		cfg.Auth.Discord.RefreshToken = token
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

func (c *Config) Save(path string) error {
	c.ApplyDefaults()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating config directory: %w", err)
	}

	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("creating config file: %w", err)
	}
	defer f.Close()

	if err := toml.NewEncoder(f).Encode(c); err != nil {
		return fmt.Errorf("encoding config: %w", err)
	}

	// Save Discord tokens to the network-namespaced keyring.
	if c.Auth.Discord.AccessToken != "" {
		_ = credstore.Set("discord", "access_token", c.Auth.Discord.AccessToken)
	} else {
		_ = credstore.Delete("discord", "access_token")
	}
	if c.Auth.Discord.RefreshToken != "" {
		_ = credstore.Set("discord", "refresh_token", c.Auth.Discord.RefreshToken)
	} else {
		_ = credstore.Delete("discord", "refresh_token")
	}

	return nil
}

func applyEnvOverrides(cfg *Config) {
	if v := os.Getenv("MARGA_USERNAME"); v != "" {
		cfg.General.Username = v
	}
	if v := os.Getenv("MARGA_CHANNEL"); v != "" {
		cfg.General.Channel = v
	}
	if v := os.Getenv("MARGA_WEBSOCKET_URL"); v != "" {
		cfg.Server.WebsocketURL = v
	}
	if v := os.Getenv("MARGA_WEBHOOK_URL"); v != "" {
		cfg.Server.WebhookURL = v
	}
	if v := os.Getenv("MARGA_RELAY_URL"); v != "" {
		cfg.Server.RelayURL = v
	}
	if v := os.Getenv("MARGA_RELAY_API_KEY"); v != "" {
		cfg.Server.APIKey = v
	} else if v := os.Getenv("MARGA_API_KEY"); v != "" {
		// MARGA_API_KEY is a legacy alias; MARGA_RELAY_API_KEY is preferred.
		cfg.Server.APIKey = v
	}
	if v := os.Getenv("MARGA_AUTH_ENABLED"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			cfg.Auth.Enabled = b
		}
	}
	if v := os.Getenv("MARGA_AUTH_PROVIDER"); v != "" {
		cfg.Auth.Provider = v
	}
	if v := os.Getenv("MARGA_DISCORD_CLIENT_ID"); v != "" {
		cfg.Auth.Discord.ClientID = v
	}
	if v := os.Getenv("MARGA_DISCORD_CLIENT_SECRET"); v != "" {
		cfg.Auth.Discord.ClientSecret = v
	}
	if v := os.Getenv("MARGA_DISCORD_REDIRECT_URL"); v != "" {
		cfg.Auth.Discord.RedirectURL = v
	}
	if v := os.Getenv("MARGA_THEME"); v != "" {
		cfg.UI.Theme = v
	}
	if v := os.Getenv("MARGA_HISTORY_LIMIT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.UI.HistoryLimit = n
		}
	}
	if v := os.Getenv("MARGA_IMAGE_PROTOCOL"); v != "" {
		cfg.UI.ImageProtocol = v
	}
	if v := os.Getenv("MARGA_GITHUB_TOKEN"); v != "" {
		cfg.Github.Token = v
	}
	if v := os.Getenv("MARGA_GITHUB_REPO"); v != "" {
		cfg.Github.Repo = v
	}
	if v := os.Getenv("MARGA_GUILD_ID"); v != "" {
		cfg.General.GuildID = v
	}
	if v := os.Getenv("MARGA_BOT_CLIENT_ID"); v != "" {
		cfg.Server.BotClientID = v
	}
	if v := os.Getenv("MARGA_WEB_SETUP_URL"); v != "" {
		cfg.Server.WebSetupURL = v
	}
	if v := os.Getenv("MARGA_LOG_LEVEL"); v != "" {
		cfg.Logging.Level = v
	}
	if v := os.Getenv("MARGA_LOG_FILE"); v != "" {
		cfg.Logging.File = v
	}
	if v := os.Getenv("MARGA_LOG_FORMAT"); v != "" {
		cfg.Logging.Format = v
	}
}

func (c *Config) Validate() error {
	if c.General.Username == "" && !c.UsesDiscordAuth() {
		c.General.Username = "anon"
	}
	// Relay endpoints and a Discord application are only required when Discord
	// is in use. Matrix (and other direct networks) connect without a relay, so
	// a Matrix-only config needs none of these.
	if c.usesDiscord() {
		if c.Server.WebsocketURL == "" {
			return fmt.Errorf("missing server.websocket_url — point it at your relay (config or MARGA_WEBSOCKET_URL); see docs/SELF_HOSTING.md")
		}
		if c.Server.WebhookURL == "" && c.Server.RelayURL == "" {
			return fmt.Errorf("missing server.webhook_url and server.relay_url — set at least one (config or MARGA_RELAY_URL)")
		}
		if c.UsesDiscordAuth() {
			if c.Auth.Discord.ClientID == "" {
				return fmt.Errorf("missing auth.discord.client_id — register a Discord application and set it (config or MARGA_DISCORD_CLIENT_ID); see docs/SELF_HOSTING.md")
			}
			if c.Auth.Discord.RedirectURL == "" {
				return fmt.Errorf("missing auth.discord.redirect_url — set it (config or MARGA_DISCORD_REDIRECT_URL)")
			}
		}
	}
	for _, n := range c.Networks {
		if n.ID == "" {
			return fmt.Errorf("each [[networks]] entry needs an id")
		}
		if n.Type == "" {
			return fmt.Errorf("network %q needs a type", n.ID)
		}
	}
	return nil
}

func (c *Config) UsesDiscordAuth() bool {
	return c.Auth.Enabled && c.Auth.Provider == "discord"
}

// usesDiscord reports whether Discord is active — either via the implicit
// [auth.discord] flow or an explicit enabled [[networks]] entry. Used to decide
// whether relay endpoints are required.
func (c *Config) usesDiscord() bool {
	if c.UsesDiscordAuth() {
		return true
	}
	for _, n := range c.Networks {
		if n.Enabled && (n.Type == "discord" || n.ID == "discord") {
			return true
		}
	}
	return false
}

func (c *Config) DiscordTokenExpiry() (time.Time, error) {
	if c.Auth.Discord.Expiry == "" {
		return time.Time{}, nil
	}
	return time.Parse(time.RFC3339, c.Auth.Discord.Expiry)
}
