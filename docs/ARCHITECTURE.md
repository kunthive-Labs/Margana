# Architecture & data flow

This document explains how marga moves data around — what stays on your
machine, what is sent to Discord directly, and what passes through the relay.
It exists to answer a fair and recurring question: **why does marga use a
centralized relay instead of connecting straight to Discord's gateway?**

If you want to avoid the public relay entirely, see
[`SELF_HOSTING.md`](SELF_HOSTING.md).

## Why a relay at all?

marga **does not connect to Discord's gateway as your user account**, and that
is a deliberate choice.

Discord's API only sanctions gateway connections for **bots**. Driving a *user*
account programmatically against the gateway — what clients like endcord do — is
"self-botting." It violates the [Discord Terms of
Service](https://discord.com/terms) and is a well-documented cause of account
suspension. marga will not ship a self-bot.

So marga is built around a **server-side bot + webhooks**:

- A Discord **bot** (the relay) holds the gateway connection and fans out
  realtime events to connected marga clients over a WebSocket.
- Outbound messages are delivered via the bot / Discord **webhooks**.
- Your **user account never touches the gateway**, so there is no self-bot risk
  to your account.

The trade-off is centralization: with the public deployment, message content
and metadata pass through `marga.kunthive.com`. That is the cost of the
ToS-safe, zero-setup design. The escape hatch is self-hosting the relay (or
using a send-only webhook with no relay at all).

## What goes where

At runtime marga talks to two places: **Discord directly**, and **the relay**.

| Data | Destination | Notes |
|---|---|---|
| OAuth2 login (PKCE code exchange) | **Discord directly** | `internal/auth/discord/discord.go` |
| Access / refresh tokens (at rest) | **Your OS keyring** | Never written to the config file (`toml:"-"`); stored via `go-keyring` |
| Your profile (`/users/@me`) | **Discord directly** | Fetched with your token; not sent to the relay |
| Your guild list & permissions | **Discord directly** | `FetchUserGuilds`; admin filtering is computed client-side |
| Message **content** you send | **Relay** | `POST /message`, `/file`, `PATCH /message/:id` |
| Message **metadata** (channel, guild id, username, avatar url) | **Relay** | Same requests as above |
| Realtime stream (messages, typing, presence) | **Relay** | WebSocket; covers channels you subscribe to |
| Guild lookup during setup | **Relay** | `GET /api/guilds` (relay's own bot view, not your token) |
| Relay authentication | **API key** | `X-API-Key` header — a separate credential, *not* your Discord token |

### What stays on your machine

- **Your Discord OAuth tokens.** They live in your OS keyring (macOS Keychain,
  GNOME Keyring / libsecret, Windows Credential Manager) and are excluded from
  the TOML config. They are used to call `discord.com` **directly**.
- **Your permission levels.** Admin/owner detection
  (`HasAdminAccess`, `FilterAdminGuilds`) runs locally against the guild list
  Discord returns to you. The relay does not receive your permission map.

### What the relay sees — and currently stores

- **Message content and metadata** for messages you send and for the channels
  you subscribe to. On the public deployment this transits
  `marga.kunthive.com`. Treat this the way you would any hosted chat bridge.

The public relay
([`kunthive-Labs/marga-discord-relay`](https://github.com/kunthive-Labs/marga-discord-relay))
persists this to SQLite. As currently implemented:

- Message **content is stored in plaintext**, alongside author, timestamp, and
  channel/user/guild IDs. Nothing is encrypted or hashed.
- **There is no automatic retention limit, expiry, or pruning** — stored
  messages accumulate indefinitely.
- **There is no per-user deletion path.** A message row is only removed when the
  original message is deleted on Discord; there is no "delete my data" endpoint.

This is an honest description of present behavior, not a target state. Adding a
retention window and a deletion path to the relay are tracked follow-ups. If
this is unacceptable for your threat model, self-host the relay or run marga
send-only (see below).

### One important caveat: web-based setup

The first-run wizard offers two onboarding methods (`internal/setup/wizard.go`):

- **Terminal setup (recommended, default):** everything stays local; your token
  is used only against Discord and your own relay.
- **Web browser setup:** marga opens
  `https://marga.kunthive.com/terminal-setup#token=<access_token>` so the web
  wizard can finish configuration for you. **In this path your Discord access
  token is handed to the web setup host** (in the URL fragment).

If you do not want your token to reach `marga.kunthive.com`, use **terminal
setup**, or point `server.web_setup_url` at your own host
(`MARGA_WEB_SETUP_URL`). Self-hosters should override it.

## Reducing or removing reliance on the relay

marga is a client; every endpoint is configurable.

- **Self-host the relay** — point `server.websocket_url` and `server.relay_url`
  at your own marga-compatible relay. See [`SELF_HOSTING.md`](SELF_HOSTING.md).
- **Send-only, no relay** — leave `server.relay_url` empty and set
  `server.webhook_url`. marga sends via a Discord webhook directly; history and
  realtime receive are disabled.
- **Your own Discord app** — register your own OAuth2 application so even login
  goes through credentials you control.
- **Override web setup** — set `server.web_setup_url` (or use terminal setup) so
  no token is sent to the public web wizard.

## Summary

marga trades a direct gateway connection for a bot-based relay specifically to
keep your Discord account ToS-compliant and setup friction-free. Your tokens and
permissions stay local (the web-setup path being the one exception to call out);
message content and metadata transit the relay. If that centralization is not
acceptable for your threat model, the relay, webhook, OAuth app, and web-setup
host are all replaceable with infrastructure you control.
