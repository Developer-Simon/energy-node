# Changelog

## v0.3.15 (2026-09-09)

### Features

- make device services self-describing with per-service manifests (#12) (2e8c911)
- **appconfig:** add dashboard.node_* alongside the node block (e1c73d1)

### Refactors

- split src/ into services/ and libs/, rename service-level device_id to service_id (#10) (75abe73)

### CI

- **version-bump:** refresh component changelogs on the PR branch (#11) (6326afb)

### Other

- feat!: drop the node block, bump schema_version to 2 (7990f5c)

## v0.3.14 (2026-09-08)

### Features

- add a manifest and schema fragment per device service (7124a64)

### Refactors

- split src/ into services/ and libs/, rename service-level device_id to service_id (#10) (75abe73)
- **appconfig:** derive the service set from manifests/, not a table (652345c)

### CI

- **version-bump:** refresh component changelogs on the PR branch (#11) (6326afb)

## v0.3.13 (2026-09-05)

### Chores

- initial public release of Energy Node (e9c9417)

## v0.3.12 (2026-09-05)

### Refactors

- rename product Werkstatt-IoT to Energy Node repo-wide (3978730)
- rename Werkstatt-IoT to Energy Node, externalize deploy config, and document pi migration (daf06da)

### Documentation

- refresh changelogs before public fork (220794e)

### Dev

- added version file to common package and adjusted versioning (3e10297)
- **changelog:** add changelog for dashboard, src, and energy_node_common (768b498)
- **scripts:** auto-generate battery_soc_core + HA integration changelogs, move deploy scripts under scripts/ (1f98ab1)

## Unversioniert (bis 2026-08-24)

### Features

- created module for central refresh rate configuration (8f8382f)
- added central simulation mode (ad8311a)
- added availability topic for all devices (7704fc0)
- added reload for configurable services (e7af942)
- replace per-service *.env files with a central config.json (d060b5a)
- **automation:** add per-rule trigger history with dashboard view (7eb80b6)

### Fixes

- non-working install script (42dd7e8)
- slave.py doesn't update last_update (8bf2de3)

### Refactors

- no device ha discovery for services, only outstation topics (3a4e503)

### Other

- reduced diagnostics multiplier to a single setter in the node (c7180e8)

