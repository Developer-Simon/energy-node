# Changelog

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

