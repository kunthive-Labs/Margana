# Operations guide

Practical notes for running Marga beyond a casual interactive session: headless
use, logs, data, upgrades, and security. For the config field reference see
[CONFIGURATION.md](CONFIGURATION.md); for diagnosing problems see
[TROUBLESHOOTING.md](TROUBLESHOOTING.md).

---

## Running headless / in CI

Marga is an interactive TUI and expects a terminal, but the non-interactive
pieces (config loading, auth, network connect) work without one if you supply
credentials through the environment:

- **Discord OAuth requires a browser** for the loopback redirect, so first-time
  Discord login can't be completed headless. Authenticate once interactively;
  the tokens are then cached in the keyring and refreshed automatically.
- **Matrix** is fully scriptable: set `MARGA_MATRIX_PASSWORD` for the first
  connect (the token is cached afterward), plus the `[[networks]]` block.
- On headless Linux, the OS keyring may be unavailable (no Secret Service). See
  [TROUBLESHOOTING.md → Secrets](TROUBLESHOOTING.md#secrets--keyring).

Configuration is fully environment-driven — every setting has a `MARGA_*`
override (see [CONFIGURATION.md](CONFIGURATION.md#environment-variables)), so a
container or CI job can run without a config file on disk.

---

## Logs

Logging is **off by default** and always writes to a **file** (never the
terminal — Marga is a full-screen TUI). Turn it on with `--debug`, `--log-file`,
or the `[logging]` config section.

- **Default location:** `<data-dir>/marga.log`
  (e.g. `~/.local/share/marga/marga.log` on Linux).
- **Format:** `text` (human-readable) or `json` for ingestion by a log
  processor. Set `logging.format` or `MARGA_LOG_FORMAT`.
- **Levels:** `debug`, `info`, `warn`, `error`.
- **Attribution:** every line carries a `component` attribute (`discord`,
  `matrix`, `tui`, …). mautrix's internal Matrix logs are included as JSON lines.

### Rotation

Marga appends to a single file and does **not** rotate it. For long-running or
production deployments, rotate externally:

- **logrotate** (Linux): point a rule at the log path with `copytruncate`.
- Or direct `--log-file` at a path managed by your platform's logging setup.
- For ad-hoc use, just delete the file when it grows large; Marga recreates it.

Log files are created with `0600` permissions because they can contain
operationally sensitive details (IDs, homeserver names, errors). Avoid logging
at `debug` long-term on shared machines.

---

## Data and databases

Marga keeps local SQLite databases (WAL mode, FTS5 full-text search) under the
[data directory](CONFIGURATION.md#file-locations):

- Discord: `<data-dir>/servers/<guild-id>.db` (or `<data-dir>/marga.db`).
- Other networks: `<data-dir>/<network-id>/<server>.db`.
- Matrix sync/crypto state: `<data-dir>/matrix/`.

### Backup

Stop Marga, then copy the data directory. With WAL mode there may be `-wal` and
`-shm` sidecar files — copy the whole directory, or use `sqlite3 db ".backup"`
for a consistent single-file snapshot.

### Growth / maintenance

History accumulates indefinitely; there is no automatic pruning. The databases
are local caches — deleting a `*.db` file just drops cached history (it will
re-fetch from the relay/homeserver on next use, subject to `history_limit`).
Running `VACUUM` on a database reclaims space after large deletes.

---

## Upgrading

Marga is built from source (see the [README](../README.md)). To upgrade:

```bash
git pull
make build      # → bin/marga (sets the goolm tag for you)
```

- `make build` uses the `goolm` tag (pure-Go Olm for Matrix E2EE) and
  `CGO_ENABLED=0` for static binaries.
- Keep your **Go toolchain current** — security fixes in the Go standard library
  ship as patch releases, and the CI `govulncheck` job scans against the latest
  stable Go. Build with an up-to-date toolchain for those fixes.
- Configuration is backward-compatible: new fields take defaults, so an older
  `config.toml` keeps working.

---

## Security and secrets

- OAuth and Matrix tokens are stored in the **OS keyring**, never in
  `config.toml`. See [CONFIGURATION.md → keyring](CONFIGURATION.md#secrets-in-the-os-keyring)
  and [SECURITY.md](../SECURITY.md).
- The Discord relay sees message content/metadata for relay-backed networks;
  Matrix connects directly with no third party. See [ARCHITECTURE.md](ARCHITECTURE.md).
- The optional **web setup** path sends your Discord access token to the
  configured `web_setup_url`. Prefer terminal setup.
- Restrict permissions on the config and data directories on shared machines.

---

## Quality gates (CI)

Continuous integration ([`.github/workflows/ci.yml`](../.github/workflows/ci.yml))
runs on Linux/macOS/Windows and enforces:

| Job | What it checks |
|-----|----------------|
| `test` | `go test` on all three OSes |
| `race detector` | `go test -race` (cgo-enabled; still uses pure-Go Olm) |
| `golangci-lint` | `errcheck`, `govet`, `ineffassign`, `staticcheck`, `unused`, `misspell` (config in [`.golangci.yml`](../.golangci.yml)) |
| `gofmt` | formatting |
| `govulncheck` | known vulnerabilities, scanned against the latest stable Go |

Run them locally: `make check` (vet + test), `gofmt -l .`, and
`golangci-lint run`.

---

## Known limitations

### Image rendering

`ui.image_protocol` and protocol detection still run, but inline image
**rendering is not currently wired up**: `RenderAttachment` returns a text
placeholder (`🖼 filename`), and the iTerm2/Kitty/sixel renderers in
`internal/tui/image.go` are not invoked. That code is retained (and excluded
from the `unused` lint gate, see [`.golangci.yml`](../.golangci.yml)) pending a
decision to re-wire or remove it. A handful of other helpers
(`internal/commands/file.go`, `internal/setup/ui.go`, `internal/tui/styles.go`,
`viewport.go`, `markdown.go`) are similarly unused. None affect runtime
behavior; they're flagged here for maintainers rather than deleted unilaterally.
