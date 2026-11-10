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

## Release lines

The repository has two release lines. `main` carries the current release and
its hotfixes. `prerelease` collects the changes for the next release.

### Where a change goes

- A hotfix goes to `main`. Open its PR with `scripts/dev/create-pr.sh`.
- A feature goes to `prerelease`. Open its PR with
  `PR_BASE=prerelease scripts/dev/create-pr.sh <branch>`. Merge it with
  `scripts/dev/merge-pr.sh` as usual. The base stays `prerelease`.

### Versions on prerelease

A component on `prerelease` carries the next patch version plus a counter,
for example `v0.4.6-pre.2`. The version files and manifests hold the same
string without the `v`, such as `0.4.6-pre.2`. The bump workflow counts per
component, so each component has its own counter.

### Updating prerelease from main

The workflow `sync-prerelease.yml` merges `main` into `prerelease` with
`scripts/version/sync_prerelease.py`. A push to `main` does not start it. It
runs in these cases:

- A PR into `prerelease` is opened. Pushing to an open PR does not start it
  again. GitHub starts no run for a PR that already conflicts with `prerelease`.
- You start it by hand with `gh workflow run sync-prerelease.yml`.
- `scripts/dev/prepare-minor.sh` runs the same script itself before a minor
  release.

So `prerelease` can lag behind `main`, and hotfixes only reach it with the next
sync. The sync moves the base of an open PR. If the sync renumbers a version the
PR also changed, the PR conflicts and needs a rebase onto `prerelease`.

The sync also renumbers the pre versions. When `main` releases `v0.4.6`, a
`v0.4.6-pre.2` on `prerelease` becomes `v0.4.7-pre.2`.

If the merge conflicts in code, the run ends red. The workflow also opens or
updates a GitHub issue with the label `prerelease-sync`. The same happens when
both lines changed the same lazy loaded web asset under
`dashboard/internal/webui/static/js/` or `static/css/`. Git merges such a file
cleanly, but the merged file is new on both lines and needs a `?v=` above both
of them. The issue lists these paths as assets.

To fix it, let the script stop with the merge in progress:

```sh
git switch prerelease && git pull --ff-only
python3 scripts/version/sync_prerelease.py --keep
```

The script settles versions and changelogs itself and prints the rest. Resolve
every `CONFLICT` path. For every `ASSET` path, set its `?v=` one above the higher
of the two lines and add a history row as described in
[lazy-assets-cache-busting.md](../_internal/lazy-assets-cache-busting.md). Stage
the files with `git add`, then finish the merge:

```sh
python3 scripts/version/sync_prerelease.py --continue --push
```

`--continue` sets the versions, commits the merge and regenerates the
changelogs exactly like the automatic run. The next run closes the issue, or
close it by hand. Changelog hashes are always 7 characters, so a local run
writes the same text as CI.

### Minor release

On a clean checkout of `prerelease`, run:

```sh
PR_AI_MODEL="Sonnet 5.5" scripts/dev/prepare-minor.sh vX.Y.0 --push
```

The script strips the `-pre` suffix from every component version and sets
`dashboard/VERSION` to `X.Y.0`. It regenerates the changelogs and opens a PR
into `main`. The CI job refuses `-pre.` versions on PRs into `main`.

Merge that PR with a merge commit, not a squash:

```sh
scripts/dev/merge-prerelease.sh <pr-number>
```

