# Smoke test

`dashboard/test/smoke/smoke.sh` starts the real dashboard on the development
machine, together with the same fakes as the demo, and checks it end to end:
from the MQTT topics through the HTTP API to the configuration page. No
mosquitto, no access to the Pi and no real devices are needed. It is meant for
changes that jsdom or Go unit tests alone cannot convincingly cover (registry,
HTTP API and form together).

The script only checks. To look at the same dashboard in a browser, use the
demo, which keeps it running until Ctrl-C: see [demo/README.md](../../../demo/README.md).
The fakes and fixtures are described there too.

```bash
dashboard/test/smoke/smoke.sh                  # all five runs
dashboard/test/smoke/smoke.sh --run haupt      # one run, the option can be repeated
```

The runs always execute in the same order (`haupt`, `ohne-dienste`, `bilanz`, `seed`,
`falsches-passwort`),
whatever order `--run` names them in. Each run starts a fresh dashboard in its
own directory `.run/<name>-XXXXXX` below this folder. A passed run deletes that
directory, a failed run keeps it for analysis. The script exits with 0 only when
all selected runs pass.

Before a run starts, all five ports below are checked. A port that is already in
use makes the run fail, and the script leaves the process that holds it alone.

## The five runs

| Run | Configuration | Check groups |
|---|---|---|
| `haupt` | Sources, fixture and seed data of the demo (`demo/demo.sh`), HTTPS, simulated power values, service logs, installed state, package and update fake | `topics`, `battery_config`, `shelly`, `seed`, `dataflow`, `live_push`, `update_offer`, `package`, `installed`, `journal`, `users`, `onboarding`, `update_install`, `companion`, `history_exchange` |
| `ohne-dienste` | The demo sources, but all seven services switched off | `services_off` |
| `bilanz` | `energie-ueberschuss.json` with the seed set `energie-alle-karten`, only the battery service, no simulation | `seed`, `energy_balance`, `companion` |
| `seed` | The demo sources, plus an installer `onboarding-seed.json` next to `auth.pw` before the start | `onboarding_seed` |
| `falsches-passwort` | The demo sources, but the MQTT password of the dashboard is wrong. The fake broker accepts only the right one (`--login`) and rejects the connection | `mqtt_falsch` |

Why each run exists:

- **`haupt`** checks the demo itself. The demo shows the same fixture, the same
  seed data and the same services.
- **`ohne-dienste`** contradicts the demo on purpose. With all seven services
  off, only the `services_off` group applies.
- **`bilanz`** needs exact energy values, which the simulation in the demo
  deliberately changes. Over plain HTTP this run also runs the probe
  "without the HTTPS flag refused", which is skipped under TLS.
- **`falsches-passwort`** checks that the dashboard starts while the broker
  refuses it. The status names the refusal, the password does not show up in
  it, and the login page still answers.

The check groups are the functions in `checks/`. Each group sets `FAILED=1` on
an error and continues, so one run reports all its failures.

## Ports

| Variable | Default | Used for |
|---|---|---|
| `DASHBOARD_SMOKE_PORT` | 18100 | dashboard, HTTP |
| `DASHBOARD_SMOKE_MQTT_PORT` | 18883 | fake MQTT broker |
| `DASHBOARD_SMOKE_UPDATES_API_PORT` | 18884 | fake GitHub releases API |
| `DASHBOARD_SMOKE_COMPANION_PORT` | 18101 | fake history peer |
| `DASHBOARD_SMOKE_CONSOLE_PORT` | 18102 | fake console |

The demo uses the 182xx range by default, so both can run side by side. To use
other ports, set the variables above before the call.

## CI

The CI job `dashboard-smoke` runs the same script (see `.github/workflows/ci.yml`).

## What is checked

The checks below are the ones of the former smoke test. The table above says
which group runs in which run, and the scripts in `checks/` are the source of
truth for the details.

- `/api/v1/topics/samples` returns the last payload per topic, sorted
- a topic without a message stays without a payload (instead of an invented value)
- the battery service configuration appears as a managed configuration
- **decimal numbers can be saved and read back** (`charge_efficiency` at 0.95),
  the regression this harness was created for
- the server rejects a value outside the schema (1.5) with HTTP 400
- the service status of `battery_soc` carries the checksum of the file as `ok`
  before and after the saves above, and the service counts as `active` in `/api/v1/health`
- a seeded `layout.json` appears with visible cards under `/api/v1/layout`
- a seeded `energy.json` appears with assignments under `/api/v1/energy/roles`
- the seeded theme is present in `/api/v1/settings`
- in `bilanz`, the balance under `/api/v1/energy` works out with no remainder
  (PV 4200 W, feed-in 1250 W, battery 900 W, "Übriger Verbrauch" 1000 W,
  `gap_applied == 0`), the exact values that the simulation deliberately
  violates, which is why the two never run together
- with the simulation on, the PV power under `/api/v1/topics/samples` changes
  within ten seconds, proof that the simulated values arrive as MQTT `PUBLISH`
  on the open connection and not only that the broker process is alive

The `mqtt_falsch` group (run `falsches-passwort`) checks that the dashboard
starts without an MQTT connection: the login page answers with HTTP 200,
`/api/v1/mqtt/status` shows `connected: false` with a `last_error` that names
the refusal, and the password itself is not part of the status.

The `companion` group also checks the history exchange interface. The
announcement under `/api/v1/history/exchange` names protocol version 1 and
exactly the tiers `1m` and `5m` (the raw tier is never exchanged), and an offer
from one peer reaches exactly the other over two simultaneously open SSE
streams, not the sender itself. This is the only check that brings two clients
together on the real server. The hub and ring buffer are in
`internal/historyexchange`, and the browser part is checked on its own in
`dashboard/test/history-exchange.test.mjs`.

The check injects its offer synthetically via `curl`, which bypasses the
compaction in the browser. In a manual test with two real tabs that is a
difference: 1m and 5m sets only come into being once `history-recorder.js` has
compacted its raw data, and that happens only for data older than
`history_raw_window_hours` (default 24h, see `compact()` in
`history-maintenance.js`). Two freshly opened tabs have nothing to exchange at
first. That is not a bug in the exchange interface but a consequence of the
default retention.

## Tools around the test

- Screenshots, the mobile overflow check and the client cost measurement run
  against the demo and are described in
  [dashboard/test/browser/README.md](../browser/README.md).
- The fake companion, the fake console and the other fakes, as well as the
  fixtures, are described in [demo/README.md](../../../demo/README.md).
