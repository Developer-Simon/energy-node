# Changelog

## v0.7.17 (2026-09-26)

### Features

- **dashboard:** per-device icons and pinned favourite entities (#53) (d649df3)
- **homeassistant:** ship the dashboard's device icons as an HA icon set (#54) (c4c196b)
- **battery_soc:** DC-only systems in the MQTT service and conditional config forms (#58) (89bacae)
- **dashboard:** localization foundation with language switcher (#59) (7f8ed9f)
- **dashboard:** number format setting and locale-aware dates (localization A2) (#60) (6d8ba8b)
- **dashboard:** localize the shell and the overview (#62) (e96f396)
- **automation:** weekdays in the editor and a sun_window condition (2c301a9)
- **webui:** move the dashboard shell texts into the catalogs (3ae8750)
- **webui:** migrate base.html and dashboard.js shells into catalogs (580a662)
- **webui:** add English texts for the dashboard shell (fef5eaa)
- **webui:** move overview, tile and device list texts into the catalogs (7929c23)
- **webui:** add English texts for overview, tiles and devices (5370ff3)
- **numfmt:** format numbers with configurable separators (4798c3c)
- **settings:** add number format and digit grouping settings (2c67fd6)
- **webui:** render device values in the configured number format (5c5d50d)
- **webui:** add number, date and collation formats to the i18n runtime (0a80e55)
- **webui:** show live device values in the configured number format (f23ebf5)
- **history:** localize chart labels, tooltips and the date range picker (dcde8a1)
- **battery-card:** take the number formatter from the host page (848952c)
- **settings:** let the operator choose the number format (772317a)
- **localize:** resolve the UI language from cookie and Accept-Language (1835383)
- **localize:** add the translator, template funcs and catalog script handler (5f7c882)
- **webui:** add the de/en message catalogs with drift tests (eba79e6)
- **webui:** render templates per language and serve the catalog script (33fa49d)
- **webui:** add the browser i18n runtime and language switch handler (1477fab)
- **webui:** localize the login page with a language switcher (38221fb)
- **webui:** localize the masthead and status bar, add the language switcher (981ff47)
- **dashboard:** installer-style language pill and a formatting settings card (16aa9a8)
- **dashboard:** validate conditional JSON schema entries (if/then/else) (56cb98e)
- **dashboard:** expose each service's reload result per configuration (6f9f9ea)
- **dashboard:** show and hide conditional schema fields in the config editor (ca8f760)
- **dashboard:** show whether a service took over a saved configuration (304d813)
- **battery_soc:** conditional battery schema for system type and bank layout (53943e9)
- **dashboard:** animate conditional schema fields in and out (90425d7)
- **battery_soc:** require the bank A voltage topic in the dashboard form (5573a33)
- **dashboard:** export the device icon catalogue for Home Assistant (e7b3f40)
- **dashboard:** store per-device icon and favourite preferences (490352a)
- **dashboard:** let favourite entities drive the compact card rows (78749f7)
- **dashboard:** add a device icon catalogue and render it on the cards (8c90408)
- **dashboard:** serve device preferences to the tab, the stream and the modal (053972a)
- **dashboard:** add the device preferences and icon catalogue endpoints (8a9256a)
- **dashboard:** edit device icon and favourites in the device modal (1f32950)
- **dashboard:** pin a device's favourite entities at the top of its modal (b385f9c)
- **dashboard:** suggest device icons by type and polish the display prefs (1b14baa)

### Fixes

- **dashboard:** pass service fields and previous versions to the redeploy preview (#51) (0c07bc1)
- **redeploy:** keep following a run across the dashboard's self-update restart (#52) (38a0609)
- **webui:** complete remaining base.html and dashboard.js migrations (5913fed)
- **webui:** tidy the shell extraction keys and tests (e55ff9b)
- **webui:** keep stored page and group names (13508e8)
- **webui:** use plural keys for the entity and control counts (292a046)
- **settings:** name the digit grouping option after the number format (37017c7)
- **webui:** keep the language switcher clear of the masthead title (dba06da)
- **dashboard:** hide conditional schema fields despite the grid display rule (e3225b3)
- **dashboard:** start the status watch after the editor save reloads the config (11911c0)
- **dashboard:** tolerate fields of inactive schema branches from older files (06071f4)
- **battery_soc:** show the imbalance threshold only for two banks in series (f402372)
- **dashboard:** keep the exported icon markup readable in diffs (dc5114a)
- **dashboard:** keep the pinned-favourites block from breaking the modal grid (c0876ee)

### Refactors

- **webui:** route number, date and sort formatting through I18n (cdd0420)

### Documentation

- remove base.html and dashboard.js from i18n-pending.txt (57c5cad)
- **localization:** document number and date formats and bump asset versions (252a88d)
- **dashboard:** document localization and bump lazy asset versions (5181b2d)
- **dashboard:** describe config_revision as the last load attempt (715dd30)
- **battery_soc:** document DC-only systems, signed inputs and current units (6c2df1f)
- **dashboard:** document the device preference endpoints and bump the assets (aed1eee)

### Tests

- **webui:** guard against UI text that bypasses the catalogs (458af28)
- **dashboard:** fix cross-realm deepEqual in the new prefsDraft test (02d2ef3)

### Style

- **dashboard:** gofmt the device-prefs struct fields (19d915e)
- **dashboard:** draw device icons larger inside their frame (86f909c)

### Chores

- apply gofmt to test and main files (607f2f3)
- **webui:** bump lazy asset versions for the localized shell (0ef9e36)

## v0.7.8 (2026-09-23)

### Features

- **installer:** add package sources (file, repo build, GitHub) (#42) (f831eab)
- **dashboard:** download the newest release bundle from the redeploy page (#44) (245b277)
- **installer:** restart only the service units whose version changed (#48) (1d2c57f)
- **shelly:** optional wake webhook so a sleeping Gen1 device is polled the moment it wakes (3105234)
- **installer:** pass step requirements from the manifest to the web UI (69ffe39)
- **installer:** carry a restart-all request from the UI to the service steps (bad757a)
- **installer:** show per service which units restart in the preview (e2464cb)
- **updater:** take the target from the root-owned target.json for user-independent bundles (e3813fc)
- **dashboard:** add a GitHub release client that finds and downloads the node bundle (0493264)
- **dashboard:** extract, validate and atomically install a downloaded bundle (e0896ae)
- **dashboard:** add the Fetcher that downloads the newest bundle into the candidate directory (d411166)
- **dashboard:** run a prepare step in the redeploy host through a Prepare seam (4def9a9)
- **dashboard:** download the newest release bundle into the redeploy candidate directory (6ae89ec)
- **updater:** record the applied manifest as installed-manifest.json (c7e3d4b)

### Fixes

- **auth:** backfill RoleCheckUpdates for an already-bootstrapped admin (#38) (58d94f3)
- **dashboard:** show the download icon in the masthead update badge (#40) (d50ebf9)
- **dashboard:** stop duplicate energy card mounts from fighting over springs (#41) (3d3d7a8)
- **installer:** restart service units on update and record the installed manifest (#43) (0f2aec2)
- **shelly:** support sleepy H&T devices with an opt-in wake webhook (#49) (1e6d571)
- render the system-action helper on the node, validate targets with fullmatch, register the new tests (e546498)
- **dashboard:** hand the redeploy page the session's CSRF token (7bd8f3a)
- **dashboard:** implement the new hostapi.Sink.Message method (8ae6af3)

### Documentation

- describe which service units an update restarts (08ae4b9)

### Tests

- **dashboard:** add a shelly-ht smoke preset for sleepy H&T devices (07f6a6e)
- **dashboard:** show the package download in the local smoke test (f2c2f2c)

## v0.7.1 (2026-09-21)

### Fixes

- **dashboard:** show the download icon in the masthead update badge (61471d2)

## v0.7.0 (2026-09-16)

### Features

- **dashboard:** add a GitHub update check with a masthead notification (#34) (e5fc9db)
- **dashboard:** make the "check for updates" button more prominent (00a5e46)
- **dashboard:** link the update-available chip to the redeploy screen (c60f545)
- **auth:** add a role gating the GitHub update check (92e35f4)
- **updatecheck:** add a GitHub release version checker (7f5a047)
- **settings:** add a toggle for the automatic update check (207c345)
- **dashboard:** run the daily update check and expose it over HTTP (4b2cf9e)
- **dashboard:** show an update-available badge in the masthead (148a77e)
- **dashboard:** add a manual update check to settings (e1e2d0b)
- **dashboard:** add the updaterjob staging format (9b727eb)
- **dashboard:** add the energy-node-updater orchestrator script Co-Authored-By: Claude Haiku 4.5 <noreply@anthropic.com> (1a90149)
- **installer:** install the updater unit and its .path trigger in step 60 Co-Authored-By: Claude Haiku 4.5 <noreply@anthropic.com> (cd5df50)
- **dashboard:** add the local updaterhost Backend (a23ab21)
- **dashboard:** mount the local redeploy screen and resume it after a self-update restart Co-Authored-By: Claude Haiku 4.5 <noreply@anthropic.com> (2f2f0a2)
- **dashboard:** allow an optional installed_services block in config.json Co-Authored-By: Claude Haiku 4.5 <noreply@anthropic.com> (0710bea)
- **dashboard:** add Config.ServiceInstalled with the default-on rule Co-Authored-By: Claude Haiku 4.5 <noreply@anthropic.com> (faa9300)
- **dashboard:** thread a resolved installed_services map into the overview handler (9dbaa36)
- **dashboard:** hide the automations tab and tailscale/tuya settings subpages when deselected Co-Authored-By: Claude Haiku 4.5 <noreply@anthropic.com> (106aa98)

### Fixes

- **updatecheck:** recognize the v-prefixed version main.go actually passes (7808e4c)
- **dashboard:** use InFlight() to allow staging new jobs after previous job completes (2086154)
- **installer:** remove unnecessary committed copy of signing key (3e23319)
- **dashboard:** gate /redeploy/ behind the system-actions role and CSRF (7f3a132)
- **installer:** make a real redeploy work and stop job.json from steering root (694cbe3)
- **dashboard:** silence SC2317 on the trap-only finish_interrupted (7d5843d)
- **dashboard:** keep the generated config schema in sync with its source (38d5d6d)
- **dashboard:** forward-compatible schema, close automations fragment gap (6549e38)

### Tests

- **dashboard:** add a smoke-test flag to simulate an available update (5c98ab5)
- **dashboard:** add --https to the local smoke test (386d716)
- **dashboard:** add a smoke-test preset with every optional service off (f1f33f5)

### Chores

- **dashboard:** bump cache-busted asset versions (d8d2094)
- **dashboard:** depend on the energy-node-webui module (0ed9456)

## v0.6.7 (2026-09-16)

### Features

- **installer:** hide dashboard tabs for deselected optional services (#31) (59f2d40)
- **installer:** add the dashboard's local self-update path (Plan D) (#33) (e50d236)

## v0.6.5 (2026-09-13)

### Features

- **dashboard:** migrate config.json schema_version 1 to 2 on load (#20) (b8f72d1)
- **dashboard:** replace theme select with a four-way segmented slider (#23) (3855e9c)
- **dashboard:** redesign the MQTT settings tab (#24) (8844555)
- **dashboard:** restyle MQTT settings tab to match card layout (2d5f2bf)
- **dashboard:** fill row gaps and add floating save bars on MQTT tab (a389ce4)

### Fixes

- **dashboard:** finish the config.json v1 -> v2 migration path (#21) (9d4fb53)
- **dashboard:** keep MQTT settings steppers from overflowing narrow cards (#27) (79ba6ee)
- **dashboard:** update webui tests for the bumped CSS cache-bust versions (49355fb)
- **dashboard:** fix MQTT tab reflow, add steppers and tooltips (927e10f)

## v0.6.2 (2026-09-11)

### Features

- **dashboard:** replace theme select with a four-way segmented slider (465c4af)
- **⚠ Breaking:** move node telemetry into the dashboard nodeagent (#19) (8049117)
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
- **dashboard:** rebuild the general and display settings tabs as material cards (fdba93e)
- **dashboard:** rebuild the Verläufe settings tab with Apple-style controls (fecc586)
- **appconfig:** add dashboard.node_* alongside the node block (e1c73d1)
- **dashboard:** compose config.schema.json from per-service fragments (fc73de5)

### Fixes

- **dashboard:** show system-config revisions as a read-only list (#7) (3e7dee7)
- **nodeagent:** address task-6 review findings (ee8e3c4)
- **nodeagent:** correct throttle bits, guard node id, unify device block (92bd8fa)
- **dashboard:** style the mqtt metric-toggle fieldset and refresh stale docs (0b3e4ec)

### Refactors

- split src/ into services/ and libs/, rename service-level device_id to service_id (#10) (75abe73)
- remove git hooks, and stop the per-PR CHANGELOG stacking (#13) (c643260)
- normalise the node MQTT/HA id to energy_node (800fd08)
- **dashboard:** remove the no-op discovery-JSON-tooltip feature (04f298a)
- read the node identity from dashboard.node_* (5e1e672)

### Documentation

- finish the energy-node -> energy_node rename and guard it (a01c32e)
- drop remaining git-hooks references (7786f6d)
- config.schema.json is now generated from per-service fragments (0105cb7)

### Style

- **dashboard:** lay the history settings cards out in a responsive grid (bc0cf9b)

### Other

- feat!: drop the node block, bump schema_version to 2 (7990f5c)

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

### Tests

- add availability topics and update minibroker for live data publishing (d3b36e9)

### Chores

- initial public release of Energy Node (e9c9417)

### Dev

- update pages and add release ci (#1) (f461a51)

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

