# Changelog

## v0.1.10 (2026-09-24)

### Features

- **battery_soc_core:** validate source combinations and generate ac_fallback only where a fallback exists (8cdd7d1)
- **battery_soc_core:** accept current (A) on DC slots, converted with the pack voltage (e2fd892)
- **battery_soc_core:** treat a DC-only side as the sole source, not an override (55f1c62)
- **battery_soc:** define series banks as A+ to B- and accept a stack sensor (e3fd939)

### Refactors

- split src/ into services/ and libs/, rename service-level device_id to service_id (#10) (75abe73)
- remove git hooks, and stop the per-PR CHANGELOG stacking (#13) (c643260)

## v0.1.9 (2026-09-09)

### Features

- **battery_soc:** extract core logic and adapter modules (4a31188)
- **battery_soc:** show net battery power as a measurement, not diagnostic (b3878e8)
- **ha:** native Home Assistant integration for battery SoC (99ef3cf)

### Refactors

- split src/ into services/ and libs/, rename service-level device_id to service_id (#10) (75abe73)
- remove git hooks, and stop the per-PR CHANGELOG stacking (#13) (c643260)
- rename product Werkstatt-IoT to Energy Node repo-wide (3978730)

### Documentation

- drop remaining git-hooks references (7786f6d)
- refresh changelogs before public fork (220794e)

### Chores

- initial public release of Energy Node (e9c9417)

### Dev

- **scripts:** auto-generate battery_soc_core + HA integration changelogs, move deploy scripts under scripts/ (1f98ab1)

