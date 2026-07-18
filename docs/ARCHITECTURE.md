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
persists this to SQLite. As currently implemented **on that public deployment**:

- Message **content is stored in plaintext**, alongside author, timestamp, and
  channel/user/guild IDs. Nothing is encrypted or hashed.
- **There is no automatic retention limit, expiry, or pruning** — stored
  messages accumulate indefinitely.
- **There is no per-user deletion path.** A message row is only removed when the
  original message is deleted on Discord; there is no "delete my data" endpoint.

This is an honest description of the *public* deployment's present behavior, not
a target state. If this is unacceptable for your threat model, self-host the
relay or run marga send-only (see below).

> **The self-host reference relay closes the last two gaps.** This repository
> ships a reference relay in [`cmd/relay`](../cmd/relay) (see
> [`RELAY.md`](RELAY.md)) that speaks the same contract and adds, **scoped to
> that relay**:
>
> - a **retention window** (`RELAY_RETENTION`, a Go duration; `0` keeps forever,
>   preserving today's default) that prunes old rows on a timer *and* filters
>   them out on read, so a not-yet-pruned row past the window is never served;
>   and
> - a **delete-my-data path** — `POST /api/delete-my-data {user_id}` and
>   `DELETE /api/users/{id}/messages`, each returning `{deleted: N}`.
>
> Content is still stored in plaintext SQLite there too. The public
> `marga-discord-relay` deployment may still lack a retention window and a
> deletion path until it adopts the same changes.

### Onboarding keeps secrets on the machine

The first-run wizard (`internal/setup/wizard.go`) is network-first: you pick
**Matrix** (works immediately, no relay) or **Discord** (advanced, needs a
self-hosted relay and a registered Discord app). Whichever you choose, all
authentication happens locally — Matrix exchanges your password for a token
stored in the OS keyring, and Discord runs its OAuth flow against endpoints you
control. **No access token is ever placed in a URL or sent to a web host.**

> Historical note: earlier builds offered a "web browser setup" that opened
> `https://marga.kunthive.com/terminal-setup#token=<access_token>`, handing the
> Discord access token to a web host in the URL fragment. That path has been
> **removed**; the `server.web_setup_url` setting is now unused and retained only
> for backward compatibility.

## Reducing or removing reliance on the relay

marga is a client; every endpoint is configurable.

- **Self-host the relay** — point `server.websocket_url` and `server.relay_url`
  at your own marga-compatible relay. See [`SELF_HOSTING.md`](SELF_HOSTING.md).
- **Send-only, no relay** — leave `server.relay_url` empty and set
  `server.webhook_url`. marga sends via a Discord webhook directly; history and
  realtime receive are disabled.
- **Your own Discord app** — register your own OAuth2 application so even login
  goes through credentials you control.

## Multi-network architecture

marga is not Discord-only. Internally it talks to a pluggable **`Network`
adapter** (`internal/network`); the TUI consumes a unified event stream and
never sees protocol-specific wire formats. Two adapter styles exist:

- **Relay-backed** (`internal/network/discordrelay`) — wraps the WebSocket +
  webhook + history clients described above. Required for Discord because
  Discord forbids self-bots; the relay holds the gateway connection.
- **Direct** (`internal/network/matrix`) — connects straight from marga to the
  network with no relay and no third party storing plaintext. Matrix uses the
  client-server API (mautrix-go); the access token is stored in the OS keyring
  (`marga-matrix`), and `/sync` state is cached locally for fast restarts.
  Joined **spaces are surfaced as servers** (`ListServers`), with a "home"
  server for rooms in no space; the flat room list is still available.

### Matrix end-to-end encryption

Encrypted rooms are decrypted and encrypted locally — plaintext never leaves
your machine. On connect the adapter starts an Olm machine via mautrix's
`cryptohelper` (the pure-Go `goolm` backend; build with `-tags goolm`):

- **Decryption** is automatic. The helper handles `m.room.encrypted` events
  during `/sync` and re-dispatches the plaintext through the normal message
  handler; history backfill is decrypted inline when the keys are available.
- **Encryption** on send is automatic for rooms the homeserver reports as
  encrypted — `Send`/`SendFile`/`Edit` need no special casing.
- **Key storage.** Olm/Megolm sessions live in a local SQLite crypto store
  (`crypto.db`, next to the sync cache). The *pickle key* that encrypts that
  store at rest is generated once and kept in the OS keyring (`marga-matrix`,
  key `pickle_key`) — never written to disk in clear.
- **Graceful degradation.** If crypto can't initialize, the client still
  connects and shows encrypted rooms as undecryptable rather than failing.
- **Scope.** Trust is currently use-on-first-key (no interactive device
  verification / cross-signing UI yet).

Each adapter exposes the same interface: connect, list servers/channels,
subscribe, fetch history, send/file/edit, and a single `Events()` channel of
messages/typing/presence/status. The TUI runs one listener per adapter and
fans them into one ordered update loop, tagging every event with its origin
`NetworkID`. Credentials are namespaced per network in the keyring; local
history is isolated per `(network, server)` on disk.

### Network-neutral relay contract

For relay-backed networks the relay is a **router** in front of per-backend
adapters. The client↔relay contract is already protocol-neutral and stays so:

- **Events** use generic types — `message_create`, `message_update`, `typing`,
  `status_update` — with opaque string IDs and format-agnostic attachments.
- **Channel identity on the wire** is network-qualified for multi-backend
  relays: `discord:general`, `slack:C0123`. Single-network relays may omit the
  prefix (back-compatible).
- **REST** endpoints (`/message`, `/file`, `PATCH /message/:id`, history,
  channel/server listing) accept a neutral `server_id` alias alongside the
  legacy `guild_id`.
- A backend (Discord bot, Slack socket-mode app) translates its native events
  into the neutral event types; **no client change is needed** to add one.

The production Discord relay lives in a separate repo
(`kunthive-Labs/marga-discord-relay`); this contract is the spec a multi-backend
relay implements. A **self-hostable reference implementation of the contract**
ships in this repo at [`cmd/relay`](../cmd/relay) (a local echo backend plus a
Discord-bridge stub) — see [`RELAY.md`](RELAY.md).

## Summary

marga trades a direct gateway connection for a bot-based relay specifically to
keep your Discord account ToS-compliant and setup friction-free. Your tokens and
permissions stay local (the web-setup path being the one exception to call out);
message content and metadata transit the relay. If that centralization is not
acceptable for your threat model, the relay, webhook, OAuth app, and web-setup
host are all replaceable with infrastructure you control. Networks that permit
direct client connections (Matrix today) bypass the relay entirely.
