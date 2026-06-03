# Changelog

All notable changes to Marga are documented here. The format is based on
[Keep a Changelog](https://keepachangelog.com/), and the project aims to follow
[Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added
- **File-based logging.** A new `internal/logging` package (built on `log/slog`)
  provides leveled, structured diagnostics written to a file — never the
  terminal, which would corrupt the TUI. Off by default; enable with `--debug`,
  `--log-file PATH`, `--log-level`, the `[logging]` config section, or
  `MARGA_LOG_*` env vars. Logs are tagged per `component` (`discord`, `matrix`,
  `tui`, …), and mautrix's own Matrix logs are routed to the same file.
- New CLI flags: `--debug`, `--log-file`, `--log-level`, `--version`/`-v`,
  `--help`/`-h`, plus startup/connect/shutdown lifecycle logging.
- Reference documentation: [docs/CONFIGURATION.md](docs/CONFIGURATION.md),
  [docs/TROUBLESHOOTING.md](docs/TROUBLESHOOTING.md), and
  [docs/OPERATIONS.md](docs/OPERATIONS.md); GoDoc package comments across the
  internal packages.
- Supply-chain/quality CI: `golangci-lint` (config in `.golangci.yml`),
  `govulncheck`, a `-race` test job, and Dependabot for Go modules and Actions.
- `/network [name]` command and `Ctrl+T` shortcut to list and switch the active
  network from the TUI.
- Matrix history backfill (`FetchHistory` via `/messages` with a per-room
  pagination cursor), so `/history` now works on Matrix.
- Matrix `ListServers` (surfaces the homeserver as a server) and presence
  broadcast via `SetStatus`.
- **Matrix end-to-end encryption.** Encrypted rooms are decrypted on `/sync`
  and during history backfill, and outgoing messages to encrypted rooms are
  encrypted automatically, via the mautrix Olm machine (pure-Go `goolm`
  backend). Olm/Megolm keys are kept in a local crypto store whose pickle key
  lives in the OS keyring. If crypto can't initialize, the client still
  connects with encrypted rooms left undecrypted.
- **Matrix spaces as servers.** Joined spaces are surfaced through
  `ListServers`/`ListChannels` alongside a "home" server for ungrouped rooms;
  the flat room list is unchanged for clients that don't switch servers.
- **Interactive Matrix login.** With no stored token and an interactive
  terminal, Marga prompts for any missing homeserver/user id and reads the
  password without echo, instead of requiring `MARGA_MATRIX_PASSWORD`.
- `Encryption` field on `network.Capabilities` so adapters can advertise E2EE.
- Test coverage for previously untested packages (model, network types,
  credstore, guilds, discordrelay, setup helpers, matrix media/store).
- `CONTRIBUTING.md` and `SECURITY.md`.

### Changed
- **Builds use the pure-Go `goolm` Olm backend** (`-tags goolm`,
  `CGO_ENABLED=0`) across the Makefile, CI, and goreleaser, so E2EE compiles
  into static binaries without a system `libolm`/C toolchain. Installing with
  `go install` now needs `-tags goolm`.
- **Config is now bring-your-own.** Removed the bundled relay endpoints and the
  shared Discord application client ID. `client_id` and relay URLs must be set
  via config or `MARGA_*` env vars; startup fails with an actionable error if a
  Discord-enabled config is missing them.
- Relay endpoints are only required when Discord is in use — a Matrix-only
  config now runs with no relay configuration.
- Per-network event isolation in the TUI: messages, typing, and presence from a
  non-active network no longer leak into the current view (they are still
  persisted in the background).
- Adapters are built from the enabled-networks list, so a Discord adapter is no
  longer created for Matrix-only setups.

### Fixed
- `marga --setup` (and any other flag beyond `-c`/`--config`) no longer fails at
  startup with `flag provided but not defined`. Config-path parsing now ignores
  unknown flags instead of rejecting them.
- Empty test assertion in the input-handling tests now actually asserts.
- Various `staticcheck` findings and intentionally-ignored errors made explicit.

### Security
- No credentials or shared infrastructure endpoints are embedded in the binary
  or config defaults.
- Matrix E2EE keys and the crypto-store pickle key stay local (crypto database
  on disk, pickle key in the OS keyring); encrypted plaintext never leaves the
  client. Device trust is use-on-first-key (no verification UI yet).

## [0.1.0] - Initial

- Terminal-native, multi-network chat client (Marga), forked and rebranded from
  its upstream origins.
- Discord support via a relay (WebSocket + webhook) and direct Matrix support
  (mautrix-go), behind one unified `Network` adapter interface and TUI event
  stream.
