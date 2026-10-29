---
title: "Lazy-loaded JS/CSS assets and their `?v=` versioning"
---

# Lazy-loaded JS/CSS assets and their `?v=` versioning

The dashboard embeds its entire frontend locally (`go:embed` in
[webui.go](../../../dashboard/internal/webui/webui.go), no CDN dependencies) and
serves every file under `/static/` with

```go
w.Header().Set("Cache-Control", "public, max-age=86400")
```

— see `Static()` in
[webui.go:572](../../../dashboard/internal/webui/webui.go#L572). There is **no
content hashing in the filename** and no `immutable`. A browser (and any proxy
in front of it, e.g. Caddy) therefore treats a loaded `history.js` as valid for
up to a day, even though the deployment replaced the file long ago.

The only lever against this is a **manually maintained query string** `?v=N` on
the `src`/`href`. When `N` changes it is a new URL as far as the cache is
concerned and gets reloaded. Almost all of these query strings live centrally in
[base.html](../../../dashboard/internal/webui/templates/base.html). **Exception:**
the overview's editor assets, which are declared on the overview panel (see below).

The overview's editor assets (`js/layout-editor.js`, `css/layout-editor.css`)
are declared in `data-editor-script` / `data-editor-css` on `#overview-panel` in
[overview.html](../../../dashboard/internal/webui/templates/overview.html) and carry
their `?v=` there. The remaining panel fragments reference no versioned assets.

For the **lazy-loaded panel assets** there is a second reason:
`loadSingleAsset()` in
[dashboard.js](../../../dashboard/internal/webui/static/js/dashboard.js#L115)
deduplicates via `this.loadedScripts` / `this.loadedStyles` — sets keyed on the
**exact URL string** (including `?v=`). As long as the URL stays the same, the
asset counts as "already loaded" within a session.

---

## The three loading channels in `base.html`

1. **`<script defer>` at the end of `<body>`** (lines 154–181). Runs on every
   full page build after parsing, in document order. Not "lazy per panel", but
   deferred. Some carry `?v=`.
2. **`data-panel-script` / `data-panel-css`** on the `<section class="panel">`
   (lines 103–114). Truly lazy: `dashboard.js` `loadPanel()` fetches them only
   when the panel is activated for the first time. Comma-separated list, loaded
   **sequentially** (dependent scripts such as flatpickr before its German locale).
3. **`EnergyCardScripts`** — the seven energy-card scripts (the six energy-chart
   variants plus `battery-status.js`) plus `energy-model.js` and
   `battery-card-core.js`, which `webui.go` (`energyCardScripts`,
   [webui.go:361](../../../dashboard/internal/webui/webui.go#L361)) injects
   conditionally as `<script defer>` depending on the configured card types.
   `battery-card-core.js` sits in `base.html` immediately before the
   `{{range .EnergyCardScripts}}` loop, in the same `{{if .EnergyCardScripts}}`
   block as `energy-model.js` — deliberately: it is the host-independent core of
   the battery card and would otherwise load on every page, even without a
   `battery_status` tile in the layout.

---

## Current version state

As of **2026-10-25**, read from `base.html` and `overview.html`. "–" means: no
`?v=`, relies solely on the 1-day cache.

### Global

| Asset | `?v=` |
|---|---|
| `css/base.css` | `45` |
| `js/i18n.js` | `4` (`login.html` still loads `3`) |

### Deferred `<script>` block (channel 1)

| Asset | `?v=` |
|---|---|
| `js/notify.js` | `2` |
| `js/toast-gesture.js` | `1` (new, swipe and pause of the toasts) |
| `js/theme.js` | – |
| `js/layout-fill.js` | – |
| `js/device-tile.js` | `2` |
| `js/entity-values.js` | `1` |
| `js/overview-values.js` | `3` |
| `js/device-tile-values.js` | `1` |
| `js/compact-card-values.js` | `1` |
| `js/device-structure.js` | `1` (new, device tiles) |
| `js/energy-presentation.js` | `2` |
| `js/energy-flow.js` | `3` |
| `js/history-rollup.js` | `3` |
| `js/history-store.js` | `5` |
| `js/history-coverage.js` | `4` |
| `js/history-exchange.js` | `9` |
| `js/history-maintenance.js` | `1` |
| `js/history-recorder.js` | `13` |
| `js/notifications.js` | `2` |
| `js/dashboard.js` | `39` |
| `js/update-bar.js` | `5` |
| `js/whatsnew.page.js` | `4` (deferred script block, no longer in the settings-panel row) |
| `js/overview.page.js` | `6` |
| `js-deps/htmx.min.js` | – |
| `js-deps/alpine-collapse.min.js` | – |
| `js-deps/alpine.min.js` | – |

### Conditional energy-card scripts (channel 3)

| Asset | `?v=` |
|---|---|
| `js/energy-model.js` | `4` |
| `js/battery-card-core.js` | `4` |
| `js/battery-status.js` | `2` |
| `js/energy-day.js` | `3` |
| `js/energy-band.js`, `js/energy-ring.js`, `js/energy-board.js`, `js/energy-schema.js` | `2` |
| `js/energy-status.js` | `1` |
| `js/energy-stats-model.js` | `4` |
| `js/energy-stats-card.js` | `4` |
| `js/history-chart.js` | `5` |
| `js/history-view-card.js` | `2` |

Card scripts in `energyCardScripts` (`webui.go`) carry their own `?v=`. A
script shared with a panel (`history-chart.js`, `apexcharts.min.js`) must use
the identical URL string in both places, because `loadSingleAsset` now also
skips scripts already present as `<script src>`.

### Lazy panel assets (channel 2)

| Panel | Scripts (`?v=`) | CSS (`?v=`) |
|---|---|---|
| `devices-panel` | – | – |

`js/device-modal.js` `1` is lazy on its own trigger: `devicesPanel` loads it on
the first device open (overview and devices tab). Its URL sits in `data-script`
on the `device-modal` template in
[devices.html](../../dashboard/internal/webui/templates/devices.html), not in
`base.html`.

| Panel | Scripts (`?v=`) | CSS (`?v=`) |
|---|---|---|
| `history-panel` | `js-deps/apexcharts.min.js` –, `js-deps/flatpickr.min.js` `1`, `js-deps/flatpickr-l10n-de.js` `1`, `js/history-export.js` `2`, `js/energy-model.js` `4`, `js/history.js` `21` | `css/flatpickr.min.css` `1`, `css/flatpickr.css` `1`, `css/history.css` `7` |
| `diagnostics-panel` | `js/servicelogs.panel.js` `2` | `css/settings-controls.css` `20`, `css/diagnostics.css` `3` |
| `config-panel` | `js/revisions.js` `4`, `js/schema-form.js` `2`, `js/config-status.js` `4`, `js/config.page.js` `10` | `css/manager.css` `48` |
| `energy-panel` | `js-deps/cytoscape.min.js` –, `js/revisions.js` `4`, `js/devicemap-model.js` `14`, `js/devicemap-subdevices.js` `1`, `js/devicemap-graph.js` `9`, `js/devicemap-wiring.js` `3`, `js/devicemap-switch.js` `2`, `js/devicemap-dataflow.js` `5`, `js/devicemap-node-svg.js` `7`, `js/devicemap-labels.js` `4`, `js/energy-plant.js` `6`, `js/device-picker.js` `2`, `js/energy.page.js` `13` | `css/manager.css` `48` |
| `devicemap-panel` | `js-deps/cytoscape.min.js` –, `js/revisions.js` `4`, `js/devicemap-model.js` `14`, `js/devicemap-subdevices.js` `1`, `js/devicemap-node-svg.js` `7`, `js/devicemap-labels.js` `4`, `js/devicemap-graph.js` `9`, `js/devicemap-wiring.js` `3`, `js/devicemap-switch.js` `2`, `js/devicemap-dataflow.js` `5`, `js/automations-view.js` `5`, `js/device-picker.js` `2`, `js/devicemap-view-prefs.js` `1` (new), `js/devicemap.page.js` `29` | `css/manager.css` `48` |
| `settings-panel` | `js-deps/choices.min.js` –, `js/revisions.js` `4`, `js/schema-form.js` `2`, `js/settings-masonry.js` `2`, `js/settings.page.js` `29`, `js/mqtt.page.js` `10`, `js/tailscale.page.js` `5`, `js/versions.page.js` `9`, `js/systemconfig.page.js` `5`, `js/users-model.js` `2`, `js/users.page.js` `3` | `css/choices.min.css` –, `css/choices.css` `2`, `css/manager.css` `48`, `css/settings-controls.css` `20` |
| `automations-panel` | `js/config-status.js` `4`, `js/automations-view.js` `5`, `js/automations-scenario.js` `3`, `js/automations-chart.js` `2`, `js/automations-devices.js` `1`, `js/device-picker.js` `2`, `js/automation-wizard.js` `1` (new), `js/automation-templates.js` `1` (new), `js/automations.page.js` `22` | `css/manager.css` `48`, `css/automations.css` `12` |
| `stats-panel` | `js-deps/apexcharts.min.js` –, `js/energy-model.js` `4`, `js/energy-stats-model.js` `4`, `js/stats.page.js` `5` | `css/history.css` `7`, `css/stats.css` `3` |

The former `layout-panel` is gone (the "layout edit mode" work): the layout
editor is now an edit mode of the overview, and its assets load through their own
channel (see below).

### Overview editor assets (`data-editor-*` on `#overview-panel`)

Loaded by `overview.page.js` `editorAssetsReady()` on the first switch into edit
mode. Declared in `data-editor-script` / `data-editor-css` on `#overview-panel`
in [overview.html](../../../dashboard/internal/webui/templates/overview.html) — the
only place outside `base.html` with versioned assets.

| Channel | Assets (`?v=`) |
|---|---|
| `data-editor-script` | `js-deps/choices.min.js` –, `js/revisions.js` `4`, `js/layout-editor.js` `19` |
| `data-editor-css` | `css/choices.min.css` –, `css/choices.css` `1`, `css/layout-editor.css` `9` |

### Assets referenced from multiple places

On a bump these must be raised **in every reference at once**, otherwise one
place pulls the old file from the cache:

- `css/manager.css` – 5 panels (`config`, `energy`, `devicemap`, `settings`, `automations`)
- `js/devicemap-subdevices.js` – `energy-panel` and `devicemap-panel`
- `js/device-picker.js` – `energy-panel`, `devicemap-panel` and `automations-panel`
- `js/devicemap-graph.js`, `js/devicemap-wiring.js` – `energy-panel` and `devicemap-panel`
- `js/energy-model.js` – channel 2 (`history-panel`), `stats-panel` **and** channel 3 (energy cards)
- `js/energy-stats-model.js` – `stats-panel` **and** channel 3 (energy cards)
- `js/revisions.js`, `js/schema-form.js` – several manager panels and the overview editor assets
- `js-deps/choices.min.js` – overview editor assets, `settings-panel` and `devices-panel` (device modal)
- `css/settings-controls.css` – `settings-panel` and `diagnostics-panel`
- `css/choices.css` – overview editor assets, `settings-panel` and `devices-panel` (device modal) (reconciled
  2026-09-11: both now carry a `?v=`, previously only `settings-panel` did)

---

## Conventions

- **The counter is an integer** and is incremented by exactly 1.
  `js/dashboard.js?v=v4` (cleaned up to `5` on 2026-09-03) is a leftover; switch
  it to a plain number (e.g. `?v=5`) on the next bump.
- **File had no `?v=` yet** → append `?v=1` on the first substantive change.
- **Vendored `js-deps/*`** get a `?v=` only when the bundled library version is
  swapped. The upstream version of each library is recorded next to it in
  `js-deps/` and in the dashboard's internal design notes; here it is only the
  cache-bust.
- The `?v=` query is **independent of `dashboard/VERSION`**. `VERSION` is the
  release triple on the settings page, whose patch position the `pre-commit`
  hook auto-increments on every commit to `main`
  ([AGENTS.md](../../../dashboard/AGENTS.md)). Nobody maintains the `?v=` tags
  automatically — only this file and `base.html`, by hand.

---

## The `installer/webui/` module: one central mark instead of many hand-kept ones

The installer's UI lives in its own Go module, `installer/webui/`, and both
hosts (the installer binary and — from Plan D on — the dashboard) embed it.
Its assets do **not** follow the per-file `?v=N` convention above. Instead
`webui.AssetVersion()` reads `installer/webui/VERSION` and the shell template
appends `?v=<version>` to every JS and CSS URL.

Why the difference: `installer/webui/VERSION` is patch-bumped by CI on every
PR that touches `installer/webui/` (`scripts/version/components.sh`), so a
changed asset busts its cache without anyone remembering to. The dashboard's
own assets keep their hand-kept marks — retrofitting them would mean
invalidating every asset on every dashboard release, which the lazy-panel
loading above makes expensive.

The vendored `installer/webui/static/js-deps/alpine.min.js` is the same build
as the dashboard's. Update both in the same PR: from Plan D on they share a
binary.

The installer UI's look is pinned to the drafts copied into
`installer/webui/test/reference/`. `installer.css` and `screens.css` contain the draft
CSS verbatim inside `/* == Vorlage: … == */` blocks; `npm test` fails when a
rule inside a block drifts from its draft or when an addition after a block
overrides a property the draft sets.

---

## Mandatory: bump at the end of every development branch

**At the end of every branch, every worktree and every small patch straight onto
`main`:**

1. Check which files under
   `dashboard/internal/webui/static/{js,js-deps,css}/` the branch changed
   substantively (`git diff --stat main -- dashboard/internal/webui/static/`).
2. For **each** changed file, raise the `?v=` counter in
   [base.html](../../../dashboard/internal/webui/templates/base.html) by 1 — append
   `?v=1` if there is no tag yet. Files referenced from multiple places (see the
   table above) in **all** references at once.
3. Add a row to the [change history](#change-history) table below: the
   `dashboard/VERSION` triple, the files touched, their new `?v=` values (same
   order across both columns), and the date. One row per version: if the
   version already has a row, extend it (latest value per file, date range)
   instead of adding a second one. Take the version CI bumped the branch to,
   not the one on `main`.
4. Bring the current-version-state section above up to date.
5. `cd dashboard && go test ./...` and `npm test` — the template tests in
   [webui_test.go](../../../dashboard/internal/webui/webui_test.go) check some
   `data-panel-*` strings verbatim and must be updated along with them.

A branch that touches frontend assets is **not done** until this bump and its
entry here are in place.

---

## Change history

Newest first. Each row is one release that moved one or more `?v=` values: the
files and the value they moved **to**, in the same order across the two columns.
The **dashboard version** is `dashboard/VERSION` at the commit that carried the
bump, taken from the predecessor repo's (`werkstatt-IoT`) history — that history
was not brought into this fork, so the mapping is fixed here and not
recomputable.

| Dashboard version | Files | New `?v=` | Date |
|---|---|---|---|
| v0.10.25 | `js/automations.page.js` (automations panel, test result read from its own endpoint for every viewer) | `22` | 2026-10-29 |
| v0.10.24 | `css/base.css` (toast redesign: glass, timer line, enter, exit and swipe motion) · `js/notify.js` (toast store: expiry for every severity, hold and release, exit state) · `js/toast-gesture.js` (new, swipe away, rubber band, pause on hover and focus) | `45` · `2` · `1` (new) | 2026-10-29 |
| v0.10.23 | `js/history-recorder.js` (errors of the leader start shown in the console, opt-in diagnostics log) · `js/history-exchange.js` (start failures shown in the console, opt-in diagnostics log) | `13` · `9` | 2026-10-29 |
| v0.10.21 | `js/automations-view.js` (devicemap and automations panels, sentences for month days and no new values) · `js/automation-templates.js` (automations panel, templates for the monthly full charge, a silent inverter and the low-tariff wallbox, restart option for the inverter warnings) · `js/automations.page.js` (automations panel, month day chips and the no new values comparison) · `css/automations.css` (automations panel, month day grid and advanced section) · `js/automation-wizard.js` (automations panel, inverter measured filter ignores the actuator lock, own switch of the inverter) | `8` · `4` · `21` · `14` · `4` | 2026-10-27 |
| v0.10.20 | `css/settings-controls.css` (settings and diagnostics panels, Home Assistant pairing section and its styles) · `js/settings.page.js` (settings panel, no account card for sessions from the companion) · `js/users-model.js` (settings panel, Home Assistant accounts follow their preset) · `js/users.page.js` (settings panel, Home Assistant pairing code, countdown and unpair) | `20` · `29` · `2` · `3` | 2026-10-27 |
| v0.10.18 | `js/automation-band.js` (automations panel, threshold slider for every preset step) · `js/automation-templates.js` (automations panel, energy templates for heat pump, tariff, grid and warnings) · `js/automation-wizard.js` (automations panel, heat pump, battery and price context, numbers and devices steps) · `js/automations-scenario.js` (automations panel, minimum run time and pause fields) · `js/automations-view.js` (automations panel, help texts for the new scenario fields) · `js/automations.page.js` (automations panel, rendering of the new assistant steps) · `css/automations.css` (automations panel, new assistant step styles) | `2` · `3` · `3` · `4` · `7` · `20` · `13` | 2026-10-26 |
| v0.10.17 | `js/update-bar.js` (install the offered update from the update bar) | `5` | 2026-10-26 |
| v0.10.16 | `css/base.css` (notice tab for accounts without view permissions) · `js/dashboard.js` (start on the first permitted tab, ignore hidden tabs) · `js/settings.page.js` (settings panel, design and setup permissions, read-only state) · `js/config.page.js` (config panel, read-only state) · `js/automations.page.js` (automations panel, read-only and operate-only states) · `css/automations.css` (automations panel, read-only hint) · `js/users.page.js` (settings panel, keep saved permissions after saving) · `js/versions.page.js` (settings panel, setup permission for update check and changelog) · `js/settings-masonry.js` (settings panel, cards inside a display contents group) · `css/settings-controls.css` (settings, diagnostics: own account card on the users page) | `44` · `39` · `28` · `10` · `19` · `12` · `2` · `9` · `2` · `19` | 2026-10-25 |
| v0.10.15 | `js/automation-band.js` (new, automations panel, target band math and drag component) · `js/automations.page.js` (automations panel, target band in the assistant) · `js/automation-wizard.js` (automations panel, no preset text for the band step) · `js/automation-templates.js` (automations panel, tolerance answer of the zero feed template) · `css/automations.css` (automations panel, target band and preset cards) | `1` (new) · `18` · `2` · `2` · `11` | 2026-10-25 |
| v0.10.14 | `js/automations.page.js` (automations panel, rule card, editor footer, mobile list with history sparklines, assistant sheet) · `js/automations-chart.js` (automations panel, last four triggers strip) · `js/automation-sheet.js` (new, automations panel, bottom sheet drag) · `css/automations.css` (automations panel, rule card and editor from the draft, mobile rule list, assistant sheet) · `js/automations-devices.js` (automations panel, live value without prefix) · `js/automations-view.js` (devicemap and automations panels, short subtitles for the add tiles) | `17` · `3` · `1` (new) · `10` · `2` · `6` | 2026-10-25 |
| v0.10.13 | `js/users-model.js` (new, settings panel, users model) · `js/users.page.js` (new, settings panel, users page) · `js/settings.page.js` (settings panel, own password card and users deep link) · `js/tailscale.page.js` (settings panel, permission flags split out of system_actions) · `js/versions.page.js` (settings panel, permission flags split out of system_actions) · `js/update-bar.js` (permission flags split out of system_actions) · `js/servicelogs.panel.js` (diagnostics panel, permission flags split out of system_actions) · `css/settings-controls.css` (settings, diagnostics: users page and account card) | `1` (new) · `1` (new) · `27` · `5` · `8` · `4` · `2` · `18` | 2026-10-25 |
| v0.10.12 | `js/automation-wizard.js` (new, automations panel, assistant step blocks and answers) · `js/automation-templates.js` (new, automations panel, assistant templates) · `js/automations.page.js` (automations panel, assistant dialog and creation) · `js/automations-scenario.js` (automations panel, maximum duration field) · `js/automations-view.js` (devicemap and automations panels, field help for maximum duration) · `css/automations.css` (automations panel, assistant dialog, role based buttons, stepper baseline) | `1` (new) · `1` (new) · `16` · `3` · `5` · `9` | 2026-10-24 |
| v0.10.11 | `js/automations.page.js` (automations panel, device chips and picker, fine tuning card, action cards, history switch) · `js/automations-chart.js` (automations panel, regulator band preview) · `js/automations-devices.js` (new, automations panel, device candidates for actuators, meters and actions) · `js/device-picker.js` (energy, device map and automations panels, single choice, role filters and tones) · `css/automations.css` (automations panel, chips, condition and action cards, steppers, sparklines) · `css/manager.css` (5 panels, picker role filters and tones) · `js/automations-scenario.js` (automations panel, new scenarios start enabled with history) | `15` · `2` · `1` (new) · `2` · `8` · `48` · `2` | 2026-10-23 |
| v0.10.10 | `js/automations-view.js` (devicemap and automations panels, scenario helpers and status pills) · `js/automations.page.js` (automations panel, scenario and rule tabs, rule editor blocks, undo bar) · `js/automations-scenario.js` (new, automations panel, scenario card and editor) · `js/automations-chart.js` (new, automations panel, scenario chart with 30 min, 6 h and today ranges) · `js/history-recorder.js` (scenario regulator outputs recorded without a settings entry) · `css/automations.css` (automations panel, scenario and rule editor layout) | `4` · `14` · `1` (new) · `1` (new) · `12` · `7` | 2026-10-23 |
| v0.10.9 | `js/automations-view.js` (devicemap and automations panels, field help for repeat and the two interval settings) · `js/automations.page.js` (automations panel, repeat field and configurable minimum interval in the rule editor) | `3` · `13` | 2026-10-23 |
| v0.10.8 | `js/servicelogs.panel.js` (new, diagnostics panel, service log view with polling) · `css/diagnostics.css` (diagnostics panel, service log section) | `1` (new) · `3` | 2026-10-22 |
| v0.10.7 | `js/settings.page.js` (settings panel, masthead title and subtitle with live update) · `css/settings-controls.css` (settings, diagnostics: masthead card rows, old save row removed) · `css/base.css` (masthead parts hideable, empty masthead collapses) · `css/manager.css` (5 panels: settings save button shares the energy action bar) | `26` · `17` · `43` · `47` | 2026-10-22 |
| v0.10.6 | `js/settings-masonry.js` (new, settings panel, cards fill the shorter column) · `js/settings.page.js` (settings panel, masonry, sidebar page picker and sheet instead of the More list) · `css/settings-controls.css` (settings, diagnostics: masonry, sidebar, picker, sheet, wizard slide-in) · `css/base.css` (settings sub tab row rules removed) | `1` (new) · `25` · `16` · `42` | 2026-10-22 |
| v0.10.5 | `js/devicemap-view-prefs.js` (new, device map, browser view settings) · `js/devicemap-model.js` (energy, device map, edit rights and view defaults) · `js/devicemap-graph.js` (energy, device map, wiring filter) · `js/devicemap.page.js` (device map, filter button, view menu) · `css/manager.css` (5 panels) | `1` (new) · `14` · `9` · `29` · `46` | 2026-10-21 |
| v0.10.3 | `js/layout-editor.js` (overview editor, unreachable Gridstack path removed, discarding changes also ends edit mode) | `19` | 2026-10-21 |
| v0.10.2 | `js/config-status.js` (config and automations panels, unreachable `pending` runtime status removed) | `4` | 2026-10-21 |
| v0.10.1 | `css/manager.css` (5 panels, update notice in settings as a row with one update button) | `45` | 2026-10-20 |
| v0.10.0 | `css/base.css` (what's new rows expand with explanation, image and guide link) · `js/whatsnew.page.js` (detailText, imageFrame, onToggle) | `41` · `4` | 2026-10-19 |
| v0.9.28 | `css/base.css` (statistics card chart area) · `js/energy-stats-model.js` (stats panel + energy cards, shared bar chart options) · `js/energy-stats-card.js` (energy cards, fixed period, key figures, bar chart) · `js/stats.page.js` (stats panel, chart options from the model) · `js/layout-editor.js` (editor, statistics card options, history card series as search field) | `40` · `4` · `4` · `5` · `18` | 2026-10-18 |
| v0.9.26 | `css/base.css` (panel headings aligned on devices, diagnostics and settings, less space above the overview edit button) · `css/manager.css` (5 panels, system tab as setting cards, no Tailscale/TinyTuya title) | `39` · `44` | 2026-10-18 |
| v0.9.25 | `css/manager.css` (5 panels, device map canvas border) · `js/dashboard.js` (device modal moved out, lazy loader) · `js/device-modal.js` (new, device modal, loaded on first open) | `43` · `38` · `1` (new) | 2026-10-18 |
| v0.9.24 | `css/base.css` · `css/manager.css` (5 panels) · `js/dashboard.js` · `js/devicemap-model.js` (energy, device map) · `js/devicemap-graph.js` (energy, device map) · `js/devicemap-wiring.js` (energy, device map) · `js/devicemap-dataflow.js` (energy, device map) · `js/devicemap-node-svg.js` (energy, device map) · `js/devicemap-subdevices.js` (new, energy, device map) · `js/devicemap.page.js` (device map) · `js/energy-plant.js` (energy) · `js/energy.page.js` (energy) | `38` · `42` · `37` · `13` · `8` · `3` · `5` · `7` · `1` · `28` · `6` · `13` | 2026-10-18 |
| v0.9.23 | `js/devicemap-model.js` (energy, device map) · `js/devicemap.page.js` (device map) · `js/energy.page.js` (energy) | `12` · `27` · `12` | 2026-10-17 |
| v0.9.22 | `js/devicemap-switch.js` (energy, device map) · `js/devicemap-model.js` (energy, device map) · `js/devicemap-graph.js` (energy, device map) · `js/devicemap-node-svg.js` (energy, device map) · `js/devicemap-wiring.js` (energy, device map) · `js/devicemap.page.js` (device map) · `js/energy.page.js` (energy) · `js/dashboard.js` · `css/manager.css` (5 panels) | `2` · `11` · `7` · `6` · `2` · `26` · `11` · `36` · `41` | 2026-10-17 |
| v0.9.21 | `js/devicemap-graph.js` (energy, device map) · `js/devicemap-node-svg.js` (energy, device map) · `js/devicemap-labels.js` (energy, device map) · `js/devicemap.page.js` (device map) · `js/dashboard.js` · `css/manager.css` (5 panels) | `6` · `5` · `4` · `25` · `35` · `39` | 2026-10-16 |
| v0.9.20 | `css/base.css` (closed help tooltips no longer widen pages, anchored under the trigger on narrow screens, settings sub tabs and, per browser setting, the main tabs overflow into a "More" list) · `css/manager.css` (5 panels: device map side panel clipped to the stage, energy role cards fit narrow screens) · `js/dashboard.js` (favourite chips keep pick order, narrow-screen tooltip offset, main tab "More" list) · `js/automations.page.js` (state document from `/api/v1/automation/state`) · `js/stats.page.js` (thinned axis labels, hatched export and battery charge) · `js/energy-stats-model.js` (stats panel + energy cards, `HATCHED_ROLES`) · `css/stats.css` (hatched swatch) · `css/settings-controls.css` (settings, diagnostics: version help tooltip on narrow screens) · `js/settings.page.js` (settings panel, "More" tab list, main menu mode switch) | `37` · `37` · `34` · `12` · `4` · `3` · `3` · `14` · `23` | 2026-10-15 |
| v0.9.18 | `js/mqtt.page.js` (settings panel, additional Home Assistant sensors multiselect) | `9` | 2026-10-15 |
| v0.9.17 | `js/history-rollup.js` (history panel, history tile) · `js/history-store.js` (`extent`, history panel, history tile) · `js/history-chart.js` (history panel, history tile) · `js/history.js` (history panel: coverage percentages, explicit resolution, refresh pause, resolution pill and pause button in the status line) · `css/history.css` (history panel, stats panel: status line controls) | `3` · `5` · `5` · `21` · `7` | 2026-10-15 |
| v0.9.16 | `js/history-rollup.js` (history panel, history tile) · `js/history-chart.js` (history panel, history tile) · `js/history.js` (history panel) · `js/layout-editor.js` (overview editor) · `css/history.css` (history panel, stats panel) · `js/history-coverage.js` · `js/history-exchange.js` | `2` · `3` · `17` · `16` · `5` · `4` · `8` | 2026-10-15 |
| v0.9.14 | `js/versions.page.js` (settings panel, changelog history) · `js/settings.page.js` (settings panel, `changelog_history`) · `js/mqtt.page.js` (settings panel, metrics multiselect and bridge help list) | `7` · `22` · `8` | 2026-10-14 |
| v0.9.13 | `js/i18n.js` (all pages, `login.html` keeps `3`) · `js/whatsnew.page.js` (`whatsNewPanel`, `entryText`) · `js/versions.page.js` (settings panel, `entryText`) | `4` · `3` · `6` | 2026-10-14 |
| v0.9.12 | `js/dashboard.js` · `js/device-structure.js` (new, device tiles) | `33` · `1` (new) | 2026-10-14 |
| v0.9.11 | `js/dashboard.js` · `js/energy-stats-card.js` (energy cards) · `js/stats.page.js` (stats panel) | `32` · `3` · `3` | 2026-10-14 |
| v0.9.9 | `css/base.css` · `css/settings-controls.css` (settings, diagnostics) · `css/stats.css` (new, stats panel) · `js/dashboard.js` · `js/energy-stats-card.js` (new, energy cards) · `js/energy-stats-model.js` (new, stats panel + energy cards) · `js/layout-editor.js` (overview editor) · `js/settings.page.js` (settings panel) · `js/stats.page.js` (new, stats panel) | `35` · `13` · `1` (new) · `30` · `1` (new) · `1` (new) · `15` · `21` · `1` (new) | 2026-10-12 |
| v0.9.7 | `js/devicemap-model.js` (energy, device map) · `js/devicemap-graph.js` (energy, device map) · `js/devicemap-wiring.js` (new, energy, device map) · `js/devicemap-dataflow.js` (energy, device map) · `js/energy-plant.js` (energy) · `js/devicemap.page.js` (device map) · `css/manager.css` (5 panels) | `9` · `4` · `1` (new) · `4` · `5` · `24` · `35` | 2026-10-12 |
| v0.9.6 | `js/devicemap-dataflow.js` (energy, device map) · `js/devicemap.page.js` (device map) · `js/energy-plant.js` (energy) | `3` · `20` · `4` | 2026-10-12 |
| v0.9.5 | `js/devicemap-model.js` (energy, device map) · `js/devicemap-dataflow.js` (energy, device map) · `js/energy-plant.js` (energy) · `js/devicemap.page.js` (device map) | `8` · `2` · `3` · `19` | 2026-10-12 |
| v0.9.4 | `js/history.js` (history panel) · `css/history.css` (history panel) · `js/settings.page.js` (settings panel) · `css/settings-controls.css` (settings, diagnostics) | `16` · `4` · `20` · `12` | 2026-10-11 |
| v0.9.3 | `js/history-store.js` · `js/history-exchange.js` · `js/history-coverage.js` · `js/history-recorder.js` · `js/history.js` (history panel) · `js/settings.page.js` (settings panel) · `js/history-view-card.js` (history tile, `webui.go`) | `4` · `5` · `3` · `11` · `15` · `18` · `2` | 2026-10-11 |
| v0.9.2 | `css/base.css` · `js/dashboard.js` · `js/update-bar.js` (new) · `js/settings.page.js` · `js/versions.page.js` · `js/whatsnew.page.js` (new) · `css/settings-controls.css` (settings, diagnostics) | `34` · `29` · `3` (new) · `16` · `5` · `2` (new) · `11` | 2026-10-10 |
| v0.9.1 | `js/automations.page.js` | `11` | 2026-10-10 |
| v0.8.20 | `css/base.css` · `js/dashboard.js` · `js/devicemap-model.js` (energy, device map) · `js/devicemap-node-svg.js` (energy, device map) · `js/devicemap.page.js` | `29` · `28` · `7` · `4` · `17` | 2026-10-10 |
| v0.8.19 | `css/settings-controls.css` (settings, diagnostics) · `css/base.css` · `css/manager.css` (5 panels) · `js/settings.page.js` · `js/versions.page.js` | `9` · `28` · `32` · `14` · `3` | 2026-10-10 |
| v0.8.18 | `css/base.css` · `css/manager.css` (5 panels) · `js/devicemap-model.js` (energy, device map) · `js/devicemap-graph.js` (energy, device map) · `js/device-picker.js` (new, energy, device map) · `js/energy-plant.js` · `js/devicemap.page.js` · `js/energy.page.js` | `27` · `31` · `6` · `3` · `1` (new) · `2` · `16` · `8` | 2026-10-10 |
| v0.8.15 | `css/base.css` · `css/manager.css` (5 panels) · `css/automations.css` · `js/devicemap-model.js` · `js/devicemap-node-svg.js` · `js/devicemap-labels.js` · `js/devicemap-graph.js` (new) · `js/devicemap-dataflow.js` (new) · `js/automations-view.js` (new) · `js/energy-plant.js` (new) · `js/devicemap.page.js` · `js/energy-model.js` (history panel + energy cards) · `js/history-recorder.js` · `js/energy.page.js` · `js/automations.page.js` · `js/config.page.js` · `js/dashboard.js` | `26` · `30` · `5` · `5` · `3` · `2` · `2` (new) · `1` (new) · `1` (new) · `1` (new) · `15` · `3` · `10` · `7` · `8` · `9` · `27` | 2026-10-08 – 2026-10-09 |
| v0.8.14 | `css/manager.css` (5 panels) · `js/dashboard.js` · `js/revisions.js` (config, energy, device map, settings panels + layout editor) · `js/energy.page.js` · `js/devicemap.page.js` · `js/devicemap-model.js` | `27` · `26` · `4` · `5` · `12` · `3` | 2026-10-08 |
| v0.8.13 | `css/manager.css` (5 panels) · `js/devicemap.page.js` · `js/devicemap-model.js` | `26` · `11` · `2` | 2026-10-08 |
| v0.8.12 | `css/manager.css` (5 panels) · `js/devicemap.page.js` · `js/devicemap-model.js` (new) · `js/devicemap-node-svg.js` (new) · `js/devicemap-labels.js` (new) | `25` · `10` · `1` · `1` · `1` | 2026-10-08 |
| v0.8.11 | `js/dashboard.js` · `css/diagnostics.css` (diagnostics panel, now also loads `css/settings-controls.css` `8`) | `25` · `2` | 2026-10-06 |
| v0.8.9 | `css/manager.css` (5 panels) · `js/revisions.js` (config, energy, device map, settings panels + layout editor) · `js/config.page.js` | `24` · `3` · `8` | 2026-10-06 |
| v0.8.7 | `js/systemconfig.page.js` | `5` | 2026-10-05 |
| v0.8.5 | `js/history-exchange.js` · `js/settings.page.js` | `3` · `13` | 2026-10-04 |
| v0.8.4 | `js/versions.page.js` | `2` | 2026-10-04 |
| v0.8.3 | `js/dashboard.js` | `23` | 2026-10-01 |
| v0.8.2 | `js/energy-flow.js` | `3` | 2026-10-01 |
| v0.8.0 | `js/config.page.js` · `js/history.js` | `7` · `13` | 2026-09-28 |
| v0.7.23 | `js/versions.page.js` (new) · `js/settings.page.js` · `css/settings-controls.css` | `1` · `12` · `8` | 2026-09-28 |
| v0.7.22 | `js/dashboard.js` · `js/mqtt.page.js` | `22` · `7` | 2026-09-28 |
| v0.7.21 | `js/i18n.js` (also `login.html`) · `js/revisions.js` (config, energy, device map, settings panels + layout editor) · `js/dashboard.js` · `js/device-tile.js` · `js/config.page.js` · `js/energy.page.js` · `js/devicemap.page.js` · `js/settings.page.js` · `js/mqtt.page.js` · `js/tailscale.page.js` · `js/systemconfig.page.js` · `js/automations.page.js` · `js/history.js` · `js/layout-editor.js` | `3` · `2` · `21` · `2` · `6` · `4` · `9` · `11` · `6` · `4` · `4` · `7` · `12` · `13` | 2026-09-28 |
| v0.7.20 | `js/automations.page.js` · `js/history.js` · `js/history-export.js` (history panel) · `js/history-store.js` · `js/history-coverage.js` · `js/history-exchange.js` · `js/history-recorder.js` · `js/dashboard.js` | `6` · `11` · `2` · `3` · `2` · `2` · `9` · `20` | 2026-09-27 |
| v0.7.19 | `js/revisions.js` (config, energy, device map, settings panels + layout editor) · `js/schema-form.js` (config + settings panels) · `js/config-status.js` (config + automations panels) · `js/config.page.js` · `js/devicemap.page.js` · `js/dashboard.js` · `js/layout-editor.js` | `1` (was unversioned) · `2` · `3` · `5` · `8` · `19` · `12` | 2026-09-27 |
| v0.7.18 | `js/energy-model.js` (energy cards + history panel) · `js/battery-card-core.js` · `js/energy.page.js` · `js/energy-day.js` · `js/energy-presentation.js` · `js/energy-flow.js` · `js/energy-band.js` · `js/energy-ring.js` · `js/energy-board.js` · `js/energy-schema.js` · `js/energy-status.js` · `js/settings.page.js` · `js/mqtt.page.js` · `js/tailscale.page.js` · `js/systemconfig.page.js` | `2` · `4` · `3` · `2` · `1` (was unversioned) · `1` (was unversioned) · `1` (was unversioned) · `1` (was unversioned) · `1` (was unversioned) · `1` (was unversioned) · `1` (was unversioned) · `10` · `5` · `3` · `3` | 2026-09-27 |
| v0.7.17 | `js/automations.page.js` · `css/automations.css` | `5` · `4` | 2026-09-26 |
| v0.7.16 | `js/notify.js` · `js/notifications.js` · `js/dashboard.js` · `js/device-tile.js` · `js/entity-values.js` · `js/overview-values.js` · `js/overview.page.js` | `1` (was unversioned) · `2` · `18` · `1` (was unversioned) · `1` (was unversioned) · `3` · `6` | 2026-09-26 |
| v0.7.15 | `js/i18n.js` (base + login) · `js/dashboard.js` · `js/overview-values.js` · `js/device-tile-values.js` · `js/compact-card-values.js` · `js/energy-model.js` (energy cards + history panel) · `js/battery-card-core.js` · `js/battery-status.js` · `js/energy-day.js` · `js/history.js` · `js/config.page.js` · `js/settings.page.js` · `js/mqtt.page.js` · `js/tailscale.page.js` · `js/automations.page.js` · `css/settings-controls.css` | `2` · `17` · `2` · `1` (was unversioned) · `1` (was unversioned) · `1` (was unversioned) · `3` · `2` · `1` (was unversioned) · `10` · `4` · `9` · `4` · `2` · `4` · `7` | 2026-09-26 |
| v0.7.14 | `css/base.css` · `js/dashboard.js` · `js/i18n.js` · `js/settings.page.js` | `23` · `16` · `1` (new) · `8` | 2026-09-25 |
| v0.7.13 | `css/manager.css` (5 panels) · `js/schema-form.js` (config + settings panels) · `js/config-status.js` (config + automations panels) · `js/config.page.js` · `js/automations.page.js` | `23` · `1` (was unversioned) · `2` (new) · `3` · `3` | 2026-09-25 |
| v0.7.0 | `css/base.css` · `css/manager.css` (5 panels) · `js/dashboard.js` | `22` · `22` · `15` | 2026-09-16 – 2026-09-22 |
| v0.6.7 | `css/base.css` · `js/dashboard.js` · `js/settings.page.js` | `20` · `14` · `7` | 2026-09-16 |
| v0.6.2 | `css/choices.css` (settings-panel + overview editor assets) · `css/settings-controls.css` | `2` · `1` (was unversioned) · `3` | 2026-09-11 |
| v0.6.0 | `js/mqtt.page.js` · `js/dashboard.js` · `js/settings.page.js` · `js/energy.page.js` · `css/manager.css` (5 panels) | `2` · `13` · `6` · `2` · `19` | 2026-09-10 |
| v0.5.25 | `js/settings.page.js` · `css/settings-controls.css` · `js/dashboard.js` · `css/base.css` | `5` · `2` · `12` · `19` | 2026-09-10 |
| v0.5.20 | `css/manager.css` (5 panels) · `js/systemconfig.page.js` | `18` · `2` | 2026-09-08 |
| v0.5.19 | `js/notifications.js` | `1` | 2026-09-08 |
| v0.5.18 | `css/base.css` | `18` | 2026-09-07 |
| v0.5.17 | `js/dashboard.js` · `js/layout-editor.js` | `11` · `11` | 2026-09-07 |
| v0.5.15 | `js/config.page.js` · `css/manager.css` (5 panels) · `js/history-recorder.js` | `2` · `17` · `8` | 2026-09-06 |
| v0.5.11 | `js/battery-card-core.js` · `js/battery-status.js` · `js/layout-editor.js` | `2` · `1` · `10` | 2026-09-05 |
| v0.5.10 | `css/manager.css` (5 panels) | `16` | 2026-09-05 |
| v0.5.9 | `js/settings.page.js` | `2` | 2026-09-05 |
| v0.5.8 | `js/battery-card-core.js` · `js/dashboard.js` | `1` · `10` | 2026-09-05 |
| v0.5.7 | `css/base.css` · `js/dashboard.js` · `js/layout-editor.js` · `css/layout-editor.css` | `17` · `9` · `9` · `9` | 2026-09-05 |
| v0.5.6 | `css/manager.css` (5 panels) | `15` | 2026-09-05 |
| v0.5.5 | `css/manager.css` (5 panels) · `js/settings.page.js` · `js/tailscale.page.js` | `14` · `1` · `1` | 2026-09-05 |
| v0.5.4 | `js/layout-editor.js` | `7` | 2026-09-04 |
| v0.5.0 | `js/history-store.js` · `js/history-recorder.js` · `js/history-export.js` · `js/systemconfig.page.js` | `2` · `7` · `1` · `1` | 2026-09-04 |
| v0.3.19 | `css/base.css` · `js/dashboard.js` · `css/manager.css` (5 panels) · `css/history.css` · `css/automations.css` · `js/overview.page.js` · `js/layout-editor.js` · `css/layout-editor.css` | `16` · `8` · `12` · `3` · `3` · `5` · `6` · `6` | 2026-09-04 |
| v0.3.15 | `css/base.css` · `css/manager.css` (5 panels) | `13` · `10` | 2026-09-03 |
| v0.3.14 | baseline — `base.html` state at commit `d6cc3e0`, no bump | — | 2026-09-02 |

The **v0.8.20** row is the device icon catalogue reorganisation (`feat/device-icon-catalogue`, reason: device icon catalogue reorganisation). The device map role icons and the sprite symbols draw from the catalogue, which bumps `base.css` and `dashboard.js` as well.

The **v0.7.15** row is `feat/dashboard-localization-formats`, the number
format setting and locale-aware dates (localization A2): every script that
formatted numbers, dates or sort order itself now goes through `I18n`,
`settings-controls.css` gains the three-option `.segmented--3` for the number
format picker. `energy-day.js` and `battery-status.js` carry their `?v=` in
`energyCardScripts` in `webui.go`, not in `base.html`.

The **v0.7.0** row merges three bumps that landed under the same version:
`base.css` and `manager.css` went to `21` on 2026-09-16, `manager.css` to
`22` later that day, `base.css` to `22` together with `dashboard.js` `15` on
2026-09-22.

The **v0.6.2** row: `choices.css` gets `box-sizing: border-box` on
`.choices`/`.choices__inner`/`.choices__list--dropdown`/`.choices__input` —
without it, the open (`.is-open { overflow: visible }`) multiselect's inner
box and dropdown list were each ~17px wider than their container (padding +
border added on top of `width: 100%` under the browser's content-box
default), spilling past the settings card's right edge. Bumped in both places
that load `choices.css` (`settings-panel` and the overview's layout-editor
assets), reconciling the pre-existing gap where only `settings-panel` carried
a `?v=`. `settings-controls.css` gains a four-option variant of `.segmented`
(`.segmented--4`, used by the new "Farbschema" theme picker replacing the
`<select>` on the settings display tab).

The **v0.6.0** row is the `feat: fold node telemetry into the dashboard
nodeagent` squash merge. On the branch the bumps landed in steps:

- `mqtt.page.js` `2` — MQTT tab gains a simulation toggle and per-metric
  publish switches; the file grows the `metricKeys` list, `saveNodeSettings()`
  and the `simulation_active`/`metrics` form fields.
- `dashboard.js` `13`, `settings.page.js` `6` — three new opt-in status-bar
  items (`cpu_temp`, `ram`, `undervoltage`) rendered from `/api/v1/health`
  `node.telemetry`; `dashboard.js` adds `formatTemp()`/`formatPct()`,
  `settings.page.js` expands the status-bar-item options list.
- `energy.page.js` `2` — the own-device filter follows the `energy_node`
  identity move: `OWN_ENERGY_DEVICE_ID` changes from
  `energy-node-dashboard-energy` to `energy_node` to match the renamed
  `energydiscovery.DeviceIdentifier`, so the dashboard's own seven energy
  sensors stay filtered out of the energy-role editor.
- `css/manager.css` `18 → 19` (5 panels) — the
  `<fieldset class="mqtt-metric-toggles">` added with the per-metric publish
  switches was rendering with raw browser fieldset/legend chrome; `manager.css`
  gains a `.mqtt-metric-toggles` rule matching the surrounding
  `.mqtt-source-toggle` block.

The **v0.3.19** row is the `feat(layout): overview layout-editing mode` squash
merge — on the branch the same files were bumped in steps (`layout-editor.*` and
`overview.page.js` `1 → 6`, `base.css` `13 → 16`, etc.), but only the merged
result reached `main`.
