# Local demo

`demo/demo.sh` starts the real dashboard on the development machine, together
with fake versions of everything it talks to: an MQTT broker, the Home
Assistant companion, a web console for driving the fakes and the GitHub
releases API. No Pi, no real devices and no real broker are involved. Use it to
look at the dashboard in a browser or to try a change by hand.

The checks themselves live in `dashboard/test/smoke/`. The demo is the
persistent counterpart: it keeps running until you stop it.

## Local demo

```bash
demo/demo.sh                    # start, the state is kept between runs
demo/demo.sh --reset            # delete the state, then set up again
demo/demo.sh --stop             # stop the running demo (with --docs: the docs instance)
demo/demo.sh --docs             # fixture for the documentation screenshots
demo/demo.sh --example          # fresh empty node with the example set and its shelly and apsystems services, state in demo/.run/example
demo/demo.sh --no-simulate      # PV, grid, battery and load stay still
demo/demo.sh --new-cert         # create a new TLS certificate, the state stays
demo/demo.sh --devices DIR      # extra device configurations, the later source wins
```

Stop everything with Ctrl-C. The dashboard and all fakes end with it.

What always runs:

- the dashboard with HTTPS (self-signed certificate, accept it in the browser),
  the service logs, an installed-state, a package and the update fake
- the MQTT broker (`minibroker.py`)
- the fake Home Assistant companion and its history peer, which fills the statistics page
- the fake console
- the fake GitHub releases API

The performance page (Einstellungen, Leistung) shows invented measurements in the
demo, set by `ENERGY_NODE_PERFORMANCE_FAKE=1` in `demo/demo.sh`, so its budget
and measurement cards have values to show. The scenarios are switched in the
console tab *Leistung*, without a restart.

The first start prints the address, the login (user `admin`, the password is
fixed in `demo/lib/env.sh`) and the console address. After a change to the
code, stop the demo and start it again.

Default ports are in the 182xx range, so the demo can run next to the smoke
test (181xx). Set `DASHBOARD_DEMO_PORT`, `DASHBOARD_DEMO_COMPANION_PORT`,
`DASHBOARD_DEMO_CONSOLE_PORT`, `DASHBOARD_DEMO_MQTT_PORT`,
`DASHBOARD_DEMO_UPDATES_API_PORT` and `DASHBOARD_DEMO_BROKER_CONTROL_PORT`
(default 18285, the control port of the broker) to use other ports. The demo refuses to start
while one of its ports is taken, and it does not delete anything in that case.

The state lives in `demo/.state/`: users, layout, history and the installer
state. `--reset` removes it. `--docs` uses its own state in `demo/.run/docs`,
which is removed again when the demo ends.

`--example` starts a fresh node for one run: no layout, no settings and the
onboarding open, as on a first start, plus the example set (`example_pv`,
`example_grid`, `example_plug` and the scenario `example_surplus`). The shelly and
apsystems services run against the demo broker, so the example devices show live
values while the simulation is on. Its state
lives in `demo/.run/example` and is deleted at the start and at the end of the
run. `demo/.state` is not touched. `--stop --example` ends that instance.

The Monitoring tab (*Monitoring*, before *Diagnostics*) shows its notices from
the start, without any step in the console: a fixed shade in the PV (the
direction, height and loss come from the learning data), a measurement chain
with a finding, and values without a role. Its learned values come from the
seeded learning data of the demo. The PV reserve only gets a value after the
PV throttle of the console has run for a few minutes, see below.

## Fake console

The console is a web page for driving the fakes by hand. The sidebar lists one
tab per area with its status. The button *Alles auf Start zurücksetzen* at the
bottom resets the fakes only. It leaves users, history and the pairing of the
companion as they are.

