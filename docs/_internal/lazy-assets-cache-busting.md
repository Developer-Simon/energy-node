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

## Bumping the marks with `cache_bust.py`

[scripts/dev/cache_bust.py](../../scripts/dev/cache_bust.py) compares the `?v=`
marks of the working tree with a base ref and tells which changed assets need a
new mark. Run this flow at the end of every development branch that touches
`dashboard/internal/webui/static/{js,js-deps,css}/`, and for every small patch
straight onto `main`:

1. `scripts/dev/cache_bust.py report` lists every changed asset with its
   expected `?v=`.
2. Edit the marks by hand in every place `report` names. Files referenced from
   several places change in all of them at once.
3. `scripts/dev/cache_bust.py history` writes the rows for the new marks into
   the history CSV. Add a note to the rows if you like.
4. `scripts/dev/cache_bust.py check` runs what CI runs. Against `prerelease`,
   pass `--base origin/prerelease`.
5. `cd dashboard && go test ./...` and `npm test`. The template tests in
   [webui_test.go](../../dashboard/internal/webui/webui_test.go) check some
   `data-panel-*` strings verbatim and must be updated along with the marks.

Without `--base`, the scripts compare against `git merge-base origin/main HEAD`.
A branch that touches frontend assets is **not done** until these checks pass.

---

## History

The history is the CSV
[cache-bust-history.csv](../../dashboard/internal/webui/cache-bust-history.csv)
with the columns `version,date,asset,v,note`:

- `version` is `dashboard/VERSION` at the commit that carried the bump. Take the
  version CI bumped the branch to, not the one on `main`.
- `asset` is the path below `static/`, `v` the new mark, `date` the day of the
  bump and `note` a short description of the change.

`history` rewrites the rows of the branch. Rows whose mark no longer applies
are removed and printed with their note. If CI moves the branch to
another version, the existing date and note are kept.

Git merges the file with `merge=union` (see `.gitattributes`). Two branches
append rows at the same place, and a union merge keeps the rows of both sides
instead of raising a conflict.

Rows older than the fork come from the predecessor repo (`werkstatt-IoT`),
whose history was not brought into this fork. They cannot be recomputed, so
`history` never changes them.

---

## What CI checks

`check` is strict on pull requests: every changed asset needs exactly the next
integer, and the history rows must match. On pushes and on pull requests from
`prerelease` it is lenient: any increase of the mark of a changed asset is
accepted.
