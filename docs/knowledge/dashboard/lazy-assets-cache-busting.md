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
Gridstack is no longer part of the editor assets since the move to the real CSS
grid — edit mode moves and scales tiles itself so that the view and the editor
show the same geometry.

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
   **sequentially** (dependent scripts such as Popper before Tippy).
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

As of **2026-09-05**, read from `base.html` and `overview.html`. "–" means: no
`?v=`, relies solely on the 1-day cache.

### Global

| Asset | `?v=` |
|---|---|
| `css/base.css` | `17` |

### Deferred `<script>` block (channel 1)

| Asset | `?v=` |
|---|---|
| `js/notify.js` | – |
| `js/theme.js` | – |
| `js/layout-fill.js` | – |
| `js/device-tile.js` | – |
| `js/entity-values.js` | – |
| `js/overview-values.js` | `1` |
| `js/device-tile-values.js` | – |
| `js/compact-card-values.js` | – |
| `js/energy-presentation.js` | – |
| `js/energy-flow.js` | – |
| `js/history-rollup.js` | `1` |
| `js/history-store.js` | `2` |
| `js/history-coverage.js` | `1` |
| `js/history-exchange.js` | `1` |
| `js/history-maintenance.js` | `1` |
| `js/history-recorder.js` | `7` |
| `js/notifications.js` | – |
| `js/dashboard.js` | `10` |
| `js/overview.page.js` | `5` |
| `js-deps/htmx.min.js` | – |
| `js-deps/alpine-collapse.min.js` | – |
| `js-deps/alpine.min.js` | – |

### Conditional energy-card scripts (channel 3)

| Asset | `?v=` |
|---|---|
| `js/energy-model.js` | – |
| `js/battery-card-core.js` | `2` |
| `js/battery-status.js` | `1` |
| `js/energy-band.js`, `js/energy-ring.js`, `js/energy-board.js`, `js/energy-day.js`, `js/energy-schema.js`, `js/energy-status.js` | – |

### Lazy panel assets (channel 2)

| Panel | Scripts (`?v=`) | CSS (`?v=`) |
|---|---|---|
| `devices-panel` | `js-deps/popper.min.js` –, `js-deps/tippy.umd.min.js` – | `css/tippy.css` – |
| `history-panel` | `js-deps/apexcharts.min.js` –, `js-deps/flatpickr.min.js` `1`, `js-deps/flatpickr-l10n-de.js` `1`, `js/history-export.js` `1`, `js/energy-model.js` –, `js/history.js` `9` | `css/flatpickr.min.css` `1`, `css/flatpickr.css` `1`, `css/history.css` `3` |
| `diagnostics-panel` | – | `css/diagnostics.css` `1` |
| `config-panel` | `js/revisions.js` –, `js/schema-form.js` –, `js/config.page.js` `1` | `css/manager.css` `16` |
| `energy-panel` | `js/revisions.js` –, `js/energy.page.js` `1` | `css/manager.css` `16` |
| `devicemap-panel` | `js-deps/cytoscape.min.js` –, `js/revisions.js` –, `js/devicemap.page.js` `7` | `css/manager.css` `16` |
| `settings-panel` | `js-deps/choices.min.js` –, `js/revisions.js` –, `js/schema-form.js` –, `js/settings.page.js` `2`, `js/mqtt.page.js` `1`, `js/tailscale.page.js` `1`, `js/systemconfig.page.js` `1` | `css/choices.min.css` –, `css/choices.css` `1`, `css/manager.css` `16` |
| `automations-panel` | `js/automations.page.js` `2` | `css/manager.css` `16`, `css/automations.css` `3` |

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
| `data-editor-script` | `js-deps/choices.min.js` –, `js/revisions.js` –, `js/layout-editor.js` `10` |
| `data-editor-css` | `css/choices.min.css` –, `css/choices.css` –, `css/layout-editor.css` `9` |

### Assets referenced from multiple places

On a bump these must be raised **in every reference at once**, otherwise one
place pulls the old file from the cache:

- `css/manager.css` – 5 panels (`config`, `energy`, `devicemap`, `settings`, `automations`)
- `js/energy-model.js` – channel 2 (`history-panel`) **and** channel 3 (energy cards)
- `js/revisions.js`, `js/schema-form.js` – several manager panels and the overview editor assets
- `js/choices.min.js` – overview editor assets and `settings-panel`

