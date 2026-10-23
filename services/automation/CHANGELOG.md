# Changelog

## v0.6.0 (2026-10-23)

### Features

- **automation:** Automation rules can repeat their actions while their conditions hold. (#163) (1f30b74)

### Tests

- **automation:** pin stale-balance regulation and scenario removal (17a4855)

### Chores

- **automation:** scenario smoke fixture and 0.6.0 manifest (dcd062c)

## v0.5.3 (2026-10-21)

### Features

- **automation:** configurable publish minimum and regulator interval (df47655)
- **automation:** repeat a rule's actions while its conditions hold (4e3707d)
- **automation:** regulator step and shared value quantizing (af319df)
- **automation:** validate scenarios (de7556c)
- **automation:** scenario state machine (5692aa1)
- **automation:** feedback regulator for scenarios (97e8dd7)
- **automation:** publish scenario state, history and discovery (48c7dfa)

### Fixes

- **automation:** use plain types in the scenario schema (56e2c04)

### Refactors

- **common:** clean up the slave status and hooks (#155) (00ddbed)
- **automation:** share condition evaluation between engines (a96a4e4)
- **common:** drop unused poll fields and pending from the slave status (00528ff)
- **services:** name the slave poll rates after what they are (7cd303e)

### Tests

- **automation:** pin episode reset and repeat result (7f939e0)

## v0.5.2 (2026-10-18)

### Documentation

- move internal notes on automations and overview cards into the docs (#140) (4651b98)
- **automations:** explain rule evaluation and document guest roles (1c2c2ab)

## v0.5.1 (2026-10-12)

### Features

- **devicemap:** hang the automation device under the node and link its rules (#127) (c4c2401)
- **automation:** announce the service device via the energy node (b9f3207)

## v0.5.0 (2026-10-10)

### Features

- **dashboard:** data flow and balance layers on the device map (#109) (bbe604b)
- custom energy categories in automations and editor polish (#115) (38776fb)
- **automation:** accept custom energy categories as balance fields (e7952c5)
- **services:** declare data flow of topic fields with x-dataflow (dfa0420)

### Documentation

- redesign the documentation site with an Energy Node layout (#94) (5a7080f)
- point the repository at the new pages and add site search (72fa46e)

### Chores

- **release:** bump the dashboard to v0.9.0 (#116) (9a051aa)
- **release:** bump automation to v0.5.0 and the icon set to v0.2.0 (2a937dd)

## v0.4.4 (2026-09-28)

### Features

- **battery_soc:** DC-only systems in the MQTT service and conditional config forms (#58) (89bacae)
- **automation:** weekdays in the editor and a sun_window condition (#63) (b32936a)
- **dashboard:** localize the system configuration form (#72) (c716d05)
- **dashboard:** localize the device configuration forms (#73) (180110d)
- **dashboard:** write the central schema in English and move German to the catalog (bd26f60)
- **dashboard:** describe every field of the system configuration form (012ea1c)
- **services:** start with an invalid device file and report it as rejected (029c604)

### Fixes

- **services:** write every schema text in English without semicolons (e3cbcf6)

## v0.4.0 (2026-09-21)

### Features

- **services:** give every service its own version and changelog (#45) (5bc91b8)

## v0.3.2 (2026-09-15)

### Features

- **webui:** add the installer's layer-3 web UI, browser tests and CI (#28) (e819b5e)

## v0.3.1 (2026-09-12)

### Features

- **installer:** build the node-half bootstrap chain and signed bundle pipeline (#25) (f11e962)

## v0.3.0 (2026-09-10)

### Features

- **⚠ Breaking:** move node telemetry into the dashboard nodeagent (#19) (8049117)
- make device services self-describing with per-service manifests (#12) (2e8c911)
- **⚠ Breaking:** fold the config.json node block into dashboard.node_* (schema_version 2) (#14) (e7f4ba1)

### Refactors

- split src/ into services/ and libs/, rename service-level device_id to service_id (#10) (75abe73)

