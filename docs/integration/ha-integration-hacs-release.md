---
title: "Home Assistant integration — HACS mirror repo & release runbook"
---

# Home Assistant integration — HACS mirror repo & release runbook

The `battery_soc` Home Assistant integration lives in this monorepo under
`integrations/homeassistant/custom_components/battery_soc/`. It is distributed
to users through a **separate, public GitHub repo** wired for HACS. That mirror
repo is a *derived artifact*: it is assembled from this monorepo by
`scripts/publish_mirror.sh` and never edited by hand.

- **Source of truth:** this monorepo.
- **Shared core:** `src/battery_soc_core/` → vendored into the integration by
  `scripts/vendor_core.py` (drift-guarded by `git-hooks/pre-commit` and
  `integrations/homeassistant/tests/test_vendor_sync.py`).
- **Test venvs:** the plain suites run in `.venv`; the Home Assistant suite
  needs its own `.venv-ha` (HA's pytest plugins conflict with the plain
  suite). Build it once:
  `python3.14 -m venv .venv-ha && .venv-ha/bin/pip install -e ./src/battery_soc_core -r integrations/homeassistant/requirements-test.txt`.
- **Public identifiers:** `integrations/homeassistant/mirror/release.env` —
  `OWNER=Developer-Simon`, `REPO=ha-battery-soc`, `HA_MIN_VERSION`.
- **Real HACS/hassfest validation:** runs as GitHub Actions **in the mirror
  repo** (`.github/workflows/validate.yml`), not here.
  `scripts/check_mirror_manifest.py` is only a fast offline pre-check.

Local mirror checkout used below: `../ha-battery-soc` (a sibling of this
repository's working copy).

---

## One-time — create the public repo (fresh, no history)

Consistent with this project's release model (open-source as a fork without
history, no git remote in the monorepo), the mirror is a brand-new repo — no
history is imported from here.

1. `mkdir -p ../ha-battery-soc && cd ../ha-battery-soc && git init && git branch -m main`
2. On GitHub: create an empty **public** repo `Developer-Simon/ha-battery-soc`
   (no README, no license, no .gitignore — `publish_mirror.sh` provides them).
3. `git -C ../ha-battery-soc remote add origin git@github.com:Developer-Simon/ha-battery-soc.git`
4. Confirm `integrations/homeassistant/mirror/release.env` has the real
   `OWNER` (`Developer-Simon`). This is the only place it lives.
5. In the **base** manifest
   `integrations/homeassistant/custom_components/battery_soc/manifest.json`,
   replace the `OWNER` / `@OWNER` placeholders in `documentation`,
   `issue_tracker` and `codeowners` with the real values, then commit in the
   monorepo. (`publish_mirror.sh` also rewrites these for the mirror, but the
   base file should not ship placeholders.)
6. First publish (assembles into the sibling `../ha-battery-soc` by default;
   override with `--mirror-path`):
   - the very first time the branch does not exist on `origin` yet, so push it
     by hand: `scripts/publish_mirror.sh --version 0.1.0` (a bare sync commit,
     no tag) then `git -C ../ha-battery-soc push -u origin main --follow-tags`
     and `gh release create v0.1.0 --repo Developer-Simon/ha-battery-soc --title v0.1.0 --notes-file <(...)`.
   - every later release: `scripts/publish_mirror.sh --release` (version taken
     from `manifest.json`) regenerates the changelog, commits + tags the
     mirror, pushes branch + tag **and** creates the GitHub Release in one
     step. A plain `scripts/publish_mirror.sh --push` (no `--release`) only
     syncs the tree and pushes the branch — no tag, no Release.
7. Set GitHub **repo topics** on the mirror (HACS rejects a repo with none):
   `gh repo edit Developer-Simon/ha-battery-soc --add-topic home-assistant --add-topic hacs --add-topic home-assistant-integration --add-topic lifepo4 --add-topic battery`
8. Watch the **Validate** workflow (hassfest + HACS) in the mirror repo. Fix
   anything it flags **in the monorepo**, then re-publish (bump the version).

A GitHub **Release** (not just a tag) is mandatory, not optional polish: with
zero Releases HACS runs the repo in *commit mode* — the update entity shows a
bare commit SHA instead of the version and the "Read release announcement" link
just opens the repo homepage. `--release` creates the Release so this cannot be
forgotten.

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

1. Land the change in `src/battery_soc_core/` (or directly in the integration),
   then `.venv/bin/python scripts/vendor_core.py` and stage the result.
2. `cd integrations/homeassistant && ../../.venv-ha/bin/pytest -q`
   (whole HA suite, including the drift and mirror-template checks).
3. `.venv/bin/python scripts/check_mirror_manifest.py`
4. `version` in
   `integrations/homeassistant/custom_components/battery_soc/manifest.json`
   (**bare** semver, no leading `v`) has its patch bumped on the PR branch by
   the `Version bump` workflow when the PR touches `integrations/homeassistant/`
   (`scripts/version/bump-patch.sh`, same `COMPONENTS` mechanism as
   `dashboard/VERSION`/`src/VERSION` — see `git-hooks/lib.sh`); major/minor stay
   hand-edited. `CHANGELOG.md` is
   generated the same way as the other components, grouped by major.minor into
   `## vX.Y.Z (date)` sections. **`publish_mirror.sh --release` runs
   `scripts/generate_changelog.sh ha-integration` for you** and stages the
   result in the monorepo (you commit it with the version bump), then slices
   the Release notes out of the matching section — no need to run it by hand
   first. `v0.1.0`-`v0.1.4` predate this automation and were written by hand as
   5 separate sections sharing minor `0.1`; a permanent floor baked into
   `generate_changelog.sh` (`min_freeze="v0.2"` for `ha-integration`) keeps
   that history frozen exactly as written on every regeneration, even
   without passing `--freeze-before` — do not lower it.
5. `scripts/publish_mirror.sh --release`
   (regenerates + stages the changelog, commits + tags the mirror, pushes
   `main` + the tag, then `gh release create`s `vX.Y.Z` with the changelog
   section as the body). The version defaults to the `manifest.json` `version`
   field from step 4, so no `--version` is needed once the bump has been
   committed; pass `--version X.Y.Z` only to override (bootstrap, or a manual
   major/minor jump before the commit lands). Then commit the staged
   `integrations/homeassistant/CHANGELOG.md` in the monorepo.
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
