# Releasing Marga

Marga distributes pre-built binaries and packages with [GoReleaser]. Pushing a
`v*` tag triggers [`.github/workflows/release.yml`](../.github/workflows/release.yml),
which cross-compiles every target and publishes them in one shot.

## One-time setup (before the first release)

1. **Create the distribution repos** under the `kunthive-Labs` org — empty,
   public is fine:
   - `kunthive-Labs/homebrew-tap` — the generated Homebrew cask is committed to
     its `Casks/` directory, enabling `brew install kunthive-Labs/tap/marga`.
   - `kunthive-Labs/scoop-bucket` — the generated Scoop manifest is committed
     here.

2. **Add a release token (`GH_PAT`).** GoReleaser pushes the cask/manifest into
   those *other* repos, which the workflow's default `GITHUB_TOKEN` cannot do.
   Create a fine-grained PAT with **Contents: read/write** on `homebrew-tap`,
   `scoop-bucket`, and `Margana`, then add it as the **`GH_PAT`** Actions secret
   on the `Margana` repo. Without it the release still produces the GitHub
   Release, archives, checksums, and Linux packages — only the brew/scoop steps
   fail.

3. *(Optional, deferred)* Nix and AUR publishing are disabled by default
   (`skip_upload: "true"` in [`.goreleaser.yml`](../.goreleaser.yml)). To enable
   them later, create `kunthive-Labs/nixpkgs` (and set an `AUR_KEY` secret for
   AUR), then flip those blocks to `skip_upload: "false"`.

## Cutting a release

1. Ensure `main` is green and `CHANGELOG.md` reflects the new version.
2. Tag and push:
   ```bash
   git tag -a v0.1.0 -m "marga v0.1.0"
   git push origin v0.1.0
   ```
3. Watch the **Release** workflow in the Actions tab. On success you'll have:
   - a GitHub Release with `marga_<version>_<os>_<arch>.{tar.gz,zip}` +
     `checksums.txt`;
   - `.deb` / `.rpm` / `.apk` / Arch packages attached;
   - `brew install kunthive-Labs/tap/marga` working;
   - `scoop install marga` working (after `scoop bucket add kunthive-Labs …`).

## Validate locally before tagging (optional but recommended)

```bash
goreleaser check                                      # config is valid
goreleaser build --snapshot --clean --single-target   # host binary compiles
goreleaser release --snapshot --clean                 # full dry run, nothing published
```

The snapshot artifacts land in `dist/` (git-ignored).

## Version stamping

`marga --version` is stamped from the git tag via `-X main.version` — set in
both [`.goreleaser.yml`](../.goreleaser.yml) (`builds.ldflags`) and the
[`Makefile`](../Makefile). Builds without a tag (e.g. `go install …@latest`)
resolve the version from the module build-info, falling back to `dev`.

[GoReleaser]: https://goreleaser.com
