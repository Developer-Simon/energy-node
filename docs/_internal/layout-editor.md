---
title: "Layout editor: development notes"
---

# Layout editor: development notes

Notes for changing the layout editor (`dashboard/internal/webui/static/js/layout-editor.js`,
`overview.page.js`, `templates/layout-editor.html`). What the editor does for
the operator is on [Layout editor](../dashboard/layout-editor.md). This page
keeps what the code does not tell you.

## One layout model

The editor works **on the real CSS grid of the overview**
(`repeat(auto-fill, minmax(min(18rem, 100%), 1fr))` with container queries).
`dressCard()` puts a `.layout-edit-slot` on the existing `.layout-grid-item` and
adds the chrome and the resize grip as its children. The card stays the same
grid child in both modes, with the same `layout-grid-item-N` class, the same
`--card-height` and the same container queries. That is why switching the mode
does not move anything.

Gridstack was used until 2026-09-03 and arranged the cards absolutely on its own
12-column grid. Two layout models cannot be identical: the view sizes cards by
their content (`min-height` is a lower bound), Gridstack set a fixed height. Do
not bring a second model back. Moving, resizing and dropping from the toolbox
are about 120 lines of own code on the real grid.

## Data model

Position is the order in the document, there are no `x` or `y`. `span` is a size
class (`"1"` to `"6"` or `"full"`), `height` an optional minimum height in units
of 7 rem (1 to 12). `validateLayout` normalises first, validates against
`layout.schema.json` and then rejects a span below the minimum of the card type.

The card catalogue, `dashboard/internal/settings/cardcatalog.go`, is the only
place that knows how much room a card type needs. It is used twice: the server
writes `--card-min-height` into the CSS (`cardStyle`), and `GET /api/v1/layout`
ships it to the editor as `card_types`. A new card type needs an entry in the
catalogue **and** in the `type` enum of `layout.schema.json`. A missing one
fails `TestCardCatalogMatchesSchema`. A variant such as `device:compact` has its
own entry. The editor rebuilds the key in `cardKey()` the way Go does in
`cardVariantKey()`.

## Live updates for a new card type

A new card type must not rely on the fragment swap for its live values. The
swap replaces all of `#overview-live`, rebuilds the shell (see the pitfalls
below), makes the grid flicker and costs the Pi a full render per tick. One
card on a page that the push does not cover forces that swap for the whole
page on every SSE event.

`liveGridPushCovers()` in `dashboard.js` decides this. The swap is skipped
only when every card on the page is covered:

- **Push branch.** The card's `type` is in `branchForKind` and maps to an SSE
  topic (`energy`, `entities`, `diagnostics`, `devices`, `energy_stats`). The
  card's own JS takes the values from that branch and patches its DOM in place,
  as `compact-card-values.js` and the energy cards do.
- **Self served.** The card loads its data in its own rhythm and is listed in
  `selfServed`, like `history_view`.

A card that fits neither falls back to the swap. When the shape of a card
changes (other rows, another device, availability), `replaceTile()` fetches
just that card with `?fragment=layout-item`. That swap of a single card is
fine, it only happens on a structure change, not on a value change.

The background is in the plans `2026-08-25-uebersicht-live-ohne-neuaufbau`,
`2026-08-26-uebersichtsraster-ohne-volltausch` and
`2026-08-28-geraetekacheln-ohne-volltausch` under `.docs/superpowers/plans/done/`.

## Pitfalls

**`mount()` runs before `load()` has finished.** `mount()` follows
`Alpine.initTree()` directly, `load()` is asynchronous, so `this.pages` is still
empty. The editor first links each card to a loose object built from the
`data-*` attributes. Changes in the options modal went there and `payload()`
never saw them. Two places fix it: `relinkEditItems()` at the end of `load()` and
`itemForSlot()`, which asks `findPagesItem()` before it falls back. When you add
an option, make sure it ends up in the object in `this.pages`.

**Modals and toolbox live in the `<body>`.** `section.panel.active` has an
animation with `transform`, and an ancestor with `transform` is the containing
block for `position: fixed` too. The toolbox, the options modal and the page
modal are therefore moved to the `<body>` on mount (`_modalHosts`) and back on
unmount. `unmount()` returns moved nodes to where they came from, it does not
delete them.

**Every fragment swap rebuilds the shell.** `#overview-shell` sits inside
`#overview-live`, which htmx swaps on every page change and live refresh.
`#overview-panel` and `#layout-editor-root` stay. `watchRowFill()` reads
`data-mode` on creation and fires `layout-editor:unmount` and `:mount` again, so
the editor hooks itself onto the new grid. `overviewShell().destroy()` has to
detach its `ResizeObserver` and the `htmx:afterSwap` listener, otherwise one
more pair is left behind for each page change.

**Swaps run one after another.** `refreshLiveFragment()` queues its calls
(`_liveQueue`). Two quick clicks on two page tabs would otherwise start two
requests, and whichever answer came last would decide the page. Every refresh
must append `&page=<activePage>`, otherwise the overview falls back to the first
page at the next live update.

