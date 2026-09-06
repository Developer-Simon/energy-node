# Local dashboard smoke test

Starts the real dashboard on the development machine — without mosquitto,
without access to the Pi, without real devices — and checks the configuration
page end to end. Meant for changes that jsdom or Go unit tests alone cannot
convincingly cover (registry → HTTP API → form).

```bash
dashboard/test/smoke/run-local-dashboard.sh            # check, then tear down
dashboard/test/smoke/run-local-dashboard.sh --keep     # leave running, view in the browser
dashboard/test/smoke/run-local-dashboard.sh --devices src/tuya_mqtt
```

`--keep` prints the URL and credentials and leaves everything up until Ctrl-C
— the way to actually look at the form.

## Screenshot without clicks

`screenshot.mjs` drives a headless Chromium (Playwright, a real devDependency
in `package.json` — no ad-hoc setup needed) against a running `--keep`
dashboard, clicks "Als Gast fortfahren" when needed (over HTTP only guest
access is possible, see `login.html`/`GuestOnly`) and writes a screenshot.
Meant for visual checks of CSS/layout without opening a browser by hand every
time:

```bash
dashboard/test/smoke/run-local-dashboard.sh --keep --preset geraete-kacheln &
node dashboard/test/smoke/screenshot.mjs --out /tmp/tiles.png
```

| Option | Effect |
|---|---|
| `--url URL` | default `http://localhost:18100/` |
| `--out PATH` | default `test/smoke/.run/screenshot.png` (already `.gitignore`d) |
| `--wait SELECTOR` | also wait for an element before the screenshot is written |
| `--width` / `--height` | viewport, default 1280×900 (e.g. 390×844 for an iPhone 13 Pro) |

| Option | Effect |
|---|---|
| `--keep` | keeps running instead of tearing down; URL and credentials are printed |
| `--devices DIR` | device configurations from `DIR` instead of `src/battery_soc` |
| `--fixture FILE` | retained messages from `FILE` instead of `fixtures/battery-soc.json` |
| `--seed-data DIR` | `*.json` from `DIR` into the data directory, **before** the dashboard starts |
| `--theme NAME` | `mint`, `stromblau`, `signalgelb` or `tageslicht` in `settings.json` |
| `--port N` | different HTTP port (default 18100) |
| `--simulate` | PV/grid/battery/house load move along a sine curve instead of sitting still (only with `fixtures/energie-ueberschuss.json`, otherwise a no-op) |
| `--preset NAME` | bundles `--fixture`/`--seed-data`/`--simulate` for one of the cases documented below (see "Presets") |

View four themes with every energy card — one call per theme, without a single
click in the UI:

```bash
dashboard/test/smoke/run-local-dashboard.sh --keep \
  --preset energie --theme tageslicht
```

## Measuring client cost

`measure-client-cost.mjs` drives the same headless Chromium as
`screenshot.mjs`, attaches to the `Performance` counters via the Chrome
DevTools Protocol and reports what an open tab costs while idle. Meant for
evidence instead of gut feeling on changes to the live update.

```bash
dashboard/test/smoke/run-local-dashboard.sh --keep --preset energie-simulate &
node dashboard/test/smoke/measure-client-cost.mjs --seconds 30 --out /tmp/before.json
```

| Option | Effect |
|---|---|
| `--url URL` | default `http://localhost:18100/` |
| `--seconds N` | length of the measurement window, default 30 |
| `--out PATH` | also write as JSON |
| `--swap-target ID` | which fragment is counted, default `overview-live`; for the devices tab `devices-live` |
| `--width` / `--height` | viewport, default 1280×900 |

Reported are `TaskDuration`, `ScriptDuration`, `LayoutDuration` and
`RecalcStyleDuration` as the delta over the window (each also as a CPU share),
plus the final values of `Nodes` and `LayoutCount` and the number of
`htmx:afterSwap` events on `#overview-live`.

The `uebersicht-push` preset is meant for exactly this measurement: it shows
all seven energy charts plus `diagnostics`, `entity_value` and `entity_group`
— every card that gets its numbers over the SSE push.

For the devices tab the panel also has to be in the URL — the
`registry-updated` listener in `dashboard.js` bails out when its panel is not
`active`, and then there would be nothing to measure:

```bash
node dashboard/test/smoke/measure-client-cost.mjs \
  --url 'http://localhost:18100/?panel=devices' --swap-target devices-live --seconds 30
```

### Presets

`--preset NAME` sets `--fixture`, `--seed-data` and, where applicable,
`--simulate` in one go to one of the combinations documented below. Individual
flags **after** `--preset` override the preset (the same ordering rule as
elsewhere in the script) — `--preset energie --fixture own.json`, for example,
takes the seed set of `energie` but the own fixture.

