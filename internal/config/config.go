package config

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"time"
	"github.com/BurntSushi/toml"
	"github.com/zalando/go-keyring"
)

type GithubConfig struct {
	Token string `toml:"token"`
	Repo  string `toml:"repo"`
}

type Config struct {
	General          GeneralConfig `toml:"general"`
	Server           ServerConfig  `toml:"server"`
	Auth             AuthConfig    `toml:"auth"`
	UI               UIConfig      `toml:"ui"`
	Github           GithubConfig  `toml:"github"`
	ConfiguredGuilds []GuildEntry  `toml:"configured_guilds"`
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

func Default() *Config {
	return &Config{
		General: GeneralConfig{
			Username: "",
			Channel:  "general",
		},
		Server: ServerConfig{
			WebsocketURL: "wss://marga.kunthive.com:8443/ws",
			WebhookURL:   "",
			RelayURL:     "https://marga.kunthive.com:8443",
			BotClientID:  "1503351063468572754",
			WebSetupURL:  "https://marga.kunthive.com",
		},
		Auth: AuthConfig{
			Enabled:  true,
			Provider: "discord",
			Discord: DiscordAuthConfig{
				ClientID:     "1503351063468572754",
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
	if c.Server.RelayURL == "" {
		c.Server.RelayURL = "https://marga.kunthive.com:8443"
	}
	if c.Server.BotClientID == "" {
		c.Server.BotClientID = "1503351063468572754"
	}
	if c.Server.WebSetupURL == "" {
		c.Server.WebSetupURL = "https://marga.kunthive.com"
	}
	if c.UI.Theme == "" {
		c.UI.Theme = "default"
	}
	if c.UI.HistoryLimit <= 0 {
		c.UI.HistoryLimit = 100
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

func ConfigPathFromArgs(args []string) (string, error) {
	configPath := ""

	fs := flag.NewFlagSet("marga", flag.ContinueOnError)
	fs.StringVar(&configPath, "config", "", "path to config file")
	fs.StringVar(&configPath, "c", "", "path to config file (shorthand)")

	if err := fs.Parse(args); err != nil {
		return "", fmt.Errorf("parsing CLI flags: %w", err)
	}

	if configPath != "" {
		return configPath, nil
	}

	defaultPath, err := DefaultConfigPath()
	if err != nil {
		return "", fmt.Errorf("resolving default config path: %w", err)
	}
	return defaultPath, nil
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

	// Load tokens from keyring
	if token, err := keyring.Get("marga", "access_token"); err == nil {
		cfg.Auth.Discord.AccessToken = token
	}
	if token, err := keyring.Get("marga", "refresh_token"); err == nil {
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

	// Save tokens to keyring
	if c.Auth.Discord.AccessToken != "" {
		_ = keyring.Set("marga", "access_token", c.Auth.Discord.AccessToken)
	} else {
		_ = keyring.Delete("marga", "access_token")
	}
	if c.Auth.Discord.RefreshToken != "" {
		_ = keyring.Set("marga", "refresh_token", c.Auth.Discord.RefreshToken)
	} else {
		_ = keyring.Delete("marga", "refresh_token")
	}

	return nil
}

func tomlDecodeFile(path string, cfg *Config) (interface{}, error) {
	return toml.DecodeFile(path, cfg)
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
	if v := os.Getenv("MARGA_API_KEY"); v != "" {
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
	if cfg.Server.WebSetupURL == "" {
		cfg.Server.WebSetupURL = "https://marga.kunthive.com"
	}
}

func (c *Config) Validate() error {
	if c.General.Username == "" && !c.UsesDiscordAuth() {
		c.General.Username = "anon"
	}
	if c.Server.WebsocketURL == "" {
		return fmt.Errorf("missing server.websocket_url — set it in config, or use MARGA_WEBSOCKET_URL env var")
	}
	if c.Server.WebhookURL == "" && c.Server.RelayURL == "" {
		return fmt.Errorf("missing server.webhook_url and server.relay_url — set at least one in config")
	}
	if c.Auth.Enabled {
		if c.Auth.Provider != "discord" {
			return fmt.Errorf("unsupported auth.provider %q — currently only \"discord\" is supported", c.Auth.Provider)
		}
		if c.Auth.Discord.ClientID == "" {
			return fmt.Errorf("missing auth.discord.client_id — set it in config, or use MARGA_DISCORD_CLIENT_ID env var")
		}
		if c.Auth.Discord.RedirectURL == "" {
			return fmt.Errorf("missing auth.discord.redirect_url — set it in config, or use MARGA_DISCORD_REDIRECT_URL env var")
		}
	}
	return nil
}

func (c *Config) UsesDiscordAuth() bool {
	return c.Auth.Enabled && c.Auth.Provider == "discord"
}

func (c *Config) DiscordTokenExpiry() (time.Time, error) {
	if c.Auth.Discord.Expiry == "" {
		return time.Time{}, nil
	}
	return time.Parse(time.RFC3339, c.Auth.Discord.Expiry)
}
