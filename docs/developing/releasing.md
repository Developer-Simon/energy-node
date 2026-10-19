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
2. Builds the release notes. They start with the highlights of every bundle
   component since the previous final release, in English and in a folded
   German block, written by `scripts/release/release_highlights.py`. The
   matching section of `dashboard/CHANGELOG.md`, extracted by
   `scripts/release/extract-changelog.sh`, follows. If there is no section for
   the tag, it uses GitHub's generated notes.
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

The marker names the first release that contains the page's last change, including a change to one of its screenshots,
with the component version from that release. A page whose last change is not
in any release yet shows the version of that commit and is flagged as
unreleased.

Versions per service started at v0.4.0, and `services/VERSION` was moved to
`v0.4.0` at the same time so that no version goes backwards. For older commits
the changelog generator falls back to `services/VERSION`.

Work inside one service bumps only that service. Work directly under
`services/` (shared config, shared tests) bumps only `services/VERSION`.

### `changelog.json`

Every bundle carries `changelog/<version>.json` for each of the last 20 bundle
versions, and a `changelog.json` with the newest bundle version only. Each
per-version file lists the component releases that came with that bundle
version: every release newer than the component's version at the previous
final tag, up to its version at this tag. Pre-release tags do not count as a
previous tag, so the range always starts after the last final release. A
component without new releases still appears with an empty `releases` list.
`scripts/build/make_changelog_json.py` builds the files from the `CHANGELOG.md`
of every component with `bundle: true` in `scripts/version/components.json`.
`make_bundle.sh` writes them before the manifest is hashed, so `manifest.files`
and the signature cover them.

The updater adds new and changed files to `/var/lib/energy-node-installer/changelog/`
once a bundle counts as applied. It never deletes a file, so the node keeps
every version it has received, and a changed file (for example a corrected
translation) replaces the old copy. The newest bundle version is also copied to
`/var/lib/energy-node-installer/changelog.json`. Older installers and updaters
read only that file, so they show what is new in the bundle and nothing older.
The dashboard and the installer merge the files of the directory, the newest
file first, and show as many versions as the user chose. A bundle without any
changelog files installs and displays normally, only without a changelog.

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

### Translations

Every highlight also needs a German text. It lives in
`changelog/highlights/<PR>.json`, one file per pull request:

```json
{"de": {"*": "Die Versionsseite öffnet mit den Highlights.", "dashboard": "Text nur für das Dashboard"}}
```

`*` applies to every component without its own line, every other key is a
target of `scripts/generate_changelog.sh`. While the PR is open, the source is
the "Highlight translations" section of its description:

```text
Highlights (de):
- Die Versionsseite öffnet mit den Highlights.
- dashboard: Text nur für das Dashboard
```

`scripts/dev/create-pr.sh` asks for these lines (or reads `PR_TITLE_DE` and
`PR_HIGHLIGHTS_DE_FILE`). The Version bump workflow writes the file in its
changelog commit, and the CI check "Highlight translations" fails while a
highlight of the PR has no German text. Entries of single commits without a PR
number are never checked. After the merge the file may be edited by hand, for
example to fix a word or to translate an older release. `changelog.json`
carries the texts as `"i18n": {"de": "…"}`, the dashboard, the installer and
the release notes show them and fall back to English.

The directory has three kinds of translation files:

* `<PR>.json` holds the texts of one pull request that is not released yet.
* `v<MAJOR>.<MINOR>.<PATCH>.json`, for example `v0.10.0.json`, holds the texts
  of one release.
* `archive-v<MAJOR>.json`, for example `archive-v0.json`, holds the texts of all
  releases of one major version.

The release and archive files have the form `{"<PR>": {"de": {...}}}`. A per-PR
file overrides them for the languages it has. The per-version changelog files
are regenerated from the current changelogs and translations on every release.
A correction in a release file or in an archive therefore reaches the node with
the next bundle, because the updater replaces every file whose content changed.