| Preset | Equivalent to |
|---|---|
| `battery-soc` | `--fixture fixtures/battery-soc.json` (script default, no seed) |
| `energie` | `--fixture fixtures/energie-ueberschuss.json --seed-data fixtures/seed/energie-alle-karten` |
| `energie-simulate` | like `energie`, plus `--simulate` |
| `uebersicht-push` | like `energie-simulate`, plus `entity_value` and `entity_group` — for performance measurements with `measure-client-cost.mjs` |
| `energie-kombiniert` | `--fixture fixtures/energie-kombiniert.json --seed-data fixtures/seed/energie-kombiniert` — `load_mode "combined"`, band and board per entity, ring collected |
| `alle-funktionen` | `--fixture fixtures/alle-funktionen.json --seed-data fixtures/seed/alle-funktionen` |
| `geraete-kacheln` | `--fixture fixtures/geraete-kacheln.json --seed-data fixtures/seed/geraete-kacheln` — three device tiles with `span: "1"`, for visual checks of `.device-tile-entity` (slider width, title wrapping for `number`/`text`, value alignment, unchanged grid for all other entity types) |

## What is solved here

Four hurdles stand in the way of a local start; all four are handled in the script:

1. **The dashboard exits when no broker is reachable at startup.**
   → `minibroker.py`, a deliberately incomplete MQTT 3.1.1 broker
   (CONNECT/SUBSCRIBE/PINGREQ, retained delivery; no QoS > 0, no wildcards, no
   forwarding between clients). Do not use outside of tests.
2. **Without a live publish every value would sit there as "veraltet".** The
   registry treats a value from a retained message as `mqtt-replay`, and the
   dashboard marks `mqtt-replay` as stale — on real hardware the sensors' first
   own publish clears that, here there is none.
   → `minibroker.py` therefore re-sends every non-discovery topic once as a
   *non*-retained `PUBLISH` right after the retained block; the same copy, just
   without the retain flag. Discovery topics stay retained, otherwise the
   "Discovery retained" field in the device dialog would flip to "nein".
   `--simulate` sends its ticks non-retained for the same reason.
3. **The API requires a login.** → `DASHBOARD_ADMIN_PASSWORD` creates an admin
   on the first start, the script logs in and uses the session cookies.
4. **The login requires HTTPS.** → `isSecureRequest()` accepts, besides TLS,
   `X-Forwarded-Proto: https`; the script sets exactly that header.

Also: configurations are copied into a throwaway directory (the real files
under `src/` are **not** changed), and ports held by an aborted run are cleared
beforehand — otherwise `curl` silently talks to an old process.

The throwaway directory lives locally under `test/smoke/.run/` (excluded from
git via `.gitignore`) instead of the system `/tmp` — easier to clean up and
without side effects outside the repo.

On top of that comes a fifth hurdle that `--seed-data`/`--theme` solve: **every
run gets a fresh `mktemp` directory.** Layout, color scheme and energy roles
are not in it — `--keep` does not carry them over a restart either, because the
working directory is a different one on the next start. Without pre-seeding you
have to click both back together after every restart.

## The fixtures

`fixtures/battery-soc.json` is the list of retained messages every subscriber
gets on connect: four discovery configurations plus the matching state
payloads. It is built so that all the interesting cases occur:

| Topic | Purpose |
|---|---|
| `shelly/netz_meanwell/status` | JSON object with several numeric fields → key suggestions |
| `shelly/netz_lumentree/status` | second JSON object, for the second field pair |
| `bms/bank_a/voltage` | **bare number** → warning "kein JSON-Objekt" |
| `bms/bank_b/voltage` | known from discovery, **never sends** → "(noch keine Daten)" |

A custom fixture goes via `--fixture` (or as the second argument directly to
`minibroker.py`).

`--simulate` makes four of its power values (PV, grid, battery, house load)
swing around the base values above along a sine curve every three seconds, sent
as real MQTT `PUBLISH` packets on the already open connection — no energy
balance model, just enough movement so that the time-based energy cards
(`energy_band`, `energy_day`) show a real curve in the browser instead of a
flat line:

```bash
dashboard/test/smoke/run-local-dashboard.sh --keep --preset energie-simulate
```

`energy_day` groups by poll (the browser fetches `/api/v1/energy` every 10 s,
see `history-recorder.js`) and needs at least two points before any area is
drawn at all — so the card stays empty for a short while after the start.

