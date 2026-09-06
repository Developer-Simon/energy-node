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

As of **2026-09-06**, read from `base.html` and `overview.html`. "–" means: no
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
| `js/history-recorder.js` | `8` |
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
| `config-panel` | `js/revisions.js` –, `js/schema-form.js` –, `js/config.page.js` `2` | `css/manager.css` `17` |
| `energy-panel` | `js/revisions.js` –, `js/energy.page.js` `1` | `css/manager.css` `17` |
| `devicemap-panel` | `js-deps/cytoscape.min.js` –, `js/revisions.js` –, `js/devicemap.page.js` `7` | `css/manager.css` `17` |
| `settings-panel` | `js-deps/choices.min.js` –, `js/revisions.js` –, `js/schema-form.js` –, `js/settings.page.js` `2`, `js/mqtt.page.js` `1`, `js/tailscale.page.js` `1`, `js/systemconfig.page.js` `1` | `css/choices.min.css` –, `css/choices.css` `1`, `css/manager.css` `17` |
| `automations-panel` | `js/automations.page.js` `2` | `css/manager.css` `17`, `css/automations.css` `3` |

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
3. Add a row to the [change history](#change-history) table below: the
   `dashboard/VERSION` triple, the files touched, their new `?v=` values (same
   order across both columns), and the date.
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
| harden-battery-soc branch | `js/config.page.js` · `css/manager.css` (5 panels) · `js/history-recorder.js` | `2` · `17` · `8` | 2026-09-06 |
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

The **v0.3.19** row is the `feat(layout): overview layout-editing mode` squash
merge — on the branch the same files were bumped in steps (`layout-editor.*` and
`overview.page.js` `1 → 6`, `base.css` `13 → 16`, etc.), but only the merged
result reached `main`.
