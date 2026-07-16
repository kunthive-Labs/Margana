# Configuration reference

Marga is configured from three layers, applied in this order (later layers win):

1. **Defaults** — sensible built-in values (see [`internal/config`](../internal/config/config.go)).
2. **Config file** — a TOML file (see [paths](#file-locations)).
3. **Environment variables** — `MARGA_*` overrides, also loaded from a `.env`
   file in the working directory on startup.

Secrets (OAuth tokens, Matrix access tokens) are **never** written to the config
file. They live in your operating system's keyring. A few settings are also
controllable with [command-line flags](#command-line-flags), which take
precedence over everything for the options they cover (logging).

A fully-commented starting point is in [`config.example.toml`](../config.example.toml).

---

## File locations

Paths follow each platform's conventions and honor the XDG environment variables
on Linux/macOS.

| Purpose | Linux / macOS | Windows |
|---------|---------------|---------|
| Config file | `$XDG_CONFIG_HOME/marga/config.toml` or `~/.config/marga/config.toml` | `%APPDATA%\marga\config.toml` |
| Data dir (databases, Matrix sync, default log) | `$XDG_DATA_HOME/marga/` or `~/.local/share/marga/` | `%LOCALAPPDATA%\marga\` (falls back to `%APPDATA%`) |
| Default log file | `<data-dir>/marga.log` | `<data-dir>\marga.log` |
| Discord history DB | `<data-dir>/servers/<guild-id>.db` (or `<data-dir>/marga.db` if no server) | same |
| Other-network DB | `<data-dir>/<network-id>/<server>.db` | same |
| Matrix sync/crypto state | `<data-dir>/matrix/` | same |
| Image cache | `~/.cache/marga/images/` | `~/.cache/marga/images/` |

Pass `--config /path/to/config.toml` (or `-c`) to use a non-default config file.

### Secrets in the OS keyring

Tokens are stored via [`go-keyring`](https://github.com/zalando/go-keyring),
namespaced per network:

| Service | Keys |
|---------|------|
| `marga-discord` | `access_token`, `refresh_token` |
| `marga-matrix` | `access_token`, `device_id` |

Matrix end-to-end-encryption keys are kept in a local crypto database under the
Matrix state directory, with the pickle key in the keyring. See
[ARCHITECTURE.md](ARCHITECTURE.md) and [SECURITY.md](../SECURITY.md).

---

## Config file sections

### `[general]`

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `username` | string | `""` (→ `anon` if no Discord auth) | Display name shown next to your messages. Populated automatically by Discord auth. |
| `channel` | string | `general` | Channel joined on startup. |
| `discord_id`, `discord_username`, `discord_global_name`, `discord_avatar_url` | string | `""` | Discord identity, populated automatically after OAuth. Don't edit by hand. |
| `guild_id`, `guild_name` | string | `""` | The active Discord server. Managed by the server picker. |

### `[server]` (Discord relay)

Discord is relay-backed. Marga ships **no** hosted relay — point these at one you
run or trust. See [SELF_HOSTING.md](SELF_HOSTING.md). Matrix needs none of this.

| Key | Type | Required | Description |
|-----|------|----------|-------------|
| `websocket_url` | string | for Discord | Relay WebSocket endpoint for the live event stream (e.g. `wss://relay.example.com/ws`). |
| `webhook_url` | string | one of webhook/relay | Discord webhook for sending messages. |
| `relay_url` | string | one of webhook/relay | Relay REST base URL for history fetching and sending. |
| `api_key` | string | depends on relay | API key sent as `X-API-Key` to the relay. |
| `bot_client_id` | string | no | Discord bot client ID (used to build invite links during setup). |
| `web_setup_url` | string | no | **Deprecated and unused.** The web-setup onboarding path was removed; no token is ever sent to a web host. Retained only for backward compatibility. |

### `[auth]` / `[auth.discord]`

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `auth.enabled` | bool | `true` | Use Discord OAuth2 on first run to authenticate and populate `general.username`. |
| `auth.provider` | string | `discord` | Auth provider. Only `discord` is supported. |
| `auth.discord.client_id` | string | `""` | **Required for Discord.** Your Discord application's client ID. Register your own at <https://discord.com/developers/applications>. |
| `auth.discord.client_secret` | string | `""` | Only for confidential/private deployments. Public CLI clients use PKCE and leave this empty. |
| `auth.discord.redirect_url` | string | `http://127.0.0.1:53682/callback` | OAuth2 loopback redirect. Must match the Discord Developer Portal config. |
| `auth.discord.token_type`, `scope`, `expiry` | string | `""` | Populated after login. The access/refresh tokens themselves live in the keyring, not here. |

### `[ui]`

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `theme` | string | `default` | Color theme: `default`, `dracula`, or `solarized`. |
| `history_limit` | int | `100` | Messages fetched on startup. Values ≤ 0 reset to `100`. |
| `image_protocol` | string | `auto` | Inline-image protocol: `auto`, `iterm2`, `kitty`, or `none`. See the [note on image rendering](OPERATIONS.md#image-rendering). |

### `[github]`

Optional GitHub activity sidebar, refreshed every 60s when `repo` is set.

| Key | Type | Description |
|-----|------|-------------|
| `repo` | string | `owner/repo` to track (e.g. `kunthive-Labs/Margana`). Empty disables the panel. |
| `token` | string | GitHub PAT. Optional for public repos (raises rate limit), required for private repos. |

### `[notifications]`

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `bell_on_mention` | bool | `false` | Ring the terminal bell on `@`-mention in a non-muted channel. |
| `desktop` | bool | `false` | Show an OS desktop notification on a mention when Marga is unfocused (or the mention is in another channel). Env override: `MARGA_NOTIFY_DESKTOP`. |
| `muted_channels` | []string | `[]` | Channel names (case-insensitive) for which mentions and the bell are suppressed. |

### `[logging]`

Diagnostic logging is **off by default**. Because Marga is a full-screen TUI,
logs go to a **file**, never the terminal. See [TROUBLESHOOTING.md](TROUBLESHOOTING.md).

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `level` | string | `info` | Minimum level: `debug`, `info`, `warn`, `error`. |
| `file` | string | `""` (disabled) | Destination path. Empty disables logging. |
| `format` | string | `text` | `text` (human-readable) or `json` (one object per line). |

Quickest way to turn it on without editing config: `marga --debug` (debug level
to `<data-dir>/marga.log`), or `marga --log-file /tmp/marga.log`.

### `[[networks]]` (additional networks)

Discord is implicit via the blocks above. Other networks are added as repeated
`[[networks]]` tables. Secrets go in the keyring (`marga-<id>`), not the file.

| Key | Type | Description |
|-----|------|-------------|
| `id` | string | Stable identifier (e.g. `matrix`). Required. |
| `type` | string | Adapter type (e.g. `matrix`). Required. |
| `enabled` | bool | Whether to connect this network. |
| `homeserver` | string | Matrix: homeserver base URL (e.g. `https://matrix.org`). |
| `user_id` | string | Matrix: full MXID (e.g. `@you:matrix.org`). Non-secret. |

Matrix connects directly (no relay). On first run, set `MARGA_MATRIX_PASSWORD`
or log in interactively; the resulting token is cached in the keyring. Joined
spaces appear as switchable servers, and encrypted rooms are handled
transparently.

### `[[configured_guilds]]`

Managed automatically by the setup wizard and server picker (the list of Discord
servers you've configured). You normally won't edit this by hand. Each entry has
`id`, `name`, `channel`, and `configured`.

---

## Environment variables

Every `MARGA_*` variable overrides the corresponding config value. They are also
read from a `.env` file in the working directory (see [`.env.example`](../.env.example)).

| Variable | Overrides | Notes |
|----------|-----------|-------|
| `MARGA_USERNAME` | `general.username` | |
| `MARGA_CHANNEL` | `general.channel` | |
| `MARGA_GUILD_ID` | `general.guild_id` | |
| `MARGA_WEBSOCKET_URL` | `server.websocket_url` | |
| `MARGA_WEBHOOK_URL` | `server.webhook_url` | |
| `MARGA_RELAY_URL` | `server.relay_url` | |
| `MARGA_RELAY_API_KEY` | `server.api_key` | `MARGA_API_KEY` is a legacy alias. |
| `MARGA_BOT_CLIENT_ID` | `server.bot_client_id` | |
| `MARGA_WEB_SETUP_URL` | `server.web_setup_url` | deprecated / unused |
| `MARGA_AUTH_ENABLED` | `auth.enabled` | `true`/`false`. |
| `MARGA_AUTH_PROVIDER` | `auth.provider` | |
| `MARGA_DISCORD_CLIENT_ID` | `auth.discord.client_id` | |
| `MARGA_DISCORD_CLIENT_SECRET` | `auth.discord.client_secret` | |
| `MARGA_DISCORD_REDIRECT_URL` | `auth.discord.redirect_url` | |
| `MARGA_THEME` | `ui.theme` | |
| `MARGA_HISTORY_LIMIT` | `ui.history_limit` | Must parse as a positive int. |
| `MARGA_IMAGE_PROTOCOL` | `ui.image_protocol` | |
| `MARGA_NOTIFY_DESKTOP` | `notifications.desktop` | `true`/`false`. |
| `MARGA_GITHUB_TOKEN` | `github.token` | |
| `MARGA_GITHUB_REPO` | `github.repo` | |
| `MARGA_LOG_LEVEL` | `logging.level` | |
| `MARGA_LOG_FILE` | `logging.file` | |
| `MARGA_LOG_FORMAT` | `logging.format` | |
| `MARGA_MATRIX_PASSWORD` | — | Matrix login password for first connect / headless runs. Not stored; exchanged for a keyring token. |

---

## Command-line flags

| Flag | Description |
|------|-------------|
| `-c`, `--config PATH` | Use a specific config file. |
| `-s`, `--setup` | Force the interactive setup wizard. |
| `--debug` | Enable debug logging to the default log file. |
| `--log-file PATH` | Write logs to `PATH` (enables logging). |
| `--log-level LEVEL` | `debug`, `info`, `warn`, or `error`. Pairs with `--log-file` / `[logging].file`. |
| `-v`, `--version` | Print version and exit. |
| `-h`, `--help` | Print usage and exit. |

For logging, flags override the config file and environment. `--debug` is the
one-flag "just turn it on" switch.
