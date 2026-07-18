# The Marga reference relay (`cmd/relay`)

This repository ships a **self-hostable reference relay**: a small Go server
that speaks the exact wire contract Marga's TUI expects (a WebSocket event
stream plus REST send/history APIs) and stores messages in a pure-Go SQLite
database. It exists so you can run a working, ToS-safe Marga relay with a single
`docker compose up`, with **no Discord account, bot token, or gateway
connection** involved.

It ships a **local echo backend**: messages you send are persisted and
broadcast straight back to subscribers, so the relay is a working loopback chat
with history out of the box. It also adds the two things
[`ARCHITECTURE.md`](ARCHITECTURE.md) flagged as missing from the hosted relay: a
**retention window** and a **delete-my-data** endpoint.

> The production Discord bridge — the gateway bot that actually talks to
> Discord — lives in the separate
> [`kunthive-Labs/marga-discord-relay`](https://github.com/kunthive-Labs/marga-discord-relay)
> repository. This reference relay deliberately does not carry a bot token or
> gateway connection; its `discord` backend is a clearly-marked stub. See
> [Backends](#backends).

## Quick start (Docker)

```bash
# From the repository root:
API_KEY=$(openssl rand -hex 32) docker compose up --build
```

That builds a static, CGO-free image, starts the relay on `:8443`, and persists
the SQLite database in the named volume `relay-data`.

Point Marga at it (config or env):

```toml
[server]
websocket_url = "ws://localhost:8443/ws"
relay_url     = "http://localhost:8443"
api_key       = "PASTE_THE_SAME_API_KEY"
```

```bash
export MARGA_WEBSOCKET_URL=ws://localhost:8443/ws
export MARGA_RELAY_URL=http://localhost:8443
export MARGA_API_KEY=PASTE_THE_SAME_API_KEY
```

Open two Marga windows against the same relay (same channel) and they chat
through your own server. For TLS (`wss://` / `https://`), terminate it at a
reverse proxy (Caddy, nginx, Traefik) in front of the relay.

## Quick start (without Docker)

```bash
CGO_ENABLED=0 go build -o bin/relay ./cmd/relay
API_KEY=secret RELAY_RETENTION=168h ./bin/relay
```

## Configuration

Every option is available as a flag and an environment variable (the env var
wins for Docker). Flags are shown with their env var in parentheses.

| Env var | Flag | Default | Meaning |
|---|---|---|---|
| `LISTEN_ADDR` | `-listen` | `:8443` | Address to listen on. |
| `API_KEY` | `-api-key` | *(empty)* | Required `X-API-Key` value. **Empty disables auth** (open relay). |
| `RELAY_DB_PATH` | `-db` | `relay.db` (`/data/relay.db` in Docker) | SQLite database path. |
| `RELAY_RETENTION` | `-retention` | `0` | Message retention as a Go duration (`24h`, `168h`, …). **`0` keeps forever.** |
| `RELAY_BACKEND` | `-backend` | `local` | `local` (echo) or `discord` (stub). |
| `RELAY_DEFAULT_CHANNEL` | `-default-channel` | `general` | Channel advertised before any message is sent. |

`GET /healthz` returns `200 ok` and is **exempt from the API key** so container
health checks work without a credential.

## The wire contract

The relay implements the contract Marga's client packages already speak
(`internal/wsclient`, `internal/webhook`, `internal/history`,
`internal/guilds`). Authentication is the `X-API-Key` header on every request
(and on the WebSocket dial) except `GET /healthz`.

### WebSocket — `GET /ws`

Client → server JSON actions:

| Action | Fields | Effect |
|---|---|---|
| `identify` | `username` | Register the connection's display name. |
| `subscribe` | `channel` | Start receiving that channel's events. |
| `unsubscribe` | `channel` | Stop receiving that channel's events. |
| `status_update` | `status`, `username` | Broadcast a presence update. |

Server → client event frames (JSON), matching `wsclient`'s decoder:

| `type` | Key fields | Sent when |
|---|---|---|
| `message_create` | `message_id`, `channel`, `username`, `user_id`, `content`, `timestamp`, `reply_to_id`, `editable` | A message is published to a subscribed channel. |
| `message_update` | same as above | A message is edited (`PATCH /message/{id}`). |
| `typing` | `channel`, `username` | A backend reports typing (see note below). |
| `status_update` | `username`, `status`, `updated_at` | A client sends `status_update`. |
| `terminal_online` | `users` | The set of identified users changes (connect/disconnect). |

Timestamps on the wire are RFC 3339.

### REST

| Method & path | Body / query | Response |
|---|---|---|
| `POST /message` | `{channel, guild_id, username, avatar_url, content, reply_to_id}` | `{message_id}` |
| `POST /file` | multipart: `channel, username, avatar_url, content, guild_id`, file field `file` | `{message_id}` |
| `PATCH /message/{id}` | `{channel, guild_id, username, content}` | `{message_id}` |
| `GET /api/channels` | — | `{channels:[{name, type}]}` |
| `GET /api/guilds/{id}/channels` | — | `{channels:[{name, type}]}` |
| `GET /api/channels/{channel}/messages` | `?limit&before&since&guild_id` | `[{id, channel, user_id, username, content, timestamp}]` |
| `GET /api/guilds` | `?q` | `{guilds:[{id, name}]}` |
| `DELETE /api/users/{id}/messages` | — | `{deleted: N}` |
| `POST /api/delete-my-data` | `{user_id}` | `{deleted: N}` |

History pagination: `before` returns messages strictly older than the given
RFC 3339 timestamp (oldest-first); `since` returns messages strictly newer.

## Retention

By default (`RELAY_RETENTION=0`) messages are kept forever — the same behavior
as the hosted relay today. Set a Go duration to enable a rolling window:

```bash
RELAY_RETENTION=168h   # keep 7 days
```

With a window set, the relay does two things:

1. **Prunes on a timer.** A background goroutine deletes rows older than the
   window on startup and periodically thereafter, reclaiming disk.
2. **Filters on read.** History responses drop anything older than the window
   even if the pruner has not run yet — a stale row is *never served*.

## Deleting your data

Two endpoints remove a user's stored messages across all channels:

```bash
# Friendly form:
curl -X POST http://localhost:8443/api/delete-my-data \
  -H "X-API-Key: $API_KEY" -H 'Content-Type: application/json' \
  -d '{"user_id":"alice"}'
# => {"deleted": 3}

# REST form:
curl -X DELETE http://localhost:8443/api/users/alice/messages \
  -H "X-API-Key: $API_KEY"
# => {"deleted": 3}
```

The neutral send contract carries a **username**, not a stable user id, so the
local backend keys authorship by username — pass the username as `user_id`.

## Backends

The relay is built around a small `Backend` seam (`internal/relay/backend.go`):

- **`local`** (default) — the echo backend described above. Fully self-contained.
- **`discord`** — an intentional **stub**. Selecting it returns
  `501 Not Implemented` on publish/list, documenting the boundary: the real
  Discord gateway bot needs a bot token and a live gateway connection and lives
  in [`kunthive-Labs/marga-discord-relay`](https://github.com/kunthive-Labs/marga-discord-relay).
  The seam is there so that bridge (or your own) can drop in without touching
  the store, hub, or HTTP server.

## Reference-relay limitations

This is a *reference* relay, deliberately small. Compared with a full bridge:

- **Edits are broadcast-only.** `PATCH /message/{id}` fans out a
  `message_update` to subscribers, but the store is append-only, so history
  keeps the original text.
- **Uploaded file bytes are not hosted.** `POST /file` records the message and
  filename and broadcasts it, but there is no public blob URL, so attachments
  are not served back.
- **`typing` has no local source.** The local echo backend never emits typing;
  the frame type exists for real backends to use.
- **TLS is out of scope.** Run it behind a reverse proxy for `wss://`/`https://`.

## Layout

| File | Responsibility |
|---|---|
| `cmd/relay/main.go` | Config (flags/env), bootstrap, retention pruner, graceful shutdown. |
| `internal/relay/store.go` | SQLite store (`modernc.org/sqlite`, WAL, single writer). |
| `internal/relay/hub.go` | WebSocket hub: upgrade, per-channel subscriptions, fan-out. |
| `internal/relay/server.go` | `http.ServeMux` routes + `X-API-Key` auth + retention filter. |
| `internal/relay/backend.go` | `Backend` interface, local echo impl, Discord stub. |