- *Companion*: pairs and unpairs with a code, sends a wrong code on purpose, cuts or reopens the check stream, edits the Home Assistant users with the manual switch, and sets the days, the factor and the faults of the history peer.
- *MQTT-Broker*: takes a device offline, lets a service refuse or stay silent, switches the simulation, requires a login (wrong MQTT password), publishes a message and disconnects clients.
- *Updates*: answers the release check and the download, and sets the installer state to installed, none, previous or reset.
- *System*: feeds the service logs with journal lines or injected errors, sets how the system helper answers, and sets which system actions succeed.
- *Leistung*: switches the scenario of the performance page with one button per preset (measuring, good, tight, over budget, throttled and hot, memory short, alone without services, many browsers). The fields below set the single values (measured time, cores, CPU, temperature with an unknown switch, throttling, RAM, swap, browser clients, foreign device with its interval or none), the table sets each unit on or off with its load. Every change is written to `fake-performance.json` in the demo directory, which the dashboard reads on each snapshot. A change makes the scenario custom values. The button *Messdauer um 1 Stunde erhöhen* adds an hour to the measured time.
- *Auto-Lernfunktionen*: three sections. *Regelprüfung* inserts example rules into the demo's automation rules file, one button per finding of the rule check (duplicate rule, contradicting rules, conflict with a regulator, condition that can never hold, missing device, never fired, fires very often), or all of them at once. The example rules carry the id prefix `chk_`. *Entfernen* removes one example again, a reset removes all of them. Reload the page Automationen after a change, the finding appears with the next refresh of the check. The example *Löst nie aus* needs the first tick of the demo, which runs with `ENERGY_NODE_AUTOLEARN_FAST=1`. The demo has no automation service, so the tab publishes the trigger counts of the example rules itself. *Löst nie aus* is reported with zero firings, and for *Löst sehr oft aus* the button *4× auslösen* reports four firings at once. *Ruht saisonal* cannot be produced by an insertion, it needs a year of statistics. *Lerndaten* writes day profiles for the past 1 to 56 days (default 21, the minimum for the detectors) into `autolearn/days` of the demo's data directory. They cover the four devices the demo registers (`plug_balkon`, `plug_entfeuchter`, `relais_keller`, `waermepumpe`) with their real switch entities and switching events by origin. The profiles make suggestions for switch time (`plug_balkon` off at about 22:30), forgotten runtime (`plug_entfeuchter`), coupling (`relais_keller` followed by `plug_balkon`) and an overridden rule (`regel_akku_leer_wp_aus` on `waermepumpe`) appear. The detectors run once a day at 03:30, or after a start when the last run is more than a day old, and read the profiles then. On a node that catch-up waits five minutes, in the demo (`ENERGY_NODE_AUTOLEARN_FAST=1`) it runs on the first minute tick, so suggestions appear about a minute after the start. The switches *Muster lernen* and, for the overridden rule, *Zuverlässigkeit* must be on. Each file carries `"fake": true`, *Tagesprofile entfernen* deletes only such files and leaves real profiles alone. *Hinweis „neu aktiv“ zurücksetzen* empties `learning_acknowledged` in `settings.json` and keeps the other fields. The dashboard reads the settings once and keeps them in memory, so the hint returns after a restart of the dashboard, not after a reload of the page. The tab gets the data directory from `--data-dir`, which the demo passes. *PV-Reserve* switches the PV throttle of the broker. The PV of the demo follows the sun over Munich and the south roof of the learning data, with the same shade in the west. Throttled, the PV delivers half, the grid stays just above zero and the batteries do not charge, so the dashboard counts the minutes as throttled and the PV reserve in the learned value dialog shows the rest in W after a few minutes. This works only by day and with the simulation on. On the first start the demo also writes a started day file for today (house values up to now), so the remaining surplus of today has a comparison, and the broker registers a washing machine (`waschmaschine`) whose seeded cycles give the operating power and the cycle energy.
- *Speicher*: plugs the demo sticks in and out and sets whether formatting or mounting fails. Pulling out the drive in use empties the mount point, and plugging it back in restores its content and volume marker. Plugging in the foreign stick mounts it with its own marker, so the storage page shows that it does not belong to this node. The status lines show what is plugged and what is mounted.

A factor only affects periods the dashboard has not fetched yet. The automatic
run of the smoke test removes the pairing at its end, so create a new pairing
code in the user management before pairing from the console.

The log at the bottom shows the messages of all fakes. The filter *Dieser
Bereich*, *Alle* or *Nur Fehler* chooses which ones are listed, and *Pause*
stops the list from growing.

### Triggering a chain finding

The demo chain *Balkonspeicher* runs plug, inverter and battery. Its learned
values are seeded for 14 days, so the diagnoses work from the first start. A
finding comes from a point that behaves differently than the learned loss
expects. The demo holds a finding for 30 seconds, so it shows quickly.

The second demo chain *Werkstatt* runs a socket and the consumer behind it, and it flows in one direction only, so its efficiency curve has a single series.

1. Start the demo with `demo/demo.sh` and open the console.
2. In the *MQTT-Broker* tab, switch the *Simulation* off. Otherwise the simulation
   overwrites the plug value again on its next tick.
