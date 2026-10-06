# Changelog

## v0.4.6 (2026-10-06)

### Features

- **services:** give every service its own version and changelog (#45) (5bc91b8)
- **battery_soc:** support DC-only systems and current sensors in HA (#56) (776bfdc)
- **battery_soc:** DC-only systems in the MQTT service and conditional config forms (#58) (89bacae)
- **dashboard:** localize the system configuration form (#72) (c716d05)
- **dashboard:** localize the device configuration forms (#73) (180110d)
- **battery_soc:** save the state on an interval and recover after a crash (#78) (dc62037)

### Documentation

- point the repository at the new pages and add site search (72fa46e)
- redesign the documentation site with an Energy Node layout (#94)

## v0.4.5 (2026-09-30)

### Features

- **services:** give every service its own version and changelog (#45) (5bc91b8)
- **battery_soc:** support DC-only systems and current sensors in HA (#56) (776bfdc)
- **battery_soc:** DC-only systems in the MQTT service and conditional config forms (#58) (89bacae)
- **dashboard:** localize the system configuration form (#72) (c716d05)
- **dashboard:** localize the device configuration forms (#73) (180110d)
- **battery_soc:** save the state on an interval and recover after a crash (#78) (dc62037)
- **battery_soc:** add the save interval setting to the service schema (df73211)
- **battery_soc:** write the state file atomically with save metadata (646ac60)
- **battery_soc:** recover the counter from the retained state or by extrapolation (01db8e3)
- **battery_soc:** save the state on an interval and flush it on shutdown (63c771e)

### Tests

- **battery_soc:** clarify why the recovery key is left out of the golden (a5ae656)
- **battery_soc:** patch only the service clock so logging keeps the real time (862d802)

## v0.4.4 (2026-09-28)

### Features

- **services:** give every service its own version and changelog (#45) (5bc91b8)
- **battery_soc:** support DC-only systems and current sensors in HA (#56) (776bfdc)
- **battery_soc:** DC-only systems in the MQTT service and conditional config forms (#58) (89bacae)
- **dashboard:** localize the system configuration form (#72) (c716d05)
- **dashboard:** localize the device configuration forms (#73) (180110d)
- **dashboard:** write the central schema in English and move German to the catalog (bd26f60)
- **dashboard:** describe every field of the system configuration form (012ea1c)
- **services:** start with an invalid device file and report it as rejected (029c604)
- **⚠ Breaking — battery_soc:** validate the MQTT service's sources with coded errors (e6e741e)
- **battery_soc:** feed one MQTT topic into several slots with invert and unit (2735b3d)
- **battery_soc:** conditional battery schema for system type and bank layout (53943e9)
- **battery_soc:** require the bank A voltage topic in the dashboard form (5573a33)
- **battery_soc:** take shared HA field descriptions from the service schema (c6a4a29)
- **battery_soc:** render shared HA descriptions at mirror time and release via the mirror's workflow (65b4a20)

### Fixes

- **services:** write every schema text in English without semicolons (e3cbcf6)
- **battery_soc:** use the shared invert description for the inverter DC input (55d7040)
- **battery_soc:** show the imbalance threshold only for two banks in series (f402372)

### Documentation

- **battery_soc:** document DC-only systems, signed inputs and current units (6c2df1f)

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

