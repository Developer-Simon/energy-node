---
title: "Device dialog: development notes"
---

# Device dialog: development notes

Notes for changing the device dialog (`dialog#device-modal`). What the operator
sees is on [Device dialog](../dashboard/device-modal.md). The markup is in
`dashboard/internal/webui/templates/devices.html`, the behaviour in
`static/js/device-modal.js` (loaded lazily by `devicesPanel()` through
`ensureDeviceModal`), the layout in `base.css` from about line 1660.

## One layout, two presentations

The dialog is a **container query on itself** (`dialog#device-modal { container:
device-modal / inline-size }`), not on the viewport. From **860 px** of dialog
width it shows the sidebar and a tab bar (`@container device-modal (min-width:
860px)`), below that an accordion (`max-width: 859px`). The same exclusive
selection logic, `activeDeviceTab`, drives both. `isAccordion` is read from the
dialog width by a `ResizeObserver` in `observeMergeThreshold()` and once more in
`openDevice()`, so keep `860` in the CSS and in the JS in step.

Every tab is a trigger and panel pair (`.device-modal-pair`). In the tab bar the
pairs dissolve with `display: contents`, so the triggers and the panels become
siblings in one flex container. That has three consequences:

- **`order` has to be set on `.device-modal-trigger` and on
  `.device-modal-panelwrap`.** The panel sits two levels below the flex child, so
  `order` on the pair never reaches it. Triggers use 0 to 3, panels 100 to 103.
  Forgetting the panels pushes every panel after *Controls* below the tab bar.
- **`row-gap` is 0 on `.device-modal-switcher`.** The collapsed panelwraps are
  0 px rows (`grid-template-rows: 0fr`) between the tab bar and the active panel.
  A gap would add up for each of them and move the separator line away from the
  tabs.
- **`align-content: flex-start` is required, not cosmetic.** The grid of the scroll
  area stretches the switcher to the height of the usually taller sidebar. With
  the default `stretch` the excess is spread between the flex rows, the tab bar
  is pulled apart and the active panel stands far below the top edge.

The panels open with `grid-template-rows: 0fr` to `1fr` on `.device-modal-panelwrap`,
driven by the sibling selector `.device-modal-trigger.active + .device-modal-panelwrap`,
not with a hard `x-show`. The selection underline exists only from 860 px
(`display: none` as the base). In the accordion the open card marks the
selection. `positionUnderline()` measures the trigger and the switcher with
`getBoundingClientRect()` and moves the underline, which is anchored at
`left: 0`, with `translateX`.

The grid of the scroll area has two columns from 860 px. The sidebar spans
`grid-row: 1 / span 2`, otherwise the pinned favourites block, as the third
child, takes row 1 and column 2 and pushes the switcher into the 14.5 rem sidebar
column.

**`display: flex` applies only to `dialog#device-modal[open]`.** Setting it on
the plain selector overrides the browser rule `dialog:not([open]) { display:
none }`, and the closed dialog stays in the middle of the page.

## Scroll and merge

The scroll area (`.device-modal-scrollarea`, sidebar and switcher together)
scrolls as a whole below the fixed header, not just the panel. That needs only
one surface with a defined size. An earlier version scrolled only the panel and
needed a chain of `align-items`, `align-content` and `max-height: 100%` through
several nested flex containers, which broke easily.

Measurements and controls share one tab when the scroll area is tall enough
(`mergeHeightThreshold`, 520 px, an empirical value) **and** the device has both
(`canMerge`). `applyMergeThreshold()` **changes `mergedTab` only while
*Measurements*, *Controls* or their merged tab is active.** If the user is on
*Management & analysis*, where content can create a scrollbar, the state stays
as it is, so the tab set does not jump. `setDeviceTab()` brings the state back in
line when the user returns to one of those tabs. `openDevice()` starts every
device unmerged and lets the observer merge again if the height allows.

## Live updates

The content is rendered by Alpine from `deviceDetail`, the JSON of
`/api/v1/devices/{id}`. There are no fragment swaps. Each SSE `registry-updated`
calls `refreshSelectedDevice()` and so `loadDeviceDetail()`.

**`loadDeviceDetail()` must not set `activeDeviceTab`.** Otherwise every live
update throws the user back to *Measurements*. The first choice happens once, at
the end of `openDevice()`, through `defaultDeviceTab()`. The same function
decides which tab opens first, in the order measurements, controls,
configuration and diagnostics, management.

Two small Alpine directives, registered in `alpine:init`, without a motion
library:

- **`x-roll`** on the value spans lets a number set through `x-text` run to the
  new value as a text tween (300 ms, ease out cubic, interruptible, starting from
  the number shown last). Non-numeric values are set at once. The directive
  disconnects its `MutationObserver` while it writes, to avoid recursion.
- **`x-flip`** on the activity containers does a FLIP animation with
  `getBoundingClientRect()` and `element.animate()`: existing entries slide to
  their new place, new ones fade in from above (at most 240 ms, only transform
  and opacity). The first fill by `x-for` is taken in silently.

Both switch off completely for `prefers-reduced-motion`. The value goes into a
`<span>` inside `.v`, so the unit `<small>` beside it is not overwritten by
`x-text`.

## Configuration and diagnostics

The tab *Configuration & diagnostics* has two sections. **Configuration** renders
`configEntities` (`entityCategory === 'configuration'`) with the control tile,
because settable configuration entities (mostly `number`, `select` and `switch`
with `entity_category: config`) are sorted as `configuration` by
`entityCategory()` before the `commandable` branch, and would otherwise be a
dead value. **Diagnostics** renders `diagnosticEntities` read-only.
`configDiagEntities` is the union for `x-show`, the counter and
`defaultDeviceTab()`. The category is a heuristic on the name, `device_class` and
`entity_category` (`entityCategory()`), there is no device type registry.

## Activity is visible in one place

The short activity feed is in the sidebar in the wide presentation
(`x-show="!isAccordion"`) and in the *Management & analysis* panel in the
accordion (`x-show="isAccordion"`). Never in both. The mobile-only tab *Device
data* is the sidebar stand-in and shows the master data without activity.

A received MQTT message is shown as a list of single states (`messageStates()`):
a JSON object splits into one `key: value` line each, a scalar stays one line
under the entity or topic name, an empty payload is marked `(empty)`. Both
activity lists use the same two line `.device-modal-log-entry` and are capped at
three entries per source.

## Permissions

*Remove discovery* is admin gated: on the server through
`auth.RoleDeleteDeviceDiscovery`, in the page through `canDeleteDiscovery`.
Changing the icon and the favourites needs the permission to edit the layout
(`canEditPrefs`). *Ignore device* (reversible) is yellow, *Remove discovery* red.
Reactivating an ignored device happens outside the dialog, in the list of
ignored devices.
