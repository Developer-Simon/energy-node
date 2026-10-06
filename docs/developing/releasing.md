---
title: "Cutting a release"
redirect_from:
  - /knowledge/releasing.html
---

# Cutting a release

Releases are cut from Git tags. Pushing a tag that matches `v*` starts the
[`Release`](https://github.com/Developer-Simon/energy-node/blob/main/.github/workflows/release.yml)
workflow. It attaches the signed node bundles and the installer binaries to a
GitHub Release and publishes it. Apart from pushing the tag, nothing has to be
done by hand. The [version overview](versions.md) shows which component
versions each release contains.

## Publishing `vX.Y.Z`

1. **Bump the version if needed.** The
   [`Version bump`](https://github.com/Developer-Simon/energy-node/blob/main/.github/workflows/version-bump.yml)
   workflow (`scripts/version/bump-patch.sh`) raises the patch level of every
   component a PR touches on the PR branch, so `dashboard/VERSION` and
   `services/VERSION` are already current on `main`. For a release that is not
   a patch, set `MAJOR` or `MINOR` in `dashboard/VERSION` by hand on the PR
   branch.
2. **Regenerate the changelog:**

   ```sh
   ./scripts/generate_changelog.sh dashboard
   ```

   This refreshes `dashboard/CHANGELOG.md`. The section for the released
   version becomes the GitHub Release notes, so check that it reads well.
3. **Commit, tag and push:**

   ```sh
   git tag vX.Y.Z
   git push origin vX.Y.Z
   ```

## What the workflow does

On a `v*` tag push, [`Release`](https://github.com/Developer-Simon/energy-node/blob/main/.github/workflows/release.yml):

1. Runs `go vet ./...` and `go test ./...` in `dashboard/`.
2. Builds the release notes from the matching section of
   `dashboard/CHANGELOG.md`, extracted by `scripts/release/extract-changelog.sh`.
   If there is no section for the tag, it uses GitHub's generated notes.
3. Creates the GitHub Release as a **draft** with `--verify-tag`.
4. Calls two reusable workflows in parallel, both attaching to that draft:
   - [`Bundle Release`](https://github.com/Developer-Simon/energy-node/blob/main/.github/workflows/bundle-release.yml)
     builds the signed `energy-node-v<version>-<arch>.tar.gz` bundles for
     `armv6`, `arm64` and `amd64`. The dashboard's update and the installer
     download them. Without them, a release reports "no published package".
     Signing needs the `BUNDLE_SIGNING_KEY` secret (the ed25519 private key
     matching `installer/internal/bundle/signing_key.pub.pem`).
   - [`Installer Release`](https://github.com/Developer-Simon/energy-node/blob/main/.github/workflows/installer-release.yml)
     tests the installer and builds it as
     `energy-node-installer_windows_amd64.exe`,
     `energy-node-installer_macos_universal.zip` (Intel and Apple Silicon) and
     `energy-node-installer_linux_<amd64|arm64>.tar.gz`, plus
     `SHA256SUMS.installer`. The names carry no version, so
     `releases/latest/download/<name>` is a stable link.
5. Publishes the draft once both have succeeded. If one fails, the release
   stays a draft and nodes keep seeing the previous one.

You can also run both reusable workflows by hand (`workflow_dispatch` with a
`tag`) to attach their assets to an existing release again.

## Pre-releases

A tag with a pre-release suffix such as `vX.Y.Z-rc1` or `vX.Y.Z-beta1` (any `-`
after the version) is published as a pre-release
(`gh release create --prerelease`). Everything else works as for a normal
release.

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

`scripts/version/components.json` lists the same components with a display
label, a kind and a `bundle` flag that says whether the component ships in the
release bundle. Tools that need these three facts read it, for example the
builder of `changelog.json` in the bundle and the
[version overview](versions.md). A test
(`scripts/tests/test_components_table.py`) fails when it differs from
`components.sh`, the generator targets or the workflow's `CHANGELOG_TARGETS`,
so a new component has to be added to all of them.

A service's version is in the `version` field of its `manifest.json` as a bare
semver (`0.4.0`, without `v`), like the HA integration. The manifest goes into
the bundle as `config/manifests/<service_id>.json`, so the version ships with
it. `make_bundle.sh` also writes it into the step entry of the bundle manifest
(`steps[].version`).

A documentation page about one component names it in its front matter as
`component: <id>`, with an `id` from `components.json`. The version marker at
the bottom of the page then shows that component's version next to the Energy
Node version (`scripts/docs/doc-versions.sh`). An unknown id fails the Pages
build.

Versions per service started at v0.4.0, and `services/VERSION` was moved to
`v0.4.0` at the same time so that no version goes backwards. For older commits
the changelog generator falls back to `services/VERSION`.

Work inside one service bumps only that service. Work directly under
`services/` (shared config, shared tests) bumps only `services/VERSION`.

### `changelog.json`

Every bundle carries a `changelog.json`. `scripts/build/make_changelog_json.py`
builds it from the `CHANGELOG.md` of every component with `bundle: true` in
`scripts/version/components.json`, with the newest 20 releases of each.
`make_bundle.sh` writes it before the manifest is hashed, so `manifest.files`
and the signature cover it. Once a bundle counts as applied, the updater and
the installer copy it to `/var/lib/energy-node-installer/changelog.json` next
to `installed-manifest.json`, where the dashboard's version page reads it. The
file is optional everywhere. An older bundle without it installs and displays
normally, only without a changelog.

### Highlights and details

Every entry in `changelog.json` has a `highlight` flag (true or false). The
installer's changelog page and the dashboard's version page open on the
highlights and switch to the full list with **Everything**. The flag is derived
from the `CHANGELOG.md` files, which stay as they are:

* A release that contains entries with a PR number (`(#NN)`, one per squash
  merge) highlights those PR entries of type `feat`, `fix` and `perf`. The
  commits inside the PR are details.
* A release without any PR number (sections from before squash merges)
  highlights every `feat`, `fix` and `perf` entry.
* A breaking change is always a highlight.

The highlight text is the PR title. To word it differently, add a paragraph to
the squash commit message (the fenced block in the PR description):

```text
Highlights:
- The battery state survives a crash.
- dashboard: The version page opens on the highlights.
```

A line `- <target>: text` applies to that component only, with the target
names of `scripts/generate_changelog.sh` (`dashboard`, `installer`,
`service:battery_soc` and so on). A line without a target applies to every
other component the PR touches. The paragraph ends at the first line that
does not start with `- `. The text replaces the PR title in the entry, the
PR number stays.

While a PR is open, its squash commit does not exist yet. The
[`Version bump`](https://github.com/Developer-Simon/energy-node/blob/main/.github/workflows/version-bump.yml)
workflow therefore passes the PR number, title and description to the
generator (`CHANGELOG_PR_NUMBER`, `CHANGELOG_PR_TITLE`,
`CHANGELOG_PR_BODY_FILE`, `CHANGELOG_PR_BASE`). The generator adds the PR's
entry to the open section of every component the PR changes. Editing the title
or the description runs the workflow again and updates the entry, so a release
cut right after the merge already lists the PR.

## One-time setup: the `BUMP_TOKEN` secret

The [`Version bump`](https://github.com/Developer-Simon/energy-node/blob/main/.github/workflows/version-bump.yml)
workflow pushes the bump commit back to the PR branch. The default
`GITHUB_TOKEN` can push, but its push does not start the required status checks
again, so the PR could not be merged. The workflow uses a separate token
instead:

1. Create a fine-grained personal access token (GitHub → *Settings* →
   *Developer settings* → *Personal access tokens* → *Fine-grained tokens*),
   scoped to this repository, with **Repository permissions → Contents:
   Read and write**.
2. Save it as the repo secret **`BUMP_TOKEN`** (repo → *Settings* → *Secrets and
   variables* → *Actions* → *New repository secret*).

Without the secret the workflow only checks. It fails the PR when a component
changed without a patch bump. Commit your work and run
`scripts/version/bump-patch.sh` on the branch to bump it. The script compares
the committed history (`origin/main...HEAD`).
