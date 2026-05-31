# Changelog

All notable changes to Marga are documented here. The format is based on
[Keep a Changelog](https://keepachangelog.com/), and the project aims to follow
[Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added
- `/network [name]` command and `Ctrl+T` shortcut to list and switch the active
  network from the TUI.
- Matrix history backfill (`FetchHistory` via `/messages` with a per-room
  pagination cursor), so `/history` now works on Matrix.
- Matrix `ListServers` (surfaces the homeserver as a server) and presence
  broadcast via `SetStatus`.
- `CONTRIBUTING.md` and `SECURITY.md`.

### Changed
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

### Security
- No credentials or shared infrastructure endpoints are embedded in the binary
  or config defaults.

## [0.1.0] - Initial

- Terminal-native, multi-network chat client (Marga), forked and rebranded from
  its upstream origins.
- Discord support via a relay (WebSocket + webhook) and direct Matrix support
  (mautrix-go), behind one unified `Network` adapter interface and TUI event
  stream.
