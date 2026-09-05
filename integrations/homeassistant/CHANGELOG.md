# Changelog

## v0.2.7 (2026-09-05)

### Features

- battery status card with column and trajectory displays (7bb501c)
- **battery-card:** responsive trajectory sizing and configurable windows (4e844c9)

### Documentation

- update README and add integration documentation for Battery SoC (72e1f48)
- **ha:** document card setup prerequisite and add card screenshots (d9a875e)
- refresh changelogs before public fork (220794e)

### Dev

- **scripts:** auto-generate battery_soc_core + HA integration changelogs, move deploy scripts under scripts/ (1f98ab1)
- enhance publish_mirror.sh with release functionality and update changelog generation (eb62959)

## v0.1.4 (2026-08-31)

### Build

- **ha:** `scripts/publish_mirror.sh --push` now pushes `main` + the new tag to
  the mirror remote and creates the GitHub Release (notes sliced from this
  changelog). Without a Release, HACS ran the repo in commit mode and showed
  bare commit SHAs instead of a version, with a dead "release announcement"
  link.

### Documentation

- **ha:** the HACS release runbook and `integrations/homeassistant/README.md`
  now state that the in-tree `custom_components/battery_soc/brand/` icons cover
  the native Home Assistant UI and the hassfest/HACS `brands` validation, but
  **not** the HACS store list or the HACS update-entity dialog — those read
  `brands.home-assistant.io` by domain and need a `home-assistant/brands` PR.
  Added a concrete checklist for that PR.

## v0.1.3 (2026-08-30)

### Documentation

- **ha:** public README/`info.md` reference brand and screenshot images by
  absolute `raw.githubusercontent.com` URL so they render on HACS; added the
  "Open in HACS" redirect button and a device-page screenshot.

## v0.1.2 (2026-08-30)

### Build

- **ha:** packaging-only re-cut of the mirror tree.

## v0.1.1 (2026-08-30)

### Build

- **ha:** ship `custom_components/battery_soc/brand/` icons, the mirror
  `AI-DISCLAIMER.md` and the PR template; manifest field order / `services.yaml`
  polish for hassfest.

## v0.1.0 (2026-08-30)

### Features

- native Home Assistant custom integration for battery SoC — config flow,
  DataUpdateCoordinator, sensor/binary_sensor/number platforms, the
  `battery_soc.set_state_of_charge` action; reuses the transport-agnostic
  `battery_soc_core` engine (99ef3cf)

### Build

- **ha:** `scripts/vendor_core.py` syncs `src/battery_soc_core/` into the
  integration's vendored copy; `--check` reports drift (695a068)
- reject commits (all branches) whose staged files leave the vendored core out
  of sync with `src/battery_soc_core/` (6cde85d)
- **ha:** mirror-repo template for HACS distribution — `hacs.json`, public
  `README`/`info.md`, `manifest.overrides.json`, `release.env`, the
  hassfest + `hacs/action` validation workflow, a bug-report form (4006a0a)
- **ha:** `scripts/publish_mirror.sh` assembles the public HACS repo tree from
  this monorepo and tags a release; `--dry-run` prints the plan (2075436)
- **ha:** `scripts/check_mirror_manifest.py` — offline manifest / vendored-core
  sanity check to run before a release (1790786)

### Tests

- **ha:** fail the suite if the vendored core drifts from
  `src/battery_soc_core/` (74df984)

### Documentation

- **ha:** HACS mirror-repo one-time setup and recurring release runbook at
  `docs/knowledge/ha-integration-hacs-release.md` (44ebe0a)
