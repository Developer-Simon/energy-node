# Contributing

Energy Node started on one specific site's hardware, but the goal is a
flexible, general solution anyone with a remote energy site could run as-is —
see [About this repository](README.md#about-this-repository). Getting there is
what contributions are for, and every kind is welcome: bug reports, fixes,
documentation, dashboard improvements, and pushing site-specific assumptions
out into configuration.

What helps the most is **support for a family of devices Energy Node doesn't
cover yet** — a new bridge widens what everyone can monitor without touching
the rest of the system. If you only send one thing upstream, make it that;
see [New device services](#new-device-services) below.

(Forking and adapting privately is fine too, and no one expects you to
upstream that work — but it is no longer the assumed way to use this.)

## Before you start

- For anything beyond a small fix, open an issue first describing the
  change. This is a single-maintainer project; a heads-up avoids wasted work
  on a PR that doesn't fit the architecture in [`dashboard/AGENTS.md`](dashboard/AGENTS.md)
  or the invariants documented under [`docs/`](docs/).
- Bridge- or dashboard-specific conventions live next to the code
  (`dashboard/AGENTS.md`, the module docstrings under `services/` and `libs/`). Read the one
  for the area you're touching before changing it.

## New device services

The most useful thing you can send upstream is a **bridge for hardware
Energy Node doesn't support yet** — another inverter, meter, battery stick
or switchable load. The device-service pattern is deliberately uniform, so a
new one is mostly filling in a known shape rather than inventing
architecture:

- Each service is a single systemd unit that polls one family of hardware
  **locally, with no cloud account**, and publishes Home Assistant MQTT
  Discovery entities under `outstation/<id>/…`. Nothing talks HTTP to the
  dashboard — the broker is the only coupling.
- [`libs/energy_node_common/`](libs/energy_node_common/) already provides the
  MQTT setup and last will, discovery-payload construction, availability
  publishing, the poll scheduler with its separate diagnostic-poll cycle,
  the central config loader, and both sides of the master/slave poll-rate
  protocol. A new bridge consumes that package; it should not re-implement
  any of it.
- [`services/shelly/`](services/shelly/) (read + switch) and
  [`services/trucki/`](services/trucki/) (read-only) are the cleanest templates. Copy
  the one whose direction matches your device.
- Ship the service with a `*_devices.schema.json` next to it — the dashboard
  renders that schema as the configuration form, so a new service is
  editable from the UI with no dashboard changes.
- Keep it honest on a Raspberry Pi 1: declare capabilities per device rather
  than probing for them, put slow-changing values on the diagnostic cycle,
  never publish credentials, and support the MQTT simulation mode every
  other slave has.

[`docs/device-services.md`](docs/device-services.md) describes every
existing service and the conventions they share — read it before starting.
Open an issue naming the hardware first, so the maintainer can flag anything
site-specific that would block a merge.

## Development setup

See **[INSTALLATION.md](INSTALLATION.md)** ("Development machine") for the
full environment. In short:

```sh
python3 -m venv .venv && .venv/bin/pip install -e libs/energy_node_common -e libs/battery_soc_core pytest
.venv/bin/pip install -r requirements-dev.txt   # needed for the full services/ test suite
```

The Home Assistant integration needs its own virtualenv — see the
[README's Development section](README.md#development) for why and how.

## Running the checks

Run whichever of these apply to your change before opening a PR — CI runs
all of them:

```sh
.venv/bin/pytest services libs scripts/tests                                    # Python bridges + tooling
bash scripts/tests/test_publish_mirror.sh                             # HACS mirror assembly
cd integrations/homeassistant && ../../.venv-ha/bin/pytest            # HA integration
cd dashboard && gofmt -l . && go vet ./... && go test ./...           # Go dashboard
cd dashboard && npm install && npm test                               # dashboard JS
dashboard/test/smoke/run-local-dashboard.sh                           # real dashboard, no Pi, no broker
```

## Secrets

Never commit real credentials, IPs from your own site's private range, or
device serials — see
[`docs/knowledge/dashboard/secrets-and-credentials.md`](docs/knowledge/dashboard/secrets-and-credentials.md).
`./scripts/deploy/check_tracked_secrets.sh` scans tracked files for obvious
leaks; run it before pushing.

## Disclose AI use

If an AI coding assistant helped write your change, say so in the PR
description — see [AI-DISCLAIMER.md](AI-DISCLAIMER.md) for what to include
and why. This is a hard requirement, not a preference.

## Commit hygiene

Commit messages follow [Conventional Commits](https://www.conventionalcommits.org):
a `type(scope): summary` subject, where `type` is one of `feat`, `fix`,
`docs`, `test`, `refactor`, `chore`, `ci`, `build` or `perf`, and `scope`
names the area touched (`dashboard`, `shelly`, `battery-soc`, `docs`, …).
Keep the summary imperative and under ~72 characters; put the rationale in
the body. This is a rule, not a suggestion — matching the existing history
in `git log` is the quickest way to get it right.

Mark a breaking change with a `!` before the colon (`feat!: …`,
`fix(dashboard)!: …`). The generated component changelog keeps the commit in
its normal section but prefixes the entry with **⚠ Breaking**.

Per-component `VERSION` files are patch-bumped on the PR branch by CI
(`.github/workflows/version-bump.yml`); `major`/`minor` you bump by hand on the
branch. CI (`.github/workflows/ci.yml`, the *Vendored artefacts in sync* job)
also fails a PR that leaves the vendored `battery_soc_core` copy under
`integrations/homeassistant/` or the vendored `battery-card-core.js` in the
Lovelace card out of sync with its source — run
`.venv/bin/python scripts/vendor_core.py` / `scripts/vendor_card.py` and commit
the result if it fires.
