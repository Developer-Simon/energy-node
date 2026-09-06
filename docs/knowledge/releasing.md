---
title: "Cutting a release"
---

# Cutting a release

Releases are cut from **Git tags**. Pushing a tag that matches `v*` triggers the
[`Release`](https://github.com/Developer-Simon/energy-node/blob/main/.github/workflows/release.yml)
workflow, which builds the dashboard binary, attaches the archives and publishes
a GitHub Release. There is nothing to run by hand on the release side beyond
pushing the tag.

## Publishing `vX.Y.Z`

1. **Bump the version if needed.** The pre-commit hook only auto-bumps the patch
   level in `dashboard/VERSION` (and `src/VERSION`) for commits on `main`. Set
   the `MAJOR` / `MINOR` in `dashboard/VERSION` by hand when the release is not a
   patch.
2. **Regenerate the changelog:**

   ```sh
   ./scripts/generate_changelog.sh dashboard
   ```

   This refreshes `dashboard/CHANGELOG.md`. The section for the version being
   released is what becomes the GitHub Release notes, so make sure it reads the
   way you want it to.
3. **Commit, tag and push:**

   ```sh
   git tag vX.Y.Z
   git push origin vX.Y.Z
   ```

## What the workflow does

On a `v*` tag push, [`Release`](https://github.com/Developer-Simon/energy-node/blob/main/.github/workflows/release.yml):

1. Runs `go vet ./...` and `go test ./...` in `dashboard/`.
2. Cross-compiles the dashboard binary (`CGO_ENABLED=0`, `-trimpath`,
   `-ldflags "-s -w"`) for:
   - `linux/arm/v6` — Raspberry Pi 1 / Pi Zero W
   - `linux/arm64`
   - `linux/amd64`
3. Packs each build as `energy-node-dashboard_<version>_linux_<arch>.tar.gz` and
   writes a `SHA256SUMS` file over the archives.
4. Assembles the release notes: the matching section of `dashboard/CHANGELOG.md`,
   extracted by `scripts/release/extract-changelog.sh`. If there is no section
   for the tag, the workflow falls back to GitHub's auto-generated notes.
5. Creates the GitHub Release with `--verify-tag`, attaching the archives and
   `SHA256SUMS`.

## Pre-releases

A tag with a pre-release suffix — `vX.Y.Z-rc1`, `vX.Y.Z-beta1`, anything with a
`-` after the version — is published as a **pre-release** (`gh release create
--prerelease`). Everything else is identical to a normal release.
