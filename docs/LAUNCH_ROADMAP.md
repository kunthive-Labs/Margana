# Launch Roadmap — Production Readiness

> Working implementation reference for hardening **Marga** (terminal-native,
> multi-network chat client) into a launch-ready, adoptable, memorable tool.
> Created: 2026-07-15 · Status: **planning → implementing** · Direction chosen:
> **"Harden Margana for launch"**.

---

## 0. Context (read this first)

**What Marga is:** a terminal TUI chat client (Go, Bubble Tea + Lipgloss) that
unifies **Discord (via a self-hosted relay bot)** and **Matrix (direct, E2EE)**
behind one keyboard-driven interface. Local SQLite history, OS-keyring secrets.
Pre-1.0 (`v0.1.0`). **No web surface exists** — the "product" is a binary.

**Real audience** (optimize for these, not the generic brief):
terminal/CLI power users, developers who live in tmux/vim/ssh, privacy &
self-hosting enthusiasts (r/selfhosted, Matrix folks), the riced-terminal crowd
(r/unixporn, r/commandline), and OSS contributors.

**What "production-ready + memorable" means here:** it installs in one line,
succeeds on first launch, shows people what it is, notifies reliably, respects
the terminal, and becomes someone's *daily driver*. Retention for a chat client
= reliability + notifications + being genuinely better than the official clients
for someone already in the terminal.

> ⚠️ This roadmap deliberately **excludes** the web-app / SEO / mobile-web /
> data-visualization / "market intelligence for VCs" items from the original
> brief — those describe a different product and do not apply. See §7 Out of scope.

---

## 1. How to use this doc

- **Follow §2 (Phased execution plan)** — it groups every item into ordered
  phases you complete one at a time. The tier sections **§3–§6** remain the
  detailed per-item reference (why / work / files / done-when).
- Each item has: **Status** checkbox · **Priority** (P0→P3) · **Complexity**
  (Low/Med/High) · **Why** · **Work** (concrete, file-referenced) · **Done when**.
- Where an item infers "X is missing," the first step is **verify** — don't
  assume; confirm against the current tree.
- Update the checkboxes and the §8 Progress log as you go.
- Status legend: ☐ not started · ◐ in progress · ☑ done.

---

## 2. Phased execution plan

Do the phases in order — each ends in a shippable, verifiable state and unblocks
the next. Items reference the detailed blocks in §3–§6. **Phases 0–3 are the
launch-critical path**; 4–5 polish the launch splash; 6–7 are post-launch.

### At a glance

| Phase | Theme | Items | What it ships |
|------|-------|-------|-------|
| **0** | Identity & version foundations | C5, C7 | Correct name / module / version everywhere |
| **1** | Discoverable & installable | C3, C1 | One-line install + demo in README |
| **2** | First-run success (onboarding) | C2, C4, H3 | New user reaches live chat, zero infra |
| **3** | Reliability & retention | C6, H4, H5 | Notifications + trustworthy states |
| **4** | Terminal-native polish | H1, H2, H6 | Respects terminal, themeable, accessible |
| **5** | Web presence & shareable demo | H7, D6 | Public landing page + self-updating demo |
| **6** | Delight / wow | D2, D4, D1, D5, D3 | Differentiators people screenshot |
| **7** | Platform & long-term | L1, L2, L3, L4 | More networks, self-host, verification, plugins |

---

### Phase 0 — Identity & version foundations  ☑
- **Goal:** Lock the canonical name, module path, and version string *before*
  anything ships publicly.
- **Items:** C5 (naming / module path), C7 (version stamp + stability note).
- **Depends on:** nothing — start here.
- **Do first because:** renaming the module path after users `go install` churns
  their imports; the first public release must already be correct.
- **Phase done when:** one name used everywhere; `go install <path>@latest`
  builds; `marga --version` prints real semver; README states pre-1.0 stability.

### Phase 1 — Discoverable & installable  ☐
- **Goal:** A stranger can find the project and install it in one line.
- **Items:** C3 (demo GIF/asciinema), C1 (publish binaries & packages).
- **Depends on:** Phase 0 (name/version stable before cutting a release).
- **Phase done when:** `brew install …/marga` **and** a GitHub Releases download
  both work on macOS/Linux/Windows; README leads with one-line install and a
  demo above the fold.

### Phase 2 — First-run success (onboarding)  ☑
- **Goal:** A brand-new user reaches a live chat session with zero infrastructure.
- **Items:** C2 (Matrix-first onboarding), C4 (remove token-in-URL web setup),
  H3 (first-run coach overlay).