When a final release tag `vX.Y.Z` is pushed, the workflow
`archive-highlight-translations.yml` runs
`highlight_translations.py archive --tag vX.Y.Z`. It folds every `<PR>.json` into
`vX.Y.Z.json`, moves every older release file into the archive of its major
version and deletes the folded files. The details of a highlight move with its
texts. The workflow then opens a PR with these changes. Merge it on GitHub with
the squash button. `scripts/dev/merge-pr.sh` does not apply to it, because the
PR has no commit message block. The workflow needs the `BUMP_TOKEN` secret, and
without it the job skips with a notice. Pre-release tags are skipped.

### Details for bigger highlights

A highlight can have a longer explanation, an image with a crop and a link to
the guide. These details are the key `details` in the same
`changelog/highlights/<PR>.json` file, next to the language keys. This complete
file is valid and checked by `validate`:

```json
{
  "de": {
    "*": "Ein Relais mit mehreren Kanälen lässt sich pro Kanal schalten."
  },
  "details": {
    "*": {
      "en": "A relay with several channels can switch each channel on its own. Choose the channel of a connection in its card.",
      "de": "Ein Relais mit mehreren Kanälen lässt sich pro Kanal schalten. Den Kanal einer Verbindung wählst du in ihrer Karte.",
      "image": "dashboard-device-map-switch-channel.png",
      "crop": [0.535, 0.17, 0.465, 0.31],
      "doc": "dashboard/device-map.html#switchable-devices"
    }
  }
}
```

The rules:

* `en` is required, and so is every language of `highlight_i18n.LANGUAGES`
  (currently `de`). No text may be empty.
* `*` applies to every component. A key with a target, such as `dashboard`,
  applies to that component only and replaces the `*` object as a whole. It
  needs `en` and the other languages too.
* `image` is the file name of a PNG in `docs/images/`, without a path.
* `crop` needs `image`. It is `[x, y, width, height]` as fractions of the image,
  each from 0 to 1. Width and height must be above 0, and `x + width` and
  `y + height` must not be above 1.
* `doc` is a page under `docs/`. Write it as the `.html` address of its `.md`
  source, such as `dashboard/device-map.html`, or as a folder ending in `/`, such
  as `dashboard/`. The page must exist. The part after `#` is not checked.

The image is pinned. The bundle build turns the file name into a URL on GitHub's
raw file server, `https://raw.githubusercontent.com/Developer-Simon/energy-node/<ref>/docs/images/<file>`.
`<ref>` is the exact tag on `HEAD`, or the commit `HEAD` when no tag points
there. The width and height are read from the PNG header. The `doc` link points
to the documentation site, which is not pinned. The bundle carries the details
as `details` on each PR entry of `changelog.json`.

`highlight_translations.py validate --repo .` checks the details of every file in
`changelog/highlights`, not only those of the open PR. It prints one line per
problem, in the form `#<PR> <key>: <problem>`, and exits with 1. CI runs it, so a
missing text or a wrong image name fails the PR before the merge. A broken detail
also stops the bundle build with a `ValueError` that names the PR and the key.
The images and doc pages that released highlights reference are checked on every run, so renaming or deleting one of them later fails CI and the bundle build, which is why the details entry must be updated first.

To see the highlights as the dialog shows them, run:

```sh
scripts/dev/highlight_details_preview.py --repo . --since v0.9.8
```

It writes `drafts/highlight-preview/index.html`, which Git ignores. The page
shows the `dashboard` highlights newer than `--since`, with the dialog view next
to the whole image. Draw a frame on the image with the mouse to set a crop. The
line below the image holds the `crop` value to copy into the JSON. Highlights
without details are marked, and the problems reported by `validate` are listed
at the top. Pass `--out` to write the page somewhere else.

In the What's new dialog, a highlight with details has a **More** link. The
image loads only when the entry is opened. If it cannot be loaded, for example
offline or because the pinned file is not on GitHub yet, the image and the
crop switch disappear, and the text and the link stay.

## One-time setup: the `BUMP_TOKEN` secret

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
