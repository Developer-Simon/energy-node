# Changelog

## v0.6.0 (2026-09-10)

### Chores

- bump minor versions for dashboard, services and energy_node_common (3916742)

## v0.5.26 (2026-09-10)

### Features

- **dashboard:** serve automation notifications from a dedicated endpoint (#6) (58bd7a6)
- make device services self-describing with per-service manifests (#12) (2e8c911)
- **⚠ Breaking:** fold the config.json node block into dashboard.node_* (schema_version 2) (#14) (e7f4ba1)
- **dashboard:** rebuild the history settings tab (#16) (5a41c5c)
- **dashboard:** rebuild the settings tabs as responsive material cards (#18) (d905f8b)
- **nodeagent:** add injectable system-metric readers (547d1f3)
- **nodeagent:** build the energy_node HA discovery and state payloads (acdd553)
- **⚠ Breaking:** move node telemetry into the dashboard, delete the energy-node service (540eac6)
- **dashboard:** broadcast simulation_active and expose per-metric toggles (99dd2a5)
- **nodeagent:** derive per-service liveness from MQTT (627cadc)
- **dashboard:** optional CPU temp, RAM and undervoltage in the status bar (1fe14fe)
- **⚠ Breaking:** move the dashboard's own topics under outstation/energy_node (25358cb)

### Fixes

- **dashboard:** show system-config revisions as a read-only list (#7) (3e7dee7)
- **nodeagent:** address task-6 review findings (ee8e3c4)
- **nodeagent:** correct throttle bits, guard node id, unify device block (92bd8fa)
- **dashboard:** style the mqtt metric-toggle fieldset and refresh stale docs (0b3e4ec)

### Refactors

- split src/ into services/ and libs/, rename service-level device_id to service_id (#10) (75abe73)
- remove git hooks, and stop the per-PR CHANGELOG stacking (#13) (c643260)
- normalise the node MQTT/HA id to energy_node (800fd08)

### Documentation

- finish the energy-node -> energy_node rename and guard it (a01c32e)

### CI

- **version-bump:** refresh component changelogs on the PR branch (#11) (6326afb)
- path-filter the workflow and flag breaking changes in the changelog (#15) (5282452)
- skip Markdown-only diffs and gate the job set on the version bump (#17) (9446219)

## v0.5.18 (2026-09-07)

### Features

- **dashboard:** configurable compact device card (#4) (e58d0e2)
- **dashboard:** modernise the non-energy overview tiles (#5) (9c19382)

### Fixes

- **dashboard:** keep topic samples payload after availability heartbeat (#3) (5c7cde4)

## v0.5.15 (2026-09-06)

### Features

- **battery_soc:** calibration hardening and guided tuning suggestions (#2) (b18974c)

## v0.5.14 (2026-09-06)

### Features

- **docs:** Add comprehensive documentation on dashboard and services (35cce56)
- **docs:** update favicon and improve documentation (a7f25f4)

### Documentation

- record license and version for vendored dashboard JS libraries (032743e)
- update changelogs (6284c1a)

### Tests

- add availability topics and update minibroker for live data publishing (d3b36e9)

### Chores

- initial public release of Energy Node (e9c9417)

### Dev

- update pages and add release ci (#1) (f461a51)

## v0.5.25 (2026-09-10)

### Features

- **dashboard:** serve automation notifications from a dedicated endpoint (#6) (58bd7a6)
- make device services self-describing with per-service manifests (#12) (2e8c911)
- **⚠ Breaking:** fold the config.json node block into dashboard.node_* (schema_version 2) (#14) (e7f4ba1)
- **dashboard:** rebuild the history settings tab (#16) (5a41c5c)
- **dashboard:** rebuild the general and display settings tabs as material cards (fdba93e)
- **dashboard:** rebuild the Verläufe settings tab with Apple-style controls (fecc586)
- **appconfig:** add dashboard.node_* alongside the node block (e1c73d1)
- **dashboard:** compose config.schema.json from per-service fragments (fc73de5)

### Fixes

- **dashboard:** show system-config revisions as a read-only list (#7) (3e7dee7)

### Refactors

- split src/ into services/ and libs/, rename service-level device_id to service_id (#10) (75abe73)
- remove git hooks, and stop the per-PR CHANGELOG stacking (#13) (c643260)
- **dashboard:** remove the no-op discovery-JSON-tooltip feature (04f298a)
- read the node identity from dashboard.node_* (5e1e672)

### Documentation

- drop remaining git-hooks references (7786f6d)
- config.schema.json is now generated from per-service fragments (0105cb7)

### Style

- **dashboard:** lay the history settings cards out in a responsive grid (bc0cf9b)

### CI

- **version-bump:** refresh component changelogs on the PR branch (#11) (6326afb)
- path-filter the workflow and flag breaking changes in the changelog (#15) (5282452)
- skip Markdown-only diffs and gate the job set on the version bump (#17) (9446219)

### Other

- feat!: drop the node block, bump schema_version to 2 (7990f5c)

## v0.5.12 (2026-09-05)

### Features

- **dashboard:** compact device tile with theme-aware toolbox icon colors (03f989a)
- battery status card with column and trajectory displays (7bb501c)
- **battery-card:** responsive trajectory sizing and configurable windows (4e844c9)

### Fixes

- resolve TinyTuya credentials handling and improve validation logic (daf69e4)
- adjust settings panel to display used storage writes (f7e3b2d)

### Refactors

- rename product Werkstatt-IoT to Energy Node repo-wide (3978730)
- rename Werkstatt-IoT to Energy Node, externalize deploy config, and document pi migration (daf06da)
- remove deprecated 'entity' card type and migrate to 'entity_value' across the dashboard (d2fbe37)
- **dashboard:** redesign Tailscale/TinyTuya setup wizards with Apple-style motion and shape (a26139b)

### Documentation

- refresh changelogs before public fork (220794e)

### Style

- update system tab styles and button designs (ab0a071)

### Dev

- **changelog:** add changelog for dashboard, src, and energy_node_common (768b498)
- **scripts:** auto-generate battery_soc_core + HA integration changelogs, move deploy scripts under scripts/ (1f98ab1)

## v0.4.0 (2026-09-04)

### Chores

- **dashboard:** bump version to v0.4.0 (b04fde1)

## v0.3.19 (2026-09-04)

### Features

- **webui:** implement compact device overview (a272741)
- **dashboard:** compact device view patches values instead of full swap (da52c59)
- **dashboard:** redesign the energy-flow card (4f825f7)
- **dashboard:** floating action bar and collapsible JSON editor on the config page (6d7bba0)
- **dashboard:** Enhance system configuration management with schema validation (144442a)
- **dashboard:** add battery SOC to history recorder with % unit (42c1710)
- **dashboard:** publish energy values as a Home Assistant MQTT device (c662c36)
- **layout:** overview layout-editing mode (2378171)

### Fixes

- **energyflow:** icons not updated and SoC not displayed (9a23d9d)
- **dashboard:** adjust energy flow middle text alignment (581cddb)
- **dashboard:** update panel CSS version for consistency across configurations (817fade)
- **dashboard:** resolve energy-device value_template in its own tiles (190f1be)
- **dashboard:** filter out dashboard's own energy device from assignments and aggregation (f8edb58)
- **dashboard:** update view mode handling to only affect current session without saving to API (21e809b)

### Refactors

- CSS for diagnostics, history, and manager panels (60e0ef8)
- **history:** implement dual Y-axis for mixed units and enhance tooltip formatting (d6cc3e0)
- **dashboard:** Apple-style redesign of the energy tab (736301f)

### Style

- **dashboard:** refactor configuration tab styling (068000c)
- **dashboard:** update base.css version and enhance masthead styling (5a759e2)

## v0.2.14 (2026-08-30)

### Features

- **dashboard:** stop rebuilding the overview grid on every live update (fdeebe9)
- **energy_flow:** enhance battery display with animated icon (9a9f754)
- **automation:** show trigger history live via a dedicated MQTT topic (5d6e453)
- **dashboard:** added icons to all energy-flow tiles (9a72720)
- **dashboard:** unify device control and analysis into one device modal (f2eaf5e)
- **dashboard:** patch device tiles in place unless discovery changes (3a20991)
- **history:** fade gap edges instead of a grey block, keep zoom on refresh (4a8749a)
- **dashboard:** ship SSE entities-delta and pause the occluded card grid (9a598ac)

### Fixes

- **dashboard:** daylight polish for the device modal (74d6d10)
- **trucki/dashboard:** cut discovery and diagnostics noise (cf78ce0)
- **dialog:** set definite height for device modal to prevent rendering issues on iOS (aedde81)
- **history:** stop gap-fade stubs from slashing across the chart (5fb01e9)

### Performance

- **httpapi:** share one poll loop across all /api/v1/events connections (14df334)

### Style

- **automation:** make history enable a toggle slider (e5d1ba6)
- **energy-flow:** battery charges horizontally instead of vertically (ead6fa6)

## v0.1.42 (2026-08-26)

### Features

- added versioning (7287d0d)
- added version to status and make display elements optional (076accd)
- **battery_soc:** topology-aware SoC model (parallel/series), per-service deploy (ef618e9)
- add control component for settings selection and improve deployment (d6f3545)
- **energy:** add combined load mode with separated measured consumption (393cdb2)
- **diagnostics:** detect missing configured devices (P1.1) (8861381)
- added animation and better rezising to energy_schema (7537c33)
- **dashboard:** add history tracking and chart export (2a4fc49)
- **dashboard:** add entity value and group layout cards (3d150c7)
- **dashboard:** animate energy tiles from their presented value, not the target (02d5bc4)
- **dashboard:** add calculated Hausverbrauch series to history panel (b20beb0)
- **automation:** add per-rule trigger history with dashboard view (7eb80b6)
- **dashboard:** give the diagnostics layout card real status (5d85fc1)
- **dashboard:** exchange recent history between browser clients (c251e51)
- added pre-exchange of raw data to other clients (c6c39d5)
- **history:** add free date range picker and restyled history page (f7780fb)
- **dashboard:** keep overview live without rebuilding on refresh (7e4dbd6)

### Fixes

- overlapping energflow for battery (4b9063c)
- automatically assigned roles can't be explicitely set to "none" (ef973a9)
- non-working revision management also added labels for konfiguration options (e286b6f)
- wrong colors in apex charts for energy values (a6b61ca)
- bad wrapping of input number and text entities on device tile (2c836ca)
- history gaps aren't displayed as gaps and linear connection instead (2c26378)
- 1-min samples in history are displayed as gaps (95eea72)
- **battery_soc:** battery voltage configuration for series and parallel topologies (772a015)
- **history:** external day ahead data wasn't displayed in raw views (5ef6667)

### Refactors

- **dashboard:** remove the energy_summary layout card type (68a8b8c)

### Tests

- fix stale test expectations left by recent refactors (37118a9)

### Style

- updated energy flow graphic in point of design (b78dabc)
- updated tile design for devices (f15ea11)
- restore old entity rows and make excludes for sensors without units and text entities (e5c5c16)
- **choices:** adjust button styles and padding for better visibility and alignment (7d90c8a)
- **device-map:** embedd toolbar and hide explanation text (5eead7d)
- **energy:** dash-duration token and stroke-dasharray transition (497514a)
- **energy-ring:** animate ring sections and table below (47d701c)

### Dev

- add presets to dashboard smoke test (9fb975b)

## Unversioniert (bis 2026-08-20)

### Features

- add dashboard showing all ha discovery (8b56f4b)
- added configuration manager for the dashboard (a411009)
- added diagnostics to dashboard (2610594)
- provide configuration json for battery soc service (a97b7ed)
- added dashboard configuration diff viewer (d57e23b)
- update tuya service to work with konfiguration json as well (50f9600)
- added energy features to dashboard (8856be7)
- added icons for overview (176df60)
- added charts tab (d58f2dc)
- added roles for wallbox, heatpump and directional energy values (1eb62fe)
- added energy flow to dashboard (d02dbb3)
- added accordeon to hide default hidden entities on overview (5819e37)
- added devices page and tooltips for ha discovery on it (c41f20a)
- added reload for configurable services (e7af942)
- added tiny tuya setup tool (ffa12d4)
- added stale display to flow chart (ce36f47)
- added set option to dashboard device list (fc137a7)
- added memory health display (9560cca)
- added health estimation for sd cards + better api design (a18ec1e)
- added health score and devices status for diagnostics (c634fff)
- added mqtt status badge at the top and devies details (8905ee2)
- added system actions and required auth sheme (8eb050c)
- added control tile device list (1408519)
- added persistance to starge health (42c3d9a)
- added device ignore and discovery removal (a522833)
- Added Drag-n-drop layout editor for the overview page (7a90719)
- added presets for shelly configuration (ba765e6)
- added entity category detection to device page (a7eff93)
- added visibility categories for tiles on overview and device tab (7b85d60)
- added trucki service (2c0e9cd)
- added facivon (ac70f95)
- added device map tab to dashboard (9893681)
- added device-map edge styles, grid and revert button (6ce01c2)
- added mqtt setup for dashboard (a6545d0)
- added mqtt setup for a home assistant bridge (451ab71)
- added tailscale setup to dashbaord (b20ad48)
- added 6 new energy charts (dac7170)
- implemented drag and drop layout editor (7a9c72c)
- added support for ha proxy, hosting the dashboard under <ha-addess>/node/ (3159be6)
- added more configuration options for battery_soc service (60b3921)
- updated energy role managment (6234951)
- add energy automations (Teil B) — standalone service, dashboard tab, deployment, docs (637bf38)
- **dashboard:** add switchable color themes (Mint, Stromblau, Signalgelb, Tageslicht) (edfdbf9)
- **dashboard:** visual automation-editor with live value and test execution (399eaff)
- **energy:** add battery_soc role (A1) and design docs for Spec A/C (34247a2)
- **automation:** add entity_value conditions backed by a shared value-template evaluator (ddbf143)
- added energy role categorisation and hiding option (736f234)
- added SoC to energy charts (1d3d4ca)
- dashboard-Serverlast-Specs und Entitäts-Auflösung bei Automations-Aktionen (0909515)
- **dashboard:** reliable Mobileview across all tabs (95f8574)
- **dashboard:** replace window.confirm/alert and inline status with modal/toast stores (1c368df)
- **dashboard:** Fluid Grid Layout v3 with Map Type Catalog and Editor (c0e8f94)
- added various options to every energy graphic (f6e91d8)
- **graphics:** let the animation speed work by its higherst value and use toggle for layout bool settings (ffd051f)
- keep failed config reloads visible until resolved (b9620a4)
- add revision history to all dashboard configuration sections (63e1d09)
- command actions are stating "in progress" and only take over on success (b5a0da2)
- **automation:** visual card-based editor for automation rules (20e3234)
- DC-aware battery SoC and simulation improvements for Shelly/APsystems (c12b7c8)
- replace per-service *.env files with a central config.json (d060b5a)

### Fixes

- non retained ha discover entries (e7f1ca8)
- roles are not correctly load on the energy page (6d8e0c8)
- storage health is reset on system reboot (e78d352)
- device discovery review doesn't work always (d93b729)
- dashboard raising console errors (3187779)
- mqtt client is offline after service restart or general boot (4b46cac)
- not working automation online display due to wrong topic binding (12056e3)
- **automation:** keep publish_allowed_prefixes fail-closed, auto-fill it from the dashboard (a358ba6)
- **automation:** error on testing action, even if they succeed (9f469d0)
- energy_day graphic flickering on value refresh (71acb4f)
- energy_day tracks non-used usage even if part of residual usage (51a0587)
- **dashboard:** Fix eight reported UI issues with grid layout, energy-flow card, household consumption, balance accuracy, and scroll behavior (b295e61)
- battery charge/discharge is not displayed properly on the overview (7f52869)

### Refactors

- improved diagnostics and layout schema + revision (2f0167f)
- implemented more alpine.js sources (15a12e0)
- improved flow by availability (c20a27c)
- split manager.html (55c29cc)
- no device ha discovery for services, only outstation topics (3a4e503)
- added hours and days to time ranges on automation page (d713f85)
- headline always presented at full width (bb348bc)
- swap order of settings and automation tab (405d295)

### Performance

- adding sse events, removing polling intensity (c23bbe8)
- added subscription for mqtt (f7c8d45)
- updated dashboard for better performance (lazy loading) (60d318e)
- **dashboard:** cut server load with targeted quick wins (c118e8f)

### Tests

- cover missing-topic, missing-availability, and wrong-unit diagnostic rules (a73516f)

### Style

- updated styling of energy interpretation settings (ac87f69)
- device name of the entity is hidden on the device tile (66e4c5f)

### Chores

- added phase0 of dashboard (e3ae4f9)
- improved layout page (32ce92b)
- hardened schema usage for configuration (81f095f)
- improved energy roles (857736a)
- added apine.js and htmx for live data and better tabs (19dd766)
- added output for tuy tool (9e3ab9d)
- finalized runtime cache and extended api/v1/health (cc624f5)
- hardened mqtt discovery and diagnostics (4bbf576)
- added detection of shelly presets (09e1537)
- restored battery_soc remaining as independ device, but acting as parent device for the trucki devices (e2ebc76)
- fixed new lazy loading not fully working (c28f252)
- improved energy flow graphic (42ca2b4)

### Dev

- reorganized repo structure (align to target deploy) (8cdac12)
- added agents for workings on the dashboard (1d5362b)
- removed compile artifact (1a8c666)
- added tiny smoke test for dashboard (aaa7fbb)
- added simulation to smoke test (9fea5ed)

### Other

- added implementation notes for dashboard (727e257)
- moved md files into (f2e7153)
- reorganized knowledge and added hints on done items (5fd861b)
- updated knowhow to mqtt integration (9a9d145)
- documented layout editor and energy flow graphics (3280bcf)
- remove plaintext MQTT credentials from tracked files (16e2f69)