3. To set the plug to 0 W, send the topic `plug/speicher/status` with the payload
   `{"apower": 0}` in the *Nachricht senden* group. The inverter still shows
   power, so the chain reports a contradiction.
4. To hold the plug instead, send its current value and leave it unchanged. A
   point that does not move while the others do can be reported as frozen.
5. The finding shows up after the hold time in the tab *Effizienz*, in the
   energy tab, in the diagnosis and on the device map.
6. Switch the *Simulation* on again to let the values move.

## Fakes

Each file in `demo/fakes/` imitates one part of the system:

- `minibroker.py`: a deliberately incomplete MQTT 3.1.1 broker for local use only.
  It relays retained and live messages, plays the services' status and can simulate the power values.
- `fake_ha_companion.py`: the Home Assistant companion, with pairing, the user table and the checks.
- `fake_companion.py`: the history peer of the energy statistics. It signs in to the HTTPS dashboard without checking the certificate and offers hourly values for 400 days, so the statistics page has data right after the start.
- `fake_console.py`: the web console with its tabs.
- `console_page.py`: the page of the console, with the sidebar, the log and the tab frame.
- `fakelog.py`: the log of the console, a ring buffer with levels.
- `console_remote.py`: the JSON client the fakes in their own processes are reached through.
- `console_companion.py` and `console_companion_ui.py`: the *Companion* tab of the console, server side and page fragment.
- `console_mqtt.py` and `console_mqtt_ui.py`: the *MQTT-Broker* tab, server side and page fragment.
- `console_updates.py` and `console_updates_ui.py`: the *Updates* tab, server side and page fragment.
- `console_system.py` and `console_system_ui.py`: the *System* tab, server side and page fragment.
- `console_performance.py` and `console_performance_ui.py`: the *Leistung* tab, which writes the scenario file the dashboard reads.
- `console_autolearn.py`, `console_autolearn_ui.py` and `test_console_autolearn.py`: the *Auto-Lernfunktionen* tab, which inserts the example rules into the demo's automation rules file and writes, removes and resets the learning data (day profiles and the acknowledgement).
- `fake_github_releases.py`: the GitHub releases API, so the update check finds a newer version.
- `fake_journal_helper.py` and `fake_sudo.sh`: the system action helper and the sudo shim that feed the *Service logs* section.
- `real_changelog.py`: copies the changelog of this repository into a simulated installer state.

Restarts that the node asks the helper for, such as after a backup restore or a drive setup, leave a request file for the fake. `demo/demo.sh` then restarts the dashboard itself, so a restored archive and a set up drive take effect at once.

The storage page has three demo sticks, listed in `demo/fixtures/storage-catalog.json`:
`sdb`, an exFAT stick that needs formatting, `sdc`, an empty ext4 stick that is ready,
and `sdd`, a stick whose volume marker belongs to another node. Only `sdb` is
plugged in at the start, as in `demo/fixtures/storage-drives.json`. The dashboard
reads the plugged sticks from `drives.json` in the demo's storage folder instead of
`lsblk`, so the demo never sees real disks. The fake helper formats and mounts into
the mount point of that folder and writes the volume marker there. Setting up the
restart at the end of the setup leaves a request file, so `demo/demo.sh` restarts
the dashboard and the drive becomes active by itself.

The tests for the fakes are pytest tests in `demo/fakes`. Run them from
`demo/fakes` with the project's `.venv`: `.venv/bin/python3 -m pytest -q`.

## Draft proxy

`demo/draft_proxy.py` puts a draft on the real pages of the running demo. The
dashboard embeds its assets with `go:embed`, so a draft that should run as real
frontend code cannot just drop files into `static/`. The proxy sits in front of
the demo, passes everything through and inserts the draft's files into the HTML
responses.

```bash
demo/demo.sh                                    # terminal 1
python3 demo/draft_proxy.py drafts/onboarding   # terminal 2
```

Open `https://localhost:18300` and accept its own self-signed certificate. The
draft files are read on every request, so a reload shows a change.

Each draft folder holds an `inject.json`:

- `assets`: the folder served under `/__draft/`.
- `page` and `head`: on a full page (recognised by the `page` marker), a
  `<link>` or `<script defer>` for each file in `head` goes before `</head>`.
- `insert`: fragments placed before a `before` marker, in responses that contain
  `when` (default: the `before` marker itself). This covers lazily loaded panel
  fragments as well.
- `replace`: a text replacement in the response for `path`, for example a list
  in a page script.

