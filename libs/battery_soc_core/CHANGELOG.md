# Changelog

## v0.1.8 (2026-09-08)

### Features

- make device services self-describing with per-service manifests (#12) (2e8c911)

### Refactors

- split src/ into services/ and libs/, rename service-level device_id to service_id (#10) (75abe73)

### CI

- **version-bump:** refresh component changelogs on the PR branch (#11) (6326afb)

## v0.1.7 (2026-09-05)

### Chores

- initial public release of Energy Node (e9c9417)

## v0.1.6 (2026-09-05)

### Features

- **battery_soc:** extract core logic and adapter modules (4a31188)
- **battery_soc:** show net battery power as a measurement, not diagnostic (b3878e8)
- **ha:** native Home Assistant integration for battery SoC (99ef3cf)

### Refactors

- rename product Werkstatt-IoT to Energy Node repo-wide (3978730)

### Documentation

- refresh changelogs before public fork (220794e)

### Dev

- **scripts:** auto-generate battery_soc_core + HA integration changelogs, move deploy scripts under scripts/ (1f98ab1)

