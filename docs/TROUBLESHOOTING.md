# Troubleshooting

Start here when something doesn't work. The single most useful step is to
**turn on logging** — Marga is silent by default.

## Enable logging

Marga is a full-screen TUI, so it never prints diagnostics to the terminal
(that would corrupt the display). Logs go to a file instead, and logging is
off until you ask for it.

```bash
marga --debug                      # debug level → <data-dir>/marga.log
marga --log-file /tmp/marga.log    # custom path
marga --log-file /tmp/marga.log --log-level warn
```

Then watch it in another terminal:

```bash
tail -f ~/.local/share/marga/marga.log     # Linux (default path)
```

You can also enable it persistently in `config.toml`:

```toml
[logging]
level = "debug"
file  = "/home/you/.local/share/marga/marga.log"
format = "text"   # or "json"
```

Log lines are tagged with a `component` (`discord`, `matrix`, `tui`, …) so you
can tell which subsystem produced them. mautrix's own Matrix logs are included
(as JSON lines) when logging is on.

See [CONFIGURATION.md](CONFIGURATION.md#logging) for the full reference.

---

## Startup

### `config: missing server.websocket_url …`

Discord is enabled but no relay is configured. Marga ships no hosted relay —
set `server.websocket_url` (and a webhook or relay URL) to one you run or trust.
See [SELF_HOSTING.md](SELF_HOSTING.md). If you only want Matrix, disable Discord:

```toml
[auth]
enabled = false
```

### `config: missing auth.discord.client_id …`

Register your own Discord application at
<https://discord.com/developers/applications> and set `auth.discord.client_id`
(or `MARGA_DISCORD_CLIENT_ID`). There is no shared default.

### `no networks enabled`

Nothing is configured to connect. Either configure Discord (relay + client ID)
or add a `[[networks]]` block (e.g. Matrix).

### Re-run setup

```bash
marga --setup
```

This forces the interactive wizard even when a config already exists.

### `parsing config file …`

Your `config.toml` has a TOML syntax error. The message names the file; fix the
reported line, or move it aside and run `marga --setup` to regenerate.

---

## Discord

- **Messages don't send / no history loads.** Check `server.webhook_url`,
  `server.relay_url`, and `server.api_key`. With logging on, look for
  `component=discord` lines and HTTP errors. The relay must be reachable.
- **`ws: connect failed … retrying`** in the log means the WebSocket relay is
  unreachable; Marga retries with exponential backoff (1s→30s).
- **OAuth login won't complete.** The redirect is a loopback server on port
  `53682`. Make sure that port is free and that `auth.discord.redirect_url`
  matches the Developer Portal exactly. A browser must be able to open the URL;
  in headless environments this flow can't complete.
- **Re-authenticate from scratch:** use `/logout` in the app, or clear the
  keyring entry (`marga-discord`).

---

## Matrix

- **`matrix: no stored token — set MARGA_MATRIX_PASSWORD or run in an
  interactive terminal to log in`.** First connect needs a password. In a normal
  terminal Marga prompts for it; for headless/CI set `MARGA_MATRIX_PASSWORD`.
  After first login the token is cached in the keyring and the password isn't
  needed again.
- **`matrix: homeserver and user_id must be configured`.** Add both to the
  `[[networks]]` block (`homeserver = "https://…"`, `user_id = "@you:server"`).
- **`matrix: end-to-end encryption unavailable: …`.** Marga connects anyway but
  leaves encrypted rooms undecrypted. This is logged at `warn`. Usually a crypto
  database/keyring problem — check the Matrix state dir under `<data-dir>/matrix/`
  is writable.
- **Sync keeps reconnecting.** With logging on, `component=matrix` `sync failed,
  retrying` lines carry the underlying error (often a homeserver or network
  issue). Marga retries every 5s.

---

## Display

### Images show as `🖼 filename` instead of rendering

Inline terminal-image rendering is currently **not wired up** — the UI shows a
text placeholder for image attachments regardless of `ui.image_protocol`. The
protocol-rendering code exists but is not currently invoked. See
[OPERATIONS.md → Image rendering](OPERATIONS.md#image-rendering).

### Colors look wrong / no color

Try a different `ui.theme` (`default`, `dracula`, `solarized`) and confirm your
terminal supports 24-bit color.

---

## Secrets / keyring

Marga stores tokens in the OS keyring (`marga-discord`, `marga-matrix`). On
headless Linux without a Secret Service (e.g. no GNOME Keyring/KWallet), keyring
access can fail. Provide credentials via environment instead
(`MARGA_MATRIX_PASSWORD`) and consult your distro's keyring setup.

---

## Still stuck?

Open an issue with: your platform, `marga --version`, the relevant
`component=…` log lines (with secrets redacted), and the steps to reproduce.