- **Batch note:** C2 and C4 both edit `internal/setup/wizard.go` — do together to
  avoid touching the same code twice.
- **Depends on:** can run parallel to Phase 1; sequence after so new installs meet
  the fixed flow.
- **Phase done when:** a new user picks Matrix, authenticates, and sees messages
  without editing config or standing up a relay; no access token appears in any
  URL; the coach overlay shows exactly once.

### Phase 3 — Daily-driver reliability & retention  ☐
- **Goal:** People keep marga open and come back to it.
- **Items:** C6 (notifications), H4 (connection/offline affordances),
  H5 (empty/error states).
- **Depends on:** Phase 2 (real usage exists to retain).
- **Phase done when:** a mention while backgrounded notifies (OS + bell);
  connection state and next-retry are always visible; no blank/ambiguous states
  on common paths.

### Phase 4 — Terminal-native polish  ☐
- **Goal:** Looks and feels right in *any* terminal, and is accessible.
- **Items:** H1 (respect terminal bg + `NO_COLOR`), H2 (config-driven themes),
  H6 (accessibility pass).
- **Batch note:** all three center on `internal/tui/styles.go`; H1 and H6 share
  the `NO_COLOR` work.
- **Phase done when:** `theme=none` inherits the terminal background; `NO_COLOR`
  disables color; custom palettes load from config; status is legible without
  relying on color alone.

### Phase 5 — Web presence & shareable demo  ☐
- **Goal:** A shareable page and a demo that never goes stale.
- **Items:** H7 (GitHub Pages landing/docs), D6 (`vhs`-scripted demo in CI).
- **Depends on:** Phase 1 (demo asset + install lines exist), Phase 0 (name).
- **Phase done when:** a public URL shows the demo + per-OS install; the demo
  regenerates automatically on release.

### Phase 6 — Delight / wow  ☐
- **Goal:** Differentiators people screenshot and recommend.
- **Items (suggested order):** D2 (command palette), D4 (unread badges +
  quick-switcher), D1 (rich rendering), D5 (ambient panels), D3 (reactions/threads).
- **Depends on:** stable fundamentals (Phases 0–4).
- **Phase done when:** each item's "done when" in §5 is met.

### Phase 7 — Platform & long-term  ☐
- **Goal:** Make marga irreplaceable.
- **Items:** L1 (more networks), L2 (self-hostable relay + retention/deletion),
  L3 (Matrix device verification UI), L4 (plugin/scripting surface).
- **Depends on:** post-launch.
- **Phase done when:** each item's "done when" in §6 is met.

---

## 3. Critical — must-have before calling it launch-ready

### C1 · Publish pre-built binaries & packages
- **Status:** ☐  ·  **Priority:** P0  ·  **Complexity:** Low  ·  **Phase:** 1
- **Why:** README's primary install is `git clone && make build` needing Go
  1.25+ — a wall for non-Go users. `.goreleaser.yml` already defines
  brew/scoop/nix/aur/deb/rpm/apk targets, but nothing is published. Install
  friction is the #1 drop-off for CLI tools; one-line install is often ~10×
  conversion.
- **Work:**
  - Verify `.github/workflows/release.yml` actually runs goreleaser on tag push;
    add/fix if it doesn't.
  - Create the distribution repos referenced in `.goreleaser.yml`:
    `kunthive-Labs/homebrew-tap`, `kunthive-Labs/scoop-bucket` (decide whether to
    enable nixpkgs/AUR now or later).
  - Wire required secrets (release `GITHUB_TOKEN`/PAT for cross-repo pushes,
    `AUR_KEY` if AUR).
  - Tag a release and cut a GitHub Release with archives + `checksums.txt`.
  - Rewrite README **Install**: lead with `brew install kunthive-Labs/tap/marga`,
    Scoop, and "download from Releases"; demote build-from-source.
- **Files:** `.goreleaser.yml`, `.github/workflows/release.yml`, `README.md`.
- **Done when:** `brew install …/marga` and a Releases download both yield a
  working binary on macOS/Linux/Windows; README reflects the one-line paths.

### C2 · Make Matrix the zero-setup onboarding default; reframe Discord as "advanced"
- **Status:** ☑  ·  **Priority:** P0  ·  **Complexity:** Med  ·  **Phase:** 2
- **Why:** The wizard opens with *"Connect Marga to your Discord server"* and
  offers Terminal/Web — but Discord needs a self-hosted relay **and** a
  registered Discord app (bundled endpoints were removed; `config.example.toml`
  ships placeholder `marga.kunthive.com` URLs). A first-timer who picks Discord
  hits a dead end. The one path that works with zero infra — **Matrix** — isn't
  led with. First-run success is the whole ballgame.
