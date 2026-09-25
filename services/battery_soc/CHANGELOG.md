# Changelog

## v0.4.2 (2026-09-25)

### Features

- **services:** give every service its own version and changelog (#45) (5bc91b8)
- **battery_soc:** support DC-only systems and current sensors in HA (#56) (776bfdc)
- **services:** start with an invalid device file and report it as rejected (029c604)
- **⚠ Breaking — battery_soc:** validate the MQTT service's sources with coded errors (e6e741e)
- **battery_soc:** feed one MQTT topic into several slots with invert and unit (2735b3d)
- **battery_soc:** conditional battery schema for system type and bank layout (53943e9)
- **battery_soc:** take shared HA field descriptions from the service schema (c6a4a29)
- **battery_soc:** render shared HA descriptions at mirror time and release via the mirror's workflow (65b4a20)

### Fixes

- **battery_soc:** use the shared invert description for the inverter DC input (55d7040)

### Documentation

- **battery_soc:** document DC-only systems, signed inputs and current units (6c2df1f)

## v0.3.2 (2026-09-15)

### Features

- **installer:** build the node-half bootstrap chain and signed bundle pipeline (#25) (f11e962)
- **webui:** add the installer's layer-3 web UI, browser tests and CI (#28) (e819b5e)

## v0.3.0 (2026-09-10)

### Features

- **⚠ Breaking:** move node telemetry into the dashboard nodeagent (#19) (8049117)

## v0.2.10 (2026-09-09)

### Features

- make device services self-describing with per-service manifests (#12) (2e8c911)
- **⚠ Breaking:** fold the config.json node block into dashboard.node_* (schema_version 2) (#14) (e7f4ba1)

### Refactors

- split src/ into services/ and libs/, rename service-level device_id to service_id (#10) (75abe73)

