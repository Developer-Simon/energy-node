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
demo/demo.sh --docs             # fixture for the documentation screenshots
demo/demo.sh --no-simulate      # PV, grid, battery and load stay still
demo/demo.sh --devices DIR      # extra device configurations, the later source wins
```

Stop everything with Ctrl-C. The dashboard and all fakes end with it.

What always runs:

- the dashboard with HTTPS (self-signed certificate, accept it in the browser),
  the service logs, an installed-state, a package and the update fake
- the MQTT broker (`minibroker.py`)
- the fake Home Assistant companion
- the fake console
- the fake GitHub releases API

The first start prints the address, the login (user `admin`, the password is
fixed in `demo/lib/env.sh`) and the console address. After a change to the
code, stop the demo and start it again.

Default ports are in the 182xx range, so the demo can run next to the smoke
test (181xx). Set `DASHBOARD_DEMO_PORT`, `DASHBOARD_DEMO_COMPANION_PORT`,
`DASHBOARD_DEMO_CONSOLE_PORT`, `DASHBOARD_DEMO_MQTT_PORT` and
`DASHBOARD_DEMO_UPDATES_API_PORT` to use other ports. The demo refuses to start
while one of its ports is taken, and it does not delete anything in that case.

The state lives in `demo/.state/`: users, layout, history and the installer
state. `--reset` removes it. `--docs` uses its own state in `demo/.run/docs`,
which is removed again when the demo ends.

## Fake console

The console is a small web page with tabs for driving the fakes by hand. Its
*Companion* tab has three cards. *Kopplung* pairs and unpairs with a code,
sends a wrong code on purpose, and cuts or reopens the check stream. *HA-Benutzer*
edits, adds and removes users, and with the manual switch the checks wait for a
Yes, No or Timeout button. *Verlauf* signs the peer in and out of the exchange,
sets the days and a factor per energy role, offers again, and injects faults:
a delay, no answer, or only the first block.

A factor only affects periods the dashboard has not fetched yet. The log on the
right shows what both fake parts report.

The automatic run of the smoke test removes the pairing at its end, so create a
new pairing code in the user management before pairing from the console.

## Fakes

Each file in `demo/fakes/` imitates one part of the system:

- `minibroker.py`: a deliberately incomplete MQTT 3.1.1 broker for local use only.
  It relays retained and live messages, plays the services' status and can simulate the power values.
- `fake_ha_companion.py`: the Home Assistant companion, with pairing, the user table and the checks.
- `fake_companion.py`: the history peer of the energy statistics, served over TLS.
- `fake_console.py`: the web console with its tabs.
- `console_companion.py` and `console_companion_ui.py`: the *Companion* tab of the console, server side and page fragment.
- `fake_github_releases.py`: the GitHub releases API, so the update check finds a newer version.
- `fake_journal_helper.py` and `fake_sudo.sh`: the system action helper and the sudo shim that feed the *Service logs* section.
- `real_changelog.py`: copies the changelog of this repository into a simulated installer state.

The tests for the fakes are pytest tests in `demo/fakes`. Run them from
`demo/fakes` with the project's `.venv`: `.venv/bin/python3 -m pytest -q`.

## Fixtures

`demo/fixtures/` holds the data the demo and the tests load:

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
  demo, its seed layout and its device configurations.
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