`fixtures/energie-ueberschuss.json` is the second fixture: seven entities with
`unit_of_measurement: W`, computed for a sunny moment — PV 4200 W, feed-in
1250 W, battery charging at 900 W, house consumption 2050 W (of which wallbox
600 W and heat pump 450 W). The balance thus works out exactly
(`gap_applied == 0`, "Übriger Verbrauch" 1000 W), so the energy cards show real
values and the state "gut" instead of the empty state "kein Energiefluss" that
`battery-soc.json` produces. It belongs together with the seed set below — the
roles are assigned to the `unique_id`s of this fixture.

`fixtures/alle-funktionen.json` is **expensive, but nearly complete**: the
battery-SoC devices from `battery-soc.json`, the seven energy entities from
`energie-ueberschuss.json` (without the single `pv_wr_power` sensor) and, in
addition, a fully modelled AP Systems EZ1 inverter (`apsystems_dach`, two PV
strings, daily/total yield, a switchable power limit and an operating-status
switch with a `command_topic`). Meant for visual checks and manual tests that
want to see as many dashboard functions as possible at once, without having to
build a custom fixture for it — not for checking each individual function in
isolation and cheaply; for that the leaner fixtures above remain the better
choice.

Every device here has an `availability_topic` including a retained payload: the
Shelly-like devices on `<prefix>/online` with `true`/`false`, the AP Systems
inverter on `outstation/apsystems_dach/status/online` with `1`/`0` — two
encodings deliberately side by side. All are set to "online", only `bms_bank_b`
("never sends") is additionally explicitly `false`: that is the only case in
the preset that shows the offline availability indicator
(`has_availability && !available`). Together with the broker's live refresh
(see "What is solved here") no value sits there as "veraltet", as long as no
device is offline.

Belongs together with `fixtures/seed/alle-funktionen/`:

```bash
dashboard/test/smoke/run-local-dashboard.sh --keep --preset alle-funktionen
```

`fixtures/geraete-kacheln.json` is lean and tailored to the tuning knobs of
`.device-tile-entity`/`.device-tile-entities` (title wrapping, slider width,
alignment of the value column across several rows), three devices:

- `apsys_nord` — a `Power-Limit` slider (`number`, the hard case for the slider
  row: the 4-column special handling `.device-tile-entity-number`), a
  `Standort-Notiz` (`text`, the hard case for long free text values:
  `.device-tile-entity-text`), a `Betriebsstatus` switch (`switch`, short value
  without a unit, deliberately stays on the normal grid) and a sensor with a
  long, unbroken name (`Wechselrichtertemperatur`) — stays on the original
  `1fr`/`auto`/`auto` grid, because it is neither `number` nor `text` nor a
  long value without a unit.
- `apsys_sued` — the same combination without the `text` entity, as a control
  that a normal `Power-Limit` slider without a sibling text row looks the same.
- `energie_automationen` — a single text *sensor* (component `sensor`, not
  `text`!) with a long, unformatted value without a unit
  (`Letzte Energie-Automatisierung`) — the reason `.device-tile-entity` does
  not hang on the MQTT component alone: `device-tile.html` lifts every row
  without `unit_of_measurement` whose value is longer than 20 characters onto
  `.device-tile-entity-text` (see `$isLongTextSensor`). Short unit-less values
  like `Betriebsstatus`'s `ON`/`OFF` stay below the threshold and thus on the
  normal grid - only real free-text sensors like this one switch.

Belongs together with `fixtures/seed/geraete-kacheln/layout.json`, which places
all three devices with `span: "1"` on the overview page: the grid
(`.layout-grid`, `minmax(min(18rem, 100%), 1fr)`) thus forces the tiles onto
the 18rem lower bound regardless of window width — exactly the width at which
the slider used to squeeze the title column down to nearly 0:

```bash
dashboard/test/smoke/run-local-dashboard.sh --keep --preset geraete-kacheln
```

## Seed data

`--seed-data DIR` copies `DIR/*.json` into the data directory before the
dashboard starts — the same pattern as `--devices`, only for `settings.json`,
`layout.json`, `energy.json`, `device-map.json`. A partially filled
`settings.json` is enough: `Settings.UnmarshalJSON` starts from `Default()` and
fills in every missing field.

`fixtures/seed/energie-alle-karten/` is the bundled set:

| File | Content |
|---|---|
| `layout.json` | all eight card types visible (`energy_status`, `energy_summary`, `energy_flow`, `energy_ring`, `energy_schema`, `energy_band`, `energy_day`, `energy_board`) plus `diagnostics` |
| `energy.json` | roles `pv`/`grid`/`battery`/`load`/`wallbox`/`heat_pump` on the entities of `fixtures/energie-ueberschuss.json` |
| `settings.json` | theme `mint` — `--theme` overrides that after the copy |