**Known inconsistency:** `css/choices.css` is `?v=1` in `settings-panel` and has
no `?v=` in `data-editor-css` on `#overview-panel`. Reconcile on the next touch.

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
3. Record the bump in the [change history](#change-history) below: date, file,
   `old → new`, reason, branch/commit.
4. Bring the current-version-state section above up to date.
5. `cd dashboard && go test ./...` and `npm test` — the template tests in
   [webui_test.go](../../../dashboard/internal/webui/webui_test.go) check some
   `data-panel-*` strings verbatim and must be updated along with them.

A branch that touches frontend assets is **not done** until this bump and its
entry here are in place.

---

## Change history

Newest entries on top. Format:
`YYYY-MM-DD — file: old → new — reason (branch/commit)`

- **2026-09-05** — `js/battery-card-core.js`: `1 → 2`, `js/battery-status.js`:
  `– → 1` (first entry), `js/layout-editor.js`: `9 → 10` — trajectory card: the
  chart SVG now has a fixed height (150 px) and a growing `viewBox` width (the
  renderer measures the card via `ResizeObserver`), so a wide tile grows wider
  instead of taller; the history and projection windows are configurable via
  `data-battery-window` / `data-battery-projection-window` (respectively `window`
  / `projection_window` in the HA card), and the layout editor gets two select
  fields for them. `css/layout-editor.css` unchanged (`9`). Branch
  `2026-09-05-batterie-trajektorie-sizing-und-zeitfenster`.
- **2026-09-05** — `css/manager.css`: `15 → 16` (all 5 panels) — storage state on
  the settings page: the estimate block was a single `<template x-if>` with six
  `<tr>` children, of which Alpine renders only the first — "used write cycles"
  (used writes in %) and four more rows were missing. Each row now has its own
  `<template x-if>`. The subtitle and the long measured/estimated note moved into
  a `.field-help` tooltip on the `<h3>` (following the automations pattern);
  `.storage-health-subtitle` and `.storage-health-note` in `manager.css` are
  gone, `.storage-health-header h3` becomes a flex row for the help icon (patch
  straight onto `main`).
- **2026-09-05** — `js/settings.page.js`: `1 → 2` — TinyTuya bugfix: saved
  credentials became unusable once the Access ID field was filled (autofill or a
  leftover from an earlier query in the same session). `credentialsValid` now
  requires a secret only for a *new* Access ID; without a secret, `savedCredentials`
  alone counts, and `loadDevices()` sends the Access ID only together with a
  typed secret. The backend (`resolveTinyTuyaCloudRequest`) now also falls back
  to the stored secret when the Access ID is set but identical (patch straight
  onto `main`).
- **2026-09-05** — `js/battery-card-core.js`: `– → 1` (first entry, backfilled),
  `js/dashboard.js`: `9 → 10` — battery status card (column and trajectory):
  `battery-card-core.js` is the host-independent core and now loads conditionally
  in the `EnergyCardScripts` channel (finding 8 of the closing review, instead of
  unconditionally on every page); `dashboard.js` gets a `battery_status: 'energy'`
  entry in `branchForKind` (`liveGridPushCovers()`), so an SSE push patches the
  card instead of swapping the whole grid (branch
  `2026-09-05-batterie-statuskarte-saeule-und-trajektorie`).
- **2026-09-05** — `css/manager.css`: `14 → 15` (all 5 panels) — settings "System"
  tab reworked in the Apple style: `System access` / `System actions` /
  `Configuration` brought onto the `--settings-ease` / `--radius-sm` /
  `--radius-lg` form language known from the Tailscale/TinyTuya wizards (scope
  `#settings-system`), badge transition modelled on `.runtime-status-badge` (dot
  + colour change as a transition instead of a jump), press feedback on the
  `.system-action-danger` buttons, and all buttons in the tab (log out, the three
  system actions, save/discard the configuration, refresh/restore revisions)
  switched to `.icon-button.labeled` with sprite icons instead of plain text.
- **2026-09-04** — `css/manager.css`: `13 → 14` (all 5 panels) — Tailscale/TinyTuya
  wizard: buttons/inputs inside the two assistants rounded to `--radius-sm`
  (Apple form language) and the toolbar buttons switched to `.icon-button.labeled`
  with icons from the existing sprite (templates only changed, but `manager.css`
  carries the new radius rule).
- **2026-09-04** — `css/manager.css`: `12 → 13` (all 5 panels), `js/settings.page.js`:
  `– → 1`, `js/tailscale.page.js`: `– → 1` — Tailscale/TinyTuya setup assistants
  moved onto a shared stepper partial (`settings-stepper.html`) with animated
  step transitions (Apple design/motion rework); the Tailscale wizard shortened
  from 4 to 3 steps ("confirm access options" merged into the result step).
- **2026-09-04** — `js/layout-editor.js`: `6 → 7`, `css/layout-editor.css`: stays
  `6` — removal of the `entity` card type ("entity with technical details"): the
  second catalogue row per entity and the per-entity options are gone,
  `refMissing()` and the thumbnails no longer know the type. JavaScript only, the
  editor CSS is unchanged.
- **2026-09-05** — `css/base.css`: `16 → 17`, `js/dashboard.js`: `8 → 9`,
  `js/layout-editor.js`: `7 → 9`, `css/layout-editor.css`: `6 → 9` — squash-merge
  of the compact-device-tile plan (tasks 1–8) onto `main`: `device` items render
  either `device-tile` or `compact-card`, the device of a dropped tile can be
  changed afterwards in the options modal, and the toolbox thumbnails draw their
  role colours (`--flow-pv`, `--flow-battery`, `--accent`, `--text-*` …) via
  `var(--token)` instead of fixed hex values, so every colour scheme (including
  `tageslicht`) supplies its own values — this replaced an interim purely
  monochrome `currentColor` draft that was briefly task 7 on the branch.
  `layout-editor.js` jumped straight from the `7` of the entity removal (see the
  entry above) to `9` because the branch had run in parallel to `8` before being
  merged here; `layout-editor.css` moves in lockstep with the `.js` file despite
  a small content change.
- **2026-09-04** — `js/history-store.js`: `1 → 2`, `js/history-recorder.js`: `6 → 7`,
  `js/history-export.js`: `– → 1`, `js/systemconfig.page.js`: `– → 1` — product
  rename: dashboard client storage keys, export header and filename prefix
  updated (task 6 of the GitHub-publication-and-rename work).
- **2026-09-04** — `css/base.css`: `13 → 16`, `js/dashboard.js`: `5 → 8`,
  `css/manager.css`: `10 → 12` (all 5 panels), `css/history.css`: `2 → 3`,
  `css/automations.css`: `2 → 3`, `js/overview.page.js`: `– → 5`,
  `js/layout-editor.js`/`css/layout-editor.css`: `– → 6`,
  `js/energy.page.js`/`js/mqtt.page.js`: stay `1` — merge of the layout edit mode
  onto `main`. `base.css`/`dashboard.js` now carry both branches (energy-tab
  redesign from `main`, editor from the branch), hence a jump past the numbers
  assigned on the branch. The former `layout-panel` channel is gone.
- **2026-09-04** — `js/layout-editor.js`: `5 → 6`, `css/layout-editor.css`:
  `5 → 6` — more follow-up on edit mode: a "discard changes" button in the
  toolbar, the toolbox-toggle listener is re-attached per mount and released in
  `unmount()` (after "save & close" the toolbox otherwise stopped opening), the
  switch back to editor width animates over the pixel width instead of jumping to
  `auto`, hidden tiles are kept in sync in the editor (`syncHiddenCards()`), and
  the duplicate visibility switch in the options modal is gone.
- **2026-09-04** — `js/dashboard.js`: `6 → 7`, `js/overview.page.js`: `4 → 5`,
  `js/layout-editor.js`: `4 → 5`, `css/layout-editor.css`: `4 → 5` — follow-up on
  edit mode: the toolbox now hangs off `<body>` and sits `position:fixed` under
  the sticky toolbar (previously `position:absolute` in the panel and off-screen
  after scrolling), the left/right selection in its footer is gone, the catalogue
  knows the entity list and carries a value card and a detail card per entity;
  `energy_schema`/`energy_status`/`entity_value`/`entity` get their own
  thumbnails. Plus the page logic: `syncActivePage()`/`renderLocalPage()`, own
  IDs per tile, `relinkEditItems()` and the queue in `refreshLiveFragment()`.
- **2026-09-03** — `css/base.css`: `14 → 15`, `css/manager.css`: `11 → 12` (all 5
  panels), `css/history.css`: `2 → 3`, `css/automations.css`: `2 → 3`,
  `js/dashboard.js`: `5 → 6`, `js/overview.page.js`: `3 → 4`,
  `js/layout-editor.js`: `3 → 4`, `css/layout-editor.css`: `3 → 4` — follow-up on
  edit mode. Three rule sets moved into `base.css` because they are needed by
  markup that is in the document without the previous file: the base rules of the
  overview toolbar (`.layout-toolbar`, `.layout-btn`, `.layout-count`,
  `.layout-spacer`) from `layout-editor.css`, the Apple switch
  (`.settings-toggle*`) from `manager.css`, and the sprite icons
  (`.automation-icon`) from `automations.css` / `history.css` where they were
  duplicated.
- **2026-09-03** — `js/overview.page.js`: `1 → 2`, `js/layout-editor.js`: `1 → 2`,
  `css/layout-editor.css`: `1 → 2` — completion of the layout edit mode:
  `overview.page.js` fetches and mounts the editor fragment, `layout-editor.js`
  wires up chrome, options modal, toolbox, target width and "save & close", the
  mode styles in `layout-editor.css` target `#overview-panel`.
- **2026-09-03** — `css/base.css`: `13 → 14`, `css/manager.css`: `10 → 11` (all 5
  panels), `js/overview.page.js`: `– → 1`, `js/layout-editor.js`: `– → 1`,
  `css/layout-editor.css`: `– → 1` — layout editor as an edit mode of the
  overview; the old `layout-panel` with `layout.html` and the dead
  `.layout-page*`/`.layout-group*` rules in `manager.css` removed. The two
  `layout-editor.*` carry their `?v=` in `data-editor-script` / `data-editor-css`
  on `#overview-panel` in `overview.html`, not in `base.html`. Note: `manager.css`
  was already at `10` in `base.html` while the version-state section above still
  said `9` — now pulled together to `11`.
- **2026-09-02** — `css/base.css`: `12 → 13` — "calculated" hint in the
  energy-flow building node as its own centred line under "Building" instead of
  as an `::after` under the power value (branch `feat/energy-tab-apple-redesign`).
- **2026-09-02** — First entry for this file. No bump; documents the state found
  in `base.html` at that time (see [current version state](#current-version-state)).
  Reference commit `d6cc3e0`.