A marker or a replacement text that is missing is reported on the console,
because the template changed and the draft has to follow. The file doubles as
the production diff: the implementation changes the templates at exactly these
places. The proxy uses the standard library only. Its tests are
`demo/fakes/test_draft_proxy.py`.

## Fixtures

`demo/fixtures/` holds the data the demo and the tests load:

- `leer.json`: an empty broker list, the fixture of `--example`, so the node starts without demo devices.
- `alle-funktionen.json`: the fixture of `--docs`. Nearly complete, with the
  battery devices, the seven energy entities and a modelled AP Systems inverter
  with two PV strings, a power limit and an operating-status switch. Every
  device has an availability topic, and only `bms_bank_b` is offline on purpose.
- `battery-soc.json`: the retained messages of four battery and meter
  discovery configurations, with the matching state payloads. It includes a bare
  number that is not JSON, and a device that never sends data. It is the default
  of `minibroker.py` and is used by the tests.
- `energie-ueberschuss.json`: seven entities with `unit_of_measurement: W` for a
  sunny moment. PV 4200 W, feed-in 1250 W, battery charging 900 W, house
  consumption 2050 W, so the balance works out exactly. Its seed set is
  `seed/energie-alle-karten`.
- `demo.json` with `seed/demo` and `devices/demo`: the fixture of the persistent
  demo, its seed layout and its device configurations. It includes the measurement
  chain `Balkonspeicher` (plug, inverter, battery), whose points the simulation
  keeps at plausible losses.
- `seed/docs-screenshots` with `devices/docs-screenshots`: the seed layout and the
  automation rule for the `--docs` screenshots.
- `installed-state/`: the simulated installer state (`changelog.json`,
  `installed-manifest.json`, `previous-manifest.json`, `selection.json`).

## Known gaps

- The `energie-kombiniert` fixture has been removed. Nothing covers `load_mode`
  `combined` with band and board per entity and the collected ring any more.
- `real_changelog.py` is not run by the demo. To use the real changelog, run it
  by hand against `demo/.state/installer-state`, for example
  `demo/fakes/real_changelog.py . demo/.state/installer-state`.

## Learning seasons (PR 4a)

The demo seeds the learning data of the season features. A demo that was
started before this change keeps its old data, so run `demo/demo.sh --reset`
once to see the new findings.

| Feature | Where to look | Demo status |
|---|---|---|
| Sun times for switch times | Automationen, switch time suggestions | Shown: `sun_on` for the balcony plug, `sun_off` for the cellar relay |
| Clock switch time | Automationen, switch time suggestions | Shown: balcony plug off at 22:30 (`switch_time.off`) |
| Sun before sunrise | Automationen | Not seeded. It needs weeks of real history |
| Coupling | Automationen | Shown: the cellar relay switches the balcony plug |
| Runtime forgotten | Automationen | Shown: the dryer runs for hours on two days |
| Overridden check with sun window | Regelprüfung | Shown: the heat pump rule is overridden after sunset, the fix is a `sun_window` |
| Away standby | Automationen | Shown: the heat pump on three away days |
| Outdoor temperature setting and picker | Einstellungen, outdoor temperature | Shown: the demo outdoor sensor is in the broker. Choose it in the picker by hand |
| Heating knee and season chip | Automationen, finding of the heat pump | Shown: knee of 15 degrees, the last three days are cold, so the chip is visible. Warm weather hides it |
| Temperature limit for on rules | Automationen, Übernehmen | Shown: the PV surplus rule gets a limit, the off rule does not |
| Season memory | Not shown on its own | Seeded for 20 weeks, used by the limit and the year course. Backfill from a running node is not shown |
| Year course of a learned value | Automationen, learned values | Shown: the usual off time of the balcony plug varies by week |
| Quiet house state | Automationen, learned values | Shown through the fallback |
| Learning level row and draft | Leistung page | Partly: the learning share is seeded and the suggested level appears. Übernehmen is a click |
| Learning breakdown in the dashboard | Dashboard | Partly: the same data, once the learning level is set |
| Location missing hint | Einstellungen, location | Not seeded. By hand: clear the location in the settings, the hint appears |
| Clock fallback without location | Automationen | Not seeded. By hand: clear the location in the settings, sun suggestions fall back to clock times |
| Energy group switch | Einstellungen | By hand: switch the energy group off, its suggestions disappear |
| Explanation texts on cards | Automationen | Shown on every seeded card |
