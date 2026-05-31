<p align="center">
  <img src="logo.png" alt="Marga" width="120">
</p>

# Marga

Terminal-native, multi-network realtime chat. One TUI for several chat
networks — Discord (through a relay bot) and Matrix (connected directly) today,
with a pluggable adapter model for more.

## Networks

Marga talks to each network through a `Network` adapter (`internal/network`);
the TUI consumes one unified event stream and never sees protocol-specific wire
formats. Two adapter styles exist:

| Network | Style | Notes |
|---------|-------|-------|
| **Discord** | Relay-backed | Discord forbids driving a *user* account against its gateway (self-botting). Marga connects through a server-side bot + webhook relay instead, so your account stays ToS-compliant. Requires a running relay. |
| **Matrix** | Direct | Connects straight to the homeserver via the client-server API (mautrix-go). No relay, no third party. The access token lives in your OS keyring; `/sync` state is cached locally. |

See [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) for the full data-flow and the
network-neutral relay contract.

## Install

Marga is built from source. (Pre-built packages / Homebrew tap / Scoop bucket
are not published yet for this org.)

```bash
git clone https://github.com/kunthive-Labs/Margana.git
cd Margana
make build          # → bin/marga
./bin/marga
```

Or install the binary directly with Go:

```bash
go install github.com/kunthive-Labs/Margana/cmd/marga@latest
```

Requires [Go](https://go.dev) 1.25+.

## Quick start

```bash
marga
```

First run launches an interactive setup wizard.

- **Discord** needs a relay. The default endpoints (`marga.kunthive.com`) are
  **placeholders** — stand up your own relay (or point at one you trust) and set
  the URLs in config. See [docs/SELF_HOSTING.md](docs/SELF_HOSTING.md).
- **Matrix** needs no relay. Add a `[[networks]]` block (below) with your
  homeserver and user id; on first connect, supply your password once via
  `MARGA_MATRIX_PASSWORD` and Marga stores the resulting token in your keyring.

## Keyboard shortcuts

| Key | Action |
|-----|--------|
| `Enter` | Send message / run command |
| `Ctrl+C` / `Ctrl+Q` | Quit |
| `Ctrl+B` | Toggle channels sidebar |
| `Ctrl+Y` | Toggle users panel |
| `Ctrl+L` | Jump to bottom |
| `Ctrl+P` / `Ctrl+N` | Previous / next channel |
| `Ctrl+T` | Switch to the next network |
| `Ctrl+W` | Delete word backwards |
| `Ctrl+K` | Delete to end of line |
| `Ctrl+U` | Clear input |
| `Tab` | Complete `@user` or `#channel` |
| `↑` / `↓` | Scroll history |
| `PgUp` / `PgDn` | Scroll half page |

## Slash commands

| Command | Description |
|---------|-------------|
| `/help` | Show available commands |
| `/network [name]` | List networks, or switch the active one |
| `/join #channel` | Switch to a channel |
| `/history` | Load older messages |
| `/search <query>` | Search local history |
| `/clear` | Clear chat view |
| `/quit` | Exit |

## Configuration

Config file at `~/.config/marga/config.toml`. Override any field via env vars
(`MARGA_USERNAME`, `MARGA_THEME`, …).

```toml
[general]
username = "anon"          # overridden by Discord auth

[server]
# Placeholder endpoints — replace with your own relay.
websocket_url = "wss://marga.kunthive.com:8443/ws"
webhook_url   = "https://discord.com/api/webhooks/..."
relay_url     = "https://marga.kunthive.com:8080"

[auth]
enabled  = true            # Discord OAuth2 login on first run
provider = "discord"

[ui]
theme          = "default" # default, dracula, solarized
history_limit  = 100
image_protocol = "auto"    # auto, iterm2, kitty, none

# Additional networks beyond Discord are configured as [[networks]] blocks.
# Secrets live in the OS keyring, not here.
[[networks]]
id         = "matrix"
type       = "matrix"
enabled    = true
homeserver = "https://matrix.org"
user_id    = "@you:matrix.org"
```

## Privacy & architecture

For relay-backed networks (Discord), message content and metadata transit the
relay you configure; your OAuth tokens and permissions stay local. Direct
networks (Matrix) bypass any relay entirely. The full breakdown — including the
one setup path that sends a token to a web host — is in
[docs/ARCHITECTURE.md](docs/ARCHITECTURE.md).

## Self-hosting

To point Marga at your own Discord app, webhook, or relay, see
[docs/SELF_HOSTING.md](docs/SELF_HOSTING.md).

## License

[Apache License 2.0](LICENSE)
