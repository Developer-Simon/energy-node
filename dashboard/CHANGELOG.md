# Changelog

## v0.10.12 (2026-10-24)

### Features

- **automation:** Automation rules can repeat their actions while their conditions hold. (#163) (1f30b74)
- **automations:** The Automations page shows scenarios with a live card, a chart and a phase editor, and has a redesigned rule editor. (#164) (04148fb)
- **automations:** Scenario and rule editors show devices as tiles with live values and pick them in a shared device picker. (#165) (8f0b222)
- **automation:** whole-day time windows and a maximum scenario duration (5483950)
- **automations:** maximum duration field in the scenario editor (2d7cf1a)
- **automations:** assistant device context (cf874ee)
- **automations:** assistant steps, search and answers (b6844f3)
- **automations:** assistant effects and regulator preview (54f188b)
- **automations:** zero feed and wallbox surplus templates (3717f33)
- **automations:** switch surplus, battery warning and time switch templates (d6b05fb)
- **automations:** validated assistant template fixture (7b58e68)
- **automations:** assistant dialog state and creation (1201636)
- **automations:** assistant dialog gallery and first step blocks (18d80b3)
- **automations:** assistant multi, number, time and summary blocks (45c2d25)
- **automations:** A template assistant creates scenarios and rules from a few questions, with five templates such as zero feed-in and surplus charging. (#166)
- **dashboard:** single choice, role filters and tones in the device picker (91d14ed)
- **automations:** device candidates for actuators, meters and actions (c76380a)
- **automations:** stepper helper and soft field styles (1d06267)
- **automations:** condition cards with soft fields and steppers (9021a6d)
- **automations:** band preview geometry for the regulator (d2cb6cf)
- **automations:** regulator device chips and picker logic (b132725)
- **automations:** regulator band preview and device chips markup (9f3c3ac)
- **automations:** fine tuning card with steppers (edbc8a0)
- **automations:** action kind and device choice logic (b207441)
- **automations:** action cards for switch, setpoint, notify and mqtt (88032aa)
- **automations:** sparkline geometry and history switch logic (cdc9fb6)
- **automations:** history switch with sparklines in collapsed rows (5e717f0)
- **history:** record scenario regulator outputs alongside extra entities (b23357d)
- **history:** sample scenario series into the exchange buffer (d0da2c6)
- **automations:** scenario status, phases and validation helpers (d07ce4f)
- **automations:** chart geometry for scenario cards and rule timelines (a45e173)
- **automations:** scenario state, undo and limits in the page component (b76494f)
- **automations:** save scenarios with validation and default stripping (b173330)
- **automations:** scenario and rule tabs, undo bar and shared row editors (d814754)
- **automations:** scenario chart data, live points and today export in the page component (48b888c)
- **automations:** live scenario card with chart and phase bar (42f0fdc)
- **automations:** rule card live bar, timeline and repeat mode helpers (9877b67)
- **automations:** redesigned rule card with when, then and flow blocks (6595beb)
- **automations:** scenario regulator editing helpers (cce322e)
- **automations:** edit scenario phases and the regulator (81e78fe)
- **automation:** validate scenarios (de7556c)
- **dashboard:** map automation scenarios in the data flow (eb3a375)
- **automations:** repeat validation and interval settings logic (a128e57)
- **automations:** repeat field and interval settings in the editor (39a3aeb)

### Fixes

- **automations:** assistant layout, neutral buttons and steppers (4782e5b)
- **dashboard:** accept a picker choice with Enter in single mode (e65eb65)
- **automations:** chip counts and value-less meter candidates (b5b9f77)
- **automations:** role tone on the setpoint action head (87c770e)
- **automations:** use theme tokens in action card styles (44da706)
- **automations:** editor review round for device chips, conditions and actions (abc57f3)
- **automations:** colour consumer regulator output by direction (a96c26d)
- **automations:** editable condition and action rows in the scenario editor (6d0b4e1)
- **automations:** hide retained scenario values while the service is offline (af504e0)
- **automations:** repeat check, unique wizard ids and dead code cleanup (d298834)
- **automations:** show net and setpoint in the collapsed scenario row (c9a200d)
- **automations:** restore the short preview in collapsed rules (fd35320)
- **dashboard:** add German texts for the scenario schema (e5aa7dd)
- **automations:** field help names the configurable minimum interval (7567a97)

### Refactors

- **automations:** remove the rule wizard from the page component (b468dc1)
- **automations:** row editors read state through owner hooks (75b2998)

### Documentation

- **automations:** plainer texts and template names in the assistant (18224e9)
- **automations:** device chips, picker and action cards (411838a)
- **automations:** scenario page and rule editor (6352d47)

### Tests

- **dashboard:** cover the automation rules schema with scenarios (d5d7d36)
- **dashboard:** make alle-funktionen the default smoke preset (5c25a87)

### Chores

- **dashboard:** bump automations asset versions (34f8085)
- **automation:** scenario smoke fixture and 0.6.0 manifest (dcd062c)

## v0.10.8 (2026-10-22)

### Features

- **devicemap:** The device map can hide connections by type (energy, data, info) with a new filter button. (#158) (2e85408)
- **settings:** Settings pages are listed in a sidebar, on phones in a sheet from the bottom. (#159) (797ce0c)
- **settings:** The dashboard title and subtitle can be changed or hidden. (#160) (74a38ad)
- **dashboard:** The diagnostics page shows the logs of the Energy Node services, with search and automatic refresh. (#161) (06fca9b)
- **dashboard:** add read-journal action to the system action helper (27a811d)
- **dashboard:** parse journal entries and validate log queries (1d1f7db)
- **dashboard:** read allowlisted service journals through the helper (18c512c)
- **dashboard:** add service log endpoints (399197c)
- **dashboard:** add service log error texts and API docs (957bad1)
- **dashboard:** add the service logs panel component (329d120)
- **dashboard:** show service logs on the diagnostics page (98b2272)
- **dashboard:** rework the service logs view (a9dcdf7)
- **dashboard:** keep the service log scrolled to the newest line (04d2e6a)
- **settings:** make the masthead title and subtitle configurable (b9217a1)
- **settings:** rework the masthead card and share the energy save bar (bfff2a3)
- **status:** offer CPU load in the system status bar (66f9c89)
- **settings:** fill gaps between settings cards with a masonry layout (f554a83)
- **settings:** let the system status and tab width cards join the masonry (1780266)
- **settings:** move the history storage hint into a tooltip on the recording card (b87478c)
- **settings:** slide in the Tailscale and TinyTuya wizard blocks like the cards (f3e5f4c)
- **webui:** add MQTT, Tailscale and Tuya icons to the sprite (4accb17)
- **settings:** replace the sub tab row with a sidebar and a page sheet (16a7870)
- **settings:** let the settings tab use the full width by default (7704136)
- **auth:** add edit_device_map and edit_device_map_view roles (5fccc61)
- **settings:** store wiring type filter in the device map view (2030cf9)
- **settings:** diff device map structure and view (36fe304)
- **devicemap:** split map structure and default view permissions (6e8bdbd)
- **devicemap:** add view preference helpers for browser storage (73dd689)
- **devicemap:** keep view settings in the browser and save the default separately (7a74092)
- **devicemap:** filter wiring types and respect map edit roles (6e928ff)
- **devicemap:** hide wiring edges by connection type (2fb76a6)
- **devicemap:** add filter texts, sprite and view prefs script (622c06a)
- **devicemap:** filter button, hidden energy notice and default view controls (e299f62)
- **devicemap:** connection icon, automation lines in the filter, save keeps the view (84c88f1)
- **devicemap:** use a funnel with a connection node as the filter icon (b663822)
- **webui:** end edit mode when changes are discarded (30afe53)
- **settings:** update notice as a row with one update button (60a73fa)

### Fixes

- clearer update notice, firewall station completes, service steps skip unchanged libraries (#153) (c184d81)
- **diagnostics:** use a working health threshold (#157) (48d39f7)
- **dashboard:** allow all service keys in read-journal (20b27c4)
- **dashboard:** pause service log polling while the panel is hidden (e72eb36)
- **settings:** measure the health threshold in seconds with a usable default (7366e0d)
- **settings:** explain the health threshold in seconds (9716ff5)
- **settings:** show the health threshold help as a tooltip (8851977)

### Refactors

- **common:** clean up the slave status and hooks (#155) (00ddbed)
- **webui:** remove Gridstack (#156) (d89aab3)
- **diagnostics:** drop the poll interval entity that no service publishes (8f3c65b)
- **webui:** remove the unreachable Gridstack path from the layout editor (269a4f1)
- **webui:** drop unused layout editor helpers (2a66065)
- **webui:** drop the unreachable pending runtime status (9c2eb2b)

### Documentation

- **dashboard:** add service log and connection filter screenshots (67e2ede)
- remove the remaining Gridstack references (1f5d014)

### Tests

- **dashboard:** add a service-logs smoke preset with a simulated journal (04ec1f6)

### Chores

- **webui:** drop the unused Gridstack library (b6cfe5f)
- **webui:** bump the layout editor asset version (9e37aa8)

## v0.10.0 (2026-10-19)

### Features

- **changelog:** expandable highlight details in the what's new dialog (#152) (1342d6c)
- **versions:** pass highlight details to the what's new dialog (825b1ee)
- **webui:** detail helpers and strings for expandable highlights (d0c02bb)
- **webui:** expandable highlight details in the what's new dialog (0aa004d)
- **webui:** list highlights with details first and align the bullets (cc9b264)
- hourly energy statistics from Home Assistant via the companion (#130) (752f59a)
- **dashboard:** energy statistics tile, page and recording mode (#132) (502a156)
- **dashboard:** SSE topic subscriptions and a swap-free statistics tile (#133) (69eb694)
- **dashboard:** device tiles patched from the devices topic without fragment swaps (#135) (76f51aa)
- **changelog:** Release highlights now have a German text that shows in the dashboard, the installer and the release notes. (#134) (ccd90ca)
- **changelog:** Choose how many versions the changelog shows, with the history kept on the device across updates. (#137) (14f781e)
- **dashboard:** The MQTT tab explains how to set up the Mosquitto bridge, and the published system metrics are chosen in one multiselect. (#138) (3c7821a)
- **history:** The history page offers the resolutions that fit the chosen span in a dropdown inside the status line and preselects the coarsest one that has data. It shows hourly values from the energy statistics for a week or longer, has a 3 month preset and lets you pause the page refresh. (#141) (d7971dd)
- **discovery:** The dashboard announces wallbox, heat pump and custom category sensors in Home Assistant once something is assigned to them, and each can be deselected in the MQTT tab. The companion supplies these roles for history and energy statistics. (#142) (66cd698)
- **energy:** The automatic load mode now always calculates the house consumption from the balance, and a house meter serves as a cross-check. A status line on the energy page shows the source, the fallback and the meter deviation, and Home Assistant gets measured and calculated house load as separate sensors. (#143) (59dfeb3)
- **devicemap:** The device map shows which devices are switched off. A switched-off output cuts the line behind it, and every device without power fades out with an off label. (#145) (b72187d)
- **devicemap:** Switches are now an energy role with an optional channel. The device map shows a green or red circle on every switched connection, and only the connection on a switched-off channel is cut. (#146) (9b162de)
- **energy:** hide only role-less entries and share the list with the device map (#147) (0afdfc3)
- **devicemap:** Name single appliances on a meter as sub-devices in the device map, the device dialog and the energy page. (#148) (054f852)
- **overview:** options for the energy statistics card (#151) (b4f0f02)
- **i18n:** texts for the energy statistics card options (0c0a08a)
- **layout:** options for the energy statistics card in the editor (7712cae)
- **layout:** store fixed period, key figures and chart switch of the statistics card (068e4f3)
- **overview:** statistics card honours fixed period, key figures and bar chart (1ba32b9)
- **i18n:** texts for the history card series chooser (63cb729)
- **layout:** pick the history card series in a search field (dbb14e2)
- **devicemap:** store sub-devices in device-map.json (2a07b5f)
- **energy:** label entities after their sub-device (cda32d0)
- **devicemap:** add the sub-device endpoint (660a51f)
- **devicemap:** keep sub-devices on save and reapply their labels (f858835)
- **devicemap:** add the sub-device model (dddd9f7)
- **devicemap:** draw sub-devices behind their meter (9609896)
- **devicemap:** draw sub-devices on the map page (5a6f9d3)
- **devicemap:** draw sub-devices on the plant view (a918205)
- **devicemap:** manage sub-devices in the map panels (f0e9713)
- **devicemap:** show sub-devices in the map templates (bdc1420)
- **energy:** label role rows after their sub-device (a353e47)
- **devices:** manage sub-devices in the device modal (8247539)
- **energy:** show sub-devices as combined labels and in the categories overview (2d41014)
- **energy:** make plant role edges opt-in and start sub-device role edges at the sub-device (5c89968)
- **devices:** make sub-device management opt-in and refine its buttons (549cf08)
- **devices:** offer sub-device management as a quiet button (25489e8)
- **devices:** add an icon to the sub-device management button (9a80731)
- **energy:** move the sub-device overview into its own accordion section (db80f56)
- **energy:** add icons to the accordion sections and flatten the sub-device list (3736316)
- **energy:** store hidden role table entries in energy.json (012eff7)
- **devicemap:** hide role table entries in the device panel (6fb2734)
- **energy:** hide only entries without a role and share the list (986870a)
- **energy:** add the switch role with a channel (ee3bf7c)
- **energy:** validate switch roles against the registry (f444022)
- **devicemap:** resolve switches from energy roles and cut edges per channel (4f2be72)
- **devicemap:** model switches as role rows (3e6c72f)
- **i18n:** add texts for the switch role, channels and edge state rows (3fa3e4b)
- **devicemap:** drive the panel and map from switch roles (c0a530d)
- **devicemap:** show switches as role tiles in the device panel (9422ecb)
- **devicemap:** draw the switch port on every switched edge (93c3223)
- **devicemap:** lead the edge card with switch state and cause (e8d7ee6)
- **devicemap:** show state rows, links and channel in the edge card (469c4ab)
- **energy:** set the switch role and channel in the energy configuration (ba74a24)
- **devicemap:** stack the panel links and add icons (049dcfc)
- **devicemap:** store switch output functions in device-map.json (6d70bc1)
- **devicemap:** serve switch entities with their resolved state (c4d8edd)
- **devicemap:** detect switch outputs and compute the area without power (877e596)
- **devicemap:** draw quiet nodes and an open output port (a97e278)
- **devicemap:** show an off pill under switched-off nodes (3db72e8)
- **devicemap:** style cut and dead edges and stack the quiet node image (e156d24)
- **devicemap:** load switch states and mark switched-off nodes (4ccec11)
- **devicemap:** show switched-off outputs and the area without power (d711591)
- **devicemap:** assign switch outputs in the device panel and explain dead edges (bab83bb)
- **stats:** hatch export and battery charge instead of a second colour (28ab382)
- **dashboard:** overflow tabs into a More list, main menu mode per browser (29a532c)
- **energy:** auto load mode calculates with the house meter as cross-check (31771de)
- **energy:** mirror the new load modes in the browser balance (334c011)
- **energy:** migrate combined load mode to one consumer category per entity (80e6e5d)
- **discovery:** publish measured and calculated house load as optional sensors (d266e0c)
- **energy:** load mode cards, meter hints and status line on the energy page (8414c1b)
- **energy:** technical mode texts, house meter on the energy node, inset notices (da47ece)
- **energydiscovery:** optional sensors for wallbox, heat pump and categories (6f21c9a)
- **dashboard:** announce optional energy sensors and let them be deselected (339fee9)
- **dashboard:** choose the additional Home Assistant sensors in the MQTT tab (a1478cb)
- **energystats:** hourly means for the history page (8ac2d1c)
- **history:** offer resolutions per span and default to the coarsest (6819e41)
- **history:** hourly tier from the server with per-series 5 min fallback (4884935)
- **history:** resolution state on the history page (51ddf71)
- **history:** resolution picker and a 3 month preset (cbdbd3b)
- **history:** coverage planning with a settle cutoff (a6c5255)
- **history:** exchange minute values up to 30 minutes ago and fetch the last half hour once (101ed8c)
- **history:** coverage based resolution default, explicit selection and refresh pause (8568e1c)
- **history:** resolution dropdown and a pause button for the history page (9ddc271)
- **history:** resolution dropdown and pause button inside the status line (b6381c3)
- **dashboard:** published diagnostics metrics as a read-only-for-guests multiselect (63f6bf4)
- **dashboard:** bridge setup step list on the MQTT tab (638c5ee)
- **dashboard:** keep every received changelog version on the node (6061446)
- **dashboard:** add the changelog_history setting (26ea4d7)
- **dashboard:** limit the changelog API to a number of bundle versions (5a1b1cc)
- **dashboard:** choose how many versions the changelog shows (9606138)
- **dashboard:** show the changelog history choice as a segmented control (95bbbda)
- **dashboard:** carry highlight translations in the highlights API (963ab1a)
- **dashboard:** show release highlights in the page language (908d3ce)
- **dashboard:** per-device live entry and localized tile status (db88e93)
- **dashboard:** single-tile fragments layout-item and device-card (e7dcb82)
- **dashboard:** per-tile structure and row template markup (f7b2274)
- **dashboard:** SSE topic devices with a per-subscriber device selection (a4c76b6)
- **dashboard:** patch device tiles from the devices topic (3059c80)
- **dashboard:** device tiles follow the devices topic instead of fragment swaps (dacff34)
- **dashboard:** subscribe to SSE topics with ?topics= (43cb488)
- **dashboard:** open the event stream with the active panel's topics (8129889)
- **energystats:** revision counter for changes to the statistics (f9300b4)
- **dashboard:** SSE topic energy_stats with the statistics revision (b937a7d)
- **dashboard:** serve the statistics tile from the energy_stats branch (a8a0d5c)
- **dashboard:** statistics tile reloads only on a new revision (871db1d)
- **dashboard:** statistics page reloads the current period on a new revision (67e35fb)
- **settings:** add the energy statistics recording mode (4490871)
- **energystats:** pause or stop own recording by policy with a short backlog (7d1fef7)
- **energystats:** navigation, Pi local time, categories and warnings in the stats API (d311552)
- **dashboard:** shared display model for energy statistics (a155d15)
- **dashboard:** energy statistics tile for today, week, month and year (dc47016)
- **dashboard:** statistics page with stacked bars, table and coverage (4aacfc7)
- **dashboard:** energy statistics recording mode and status in the history settings (8abfcad)
- **dashboard:** align the statistics page and tile with the history design (b7a14ef)
- **energystats:** store hours from Home Assistant and count them per day (c75cf9e)
- **energystats:** fetch hourly energy from the companion as a pseudo peer (31626d7)
- **dashboard:** exchange the 1h tier and route companion statistics to energystats (5986c21)

### Fixes

- **dashboard:** Help texts, device map, energy roles and statistics fit narrow screens, and export and battery charge are hatched in the statistics. (#144) (ba54b31)
- **devices:** The device map shows the right edge of its canvas again (#149) (5727ba1)
- **webui:** settings in card layout and aligned panel headings (#150) (cb9fa72)
- **webui:** align panel headings and tighten the overview toolbar (bb8844e)
- **devicemap:** keep the canvas right border inside the clipped stage (4278166)
- **devices:** let the device modal activity grow as a log (6fc8edd)
- **devicemap:** read sub-devices back from disk and keep the caller's slice (c32036e)
- **devicemap:** let stored sub-device pairs win over stale request pairs (51c2646)
- **devicemap:** keep sub-device placement moving and skip null entries (4d2561c)
- **devicemap:** match sub-device entities by unique_id (5193882)
- **devicemap:** use the absolute power of sub-devices in the meter edge (b55ff9e)
- **devicemap:** default sub-device channel and guard panel links (dd0ec58)
- **devices:** reset and tokenise sub-device load in the device modal (86f1e50)
- **devicemap:** serialise device-map writes and check sub-device entities (7a92b37)
- **devicemap:** hide the empty action box on sub-device edges (6fcb374)
- **devicemap:** skip the empty settings box on sub-device edges (a27a0d2)
- **devicemap:** keep the edge card chevron pointing right and update catalogs (b083235)
- **devicemap:** keep saved switch roles and edge channels on save (5f8edc8)
- **devices:** open the device modal only in the devices tab (d0d8031)
- **devicemap:** save the empty role of the panel select (2230e03)
- **devicemap:** require edit_energy and CSRF for every map write (1e2169b)
- **devicemap:** keep labels live and harden switch fade teardown (5b9ea98)
- **devicemap:** prefix the legend dotted class and use English spec references (51b6caa)
- **devicemap:** draw quiet node images, move the off pill and echo switch commands (af157a3)
- **dashboard:** keep favourite chips in pick order after saving (04b4d18)
- **dashboard:** hidden help tooltips no longer widen narrow pages (543c498)
- **devicemap:** closed side panel no longer widens the page (3d8fd4b)
- **energy:** role cards fit narrow screens (1e157cd)
- **dashboard:** show help tooltips as a bottom bar on narrow screens (0a1ff81)
- **stats:** thin out axis labels that do not fit the chart (8b23d2f)
- **stats:** keep legend swatches round (d0d1c82)
- **dashboard:** update bar names service steps (890c466)
- **automation:** read the state document through its own route (45bf28b)
- **runtimecache:** persist the state payload, not the availability heartbeat (ad4d48c)
- **dashboard:** anchor narrow-screen help tooltips under their trigger (e4cb788)
- **stats:** keep thinned axis labels when the chart renders hidden (5708884)
- **settings:** anchor the version help tooltip on narrow screens (1bbc768)
- **energy:** plain wording in the load mode texts (a04e740)
- **energydiscovery:** leave role empty for category sensors (a2f0f81)
- **dashboard:** keep announced sensors when settings cannot be loaded (5661b83)
- **energystats:** accept only consumer and producer categories from the companion (ec8339e)
- **dashboard:** refresh additional sensor choices and cover them with tests (ce6554a)
- **history:** fetch the minute tail for every watched tier (c72a53a)
- **history:** saved views use the coarsest resolution and failed hour fetches are not cached (e24f506)
- **dashboard:** skip the bridge config request for guests on the MQTT tab (726e39f)
- **dashboard:** align bridge verify step with the status labels (985ba39)
- **dashboard:** tighten the bridge setup step texts (dc0a550)
- **dashboard:** guest view of the MQTT tab hides the bridge steps and explains why (eee699c)
- **dashboard:** retry a tile structure change that arrives mid-request (3a78eca)
- **dashboard:** restore attribute spacing in base.html (7a52161)

### Refactors

- **dashboard:** remove unused Tippy.js and Popper.js (#139) (8f2318c)
- **stats:** share the bar chart options between page and card (fb7f4fd)
- **devices:** load the device modal lazily from its own file (2544335)
- **devicemap:** move switch outputs into energy roles and add edge channels (d2ab424)
- **devicemap:** draw the panel link icons from the icon pool (0134e7a)
- **energy:** drop measured load positions and the measured_split option (b927086)
- **dashboard:** drop the unused Tippy.js and Popper.js bundles (dfc102e)
- **dashboard:** reword the base.css tooltip comment (7856e87)
- **dashboard:** drop the global structure fingerprints (c71a352)
- **registryevents:** build and deliver SSE bodies per topic (dc7ddea)

### Documentation

- **versions:** correct the highlight crop comment (7b42687)
- move internal notes on automations and overview cards into the docs (#140) (4651b98)
- **layout:** note how a new card type gets live updates (d86cfad)
- **automations:** explain rule evaluation and document guest roles (1c2c2ab)
- **dashboard:** document every overview card with screenshots (7cb493b)
- **energy:** explain balance gap, tolerance and key figures (e6d8d6b)
- **devices:** document the device dialog with screenshots (3350952)
- **dashboard:** document the layout editor and keep its pitfalls internal (7fd15b6)
- **mqtt:** describe bridge failure handling and the helper trust boundary (11000ac)
- **tailscale:** point the package comment at the install guide (3e9495c)
- **devicemap:** add a sub-device screenshot (b8806a4)
- **dashboard:** refresh the screenshots with English names and add a statistics image (c772066)
- **energy:** describe hiding entries in the role table (294634a)
- **devicemap:** document the switch role and retake screenshots (7dcb363)
- **energy:** document the reworked load modes and status line (4469508)
- **dashboard:** drop Tippy.js and Popper.js from notices, manifest and docs (4761345)
- **dashboard:** MQTT tab metrics select and bridge step list (81d3985)
- **release:** document the per-version changelog (865f72a)

### Tests

- **smoke:** real changelog preset and card shots for the MQTT tab (2fa378e)
- **smoke:** add a Waschmaschine sub-device to the device map fixture (ea31cad)
- **smoke:** simulate heat pump, dehumidifier and cellar relay consumption (d0238d2)
- **smoke:** add a two channel relay fixture and bump the lazy assets (57e029a)
- **devicemap:** add switch fixtures and screenshot (baf7e2a)
- **devicemap:** wire the balcony plug in front of its own inverter (4215284)
- **smoke:** bind the bank A edge probe to the preset that seeds it (ef36c7f)
- **automation:** cover the state route on the automations page (95552d8)
- **smoke:** overflow probe checks settings subpages and open tooltips (ca9e7c6)
- **dashboard:** cover the compact card row and row template attributes (8c52a25)
- **smoke:** fake companion that feeds the energy statistics (b41a808)
- **smoke:** expect the 1h tier in the exchange announcement (b9ddd86)

### Style

- **settings:** lay out the system tab as setting cards (ed93af2)
- **settings:** drop the Tailscale and TinyTuya tab titles (e87d2cb)

### Chores

- **webui:** bump cache-bust versions of the changed statistics assets (be49e3e)
- **webui:** bump cache-busting versions for sub-devices (3de3b95)
- **i18n:** drop the unused switch output texts (99b1aeb)
- **webui:** bump lazy assets for switch state (bc7a8b6)
- **dashboard:** bump cache-busting versions for changed assets (a5c7064)
- **dashboard:** bump settings-controls.css for the version help tooltip (b110b3a)
- **dashboard:** bump asset versions and document the devices topic (099cf25)
- **dashboard:** bump asset versions and document the energy_stats topic (6f01151)

## v0.9.8 (2026-10-12)

### Features

- **dashboard:** show update progress and what's new after an update (#123) (7c1388f)
- **dashboard:** show exchange and history status as tiles (#125) (1013cd2)
- **devicemap:** merge the balance node into the energy_node device (#126) (67870d1)
- **devicemap:** hang the automation device under the node and link its rules (#127) (c4c2401)
- **dashboard:** hourly energy statistics recorded on the server (#128) (b18f85e)
- **devicemap:** connection types with flow direction and reversible manual edges (#129) (e36ecaf)
- **devicemap:** store a connection type and flow direction per edge (727b779)
- **devicemap:** reverse a manual relation in place (484c889)
- **devicemap:** resolve connection type and flow direction per edge (4a4b69c)
- **devicemap:** draw connection types and run flows over energy edges only (49794ea)
- **devicemap:** compute group values over energy edges with their direction (1c18aaf)
- **devicemap:** set connection type and direction and reverse manual edges on the map page (6a1bea2)
- **devicemap:** add the connection type and direction controls to the edge card (96a97b0)
- **devicemap:** polish the reverse button, reorder the layer buttons and add a width icon (70a0752)
- **devicemap:** rename the balance layer to roles and move balance conditions to the data layer (39d962a)
- **devicemap:** calm down the wiring edge card (922d9a2)
- **energystats:** hourly month model with source precedence (4efb3e7)
- **energystats:** integrate power samples into hourly kWh (ded870b)
- **energystats:** persist months atomically and survive corrupt files (2acdbd2)
- **energystats:** record balance snapshots with clock and staleness guards (8872b93)
- **energystats:** sum hours into local days, months and years (e64d3f4)
- **dashboard:** record energy statistics and serve them at /api/v1/energy/stats (b444540)
- **energystats:** add a week period to the statistics query (803d5b1)
- **devicemap:** link the automation device to the rules it runs (11d6c51)
- **devicemap:** place rules below the automation device (c61106b)
- **devicemap:** show the automation device under the energy node in the example (ad10903)
- **devicemap:** toggle the lines to the automation node and default to curved arrows (b463be2)
- **devicemap:** resolve the balance node to the energy_node device (fbc32dc)
- **devicemap:** draw energy_node as the balance node on the map (b167ee7)
- **devicemap:** draw energy_node as the balance node on the plant view (993e14e)
- **dashboard:** show the exchange status as a tile that updates itself (7e631b6)
- **dashboard:** show the history status as a strip with pills and a calm empty state (d8d35cf)
- **dashboard:** add a per-device history exchange mode, on demand by default (d55c003)
- **dashboard:** choose the history exchange mode per device in the settings (a997f48)
- **dashboard:** let the history exchange plan only a given range (87fde60)
- **dashboard:** request only the shown history range in on-demand mode (95f6b9c)
- **dashboard:** route shown history ranges and deliveries between tabs (17cea2a)
- **dashboard:** register the shown range of the history page and tiles (f275f2a)
- **dashboard:** let a device in on-demand mode still provide history to others (86faea4)
- **dashboard:** always provide history in always mode and hide the switch there (b20c4f2)
- **updater:** keep the previously installed manifest on a version change (45d303d)
- **dashboard:** serve the highlights of the last update (b8e7a3d)
- **dashboard:** summarise the updater job for an update progress view (29af202)
- **dashboard:** add an update progress endpoint (c8352b0)
- **dashboard:** show update availability and progress in a bar under the masthead (0a68dd4)
- **dashboard:** add a what's new page for the last update (dc30326)
- **dashboard:** open the what's new button in a modal dialog (fc92781)
- **dashboard:** fly the what's new dialog in and open it from the versions page (aca54b4)
- **dashboard:** show the what's new page in the modal dialog (b75c89c)
- **dashboard:** show the what's new dialog as a sheet with one tile per component (b4c8a08)

### Fixes

- **dashboard:** serve the automation history in full (#119) (1e04a2a)
- **devicemap:** hint why a feed into a group cannot be reversed and bump cache versions (5303872)
- **energystats:** check the error of the future-period query test (75c038a)
- **devicemap:** place balance-fed data nodes by energy_node position (07906f9)
- **dashboard:** address final review of the update bar and what's new page (b3ab4d3)

### Performance

- **dashboard:** exchange history only on demand (#124) (fddb2b4)
- **dashboard:** count history coverage per bucket instead of walking every key (eca1e74)
- **dashboard:** cache closed history coverage buckets per tab (9f5ca98)

### Documentation

- **devicemap:** describe fixed direction and reversing, retake the device map screenshots (0581688)
- **devicemap:** describe energy_node as the balance node (c8fc1fd)
- **dashboard:** describe the on-demand history exchange and bump asset versions (abc7908)
- **dashboard:** refresh the history settings screenshot for the exchange mode (7d47494)

### Tests

- **smoke:** relay client publishes in the minibroker so energy_node shows up (ff720a4)
- **smoke:** hang four example devices under the energy node via via_device (52dda83)
- **dashboard:** wait for the offer instead of a fixed delay in the exchange tests (706f5cd)
- **dashboard:** cover update progress and highlights in the smoke test (beacefa)

## v0.9.0 (2026-10-10)

### Features

- **installer:** check and install system package updates (opt-out) (#90) (d898a63)
- **dashboard:** show the config service status as a pill (#98) (d639c36)
- **dashboard:** add history view tiles to the overview layout (#99) (4c2ee2a)
- **dashboard:** redesign the diagnostics tab (#101) (b01e84e)
- **dashboard:** device map layers, role rings and focus (#103) (ed0959e)
- **dashboard:** live energy flow on the device map (#105) (2873ccd)
- **dashboard:** edit energy roles on the device map (#107) (0f1a4dc)
- **dashboard:** energy groups and custom categories (#108) (3facc40)
- **dashboard:** data flow and balance layers on the device map (#109) (bbe604b)
- **dashboard:** live plant view on the energy tab (#110) (a3951fd)
- **dashboard:** polish the device map and the energy tab (#111) (55becd6)
- **dashboard:** reorganise the device icon catalogue (#114) (711e805)
- custom energy categories in automations and editor polish (#115) (38776fb)
- **dashboard:** add texts for custom categories in automations (ffe375e)
- **dashboard:** add error texts for the category references endpoint (f94b7b6)
- **dashboard:** validate schema patterns and list rules using a category (602007b)
- **dashboard:** offer custom categories as automation balance fields (63b52d0)
- **dashboard:** warn before removing a category used by automations (22824b9)
- **dashboard:** add the unsaved badge text for automation rules (5daf346)
- **dashboard:** open the advanced blocks of automations with a collapse animation (83aeef7)
- **dashboard:** add device icon name aliases (446f3ae)
- **dashboard:** migrate saved device icon names to energy-node names (6ab39de)
- **dashboard:** add shared shapes for device icons (6eb7d98)
- **dashboard:** add device icon labels and categories (e0ab443)
- **dashboard:** reorganise the device icon catalogue with energy-node names (7f24eb2)
- **ha:** add MDI names and aliases to the icon source (11a6f5f)
- **dashboard:** group the device icon picker by category (636806e)
- **dashboard:** draw device map role icons from the catalogue (379c5c3)
- **dashboard:** show a minimal fill level in the battery icon (8f9c311)
- **dashboard:** move the automatic update check switch to the versions page (2aaca8c)
- **dashboard:** show the update check explanation as a tooltip (7b4a38f)
- **dashboard:** polish the device map toolbar, cards and group editing (82c7dc2)
- **dashboard:** two-column group cards and even spacing on the energy page (bd8896b)
- **dashboard:** energy settings accordion and group role switch (987be61)
- **dashboard:** clearer create group dialog on the device map (11f862b)
- **dashboard:** add the read-only plant view component (0871c27)
- **dashboard:** show the live plant view on the energy tab (689053c)
- **services:** declare data flow of topic fields with x-dataflow (dfa0420)
- **dashboard:** expose template json key and active schema properties (f3b96ff)
- **dashboard:** add dataflow package with topic index (b20f0ab)
- **dashboard:** derive service inputs from x-dataflow annotations (5565afc)
- **dashboard:** derive automation rule data flow (aaef07a)
- **dashboard:** serve the derived data flow on /api/v1/device/map/flows (6a1982e)
- **dashboard:** add pure data flow logic for the device map (16086e1)
- **dashboard:** draw balance, rule, service and stub nodes (99dea07)
- **dashboard:** add data flow elements and layer switches to the device map graph (4e5f428)
- **dashboard:** add device map data flow hint texts (741ee4e)
- **dashboard:** show the energy balance and data flow layers on the device map (fb509d5)
- **dashboard:** add data flow texts to the catalogs (ebe2dc3)
- **dashboard:** open configuration entries and rules from the device map (f6f9b45)
- **dashboard:** add data flow popover, panel section and legend to the device map (9a5cb31)
- **dashboard:** add energy categories and groups to the energy model (a66ef13)
- **dashboard:** aggregate custom categories and energy groups (9080adc)
- **dashboard:** balance custom categories by their base in Go and JS (7df230b)
- **dashboard:** record custom energy categories in the history (fddcedc)
- **dashboard:** store energy categories and groups with validation (c9042c7)
- **dashboard:** edit categories and groups through the energy roles endpoint (dd7e9a2)
- **dashboard:** device map version 2 with virtual group nodes and group feeds (27e7e7e)
- **dashboard:** add categories, groups and placement to the device map model (2ad208f)
- **dashboard:** draw group nodes and category icons on the device map (2f08bea)
- **dashboard:** place group nodes and connect memberships on the device map (5b7ab4e)
- **dashboard:** add the group panel, category dialog and legend to the device map (c52fd69)
- **dashboard:** manage energy groups and custom categories on the energy page (1abb645)
- **dashboard:** add the edit_energy role for guests and admins (1571966)
- **dashboard:** add an atomic energy config patch to the settings store (6cdac57)
- **dashboard:** gate energy writes behind edit_energy and add a merging PATCH (0e5836f)
- **dashboard:** send CSRF on energy writes and let other tabs focus role rows (05f1c3f)
- **dashboard:** add role panel rows and draft preview to the device map model (6bd0d74)
- **dashboard:** open a role panel on device tap with a guarded close (875a646)
- **dashboard:** add the device map role panel markup and styles (5c323ae)
- **dashboard:** save panel roles by PATCH and jump to the role table (6c76e5b)
- **dashboard:** compute energy flow per device map edge (a08bcdb)
- **dashboard:** add the width by power switch to the device map (2ccbb14)
- **dashboard:** draw live energy flows along device map wiring (e0a7187)
- **dashboard:** animate device map energy flows (c629d25)
- **dashboard:** add the energy legend and document live flows (9410ab3)
- **dashboard:** store device map layers and flow width in the view (870b8dd)
- **dashboard:** add the device map energy model (75faffc)
- **dashboard:** draw device map nodes as role rings (258c3f5)
- **dashboard:** add an html label layer for device map nodes (f1aa809)
- **dashboard:** show energy roles and live values on device map nodes (1e1673d)
- **dashboard:** add switchable layers to the device map (cf0ad4d)
- **dashboard:** focus related devices and refresh live values on the device map (45d1c89)
- **dashboard:** add the history_view layout item type (32d4476)
- **dashboard:** render history_view tiles on the overview (bfd09b7)
- **dashboard:** add shared history chart module (2a1738f)
- **dashboard:** draw history view tiles with a preserved ApexCharts instance (a7fde99)
- **dashboard:** place and configure history view tiles in the layout editor (5686c7d)
- **dashboard:** show the config service status as a pill and move the revision into the revision panel (52b235d)
- **installer:** carry pending system updates into the preview and diagnose (6ad251f)
- **installer:** check system updates on request and refresh them in the diagnose (b00f687)

### Fixes

- **dashboard:** make saving the system configuration work and report it properly (#91) (0c7d266)
- **installer:** skip unchanged update steps and keep the session across the restart (#102) (726ad05)
- **dashboard:** settings polish and local docs serve (#112) (1c7e1d3)
- **dashboard:** select a saved custom category in the rule editor (7d177ff)
- **dashboard:** close the rule editor on collapse and mark unsaved rules (3e7340e)
- **dashboard:** accept legacy category icons and update icon name expectations (fd9086d)
- **dashboard:** keep the segmented control thumb inside its frame (9b5ed56)
- **dashboard:** drop the arrow instead of spinning the update check icon (3ed48de)
- **dashboard:** give the setup help notes their own footer (7806394)
- **dashboard:** stop the category control from overriding the .segmented slider (18ca390)
- **dashboard:** drop the hint text next to the assigned chip (79d8583)
- **dashboard:** mobile device map sheet stays at the bottom and scrolls (216e695)
- **dashboard:** keep the device map and the energy tab in sync (4181bb0)
- **dashboard:** keep the device map panel tooltips inside the panel (7a0252d)
- **dashboard:** never save unplaced balance or data flow nodes (16d55fe)
- **dashboard:** show custom category names on the history page (8d2d1e1)
- **dashboard:** sort the new catalog keys (51ca4c9)
- **dashboard:** show the legend only for energy or data layers and name virtual focus nodes (26d65ad)
- **dashboard:** keep sessions across a dashboard restart (db7eefd)
- **installer:** skip unchanged steps on an update (446e9d2)
- **dashboard:** keep the fixed date range of a saved history view (1c5ccb1)
- **dashboard:** keep the overview swap-free with history view tiles (802f50c)
- **dashboard:** rename the history range catalog keys (b0a90ca)
- **dashboard:** comment out the verlauf-kachel preset description in the smoke script (037e9f1)
- **dashboard:** stop the history tile tooltip from causing scrollbars and flicker (27dcdcb)
- **dashboard:** save the system config through the privileged helper (478744f)
- **dashboard:** report system config save results as toasts (4e6e8fb)
- **dashboard:** translate the secret path rejection message (a03eee6)
- **installer:** harden step 15 and select it on dashboard updates (bab88e1)

### Refactors

- **dashboard:** draw sprite glyphs from the icon catalogue (3f9ba11)
- **dashboard:** reuse sprite symbols for inline icons (3f323cd)
- **dashboard:** share the device map flow animator (2b32891)
- **dashboard:** move device map graph building into devicemap-graph.js (d34895a)
- **dashboard:** move automation description helpers into automations-view.js (2af6a5a)
- **dashboard:** wire history.js to use the shared chart module (b7a2f0d)
- **dashboard:** share app config staging between migration and handler (073acf3)

### Documentation

- redesign the documentation site with an Energy Node layout (#94) (5a7080f)
- describe custom categories in automations, bump lazy assets (041dd6d)
- describe the reorganised device icon catalogue (2144a4c)
- **dashboard:** merge the device map into the energy page (b9bee03)
- **dashboard:** describe the plant view on the energy tab (ccc8f3e)
- **dashboard:** describe the balance and data flow layers (082591c)
- **dashboard:** describe the device map role panel (9817599)
- **dashboard:** describe the history tile and bump its cache busting (06a1194)
- point the repository at the new pages and add site search (72fa46e)

### Tests

- **dashboard:** follow the lazy asset version bump (f6a7a81)
- **dashboard:** add data flow and balance fixtures to the smoke preset (1d65b7e)
- **dashboard:** add a group and a custom category to the smoke fixture (fa84895)
- **dashboard:** give the smoke preset's charger power sensors a unit (12b9f28)
- **dashboard:** smoke-check that history view tiles survive the live swap (09dbc4c)
- **dashboard:** make the history tile smoke check rerunnable (e7229e1)
- **dashboard:** simulate service status in the local smoke test (78e15c8)

### Style

- **dashboard:** gofmt settings test (cf9b0c1)

### Chores

- **dashboard:** bump automations assets for the editor changes (fe1f569)

## v0.8.5 (2026-10-04)

### Features

- The changelog pages open on the highlights, the switch at the top shows everything. (#84) (5cc1cfb)
- **homeassistant:** add Energy Node Companion as a history exchange peer (#85) (5bd7f48)
- **dashboard:** list the recorded series in the history exchange announcement (58c59a2)
- **dashboard:** relay an optional sender label with history exchange offers (e4dc868)
- **dashboard:** name the source of exchanged history rows in the settings (6a5d816)
- **dashboard:** link the HA device to the node's Tailscale address (7c6d6ab)
- **dashboard:** switch the versions page between highlights and everything (25b7c01)

### Documentation

- **dashboard:** use matching German quotes for the device link note (7f5a20f)
- describe the Home Assistant history exchange peer (793aad8)
- highlights and details, screenshot script for the installer (105e72e)

### Tests

- **dashboard:** follow the settings and history exchange cache-bust (979e317)
- **dashboard:** smoke fixtures carry highlight flags (0fadf49)

## v0.8.3 (2026-10-01)

### Features

- **battery_soc:** save the state on an interval and recover after a crash (#78) (dc62037)
- ship changelog.json in the bundle and show what is new before an update (#82) (d862195)
- **updater:** record changelog.json next to the installed manifest (a823aa6)
- **dashboard:** lead the update pill to the update screen for users who may update (70fd1b0)
- **battery_soc:** add the save interval setting to the service schema (df73211)

### Fixes

- **dashboard:** rest the battery flow icon on its fill level at 0 W (#81) (295ab6a)
- **dashboard:** report the installed version as from in the redeploy preview (adcdc18)

### Tests

- **dashboard:** cover /redeploy/api/changelog through the real handler chain (4977691)
- **dashboard:** ship a changelog in the simulated package so the smoke run shows what is new (9829d3b)

## v0.8.0 (2026-09-29)

### Features

- **dashboard:** per-device icons and pinned favourite entities (#53) (d649df3)
- **homeassistant:** ship the dashboard's device icons as an HA icon set (#54) (c4c196b)
- **battery_soc:** DC-only systems in the MQTT service and conditional config forms (#58) (89bacae)
- **dashboard:** localization foundation with language switcher (#59) (7f8ed9f)
- **dashboard:** number format setting and locale-aware dates (localization A2) (#60) (6d8ba8b)
- **dashboard:** localize the shell and the overview (#62) (e96f396)
- **automation:** weekdays in the editor and a sun_window condition (#63) (b32936a)
- **dashboard:** localize energy and settings (#64) (5445f5b)
- **dashboard:** localize configuration and the layout editor (#67) (9609a2d)
- **dashboard:** localize automations and history, guard every UI text (#68) (cce3f4f)
- **dashboard:** localize API error messages (#69) (368214a)
- **dashboard:** localize validation errors and diagnostics (#70) (9827111)
- **dashboard:** show installed component versions under settings (#71) (c8ef59b)
- **dashboard:** localize the system configuration form (#72) (c716d05)
- **dashboard:** localize the device configuration forms (#73) (180110d)
- **dashboard:** show the redeploy screen in the dashboard language (#74) (6542022)
- **dashboard:** write the central schema in English and move German to the catalog (bd26f60)
- **dashboard:** describe every field of the system configuration form (012ea1c)
- **dashboard:** add a versions package that reads what the installer left on the node (0dee5e2)
- **dashboard:** add read-only /api/v1/versions and /api/v1/changelog (637efd3)
- **dashboard:** add the versionsPanel component and its texts (56c0ffa)
- **dashboard:** add a versions page under Einstellungen (d2c90cf)
- **dashboard:** drop the dashboard version from the System panel (5d446fb)
- **dashboard:** drop the link to the Versionen tab from the System panel (be8b2ee)
- **dashboard:** move the update check into its own card on the Versionen page (643fa05)
- **dashboard:** catalog keys for diagnostics warnings (f412fb4)
- **dashboard:** translatable fields in API error responses (b1afd64)
- **config:** deliver catalog keys for configuration display names (a123577)
- **storagehealth:** return catalog keys next to the German labels (8b8b464)
- **settings:** return a catalog key for the bridge address warning (10d63fe)
- **energy:** return catalog keys for role labels and sign hints (a0e4fd6)
- **settings:** add number format and digit grouping settings (2c67fd6)
- **webui:** show live device values in the configured number format (f23ebf5)
- **history:** localize chart labels, tooltips and the date range picker (dcde8a1)
- **localize:** resolve the UI language from cookie and Accept-Language (1835383)
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

- **dashboard:** name the signed battery and grid energy roles (99a5cb4)
- **dashboard:** name history series instead of showing their ids (84753a7)
- **dashboard:** pass service fields and previous versions to the redeploy preview (#51) (0c07bc1)
- **redeploy:** keep following a run across the dashboard's self-update restart (#52) (38a0609)
- **dashboard:** tidy localization texts after the final review (c2b5853)
- **dashboard:** drop two unused catalog keys (b1e01ca)
- **dashboard:** keep plural-reserved .other off the versions catalog keys (e4e2b09)
- **dashboard:** wrap version rows on narrow screens and localize release dates (036591f)
- **config:** name Trucki devices as IoT sticks (2a16239)
- **dashboard:** hide conditional schema fields despite the grid display rule (e3225b3)
- **dashboard:** start the status watch after the editor save reloads the config (11911c0)
- **dashboard:** tolerate fields of inactive schema branches from older files (06071f4)
- **battery_soc:** show the imbalance threshold only for two banks in series (f402372)
- **dashboard:** keep the exported icon markup readable in diffs (dc5114a)
- **dashboard:** keep the pinned-favourites block from breaking the modal grid (c0876ee)

### Refactors

- **dashboard:** stop reading the services VERSION file on the node (c1e19de)
- **dashboard:** report passed-through API errors as detail (aa941d5)

### Documentation

- **dashboard:** final review of the localization (#75) (ea05c63)
- **dashboard:** English screenshots and docs for the bilingual UI (85caf1c)
- **dashboard:** describe config_revision as the last load attempt (715dd30)
- **battery_soc:** document DC-only systems, signed inputs and current units (6c2df1f)
- **dashboard:** document the device preference endpoints and bump the assets (aed1eee)

### Tests

- **dashboard:** add --simulate-installed to the smoke test and check the versions API with it (25c06ff)
- **dashboard:** fix cross-realm deepEqual in the new prefsDraft test (02d2ef3)

### Style

- **dashboard:** gofmt the device-prefs struct fields (19d915e)
- **dashboard:** draw device icons larger inside their frame (86f909c)

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

## v0.7.0 (2026-09-21)

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
- **installer:** hide dashboard tabs for deselected optional services (#31) (59f2d40)
- **installer:** add the dashboard's local self-update path (Plan D) (#33) (e50d236)

### Fixes

- **auth:** backfill RoleCheckUpdates for an already-bootstrapped admin (#38) (58d94f3)
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

## v0.6.5 (2026-09-13)

### Features

- **dashboard:** replace theme select with a four-way segmented slider (#23) (3855e9c)
- **dashboard:** redesign the MQTT settings tab (#24) (8844555)
- **dashboard:** restyle MQTT settings tab to match card layout (2d5f2bf)
- **dashboard:** fill row gaps and add floating save bars on MQTT tab (a389ce4)

### Fixes

- **dashboard:** keep MQTT settings steppers from overflowing narrow cards (#27) (79ba6ee)
- **dashboard:** update webui tests for the bumped CSS cache-bust versions (49355fb)
- **dashboard:** fix MQTT tab reflow, add steppers and tooltips (927e10f)

## v0.6.2 (2026-09-11)

### Features

- **dashboard:** migrate config.json schema_version 1 to 2 on load (#20) (b8f72d1)
- **dashboard:** replace theme select with a four-way segmented slider (465c4af)
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

- **dashboard:** finish the config.json v1 -> v2 migration path (#21) (9d4fb53)
- **nodeagent:** address task-6 review findings (ee8e3c4)
- **nodeagent:** correct throttle bits, guard node id, unify device block (92bd8fa)
- **dashboard:** style the mqtt metric-toggle fieldset and refresh stale docs (0b3e4ec)

### Refactors

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

## v0.6.0 (2026-09-10)

### Features

- **⚠ Breaking:** move node telemetry into the dashboard nodeagent (#19) (8049117)
- **dashboard:** serve automation notifications from a dedicated endpoint (#6) (58bd7a6)
- make device services self-describing with per-service manifests (#12) (2e8c911)
- **⚠ Breaking:** fold the config.json node block into dashboard.node_* (schema_version 2) (#14) (e7f4ba1)
- **dashboard:** rebuild the history settings tab (#16) (5a41c5c)
- **dashboard:** rebuild the settings tabs as responsive material cards (#18) (d905f8b)

### Fixes

- **dashboard:** show system-config revisions as a read-only list (#7) (3e7dee7)

### Refactors

- split src/ into services/ and libs/, rename service-level device_id to service_id (#10) (75abe73)
- remove git hooks, and stop the per-PR CHANGELOG stacking (#13) (c643260)

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