Then tag the release as described in [Publishing `vX.Y.Z`](#publishing-vxyz).
The release workflow refuses a final tag while a component still carries a
`-pre.` version.

### Pre-tag

You can tag the state of `prerelease` before the final release, for example
`v0.11.2-pre.5`, with the dashboard version of that state. Start the sync by
hand first, so the tag contains the hotfixes from `main`. The `Release`
workflow creates a GitHub pre-release for the tag (`--prerelease`). It
attaches the same bundles and installer as a final release.

### Known limit

The dashboard's update check ignores version suffixes. A test device on a
`v0.11.2-pre.5` bundle is not offered the final `v0.11.2`. It is only offered
the next higher version.

## Pinned packages

The installation package carries Caddy and Tailscale as Debian packages, one
per architecture. The pins live in `scripts/build/packages.json`. Run the
script before a release:

```sh
bash scripts/build/update_packages.sh          # shows the changes, writes nothing
bash scripts/build/update_packages.sh --write  # writes packages.json
```

It takes the newest Caddy stable release and the newest Tailscale stable
release from the Debian index. Every Caddy package is checked against the
SHA-512 in the upstream checksum file, and `packages.json` pins its SHA-256. The
script writes only when every check passes. Read the diff, then commit
`packages.json` with the release.

The `armv6` Tailscale pin stays at 1.62.0. Its file is a project mirror asset
under the release tag `pinned-packages`, uploaded once by hand, because the
upstream pool drops old versions. The script never bumps it, and its checksum
stays the upstream one. A node installs 1.62.0 and then runs `tailscale update`.
A new pin reruns steps 40 and 70 on the next update of a node.

The `armv6` Tailscale download tries the project mirror first and the upstream
pool second, from `fallback_urls` in `packages.json`. Both serve the same file
with the same SHA-256.

The repository keys for Tailscale and Caddy are in `scripts/bootstrap/keyrings/`
as `tailscale.gpg` and `caddy.gpg`. When an upstream repository changes its
signing key, replace the matching file and check its fingerprint before you
commit it.

## Publishing `vX.Y.Z`

1. **Bump the version if needed.** The
   [`Version bump`](https://github.com/Developer-Simon/energy-node/blob/main/.github/workflows/version-bump.yml)
   workflow (`scripts/version/bump-patch.sh`) raises the patch level of every
   component a PR touches on the PR branch, so `dashboard/VERSION` is already
   current on `main`. For a release that is not
   a patch, set `MAJOR` or `MINOR` in `dashboard/VERSION` by hand on the PR
   branch. For a minor release from the prerelease line, use the steps under
   [Release lines](#minor-release) instead.
2. **Regenerate the changelog:**

   ```sh
   ./scripts/generate_changelog.sh dashboard
   ```

   This refreshes `dashboard/CHANGELOG.md`. The release notes link to it.
3. **Commit, tag and push:**

   ```sh
   git tag vX.Y.Z
   git push origin vX.Y.Z
   ```

4. **Add the story.** Once the release is published, add a few short paragraphs
   above the links that tell what the release brings, like a blog post, on
   GitHub or with `gh release edit`. The workflow cannot write them.

## What the workflow does

On a `v*` tag push, [`Release`](https://github.com/Developer-Simon/energy-node/blob/main/.github/workflows/release.yml):

1. Runs `go vet ./...` and `go test ./...` in `dashboard/`.
2. Builds the release notes with `scripts/release/release_highlights.py`. They
   link to the release on the [What's new](../whats-new.html) page and on the
   [version overview](versions.md), and to `dashboard/CHANGELOG.md` at the tag.
   Two folded blocks below list the highlights of every bundle component since
   the previous final release, one in English and one in German. The English
   block shows the `en` texts of `changelog/highlights`.
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
release. The pre-releases of the prerelease line are described in
[Release lines](#release-lines).

## Components and their version files

Every component carries its own version and its own `CHANGELOG.md`. The list
lives in [`scripts/version/components.sh`](https://github.com/Developer-Simon/energy-node/blob/main/scripts/version/components.sh).
The changelog targets are the `<target>` arguments of
`scripts/generate_changelog.sh`.

| Component | Version file | Changelog | Target |
|---|---|---|---|
| Dashboard | `dashboard/VERSION` | `dashboard/CHANGELOG.md` | `dashboard` |
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

Work inside one service bumps only that service.

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

The `CHANGELOG.md` files keep this text. `changelog.json`, the dashboard, the
installer, the What's new page and the release notes take the English text from
`changelog/highlights/<PR>.json` instead, see [Translations](#translations).
That is where a highlight is shortened.

While a PR is open, its squash commit does not exist yet. The
[`Version bump`](https://github.com/Developer-Simon/energy-node/blob/main/.github/workflows/version-bump.yml)
workflow therefore passes the PR number, title and description to the
generator (`CHANGELOG_PR_NUMBER`, `CHANGELOG_PR_TITLE`,
`CHANGELOG_PR_BODY_FILE`, `CHANGELOG_PR_BASE`). The generator adds the PR's
entry to the open section of every component the PR changes. Editing the title
or the description runs the workflow again and updates the entry, so a release
cut right after the merge already lists the PR.

### What's new page

The [What's new](../whats-new.html) page lists the highlights since v0.8.0, one
section per release, newest first. It shows the bundle components, the installer
and its web UI, but no Home Assistant component. Highlights newer than the last
tag sit at the top under "Next release".

Within a release, the highlights come in this order:

* A highlight with an image (`details.image`) comes first as a large card with
  the image on the right.
* A highlight with a detail text (the `en` text in `details`) follows as a card.
* A highlight with only a tour (`tour`) follows as a collapsed block that lists
  the tour steps.
* A highlight with only a title sits in one collapsed "N more changes" block.

The "By area" view groups the same highlights by area. An area is the navigation
section of the doc page that the highlight links to (`details.doc`). Without a
doc link, the changelog scope decides, and without a scope, the component. All
installer components count as "Installer".

The page shows the English highlight text (`en`) from
`changelog/highlights`, see [Translations](#translations).

### Translations

Every highlight needs an English and a German text. Both live in
`changelog/highlights/<PR>.json`, one file per pull request:

```json
{
  "de": {"*": "Die Statistikseite funktioniert auf dem Handy", "dashboard": "Text nur für das Dashboard"},
  "en": {"*": "The statistics page works on a phone"}
}
```

`*` applies to every component without its own line, every other key is a
target of `scripts/generate_changelog.sh`. Both texts start with a capital
letter, have no full stop at the end, no semicolon and at most 100 characters.
Put the rest into details.

While the PR is open, its description is the source. When the file has no
English text yet, the Version bump workflow copies the text of the PR's
changelog entries (the PR title or the `Highlights:` lines), with a capital
first letter and without the full stop. Later runs keep it. To set it yourself,
add a block to the "Highlight translations" section, it replaces the English
text on every run. The German text comes from the same section:

```text
Highlights (en):
- The statistics page works on a phone
Highlights (de):
- Die Statistikseite funktioniert auf dem Handy
- dashboard: Text nur für das Dashboard
```

`scripts/dev/create-pr.sh` asks for the German lines (or reads `PR_TITLE_DE`
and `PR_HIGHLIGHTS_DE_FILE`). The Version bump workflow writes the file in its
changelog commit, and the CI check "Highlight translations" fails while a
highlight of the PR has no English or German text. Entries of single commits
without a PR number are never checked. After the merge the file may be edited,
for example to fix a word, to translate an older release or to shorten the
English text. `changelog.json` carries the English text as `text` and the other
languages as `"i18n": {"de": "…"}`. The dashboard, the installer, the What's new
page and the release notes show them and fall back to English. The
`CHANGELOG.md` files keep the text of the merge.

To shorten a highlight after the merge, change `en` (and `de` with it, so both
say the same) in the file that holds the PR: `<PR>.json`, `v<X.Y.Z>.json` or
`archive-v<MAJOR>.json`. Keep the format of the file. `validate` checks every
file: a PR with a text needs `en`, every key is `*` or a target, every text is
one line without the PR number, the English one also follows the form above,
and every component of a highlight finds a text in every language. The next
bundle carries the change.

The directory has three kinds of translation files:

* `<PR>.json` holds the texts of one pull request that is not released yet.
* `v<MAJOR>.<MINOR>.<PATCH>.json`, for example `v0.10.0.json`, holds the texts
  of one release.
* `archive-v<MAJOR>.json`, for example `archive-v0.json`, holds the texts of all
  releases of one major version.

The release and archive files have the form `{"<PR>": {"de": {...}, "en": {...}}}`. A per-PR
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

### Tours for web UI highlights

Every highlight of the `dashboard` component brings a tour. After an update the
tour walks each user through the new feature in the web UI. It is the key `tour`
in the same `changelog/highlights/<PR>.json` file, next to the language keys and
`details`:

```json
{
  "de": {
    "*": "Geräte lassen sich als Favoriten markieren."
  },
  "tour": {
    "*": {
      "steps": [
        {
          "panel": "devices-panel",
          "target": "favorites",
          "device": "any",
          "need": "view_devices",
          "title": {"en": "Favourites", "de": "Favoriten"},
          "text": {"en": "Star a device to keep it at the top.", "de": "Markiere ein Gerät mit dem Stern, dann steht es oben."}
        }
      ]
    }
  }
}
```

The rules:

* `target` is required. It is the `data-tour` attribute of the element the step
  points at.
* `panel` and `page` name where the element lives, as in the address
  `?panel=…&page=…`. Both are optional.
* `device` is `any`, `phone` or `desktop`. Without it the step counts as `any`.
* `need` is the permission a user needs for the step. Users without it skip the
  step.
* `open` is optional. It is a list of `data-tour` ids that the tour clicks in
  order, and `@name` for a named action from `onboarding-actions.js`. The
  tour takes the clicks back when it moves on, unless the user clicked or typed
  something meanwhile. An element that is already open is not clicked.
* A target that exists on one element out of many may use
  `x-bind:data-tour="<condition> ? '<id>' : null"`. The id must stand in single
  quotes inside the expression.
* `title` and `text` need `en` and every language of
  `highlight_i18n.LANGUAGES`. No text may be empty. Unknown keys are an error.
* `*` and target keys work as for the details. A key with a target replaces the
  `*` object as a whole.

A highlight with nothing to show in the web UI, such as a backend fix, gets an
exemption with its reason instead of steps:

```json
"tour": {"*": {"exempt": "Only the MQTT reconnect changed, nothing to show."}}
```

An exemption needs the user's consent. The agent that writes the highlight asks
for it explicitly and adds `exempt` only after the user agreed. A tour has either
steps or an exemption, never both.

The tour can travel with the PR. `scripts/dev/create-pr.sh` reads the value of
`tour` from the optional file `PR_TOUR_FILE`, checks it and puts it into the "Tour"
section of the description. The Version bump workflow copies it into
`changelog/highlights/<PR>.json`, where a tour from the description replaces the
one in the file. A section with broken JSON or an invalid tour fails that workflow.

A tour may carry `replaced_by: {"pr": N, "step": S}` next to `steps`. When the
tour goes stale, the checklist starts step S of the tour of #N instead. The Go
test checks that the step exists.

`highlight_translations.py validate --repo .` checks the tours of every file as
well. A `dashboard` highlight with neither a tour nor an exemption does not fail
the build. The last job of CI, "CI ok", runs `scripts/build/tour_gate.py` after
all other jobs passed:

1. It writes a comment in the PR that names the highlights, and the commit status
   CI stays pending.
2. It waits up to 10 minutes for a 👍 on that comment. Only the reaction of the
   repository owner counts. It then counts as the exemption, and the commit status
   turns green. Later runs of the same PR find the reaction and do not ask again.
3. Without a reaction the job fails. To continue, react and re-run the failed job
   from the run linked in the comment. A reaction alone starts nothing.

The commit status CI is a required check of the `main` branch protection, also for
admins. A PR merges only after it turned green. That holds for every PR, because
CI always reports to this status. A PR from a fork gets no CI run and cannot merge.

Adding a tour to the description instead starts CI again and cancels the waiting
run. An agent never reacts on this comment, the reaction is the user's decision.
The exemption stays in the PR comment and does not reach the repository. That is
enough for the web UI, which shows no steps for an exemption and for a missing
tour alike.

`highlight_tours.py check --repo . --pr <N>` shows the same gaps locally. The check
starts at PR #186, the first PR of the onboarding. Older PR files and the archives
`v*.json` and `archive-v*.json` are not checked. A tour may also be written by hand
into the PR file once the PR has its number. The Version bump workflow keeps it
when it adds the translations. The bundle
carries the tour as `tour` on each PR entry of `changelog.json`, and
`/api/v1/changelog/highlights` passes it on.

Tours of highlights that are already released live in the archive files, next to
their translations: `archive-v<MAJOR>.json` for older releases and
`v<X.Y.Z>.json` for the latest one. `validate` checks them, `check` does not.
The Go test `TestHighlightToursPointAtKnownTargets` in
`dashboard/internal/webui` checks every tour in `changelog/highlights/` against
the targets, panels, pages and permissions of the web UI. "Explore what is new"
lists the last three minor releases. Older releases appear behind the second
toggle of the checklist.

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
