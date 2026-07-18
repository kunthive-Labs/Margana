<p align="center">
  <img src="logo.png" alt="Marga" width="160">
</p>

<p align="center">
  <img src="assets/demo.gif" alt="Marga - one terminal UI for Discord and Matrix" width="820">
</p>

<p align="center">
  <em>Try it now, no setup required:</em> <code>marga --demo</code>
</p>

# Marga

Terminal-native, multi-network realtime chat. One TUI for several chat
networks — Discord (through a relay bot) and Matrix (connected directly) today,
with a pluggable adapter model for more.

> **Pre-1.0 software** (`v0.1.0`). Marga is usable day to day, but config keys,
> keybindings, and the relay contract may still change between releases until
> 1.0 — pin a version if you need stability.

## Networks

Marga talks to each network through a `Network` adapter (`internal/network`);
the TUI consumes one unified event stream and never sees protocol-specific wire
formats. Two adapter styles exist:

| Network | Style | Notes |
|---------|-------|-------|
| **Discord** | Relay-backed | Discord forbids driving a *user* account against its gateway (self-botting). Marga connects through a server-side bot + webhook relay instead, so your account stays ToS-compliant. Requires a running relay. |
| **Matrix** | Direct | Connects straight to the homeserver via the client-server API (mautrix-go). No relay, no third party. The access token lives in your OS keyring; `/sync` state is cached locally. **End-to-end encrypted rooms are decrypted and encrypted transparently** (Olm/Megolm via mautrix, keys stored locally), and **spaces are surfaced as servers**. |

See [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) for the full data-flow and the
network-neutral relay contract.

## Install

### Homebrew (macOS · Linux)

```bash
brew install kunthive-Labs/tap/marga
```

### Scoop (Windows)

```powershell
scoop bucket add kunthive-Labs https://github.com/kunthive-Labs/scoop-bucket
scoop install marga
```

### Linux packages

Grab the `.deb`, `.rpm`, `.apk`, or Arch package for your architecture from the
[latest release](https://github.com/kunthive-Labs/Margana/releases/latest):

```bash
sudo dpkg -i  marga_*_linux_amd64.deb   # Debian / Ubuntu
sudo rpm  -i  marga_*_linux_amd64.rpm   # Fedora / RHEL
```

### Pre-built binary

Download the archive for your OS/arch from the
[Releases page](https://github.com/kunthive-Labs/Margana/releases/latest),
verify it against `checksums.txt`, extract, and put `marga` on your `PATH`.

### From source

```bash
go install -tags goolm github.com/kunthive-Labs/Margana/cmd/marga@latest
```

or clone and `make build` (→ `bin/marga`). Requires [Go](https://go.dev) 1.25+.
The `goolm` build tag selects mautrix's pure-Go Olm backend for Matrix
end-to-end encryption, so no system `libolm` or C toolchain is needed
(`make build` sets it for you). Without the tag the build falls back to CGo
`libolm`.

> Packages are published by tagging a release — see
> [docs/RELEASING.md](docs/RELEASING.md). If a one-liner above 404s, that
> release hasn't been cut yet; build from source in the meantime.

## Quick start

```bash
marga
```

First run launches an interactive setup wizard that asks which network to
connect. Pick **Matrix** for a zero-setup start.

- **Matrix** *(works now, no server needed)* — choose it in the wizard and enter
  your homeserver and user id. Marga prompts for your password in the terminal
  (read without echo) on first connect and stores the resulting token in your OS
  keyring; for headless/CI runs, set `MARGA_MATRIX_PASSWORD` instead. Encrypted
  rooms work out of the box, and joined spaces appear as switchable servers.
- **Discord** *(advanced)* — needs a self-hosted relay and a registered Discord
  application. The default endpoints (`marga.kunthive.com`) are **placeholders**;
  stand up your own relay and set the URLs in config first. See
  [docs/SELF_HOSTING.md](docs/SELF_HOSTING.md).

## Keyboard shortcuts

| Key | Action |
|-----|--------|
| `Enter` | Send message / run command |
| `Ctrl+C` / `Ctrl+Q` | Quit |
| `Ctrl+B` | Toggle channels sidebar |
| `Ctrl+Y` | Toggle users panel |
| `Ctrl+L` | Jump to bottom |
| `Ctrl+P` / `Ctrl+N` | Previous / next channel |
| `Alt+1`…`Alt+9` | Jump to the Nth channel |
| `Ctrl+T` | Switch to the next network |
| `Ctrl+K` | Command palette — jump to any channel / command / network |
| `Ctrl+W` | Delete word backwards |
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

## Command-line flags

| Flag | Description |
|------|-------------|
| `-c`, `--config PATH` | Use a specific config file |
| `-s`, `--setup` | Force the interactive setup wizard |
| `--debug` | Enable debug logging to the default log file |
| `--log-file PATH` | Write logs to `PATH` (enables logging) |
| `--log-level LEVEL` | `debug`, `info`, `warn`, or `error` |
| `-v`, `--version` | Print version and exit |
| `-h`, `--help` | Print usage and exit |

## Logging & troubleshooting

Logging is **off by default**. Marga is a full-screen TUI, so logs are written
to a **file**, never the terminal. Turn it on when something needs diagnosing:

```bash
marga --debug                    # debug level → <data-dir>/marga.log
tail -f ~/.local/share/marga/marga.log
```

See [docs/TROUBLESHOOTING.md](docs/TROUBLESHOOTING.md) for common issues and
[docs/OPERATIONS.md](docs/OPERATIONS.md) for headless use, log rotation,
backups, and upgrades.

## Configuration

Config file at `~/.config/marga/config.toml`. Override any field via env vars
(`MARGA_USERNAME`, `MARGA_THEME`, …). The complete reference — every field, env
var, file path, and the keyring layout — is in
[docs/CONFIGURATION.md](docs/CONFIGURATION.md).

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
