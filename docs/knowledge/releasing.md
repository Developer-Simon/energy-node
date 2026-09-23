---
title: "Cutting a release"
---

# Cutting a release

Releases are cut from **Git tags**. Pushing a tag that matches `v*` triggers the
[`Release`](https://github.com/Developer-Simon/energy-node/blob/main/.github/workflows/release.yml)
workflow, which attaches the signed node bundles and the installer binaries to
a GitHub Release and publishes it. There is nothing to run by hand on the release side beyond
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
2. Assembles the release notes: the matching section of `dashboard/CHANGELOG.md`,
   extracted by `scripts/release/extract-changelog.sh`. If there is no section
   for the tag, the workflow falls back to GitHub's auto-generated notes.
3. Creates the GitHub Release as a **draft** with `--verify-tag`.
4. Calls two reusable workflows in parallel, both attaching to that draft:
   - [`Bundle Release`](https://github.com/Developer-Simon/energy-node/blob/main/.github/workflows/bundle-release.yml)
     builds the signed `energy-node-v<version>-<arch>.tar.gz` bundles for
     `armv6`, `arm64` and `amd64`. The in-dashboard update and the installer
     download these; a release without them reports "no published package".
     Signing needs the `BUNDLE_SIGNING_KEY` secret (the ed25519 private key
     matching `installer/internal/bundle/signing_key.pub.pem`).
   - [`Installer Release`](https://github.com/Developer-Simon/energy-node/blob/main/.github/workflows/installer-release.yml)
     tests the installer and builds it as
     `energy-node-installer_windows_amd64.exe`,
     `energy-node-installer_macos_universal.zip` (Intel and Apple Silicon) and
     `energy-node-installer_linux_<amd64|arm64>.tar.gz`, plus
     `SHA256SUMS.installer`. The names carry no version, so
     `releases/latest/download/<name>` is a stable link.
5. Publishes the draft once both succeeded. If either fails, the release stays
   a draft and nodes keep seeing the previous one.

Both reusable workflows can also be run by hand (`workflow_dispatch` with a
`tag`) to re-attach their assets to an existing release.

## Pre-releases

A tag with a pre-release suffix — `vX.Y.Z-rc1`, `vX.Y.Z-beta1`, anything with a
`-` after the version — is published as a **pre-release** (`gh release create
--prerelease`). Everything else is identical to a normal release.

## Components and their version files

Every component carries its own version and its own `CHANGELOG.md`. The list
lives in [`scripts/version/components.sh`](https://github.com/Developer-Simon/energy-node/blob/main/scripts/version/components.sh);
the changelog targets are the `<target>` arguments of
`scripts/generate_changelog.sh`.

| Component | Version file | Changelog | Target |
|---|---|---|---|
| Dashboard | `dashboard/VERSION` | `dashboard/CHANGELOG.md` | `dashboard` |
| Services (shared) | `services/VERSION` | `services/CHANGELOG.md` | `services` |
| APsystems EZ1 | `services/apsystems_ez1/manifest.json` (`version`) | `services/apsystems_ez1/CHANGELOG.md` | `service:apsystems_ez1` |
| Automation | `services/automation/manifest.json` (`version`) | `services/automation/CHANGELOG.md` | `service:automation` |
| Battery SoC | `services/battery_soc/manifest.json` (`version`) | `services/battery_soc/CHANGELOG.md` | `service:battery_soc` |
| Shelly | `services/shelly/manifest.json` (`version`) | `services/shelly/CHANGELOG.md` | `service:shelly` |
| Trucki | `services/trucki/manifest.json` (`version`) | `services/trucki/CHANGELOG.md` | `service:trucki` |
| Tuya | `services/tuya_mqtt/manifest.json` (`version`) | `services/tuya_mqtt/CHANGELOG.md` | `service:tuya_mqtt` |
| `energy_node_common` | `libs/energy_node_common/VERSION` | `libs/energy_node_common/CHANGELOG.md` | `common` |
| `battery_soc_core` | `libs/battery_soc_core/VERSION` | `libs/battery_soc_core/CHANGELOG.md` | `battery_soc_core` |
| HA integration | `integrations/homeassistant/.../manifest.json` (`version`) | `integrations/homeassistant/CHANGELOG.md` | `ha-integration` |
| Bootstrap | `scripts/bootstrap/VERSION` | `scripts/bootstrap/CHANGELOG.md` | `bootstrap` |
| Installer | `installer/VERSION` | `installer/CHANGELOG.md` | `installer` |
| Installer web UI | `installer/webui/VERSION` | `installer/webui/CHANGELOG.md` | `installer-webui` |

`scripts/version/components.json` carries the same components with a display
label, a kind and a `bundle` flag (does it ship in the release bundle). It
exists for tooling that needs those three facts — `changelog.json` in the bundle
reads it. A test (`scripts/tests/test_components_table.py`) fails when it drifts
from `components.sh`, the generator targets or the workflow's
`CHANGELOG_TARGETS`, so a new component has to be added to all of them.

Notes:

* A service's version lives in the `version` field of its `manifest.json` as a
  **bare** semver (`0.4.0`, no `v`), the same convention the HA integration
  uses. The manifest already travels into the bundle as
  `config/manifests/<service_id>.json`, so the version ships with it, and
  `make_bundle.sh` additionally writes it into the bundle manifest's step
  entry (`steps[].version`).
* Per-service versioning started at **v0.4.0**; `services/VERSION` was moved to
  `v0.4.0` at the same time so no version appears to go backwards. Commits from
  before that point fall back to `services/VERSION` when the changelog
  generator needs a version for them.
* Work inside one service bumps only that service. Work directly under
  `services/` (shared config, shared tests) bumps only `services/VERSION`.

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
