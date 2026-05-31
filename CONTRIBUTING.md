# Contributing to Marga

Thanks for your interest in improving Marga. This is a terminal-native,
multi-network chat client written in Go.

## Development setup

```bash
git clone https://github.com/kunthive-Labs/Margana.git
cd Margana
make build      # → bin/marga
```

Requires Go 1.25+. No external services are needed to build or run the test
suite. To actually connect:

- **Matrix** works with no extra infrastructure — configure a `[[networks]]`
  block (see `config.example.toml`) and set `MARGA_MATRIX_PASSWORD` for the
  first login.
- **Discord** requires a relay and your own Discord application. See
  [`docs/SELF_HOSTING.md`](docs/SELF_HOSTING.md). Marga ships no hosted relay
  and no shared Discord app.

## Before you open a PR

Run the same checks CI does:

```bash
gofmt -l .        # must print nothing
go vet ./...
go build ./...
go test ./...
```

A `make check` / `make test` target is provided as a shortcut.

## Guidelines

- **Match the surrounding code.** Keep comment density, naming, and idioms
  consistent with the file you're editing.
- **Keep the network abstraction clean.** The TUI talks only to the
  `network.Network` interface (`internal/network`); protocol-specific code lives
  in an adapter (`internal/network/discordrelay`, `internal/network/matrix`).
  New networks should be new adapters, not special cases in the TUI.
- **Add tests** for new behavior. The TUI is exercised through its `Update`
  loop with fake adapters (`internal/tui/network_test.go` is a good template).
- **Never commit secrets.** Tokens live in the OS keyring; endpoints and IDs
  live in config or `MARGA_*` env vars, never hard-coded.

## Reporting security issues

Please do not file public issues for vulnerabilities — see
[`SECURITY.md`](SECURITY.md).
