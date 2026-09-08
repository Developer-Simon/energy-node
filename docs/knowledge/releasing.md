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

1. **Bump the version if needed.** Every PR has its touched components' patch
   level bumped automatically on the PR branch by the
   [`Version bump`](https://github.com/Developer-Simon/energy-node/blob/main/.github/workflows/version-bump.yml)
   workflow (`scripts/version/bump-patch.sh`), so `dashboard/VERSION` /
   `services/VERSION` are already current on `main`. Set the `MAJOR` / `MINOR` in
   `dashboard/VERSION` by hand (on the PR branch) when the release is not a
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

## One-time setup: the `BUMP_TOKEN` secret

The [`Version bump`](https://github.com/Developer-Simon/energy-node/blob/main/.github/workflows/version-bump.yml)
workflow pushes the bump commit back onto the PR branch. The default
`GITHUB_TOKEN` can push, but its push does not re-trigger the required status
checks, so the PR would sit unmergeable — the workflow uses a separate token
instead:

1. Create a **fine-grained personal access token** (GitHub → *Settings* →
   *Developer settings* → *Personal access tokens* → *Fine-grained tokens*),
   scoped to this repository, with **Repository permissions → Contents:
   Read and write**.
2. Save it as the repo secret **`BUMP_TOKEN`** (repo → *Settings* → *Secrets and
   variables* → *Actions* → *New repository secret*).

Without the secret the workflow still runs, but only in check-only mode: it
fails the PR when a component was changed without its patch version being
bumped, and you bump it by committing your work and running
`scripts/version/bump-patch.sh` on the branch (it diffs committed history,
`origin/main...HEAD`).
