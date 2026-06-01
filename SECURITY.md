# Security Policy

## Reporting a vulnerability

Please report security issues **privately**. Do not open a public GitHub issue
for a vulnerability.

- Use GitHub's **Report a vulnerability** (Security → Advisories) on the
  `kunthive-Labs/Margana` repository, or
- email the maintainers at the address listed on the org profile.

Include reproduction steps and the affected version/commit. We aim to acknowledge
reports within a few days.

## Security model (what to keep in mind)

Marga is a client; its security posture depends partly on how you deploy it.

- **Credentials stay local.** Discord OAuth tokens and the Matrix access token
  are stored in the OS keyring (macOS Keychain, libsecret, Windows Credential
  Manager), never in the config file or repo.
- **No shared infrastructure.** Marga ships no hosted relay and no shared
  Discord application. You supply your own relay endpoints and Discord client
  ID. There are no embedded credentials to leak.
- **Discord relay.** For Discord, message content and metadata transit the relay
  you configure. Run it over TLS and treat its API key as a secret. The relay's
  storage/retention policy is yours to set — see
  [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md).
- **Matrix is direct.** The Matrix adapter connects straight to your homeserver
  with no relay or third party. End-to-end encrypted rooms are decrypted and
  encrypted locally via the Olm machine (mautrix); plaintext never leaves your
  machine. Olm/Megolm keys live in a local crypto database whose pickle key is
  stored in the OS keyring, not on disk in clear. Device trust is currently
  use-on-first-key — there is no interactive verification / cross-signing UI
  yet, so this does not defend against a homeserver substituting device keys.
- **Web setup caveat.** The optional "web setup" onboarding path sends your
  Discord access token to `server.web_setup_url`. Leave it empty (use terminal
  setup) unless you host that wizard yourself over HTTPS.

## Supported versions

Security fixes target the latest release and `main`.
