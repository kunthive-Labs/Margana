# Self-hosting marga

By default marga talks to the public infrastructure run by kunthive-Labs
(`marga.kunthive.com`) and uses a pre-registered Discord application. This
guide explains how to point marga at your own infrastructure instead.

## Architecture

marga is a **client**. At runtime it talks to three things:

| Concern | Component | Default | Config key |
|---|---|---|---|
| Receiving messages in realtime | WebSocket **relay** | `wss://marga.kunthive.com:8443/ws` | `server.websocket_url` |
| Loading message history | relay **REST** API | `https://marga.kunthive.com:8443` | `server.relay_url` |
| Sending messages | Discord **webhook** | — | `server.webhook_url` |
| Login / identity | Discord **OAuth2** app | app id `1503351063468572754` | `auth.discord.*` |

> **Note:** The relay (the WebSocket + history server) is a separate component
> and is **not** part of this repository. To fully self-host you need a relay
> that speaks marga's protocol. If you only want to use your own Discord app
> and webhook against the hosted relay, you can stop after steps 1–2.

## 1. Register your own Discord application

1. Go to <https://discord.com/developers/applications> and create an app.
2. Under **OAuth2 → Redirects**, add a loopback redirect:
   `http://127.0.0.1:53682/callback` (or your own port).
3. Note the **Client ID**. marga uses PKCE (public client), so you do **not**
   need a client secret for the default flow. Set one only for a confidential
   deployment.

Point marga at your app:

```toml
[auth.discord]
client_id    = "YOUR_CLIENT_ID"
redirect_url = "http://127.0.0.1:53682/callback"
```

or via environment variables:

```bash
export MARGA_DISCORD_CLIENT_ID=YOUR_CLIENT_ID
export MARGA_DISCORD_REDIRECT_URL=http://127.0.0.1:53682/callback
# optional, confidential deployments only:
export MARGA_DISCORD_CLIENT_SECRET=...
```

## 2. Create a Discord webhook for sending

In your Discord server: **Server Settings → Integrations → Webhooks → New
Webhook**, then copy its URL.

```toml
[server]
webhook_url = "https://discord.com/api/webhooks/123456789/abcdef"
```

or `export MARGA_WEBHOOK_URL=...`.

## 3. Point marga at your own relay

If you run a marga-compatible relay, override both the WebSocket and REST
endpoints:

```toml
[server]
websocket_url = "wss://relay.example.com/ws"
relay_url     = "https://relay.example.com"
```

or:

```bash
export MARGA_WEBSOCKET_URL=wss://relay.example.com/ws
export MARGA_RELAY_URL=https://relay.example.com
```

History fetching is disabled if `relay_url` is empty — marga still works as a
send-only / live-only client in that case.

## 4. Other override knobs

Every config field has an environment-variable override. The ones relevant to
self-hosting:

| Env var | Overrides |
|---|---|
| `MARGA_WEBSOCKET_URL` | `server.websocket_url` |
| `MARGA_RELAY_URL` | `server.relay_url` |
| `MARGA_WEBHOOK_URL` | `server.webhook_url` |
| `MARGA_API_KEY` | `server.api_key` (relay auth, if your relay requires it) |
| `MARGA_WEB_SETUP_URL` | `server.web_setup_url` (web onboarding wizard) |
| `MARGA_DISCORD_CLIENT_ID` | `auth.discord.client_id` |
| `MARGA_DISCORD_CLIENT_SECRET` | `auth.discord.client_secret` |
| `MARGA_DISCORD_REDIRECT_URL` | `auth.discord.redirect_url` |
| `MARGA_GUILD_ID` | `general.guild_id` |
| `MARGA_BOT_CLIENT_ID` | `server.bot_client_id` |

Secrets (Discord tokens, GitHub PAT) are stored in your OS keyring, never in
the config file. See the comments in
[`config.example.toml`](../config.example.toml) for the full field reference.

## 5. Verify

```bash
make build
./bin/marga --config /path/to/your/config.toml
```

On first run marga opens your browser for the OAuth flow against **your**
Discord app. After login you should land in the chat connected to **your**
relay.
