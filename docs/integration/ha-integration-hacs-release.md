---
title: "Home Assistant integration — HACS mirror repo & release runbook"
---

# Home Assistant integration — HACS mirror repo & release runbook

Home Assistant integrations in this monorepo (`battery_soc` and `energy_node_icons`) are
distributed to users through **separate, public GitHub repos** wired for HACS. Those mirror
repos are *derived artifacts*: they are assembled from this monorepo by
`scripts/publish_mirror.sh` and never edited by hand.

- **Source of truth:** this monorepo.
- **Mirror repos:** 
  - [`Developer-Simon/ha-battery-soc`](https://github.com/Developer-Simon/ha-battery-soc) — the public, HACS-facing repo for `battery_soc`.
  - [`Developer-Simon/ha-energy-node-icons`](https://github.com/Developer-Simon/ha-energy-node-icons) — the public, HACS-facing repo for `energy_node_icons`.
  
  Both are assembled from here by `scripts/publish_mirror.sh` (pass `--component battery_soc` or `--component energy_node_icons`).
- **Shared core:** `libs/battery_soc_core/` → vendored into the integration by
  `scripts/vendor_core.py` (drift-guarded by the CI job *Vendored artefacts in
  sync* and `integrations/homeassistant/tests/test_vendor_sync.py`).
- **Test venvs:** the plain suites run in `.venv`; the Home Assistant suite
  needs its own `.venv-ha` (HA's pytest plugins conflict with the plain
  suite). Build it once:
  `python3.14 -m venv .venv-ha && .venv-ha/bin/pip install -e ./libs/battery_soc_core -r integrations/homeassistant/requirements-test.txt`.
- **Public identifiers:** each component has its own `release.env` file:
  - `integrations/homeassistant/mirror/battery_soc/release.env` — `OWNER=Developer-Simon`, `REPO=ha-battery-soc`, `HA_MIN_VERSION`, `MIRROR_PATH=/home/simon/dev/ha-battery-soc` (absolute path where the mirror repo is checked out), `CHANGELOG_TARGET=ha-integration`, `CHANGELOG_PATH=integrations/homeassistant/CHANGELOG.md`.
  - `integrations/homeassistant/mirror/energy_node_icons/release.env` — `OWNER=Developer-Simon`, `REPO=ha-energy-node-icons`, `HA_MIN_VERSION`, `MIRROR_PATH=/home/simon/dev/ha-energy-node-icons` (absolute path where the mirror repo is checked out), `CHANGELOG_TARGET=ha-icons`, `CHANGELOG_PATH=integrations/homeassistant/custom_components/energy_node_icons/CHANGELOG.md` (monorepo-rooted path).
- **Real HACS/hassfest validation:** runs as GitHub Actions **in the mirror
  repo** (`.github/workflows/validate.yml`), not here.
  `scripts/check_mirror_manifest.py` is only a fast offline pre-check.

Local mirror checkout used below: `../ha-battery-soc` (a sibling of this
repository's working copy).

---

## One-time — create the public repos (fresh, no history)

Consistent with this project's release model (open-source as a fork without
history, no git remote in the monorepo), each mirror is a brand-new repo — no
history is imported from here. Repeat this once for each component.

### For `battery_soc`:

1. `mkdir -p ../ha-battery-soc && cd ../ha-battery-soc && git init && git branch -m main`
2. On GitHub: create an empty **public** repo `Developer-Simon/ha-battery-soc`
   (no README, no license, no .gitignore — `publish_mirror.sh` provides them).
3. `git -C ../ha-battery-soc remote add origin git@github.com:Developer-Simon/ha-battery-soc.git`
4. Confirm `integrations/homeassistant/mirror/battery_soc/release.env` has the real values.
5. In the **base** manifest
   `integrations/homeassistant/custom_components/battery_soc/manifest.json`,
   replace the `OWNER` / `@OWNER` placeholders in `documentation`,
   `issue_tracker` and `codeowners` with the real values, then commit in the
   monorepo. (`publish_mirror.sh` also rewrites these for the mirror, but the
   base file should not ship placeholders.)
6. First publish (assembles into the sibling `../ha-battery-soc` by default):
   - the very first time the branch does not exist on `origin` yet, so push it
     by hand: `scripts/publish_mirror.sh --component battery_soc --version 0.1.0` (a bare sync commit,
     no tag) then `git -C ../ha-battery-soc push -u origin main --follow-tags`
     and `gh release create v0.1.0 --repo Developer-Simon/ha-battery-soc --title v0.1.0 --notes-file <(...)`.
   - every later release: `scripts/publish_mirror.sh --component battery_soc --release` (version taken
     from `manifest.json`) regenerates the changelog, commits + tags the
     mirror, pushes branch + tag **and** starts the mirror's release
     workflow, which creates the GitHub Release, in one step. A plain `scripts/publish_mirror.sh --component battery_soc --push` (no `--release`) only
     syncs the tree and pushes the branch — no tag, no Release.
7. Set GitHub **repo topics** on the mirror (HACS rejects a repo with none):
   `gh repo edit Developer-Simon/ha-battery-soc --add-topic home-assistant --add-topic hacs --add-topic home-assistant-integration --add-topic lifepo4 --add-topic battery`
8. Watch the **Validate** workflow (hassfest + HACS) in the mirror repo. Fix
   anything it flags **in the monorepo**, then re-publish (bump the version).

### For `energy_node_icons`:

Repeat steps 1–8 above with:
- Step 1: `mkdir -p ../ha-energy-node-icons && cd ../ha-energy-node-icons && git init && git branch -m main`
- Step 2: repo name `Developer-Simon/ha-energy-node-icons`
- Step 3: `git remote add origin git@github.com:Developer-Simon/ha-energy-node-icons.git`
- Step 4: `integrations/homeassistant/mirror/energy_node_icons/release.env`
- Step 5: `integrations/homeassistant/custom_components/energy_node_icons/manifest.json`
- Step 6 & 7: replace `battery_soc` with `energy_node_icons` in the commands.
- Step 7 topics: `--add-topic home-assistant --add-topic hacs --add-topic icons`
- Mirror path: `../ha-energy-node-icons` (inferred from `release.env`).

A GitHub **Release** (not just a tag) is mandatory, not optional polish: with
zero Releases HACS runs the repo in *commit mode* — the update entity shows a
bare commit SHA instead of the version and the "Read release announcement" link
just opens the repo homepage. `--release` creates the Release so this cannot be
forgotten.

The Release is created by the mirror's own **Release** workflow
(`mirror/<component>/.github/workflows/release.yml`), which `--release` starts
with `gh workflow run` and then follows with `gh run watch`. Before it creates
the Release, the workflow checks the tagged tree for `[%schema:...%]`
placeholders. In the monorepo, the English strings of `battery_soc` keep the
field descriptions it shares with the MQTT service as such placeholders
(source: `services/battery_soc/battery_soc_devices.schema.json`, see
`scripts/render_ha_descriptions.py`). `publish_mirror.sh` renders them into the
mirror tree when `release.env` sets `SCHEMA_DESCRIPTIONS`. A tree published
some other way, with placeholders left, gets no Release.

Brand assets ship in-tree at `custom_components/battery_soc/brand/icon.png`
(+ `icon@2x.png`; `icon.svg` is the editable source). That local `brand/` folder
covers **the native Home Assistant UI** (config-flow card, device page — HA
2025.x+ serves it via `/api/brands/integration/...`) **and the hassfest / HACS
`brands` validation check**. It does **not** cover the **HACS store list** or the
**HACS update-entity dialog** — both fetch `https://brands.home-assistant.io/`
by domain from the browser, so the icon is blank there until `battery_soc` is in
`home-assistant/brands` (see the last section). `mirror/AI-DISCLAIMER.md` and
`mirror/.github/pull_request_template.md` are assembled into the mirror too.

## One-time — make it installable in Home Assistant

10. HACS → ⋮ → **Custom repositories** → add
    `https://github.com/Developer-Simon/ha-battery-soc`, category
    **Integration**.
11. HACS → search **Battery SoC** → **Download** → restart Home Assistant.
12. **Settings → Devices & Services → Add Integration → “Battery SoC (LiFePO4
    coulomb-counting)”.**
13. Smoke test: add one battery pointing at real power/voltage sensors; confirm
    the `SoC` sensor and the diagnostic entities appear; call
    `battery_soc.set_state_of_charge` with `state_of_charge: 60` and confirm the
    SoC sensor jumps to 60.

---

## Recurring — cut a release

The two components (`battery_soc` and `energy_node_icons`) have independent version
streams. Release each component **only when it has changed** — do not release both
together if only one changed. The changelog target and path for each are read
from the `release.env` file of the chosen component (use `--component` to select it).

### From GitHub Actions (preferred)

The **HA Mirror Release** workflow (`.github/workflows/ha-mirror-release.yml`)
runs the same `scripts/publish_mirror.sh` as the local steps below, so a
release looks the same whichever way you cut it.

One-time setup: create a fine-grained personal access token with
**Contents**, **Workflows** and **Actions** set to read and write on both
mirror repos (`ha-battery-soc`, `ha-energy-node-icons`) and store it as the
repository secret `HA_MIRROR_TOKEN` in this monorepo. Contents pushes the
tree, Workflows lets it update the mirror's `.github/workflows`, and Actions
starts the mirror's release workflow. The default `GITHUB_TOKEN` can do none
of this in another repository.

Per release:

1. Make sure the change is merged to `main` and the component's
   `manifest.json` carries the version you want to publish (CI patch-bumps it
   on the PR branch; bump minor/major by hand there).
2. **Actions → HA Mirror Release → Run workflow** on `main`. Pick the
   component and leave **Dry run** on. The log shows the assembled mirror
   tree and manifest.
3. Run it again with **Dry run** off. The workflow commits the mirror, tags
   `vX.Y.Z`, pushes, and starts the mirror's release workflow, which creates
   the GitHub Release. The release notes come from the component's changelog.
   If the changelog has no section for that version, the run stops before
   anything is tagged or pushed.
4. The run regenerates that changelog in the monorepo too. If **Version**
   differs from the manifest, it also writes that version into the manifest.
   The summary shows the diff, and the **release-files** artifact holds both
   files. Commit them through a normal PR, because `main` is protected.

A real release (dry run off) refuses to run on any branch other than `main`.

### Pre-release (beta)

A beta lets you test a change through HACS (**Show beta versions**) before
the real release. Run **HA Mirror Release** on the monorepo branch that holds
the change and set **Pre-release** to a mirror branch name, for example
`prerelease/dc-systems`. The workflow then:

- switches the mirror to that branch (continues it if it exists, else starts
  it from the mirror's `main`),
- sets the manifest version to `vX.Y.Z-bN`, where `X.Y.Z` comes from the
  manifest (or **Version**) and `N` is the next free beta number,
- commits, tags and pushes branch and tag, and creates a GitHub pre-release.

The mirror's `main` and the component's changelog stay untouched. `X.Y.Z` must
not be released yet. Pre-releases may run from any monorepo branch, and dry
run shows the beta version it would publish. Locally the same is
`scripts/publish_mirror.sh --component battery_soc --prerelease prerelease/dc-systems`.
The mirror's release workflow must already be on the mirror's `main`, since
GitHub only starts a workflow that the default branch knows.

### For `battery_soc`:

1. Land the change in `libs/battery_soc_core/` (or directly in the integration),
   then `.venv/bin/python scripts/vendor_core.py` and stage the result.
2. `cd integrations/homeassistant && ../../.venv-ha/bin/pytest -q`
   (whole HA suite, including the drift and mirror-template checks).
3. `.venv/bin/python scripts/check_mirror_manifest.py --component battery_soc`
4. `version` in
   `integrations/homeassistant/custom_components/battery_soc/manifest.json`
   (**bare** semver, no leading `v`) has its patch bumped on the PR branch by
   the `Version bump` workflow when the PR touches `integrations/homeassistant/`
   (`scripts/version/bump-patch.sh`, same `COMPONENTS` mechanism as
   `dashboard/VERSION`/`services/VERSION` — see `scripts/version/components.sh`); major/minor stay
   hand-edited. `CHANGELOG.md` (in `integrations/homeassistant/CHANGELOG.md`)
   is generated the same way as the other components, grouped by major.minor into
   `## vX.Y.Z (date)` sections. **`publish_mirror.sh --component battery_soc --release` runs
   `scripts/generate_changelog.sh ha-integration` for you** and stages the
   result in the monorepo (you commit it with the version bump), then slices
   the Release notes out of the matching section — no need to run it by hand
   first. `v0.1.0`-`v0.1.4` predate this automation and were written by hand as
   5 separate sections sharing minor `0.1`; a permanent floor baked into
   `generate_changelog.sh` (`min_freeze="v0.2"` for `ha-integration`) keeps
   that history frozen exactly as written on every regeneration, even
   without passing `--freeze-before` — do not lower it.
5. `scripts/publish_mirror.sh --component battery_soc --release`
   (regenerates + stages the changelog, commits + tags the mirror, pushes
   `main` + the tag, then starts the mirror's release workflow, which creates
   `vX.Y.Z` with the changelog section as the body). The version defaults to the `manifest.json` `version`
   field from step 4, so no `--version` is needed once the bump has been
   committed; pass `--version X.Y.Z` only to override (bootstrap, or a manual
   major/minor jump before the commit lands). Then commit the staged
   `integrations/homeassistant/CHANGELOG.md` in the monorepo.

### For `energy_node_icons`:

The process is similar, but includes icon regeneration:

1. Edit the icon drawings in `dashboard/internal/webui/deviceicons.go`.
2. Regenerate the icon source and module:
   - Build the dashboard source: `cd dashboard && go run ./cmd/deviceicons` (writes `integrations/homeassistant/icons.source.json`).
   - Convert to Home Assistant format: `.venv/bin/python scripts/icons/flatten_icons.py` (writes `integrations/homeassistant/custom_components/energy_node_icons/www/energy-node-icons.js`).
   - Verify no drift: `python3 scripts/icons/flatten_icons.py --check` (exit 0 = OK).
   - Commit the updated source and JS module.
3. Run the integration tests: `cd integrations/homeassistant && ../../.venv-ha/bin/pytest -q`.
4. `.venv/bin/python scripts/check_mirror_manifest.py --component energy_node_icons`
5. Version in `integrations/homeassistant/custom_components/energy_node_icons/manifest.json` follows the same auto-bump scheme as `battery_soc`.
6. `scripts/publish_mirror.sh --component energy_node_icons --release` (regenerates the changelog, commits + tags the mirror, pushes branch + tag and starts the mirror's release workflow). The version defaults to the `manifest.json` `version` field; pass `--version X.Y.Z` only to override.
7. Then commit the staged `integrations/homeassistant/custom_components/energy_node_icons/CHANGELOG.md` in the monorepo.

The `CHANGELOG.md` is generated into `custom_components/energy_node_icons/CHANGELOG.md` (see the `release.env` `CHANGELOG_TARGET` and `CHANGELOG_PATH`).

### General notes:

6. HACS shows the update to users within a few hours (or immediately on a
   manual "Redownload"). An install that was made **before** the first Release
   existed is pinned to commit mode until its next **Redownload** in HACS —
   that one manual redownload flips it to version tracking.

`--dry-run` on `publish_mirror.sh` stops after assembling the tree and prints
`git status --porcelain` plus the rewritten `manifest.json` — use it to review
before the real run; it cannot be combined with `--push` or `--release`.
Without `--release` the script only syncs the tree into the mirror as a plain
commit (no tag, no changelog, no Release); add `--push` to also push the
branch. `--release` implies publishing, so `--push` is redundant with it.
The `--component` flag defaults to `battery_soc` if not provided.
