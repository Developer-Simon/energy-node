# Changelog

## v0.3.5 (2026-11-01)

### Features

- **auth:** sign in home assistant users through the companion (#174) (7b1ba8c)
- **battery-soc:** render tuning findings from a shared message catalog (#196) (2332ed5)

## v0.3.4 (2026-10-21)

### Features

- **homeassistant:** ship the dashboard's device icons as an HA icon set (#54) (c4c196b)
- **battery_soc:** support DC-only systems and current sensors in HA (#56) (776bfdc)
- hourly energy statistics from Home Assistant via the companion (#130) (752f59a)
- **discovery:** The dashboard announces wallbox, heat pump and custom category sensors in Home Assistant once something is assigned to them, and each can be deselected in the MQTT tab. The companion supplies these roles for history and energy statistics. (#142) (66cd698)
- **energy:** The automatic load mode now always calculates the house consumption from the balance, and a house meter serves as a cross-check. A status line on the energy page shows the source, the fallback and the meter deviation, and Home Assistant gets measured and calculated house load as separate sensors. (#143) (59dfeb3)
- **homeassistant:** add history, sidebar name and icon options to the companion (#96) (60ca521)
- **homeassistant:** add Energy Node Companion as a history exchange peer (#85) (5bd7f48)
- **homeassistant:** show the dashboard in the Home Assistant sidebar (#88) (c15a920)
- **battery_soc:** DC-only systems in the MQTT service and conditional config forms (#58) (89bacae)
- **dashboard:** number format setting and locale-aware dates (localization A2) (#60) (6d8ba8b)
- **dashboard:** localize energy and settings (#64) (5445f5b)
- **dashboard:** localize the device configuration forms (#73) (180110d)

### Fixes

- **homeassistant:** full-height sidebar panel and working language switch (#89) (42baa01)
- **homeassistant:** polish Energy Node Companion for its first releases (#86) (7df380c)
- **ha-mirror:** take release notes from the changelog when --version overrides the manifest (#57) (bdd6b09)

### Refactors

- **webui:** remove Gridstack (#156) (d89aab3)

### Documentation

- redesign the documentation site with an Energy Node layout (#94) (5a7080f)
- **homeassistant:** point the integration manifests at the documentation site (#97) (b5e506d)
- **homeassistant:** move integration docs to Pages and turn the READMEs into feature overviews (#104) (ce60ec6)

## v0.2.10 (2026-09-08)

### Refactors

- split src/ into services/ and libs/, rename service-level device_id to service_id (#10) (75abe73)

## v0.2.9 (2026-09-06)

### Features

- **battery_soc:** calibration hardening and guided tuning suggestions (#2) (b18974c)

## v0.2.8 (2026-09-06)

### Features

- **docs:** update favicon and improve documentation (a7f25f4)

### Chores

- initial public release of Energy Node (e9c9417)

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