**The server renders exactly one page.** A page created with *Page* exists only
in the editor. A fragment request with its name would return the first saved page
(`activePage()` in `webui.go`). For this case `renderLocalPage()` draws the grid
itself with the same placeholder cards as a freshly added card.
`syncActivePage()` runs on every `mount()` and at the end of `load()`, because
the tab bar (`dashboardShell.activePage`) decides which page is meant, not the
editor's own memory.

**Every card gets a fresh ID.** `addFromCatalog()` never reuses the ID of the
catalogue entry. Two diagnostics cards would share the ID `diagnostics`,
`findPagesItem()` would find the first, and the options modal of the second would
change the first. The card name depends on type and ref (`itemLabel()`), not on
the ID.

**The view fills rows, the editor does not.** `fillRows(spans, columns)` in
`overview.page.js` stretches the largest card of an incomplete row, the last one
on a tie, via an inline `grid-column`. It lives in the always-loaded file,
because the view has to look right without the editor ever being loaded.
`applyRowFill()` removes it in edit mode.

**Editor assets load on demand.** `dashboardShell.editorAssetsReady()` loads
`choices`, `revisions.js`, `layout-editor.js` and the CSS on the first click on
*Edit*, remembers the promise and discards it on failure, so a network error does
not leave the editor dead. `enterEdit()` only goes on when this succeeded.
`overview.page.js` stays tiny for that reason, anything beyond switching the
mode belongs in `layout-editor.js`. See
[lazy assets](lazy-assets-cache-busting.md) for the `?v=` rules.

**The last page cannot be deleted.** With no page there is no *Edit* button and
no way back into the editor. A layout saved with no pages shows the empty state
("No page set up yet") with an *Edit* button. A fresh installation without
`layout.json` still gets `defaultLayout(devices)`, which is told apart through
`store.LayoutConfigured()`.

**The first tab.** `base.html` draws the fixed `#tab-overview` only while the
layout has no pages. Otherwise the `.tab.is-page` buttons take over.

## Device cards

A `device` card has the field `display` (`detail` or `compact`). There is no
second item type. The category switches only apply to the detail card, the
compact card picks its rows itself in `priorityEntities()` (`webui.go`), unless
the item has `entity_refs`, which fixes them (up to three, in order). Changing to
*Detail* or to another device empties `entity_refs`.

A configured compact card does not depend on the `structure_compact`
fingerprint. Its rows are fixed. `dashboard.js` recognises it by
`data-compact-configured="true"`. When every compact card in a grid is
configured, `structure` and `structure_availability` (device ID and status dot)
decide whether the fragment is swapped. Plain measurement changes are patched in
place by `compact-card-values.js`.

## Leaving and saving

`confirmLeave()` resolves at once when nothing is unsaved. Otherwise it opens
`#layout-guard-modal`, a themed `<dialog>`, and resolves with the choice.
`confirmUnsavedUnload()` stays as the `beforeunload` handler, because a dialog
cannot stop a real tab close. `save()` sends `{version: 3, pages}`, empty and
`0` values fall away, and fires `layout-saved`.

The target width preview (`applyTargetWidth`) sets `--target-w` and
`--target-zoom` on `.layout-canvas-stage`. Measure the **carrier** of the stage,
not the grid inside it: the grid already stands at the last chosen width and
would measure itself, so only the second click would be right.

`insertionPoint()` and `resizeTo()` get their rectangles and geometry passed in
instead of measuring. All of their logic is therefore testable without a layout
engine.

## Tests

- Go: `cardcatalog_test.go` (catalogue against schema), `settings_test.go`
  (validation, stability), `webui_test.go` (`TestOverviewRendersCardStyleAndFlowScale`,
  `TestManagerAssetsLoadIndependently`, `TestOverviewDoesNotLoadManagerAssetsInitially`,
  `TestEditorLaedtKeinGridstack`), `httpapi_test.go` (`TestLayoutGetShipsCardCatalog`).
- JS: `layout-editor.test.mjs`, `layout-editor.guard.test.mjs`,
  `layout-editor.css.test.mjs` (the motion table of the design lock),
  `overview.page.test.mjs`.
- By hand: view, *Edit*, hover chrome, options modal, toolbox, target width,
  *Page* creates a tab, *Save & close*.

## Known limits

- Groups are invisible on the overview, only the order inside one counts.
- Above size 6 there is only `full`. The column width of 18 rem is fixed.
- `energy_flow` and the energy graphics have fixed element IDs, so they exist
  once per layout. Whether a second copy breaks is untested.
- `energy_summary` is gone but stays in the catalogue, so that an old layout
  still loads. `overview.html` renders such an item silently.
- `/api/v1/layout` itself checks no role. The editor fragment is only offered by
  the manager and guest handlers.

## Ideas

- Real WYSIWYG: after a change in the options modal the server-rendered card is
  not redrawn, the change shows after saving and reloading. A client-side
  re-render of the real card templates would close that.
- Drag between pages.
- The revision list as a real undo ("back to yesterday").