- **Work:**
  - In `internal/setup/wizard.go`, restructure so the **first** choice is the
    network: *"Matrix — works now, no server needed"* vs *"Discord — advanced,
    needs a relay."*
  - Add a Matrix quick-connect path (homeserver + user id + no-echo password
    already supported per CHANGELOG) that lands the user in a room.
  - Relabel Discord clearly; link `docs/SELF_HOSTING.md`.
  - Remove the hardcoded Discord-only copy in `RunSetup`.
- **Files:** `internal/setup/wizard.go`, `internal/setup/ui.go`,
  `cmd/marga/main.go` (setup invocation), `README.md` Quick start.
- **Done when:** a brand-new user with no config can choose Matrix, authenticate,
  and see messages **without editing config files or standing up infrastructure.**

### C3 · Add a real demo to the README (asciinema cast / GIF)
- **Status:** ☐  ·  **Priority:** P0  ·  **Complexity:** Low  ·  **Phase:** 1
- **Why:** For a TUI this is the hero shot *and* the shareability engine. Today
  the README has a logo but no motion demo — a visitor can't see what they'd get.
- **Work:**
  - Script a 20–30s session with `vhs` (preferred — reproducible `.tape`) or
    `asciinema` + `agg`: connect → chat → switch network → reply → E2EE room.
  - Commit `assets/demo.gif` (and/or `.cast`); embed at the top of `README.md`.
  - Keep the `.tape`/script in-repo so it can be regenerated (see D6).
- **Files:** `assets/` (new), `README.md`, optional `docs/demo.tape`.
- **Done when:** README shows a looping demo above the fold.

### C4 · Remove the token-in-URL web-setup path
- **Status:** ☑  ·  **Priority:** P0  ·  **Complexity:** Med  ·  **Phase:** 2
- **Why:** `handleChooseMethod` (web option) opens
  `…/terminal-setup#token=<access_token>` — the Discord **access token travels in
  a URL fragment to a web host** (`marga.kunthive.com`). Documented honestly in
  `docs/ARCHITECTURE.md`, but it undermines the privacy story; security-minded
  users will flag it.
- **Work:**
  - Either **remove** the web-setup option, or replace the token fragment with a
    short-lived one-time code the client generates and the host exchanges, so the
    long-lived token never leaves the machine.
  - Update the "web-based setup" caveat in `docs/ARCHITECTURE.md`.
- **Files:** `internal/setup/wizard.go` (`handleChooseMethod`, `fetchWebConfig`),
  `docs/ARCHITECTURE.md`.
- **Done when:** no Discord access token is ever placed in a URL/fragment sent to
  a web host; docs updated.

### C5 · Fix naming / identity consistency
- **Status:** ☑  ·  **Priority:** P1  ·  **Complexity:** Low–Med  ·  **Phase:** 0
- **Why:** Product "Marga", repo "Margana", binary `marga`, module
  `github.com/kunthive-Labs/Margana` (mixed-case org+repo — Go escapes uppercase
  with `!`, non-idiomatic, confuses `go install`). Inconsistency taxes
  discoverability, word-of-mouth, and trust.
- **Work:**
  - **Name decided: `Marga`** (confirmed by the official logo/wordmark, added
    2026-07-15).
  - **Resolved 2026-07-15:** the GitHub repo stays **`Margana`** (brand /
    marketing name); the shipped tool, binary, packages, and all prose are
    **`marga`** (proper noun **Marga**). The Go module path
    `github.com/kunthive-Labs/Margana` is **kept as-is** — it must track the repo,
    so there is **no import churn** and `go install …@latest` keeps working.
    Fixed only the two spots that misused `Margana` as the *product* name
    (`internal/tui/tui.go` update banner → `Marga`; `internal/network/matrix/adapter.go`
    device display name → `Marga`) and set goreleaser `project_name: marga` so
    download archives are `marga_*` while `release.github.name` stays `Margana`.
- **Files:** `go.mod`, every `.go` import, `.goreleaser.yml`, `README.md`, docs.
- **Done when:** one name used consistently; `go install <path>@latest` works;
  `marga --version` and docs agree.
