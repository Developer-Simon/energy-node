---
title: "Dashboard: rules for narrow screens"
---

# Dashboard: rules for narrow screens

Rules for CSS changes in the dashboard that affect phones. They hold for every
page, apart from the exception below.

## One breakpoint

**700 px** is the switch between the phone and the desktop layout. CSS variables
do not work in media queries, so the value is a literal. Every stylesheet that
needs a phone rule has its own `@media (max-width: 700px)` block: `base.css`
(the main block at the end, plus `max-width: 430px` for the narrowest phones),
`manager.css`, `history.css`, `settings-controls.css` and `automations.css`.
Keep the phone rules of a component in one block next to it and use 700 px, not
a new number.

`manager.css` also has 720 px and 860 px blocks. Those belong to specific
components (energy plant, device dialog) and are measured on the width of the
component, not of the page. Do not move them to 700 px.

**The one exception is `automations.css`.** The chain view of an automation rule
needs more than 700 px, so it switches at `min-width: 62rem`. Between 700 px and
992 px the automations tab shows the recipe layout while every other tab already
shows the desktop layout. This is on purpose, do not unify it.

## Target

A 390 px viewport with `.75rem` body padding leaves 366 px of usable width. The
narrowest case checked is a 340 px card (iPhone SE). The objective test is

```js
document.documentElement.scrollWidth <= window.innerWidth
```

A page wider than the window is a bug, also with a tooltip open.

## Patterns

1. **Wide tables** go into `<div class="table-scroll">` and scroll sideways in
   their own frame. Columns and structure stay, there is deliberately no card
   layout for tables. An exception is `.energy-board-table-wrap`.
2. **Multi-column forms** become one column on a phone. Grids need
   `minmax(0, 1fr)` and `min-width: 0` on their children, otherwise the minimum
   width of a select or number field keeps the columns wider than the card.
3. **Touch targets** get at least 2.75 rem (44 px) below 700 px. The desktop
   density stays as it is, 22 px with a mouse is fine. Selectors mirror the
   desktop rules in specificity and win by coming later in the stylesheet.
4. **The tab bar** has two modes, set in Settings > Appearance and stored in
   this browser only (`dashboard.navMode`): *More* keeps one row and moves the
   rest to a list, *Dropdown* collapses the bar into a menu below 700 px. The
   state `navOpen` and the mode `navMode` live in `dashboardShell()`. Only rules
   below 700 px read the dropdown state, above it the bar renders as without the
   menu. The label of the trigger is read from the DOM (`activeTabLabel`), not
   duplicated in JavaScript.
5. **Cards with fixed geometry** have to be checked at 340 px. The energy flow
   card places its corner nodes at 15 % and 85 % of the card width, so a node of
   width `w` clips once the card is narrower than `w / 0.3`. The phone rule uses
   `6.4rem` for that reason.

Do not use `!important` for the phone rules of the layout grid, the grid falls
to one column by itself.

## What is not optimised

- **Layout editor.** Dragging is a desktop job. The page is readable and single
  column on a phone.
- **Device map.** The Cytoscape canvas brings its own touch behaviour and is not
  touched. Only its toolbars have bigger buttons.

## Checking

- `dashboard/test/dashboard.nav.test.mjs` covers the logic of the tab menu with
  JSDOM. Anything spatial is a visual check, JSDOM has no layout.
- `dashboard/test/smoke/mobile-overflow.mjs` runs against a running smoke
  dashboard (see `dashboard/test/smoke/README.md`). For each tab and width it
  reports elements that reach beyond the right edge, once closed and once with
  every help button focused, because an open tooltip can widen the page too.
  Exit code 1 on overflow. It is not part of the CI, run it by hand after a
  change that touches layout:

  ```sh
  dashboard/test/smoke/run-local-dashboard.sh --keep --preset docs-screenshots
  node dashboard/test/smoke/mobile-overflow.mjs --widths 340,390,1280
  ```