`fixtures/seed/alle-funktionen/` is the counterpart to `alle-funktionen.json`
— likewise **expensive, but nearly complete**:

| File | Content |
|---|---|
| `layout.json` | like `energie-alle-karten/`, plus a second group "Geräte" with the so-far unused layout types `device` (AP Systems inverter) and `entity` (a single voltage entity) |
| `energy.json` | seven of the eleven roles, among them `battery_soc` (with `capacity_kwh`); `pv` deliberately comes from two entities (`apsystems_dach_pv1_power` + `apsystems_dach_pv2_power`), to show the summing of several sources per role — the inverter's total power stays deliberately unassigned and thus appears in the list of unassigned entities |
| `settings.json` | additionally `device_view_mode: analysis` plus the diagnostic/config and discovery-tooltip display enabled |
| `device-map.json` | positions for all ten devices plus one edge, so the device card is not empty |

Deliberately **not** covered: the automations tab (hangs on the separate Python
service over MQTT, which this harness does not start) and the mirrored roles
`grid_import`/`grid_export`/`battery_charge`/`battery_discharge` (would need
second, redundant sensors next to `grid`/`battery`).

`users.json` can be copied along, but then `DASHBOARD_ADMIN_PASSWORD` no longer
creates an admin: if the hash does not match the password in the script, the
login already fails.

## Two pitfalls when viewing in the browser

1. **`go:embed` bakes in templates, CSS and JS at compile time.** A running
   `go run ./cmd/dashboard` never sees later source changes — not even newly
   created files. The restart of the smoke test is therefore the **last** step
   before a visual check, not the first.
2. **Panel scripts (`data-panel-script` in `base.html`) carry no `?v=` cache
   buster**, unlike `base.css`, `manager.css` and `devicemap.page.js`. The
   browser caches them with `max-age=86400` and serves them from the disk cache
   on every tab click — across server restarts. `Ctrl+Shift+R` does not help,
   because the panel loader pulls them in only after the `load` event. To see a
   change to a panel script, clear the cache for the page (DevTools → Network →
   "Disable cache") or bypass the panel loader entirely: set state via
   `curl -X PUT` (with `X-Forwarded-Proto: https` and the cookie from
   `$WORK/cookies.txt`) and reload the page. For theme and layout that is now
   exactly what `--theme` and `--seed-data` do.

## What is checked

- `/api/v1/topics/samples` returns the last payload per topic, sorted
- a topic without a message stays without a payload (instead of an invented value)
- `battery_soc_devices` appears as a managed configuration
- **decimal numbers can be saved and read back** (`charge_efficiency` at 0.95)
  — the regression this harness was created for
- the server rejects a value outside the schema (1.5) with HTTP 400

The first two points name topics from `fixtures/battery-soc.json` and therefore
only run without `--fixture`. On top of that come checks once pre-seeding has
happened — a seed that silently does not arrive looks like a render error in
the browser:

- a seeded `layout.json` appears with visible cards under `/api/v1/layout`
- a seeded `energy.json` appears with assignments under `/api/v1/energy/roles`
- `--theme` is present in `/api/v1/settings`
- with `fixtures/energie-ueberschuss.json` **and** seeded roles, but
  **without** `--simulate`: the balance under `/api/v1/energy` works out with
  no remainder (PV 4200 W, feed-in 1250 W, battery 900 W, "Übriger Verbrauch"
  1000 W, `gap_applied == 0`) — the exact values `--simulate` deliberately
  violates, which is why the two checks never run at the same time
- with `--simulate` (and `fixtures/energie-ueberschuss.json`): the PV power
  under `/api/v1/topics/samples` changes within ten seconds — proof that the
  simulated values actually arrive as MQTT `PUBLISH` on the open connection,
  not just that the broker process is alive

### History exchange

The script also checks the exchange interface: the announcement under
`/api/v1/history/exchange` names protocol version 1 and exactly the tiers `1m`
and `5m` (the raw tier is never exchanged), and an offer from one peer reaches
exactly the other over two simultaneously open SSE streams — not the sender
itself. This is the only check that brings two clients together on the real
server; the hub and ring buffer are in `internal/historyexchange`, the browser
part is checked on its own in `test/history-exchange.test.mjs`.

The check injects its offer synthetically via `curl` and thereby bypasses the
compaction in the browser. In a manual test with two real tabs that is a
difference: 1m/5m sets only come into being once `history-recorder.js`
compacts its raw data, and that happens only for data older than
`history_raw_window_hours` (default 24h, see `compact()` in
`history-maintenance.js`). Two freshly opened tabs thus have nothing to
exchange at first — that is not a bug in the exchange interface but a
consequence of the default retention.

The exit code is 0 when all checks pass.