- **⚠ Dependency:** a module-path rename breaks existing import paths — do it
  **before** C1 (publishing) to avoid churning users. (Phase 0 before Phase 1.)

### C6 · Desktop + terminal-bell notifications on mention/DM
- **Status:** ☐  ·  **Priority:** P1  ·  **Complexity:** Med  ·  **Phase:** 3
- **Why:** A chat client people return to daily must alert them when pinged while
  backgrounded. Mentions are already tracked (`m.notifications`, `m.unreadCount`)
  but there's no OS/bell notification. Without this, nobody keeps it open — and if
  it's not open, it's not their client.
- **Work:**
  - **Verify** none exists (`grep -rn "notify\|beep\|\\\\a" internal/`).
  - On mention/DM while unfocused: emit terminal bell (BEL) + an OS notification
    (pure-Go cross-platform lib, or `terminal-notifier`/`notify-send`/PowerShell
    fallbacks). Add a config toggle (`[notifications]` or `[ui]`).
- **Files:** `internal/tui/events.go` / `internal/tui/messages.go`,
  `internal/config/config.go`, `docs/CONFIGURATION.md`.
- **Done when:** a mention while marga is backgrounded produces a
  visible/audible notification; configurable on/off.

### C7 · Stamp a real version + state stability posture
- **Status:** ☑  ·  **Priority:** P1  ·  **Complexity:** Low  ·  **Phase:** 0
- **Why:** `version` defaults to `"dev"` in `cmd/marga/main.go:34`.
  `marga --version → dev` reads as unfinished and erodes credibility.
- **Work:**
  - Ensure release builds always stamp `-X main.version` (goreleaser does; verify
    `make build` in `Makefile` does too, e.g. from `git describe`).
  - Add a short pre-1.0 stability note to the README (expect breaking changes).
- **Files:** `cmd/marga/main.go`, `Makefile`, `README.md`.
- **Done when:** released `marga --version` prints real semver; README states
  stability.

---

## 4. High-impact improvements

### H1 · Respect terminal background + honor `NO_COLOR`
- **Status:** ☐  ·  **Priority:** P1  ·  **Complexity:** Low–Med  ·  **Phase:** 4
- **Why:** Default theme hardcodes `themeBg = #000000` (`internal/tui/styles.go`),
  which fights transparent terminals and breaks on light backgrounds. The
  riced-terminal crowd is your best evangelist base and hates palette overrides.
- **Work:** add a `none`/`terminal` theme that doesn't set `Background(...)`
  (inherit the terminal); detect and honor `NO_COLOR`.
- **Files:** `internal/tui/styles.go`, `cmd/marga/main.go` (`ApplyTheme`),
  `internal/config/config.go`.
- **Done when:** with `theme=none`, transparent terminals show through and light
  terminals are readable; `NO_COLOR` disables color.

### H2 · Config-driven / custom themes
- **Status:** ☐  ·  **Priority:** P2  ·  **Complexity:** Med  ·  **Phase:** 4
- **Why:** Themes are hardcoded in Go (`default`, `dracula`, `solarized`).
  Personalization drives attachment and "here's my setup" screenshots → sharing.
- **Work:** load palettes from config (e.g. `[theme.<name>]` blocks or a themes
  file; base16-style keys); keep built-ins as fallback.
- **Files:** `internal/tui/styles.go`, `internal/config/config.go`,
  `docs/CONFIGURATION.md`.
- **Done when:** a user-defined palette in config can be selected and applied.

