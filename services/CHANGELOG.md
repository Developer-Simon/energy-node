# Changelog

## v0.2.10 (2026-09-09)

### Features

- make device services self-describing with per-service manifests (#12) (2e8c911)
- **⚠ Breaking:** fold the config.json node block into dashboard.node_* (schema_version 2) (#14) (e7f4ba1)
- **appconfig:** add dashboard.node_* alongside the node block (e1c73d1)
- add a manifest and schema fragment per device service (7124a64)
- **docs:** Add comprehensive documentation on dashboard and services (35cce56)

### Refactors

- split src/ into services/ and libs/, rename service-level device_id to service_id (#10) (75abe73)
- remove git hooks, and stop the per-PR CHANGELOG stacking (#13) (c643260)
- read the node identity from dashboard.node_* (5e1e672)
- **appconfig:** derive the service set from manifests/, not a table (652345c)
- rename product Werkstatt-IoT to Energy Node repo-wide (3978730)
- rename Werkstatt-IoT to Energy Node, externalize deploy config, and document pi migration (daf06da)

### Documentation

- fold the node block into dashboard.node_* in the docs (297586f)
- refresh changelogs before public fork (220794e)

### Tests

- make the full src/ suite collectible and skip local-only fixtures (cb6abfe)

### Chores

- initial public release of Energy Node (e9c9417)

### Dev

- **changelog:** add changelog for dashboard, src, and energy_node_common (768b498)
- **scripts:** auto-generate battery_soc_core + HA integration changelogs, move deploy scripts under scripts/ (1f98ab1)

### CI

- **version-bump:** refresh component changelogs on the PR branch (#11) (6326afb)

### Other

- feat!: drop the node block, bump schema_version to 2 (7990f5c)

## v0.1.24 (2026-08-30)

### Features

- added versioning (7287d0d)
- added version to status and make display elements optional (076accd)
- **battery_soc:** topology-aware SoC model (parallel/series), per-service deploy (ef618e9)
- **diagnostics:** detect missing configured devices (P1.1) (8861381)
- **automation:** add per-rule trigger history with dashboard view (7eb80b6)
- **battery_soc:** update battery calibration and SOC curve handling (e883de7)
- **automation:** show trigger history live via a dedicated MQTT topic (5d6e453)
- **battery_soc:** extract core logic and adapter modules (4a31188)
- **battery_soc:** show net battery power as a measurement, not diagnostic (b3878e8)
- **shelly:** fetch device info in the diagnostic poll for HA discovery (2f28886)
- **ha:** native Home Assistant integration for battery SoC (99ef3cf)

### Fixes

- shelly preset targeting 2 instead of 1 adc (c3b8d95)
- node doesn't report cpu load and memory use (12db2bc)
- battery_soc always stops working when one input topic is stale (eb22991)
- battery soc doesn't subscribe topics without json key (509a1bc)
- battery soc tries to write in root path (9950b35)
- **trucki/dashboard:** cut discovery and diagnostics noise (cf78ce0)

### Performance

- **src:** add analysis documentation and optimize service updates handling (4c67bcc)

### Dev

- organized secrects (7f5d6f4)
- added version file to common package and adjusted versioning (3e10297)

## Unversioniert (bis 2026-08-17)

### Features

- added mqtt bridge settings (20797fe)
- add simulation mode to apsystems ez1 (c0bde2d)
- added multi-device support for apsystems (1b140b9)
- added switch for full power disable (62d9d0b)
- added tuya valve, including preparation (ab47b3a)
- added simulation mode to tuya_valve and improved availability topic with disconnect reason (283b8e1)
- added diagnostics node for werkstatt-iot (663efbf)
- added connection ref to node for both devices (7a7c8a7)
- added battery_soc service (480d206)
- added availability and simulation to battery soc (5d4686b)
- added ha topic extension for trucki stick (17d6cb9)
- created module for central refresh rate configuration (8f8382f)
- added central simulation mode (ad8311a)
- added shelly rpc support (4e9dc7b)
- make apsystems ez1 configurable via device.json (e8f1523)
- add readback for shelly service (b90b137)
- added availability topic for all devices (7704fc0)
- provide configuration json for battery soc service (a97b7ed)
- update tuya service to work with konfiguration json as well (50f9600)
- added reload for configurable services (e7af942)
- added tiny tuya setup tool (ffa12d4)
- added device ignore and discovery removal (a522833)
- added presets for shelly configuration (ba765e6)
- added trucki service (2c0e9cd)
- added more configuration options for battery_soc service (60b3921)
- add energy automations (Teil B) — standalone service, dashboard tab, deployment, docs (637bf38)
- **dashboard:** visual automation-editor with live value and test execution (399eaff)
- **energy:** add battery_soc role (A1) and design docs for Spec A/C (34247a2)
- **automation:** add entity_value conditions backed by a shared value-template evaluator (ddbf143)
- DC-aware battery SoC and simulation improvements for Shelly/APsystems (c12b7c8)
- replace per-service *.env files with a central config.json (d060b5a)

### Fixes

- wrong simulation handling (fee3c72)
- non retained ha discover entries (e7f1ca8)
- possible status topic, beeing 0 (false) even when operating (6dd230c)
- **automation:** keep publish_allowed_prefixes fail-closed, auto-fill it from the dashboard (a358ba6)
- automation service looses mqtt subscriptions on broker restart (98868d7)
- tuya service migration faults after config update (c2ae8eb)

### Refactors

- no device ha discovery for services, only outstation topics (3a4e503)

### Chores

- added tailscale setup (629acb3)
- added schema files for device .json (a0984e7)
- hardened schema usage for configuration (81f095f)
- added output for tuy tool (9e3ab9d)
- restored battery_soc remaining as independ device, but acting as parent device for the trucki devices (e2ebc76)
- updated trucki service to actual fetched information (539294d)
- added updated shelly device description (cf65c24)

### Dev

- reorganized repo structure (align to target deploy) (8cdac12)
- updated devices to latest settings (b6f994d)

### Other

- added used inverter settings (53582b6)
- moved md files into (f2e7153)
- reduced diagnostics multiplier to a single setter in the node (c7180e8)
- remove plaintext MQTT credentials from tracked files (16e2f69)

