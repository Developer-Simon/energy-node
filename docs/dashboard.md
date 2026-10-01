---
title: "The dashboard, page by page"
---

# The dashboard, page by page

The Energy Node dashboard is a single statically linked Go binary. Templates,
CSS and JavaScript are compiled into it with `go:embed`; there is no build
step, no CDN and no server-side database. It renders HTML on the server,
updates values over an SSE push, and keeps chart history in the browser.

The UI speaks German and English, chosen per browser with the `DE | EN`
switch in the header (see
[`knowledge/dashboard/localization.md`](knowledge/dashboard/localization.md)).
The screenshots and the labels quoted on this page are the English ones.
Device and entity names are not translated. They come from MQTT Discovery and
stay in the language the devices announce them in, which is German for the
fixture devices in the screenshots.

Every screenshot on this page comes from the local smoke test with the
`docs-screenshots` preset. That is the real dashboard against a fixture
broker, with no Raspberry Pi and no device involved.
[Reproduce them yourself](#reproducing-these-screenshots) with one script.

---

## The window frame

![The dashboard on a wide screen](images/dashboard.png)

Three things are always present, on every tab:

- **The system status bar** — broker connection, storage
  health, uptime and dashboard version. Which of these fields appear is
  configurable, and the whole bar can be switched off.
- **The tab bar.** The first entries are the *layout pages* — user-defined
  overview pages, named in the layout editor. After the divider come the
  fixed tabs: Devices, History, Configuration, Energy, Device Map,
  Diagnostics, Automations, Settings.
- **Who you are logged in as.** Over plain HTTP only *continue as guest* is
  offered; the admin login is refused unless the request arrives over HTTPS.
  A guest can look at everything, edit layouts and automation rules, but the
  protected actions — the MQTT and bridge configuration (`mqtt_config`),
  restart/reboot/shutdown (`system_actions`) — need a registered user with the
  matching role **and** HTTPS. See
  [`knowledge/dashboard/secrets-and-credentials.md`](knowledge/dashboard/secrets-and-credentials.md).

Panels are lazily loaded: only the active tab's HTML fragment, scripts and
stylesheets are fetched, which is what keeps the first paint cheap on a
Raspberry Pi 1.

---

## Start — the overview pages

![Overview page](images/dashboard-overview.png)

The start tab is not a fixed screen but a **grid of cards you arrange
yourself**. Above, the fixture's cards:

- **Status card** — the current situation in one sentence
  ("1.25 kW surplus. Good time for the wallbox …"), a bar showing where the
  balance sits between grid import and feed-in, and the surplus / grid draw /
  battery / data-quality tiles with their thresholds.
- **Battery status card** — segmented charge column,
  time to full or empty, the reserve kept back for a grid outage, and the
  usable capacity.
- **Energy flow** — PV, storage, building, grid and the
  individual loads as animated flows whose speed follows the actual watts.
- **Self-sufficiency ring** — coverage versus use, split by
  source and by consumer.
- **System diagram** (titled *Installation* on the card) — a wiring-style
  diagram of the site with the live power on each leg.

Every card is driven purely by MQTT Discovery data plus the role assignment
from the Energy tab. Nothing here is hard-coded to a particular device.

### The layout editor

![Layout editor with the block picker open](images/dashboard-layout-editor.png)

**Edit** turns the overview into an editor: drag and resize cards, add
pages, and pick new blocks from the panel on the right. The picker has three
tabs — **cards**, **devices** and **entities** — so a layout page can mix
computed energy cards with a raw device tile or a single measured value.

| Block | Layout type | What it shows |
|---|---|---|
| Energy: status card | `energy_status` | Situation, thresholds, data quality |
| Energy: energy flow | `energy_flow` | Animated flow graph |
| Energy: self-sufficiency ring | `energy_ring` | Coverage/use ring |
| Energy: system diagram | `energy_schema` | Plant diagram |
| Energy: balance band | `energy_band` | Balance over time as a band |
| Energy: day band | `energy_day` | The day's curve |
| Energy: data board | `energy_board` | The balance as a plain number board |
| Battery: status card | `battery_status` | Charge column or projection |
| Diagnostics | `diagnostics` | Health summary of all devices |
| Value card | `entity_value` | One entity as a large value |
| Entity list | `entity_group` | Several entities in one card |
| (Devices tab) | `device` | A full device tile with its controls |

Editor width can be previewed as phone, tablet or monitor. Layouts are
versioned — every save is a revision that can be restored.

---

## Devices

Everything the node has seen through MQTT Discovery, grouped by device. Two
view modes, switched with the button in the top right:

**Compact** — name, availability, and the few headline values:

![Device list, compact mode](images/dashboard-devices.png)

**Details** — every entity of every device, with its component type and its
controls: sliders for `number` entities, toggles for `switch`, live values for
`sensor`:

![Device list, detailed mode](images/dashboard-devices-detailed.png)

Availability comes from each device's `availability_topic`; `Shelly Uni
Bank B` above is the fixture's deliberately offline device. `Details` on a
device opens a dialog with the raw discovery payload, the topics involved and
whether the discovery message was retained — the fastest way to answer "why
does Home Assistant not see this".

The default view mode is a setting, so a wall-mounted tablet can open straight
into compact mode.

---

## History

![History charts](images/dashboard-history.png)

The node stores **no** history. The browser records it: a recorder samples
`/api/v1/energy` on an interval and writes to IndexedDB, which is what keeps
the Pi's SD card and RAM out of the loop entirely.

- **Time range** — 1 h, 6 h, day, week, month, or a custom range.
- **Metric** — average, minimum or maximum per bucket.
- **Series** — every energy role plus computed series such as
  *House consumption (calculated)*. Click a chip to show or hide it.
- **Advanced: views & export** — save a set of series as a named view, and
  export the visible data as CSV or JSON.

Raw samples are compacted into one-minute and five-minute tiers as they age.
Two browsers that have the dashboard open at the same time will **exchange the
tiers each is missing** over the node — an SSE hub on the server, no data
stored server-side, and never overwriting a browser's own measurements.

The line above the chart names the tier, the number of points, the number of
series, and whether the browser has promised the storage as persistent.

---

## Configuration

![Configuration editor for the battery service](images/dashboard-config.png)

This is the editor for the **Python services' device JSON files** — the same
`*_devices.json` files described in
[Device services](device-services.md). The form is generated from the JSON
schema that sits next to each config file, so field names, help texts,
required markers and value ranges all come from one source.

Two things this page does that a text editor cannot:

- **It suggests MQTT topics and JSON keys from traffic it has actually seen.**
  The dropdowns above are filled from retained payloads on the broker, and a
  topic that has never carried a message says so instead of silently offering
  nothing.
- **It validates before writing.** A value outside the schema's range is
  rejected with an HTTP 400 rather than written and discovered later by a
  service that then fails to start.

Saving writes the file and asks the owning service to reload it — the response
carries the reload result, so a service that refused the new file says so here
rather than failing silently on the next restart. Editing automation rules
additionally requires the `automations` role and a CSRF token.

---

## Energy

![Energy roles and balance interpretation](images/dashboard-energy.png)

The bridge between "a pile of sensors" and "an energy balance". Each measured
entity is given a **role** — PV, grid, battery, house load, wallbox, heat
pump, battery state of charge, and the mirrored import/export and
charge/discharge variants. Several entities may share a role; they are summed.
Everything else stays unassigned and is listed at the top, because an
unassigned power sensor is the usual cause of a balance that does not add up.

**Balance interpretation** decides how house consumption is obtained:

| Mode | Meaning |
|---|---|
| Measured | Take the entity carrying the house consumption role |
| Calculated | PV + grid import + discharging − feed-in − charging |
| Combined | Calculated, with measured consumers shown separately |
| Automatic (default) | Measured if available, otherwise calculated |

Below that sit the knobs that the status card reads: whether a remaining
balance gap is folded into house consumption, the tolerance threshold in watts
or percent, and the thresholds at which the status card calls a situation a
surplus or a grid draw, plus the battery reserve.

The tiles at the bottom show every assigned role with its current value and
two flags: whether the value is fresh, and whether it is arriving live.

---

## Device map

![Device map](images/dashboard-device-map.png)

A free-form canvas of all devices. Nodes are placed by hand, coloured by
health, and can be connected with edges — straight, orthogonal or curved — to
record how the site is actually wired. It answers questions the automatic
views cannot: which meter sits ahead of which sub-distribution, what a device
is physically attached to.

Positions and edges are stored server-side and versioned like the layout, so
an accidental drag can be rolled back.

---

## Diagnostics

![Diagnostics](images/dashboard-diagnosis.png)

Two blocks:

- **Health score** — one row per device: score, status, the time of its last
  signal, the staleness threshold it is measured against, and the number of
  warnings. A device that has never sent shows as critical. In the screenshot
  every device is "unhealthy" because the fixture broker replays retained
  messages once and then goes quiet — on real hardware the poll intervals keep
  the scores up.
- **MQTT Discovery** — the count of devices, entities, parse errors and
  duplicate `unique_id`s, plus the concrete problems behind them. Duplicate
  unique IDs are the classic reason Home Assistant merges two devices into
  one.

Both can be filtered by severity, rule and device.

---

## Automations

![Automation rules](images/dashboard-automation.png)

The editor for the rules of the separate `automation_mqtt.py` service. Each
rule reads left to right: **conditions → state → actions**. A condition can be
a threshold on a balance field (with hysteresis and hold time), a raw MQTT
topic value, a Home Assistant style entity template, or a time window; an
action publishes to a topic or raises a notification.

The header shows whether the automation service is reachable. In the
screenshot it is offline — the smoke-test harness does not start it — so the
rules render from their saved state with no live values. That is deliberate:
the dashboard **never** executes rules itself. It only edits the JSON, and the
Python service owns evaluation and publishing. See
[Device services → Automations](device-services.md#automations).

The **Assistant** button walks through building a rule. **Show history**
shows the last triggers of a single rule.

---

## Settings

Eight sub-tabs: General, Appearance, History, MQTT, Tailscale, TinyTuya,
Versions and System. Tailscale and TinyTuya only appear when the matching
service is installed.

### General

![General settings](images/dashboard-settings-general.png)

Health threshold, sweep interval and live-update interval, plus the storage
health check — on a Pi this reads the SD card's wear counters, and it says so
plainly when the root filesystem is not a readable MMC medium. Settings are
versioned. **Settings revisions** restores an earlier state. The
footer lists every bundled JavaScript library with its version, license and
license link.

### Appearance

![Appearance settings](images/dashboard-settings-display.png)

Default device view mode, colour scheme, and what extra information the UI
shows: discovery JSON tooltips, the global status bar, configuration and
diagnostic values on tiles. **Tabs without a width limit** lets chosen tabs
use the full window width instead of the centred column, and
**Items in the system status** picks the fields in the status bar.

The **Formatting** card holds the language of this browser, the switch that
shows or hides the `DE | EN` pill for everyone, and the number format.

### History

![History settings](images/dashboard-settings-history.png)

Recording and retention for the browser-side history — sample rate, how long
raw data is kept before compaction, how long minute values survive. The
retention limit is either by time or by a storage budget in MB, and the page
shows the projected daily volume, the actual usage and the browser's quota.
The last block switches the device-to-device exchange off.

Note the wording on the page: the setting applies to every browser, the
recorded data does not — the measurements live only in the browser that
recorded them.

### MQTT

![MQTT settings](images/dashboard-settings-mqtt.png)

Broker connection with a live status table — connected, address, where the
configuration came from, connected since, last reconnect, failed attempts.
Two switches worth calling out:

- **Publish energy values as a Home Assistant device.** The dashboard then
  announces itself over MQTT Discovery as the device "Energy Node" with PV,
  grid, battery and house-load sensors, so the computed balance reaches Home
  Assistant without any extra service.
- **Use dashboard settings instead of the central configuration.** Off means
  the values from `/etc/energy-node/config.json` win unchanged; on means the
  settings stored here take over completely, with no field-wise merging.

Further down the same tab configures the **Mosquitto bridge to the main site**
and can apply and restart it.

### Tailscale

![Tailscale wizard](images/dashboard-settings-tailscale.png)

Three steps — check prerequisites, start login, check the result. It reports
whether Tailscale is installed and whether `tailscaled` is active and enabled,
and once connected shows the connection state, peer count and key expiry, with
buttons to re-check, log out or restart the service. It exists because
bringing a headless Pi onto a tailnet otherwise means an SSH session and a
copied login URL.

### TinyTuya

![TinyTuya wizard](images/dashboard-settings-tinytuya.png)

A four-step wizard — credentials, device, data points, apply — around the
`tinytuya` cloud lookup: enter the Tuya region and API credentials, list the
devices on the account, probe the device's data points locally, and write the
result into `tuya_devices.json`. The data point number for a switch is not
reliably `1`, which is the whole reason the probe step exists. Credentials are
only stored when explicitly asked for.

### Versions

![Versions settings](images/dashboard-settings-versions.png)

What is installed on the node. The top card shows the installed package with
its version, build date and architecture, and the version the dashboard itself
runs as. If the two differ, the page says so, which happens when the
dashboard was updated on its own after the installation. Below that every
application, service and library is listed with its version. **Changes**
opens its changelog, and a service that is not installed is marked as such.

The **Updates** card checks GitHub for a newer release on demand
(**Check for updates**).

### System

![System settings](images/dashboard-settings-system.png)

Who you are logged in as and with which role, and the system actions:
restart the dashboard service, restart the system, shut it down. The
screenshot shows an admin session, so the actions are enabled. In a guest
session they are greyed out, because system actions need a registered user
with the `system_actions` role **and** HTTPS.

Below that, the content of `/etc/energy-node/config.json` as a generated form,
same mechanism as the configuration tab. Passwords are never in that file,
only the paths of the files that hold them.

### Updating from the dashboard

![Update notice in the header](images/dashboard-update-available.png)

When a newer release is out on GitHub, the header shows a notice next to the
title (**Update … available**). For a registered user with the
`system_actions` role it opens the update page (`/redeploy/`), where
**View changes** lists what the new package changes. Everyone else gets the
release notes on GitHub in a new tab. The same notice appears under
*Settings → Versions*, next to the button that checks for updates on demand
(**Check for updates**) and the link **Release notes on GitHub**.

![Update preview](images/dashboard-update-preview.png)

From that notice, a registered user with the `system_actions` role opens the
update page (`/redeploy/`). It first downloads the newest signed release
package for the node's architecture, then shows the same preview as the
[desktop installer](installer.md#updating-a-node): which components change,
which services restart, and which services the node runs. **Update**
runs the pending steps on the node itself, without the installer and without
SSH.

---

## Colour schemes

Four themes ship with the dashboard, chosen in *Settings → Appearance*:

| Mint (default) | Power blue |
|---|---|
| ![Mint theme](images/dashboard-theme-default-mint.png) | ![Power blue theme](images/dashboard-theme-blue.png) |

| Signal yellow | Daylight |
|---|---|
| ![Signal yellow theme](images/dashboard-theme-yellow.png) | ![Daylight theme](images/dashboard-theme-light.png) |

Three are dark. **Daylight** is the light one, meant for a screen in a bright
workshop.

---

## Reproducing these screenshots

The smoke test starts the real dashboard against a fixture broker, with no
Raspberry Pi, no Mosquitto and no devices. The `docs-screenshots` preset is
built on the near-complete `alle-funktionen` fixture: battery SoC devices, the
seven energy entities of a sunny moment, a fully modelled APsystems EZ1 with
two strings and a writable power limit. It adds the layout, role assignment,
device map and the automation rule seen above.

```bash
dashboard/test/smoke/run-local-dashboard.sh --keep --preset docs-screenshots --simulate-package &
node dashboard/test/smoke/docs-screenshots.mjs
```

The first command prints a URL and credentials and stays up until Ctrl-C.
`--simulate-package` makes a fake GitHub offer version 9.9.9 with a small
package, which is what the update screenshots show. The second command writes
every image on this page into `docs/images/`. Two images need their own run:

- **History:** `docs-screenshots.mjs --history` keeps the dashboard open for
  about twelve minutes so the browser records enough history, then takes the
  History tab.
- **Versions:** restart the first command with `--simulate-installed` instead
  of `--simulate-package`, then run
  `docs-screenshots.mjs dashboard-settings-versions`.

For a single screenshot of any page, `screenshot.mjs` is the simpler tool:

```bash
node dashboard/test/smoke/screenshot.mjs --out /tmp/overview.png --lang en
```

The full option list, the other presets and the fixtures behind them are
documented in `dashboard/test/smoke/README.md` in the repository.

---

## See also

- [Device services](device-services.md) — what fills the dashboard with data
- [`knowledge/dashboard/api-documentation.md`](knowledge/dashboard/api-documentation.md) — the `/api/v1` HTTP API behind every page
- [`knowledge/data-flow.md`](knowledge/data-flow.md) — where each value comes from
- [`knowledge/dashboard/reverse-proxy.md`](knowledge/dashboard/reverse-proxy.md) — running the dashboard under a sub-path
