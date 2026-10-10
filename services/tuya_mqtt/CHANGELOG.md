# Changelog

## v0.4.4 (2026-10-06)

### Features

- **services:** give every service its own version and changelog (#45) (5bc91b8)
- **battery_soc:** DC-only systems in the MQTT service and conditional config forms (#58) (89bacae)
- **dashboard:** localize the system configuration form (#72) (c716d05)

### Fixes

- **tuya:** retry once on a stale persistent socket before reporting offline (#47) (1dfc8bd)

### Documentation

- redesign the documentation site with an Energy Node layout (#94) (5a7080f)
- point the repository at the new pages and add site search (72fa46e)

## v0.4.3 (2026-09-28)

### Features

- **services:** give every service its own version and changelog (#45) (5bc91b8)
- **battery_soc:** DC-only systems in the MQTT service and conditional config forms (#58) (89bacae)
- **dashboard:** localize the system configuration form (#72) (c716d05)
- **dashboard:** write the central schema in English and move German to the catalog (bd26f60)
- **dashboard:** describe every field of the system configuration form (012ea1c)
- **services:** start with an invalid device file and report it as rejected (029c604)

### Fixes

- **tuya:** retry once on a stale persistent socket before reporting offline (#47) (1dfc8bd)

## v0.3.2 (2026-09-15)

### Features

- **installer:** build the node-half bootstrap chain and signed bundle pipeline (#25) (f11e962)
- **webui:** add the installer's layer-3 web UI, browser tests and CI (#28) (e819b5e)

## v0.3.0 (2026-09-10)

### Features

- **⚠ Breaking:** move node telemetry into the dashboard nodeagent (#19) (8049117)
- make device services self-describing with per-service manifests (#12) (2e8c911)
- **⚠ Breaking:** fold the config.json node block into dashboard.node_* (schema_version 2) (#14) (e7f4ba1)

### Refactors

- split src/ into services/ and libs/, rename service-level device_id to service_id (#10) (75abe73)

