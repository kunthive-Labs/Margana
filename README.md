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
relay_url = "https://marga.kunthive.com:8443"

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

```bash
git clone https://github.com/kunthive-Labs/Margana.git
cd Margana
make build          # → bin/marga
./bin/marga
```

Requires [Go](https://go.dev) 1.21+.

## License

[Apache License 2.0](LICENSE)
