<p align="center">
  <img src="logo.png" alt="marga" width="120">
</p>

# marga

Terminal-native realtime chat client for Discord.

## Install

**macOS**
```bash
brew install kunthive-Labs/tap/marga
```

**Linux (deb/rpm/arch)**
```bash
# Debian/Ubuntu
curl -LO https://github.com/kunthive-Labs/Margana/releases/latest/download/marga_$(curl -s https://api.github.com/repos/kunthive-Labs/Margana/releases/latest | grep tag_name | cut -d'"' -f4 | sed 's/^v//')_linux_amd64.deb
sudo dpkg -i marga_*_linux_amd64.deb

# Arch
yay -S marga-bin

# Fedora/RHEL
sudo rpm -i marga_*_linux_amd64.rpm
```

**Windows**
```powershell
scoop bucket add kunthive-Labs https://github.com/kunthive-Labs/scoop-bucket
scoop install marga
```

**Go**
```bash
go install github.com/kunthive-Labs/Margana/cmd/marga@latest
```

## Quick Start

```bash
marga
```

First run opens your browser for Discord auth, then you're in the chat. No config needed.

## Keyboard Shortcuts

| Key | Action |
|-----|--------|
| `Enter` | Send message / run command |
| `Ctrl+C` / `Ctrl+Q` | Quit |
| `Ctrl+B` | Toggle channels sidebar |
| `Ctrl+Y` | Toggle users panel |
| `Ctrl+L` | Jump to bottom |
| `Ctrl+P` / `Ctrl+N` | Previous / next channel |
| `Ctrl+W` | Delete word backwards |
| `Ctrl+K` | Delete to end of line |
| `Ctrl+U` | Clear input |
| `Tab` | Complete `@user` or `#channel` |
| `↑` / `↓` | Scroll history |
| `PgUp` / `PgDn` | Scroll half page |

## Slash Commands

| Command | Description |
|---------|-------------|
| `/help` | Show available commands |
| `/join #channel` | Switch to a channel |
| `/history` | Load older messages |
| `/search <query>` | Search local history |
| `/clear` | Clear chat view |
| `/quit` | Exit |

## Config (optional)

Config file at `~/.config/marga/config.toml`. Everything is pre-configured — only edit if needed.

```toml
[general]
username = "anon"          # overridden by Discord auth

[server]
websocket_url = "wss://marga.kunthive.com:8443/ws"
webhook_url = "https://discord.com/api/webhooks/..."
relay_url = "https://marga.kunthive.com:8080"

[auth]
enabled = true             # Discord OAuth2 login on first run
provider = "discord"

[ui]
theme = "default"          # default, dracula, solarized
history_limit = 100
image_protocol = "auto"    # auto, iterm2, kitty, none
```

Override any field via env vars (`MARGA_USERNAME`, `MARGA_THEME`, etc.).

## Run locally

To run Marga Terminal locally, you also need to clone and set up the relay backend.

1. First, set up the relay server:
```bash
git clone https://github.com/kunthive-Labs/marga-discord-relay.git
cd marga-discord-relay
cp .env.example .env
# Edit .env with your Discord bot token and a strong API_KEY
make build
./bin/marga-discord-relay
```

2. Then, set up the terminal client in a new terminal window:
```bash
git clone https://github.com/kunthive-Labs/Margana.git
cd Margana
cp .env.example .env
# Edit .env to set MARGA_RELAY_API_KEY to match the relay's API_KEY
make build          # → bin/marga
./bin/marga
```

Requires [Go](https://go.dev) 1.21+.

## Privacy & architecture

marga uses a server-side bot + relay rather than connecting to Discord's gateway
as your account (which would be self-botting and against Discord's ToS). Your
OAuth tokens and permissions stay local; message content and metadata transit
the relay. For the full data-flow breakdown — and the one setup path that is the
exception — see [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md).

## Self-hosting

To point marga at your own Discord app, webhook, or relay instead of the hosted
defaults, see [docs/SELF_HOSTING.md](docs/SELF_HOSTING.md).

## License

[Apache License 2.0](LICENSE)