### H3 · First-run coach overlay in the TUI
- **Status:** ☑  ·  **Priority:** P2  ·  **Complexity:** Low  ·  **Phase:** 2
- **Why:** After first connect the screen is blank-ish; the status-bar hint is
  easy to miss. A dismissible overlay ("Enter send · Ctrl+B channels · Ctrl+H
  help · `/` commands") turns "now what?" into guided discovery.
- **Work:** one-time overlay gated by a persisted `firstRun` flag.
- **Files:** `internal/tui/tui.go`, `internal/tui/view.go`, config/state.
- **Done when:** shown once, dismissible, never shown again.

### H4 · Better connection / offline affordances
- **Status:** ☐  ·  **Priority:** P2  ·  **Complexity:** Low–Med  ·  **Phase:** 3
- **Why:** Status bar shows connected/disconnected/reconnecting but no reconnect
  countdown or clear offline banner; ambiguity reads as bugs.
- **Work:** add a reconnect countdown + an explicit offline banner with a retry
  hint.
- **Files:** `internal/tui/view.go` (`renderStatusBar`), network reconnect logic.
- **Done when:** the user always knows connection state and the next retry.

### H5 · Warmer empty & error states
- **Status:** ☐  ·  **Priority:** P2  ·  **Complexity:** Low  ·  **Phase:** 3
- **Why:** "no mentions yet" is good — extend the pattern. The main chat empty
  state and connection failures should suggest the next action.
- **Work:** chat empty state ("No messages yet — say hi, or `/join #general`");
  actionable relay/connect errors linking `docs/SELF_HOSTING.md`.
- **Files:** `internal/tui/viewport.go`, `internal/tui/view.go` (errors).
- **Done when:** no blank/ambiguous states remain on the common paths.

### H6 · Accessibility pass
- **Status:** ☐  ·  **Priority:** P2  ·  **Complexity:** Med  ·  **Phase:** 4
- **Why:** Status is encoded by color alone; contrast varies by theme; broadens
  reach and signals craft.
- **Work:** add glyphs/text alongside color for status, verify contrast across
  themes, honor `NO_COLOR` (overlaps H1), document screen-reader behavior.
- **Files:** `internal/tui/styles.go`, `internal/tui/view.go`, docs.
- **Done when:** status is legible without color; behavior documented.

### H7 · GitHub Pages landing / docs page
- **Status:** ☐  ·  **Priority:** P2  ·  **Complexity:** Low–Med  ·  **Phase:** 5
- **Why:** The legitimate analog to the brief's "web experience" — one shareable
  page for HN/Product Hunt/socials with the demo + install one-liners + docs.
- **Work:** simple static page (embed the C3 demo, per-OS install, docs links) via
  GitHub Pages.
- **Files:** `docs/` site + Pages workflow (or `gh-pages`).
- **Done when:** a public URL exists to share.

---

## 5. Delightful "wow" enhancements

- **D1 · Rich in-terminal rendering** (Phase 6) — syntax-highlighted code blocks,
  link previews, inline images (image protocol + `internal/tui/markdown.go`
  already exist). *Med · P2*
- **D2 · Fuzzy command palette (`Ctrl+K`)** (Phase 6) — jump to any
  channel/server/command. *Med · P2*
- **D3 · Emoji reactions & threads** (Phase 6) — makes it feel like a real client.
  *Med–High · P3*
- **D4 · Cross-server unread badges + quick-switcher** (Phase 6) — one keystroke
  across Discord + Matrix unreads; turns multi-network into a visible superpower.
  *Med · P2*
- **D5 · Ambient panels** (Phase 6) — generalize the existing GitHub-activity
  sidebar (`renderGithubActivity`) into configurable panels (CI, RSS). Novel
  differentiator. *Med · P3*
- **D6 · `vhs`-scripted demo in CI** (Phase 5) — auto-regenerate the README demo
  on release so it never goes stale; doubles as visual regression. *Low–Med · P3*

---

## 6. Long-term vision

- **L1 · Add networks (deliver the pluggable-adapter promise)** (Phase 7) — the
  relay contract is already protocol-neutral (`docs/ARCHITECTURE.md`). Slack/IRC/
  XMPP/Telegram → **"one terminal for every chat network."** Start with one.
  *High · P2*
- **L2 · One-command self-hostable relay** (Phase 7) — `docker compose up` relay
  with a retention window + "delete my data" endpoint (both currently missing per
  ARCHITECTURE.md). Turns the biggest trust weakness into a strength. *High · P2*
- **L3 · Matrix device verification / cross-signing UI** (Phase 7) — currently
  trust-on-first-use only; serious Matrix users expect verification. *High · P3*
- **L4 · Plugin / scripting surface** (Phase 7) — custom commands, bots,
  keybindings; the mechanism by which a tool becomes a platform. *High · P3*

---

## 7. Out of scope (from the original brief — intentionally NOT doing)

The initiating brief described a **data-intelligence web app** (SEO, mobile web,
dashboards, data visualization, "market intelligence for VCs / students /
researchers"). **None of it applies** — Marga is a terminal chat client with no
web surface and a different audience. Do not reintroduce these here:

- Web pages / responsive mobile-web / SEO meta / shareable page URLs
- Data visualization, charts, dashboards, "trends/insights" storytelling
- Audience targeting for VCs / analysts / data-curious visitors

*(If a separate data product is ever wanted, scope it as its own project.)*

---

## 8. Progress log

| Date | Phase | Item | Status | Notes |
|------|-------|------|--------|-------|
| 2026-07-15 | — | — | planning | Roadmap created; direction = harden the TUI for launch. |
| 2026-07-15 | — | — | planning | Broke work into 8 sequential phases (§2). |
| 2026-07-15 | 0 | C5 (branding) | ◐ | New **MARGA** logo added: `assets/logo-master.png` (1024² master) + root `logo.png` (cropped 512², replaces the old ASCII tile). README `<img>` width 120→160; logo bundled in release archives. Brand name confirmed = **Marga**. |
| 2026-07-15 | 0 | C5 (naming) | ☑ | Decided: repo=**Margana** (brand), tool/binary/packages=**marga**. Module path kept (`github.com/kunthive-Labs/Margana`) → no import churn, `go install` intact. Fixed 2 product-name misuses (tui update banner, matrix device name); goreleaser `project_name: marga`. |
| 2026-07-15 | 0 | C7 (version) | ☑ | `resolveVersion()` added in `cmd/marga/main.go`: ldflags stamp → module build-info fallback (covers `go install @latest`) → `dev`; leading `v` trimmed to match goreleaser. Pre-1.0 stability note added to README. Makefile/goreleaser already stamp `-X main.version`. |
| 2026-07-15 | 0 | Phase 0 | ☑ | Identity & version foundations complete. To actually emit `0.1.0`, tag `v0.1.0` at the release commit (Phase 1 release step). |
| 2026-07-16 | 2 | C2 (Matrix-first onboarding) | ☑ | Wizard is network-first (Matrix vs Discord-advanced). New `NeedsOnboarding` gate; `cmd/marga` no longer rejects a brand-new user (Load-fails-with-no-file → onboard with Discord off) and skips pre-wizard Discord OAuth until Discord is chosen. Matrix quick-connect writes a valid `[[networks]]` entry + disables Discord; adapter prompts for the password at Connect. Verified end-to-end against an empty config dir. |
| 2026-07-16 | 2 | C4 (remove token-in-URL) | ☑ | Deleted the web-setup wizard branch + `fetchWebConfig`/`webSetupConfig` and their tests; no access token ever enters a URL. `server.web_setup_url` kept inert (deprecated) to avoid churn. Docs updated: ARCHITECTURE, SECURITY, CONFIGURATION, OPERATIONS, SELF_HOSTING, config.example.toml. |
| 2026-07-16 | 2 | H3 (coach overlay) | ☑ | One-time first-run overlay gated by persisted `ui.coach_shown`; dismissed by any key, then saved. Mirrors help-modal styling. |
| 2026-07-16 | 2 | Phase 2 | ☑ | First-run success complete: a brand-new user picks Matrix, is prompted for credentials, and reaches chat with zero infra; no token in any URL; coach shows once. `go build`/`vet`/`test` green (16 pkgs). Not committed (per request). |

---

## 9. Appendix — file map (where things live)

| Area | Path |
|------|------|
| Entry point, CLI flags, greeting, `version` var, server picker | `cmd/marga/main.go` |
| First-run wizard (Discord + web-setup token path) | `internal/setup/wizard.go`, `internal/setup/ui.go` |
| TUI model / event loop | `internal/tui/tui.go`, `internal/tui/events.go`, `internal/tui/messages.go` |
| Rendering: status bar, panels, help modal, empty/error states | `internal/tui/view.go`, `internal/tui/viewport.go` |
| Themes / colors (hardcoded `#000000` default bg) | `internal/tui/styles.go` |
| Input, autocomplete, completions | `internal/tui/input.go`, `internal/tui/completion_test.go` |
| Markdown / image rendering | `internal/tui/markdown.go`, `internal/tui/image.go` |
| GitHub activity sidebar | `internal/tui/github.go` (`renderGithubActivity` in `view.go`) |
| Config + env overrides | `internal/config/config.go`, `config.example.toml` |
| Network adapters (Discord relay / Matrix) | `internal/network/`, `internal/network/discordrelay/`, `internal/network/matrix/` |
| Local history store (SQLite) | `internal/db/store.go` |
| Slash commands | `internal/commands/` |
| Release / packaging (brew/scoop/nix/aur/nfpm) | `.goreleaser.yml`, `.github/workflows/release.yml` |
| Architecture, privacy caveats, relay contract | `docs/ARCHITECTURE.md` |
| Other docs | `docs/CONFIGURATION.md`, `docs/OPERATIONS.md`, `docs/SELF_HOSTING.md`, `docs/TROUBLESHOOTING.md` |
